package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/drewlsvern/rest-o-matic/internal/central"
	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/secrets"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

var (
	enrolToken          string
	enrolCAFile         string
	enrolAllowHTTP      bool
	enrolAcceptRecovery []string
	enrolForce          bool
)

var enrolCmd = &cobra.Command{
	Use:     "enrol <url>",
	Aliases: []string{"enroll"},
	Short:   "Connect this host to the central app",
	Long: `enrol registers this host with the central app at <url>, using the one-time
token the central app gave you, given with --token or typed at a prompt.
From then on every tick reports to the central app. Nothing is ever sent
for a config file other than the one given now (--config).

This host's key is created first if there isn't one. The address, the
host's ID and the credential it uses from now on are kept beside the key,
readable only by you.

If the central app offers recovery keys, each new one is shown and added to
the recovery recipients only once you confirm it, or name it with
--accept-recovery-key.

The address must be https:// and its certificate must be valid. If it is
issued by a private authority, pass that authority with --ca-file.
--allow-http permits a plain http:// address, for testing only.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := keyPath()
		if err != nil {
			return err
		}
		enrolmentPath := central.EnrolmentPath(path)
		if _, err := os.Stat(enrolmentPath); err == nil && !enrolForce {
			what := "this host is already enrolled"
			if e, err := central.LoadEnrolment(enrolmentPath); err == nil {
				what = fmt.Sprintf("this host is already enrolled with %s as %s", e.URL, e.HostName)
			}
			return fmt.Errorf("%s; use --force to replace that enrolment, or `rest-o-matic unenrol` to remove it", what)
		}

		url, err := central.CheckURL(args[0], enrolAllowHTTP)
		if err != nil {
			return err
		}
		caFile := ""
		if enrolCAFile != "" {
			if caFile, err = filepath.Abs(enrolCAFile); err != nil {
				return err
			}
		}
		if _, err := loadAndValidate(); err != nil {
			return err
		}
		cfgPath, err := canonicalPath(configPath)
		if err != nil {
			return err
		}
		version := readBuildInfo().Version
		client, err := central.NewClient(central.Settings{URL: url, CAFile: caFile, AllowHTTP: enrolAllowHTTP, Version: version})
		if err != nil {
			return err
		}
		token, err := readToken()
		if err != nil {
			return err
		}

		key, err := secrets.LoadHostKey(path)
		if errors.Is(err, secrets.ErrNoHostKey) {
			if _, err := secrets.Generate(path); err != nil {
				return err
			}
			fmt.Println("created this host's key:", path)
			key, err = secrets.LoadHostKey(path)
		}
		if err != nil {
			return err
		}

		var host central.Host
		if st, err := state.NewStore(statePath(), lockDir()).Load(); err == nil {
			host = hostInfo(nil, st)
		} else {
			host = hostInfo(nil, &state.State{})
		}
		ctx, cancel := context.WithTimeout(context.Background(), contentTimeout)
		defer cancel()
		reply, err := client.Enrol(ctx, central.EnrolRequest{
			FormatVersion: central.FormatVersion,
			Token:         token,
			PublicKey:     key.PublicKey(),
			Host:          host,
		})
		if err != nil {
			return fmt.Errorf("enrolling with %s: %w", url, err)
		}

		e := &central.Enrolment{
			URL: url, HostID: reply.HostID, HostName: reply.HostName, Credential: reply.Credential,
			ConfigPath: cfgPath, CAFile: caFile, AllowHTTP: enrolAllowHTTP, EnrolledAt: time.Now().UTC().Truncate(time.Second),
		}
		if err := central.SaveEnrolment(enrolmentPath, e); err != nil {
			return err
		}
		fmt.Printf("%s with %s as %s, for %s\n", color.Stdout.Success("enrolled"), url, reply.HostName, cfgPath)

		added, err := offerRecoveryKeys(path, reply.RecoveryRecipients)
		if err != nil {
			return err
		}
		if added > 0 {
			offerRelock(path, key)
		}
		return nil
	},
}

var unenrolCmd = &cobra.Command{
	Use:     "unenrol",
	Aliases: []string{"unenroll"},
	Short:   "Disconnect this host from the central app",
	Long: `unenrol removes this host's enrolment, so that it stops reporting to the
central app. The host key and recovery recipients are kept. The host also
remains listed in the central app until it is deleted there.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := keyPath()
		if err != nil {
			return err
		}
		enrolmentPath := central.EnrolmentPath(path)
		url := ""
		if e, err := central.LoadEnrolment(enrolmentPath); err == nil {
			url = " from " + e.URL
		}
		switch err := central.RemoveEnrolment(enrolmentPath); {
		case errors.Is(err, central.ErrNotEnrolled):
			fmt.Println("this host is not enrolled")
			return nil
		case err != nil:
			return err
		}
		fmt.Printf("unenrolled%s; the host key was kept\n", url)
		return nil
	},
}

func init() {
	enrolCmd.Flags().StringVar(&enrolToken, "token", "", "the one-time enrolment token (prompted for when not given)")
	enrolCmd.Flags().StringVar(&enrolCAFile, "ca-file", "", "also trust the certificate authorities in this PEM file")
	enrolCmd.Flags().BoolVar(&enrolAllowHTTP, "allow-http", false, "allow a plain http:// address (for testing only)")
	enrolCmd.Flags().StringArrayVar(&enrolAcceptRecovery, "accept-recovery-key", nil, "add this recovery public key if the central app offers it (repeatable)")
	enrolCmd.Flags().BoolVar(&enrolForce, "force", false, "replace an existing enrolment")
}

// stdin is shared by every prompt, so that nothing one reads ahead is lost
// to the next.
var stdin = bufio.NewReader(os.Stdin)

// stdinIsTerminal is whether someone can answer a prompt.
func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// readToken returns --token, or reads the token from a prompt that doesn't
// show it, or from standard input when that isn't a terminal.
func readToken() (string, error) {
	if enrolToken != "" {
		return enrolToken, nil
	}
	var token string
	if stdinIsTerminal() {
		fmt.Fprint(os.Stderr, "Enrolment token: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("reading the token: %w", err)
		}
		token = string(b)
	} else {
		line, err := stdin.ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no enrolment token: pass --token, or run enrol in a terminal to be asked for it")
		}
		token = line
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("no enrolment token given")
	}
	return token, nil
}

// confirm asks a yes/no question. Without a terminal there is nobody to
// answer, and the answer is no.
func confirm(question string, defaultYes bool) bool {
	if !stdinIsTerminal() {
		return false
	}
	choices := "[y/N]"
	if defaultYes {
		choices = "[Y/n]"
	}
	fmt.Printf("%s %s ", question, choices)
	line, _ := stdin.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	case "":
		return defaultYes
	}
	return false
}

// offerRecoveryKeys adds each offered recovery key that isn't listed yet
// to the recovery recipients file, if it was named with
// --accept-recovery-key or the user confirms it. It returns how many it
// added.
func offerRecoveryKeys(keyPath string, offered []string) (int, error) {
	recipientsPath := secrets.RecipientsPath(keyPath)
	listed, err := listedRecipients(recipientsPath)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, publicKey := range offered {
		publicKey = strings.TrimSpace(publicKey)
		if listed[publicKey] {
			continue
		}
		if _, err := secrets.ParseRecipient(publicKey); err != nil {
			fmt.Fprintf(os.Stderr, "%s the central app offered a recovery key that is not valid, and it was ignored: %v\n", color.Stderr.Warn("warning:"), err)
			continue
		}
		fmt.Println("The central app offers this recovery key:")
		fmt.Println("  " + publicKey)
		accepted := false
		for _, k := range enrolAcceptRecovery {
			accepted = accepted || strings.TrimSpace(k) == publicKey
		}
		if !accepted {
			fmt.Println("Values locked on this host can then also be opened with its private half. Only add it if you recognise it.")
			accepted = confirm("Add it to this host's recovery keys?", false)
		}
		if !accepted {
			fmt.Fprintf(os.Stderr, "%s the recovery key was not added, so values locked on this host can't be opened with it. To add it later, put it in %s and run `rest-o-matic secret relock`\n",
				color.Stderr.Warn("warning:"), recipientsPath)
			continue
		}
		if err := appendRecipient(recipientsPath, publicKey); err != nil {
			return added, err
		}
		listed[publicKey] = true
		added++
		fmt.Println("added it to", recipientsPath)
	}
	return added, nil
}

// listedRecipients returns the public keys in a recipients file.
func listedRecipients(path string) (map[string]bool, error) {
	listed := map[string]bool{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return listed, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			listed[line] = true
		}
	}
	return listed, nil
}

// appendRecipient adds a public key to the end of a recipients file.
func appendRecipient(path, publicKey string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	text := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		text = "\n"
	}
	text += fmt.Sprintf("# added at enrolment, %s\n%s\n", time.Now().Format("2006-01-02"), publicKey)
	_, err = f.WriteString(text)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// offerRelock follows the adding of a recovery key: values already locked
// in the config can't be opened with it, so it offers to lock them again,
// or says how.
func offerRelock(keyPath string, hostKey *secrets.HostKey) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	values, err := config.LockedValues(data)
	if err != nil || len(values) == 0 {
		return
	}
	command := "rest-o-matic"
	if abs, err := canonicalPath(configPath); err == nil {
		command += " --config " + abs
	}
	if keyFile != "" {
		command += " --key-file " + keyFile
	}
	command += " secret relock"

	fmt.Printf("The config holds %d locked value(s) that may have been locked before this recovery key was added, so it may not open them.\n", len(values))
	if !confirm("Re-lock them now for this host's key and the recovery keys?", true) {
		fmt.Println("To re-lock them, run:", command)
		return
	}
	recovery, err := secrets.LoadRecipients(secrets.RecipientsPath(keyPath))
	if err == nil {
		err = relockConfig(configPath, hostKey, hostKey, recovery, false)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s re-locking failed: %v\nTo try again, run: %s\n", color.Stderr.Warn("warning:"), err, command)
	}
}

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"filippo.io/age"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/secrets"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Lock config values so that only this host's key can read them",
	Long: `A repository's password, and any value under its env, can be written in the
config in a locked form:

    password: !locked "..."

A locked value is encrypted for this host's key, and for any recovery keys
you list. rest-o-matic unlocks it in memory when it starts restic and never
writes the plain text to disk, so the config file can be copied, shared and
stored elsewhere without exposing the credentials in it.

The host key lives outside the config, by default as host.key in your user
config directory (--key-file changes that). Beside it, a file named
recovery-recipients may list other public keys, one per line; every value
locked on this host can be opened with those too. Without one, losing the
host key means losing every value locked for it.

Locked values are ordinary age files (https://age-encryption.org), base64
encoded, and can be opened with the age tool and a matching key.`,
}

var secretKeygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Create this host's key",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := keyPath()
		if err != nil {
			return err
		}
		publicKey, err := secrets.Generate(path)
		if err != nil {
			return err
		}
		fmt.Println("public key:", publicKey)
		fmt.Println("key file:  ", path)
		warnIfNoRecoveryKey(path)
		return nil
	},
}

var secretPublicKeyCmd = &cobra.Command{
	Use:   "public-key",
	Short: "Print this host's public key",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := keyPath()
		if err != nil {
			return err
		}
		key, err := secrets.LoadHostKey(path)
		if err != nil {
			return hostKeyError(err)
		}
		fmt.Println(key.PublicKey())
		return nil
	},
}

var lockRecipients []string

var secretLockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Turn a value into its locked form, ready to paste into the config",
	Long: `lock reads a secret and prints its locked form, which goes in the config in
place of the plain value:

    password: !locked "..."

The value is read from standard input when that is a pipe or a file, and is
otherwise prompted for without being shown. It is never taken from the
command line, which would leave it in your shell history.

The locked value can be opened by this host's key, by every key in the
recovery-recipients file beside it, and by each --recipient. To lock a value
for another host, pass that host's public key with --recipient.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		recipients, err := lockTargets(lockRecipients)
		if err != nil {
			return err
		}
		value, err := readSecret(os.Stdin, os.Stderr)
		if err != nil {
			return err
		}
		locked, err := secrets.Lock(value, recipients)
		if err != nil {
			return err
		}
		fmt.Printf("%s %q\n", secrets.Tag, locked)
		return nil
	},
}

var secretRevealCmd = &cobra.Command{
	Use:   "reveal <repository> [ENV_NAME]",
	Short: "Print the plain text of a locked value from the config",
	Long: `reveal unlocks a repository's locked password with this host's key and prints
it, for the times you need to run restic by hand. Give the name of one of
the repository's env values to reveal that instead.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		repo, ok := cfg.Repositories[args[0]]
		if !ok {
			return fmt.Errorf("no such repository %q", args[0])
		}
		field, secret := "password", repo.Password
		if len(args) == 2 {
			field = "env " + args[1]
			if secret, ok = repo.Env[args[1]]; !ok {
				return fmt.Errorf("repository %q has no env value %q", args[0], args[1])
			}
		}
		if !secret.Locked {
			return fmt.Errorf("repository %q: %s is not locked", args[0], field)
		}
		value, err := secrets.NewUnlocker(keyPath()).Reveal(secret.Value)
		if err != nil {
			return fmt.Errorf("repository %q: %s: %w", args[0], field, hostKeyError(err))
		}
		fmt.Println(value)
		return nil
	},
}

var secretCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check that this host can unlock every locked value in the config",
	Long: `check tries to unlock each locked value in the config with this host's key
and lists any it can't, without printing a value. Run it after copying a
config to a host, or after replacing a host's key. (validate checks only
that locked values are well-formed, and never needs a key.)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		unlocker := secrets.NewUnlocker(keyPath())

		names := make([]string, 0, len(cfg.Repositories))
		for name := range cfg.Repositories {
			names = append(names, name)
		}
		sort.Strings(names)

		total, failed := 0, 0
		check := func(repo, field string, s config.Secret) {
			if !s.Locked {
				return
			}
			total++
			if _, err := unlocker.Reveal(s.Value); err != nil {
				failed++
				fmt.Printf("repository %s: %s: %s: %v\n", repo, field, color.Stdout.Error("cannot be unlocked"), err)
			}
		}
		for _, name := range names {
			repo := cfg.Repositories[name]
			check(name, "password", repo.Password)
			envNames := make([]string, 0, len(repo.Env))
			for envName := range repo.Env {
				envNames = append(envNames, envName)
			}
			sort.Strings(envNames)
			for _, envName := range envNames {
				check(name, "env "+envName, repo.Env[envName])
			}
		}

		switch {
		case total == 0:
			fmt.Println("the config has no locked values")
		case failed == 0:
			fmt.Println(color.Stdout.Success(fmt.Sprintf("all %d locked value(s) can be unlocked", total)))
		default:
			return fmt.Errorf("%d of %d locked value(s) cannot be unlocked", failed, total)
		}
		return nil
	},
}

func init() {
	secretLockCmd.Flags().StringArrayVar(&lockRecipients, "recipient", nil, "also lock for this public key (repeatable)")
	secretCmd.AddCommand(secretKeygenCmd, secretPublicKeyCmd, secretLockCmd, secretRevealCmd, secretCheckCmd)
}

// lockTargets is every key a value locked now can be opened by: this
// host's, if it has one; the recovery recipients beside it; and extra.
func lockTargets(extra []string) ([]age.Recipient, error) {
	path, err := keyPath()
	if err != nil {
		return nil, err
	}
	var recipients []age.Recipient
	key, err := secrets.LoadHostKey(path)
	switch {
	case err == nil:
		recipients = append(recipients, key.Recipient())
	case !errors.Is(err, secrets.ErrNoHostKey):
		return nil, err
	}
	recovery, err := secrets.LoadRecipients(secrets.RecipientsPath(path))
	if err != nil {
		return nil, err
	}
	recipients = append(recipients, recovery...)
	for _, publicKey := range extra {
		r, err := secrets.ParseRecipient(publicKey)
		if err != nil {
			return nil, err
		}
		recipients = append(recipients, r)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("there is no key to lock the value for: create this host's key with `rest-o-matic secret keygen`, or pass --recipient")
	}
	return recipients, nil
}

// readSecret reads the value to lock: all of in when it is a pipe or file
// (without one trailing newline), or a no-echo prompt, asked twice, when
// it is a terminal.
func readSecret(in *os.File, prompt io.Writer) (string, error) {
	if !term.IsTerminal(int(in.Fd())) {
		data, err := io.ReadAll(bufio.NewReader(in))
		if err != nil {
			return "", fmt.Errorf("reading the value: %w", err)
		}
		value := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if value == "" {
			return "", errors.New("no value given on standard input")
		}
		return value, nil
	}

	ask := func(label string) (string, error) {
		fmt.Fprint(prompt, label)
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(prompt)
		return string(b), err
	}
	first, err := ask("Value to lock: ")
	if err != nil {
		return "", fmt.Errorf("reading the value: %w", err)
	}
	if first == "" {
		return "", errors.New("no value given")
	}
	second, err := ask("Again: ")
	if err != nil {
		return "", fmt.Errorf("reading the value: %w", err)
	}
	if first != second {
		return "", errors.New("the two values did not match")
	}
	return first, nil
}

// hostKeyError adds how to get a host key to an error about not having one.
func hostKeyError(err error) error {
	if errors.Is(err, secrets.ErrNoHostKey) {
		return fmt.Errorf("%w; create one with `rest-o-matic secret keygen`", err)
	}
	return err
}

// warnIfNoRecoveryKey says, once a host key exists, what losing it would
// cost when nothing else can open the values locked for it.
func warnIfNoRecoveryKey(keyPath string) {
	path := secrets.RecipientsPath(keyPath)
	if recovery, err := secrets.LoadRecipients(path); err != nil || len(recovery) > 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "%s no recovery key is set up. Values locked for this host can only be opened with its key,\nso losing %s would lose them. To be able to recover, put a recovery public key in\n%s\n",
		color.Stderr.Warn("warning:"), keyPath, path)
}

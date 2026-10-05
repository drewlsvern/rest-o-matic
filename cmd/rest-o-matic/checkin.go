package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/central"
	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
	"github.com/drewlsvern/rest-o-matic/internal/state"
	"github.com/drewlsvern/rest-o-matic/internal/statedir"
)

// Time limits for one check-in: short when it is only a heartbeat, so an
// unresponsive central app can't hold up a tick's jobs for long, and
// longer when it carries parts that may take a while to upload.
var (
	heartbeatTimeout = 10 * time.Second
	contentTimeout   = 60 * time.Second
)

var checkinPrint bool

var checkinCmd = &cobra.Command{
	Use:   "checkin",
	Short: "Report to the central app now",
	Long: `checkin sends one check-in to the central app this host is enrolled with,
just as every tick does, and says which parts it included. A part is
included only when it has changed since the central app last received it,
or when the central app asked for it again.

With --print it sends nothing, and prints the check-in with every part
included instead. That works without being enrolled, and shows exactly
what the central app would receive from this host.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadAndValidate()
		if err != nil {
			return err
		}
		store := state.NewStore(statePath(), lockDir())

		if checkinPrint {
			st, err := store.Load()
			if err != nil {
				return err
			}
			var hostID *string
			if e, err := enrolmentForConfig(); err == nil {
				hostID = &e.HostID
			}
			// Nothing is written: restic's version isn't cached here.
			req, err := buildCheckin(cfg, st, store, time.Now(), hostInfo(nil, st), hostID, state.CheckinState{}, true)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(req)
		}

		e, err := enrolmentForConfig()
		if err != nil {
			return err
		}
		if err := statedir.Check(stateDir); err != nil {
			return err
		}
		res := checkIn(cfg, store, e)
		if res.err != nil {
			return checkinFailure(res.err, e)
		}
		sent := "nothing had changed"
		if len(res.included) > 0 {
			sent = "sent " + strings.Join(res.included, ", ")
		}
		fmt.Printf("%s with %s: %s\n", color.Stdout.Success("checked in"), e.URL, sent)
		if res.withheld != nil {
			fmt.Println(configWithheldLine(res.withheld.Fields))
		}
		return nil
	},
}

func init() {
	checkinCmd.Flags().BoolVar(&checkinPrint, "print", false, "print the full check-in instead of sending it")
}

// enrolmentForConfig returns this host's enrolment if it is for the config
// in use. It returns an error wrapping central.ErrNotEnrolled if there is
// no enrolment, or it is for another config file.
func enrolmentForConfig() (*central.Enrolment, error) {
	path, err := keyPath()
	if err != nil {
		return nil, err
	}
	e, err := central.LoadEnrolment(central.EnrolmentPath(path))
	if err != nil {
		if errors.Is(err, central.ErrNotEnrolled) {
			return nil, fmt.Errorf("%w; connect it with `rest-o-matic enrol`", err)
		}
		return nil, err
	}
	current, err := canonicalPath(configPath)
	if err != nil {
		return nil, err
	}
	if current != e.ConfigPath {
		return nil, fmt.Errorf("%w for this config: it is enrolled for %s", central.ErrNotEnrolled, e.ConfigPath)
	}
	return e, nil
}

// canonicalPath is p as an absolute path with any links resolved, so the
// same config file always gives the same path.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// checkinResult is what became of one check-in.
type checkinResult struct {
	included   []string
	withheld   *central.Withheld
	wasFailing bool
	err        error
}

// checkIn sends one check-in for the enrolment e and records the outcome.
func checkIn(cfg *config.Config, store *state.Store, e *central.Enrolment) checkinResult {
	now := time.Now()
	result := func(err error) checkinResult {
		wasFailing, recordErr := store.RecordCheckin(e.HostID, now, nil, nil, err)
		if recordErr != nil {
			fmt.Fprintf(os.Stderr, "%s check-in could not be recorded: %v\n", color.Stderr.Warn("warning:"), recordErr)
		}
		return checkinResult{wasFailing: wasFailing, err: err}
	}

	st, err := store.Load()
	if err != nil {
		return result(err)
	}
	req, err := buildCheckin(cfg, st, store, now, hostInfo(store, st), &e.HostID, st.CheckinFor(e.HostID), false)
	if err != nil {
		return result(err)
	}
	client, err := central.NewClient(e.Settings(readBuildInfo().Version))
	if err != nil {
		return result(err)
	}
	limit := heartbeatTimeout
	included := req.Included()
	if len(included) > 0 {
		limit = contentTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	reply, err := client.Checkin(ctx, req)
	if err != nil {
		return result(err)
	}

	sent := map[string]string{}
	fingerprints := req.Fingerprints()
	for _, part := range included {
		sent[part] = fingerprints[part]
	}
	wasFailing, err := store.RecordCheckin(e.HostID, now, sent, reply.Resend, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s check-in could not be recorded: %v\n", color.Stderr.Warn("warning:"), err)
	}
	return checkinResult{included: included, withheld: req.Parts.Config.Withheld, wasFailing: wasFailing}
}

// checkInFromTick checks in if the host is enrolled for this config, and
// prints only when check-ins start failing or start working again. It
// never affects the tick's outcome.
func checkInFromTick(cfg *config.Config, store *state.Store) {
	e, err := enrolmentForConfig()
	switch {
	case errors.Is(err, central.ErrNotEnrolled):
		return
	case err != nil:
		fmt.Fprintf(os.Stderr, "%s cannot check in with the central app: %v\n", color.Stderr.Warn("warning:"), err)
		return
	}
	res := checkIn(cfg, store, e)
	switch {
	case res.err != nil && !res.wasFailing:
		fmt.Fprintf(os.Stderr, "%s %v (nothing more is printed until it works again; see `rest-o-matic status`)\n", color.Stderr.Warn("warning:"), checkinFailure(res.err, e))
	case res.err == nil && res.wasFailing:
		fmt.Printf("check-ins with %s are %s again\n", e.URL, color.Stdout.Success("working"))
	}
}

// checkinFailure explains a failed check-in, adding what to do about it
// where something can be done.
func checkinFailure(err error, e *central.Enrolment) error {
	if errors.Is(err, central.ErrCredentialRejected) {
		return fmt.Errorf("check-in with %s failed: %w; this host must be enrolled again (rest-o-matic enrol %s --force)", e.URL, err, e.URL)
	}
	return fmt.Errorf("check-in with %s failed: %w", e.URL, err)
}

// configWithheldLine explains why the config isn't sent.
func configWithheldLine(fields []string) string {
	verb := "are"
	if len(fields) == 1 {
		verb = "is"
	}
	return fmt.Sprintf("config not backed up to the central app: %s %s in plain text (lock with `rest-o-matic secret lock`, or mark a value that isn't secret !plain)",
		strings.Join(fields, ", "), verb)
}

// statusPart is the content of the status part.
type statusPart struct {
	Jobs []jobStatus `json:"jobs"`
}

// snapshotsPart is the content of the snapshots part: each job's recorded
// lists, by job and then repository.
type snapshotsPart struct {
	Jobs map[string]map[string]snapshotsPartList `json:"jobs"`
}

type snapshotsPartList struct {
	ListedAt  time.Time        `json:"listed_at"`
	Snapshots []snapshotStatus `json:"snapshots"`
}

// buildCheckin builds a check-in from the config and state. A part's
// content is included when all is set, when its fingerprint differs from
// the one acknowledged in c, or when c asks for it again.
func buildCheckin(cfg *config.Config, st *state.State, store *state.Store, now time.Time, host central.Host, hostID *string, c state.CheckinState, all bool) (central.CheckinRequest, error) {
	held := func(job string) (bool, error) { return lock.JobHeld(lockDir(), job) }
	names := jobNames(cfg)
	jobs, err := buildJobStatuses(cfg, st, now, names, jobDetail{runs: true}, held, store.LoadSnapshots)
	if err != nil {
		return central.CheckinRequest{}, err
	}
	statusJSON, err := json.Marshal(statusPart{Jobs: jobs})
	if err != nil {
		return central.CheckinRequest{}, err
	}

	snaps := snapshotsPart{Jobs: map[string]map[string]snapshotsPartList{}}
	for _, job := range jobs {
		lists, err := store.LoadSnapshots(job.Name)
		if err != nil {
			return central.CheckinRequest{}, fmt.Errorf("job %q: %w", job.Name, err)
		}
		repos := map[string]snapshotsPartList{}
		for _, list := range snapshotListStatuses(job.Repositories, lists, true) {
			if list.ListedAt != nil {
				repos[list.Repository] = snapshotsPartList{ListedAt: *list.ListedAt, Snapshots: list.Snapshots}
			}
		}
		if len(repos) > 0 {
			snaps.Jobs[job.Name] = repos
		}
	}
	snapshotsJSON, err := json.Marshal(snaps)
	if err != nil {
		return central.CheckinRequest{}, err
	}

	configText, err := os.ReadFile(configPath)
	if err != nil {
		return central.CheckinRequest{}, fmt.Errorf("reading config: %w", err)
	}

	wanted := func(part, fingerprint string) bool {
		if all || c.Acknowledged[part] != fingerprint {
			return true
		}
		for _, p := range c.Resend {
			if p == part {
				return true
			}
		}
		return false
	}

	req := central.CheckinRequest{
		FormatVersion: central.FormatVersion,
		HostID:        hostID,
		SentAt:        utc(now),
		Host:          host,
	}
	req.Parts.Status.Fingerprint = central.Fingerprint(statusJSON)
	if wanted(central.PartStatus, req.Parts.Status.Fingerprint) {
		req.Parts.Status.Content = statusJSON
	}
	req.Parts.Snapshots.Fingerprint = central.Fingerprint(snapshotsJSON)
	if wanted(central.PartSnapshots, req.Parts.Snapshots.Fingerprint) {
		req.Parts.Snapshots.Content = snapshotsJSON
	}
	req.Parts.Config.Fingerprint = central.Fingerprint(configText)
	if fields := config.PlainTextSecrets(cfg); len(fields) > 0 {
		req.Parts.Config.Withheld = &central.Withheld{Reason: central.WithheldPlainTextSecrets, Fields: fields}
	} else if wanted(central.PartConfig, req.Parts.Config.Fingerprint) {
		text := string(configText)
		req.Parts.Config.Content = &text
	}
	return req, nil
}

// hostInfo describes this host. restic's version is taken from the cache
// in st while restic's binary is unchanged, and cached in store unless it
// is nil.
func hostInfo(store *state.Store, st *state.State) central.Host {
	h := central.Host{
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		RestOMaticVersion: readBuildInfo().Version,
	}
	h.Hostname, _ = os.Hostname()
	if v := resticVersion(store, st); v != "" {
		h.ResticVersion = &v
	}
	return h
}

// resticVersion returns restic's version, such as "0.17.3", or "" if
// restic can't be found or run. Running restic is avoided while the binary
// matches the one the cached version was read from.
func resticVersion(store *state.Store, st *state.State) string {
	path, err := exec.LookPath("restic")
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if c := st.Restic; c != nil && c.Path == path && c.Size == info.Size() && c.ModTime.Equal(info.ModTime()) {
		return c.Version
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return ""
	}
	// "restic 0.17.3 compiled with go1.23.1 on linux/amd64"
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[0] != "restic" {
		return ""
	}
	version := fields[1]
	if store != nil {
		// Only a cache: if it can't be saved, restic is asked again next time.
		_ = store.RecordResticVersion(state.ResticVersion{Path: path, Size: info.Size(), ModTime: info.ModTime(), Version: version})
	}
	return version
}

// checkinStatusFor reports on the link to the central app for status.
func checkinStatusFor(cfg *config.Config, st *state.State) *checkinStatus {
	e, err := enrolmentForConfig()
	if err != nil {
		note := "not connected"
		if !errors.Is(err, central.ErrNotEnrolled) || strings.Contains(err.Error(), "enrolled for") {
			note = "not connected: " + err.Error()
		}
		return &checkinStatus{note: note}
	}
	c := st.CheckinFor(e.HostID)
	cs := &checkinStatus{
		Enrolled:    true,
		URL:         &e.URL,
		HostID:      &e.HostID,
		HostName:    &e.HostName,
		LastAttempt: utcPtr(c.LastAttempt),
		LastSuccess: utcPtr(c.LastSuccess),
		LastError:   strPtr(c.LastError),
	}
	if fields := config.PlainTextSecrets(cfg); len(fields) > 0 {
		cs.ConfigWithheld = fields
	}
	return cs
}

// writeCheckin prints the central app lines of the status overview.
func writeCheckin(w io.Writer, c checkinStatus, now time.Time, p color.Painter) {
	if !c.Enrolled {
		fmt.Fprintln(w, "central app:", c.note)
		return
	}
	line := "central app: " + *c.URL + ", "
	switch {
	case c.LastSuccess != nil:
		line += "last check-in " + ago(now.Sub(*c.LastSuccess))
	case c.LastAttempt != nil:
		line += "never checked in"
	default:
		line += "no check-in yet"
	}
	fmt.Fprintln(w, line)
	if c.LastError != nil {
		note := *c.LastError
		if strings.Contains(note, central.ErrCredentialRejected.Error()) {
			note += "; this host must be enrolled again"
		}
		fmt.Fprintf(w, "  %s %s (last attempt %s)\n", p.Error("failing:"), clip(note), ago(now.Sub(*c.LastAttempt)))
	}
	if c.ConfigWithheld != nil {
		fmt.Fprintln(w, configWithheldLine(c.ConfigWithheld))
	}
}

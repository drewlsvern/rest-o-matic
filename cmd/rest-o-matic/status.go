package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
	"github.com/drewlsvern/rest-o-matic/internal/schedule"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:   "status [job]",
	Short: "Show each job's last run, its outcome, and when it is next due",
	Long: `status reports what rest-o-matic has recorded about every job in the
config: its schedule, when it last ran and how that went, how long it took,
when it is next due, and whether it is running or waiting right now. A
failed job shows its error and the result for each repository. The last tick
time shows whether your scheduler is still firing.

Give a job name to see that job's recent runs.

status only reads: it never runs restic or a hook, and never writes to the
state directory. It exits 0 whenever it could report, whatever the health
of the jobs; use --json to act on the details from a script.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadAndValidate()
		if err != nil {
			return err
		}
		only := ""
		if len(args) == 1 {
			only = args[0]
			if _, ok := cfg.Backups[only]; !ok {
				return fmt.Errorf("no such job %q", only)
			}
		}

		store := state.NewStore(statePath(), lockDir())
		st, err := store.Load()
		if err != nil {
			return err
		}
		held := func(job string) (bool, error) { return lock.JobHeld(lockDir(), job) }
		report, err := buildStatus(cfg, st, time.Now(), only, held, store.LoadSnapshots)
		if err != nil {
			return err
		}

		if statusJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(report)
		}
		if only != "" {
			writeJobHistory(os.Stdout, report, color.Stdout, time.Local)
		} else {
			writeStatus(os.Stdout, report, color.Stdout, time.Local)
		}
		return nil
	},
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "print one JSON document instead of the table")
}

// statusFormatVersion identifies the shape of the JSON document. It changes
// only for an incompatible change; adding keys does not change it.
const statusFormatVersion = 1

// statusReport is everything status knows, and is what --json prints. The
// text output is rendered from the same value, so the two can't disagree.
// Absent values are null rather than omitted, so consumers can rely on the
// keys; all times are UTC.
type statusReport struct {
	FormatVersion int         `json:"format_version"`
	GeneratedAt   time.Time   `json:"generated_at"`
	LastTick      *time.Time  `json:"last_tick"`
	Jobs          []jobStatus `json:"jobs"`
}

type jobStatus struct {
	Name         string   `json:"name"`
	Schedule     string   `json:"schedule"`
	Repositories []string `json:"repositories"`
	// Due is whether tick would start the job now. NextDue is the time from
	// which that is so, null for a job that has never run.
	Due     bool       `json:"due"`
	NextDue *time.Time `json:"next_due"`
	// Waiting means an execution is queued behind a busy repository or
	// slot; Running means it has begun. At most one is set.
	Waiting bool           `json:"waiting"`
	Running *runningStatus `json:"running"`
	LastRun *runStatus     `json:"last_run"`
	// FailingSince is when the first of the job's current run of failures
	// started; null unless its most recent run failed.
	FailingSince *time.Time `json:"failing_since"`
	// Runs is the job's history, newest first; present only when a single
	// job was asked for.
	Runs []runStatus `json:"runs,omitzero"`
	// SnapshotLists has one entry for each repository the job backs up
	// to, in config order.
	SnapshotLists []snapshotListStatus `json:"snapshot_lists"`
}

// snapshotListStatus is what is recorded about a job's snapshots in one
// repository. The list is taken each time the job runs, not live;
// ListedAt, and Newest, are null when none has been recorded yet.
type snapshotListStatus struct {
	Repository string     `json:"repository"`
	ListedAt   *time.Time `json:"listed_at"`
	Count      int        `json:"count"`
	Newest     *time.Time `json:"newest"`
	// Snapshots are the snapshots themselves, newest first; present only
	// when a single job was asked for.
	Snapshots []snapshotStatus `json:"snapshots,omitzero"`
}

type snapshotStatus struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	Paths    []string  `json:"paths"`
	Tags     []string  `json:"tags"`
	// Size and Files are null when restic didn't report them, as are the
	// figures for what the snapshot changed: files that were new or had
	// changed, and bytes added to the repository before and after
	// compression.
	Size            *int64 `json:"size"`
	Files           *int64 `json:"files"`
	FilesNew        *int64 `json:"files_new"`
	FilesChanged    *int64 `json:"files_changed"`
	DataAdded       *int64 `json:"data_added"`
	DataAddedPacked *int64 `json:"data_added_packed"`
}

type runningStatus struct {
	Started time.Time `json:"started"`
	Trigger string    `json:"trigger"`
}

// runStatus is one finished run. Started and Trigger are null for a record
// written by a version that kept only the finish time and outcome.
type runStatus struct {
	Started      *time.Time   `json:"started"`
	Finished     time.Time    `json:"finished"`
	Outcome      string       `json:"outcome"`
	Trigger      *string      `json:"trigger"`
	Error        *string      `json:"error"`
	Repositories []repoStatus `json:"repositories"`
}

type repoStatus struct {
	Name       string  `json:"name"`
	Result     string  `json:"result"`
	Error      *string `json:"error"`
	SnapshotID *string `json:"snapshot_id"`
}

// buildStatus assembles the report for every configured job, or for only
// when it is non-empty. held reports whether a job's lock is currently
// held, which is what makes a recorded running marker believable, and
// snapshots returns a job's recorded snapshot lists by repository.
func buildStatus(cfg *config.Config, st *state.State, now time.Time, only string, held func(job string) (bool, error), snapshots func(job string) (map[string]state.SnapshotList, error)) (statusReport, error) {
	report := statusReport{
		FormatVersion: statusFormatVersion,
		GeneratedAt:   utc(now),
		LastTick:      utcPtr(st.LastTick),
		Jobs:          []jobStatus{},
	}

	var names []string
	if only != "" {
		names = []string{only}
	} else {
		for name := range cfg.Backups {
			names = append(names, name)
		}
		sort.Strings(names)
	}

	for _, name := range names {
		js := st.Jobs[name]
		job := jobStatus{Name: name, Repositories: []string{}}
		for _, ref := range cfg.Backups[name].Repositories {
			job.Repositories = append(job.Repositories, ref.Name)
		}

		sched, err := cfg.EffectiveSchedule(name)
		if err != nil {
			return statusReport{}, err
		}
		job.Schedule = sched
		if job.Due, err = schedule.Due(sched, js.LastRun, now); err != nil {
			return statusReport{}, fmt.Errorf("job %q: %w", name, err)
		}
		if !js.LastRun.IsZero() {
			next, err := schedule.Next(sched, js.LastRun)
			if err != nil {
				return statusReport{}, fmt.Errorf("job %q: %w", name, err)
			}
			job.NextDue = utcPtr(&next)
		}

		isHeld, err := held(name)
		if err != nil {
			return statusReport{}, fmt.Errorf("job %q: %w", name, err)
		}
		switch {
		case isHeld && js.Running != nil:
			job.Running = &runningStatus{Started: utc(js.Running.Started), Trigger: js.Running.Trigger}
		case isHeld:
			job.Waiting = true
		}

		runs := runStatuses(js)
		if len(runs) > 0 {
			job.LastRun = &runs[0]
		}
		if js.LastOutcome == state.OutcomeFailed {
			// A state from before this was tracked only knows the last run.
			job.FailingSince = utcPtr(js.FailingSince)
			if job.FailingSince == nil {
				job.FailingSince = utcPtr(&js.LastRun)
			}
		}
		if only != "" {
			job.Runs = runs
		}

		lists, err := snapshots(name)
		if err != nil {
			return statusReport{}, fmt.Errorf("job %q: %w", name, err)
		}
		job.SnapshotLists = snapshotListStatuses(job.Repositories, lists, only != "")
		report.Jobs = append(report.Jobs, job)
	}
	return report, nil
}

// runStatuses returns a job's recorded runs, newest first, never nil. A
// state file from an earlier version has no run records, only the finish
// time and outcome of the last run, which becomes a single sparse entry.
func runStatuses(js state.JobState) []runStatus {
	runs := []runStatus{}
	for _, r := range js.Runs {
		started, trigger := utc(r.Started), r.Trigger
		run := runStatus{
			Started:      &started,
			Finished:     utc(r.Finished),
			Outcome:      r.Outcome,
			Trigger:      &trigger,
			Error:        strPtr(r.Error),
			Repositories: []repoStatus{},
		}
		if r.Started.IsZero() {
			run.Started = nil // unknown, not the year 1
		}
		for _, repo := range r.Repositories {
			run.Repositories = append(run.Repositories, repoStatus{
				Name:       repo.Name,
				Result:     repo.Result,
				Error:      strPtr(repo.Error),
				SnapshotID: strPtr(repo.SnapshotID),
			})
		}
		runs = append(runs, run)
	}
	if len(runs) == 0 && !js.LastRun.IsZero() {
		runs = append(runs, runStatus{
			Finished:     utc(js.LastRun),
			Outcome:      js.LastOutcome,
			Repositories: []repoStatus{},
		})
	}
	return runs
}

// snapshotListStatuses reports on each of a job's repositories in turn. A
// repository with no recorded list gets an entry of nulls, so a consumer
// can tell "none recorded" from "recorded as empty". full includes the
// snapshots themselves.
func snapshotListStatuses(repos []string, lists map[string]state.SnapshotList, full bool) []snapshotListStatus {
	out := []snapshotListStatus{}
	for _, repo := range repos {
		entry := snapshotListStatus{Repository: repo}
		if full {
			entry.Snapshots = []snapshotStatus{}
		}
		list, ok := lists[repo]
		if !ok {
			out = append(out, entry)
			continue
		}
		listedAt := utc(list.ListedAt)
		entry.ListedAt, entry.Count = &listedAt, len(list.Snapshots)
		for _, s := range list.Snapshots {
			t := utc(s.Time)
			if entry.Newest == nil || t.After(*entry.Newest) {
				entry.Newest = &t
			}
			if full {
				entry.Snapshots = append(entry.Snapshots, snapshotStatus{
					ID: s.ID, ShortID: s.ShortID, Time: t, Hostname: s.Hostname,
					Paths: orEmpty(s.Paths), Tags: orEmpty(s.Tags), Size: s.Size, Files: s.Files,
					FilesNew: s.FilesNew, FilesChanged: s.FilesChanged, DataAdded: s.DataAdded, DataAddedPacked: s.DataAddedPacked,
				})
			}
		}
		out = append(out, entry)
	}
	return out
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func utc(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := utc(*t)
	return &u
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// writeStatus prints the one-line-per-job table. Times are shown relative
// to the report's own time, and absolute ones in loc.
func writeStatus(w io.Writer, r statusReport, p color.Painter, loc *time.Location) {
	now := r.GeneratedAt
	if r.LastTick == nil {
		fmt.Fprintln(w, "last tick: never")
	} else {
		fmt.Fprintln(w, "last tick:", ago(now.Sub(*r.LastTick)))
	}
	fmt.Fprintln(w)

	t := table{header: []string{"JOB", "SCHEDULE", "LAST RUN", "OUTCOME", "TOOK", "NEXT DUE"}}
	for _, job := range r.Jobs {
		lastRun, took := "never", "-"
		outcome := cell{text: "-"}
		if run := job.LastRun; run != nil {
			lastRun, took, outcome = ago(now.Sub(run.Finished)), duration(*run), outcomeCell(run.Outcome, p)
		}
		var notes []string
		if note := activity(job, now); note != "" {
			notes = append(notes, note)
		}
		if run := job.LastRun; run != nil && run.Outcome != state.OutcomeSuccess {
			// Only worth a line once it has failed more than once in a row.
			if since := job.FailingSince; since != nil && run.Started != nil && since.Before(*run.Started) {
				notes = append(notes, "failing since "+ago(now.Sub(*since)))
			}
			notes = append(notes, failureNotes(*run)...)
		}
		t.add(notes, cell{text: job.Name}, cell{text: job.Schedule}, cell{text: lastRun}, outcome, cell{text: took}, cell{text: nextDue(job, now, loc)})
	}
	t.write(w)
}

// writeJobHistory prints one job's recorded runs, newest first.
func writeJobHistory(w io.Writer, r statusReport, p color.Painter, loc *time.Location) {
	job, now := r.Jobs[0], r.GeneratedAt
	fmt.Fprintf(w, "job %s: %s, repositories %s\n", job.Name, job.Schedule, strings.Join(job.Repositories, ", "))
	fmt.Fprintln(w, "next due:", nextDue(job, now, loc))
	if note := activity(job, now); note != "" {
		fmt.Fprintln(w, note)
	}
	fmt.Fprintln(w)

	if len(job.Runs) == 0 {
		fmt.Fprintln(w, "no recorded runs")
	}
	t := table{header: []string{"STARTED", "TOOK", "OUTCOME", "BY"}}
	for _, run := range job.Runs {
		// A record from an earlier version has only its finish time.
		started, by := run.Finished.In(loc).Format("2006-01-02 15:04")+" (finished)", "-"
		if run.Started != nil {
			started = run.Started.In(loc).Format("2006-01-02 15:04")
		}
		if run.Trigger != nil {
			by = *run.Trigger
		}
		var notes []string
		if run.Outcome == state.OutcomeSuccess {
			notes = repoNotes(run)
		} else {
			notes = failureNotes(run)
		}
		t.add(notes, cell{text: started}, cell{text: duration(run)}, outcomeCell(run.Outcome, p), cell{text: by})
	}
	if len(job.Runs) > 0 {
		t.write(w)
	}

	for _, list := range job.SnapshotLists {
		fmt.Fprintln(w)
		writeSnapshotList(w, list, now, loc)
	}
}

// writeSnapshotList prints the snapshots recorded for one repository. The
// list is as old as the job's last successful run there, so its age is
// always shown.
func writeSnapshotList(w io.Writer, list snapshotListStatus, now time.Time, loc *time.Location) {
	if list.ListedAt == nil {
		fmt.Fprintf(w, "Snapshots in %s: not listed yet (a list is taken each time the job runs)\n", list.Repository)
		return
	}
	fmt.Fprintf(w, "Snapshots in %s, as of %s: %d\n", list.Repository, ago(now.Sub(*list.ListedAt)), list.Count)
	if len(list.Snapshots) == 0 {
		return
	}
	t := table{header: []string{"ID", "TIME", "SIZE", "CHANGED"}}
	for _, s := range list.Snapshots {
		size := "-"
		if s.Size != nil {
			size = humanBytes(*s.Size)
		}
		t.add(nil, cell{text: shortID(s.ID)}, cell{text: s.Time.In(loc).Format("2006-01-02 15:04")}, cell{text: size}, cell{text: changed(s)})
	}
	t.write(w)
}

// changed says what a snapshot changed compared with the one before it,
// or "-" when restic didn't report it. The size is what it added to the
// repository: after compression where restic says, otherwise before.
func changed(s snapshotStatus) string {
	if s.FilesNew == nil || s.FilesChanged == nil {
		return "-"
	}
	text := fmt.Sprintf("%d new, %d changed", *s.FilesNew, *s.FilesChanged)
	switch {
	case s.DataAddedPacked != nil:
		text += ", +" + humanBytes(*s.DataAddedPacked)
	case s.DataAdded != nil:
		text += ", +" + humanBytes(*s.DataAdded)
	}
	return text
}

// humanBytes renders a byte count in binary units, as restic does.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), ""
	for _, s := range []string{"KiB", "MiB", "GiB", "TiB", "PiB"} {
		value, suffix = value/unit, s
		if value < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}

func outcomeCell(outcome string, p color.Painter) cell {
	if outcome == state.OutcomeSuccess {
		return cell{text: "ok", paint: p.Success}
	}
	return cell{text: "FAILED", paint: p.Error}
}

// activity describes what the job is doing right now, or "" if nothing.
func activity(job jobStatus, now time.Time) string {
	switch {
	case job.Running != nil:
		return fmt.Sprintf("running since %s (started by %s)", ago(now.Sub(job.Running.Started)), job.Running.Trigger)
	case job.Waiting:
		return "waiting for a repository or a free slot"
	}
	return ""
}

// failureNotes explains a failed run: the result for each repository it
// attempted, or the run's own error when no repository is to blame (a
// hook failed, or the run was interrupted).
func failureNotes(run runStatus) []string {
	notes := repoNotes(run)
	repoFailed := false
	for _, repo := range run.Repositories {
		repoFailed = repoFailed || repo.Result != state.ResultOK
	}
	if !repoFailed && run.Error != nil {
		notes = append(notes, *run.Error)
	}
	return notes
}

// repoNotes is one line per repository a run attempted.
func repoNotes(run runStatus) []string {
	var notes []string
	for _, repo := range run.Repositories {
		note := repo.Name + ": "
		switch repo.Result {
		case state.ResultOK:
			note += "ok"
			if repo.SnapshotID != nil {
				note += " (snapshot " + shortID(*repo.SnapshotID) + ")"
			}
		case state.ResultBackupFailed:
			note += "backup failed"
		case state.ResultForgetFailed:
			note += "backup ok, retention failed"
		default:
			note += repo.Result
		}
		if repo.Error != nil {
			note += ": " + *repo.Error
		}
		notes = append(notes, note)
	}
	return notes
}

// shortID abbreviates a snapshot ID the way restic's own listings do. The
// full ID is in the JSON output.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// maxNoteLen keeps a note to a couple of terminal lines; restic's errors
// can run to several hundred characters. The full text is in the JSON
// output.
const maxNoteLen = 200

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxNoteLen {
		return s
	}
	return string([]rune(s)[:maxNoteLen]) + "…"
}

// nextDue says when tick will next start the job.
func nextDue(job jobStatus, now time.Time, loc *time.Location) string {
	switch {
	case job.NextDue == nil:
		return "due now"
	case job.Due:
		since, layout := job.NextDue.In(loc), "15:04"
		if y, m, d := since.Date(); !sameDate(y, m, d, now.In(loc)) {
			layout = "Jan 2 15:04"
		}
		return "due since " + since.Format(layout)
	}
	return until(job.NextDue.Sub(now))
}

func sameDate(y int, m time.Month, d int, t time.Time) bool {
	ty, tm, td := t.Date()
	return y == ty && m == tm && d == td
}

// duration is how long a run took, or "-" when its start wasn't recorded.
func duration(run runStatus) string {
	if run.Started == nil {
		return "-"
	}
	d := run.Finished.Sub(*run.Started).Round(time.Second)
	if d < time.Second {
		return "<1s"
	}
	return d.String()
}

func ago(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	return span(d) + " ago"
}

func until(d time.Duration) string {
	if d < time.Minute {
		return "in under a minute"
	}
	return "in " + span(d)
}

// span renders a duration of at least a minute in its largest sensible
// unit: minutes up to an hour, hours up to two days, then days.
func span(d time.Duration) string {
	switch {
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 48*time.Hour:
		return plural(int(d/time.Hour), "hour")
	}
	return plural(int(d/(24*time.Hour)), "day")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// table lays out left-aligned columns. Widths come from the plain text, so
// a coloured cell (whose escape codes take no room on screen) still lines
// up. Each row may carry notes, printed indented beneath it.
type table struct {
	header []string
	rows   []tableRow
}

type cell struct {
	text  string
	paint func(string) string
}

type tableRow struct {
	cells []cell
	notes []string
}

func (t *table) add(notes []string, cells ...cell) {
	t.rows = append(t.rows, tableRow{cells: cells, notes: notes})
}

func (t *table) write(w io.Writer) {
	widths := make([]int, len(t.header))
	for i, h := range t.header {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range t.rows {
		for i, c := range row.cells {
			widths[i] = max(widths[i], utf8.RuneCountInString(c.text))
		}
	}

	line := func(cells []cell) {
		var b strings.Builder
		for i, c := range cells {
			text := c.text
			if c.paint != nil {
				text = c.paint(text)
			}
			b.WriteString(text)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c.text)+2))
			}
		}
		fmt.Fprintln(w, b.String())
	}

	header := make([]cell, len(t.header))
	for i, h := range t.header {
		header[i] = cell{text: h}
	}
	line(header)
	for _, row := range t.rows {
		line(row.cells)
		for _, note := range row.notes {
			fmt.Fprintln(w, "  "+clip(note))
		}
	}
}

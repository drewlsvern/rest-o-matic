package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/central"
	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// progressOf returns a progress loader that knows only the given records.
func progressOf(records map[string]*state.Progress) func(string) *state.Progress {
	return func(job string) *state.Progress { return records[job] }
}

func statusWithProgress(t *testing.T, now time.Time, only string, held func(string) (bool, error), records map[string]*state.Progress) statusReport {
	t.Helper()
	marker := &state.Running{Started: now.Add(-70 * time.Minute), Trigger: state.TriggerRun}
	st := &state.State{Jobs: map[string]state.JobState{"gitea": {Running: marker}, "documents": {Running: marker}}}
	r, err := buildStatus(statusConfig(), st, now, only, held, noSnapshots, progressOf(records))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func backupProgress(now time.Time, moved time.Duration) *state.Progress {
	left := int64(5400)
	report, lastMoved := now.Add(-2*time.Second), now.Add(-moved)
	return &state.Progress{Phase: "backup", Repository: "nas", RepositoryIndex: 1, Repositories: 2,
		PercentDone: 0.43, BytesDone: 3_650_000_000, TotalBytes: 8_500_000_000, FilesDone: 5, TotalFiles: 10,
		SecondsRemaining: &left, CurrentFiles: []string{"/data/big.mdf"},
		Updated: report, LastReport: &report, LastMoved: &lastMoved}
}

func TestStatus_ShowsBackupProgress(t *testing.T) {
	now := ts(t, "2026-10-21T10:00:00Z")
	r := statusWithProgress(t, now, "", heldJobs("gitea"), map[string]*state.Progress{"gitea": backupProgress(now, 3*time.Second)})
	out := render(r)
	want := "running since 1 hour ago (started by run): backing up to nas (1 of 2), 43%, 3.4 GiB of 7.9 GiB, about 1 hour left"
	if !strings.Contains(out, want) {
		t.Fatalf("output lacks %q:\n%s", want, out)
	}
}

func TestStatus_ShowsPause(t *testing.T) {
	now := ts(t, "2026-10-21T10:00:00Z")
	r := statusWithProgress(t, now, "", heldJobs("gitea"), map[string]*state.Progress{"gitea": backupProgress(now, 12*time.Minute)})
	out := render(r)
	if !strings.Contains(out, "43%, 3.4 GiB of 7.9 GiB, no progress reported for 12 minutes") || strings.Contains(out, "left") {
		t.Fatalf("got:\n%s", out)
	}
	// Under a minute without movement is not shown as a pause.
	r = statusWithProgress(t, now, "", heldJobs("gitea"), map[string]*state.Progress{"gitea": backupProgress(now, 50*time.Second)})
	if out := render(r); strings.Contains(out, "no progress") {
		t.Fatalf("a 50s pause was shown:\n%s", out)
	}
}

func TestStatus_ShowsPhaseOutsideBackup(t *testing.T) {
	now := ts(t, "2026-10-21T10:00:00Z")
	for phase, want := range map[string]string{
		"before_hooks": "(started by run): before hooks",
		"after_hooks":  "(started by run): after hooks",
	} {
		r := statusWithProgress(t, now, "", heldJobs("gitea"), map[string]*state.Progress{"gitea": {Phase: phase, Updated: now}})
		if out := render(r); !strings.Contains(out, want) {
			t.Errorf("%s: output lacks %q:\n%s", phase, want, out)
		}
	}
	forget := &state.Progress{Phase: "forget", Repository: "offsite", RepositoryIndex: 2, Repositories: 2, Updated: now}
	r := statusWithProgress(t, now, "", heldJobs("gitea"), map[string]*state.Progress{"gitea": forget})
	if out := render(r); !strings.Contains(out, "applying retention on offsite (2 of 2)") {
		t.Errorf("forget: got\n%s", out)
	}
	// A backup with no report from restic yet shows only where it is.
	starting := &state.Progress{Phase: "backup", Repository: "nas", RepositoryIndex: 1, Repositories: 2, Updated: now}
	r = statusWithProgress(t, now, "", heldJobs("gitea"), map[string]*state.Progress{"gitea": starting})
	if out := render(r); !strings.Contains(out, "(started by run): backing up to nas (1 of 2)\n") {
		t.Errorf("starting backup: got\n%s", out)
	}
}

func TestStatus_LeftoverProgressIgnored(t *testing.T) {
	now := ts(t, "2026-10-21T10:00:00Z")
	// documents' record is left by a killed process: its lock is free.
	r := statusWithProgress(t, now, "", heldJobs("gitea"), map[string]*state.Progress{"documents": backupProgress(now, 0)})
	if docs := jobNamed(t, r, "documents"); docs.Running != nil {
		t.Fatalf("documents shown running: %+v", docs.Running)
	}
	if out := render(r); strings.Contains(out, "43%") {
		t.Fatalf("leftover progress shown:\n%s", out)
	}
	// A running job with no record yet is shown as before.
	if gitea := jobNamed(t, r, "gitea"); gitea.Running == nil || gitea.Running.Progress != nil {
		t.Fatalf("gitea: %+v", gitea.Running)
	}
}

func TestStatus_JobDetailAndJSONProgress(t *testing.T) {
	now := ts(t, "2026-10-21T10:00:00Z")
	r := statusWithProgress(t, now, "gitea", heldJobs("gitea"), map[string]*state.Progress{"gitea": backupProgress(now, 4*time.Minute)})

	var b bytes.Buffer
	writeJobHistory(&b, r, color.NewPainter(false), time.UTC)
	for _, want := range []string{"  reading /data/big.mdf", "  restic last reported just now", "  figures last moved 4 minutes ago"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("detail lacks %q:\n%s", want, b.String())
		}
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Jobs []struct {
			Running *struct {
				Progress *struct {
					Phase           string   `json:"phase"`
					Repository      string   `json:"repository"`
					RepositoryIndex int      `json:"repository_index"`
					BytesDone       int64    `json:"bytes_done"`
					CurrentFiles    []string `json:"current_files"`
					LastReport      string   `json:"last_report"`
					LastMoved       string   `json:"last_moved"`
				} `json:"progress"`
			} `json:"running"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	p := doc.Jobs[0].Running.Progress
	if p == nil || p.Phase != "backup" || p.Repository != "nas" || p.RepositoryIndex != 1 || p.BytesDone != 3_650_000_000 ||
		len(p.CurrentFiles) != 1 || p.LastReport != "2026-10-21T09:59:58Z" || p.LastMoved != "2026-10-21T09:56:00Z" {
		t.Fatalf("got %s", data)
	}
}

// The check-in's status part never carries progress, so it doesn't change
// while a job's progress moves.
func TestCheckin_StatusPartLeavesOutProgress(t *testing.T) {
	oldStateDir, oldConfig := stateDir, configPath
	t.Cleanup(func() { stateDir, configPath = oldStateDir, oldConfig })
	stateDir = t.TempDir()
	configPath = filepath.Join(stateDir, "rest-o-matic.yaml")
	if err := os.WriteFile(configPath, []byte("backups: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := state.NewStore(statePath(), lockDir())
	cfg := statusConfig()
	if err := store.MarkRunning("gitea", time.Now(), state.TriggerTick); err != nil {
		t.Fatal(err)
	}
	l, ok, err := lock.AcquireJob(lockDir(), "gitea")
	if err != nil || !ok {
		t.Fatalf("taking the job lock: %v %v", ok, err)
	}
	defer l.Unlock()

	now := time.Now()
	fingerprint := func() (string, string) {
		st, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		req, err := buildCheckin(cfg, st, store, now, central.Host{}, nil, state.CheckinState{}, true)
		if err != nil {
			t.Fatal(err)
		}
		return req.Parts.Status.Fingerprint, string(req.Parts.Status.Content)
	}
	before, content := fingerprint()
	if !strings.Contains(content, `"running":{`) {
		t.Fatalf("expected gitea to be running in the status part: %s", content)
	}
	if err := store.SaveProgress("gitea", *backupProgress(now, 0)); err != nil {
		t.Fatal(err)
	}
	after, content := fingerprint()
	if before != after || strings.Contains(content, "progress") {
		t.Fatalf("progress reached the status part: fingerprint %s -> %s, %s", before, after, content)
	}
}

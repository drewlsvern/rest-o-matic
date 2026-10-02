//go:build !windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// runCLIEnv is runCLI with extra environment, returning combined output.
func runCLIEnv(t *testing.T, bin, workdir string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	code := 0
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running CLI: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return out.String(), code
}

// snapshotWorkspace is setupWorkspace plus a second job, postgres, backing
// up to the same repository, and a second repository, offsite, that was
// never initialised.
func snapshotWorkspace(t *testing.T) (workdir, configPath string) {
	t.Helper()
	workdir, configPath = setupWorkspace(t)
	other := filepath.Join(workdir, "db")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(workdir, "src", "a.txt"), filepath.Join(other, "dump.sql")} {
		if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	extra := `
  postgres:
    source:
      paths: ["` + other + `"]
    policy: hot
    repositories: [nas]
  both:
    source:
      paths: ["` + other + `"]
    policy: hot
    repositories: [nas, offsite]
`
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	withRepo := strings.Replace(string(data), "\nbackups:", `
  offsite:
    backend: local
    url: `+filepath.Join(workdir, "never-initialised")+`
    password: testpass

backups:`, 1)
	if err := os.WriteFile(configPath, []byte(withRepo+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	return workdir, configPath
}

func loadLists(t *testing.T, workdir, job string) map[string]state.SnapshotList {
	t.Helper()
	store := state.NewStore(filepath.Join(workdir, ".rest-o-matic", "state.json"), filepath.Join(workdir, ".rest-o-matic", "locks"))
	lists, err := store.LoadSnapshots(job)
	if err != nil {
		t.Fatal(err)
	}
	return lists
}

// resticIDs asks the repository itself which snapshots carry a tag.
func resticIDs(t *testing.T, repo, tag string) []string {
	t.Helper()
	cmd := exec.Command("restic", "-r", repo, "snapshots", "--json", "--tag", tag)
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD=testpass")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("restic snapshots: %v", err)
	}
	var snaps []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &snaps); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range snaps {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return ids
}

func listIDs(list state.SnapshotList) []string {
	var ids []string
	for _, s := range list.Snapshots {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestCLI_SnapshotListMatchesTheRepositoryAfterEachRun(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath := snapshotWorkspace(t)
	repo := filepath.Join(workdir, "repo-nas")

	// Three runs of one job, with another job's snapshot in the same
	// repository. Retention prunes as it goes; whatever it leaves is what
	// the list must hold.
	for i := 0; i < 3; i++ {
		if out, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents"); code != 0 {
			t.Fatalf("run %d failed: %s", i, out)
		}
	}
	if out, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "postgres"); code != 0 {
		t.Fatalf("run postgres failed: %s", out)
	}

	list, ok := loadLists(t, workdir, "documents")["nas"]
	if !ok {
		t.Fatal("no snapshot list was recorded for documents in nas")
	}
	want := resticIDs(t, repo, "documents")
	if got := listIDs(list); strings.Join(got, ",") != strings.Join(want, ",") || len(got) == 0 {
		t.Fatalf("recorded list %v does not match the repository's own %v", got, want)
	}
	for _, s := range list.Snapshots {
		if strings.Join(s.Tags, ",") != "documents" {
			t.Errorf("snapshot %s has tags %v; another job's snapshot is in this job's list", s.ShortID, s.Tags)
		}
	}
	if time.Since(list.ListedAt) > time.Minute {
		t.Errorf("list time %v is not the time of the last run", list.ListedAt)
	}

	// The newest listed snapshot is the one the last run recorded.
	st, err := os.ReadFile(filepath.Join(workdir, ".rest-o-matic", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(st), `"snapshot_id": "`+list.Snapshots[0].ID+`"`) {
		t.Errorf("the newest listed snapshot %s is not the one the last run created", list.Snapshots[0].ShortID)
	}
}

// The list comes from the retention step, so a run invokes restic once to
// back up and once to forget, and no third time.
func TestCLI_SnapshotListNeedsNoExtraResticCall(t *testing.T) {
	requireRestic(t)
	real, err := exec.LookPath("restic")
	if err != nil {
		t.Fatal(err)
	}
	bin := buildBinary(t)
	workdir, configPath := snapshotWorkspace(t)
	shimDir, calls := filepath.Join(workdir, "shim"), filepath.Join(workdir, "calls")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Arguments are `-r <url> <subcommand> ...`.
	shim := "#!/bin/sh\necho \"$3\" >> '" + calls + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shimDir, "restic"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}

	out, code := runCLIEnv(t, bin, workdir, []string{"PATH=" + shimDir + string(os.PathListSeparator) + os.Getenv("PATH")}, "--config", configPath, "run", "documents")
	if code != 0 {
		t.Fatalf("run failed: %s", out)
	}
	data, _ := os.ReadFile(calls)
	if got := strings.Join(strings.Fields(string(data)), " "); got != "backup forget" {
		t.Fatalf("restic was invoked as %q, want exactly a backup and a forget", got)
	}
	if _, ok := loadLists(t, workdir, "documents")["nas"]; !ok {
		t.Fatal("no list was recorded")
	}
}

func TestCLI_FailedRepositoryKeepsItsPreviousList(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath := snapshotWorkspace(t)
	listFile := filepath.Join(workdir, ".rest-o-matic", "snapshots", "documents.json")

	if out, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents"); code != 0 {
		t.Fatalf("setup run failed: %s", out)
	}
	before, err := os.ReadFile(listFile)
	if err != nil {
		t.Fatal(err)
	}

	// The source is gone, so the next backup fails.
	if err := os.RemoveAll(filepath.Join(workdir, "src")); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents"); code == 0 {
		t.Fatal("setup: expected the second run to fail")
	}
	after, err := os.ReadFile(listFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("a failed run changed the recorded list:\nbefore: %s\nafter:  %s", before, after)
	}

	// With two repositories and one failing, only the other's is listed.
	if _, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "both"); code == 0 {
		t.Fatal("setup: expected the job with an uninitialised repository to fail")
	}
	lists := loadLists(t, workdir, "both")
	if _, ok := lists["nas"]; !ok {
		t.Error("the repository that succeeded has no list")
	}
	if _, ok := lists["offsite"]; ok {
		t.Error("the repository that failed has a list")
	}
}

func TestCLI_StatusShowsSnapshotListsWithoutTheRepository(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath := snapshotWorkspace(t)
	if out, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "both"); code == 0 {
		t.Fatalf("setup: expected the run to fail on offsite: %s", out)
	}
	id := loadLists(t, workdir, "both")["nas"].Snapshots[0].ShortID

	// No repository, and no restic to reach one with.
	if err := os.RemoveAll(filepath.Join(workdir, "repo-nas")); err != nil {
		t.Fatal(err)
	}
	noRestic := []string{"PATH=" + t.TempDir()}

	out, code := runCLIEnv(t, bin, workdir, noRestic, "--config", configPath, "status", "both")
	if code != 0 {
		t.Fatalf("status failed (exit %d): %s", code, out)
	}
	for _, want := range []string{"Snapshots in nas, as of just now: 1", id, "Snapshots in offsite: not listed yet"} {
		if !contains(out, want) {
			t.Errorf("expected status to contain %q, got:\n%s", want, out)
		}
	}

	one, code := runCLIEnv(t, bin, workdir, noRestic, "--config", configPath, "status", "both", "--json")
	if code != 0 || !contains(one, `"short_id": "`+id+`"`) {
		t.Errorf("expected the single-job JSON to carry the snapshot (exit %d): %s", code, one)
	}
	all, code := runCLIEnv(t, bin, workdir, noRestic, "--config", configPath, "status", "--json")
	if code != 0 || !contains(all, `"snapshot_lists"`) || contains(all, `"short_id"`) {
		t.Errorf("expected the all-jobs JSON to carry list summaries but no snapshots (exit %d): %s", code, all)
	}
}

// previousList is a list recorded by an earlier run, to check a later run
// leaves alone.
func previousList() state.SnapshotList {
	at := time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC)
	return state.SnapshotList{ListedAt: at.Add(time.Minute), Snapshots: []state.Snapshot{{ID: "earlier", ShortID: "earlier", Time: at, Tags: []string{"documents"}}}}
}

func assertListUnchanged(t *testing.T, f *concurrencyFixture) {
	t.Helper()
	lists, err := f.store.LoadSnapshots("documents")
	if err != nil {
		t.Fatal(err)
	}
	want := previousList()
	got := lists["nas"]
	if !got.ListedAt.Equal(want.ListedAt) || len(got.Snapshots) != 1 || got.Snapshots[0].ID != "earlier" {
		t.Fatalf("got list %+v, want the earlier one untouched", got)
	}
}

func TestSnapshotList_KeptWhenNoRepositoryIsReached(t *testing.T) {
	f := newConcurrencyFixture(t)
	job := f.job("documents", "nas")
	job.Hooks.Before = []string{"exit 1"}
	cfg := f.config(2, job)
	if err := f.store.SaveSnapshots("documents", "nas", previousList()); err != nil {
		t.Fatal(err)
	}

	if s := waitStarted(t, f.startJob(context.Background(), cfg, "documents")); s.kind != jobRan || s.result.Success() {
		t.Fatalf("setup: expected the job to run and fail, got kind=%v", s.kind)
	}
	assertListUnchanged(t, f)
}

// If restic's forget output holds no list, the job still succeeds and the
// earlier list stays.
func TestSnapshotList_KeptWhenForgetReportsNone(t *testing.T) {
	f := newConcurrencyFixture(t)
	fake := filepath.Join(f.dir, "restic")
	script := `#!/bin/sh
case "$3" in
  backup) echo '{"message_type":"summary","snapshot_id":"abc123"}' ;;
  forget) echo "nothing a list could be read from" ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	f.opts.Restic = &execution.ResticRunner{Path: fake}
	job := f.job("documents", "nas")
	job.Hooks = config.Hooks{}
	cfg := f.config(2, job)
	if err := f.store.SaveSnapshots("documents", "nas", previousList()); err != nil {
		t.Fatal(err)
	}

	s := waitStarted(t, f.startJob(context.Background(), cfg, "documents"))
	if s.kind != jobRan || !s.result.Success() {
		t.Fatalf("got kind=%v result=%+v, want the job to succeed although no list could be read", s.kind, s.result)
	}
	assertListUnchanged(t, f)
}

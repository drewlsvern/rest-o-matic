//go:build !windows

package execution

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// fakeBackupRestic writes a fake restic that prints the given standard
// output for `backup` (and exits with exitCode), and an empty `forget`
// result otherwise.
func fakeBackupRestic(t *testing.T, stdout string, exitCode int) *ResticRunner {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "backup.out")
	if err := os.WriteFile(out, []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do [ \"$a\" = backup ] && { cat '" + out + "'; exit " + strconv.Itoa(exitCode) + "; }; done\n" +
		"echo '[]'\n"
	path := filepath.Join(dir, "restic")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ResticRunner{Path: path}
}

func plainRepo(t *testing.T) config.Repository {
	return config.Repository{Backend: "local", URL: filepath.Join(t.TempDir(), "repo"), Password: config.Plain("x")}
}

func TestBackup_StatusLinesReachTheCallback(t *testing.T) {
	long := "/" + strings.Repeat("a", 200_000)
	out := `{"message_type":"status","percent_done":0.1,"total_files":10,"files_done":1,"total_bytes":1000,"bytes_done":100}
not json at all
{"message_type":"status","seconds_elapsed":5,"seconds_remaining":30,"percent_done":0.5,"total_files":10,"files_done":5,"total_bytes":1000,"bytes_done":500,"current_files":["` + long + `"]}
{"message_type":"verbose_status","action":"new","item":"/x"}
{"message_type":"summary","snapshot_id":"abc123"}`
	r := fakeBackupRestic(t, out, 0)
	var got []BackupStatus
	id, err := r.Backup(context.Background(), plainRepo(t), config.ReadDirect, config.Source{Paths: []string{"/src"}}, []string{"docs"}, func(s BackupStatus) { got = append(got, s) }, nil)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if id != "abc123" {
		t.Errorf("snapshot id = %q, want abc123 (from a summary line with no trailing newline)", id)
	}
	if len(got) != 2 {
		t.Fatalf("got %d status reports, want 2: %+v", len(got), got)
	}
	if got[0].BytesDone != 100 || got[0].SecondsRemaining != nil || got[1].BytesDone != 500 {
		t.Errorf("status reports out of order or wrong: %+v", got)
	}
	if got[1].SecondsRemaining == nil || *got[1].SecondsRemaining != 30 || len(got[1].CurrentFiles) != 1 || got[1].CurrentFiles[0] != long {
		t.Errorf("second report lost fields: remaining %v, %d current files", got[1].SecondsRemaining, len(got[1].CurrentFiles))
	}
}

func TestBackup_NilCallback(t *testing.T) {
	r := fakeBackupRestic(t, `{"message_type":"status","bytes_done":1}`+"\n"+`{"message_type":"summary","snapshot_id":"abc"}`+"\n", 0)
	id, err := r.Backup(context.Background(), plainRepo(t), config.ReadDirect, config.Source{Paths: []string{"/src"}}, []string{"docs"}, nil, nil)
	if err != nil || id != "abc" {
		t.Fatalf("got %q, %v", id, err)
	}
}

func TestBackup_FailureStillReported(t *testing.T) {
	r := fakeBackupRestic(t, `{"message_type":"status","bytes_done":1}`+"\n", 3)
	var mu sync.Mutex
	n := 0
	_, err := r.Backup(context.Background(), plainRepo(t), config.ReadDirect, config.Source{Paths: []string{"/src"}}, []string{"docs"}, func(BackupStatus) { mu.Lock(); n++; mu.Unlock() }, nil)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("expected restic's failure, got %v", err)
	}
	if n != 1 {
		t.Errorf("got %d status reports before the failure, want 1", n)
	}
}

// progressLog records what a Progress is told, as strings.
type progressLog struct {
	mu     sync.Mutex
	events []string
}

func (l *progressLog) Phase(job string, p Phase) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := job + " " + p.Name
	if p.Repository != "" {
		e += " " + p.Repository + " " + strconv.Itoa(p.Index) + "/" + strconv.Itoa(p.Count)
	}
	l.events = append(l.events, e)
}

func (l *progressLog) Retry(job, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, job+" retry "+message)
}

func (l *progressLog) BackupStatus(job string, s BackupStatus) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, job+" status "+strconv.FormatInt(s.BytesDone, 10))
}

func TestRunJob_ReportsPhases(t *testing.T) {
	for _, readAs := range []string{"", config.ReadPodmanUnshare} {
		t.Run("read_as="+readAs, func(t *testing.T) {
			f := newReadModeFakes(t)
			offsite := f.repo()
			offsite.URL = filepath.Join(f.dir, "offsite")
			cfg := &config.Config{
				Repositories: map[string]config.Repository{"nas": f.repo(), "offsite": offsite},
				Policies:     map[string]config.Policy{"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 1}}},
				Backups: map[string]config.Job{
					"docs": {Name: "docs", Policy: "hot", ReadAs: readAs, Source: config.Source{Paths: []string{"/src"}},
						Hooks:        config.Hooks{Before: []string{"true"}, After: config.AfterHooks{Always: []string{"true"}}},
						Repositories: []config.RepositoryRef{{Name: "nas"}, {Name: "offsite"}}},
				},
			}
			var log progressLog
			res := RunJob(context.Background(), context.Background(), cfg, "docs", Options{Restic: f.runner(), LockDir: t.TempDir(), Progress: &log})
			if !res.Success() {
				t.Fatalf("job failed: %+v", res)
			}
			want := "docs before_hooks|docs check nas 1/2|docs backup nas 1/2|docs status 5|docs forget nas 1/2|docs check offsite 2/2|docs backup offsite 2/2|docs status 5|docs forget offsite 2/2|docs after_hooks"
			if got := strings.Join(log.events, "|"); got != want {
				t.Fatalf("got  %s\nwant %s", got, want)
			}
		})
	}
}

func TestRunJob_NoHookPhasesWithoutHooks(t *testing.T) {
	f := newReadModeFakes(t)
	cfg := &config.Config{
		Repositories: map[string]config.Repository{"nas": f.repo()},
		Policies:     map[string]config.Policy{"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 1}}},
		Backups: map[string]config.Job{"docs": {Name: "docs", Policy: "hot", Source: config.Source{Paths: []string{"/src"}},
			Repositories: []config.RepositoryRef{{Name: "nas"}}}},
	}
	var log progressLog
	RunJob(context.Background(), context.Background(), cfg, "docs", Options{Restic: f.runner(), LockDir: t.TempDir(), Progress: &log})
	if got := strings.Join(log.events, "|"); got != "docs check nas 1/1|docs backup nas 1/1|docs status 5|docs forget nas 1/1" {
		t.Fatalf("got %s", got)
	}
}

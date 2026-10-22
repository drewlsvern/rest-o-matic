//go:build !windows

package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// checkFakes is readModeFakes with a restic whose `cat config` of the s3
// repository does what the test asks: hangs after printing a retry, or
// fails at once. Every other repository's check succeeds.
func checkFakes(t *testing.T, catConfig string) readModeFakes {
	t.Helper()
	f := newReadModeFakes(t)
	script := `#!/bin/sh
{ printf '%s\n' "$@"; echo --; } >> "$FAKES/restic.args"
case " $* " in
*"/s3 cat config "*)
` + catConfig + `
  ;;
esac
echo '{"message_type":"status","percent_done":0.5,"bytes_done":5,"total_bytes":10}'
echo '{"message_type":"summary","snapshot_id":"abc123"}'
`
	if err := os.WriteFile(f.restic, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

// The s3 repository's url ends in /s3; see twoRepoJob.
const (
	hangsAfterRetry = `  echo 'Stat(<config/>) returned error, retrying after 1.1s: Stat: Get "https://s3.example.com/b/?location=": dial tcp 192.0.2.1:443: i/o timeout' >&2
  exec sleep 30`
	failsAtOnce = `  echo '{"message_type":"exit_error","code":12,"message":"Fatal: wrong password or no key found"}' >&2
  exit 12`
)

// twoRepoJob backs up to s3 then nas; s3's connect_timeout is as given.
func twoRepoJob(f readModeFakes, readAs string, s3Timeout time.Duration, hookLog string) *config.Config {
	s3 := f.repo()
	s3.URL = filepath.Join(f.dir, "s3")
	ct := config.ConnectTimeout(s3Timeout)
	s3.ConnectTimeout = &ct
	job := config.Job{Name: "docs", Policy: "hot", ReadAs: readAs, Source: config.Source{Paths: []string{"/src"}},
		Repositories: []config.RepositoryRef{{Name: "s3"}, {Name: "nas"}}}
	if hookLog != "" {
		job.Hooks.After.Failure = []string{`echo "$RESTOMATIC_FAILED_REPOS|$RESTOMATIC_UNREACHABLE_REPOS|$RESTOMATIC_ERROR" > ` + hookLog}
	}
	return &config.Config{
		Repositories: map[string]config.Repository{"s3": s3, "nas": f.repo()},
		Policies:     map[string]config.Policy{"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 1}}},
		Backups:      map[string]config.Job{"docs": job},
	}
}

func TestRunJob_UnreachableRepositoryIsSkipped(t *testing.T) {
	for _, readAs := range []string{"", config.ReadPodmanUnshare} {
		t.Run("read_as="+readAs, func(t *testing.T) {
			f := checkFakes(t, hangsAfterRetry)
			hookLog := filepath.Join(f.dir, "hook.log")
			var log progressLog
			start := time.Now()
			res := RunJob(context.Background(), context.Background(), twoRepoJob(f, readAs, 500*time.Millisecond, hookLog), "docs",
				Options{Restic: f.runner(), LockDir: t.TempDir(), Progress: &log})
			if took := time.Since(start); took > 10*time.Second {
				t.Fatalf("took %v: the check wasn't stopped at its timeout", took)
			}

			if len(res.Repos) != 2 || res.Success() {
				t.Fatalf("got %+v", res.Repos)
			}
			s3, nas := res.Repos[0], res.Repos[1]
			wantReason := `Stat: Get "https://s3.example.com/b/?location=": dial tcp 192.0.2.1:443: i/o timeout`
			if !s3.Unreachable || s3.CheckTimeout != 500*time.Millisecond || s3.BackupErr == nil || s3.BackupErr.Error() != wantReason {
				t.Fatalf("s3: %+v", s3)
			}
			if nas.BackupErr != nil || nas.SnapshotID != "abc123" {
				t.Fatalf("nas, after the unreachable s3, didn't back up: %+v", nas)
			}
			// s3 got no backup at all.
			for _, c := range f.calls(t, "restic") {
				if strings.Contains(c, "/s3 backup") || strings.Contains(c, "/s3 forget") {
					t.Errorf("s3 was given %q", c)
				}
			}
			if !strings.Contains(strings.Join(log.events, "|"), "docs check s3 1/2|docs retry "+wantReason+"|docs check nas 2/2") {
				t.Errorf("events: %q", log.events)
			}
			if got := res.FailureSummary(); got != "repository s3: unreachable (checked for 0s): "+wantReason {
				t.Errorf("summary %q", got)
			}
			hook, _ := os.ReadFile(hookLog)
			if got := strings.TrimSpace(string(hook)); got != "s3|s3|repository s3: unreachable (checked for 0s): "+wantReason {
				t.Errorf("failure hook saw %q", got)
			}
		})
	}
}

func TestRunJob_CheckFailingAtOnceIsABackupFailure(t *testing.T) {
	f := checkFakes(t, failsAtOnce)
	res := RunJob(context.Background(), context.Background(), twoRepoJob(f, "", time.Minute, ""), "docs", Options{Restic: f.runner(), LockDir: t.TempDir()})
	s3 := res.Repos[0]
	if s3.Unreachable || s3.BackupErr == nil || !strings.Contains(s3.BackupErr.Error(), "wrong password or no key found") || !strings.Contains(s3.BackupErr.Error(), "exit status 12") {
		t.Fatalf("s3: %+v", s3)
	}
}

func TestRunJob_CheckTurnedOff(t *testing.T) {
	f := checkFakes(t, hangsAfterRetry)
	res := RunJob(context.Background(), context.Background(), twoRepoJob(f, "", 0, ""), "docs", Options{Restic: f.runner(), LockDir: t.TempDir()})
	if !res.Success() {
		t.Fatalf("got %+v", res.Repos)
	}
	for _, c := range f.calls(t, "restic") {
		if strings.Contains(c, "/s3 cat config") {
			t.Fatalf("s3 was checked with connect_timeout 0: %q", c)
		}
	}
}

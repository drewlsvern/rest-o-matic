//go:build !windows

package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/secrets"
)

// lockedFixture has a host key, a second key the host doesn't hold, and a
// fake restic that records the credentials it was started with.
type lockedFixture struct {
	dir      string
	keyPath  string
	host     *secrets.HostKey
	stranger *secrets.HostKey
	fake     string
	seen     string
}

func newLockedFixture(t *testing.T) *lockedFixture {
	t.Helper()
	dir := t.TempDir()
	f := &lockedFixture{dir: dir, keyPath: filepath.Join(dir, "keys", "host.key"), fake: filepath.Join(dir, "restic"), seen: filepath.Join(dir, "seen")}
	newKey := func(path string) *secrets.HostKey {
		if _, err := secrets.Generate(path); err != nil {
			t.Fatal(err)
		}
		key, err := secrets.LoadHostKey(path)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	f.host = newKey(f.keyPath)
	f.stranger = newKey(filepath.Join(dir, "other", "host.key"))

	script := "#!/bin/sh\necho \"$RESTIC_PASSWORD|$AWS_SECRET_ACCESS_KEY|$*\" >> '" + f.seen + "'\n"
	if err := os.WriteFile(f.fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *lockedFixture) lock(t *testing.T, value string, key *secrets.HostKey) config.Secret {
	t.Helper()
	locked, err := secrets.Lock(value, []age.Recipient{key.Recipient()})
	if err != nil {
		t.Fatal(err)
	}
	return config.Secret{Value: locked, Locked: true}
}

func (f *lockedFixture) runner() *ResticRunner {
	return &ResticRunner{Path: f.fake, Secrets: secrets.NewUnlocker(f.keyPath, nil)}
}

func (f *lockedFixture) started(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.seen)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func TestLocked_PasswordAndEnvReachResticAsPlainText(t *testing.T) {
	f := newLockedFixture(t)
	repo := config.Repository{
		Backend:  "local",
		URL:      "/srv/repo",
		Password: f.lock(t, "correct-horse", f.host),
		Env: map[string]config.Secret{
			"AWS_ACCESS_KEY_ID":     config.Plain("AKIAEXAMPLE"),
			"AWS_SECRET_ACCESS_KEY": f.lock(t, "wJalrXUt", f.host),
		},
	}
	runner := f.runner()

	if _, err := runner.Backup(context.Background(), repo, []string{f.dir}, []string{"job"}); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if err := runner.Forget(context.Background(), repo, "job", config.Retention{"daily": 7}); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if code, err := runner.PassThrough(context.Background(), repo, []string{"snapshots"}); err != nil || code != 0 {
		t.Fatalf("PassThrough: code=%d err=%v", code, err)
	}

	lines := strings.Split(strings.TrimSpace(f.started(t)), "\n")
	if len(lines) != 3 {
		t.Fatalf("restic was started %d times, want 3: %q", len(lines), lines)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "correct-horse|wJalrXUt|") {
			t.Errorf("restic was started with %q, want the unlocked password and env value", line)
		}
	}
}

func TestLocked_CannotUnlockFailsWithoutStartingRestic(t *testing.T) {
	f := newLockedFixture(t)
	cases := map[string]struct {
		runner *ResticRunner
		repo   config.Repository
		want   []string
	}{
		"no host key": {
			runner: &ResticRunner{Path: f.fake, Secrets: secrets.NewUnlocker(filepath.Join(f.dir, "missing.key"), nil)},
			repo:   config.Repository{URL: "/srv/repo", Password: f.lock(t, "correct-horse", f.host)},
			want:   []string{"password is locked", "no host key", "missing.key"},
		},
		"locked for another host": {
			runner: f.runner(),
			repo:   config.Repository{URL: "/srv/repo", Password: f.lock(t, "correct-horse", f.stranger)},
			want:   []string{"password is locked", "not locked for this host's key"},
		},
		"env value locked for another host": {
			runner: f.runner(),
			repo: config.Repository{URL: "/srv/repo", Password: config.Plain("x"),
				Env: map[string]config.Secret{"AWS_SECRET_ACCESS_KEY": f.lock(t, "wJalrXUt", f.stranger)}},
			want: []string{"env AWS_SECRET_ACCESS_KEY is locked", "not locked for this host's key"},
		},
		"no unlocker at all": {
			runner: &ResticRunner{Path: f.fake},
			repo:   config.Repository{URL: "/srv/repo", Password: f.lock(t, "correct-horse", f.host)},
			want:   []string{"password is locked", "no host key is configured"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := c.runner.Backup(context.Background(), c.repo, []string{f.dir}, []string{"job"})
			if err == nil {
				t.Fatal("expected the backup to fail")
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("got %q, want it to contain %q", err, want)
				}
			}
			for _, secret := range append([]config.Secret{c.repo.Password}, c.repo.Env["AWS_SECRET_ACCESS_KEY"]) {
				if secret.Locked && strings.Contains(err.Error(), secret.Value[:16]) {
					t.Errorf("the error contains part of the locked value: %q", err)
				}
			}
			if got := f.started(t); got != "" {
				t.Errorf("restic was started although the value couldn't be unlocked: %q", got)
			}
		})
	}
}

func TestLocked_OneRepositoryFailingLeavesTheOtherAttempted(t *testing.T) {
	f := newLockedFixture(t)
	cfg := &config.Config{
		MaxConcurrent: 2,
		Policies:      map[string]config.Policy{"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 24}}},
		Repositories: map[string]config.Repository{
			"nas":     {Backend: "local", URL: "/srv/nas", Password: config.Plain("plain-pw")},
			"offsite": {Backend: "local", URL: "/srv/offsite", Password: f.lock(t, "correct-horse", f.stranger)},
		},
		Backups: map[string]config.Job{"documents": {
			Name:         "documents",
			Source:       config.Source{Paths: []string{f.dir}},
			Policy:       "hot",
			Repositories: []config.RepositoryRef{{Name: "offsite"}, {Name: "nas"}},
		}},
	}

	result := RunJob(context.Background(), context.Background(), cfg, "documents", Options{Restic: f.runner()})

	if result.Success() {
		t.Fatal("expected the job to be reported as failed")
	}
	if got := result.Repos[0]; got.Repository != "offsite" || got.BackupErr == nil {
		t.Errorf("got %+v, want offsite to have failed", got)
	}
	if got := result.Repos[1]; got.Repository != "nas" || got.BackupErr != nil {
		t.Errorf("got %+v, want nas to have been backed up", got)
	}
	if got := result.FailureSummary(); !strings.Contains(got, "repository offsite") || !strings.Contains(got, "password is locked") {
		t.Errorf("got failure summary %q, want it to name the repository and the field", got)
	}
	if started := f.started(t); !strings.Contains(started, "plain-pw|") || strings.Contains(started, "/srv/offsite") {
		t.Errorf("restic should have run for nas only, got: %q", started)
	}
}

func TestLocked_ExecUsesOnlyTheRepositoryItIsGiven(t *testing.T) {
	f := newLockedFixture(t)
	cfg := &config.Config{
		Repositories: map[string]config.Repository{
			"nas":     {Backend: "local", URL: "/srv/nas", Password: f.lock(t, "correct-horse", f.host)},
			"offsite": {Backend: "local", URL: "/srv/offsite", Password: f.lock(t, "other-pw", f.stranger)},
		},
	}
	opts := Options{Restic: f.runner(), LockDir: filepath.Join(f.dir, "locks")}

	result, err := Exec(context.Background(), cfg, "nas", []string{"snapshots"}, false, opts)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("exec on nas: result=%+v err=%v, want it to run although offsite is locked for another host", result, err)
	}
	if started := f.started(t); !strings.HasPrefix(started, "correct-horse|") {
		t.Fatalf("restic was started with %q, want nas's unlocked password", started)
	}

	_, err = Exec(context.Background(), cfg, "offsite", []string{"snapshots"}, false, opts)
	if err == nil || !strings.Contains(err.Error(), `repository "offsite"`) || !strings.Contains(err.Error(), "password is locked") {
		t.Fatalf("exec on offsite: got %v, want an error naming the repository and the field", err)
	}
}

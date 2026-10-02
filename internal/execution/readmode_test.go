//go:build !windows

package execution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// readModeFakes writes a fake restic that appends each call's arguments to
// <dir>/restic.args and a fake podman that does the same in
// <dir>/podman.args before running `podman unshare`'s command, so tests can
// see exactly what was invoked without a real restic or podman.
type readModeFakes struct {
	dir, restic, podman string
}

func newReadModeFakes(t *testing.T) readModeFakes {
	t.Helper()
	dir := t.TempDir()
	f := readModeFakes{dir: dir, restic: filepath.Join(dir, "restic"), podman: filepath.Join(dir, "podman")}
	scripts := map[string]string{
		f.restic: `#!/bin/sh
{ printf '%s\n' "$@"; echo --; } >> "$FAKES/restic.args"
echo '{"message_type":"summary","snapshot_id":"abc123"}'
`,
		f.podman: `#!/bin/sh
{ printf '%s\n' "$@"; echo --; } >> "$FAKES/podman.args"
[ "$1" = unshare ] || exit 99
shift
exec "$@"
`,
	}
	for path, script := range scripts {
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f readModeFakes) runner() *ResticRunner {
	return &ResticRunner{Path: f.restic, Podman: f.podman}
}

func (f readModeFakes) repo() config.Repository {
	return config.Repository{Backend: "local", URL: filepath.Join(f.dir, "repo"), Password: "x", Env: map[string]string{"FAKES": f.dir}}
}

// calls returns each recorded call of the named fake as one
// space-joined string, in order; nil if it never ran.
func (f readModeFakes) calls(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, name+".args"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, c := range strings.Split(strings.TrimSuffix(string(b), "--\n"), "--\n") {
		calls = append(calls, strings.Join(strings.Fields(c), " "))
	}
	return calls
}

// backupCall returns the fake restic's `backup` call.
func (f readModeFakes) backupCall(t *testing.T) string {
	t.Helper()
	for _, c := range f.calls(t, "restic") {
		if strings.Contains(c, " backup ") {
			return c
		}
	}
	t.Fatal("restic backup was never called")
	return ""
}

func TestBackup_DirectRunsResticItself(t *testing.T) {
	f := newReadModeFakes(t)
	if _, err := f.runner().Backup(context.Background(), f.repo(), config.ReadDirect, []string{"/src"}, []string{"docs"}); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	want := "-r " + f.repo().URL + " backup /src --tag docs --json"
	if got := f.backupCall(t); got != want {
		t.Fatalf("restic args = %q, want %q", got, want)
	}
	if got := f.calls(t, "podman"); got != nil {
		t.Fatalf("expected podman not to run, got %q", got)
	}
}

func TestBackup_PodmanUnshareWrapsRestic(t *testing.T) {
	f := newReadModeFakes(t)
	id, err := f.runner().Backup(context.Background(), f.repo(), config.ReadPodmanUnshare, []string{"/src"}, []string{"gitea"})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if id != "abc123" {
		t.Errorf("expected the snapshot id to come through podman, got %q", id)
	}
	resticArgs := "-r " + f.repo().URL + " backup /src --tag gitea --json"
	if got := f.calls(t, "podman"); len(got) != 1 || got[0] != "unshare "+f.restic+" "+resticArgs {
		t.Fatalf("podman calls = %q, want one call: unshare %s %s", got, f.restic, resticArgs)
	}
	// The fake restic only finds $FAKES if the environment passed through.
	if got := f.backupCall(t); got != resticArgs {
		t.Fatalf("restic args = %q, want %q", got, resticArgs)
	}
}

func TestBackup_PodmanUnshareWithoutPodmanFails(t *testing.T) {
	f := newReadModeFakes(t)
	r := f.runner()
	r.Podman = filepath.Join(f.dir, "no-podman")
	_, err := r.Backup(context.Background(), f.repo(), config.ReadPodmanUnshare, []string{"/src"}, []string{"gitea"})
	if err == nil || !strings.Contains(err.Error(), "needs podman") {
		t.Fatalf("expected a needs-podman error, got %v", err)
	}
	if got := f.calls(t, "restic"); got != nil {
		t.Fatalf("expected restic not to run, got %q", got)
	}
}

func TestRunJob_ReadModeTag(t *testing.T) {
	for _, tc := range []struct {
		readAs  string
		wrapped bool
	}{
		{"", false},
		{config.ReadPodmanUnshare, true},
	} {
		t.Run("read_as="+tc.readAs, func(t *testing.T) {
			f := newReadModeFakes(t)
			cfg := &config.Config{
				Repositories: map[string]config.Repository{"nas": f.repo()},
				Policies:     map[string]config.Policy{"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 1}}},
				Backups: map[string]config.Job{
					"gitea": {Name: "gitea", Source: config.Source{Paths: []string{"/src"}}, Policy: "hot",
						Repositories: []config.RepositoryRef{{Name: "nas"}}, ReadAs: tc.readAs},
				},
			}
			res := RunJob(context.Background(), context.Background(), cfg, "gitea", Options{Restic: f.runner(), LockDir: t.TempDir()})
			if len(res.Repos) != 1 || res.Repos[0].BackupErr != nil || res.Repos[0].ForgetErr != nil {
				t.Fatalf("expected backup and forget to succeed, got %+v", res.Repos)
			}

			hasTag := strings.Contains(f.backupCall(t), "--tag restomatic-read="+config.ReadPodmanUnshare)
			if hasTag != tc.wrapped {
				t.Errorf("read-mode tag present = %v, want %v", hasTag, tc.wrapped)
			}
			// Only the backup is wrapped; forget always runs directly.
			podmanCalls := f.calls(t, "podman")
			if tc.wrapped && (len(podmanCalls) != 1 || !strings.Contains(podmanCalls[0], " backup ")) {
				t.Errorf("expected podman to wrap only the backup, got %q", podmanCalls)
			}
			if !tc.wrapped && podmanCalls != nil {
				t.Errorf("expected podman not to run for a direct job, got %q", podmanCalls)
			}
		})
	}
}

// exitError returns a real *exec.ExitError with the given exit code.
func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
	if err == nil {
		t.Fatalf("expected exit code %d", code)
	}
	return err
}

func TestUnreadableHint(t *testing.T) {
	const denied = "error: open /src/db: permission denied\n"
	// What restic 0.16 prints under --json: the errno, with no message.
	const deniedOldJSON = `{"message_type":"error","error":{"Op":"open","Path":"/src/db","Err":13},"during":"archival","item":"/src/db"}` + "\n"
	const goneOldJSON = `{"message_type":"error","error":{"Op":"lstat","Path":"/src/gone","Err":2},"during":"archival","item":"/src/gone"}` + "\n"
	for _, tc := range []struct {
		name       string
		code       int
		stderr     string
		mode       string
		goos       string
		havePodman bool
		want       string // substring; "" means no hint
	}{
		{"direct on a podman host", 3, denied, config.ReadDirect, "linux", true, "read_as: podman-unshare"},
		{"direct on a podman host, restic 0.16 output", 3, deniedOldJSON, config.ReadDirect, "linux", true, "read_as: podman-unshare"},
		{"exit 3 without permission errors, restic 0.16 output", 3, goneOldJSON, config.ReadDirect, "linux", true, ""},
		{"direct without podman", 3, denied, config.ReadDirect, "linux", false, "run rest-o-matic as the user that owns them"},
		{"direct on macOS", 3, denied, config.ReadDirect, "darwin", true, "run rest-o-matic as the user that owns them"},
		{"already podman-unshare", 3, denied, config.ReadPodmanUnshare, "linux", true, "run rest-o-matic as the user that owns them"},
		{"exit 3 without permission errors", 3, "error: lstat /src/gone: no such file or directory\n", config.ReadDirect, "linux", true, ""},
		{"other exit code", 1, denied, config.ReadDirect, "linux", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origGOOS, origLookPath := hostGOOS, lookPath
			t.Cleanup(func() { hostGOOS, lookPath = origGOOS, origLookPath })
			hostGOOS = tc.goos
			lookPath = func(string) (string, error) {
				if tc.havePodman {
					return "/usr/bin/podman", nil
				}
				return "", errors.New("not found")
			}

			got := unreadableHint(exitError(t, tc.code), tc.stderr, tc.mode)
			switch {
			case tc.want == "" && got != "":
				t.Fatalf("expected no hint, got %q", got)
			case tc.want != "" && !strings.Contains(got, tc.want):
				t.Fatalf("expected a hint containing %q, got %q", tc.want, got)
			case tc.want != "" && tc.want != "read_as: podman-unshare" && strings.Contains(got, "podman-unshare"):
				t.Fatalf("expected no podman-unshare suggestion, got %q", got)
			}
		})
	}
}

func TestExec_PodmanUnshareModeWrapsRestic(t *testing.T) {
	f := newReadModeFakes(t)
	cfg := &config.Config{
		Repositories: map[string]config.Repository{"nas": f.repo()},
		Backups: map[string]config.Job{
			"gitea": {Name: "gitea", Repositories: []config.RepositoryRef{{Name: "nas"}}, ReadAs: config.ReadPodmanUnshare},
		},
	}
	res, err := Exec(context.Background(), cfg, "nas", config.ReadPodmanUnshare, []string{"ls", "abc123"}, false, Options{Restic: f.runner(), LockDir: t.TempDir()})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Exec: %v %+v", err, res)
	}
	want := "unshare " + f.restic + " -r " + f.repo().URL + " ls abc123"
	if got := f.calls(t, "podman"); len(got) != 1 || got[0] != want {
		t.Fatalf("podman calls = %q, want %q", got, want)
	}
}

// --- Restore read-mode check (spec: Restore Must Match the Job's Read Mode) ---

func TestRestoreSnapshotArg(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		want   string
		wantOK bool
	}{
		{[]string{"restore", "9e0e530a", "--target", "/r"}, "9e0e530a", true},
		{[]string{"restore", "--target", "/r", "-i", "/x", "latest"}, "latest", true},
		{[]string{"restore", "--target=/r", "9e0e530a:/home/me/data"}, "9e0e530a", true},
		{[]string{"restore", "--tag", "a,b", "--verify", "latest", "-t", "/r"}, "latest", true},
		{[]string{"restore", "--target", "/r"}, "", false},
		{[]string{"restore", "--some-new-flag", "value", "9e0e530a"}, "", false},
	} {
		got, ok := restoreSnapshotArg(tc.args)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("restoreSnapshotArg(%q) = %q, %v; want %q, %v", tc.args, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestRestoreReadModeCheck_Latest(t *testing.T) {
	const want = "restomatic-read=podman-unshare"
	for _, tc := range []struct {
		name    string
		args    []string
		allowed bool
	}{
		{"no tag filter", []string{"restore", "latest", "-t", "/r"}, false},
		{"filter without the tag", []string{"restore", "latest", "--tag", "gitea", "-t", "/r"}, false},
		{"filter with the tag", []string{"restore", "latest", "--tag", "gitea," + want, "-t", "/r"}, true},
		{"= form with the tag", []string{"restore", "latest", "--tag=" + want}, true},
		{"one of two filters lacks it", []string{"restore", "latest", "--tag", "gitea," + want, "--tag", "gitea"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// latest never needs restic: the filters alone decide.
			msg, err := restoreReadModeCheck(context.Background(), &ResticRunner{Path: "/nonexistent"}, config.Repository{}, config.ReadPodmanUnshare, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if (msg == "") != tc.allowed {
				t.Fatalf("allowed = %v, want %v (msg %q)", msg == "", tc.allowed, msg)
			}
			if !tc.allowed && !strings.Contains(msg, want) {
				t.Errorf("expected the refusal to name %s, got %q", want, msg)
			}
		})
	}
}

func TestExec_RestoreReadModeCheck(t *testing.T) {
	requireRestic(t)
	repo := initRepo(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	direct := NewResticRunner()
	untagged, err := direct.Backup(context.Background(), repo, config.ReadDirect, []string{src}, []string{"gitea"})
	if err != nil {
		t.Fatal(err)
	}
	tagged, err := direct.Backup(context.Background(), repo, config.ReadDirect, []string{src}, []string{"gitea", readModeTag(config.ReadPodmanUnshare)})
	if err != nil {
		t.Fatal(err)
	}

	// A pass-through podman, so the allowed restore runs without needing
	// rootless podman here.
	dir := t.TempDir()
	podman := filepath.Join(dir, "podman")
	if err := os.WriteFile(podman, []byte("#!/bin/sh\nshift\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Repositories: map[string]config.Repository{"nas": repo}}
	opts := Options{Restic: &ResticRunner{Path: "restic", Podman: podman}, LockDir: t.TempDir()}
	restore := func(mode, snapshot string) ExecResult {
		t.Helper()
		res, err := Exec(context.Background(), cfg, "nas", mode, []string{"restore", snapshot, "--target", filepath.Join(dir, "out-"+snapshot)}, false, opts)
		if err != nil {
			t.Fatalf("Exec: %v", err)
		}
		return res
	}

	if res := restore(config.ReadPodmanUnshare, untagged); res.Blocked != BlockedByReadMode || res.ExitCode != ExitReadModeBlocked || !strings.Contains(res.ReadModeMsg, "without --job") {
		t.Errorf("expected the untagged snapshot to be refused, got %+v", res)
	}
	if res := restore(config.ReadPodmanUnshare, "deadbeef"); res.Blocked != BlockedByReadMode || !strings.Contains(res.ReadModeMsg, "could not check") {
		t.Errorf("expected an unknown snapshot to be refused, got %+v", res)
	}
	if res := restore(config.ReadPodmanUnshare, tagged); res.Blocked != NotBlocked || res.ExitCode != 0 {
		t.Errorf("expected the tagged snapshot to restore, got %+v", res)
	}
	// Direct mode (plain exec, or a direct job) is never checked.
	if res := restore(config.ReadDirect, untagged); res.Blocked != NotBlocked || res.ExitCode != 0 {
		t.Errorf("expected a direct restore to run unchecked, got %+v", res)
	}
	// Other subcommands aren't checked either.
	res, err := Exec(context.Background(), cfg, "nas", config.ReadPodmanUnshare, []string{"ls", untagged}, false, opts)
	if err != nil || res.Blocked != NotBlocked {
		t.Errorf("expected ls to run unchecked, got %+v, %v", res, err)
	}
}

func TestExitReadModeBlocked_IsDistinct(t *testing.T) {
	for _, c := range []int{ExitLockBlocked, ExitGateBlocked, ExitStateDirBlocked, 3} {
		if ExitReadModeBlocked == c {
			t.Fatalf("ExitReadModeBlocked collides with %d", c)
		}
	}
}

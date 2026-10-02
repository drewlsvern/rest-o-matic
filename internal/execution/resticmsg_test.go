package execution

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// These are what restic 0.19 and 0.16 really print on standard error.
const (
	stderrNoRepository = `{"message_type":"exit_error","code":10,"message":"Fatal: repository does not exist: unable to open config file: stat /mnt/nas/restic-repo/config: no such file or directory\nIs there a repository at the following location?\n/mnt/nas/restic-repo"}`
	stderrWrongPass    = `{"message_type":"exit_error","code":12,"message":"Fatal: wrong password or no key found"}`
	stderrUnreadable   = `{"message_type":"error","error":{"message":"/srv/db/secret: open /srv/db/secret: permission denied"},"during":"archival","item":"/srv/db/secret"}
{"message_type":"exit_error","code":3,"message":"Warning: at least one source file could not be read"}`
	stderrUnreadable016 = `{"message_type":"error","error":{"Op":"open","Path":"/srv/db/secret","Err":13},"during":"archival","item":"/srv/db/secret"}
Warning: at least one source file could not be read`
	stderrNoSource = `/home/me/gone does not exist, skipping
{"message_type":"exit_error","code":1,"message":"Fatal: all source directories/files do not exist"}`
	stderrPlain = `Fatal: repository does not exist: unable to open config file: stat /mnt/nas/restic-repo/config: no such file or directory
Is there a repository at the following location?
/mnt/nas/restic-repo`
)

func TestResticMessages(t *testing.T) {
	for _, tc := range []struct{ name, stderr, want string }{
		{"missing repository", stderrNoRepository, "repository does not exist: unable to open config file: stat /mnt/nas/restic-repo/config: no such file or directory"},
		{"wrong password", stderrWrongPass, "wrong password or no key found"},
		{"unreadable file", stderrUnreadable, "/srv/db/secret: open /srv/db/secret: permission denied; Warning: at least one source file could not be read"},
		// The number is the operating system's own, so its text is too:
		// "permission denied" on Linux and macOS.
		{"unreadable file, restic 0.16", stderrUnreadable016, "open /srv/db/secret: " + syscall.Errno(13).Error() + "; Warning: at least one source file could not be read"},
		{"missing source path", stderrNoSource, "/home/me/gone does not exist, skipping; all source directories/files do not exist"},
		{"empty", "", ""},
		{"only blank lines", "\n  \n", ""},
		{"progress objects carry no message", `{"message_type":"status","percent_done":0.5}` + "\n" + `{"message_type":"summary","snapshot_id":"abc"}`, ""},
		{"a brace that isn't JSON is kept", "{oops", "{oops"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resticMessages(tc.stderr); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// Without --json restic prints plain lines. A fatal error is followed by
// advice about it, which is dropped just as it is from a JSON message.
func TestResticMessages_PlainFatalDropsTheAdviceAfterIt(t *testing.T) {
	got := resticMessages("unable to open cache\n" + stderrPlain)
	want := "unable to open cache; repository does not exist: unable to open config file: stat /mnt/nas/restic-repo/config: no such file or directory"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestResticMessages_ManyErrorsAreCounted(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf(`{"message_type":"error","error":{"message":"/srv/f%d: permission denied"}}`, i))
	}
	// The same message twice is one message.
	lines = append(lines, lines[0])

	got := resticMessages(strings.Join(lines, "\n"))
	want := "/srv/f0: permission denied; /srv/f1: permission denied; /srv/f2: permission denied; and 17 more"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestResticMessages_ErrorNumberReadsAsPermissionDeniedOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("error numbers mean something else on Windows")
	}
	if got := resticMessages(stderrUnreadable016); !strings.Contains(got, "open /srv/db/secret: permission denied") {
		t.Errorf("got %q, want restic 0.16's error number 13 to read as permission denied", got)
	}
}

func TestResticFailure(t *testing.T) {
	runErr := errors.New("exit status 10")

	err := resticFailure("restic backup failed", runErr, stderrNoRepository)
	want := "restic backup failed: exit status 10: repository does not exist: unable to open config file: stat /mnt/nas/restic-repo/config: no such file or directory"
	if err.Error() != want {
		t.Errorf("got  %q\nwant %q", err, want)
	}
	if !errors.Is(err, runErr) {
		t.Error("the exit error must still be reachable, for the unreadable-files hint and exit codes")
	}
	if strings.Contains(err.Error(), "message_type") {
		t.Errorf("the error still contains JSON: %q", err)
	}

	if got := resticFailure("restic forget failed", runErr, "").Error(); got != "restic forget failed: exit status 10" {
		t.Errorf("with no output got %q, want no trailing separator", got)
	}
}

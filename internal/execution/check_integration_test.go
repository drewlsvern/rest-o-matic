package execution

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// silentServer accepts connections and never answers them, like an
// endpoint that has stopped responding. It returns its address.
func silentServer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		l.Close()
		for _, c := range conns {
			c.Close()
		}
	})
	return l.Addr().String()
}

// resticRetriesOnOpen reports whether the restic on PATH retries opening a
// repository, printing a line for each retry; restic 0.16 doesn't.
func resticRetriesOnOpen(t *testing.T) bool {
	out, err := exec.Command("restic", "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	return !strings.HasPrefix(strings.TrimPrefix(string(out), "restic "), "0.16.")
}

func TestCheck_AgainstRealRestic(t *testing.T) {
	requireRestic(t)
	r := NewResticRunner()

	if err := r.Check(context.Background(), initRepo(t), config.ReadDirect, time.Minute, nil); err != nil {
		t.Fatalf("a healthy repository failed the check: %v", err)
	}

	missing := config.Repository{Backend: "local", URL: filepath.Join(t.TempDir(), "never-initialised"), Password: config.Plain("x")}
	err := r.Check(context.Background(), missing, config.ReadDirect, time.Minute, nil)
	// restic 0.16 says "unable to open config file: … no such file", later
	// versions "repository does not exist".
	if err == nil || errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "restic could not open the repository: exit status") ||
		!strings.Contains(err.Error(), "does not exist") && !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("a missing repository: got %v, want restic's failure", err)
	}

	silent := config.Repository{Backend: "s3", URL: "s3:https://" + silentServer(t) + "/bucket", Password: config.Plain("x"),
		Env: map[string]config.Secret{"AWS_ACCESS_KEY_ID": config.Plain("x"), "AWS_SECRET_ACCESS_KEY": config.Plain("y")}}
	var retries []string
	start := time.Now()
	err = r.Check(context.Background(), silent, config.ReadDirect, 15*time.Second, func(m string) { retries = append(retries, m) })
	took := time.Since(start)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("an endpoint that never answers: got %v, want unreachable", err)
	}
	if took > 25*time.Second {
		t.Errorf("the check took %v with a 15s timeout", took)
	}
	if resticRetriesOnOpen(t) {
		if len(retries) == 0 || !strings.Contains(err.Error(), "TLS handshake timeout") {
			t.Errorf("want restic's retry message as the reason, got %v (retries %q)", err, retries)
		}
	} else if !strings.Contains(err.Error(), "restic gave no reason") {
		t.Errorf("got %v", err)
	}
}

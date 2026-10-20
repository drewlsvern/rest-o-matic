package execution

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// cacheDirTag is a valid CACHEDIR.TAG (https://bford.info/cachedir/):
// restic only honours one that starts with this signature.
const cacheDirTag = "Signature: 8a477f597d28d172789f06886806bc55\n"

// writeFiles creates each file under dir, with its parent directories.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// snapshotFiles returns the names of the regular files in a snapshot,
// sorted. Names, not paths, so it reads the same on Windows, where restic
// writes C:\x as /C/x.
func snapshotFiles(t *testing.T, repo config.Repository, id string) []string {
	t.Helper()
	cmd := exec.Command("restic", "-r", repo.URL, "ls", "--json", id)
	cmd.Env = testEnv(t, repo)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("restic ls: %v", err)
	}
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		var n struct{ Type, Name string }
		if json.Unmarshal(sc.Bytes(), &n) == nil && n.Type == "file" {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// snapshotParent returns the parent recorded in a snapshot.
func snapshotParent(t *testing.T, repo config.Repository, id string) string {
	t.Helper()
	cmd := exec.Command("restic", "-r", repo.URL, "snapshots", "--json", id)
	cmd.Env = testEnv(t, repo)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("restic snapshots: %v", err)
	}
	var snaps []struct{ Parent string }
	if err := json.Unmarshal(out, &snaps); err != nil || len(snaps) != 1 {
		t.Fatalf("parsing snapshots: %v: %s", err, out)
	}
	return snaps[0].Parent
}

func TestBackup_ExclusionsLeaveFilesOut(t *testing.T) {
	requireRestic(t)
	repo := initRepo(t)
	src := t.TempDir()
	writeFiles(t, src, map[string]string{
		"keep.txt":                    "k",
		"skip/skipped.txt":            "s",
		"slashed/slashed.txt":         "s",
		"a/node_modules/nm1.js":       "n",
		"a/b/c/node_modules/nm2.js":   "n",
		"cache/CACHEDIR.TAG":          cacheDirTag,
		"cache/cached.bin":            "c",
		"later/later.txt":             "l",
		"code/main.go":                "m",
		"untagged-cache/notcache.bin": "u",
	})
	source := config.Source{
		Paths: []string{src},
		Exclude: []string{
			filepath.Join(src, "skip"), // as ~ expansion writes it: backslashes on Windows
			filepath.ToSlash(filepath.Join(src, "slashed")),
			"node_modules",
		},
		ExcludeCaches: true,
	}
	r := NewResticRunner()

	first, err := r.Backup(context.Background(), repo, config.ReadDirect, source, []string{"docs"})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	// restic keeps the tag file itself and leaves out the rest of the directory.
	want := []string{"CACHEDIR.TAG", "keep.txt", "later.txt", "main.go", "notcache.bin"}
	if got := snapshotFiles(t, repo, first); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("on %s, snapshot holds %v, want %v", runtime.GOOS, got, want)
	}

	// Adding a pattern doesn't lose the parent snapshot.
	source.Exclude = append(source.Exclude, filepath.Join(src, "later"))
	second, err := r.Backup(context.Background(), repo, config.ReadDirect, source, []string{"docs"})
	if err != nil {
		t.Fatalf("second Backup: %v", err)
	}
	if got := snapshotFiles(t, repo, second); strings.Join(got, " ") != "CACHEDIR.TAG keep.txt main.go notcache.bin" {
		t.Fatalf("second snapshot holds %v", got)
	}
	if parent := snapshotParent(t, repo, second); parent == "" || !strings.HasPrefix(parent, first) {
		t.Fatalf("second snapshot's parent is %q, want %s", parent, first)
	}
}

// Package statedir guards the directory holding rest-o-matic's state and
// lock files against use by more than one user. Those files are created
// on first use with mode 0600 and never removed, so a single run as the
// wrong user (typically `sudo rest-o-matic run ...`) would otherwise leave
// files behind that make every later run as the usual user fail with a
// bare "permission denied".
package statedir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// Layout of the state directory. Shared with the commands that build
// these paths so the check always looks where the files really are.
const (
	LocksDir  = "locks"
	StateFile = "state.json"
)

// currentUID and ownerOf are the platform hooks (see statedir_unix.go and
// statedir_windows.go); variables so tests can fake users without root.
var (
	currentUID = platformCurrentUID
	ownerOf    = platformOwnerOf
)

// Mismatch is one path in the state directory not owned by the running user.
type Mismatch struct {
	Path string
	UID  int
}

// OwnerError reports every path in the state directory that the running
// user does not own.
type OwnerError struct {
	Dir        string
	UID        int // the user rest-o-matic is running as
	DirOwned   bool
	Mismatches []Mismatch
}

func (e *OwnerError) Error() string {
	var b strings.Builder
	running := userName(e.UID)
	fmt.Fprintf(&b, "state directory %s contains paths not owned by %s, the user rest-o-matic is running as:\n", e.Dir, running)
	for _, m := range e.Mismatches {
		fmt.Fprintf(&b, "  %s (owned by %s)\n", m.Path, userName(m.UID))
	}
	if e.DirOwned {
		fmt.Fprintf(&b, "these were left by a run as another user; remove them, or change their owner to %s", running)
	} else {
		// The directory itself is always the first mismatch when it has
		// the wrong owner.
		fmt.Fprintf(&b, "run rest-o-matic as %s, or pass --state-dir to use a separate state directory", userName(e.Mismatches[0].UID))
	}
	return b.String()
}

// Check returns an *OwnerError if the state directory, its locks
// directory, any lock file, or the state file exists and is owned by
// someone other than the running user. A state directory that doesn't
// exist yet passes, and on platforms without Unix ownership (Windows)
// Check always passes. It only reads metadata and never creates anything.
func Check(dir string) error {
	uid, ok := currentUID()
	if !ok {
		return nil
	}
	paths, err := candidates(dir)
	if err != nil {
		return err
	}
	var mismatches []Mismatch
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("checking state directory: %w", err)
		}
		if owner := ownerOf(info); owner != uid {
			mismatches = append(mismatches, Mismatch{Path: p, UID: owner})
		}
	}
	if len(mismatches) == 0 {
		return nil
	}
	return &OwnerError{Dir: dir, UID: uid, DirOwned: mismatches[0].Path != dir, Mismatches: mismatches}
}

// candidates lists the paths Check looks at, the state directory first.
// Anything else in the directory (temporary files, files the user put
// there) is ignored.
func candidates(dir string) ([]string, error) {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("checking state directory: %w", err)
	}
	paths := []string{dir}
	locks := filepath.Join(dir, LocksDir)
	if _, err := os.Stat(locks); err == nil {
		paths = append(paths, locks)
		// An unreadable locks directory is already reported through its
		// own owner, so its entries are only listed when they can be.
		if entries, err := os.ReadDir(locks); err == nil {
			for _, e := range entries {
				if e.Type().IsRegular() {
					paths = append(paths, filepath.Join(locks, e.Name()))
				}
			}
		}
	}
	return append(paths, filepath.Join(dir, StateFile)), nil
}

// userName resolves uid to a user name, falling back to the number when
// there is no passwd entry (e.g. inside a container).
func userName(uid int) string {
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		return u.Username
	}
	return strconv.Itoa(uid)
}

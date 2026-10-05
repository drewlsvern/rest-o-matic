//go:build !windows

package central

import (
	"fmt"
	"io/fs"
)

// checkPrivate refuses a file that anyone but its owner can read.
func checkPrivate(path string, info fs.FileInfo) error {
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return fmt.Errorf("enrolment %s has permissions %04o, which let other users read the host's credential; run: chmod 600 %s", path, mode, path)
	}
	return nil
}

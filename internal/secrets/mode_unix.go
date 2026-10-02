//go:build !windows

package secrets

import (
	"fmt"
	"io/fs"
)

// checkKeyMode refuses a host key that anyone other than its owner can
// read, as ssh does for private keys: a key that has quietly become
// readable by others no longer protects anything.
func checkKeyMode(path string, info fs.FileInfo) error {
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return fmt.Errorf("host key %s has permissions %04o, which let other users read it; run: chmod 600 %s", path, mode, path)
	}
	return nil
}

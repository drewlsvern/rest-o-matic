//go:build windows

package statedir

import "os"

// Windows has no Unix owner model, so the check is skipped there.
func platformCurrentUID() (int, bool) { return 0, false }

func platformOwnerOf(os.FileInfo) int { return 0 }

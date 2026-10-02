//go:build !windows

package statedir

import (
	"os"
	"syscall"
)

func platformCurrentUID() (int, bool) { return os.Geteuid(), true }

func platformOwnerOf(info os.FileInfo) int {
	return int(info.Sys().(*syscall.Stat_t).Uid)
}

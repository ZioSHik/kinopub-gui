//go:build !windows

package fsutil

import "syscall"

// syscallUmask sets the process umask and returns the previous value. Tests use
// it to make requested permission bits observable without umask interference.
func syscallUmask(mask int) int {
	return syscall.Umask(mask)
}

//go:build !windows

package main

import (
	"os"
	"syscall"
)

// reexec replaces the current process image with a fresh run of exe (used after
// an in-place update). On success it does not return.
func reexec(exe string, args []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}

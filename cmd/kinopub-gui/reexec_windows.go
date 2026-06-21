//go:build windows

package main

import (
	"os"
	"os/exec"
)

// reexec launches a fresh copy of exe and exits the current process. Windows
// has no exec(2), so the new process is started detached and the old one quits,
// having already released the listening port.
func reexec(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

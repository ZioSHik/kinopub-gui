//go:build windows

package gui

import "os"

// replaceExecutable installs newPath at exePath on Windows. A running .exe
// cannot be overwritten, but it CAN be renamed, so we move the running binary
// aside to "<exe>.old" and move the new one into place. The .old file stays
// locked until the process exits; cleanupOldExecutable removes it on the next
// launch.
func replaceExecutable(newPath, exePath string) error {
	old := exePath + ".old"
	_ = os.Remove(old) // remove a stale leftover, if any
	if err := os.Rename(exePath, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, exePath); err != nil {
		// Roll back so the app stays runnable.
		_ = os.Rename(old, exePath)
		return err
	}
	return nil
}

// cleanupOldExecutable best-effort removes the "<exe>.old" file left by a
// previous in-place update (it could not be deleted while that process ran).
func cleanupOldExecutable() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

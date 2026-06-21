//go:build !windows

package gui

import "os"

// replaceExecutable atomically moves newPath onto exePath. On Unix the running
// process keeps its open file handle to the old inode, so overwriting the path
// is safe and takes effect on the next exec.
func replaceExecutable(newPath, exePath string) error {
	return os.Rename(newPath, exePath)
}

// cleanupOldExecutable is a no-op on Unix (no leftover .old files are created).
func cleanupOldExecutable() {}

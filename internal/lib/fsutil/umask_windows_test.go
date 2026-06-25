//go:build windows

package fsutil

// syscallUmask is a no-op on Windows, which has no umask. The permission-mode
// tests that call it are skipped on Windows anyway.
func syscallUmask(mask int) int { return 0 }

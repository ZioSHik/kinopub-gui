//go:build !windows

package main

import "os"

// enableVT is a no-op on non-Windows platforms, where terminals understand ANSI
// escapes natively. The TTY check in useColor already gated this call.
func enableVT(_ *os.File) bool { return true }

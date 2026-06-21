//go:build !windows

package gui

import "context"

// openOnWindows is never called on non-Windows platforms (the runtime.GOOS
// switch in openInOS guards it); it exists only so the gui package compiles on
// every target.
func openOnWindows(_ context.Context, _ string, _ bool) error { return nil }

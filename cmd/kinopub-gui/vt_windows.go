//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on ENABLE_VIRTUAL_TERMINAL_PROCESSING for the console attached
// to f so that ANSI escape sequences render as colors on modern Windows
// terminals. It returns false if VT cannot be enabled (legacy cmd.exe), in which
// case the caller strips colors rather than printing raw escape codes.
func enableVT(f *os.File) bool {
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

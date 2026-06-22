//go:build (darwin && cgo) || windows || (linux && cgo)

package main

import (
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"fyne.io/systray"
)

// Tray icons embedded into the binary. PNG for the macOS menu bar / Linux
// app-indicator; ICO for the Windows notification area. Regenerate with
// `make icons` (scripts/gen-icons.sh).
//
//go:embed trayicon.png
var trayIconPNG []byte

//go:embed trayicon.ico
var trayIconICO []byte

func init() {
	// The tray/menu-bar host (Cocoa on macOS, GTK on Linux) must run on the
	// process's main OS thread; pin the main goroutine to it.
	runtime.LockOSThread()
}

// runApp lives in the menu bar (macOS) / notification area (Windows) /
// app-indicator (Linux) with an Open and a Quit item, instead of occupying the
// Dock or taskbar. The server's non-quit exit reasons — a fatal error, a
// self-update restart, or an interrupt signal — run on a background goroutine
// that exits the process directly. When no tray host is available (a terminal
// run, SSH, Termux) it falls back to the plain headless wait loop.
func runApp(srv *http.Server, ln net.Listener, errCh <-chan error, restartCh <-chan struct{}) int {
	if !useTray() {
		return runHeadless(srv, ln, errCh, restartCh)
	}
	url := "http://" + ln.Addr().String()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		select {
		case err := <-errCh:
			if err != nil && err != http.ErrServerClosed {
				fmt.Fprintf(os.Stderr, "kinopub-gui: server error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		case <-restartCh:
			os.Exit(performRestart(ln))
		case <-sigCh:
			gracefulShutdown(srv)
			os.Exit(0)
		}
	}()

	onReady := func() {
		if runtime.GOOS == "windows" {
			systray.SetIcon(trayIconICO)
		} else {
			systray.SetIcon(trayIconPNG)
		}
		systray.SetTitle("")
		systray.SetTooltip("kino.pub downloader")
		mOpen := systray.AddMenuItem("Open kino.pub", "Open the app in your browser")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Stop the server and quit")
		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					openBrowser(url)
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}

	// Blocks until systray.Quit(); onExit then stops the server gracefully.
	systray.Run(onReady, func() { gracefulShutdown(srv) })
	return 0
}

// useTray reports whether a tray/menu-bar host is available and wanted. On macOS
// only inside a .app bundle (a terminal run stays a plain Ctrl-C server); on
// Linux only within a desktop session; on Windows always.
func useTray() bool {
	switch runtime.GOOS {
	case "darwin":
		return insideAppBundle()
	case "linux":
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	default: // windows
		return true
	}
}

// insideAppBundle reports whether the executable lives in a macOS .app bundle.
func insideAppBundle() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return strings.Contains(exe, ".app/Contents/MacOS/")
}

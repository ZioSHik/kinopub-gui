//go:build windows

package gui

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// openOnWindows opens (reveal=false) or reveals (reveal=true) a path on Windows.
//
// It sets the raw command line explicitly via SysProcAttr.CmdLine so that paths
// containing spaces or cmd metacharacters (& ^ % ( ) !) are handled literally,
// rather than relying on Go's argv escaping which does not quote a bare "&" and
// would let cmd.exe mis-parse such a path. explorer.exe returns a non-zero exit
// code even on success, so the exit status is ignored.
func openOnWindows(ctx context.Context, path string, reveal bool) error {
	p := filepath.FromSlash(filepath.Clean(path))
	q := strings.ReplaceAll(p, `"`, `""`)

	var cmd *exec.Cmd
	if reveal {
		cmd = exec.CommandContext(ctx, "explorer.exe")
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + q + `"`}
	} else {
		cmd = exec.CommandContext(ctx, "cmd.exe")
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /c start "" "` + q + `"`}
	}
	_ = cmd.Run()
	return nil
}

//go:build !((darwin && cgo) || windows || (linux && cgo))

package main

import (
	"net"
	"net/http"
)

// runApp on platforms without a tray host — Android/Termux, and CGO-disabled
// darwin/linux builds — just waits headlessly (Ctrl-C to stop).
func runApp(srv *http.Server, ln net.Listener, errCh <-chan error, restartCh <-chan struct{}) int {
	return runHeadless(srv, ln, errCh, restartCh)
}

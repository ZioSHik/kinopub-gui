//go:build !android

package httpx

import (
	"net"
	"time"
)

// NewDialer is the exported alias used by other packages.
func NewDialer() *net.Dialer { return newDialer() }

func newDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
}

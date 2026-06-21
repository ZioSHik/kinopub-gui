//go:build darwin && !cgo

package browsercookies

import "fmt"

// darwinSafeStoragePassword is unavailable without CGO (the macOS Security
// framework needs cgo). Cross-compiled (CGO-off) macOS builds fall back to a
// clear error pointing the user at manual cookie paste.
func darwinSafeStoragePassword(service, account string) ([]byte, error) {
	return nil, fmt.Errorf("reading the macOS keychain requires a CGO build (this binary was built with CGO disabled) — paste the Cookie header manually or rebuild with CGO_ENABLED=1")
}

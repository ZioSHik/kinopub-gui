//go:build darwin && cgo

package browsercookies

import (
	"fmt"

	keychain "github.com/keybase/go-keychain"
)

// darwinSafeStoragePassword reads a generic-password keychain item. macOS will
// prompt for access the first time; the user must click Allow.
func darwinSafeStoragePassword(service, account string) ([]byte, error) {
	pw, err := keychain.GetGenericPassword(service, account, "", "")
	if err != nil {
		return nil, fmt.Errorf("reading the %q keychain entry failed: %w (allow access when macOS prompts, or paste the Cookie header manually)", service, err)
	}
	if len(pw) == 0 {
		return nil, fmt.Errorf("the %q keychain entry is empty — is Yandex Browser installed and have you opened it at least once?", service)
	}
	return pw, nil
}

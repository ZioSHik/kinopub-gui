//go:build linux

package browsercookies

import (
	"os"
	"path/filepath"
)

func yandexCookiePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return chromiumProfileCookiePaths([]string{
		filepath.Join(home, ".config", "yandex-browser"),
		filepath.Join(home, ".config", "yandex-browser-beta"),
	})
}

// yandexSafeStoragePassword is best-effort on Linux: when no desktop keyring is
// in use, Chromium encrypts with the fixed "peanuts" password (v10, 1 PBKDF2
// iteration). Keyring-encrypted (v11) cookies won't decrypt and are skipped.
func yandexSafeStoragePassword() ([]byte, int, error) {
	return []byte("peanuts"), 1, nil
}

//go:build darwin

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
		filepath.Join(home, "Library", "Application Support", "Yandex", "YandexBrowser"),
	})
}

// yandexSafeStoragePassword reads the AES password from the macOS keychain entry
// Yandex creates ("Yandex Safe Storage"). Chromium on macOS derives the key with
// PBKDF2-SHA1 over 1003 iterations.
func yandexSafeStoragePassword() ([]byte, int, error) {
	pw, err := darwinSafeStoragePassword("Yandex Safe Storage", "Yandex")
	if err != nil {
		return nil, 0, err
	}
	return pw, 1003, nil
}

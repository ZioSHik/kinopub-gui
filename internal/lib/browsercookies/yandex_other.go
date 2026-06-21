//go:build !darwin && !linux

package browsercookies

import "fmt"

func yandexCookiePaths() []string { return nil }

func yandexSafeStoragePassword() ([]byte, int, error) {
	return nil, 0, fmt.Errorf("Yandex Browser cookie import is only supported on macOS and Linux — paste the Cookie header manually")
}

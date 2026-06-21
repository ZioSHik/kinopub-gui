//go:build linux

package credstore

import (
	"fmt"
	"os"
	"strings"
)

// machineSeed returns a machine-specific identifier on Linux.
// It tries (in order):
//  1. /etc/machine-id (systemd, present on most distros)
//  2. $PREFIX/etc/machine-id (Termux on Android)
//  3. /proc/sys/kernel/random/boot_id (fallback, changes on reboot but
//     combined with a stable path it's still useful)
func machineSeed() ([]byte, error) {
	// Standard systemd machine-id.
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) > 0 {
			return []byte(id), nil
		}
	}

	// Termux: $PREFIX/etc/machine-id
	if prefix := os.Getenv("PREFIX"); prefix != "" {
		path := prefix + "/etc/machine-id"
		if data, err := os.ReadFile(path); err == nil {
			id := strings.TrimSpace(string(data))
			if len(id) > 0 {
				return []byte(id), nil
			}
		}
	}

	// Fallback: /var/lib/dbus/machine-id (older systems without systemd).
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) > 0 {
			return []byte(id), nil
		}
	}

	// Last resort: boot_id (unique per boot, but better than nothing).
	if data, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) > 0 {
			return []byte(id), nil
		}
	}

	return nil, fmt.Errorf("no machine identifier found: tried /etc/machine-id, $PREFIX/etc/machine-id, /var/lib/dbus/machine-id, /proc/sys/kernel/random/boot_id")
}

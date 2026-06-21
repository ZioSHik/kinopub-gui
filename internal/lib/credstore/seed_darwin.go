//go:build darwin

package credstore

import (
	"fmt"
	"os/exec"
	"strings"
)

// machineSeed returns the macOS hardware UUID as the machine-specific secret.
// This UUID is unique per Mac and persists across OS reinstalls.
func machineSeed() ([]byte, error) {
	out, err := exec.Command(
		"ioreg", "-rd1", "-c", "IOPlatformExpertDevice",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("ioreg failed: %w", err)
	}

	// Parse the IOPlatformUUID from the output.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			// Line looks like: "IOPlatformUUID" = "XXXXXXXX-XXXX-..."
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				uuid := strings.TrimSpace(parts[1])
				uuid = strings.Trim(uuid, `"`)
				if len(uuid) > 0 {
					return []byte(uuid), nil
				}
			}
		}
	}

	return nil, fmt.Errorf("IOPlatformUUID not found in ioreg output")
}

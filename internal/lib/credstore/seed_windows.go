//go:build windows

package credstore

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// machineSeed returns a machine-specific identifier on Windows.
//
// It tries, in order:
//  1. The registry value HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid —
//     a stable per-install identifier present on every supported Windows, read
//     in-process (no external program), so it is unaffected by the removal of
//     wmic on Windows 11 24H2 / Server 2025 and later.
//  2. PowerShell's CIM query for the SMBIOS UUID (works without wmic).
//  3. The legacy `wmic csproduct get UUID` call, kept only as a last resort so
//     that credentials encrypted under the old seed on older machines remain
//     decryptable.
func machineSeed() ([]byte, error) {
	// 1. Registry MachineGuid.
	if k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`,
		registry.QUERY_VALUE|registry.WOW64_64KEY,
	); err == nil {
		guid, _, gErr := k.GetStringValue("MachineGuid")
		k.Close()
		if gErr == nil {
			if guid = strings.TrimSpace(guid); guid != "" {
				return []byte(guid), nil
			}
		}
	}

	// 2. PowerShell CIM (Win32_ComputerSystemProduct.UUID).
	if out, err := exec.Command(
		"powershell", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-CimInstance -ClassName Win32_ComputerSystemProduct).UUID",
	).Output(); err == nil {
		if id := strings.TrimSpace(string(out)); id != "" {
			return []byte(id), nil
		}
	}

	// 3. Legacy wmic (removed on current Windows, but kept for back-compat).
	if out, err := exec.Command("wmic", "csproduct", "get", "UUID").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line = strings.TrimSpace(line); line != "" && line != "UUID" {
				return []byte(line), nil
			}
		}
	}

	return nil, fmt.Errorf("could not determine a machine identifier " +
		"(tried registry MachineGuid, Win32_ComputerSystemProduct, wmic)")
}

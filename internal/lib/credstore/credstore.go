// Package credstore provides encrypted storage for authentication credentials.
//
// Credentials (Cookie header, User-Agent) are encrypted with AES-256-GCM using
// a key derived from a machine-specific secret. The encrypted file is stored at
// ~/.config/kinopub/credentials.enc
//
// Key derivation strategy (platform-dependent):
//   - macOS: uses the hardware UUID from IOPlatformExpertDevice (unique per Mac,
//     survives OS reinstalls, not exposed to other machines).
//   - Linux: uses /etc/machine-id (systemd machine identifier, unique per install).
//   - Termux/Android: uses $PREFIX/etc/machine-id or falls back to
//     /proc/sys/kernel/random/boot_id combined with the Android ID.
//
// The key is derived via PBKDF2-SHA256 with a fixed salt (the salt is not secret
// — the security comes from the machine-specific seed being unavailable on other
// devices). This means copying the .enc file to another machine won't help an
// attacker unless they also know the source machine's hardware UUID / machine-id.
package credstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

// rmwMu serializes read-modify-write cycles on the single encrypted credential
// file, so concurrent callers (a background token refresh, a logout, a settings
// save) cannot clobber each other's changes by interleaving Load → modify → Save.
var rmwMu sync.Mutex

// Update atomically applies fn to the stored credentials under rmwMu: it loads
// the current credentials (treating a missing/zero file as empty), lets fn mutate
// them, and persists the result. It is the safe way to change a subset of fields
// without racing other writers of the same file.
func Update(fn func(*Credentials)) error {
	rmwMu.Lock()
	defer rmwMu.Unlock()
	creds, _ := Load()
	fn(&creds)
	return Save(creds)
}

// Credentials holds the authentication data persisted between runs.
type Credentials struct {
	Cookie    string `json:"cookie"`
	UserAgent string `json:"user_agent"`

	// Official kino.pub API (device-code OAuth) token set. Stored alongside the
	// cookie so both auth paths share one encrypted file. APIExpiry is unix
	// seconds; zero means unknown.
	APIAccessToken  string `json:"api_access_token,omitempty"`
	APIRefreshToken string `json:"api_refresh_token,omitempty"`
	APIExpiry       int64  `json:"api_expiry,omitempty"`
}

// IsEmpty reports whether the cookie-based credentials carry no useful data.
// (API tokens are tracked separately via HasAPIToken.)
func (c Credentials) IsEmpty() bool {
	return c.Cookie == "" && c.UserAgent == ""
}

// HasAPIToken reports whether an official-API token set is stored.
func (c Credentials) HasAPIToken() bool {
	return c.APIAccessToken != "" || c.APIRefreshToken != ""
}

// credDir returns the directory where the credential file is stored. It honors
// XDG_CONFIG_HOME first (matching the GUI's settings store, so credentials and
// gui.json stay co-located), then falls back to ~/.config/kinopub.
func credDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "kinopub"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "kinopub"), nil
}

// credPath returns the full path to the encrypted credential file.
func credPath() (string, error) {
	dir, err := credDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.enc"), nil
}

// pbkdf2Salt is a fixed application-specific salt. The security does not depend
// on this being secret — it prevents rainbow-table attacks against the
// machine-specific seed.
var pbkdf2Salt = []byte("kinopub-credstore-v1-salt-2024")

// deriveKey produces a 32-byte AES key from the machine-specific seed.
func deriveKey(seed []byte) []byte {
	return pbkdf2.Key(seed, pbkdf2Salt, 100_000, 32, sha256.New)
}

// Save encrypts and persists the given credentials.
func Save(creds Credentials) error {
	seed, err := machineSeed()
	if err != nil {
		return fmt.Errorf("machine seed: %w", err)
	}

	plaintext, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	key := deriveKey(seed)
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	dir, err := credDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	path, err := credPath()
	if err != nil {
		return err
	}

	// Write with restrictive permissions (owner-only read/write).
	if err := os.WriteFile(path, ciphertext, 0600); err != nil {
		return fmt.Errorf("write credential file: %w", err)
	}

	return nil
}

// Load decrypts and returns the stored credentials.
// Returns empty Credentials (not an error) if the file does not exist.
func Load() (Credentials, error) {
	path, err := credPath()
	if err != nil {
		return Credentials{}, err
	}

	ciphertext, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{}, nil
		}
		return Credentials{}, fmt.Errorf("read credential file: %w", err)
	}

	seed, err := machineSeed()
	if err != nil {
		return Credentials{}, fmt.Errorf("machine seed: %w", err)
	}

	key := deriveKey(seed)
	block, err := aes.NewCipher(key)
	if err != nil {
		return Credentials{}, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Credentials{}, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return Credentials{}, fmt.Errorf("credential file is corrupted (too short)")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return Credentials{}, fmt.Errorf("decrypt credentials failed (wrong machine or corrupted file): %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials: %w", err)
	}

	return creds, nil
}

// Clear removes the stored credential file.
func Clear() error {
	path, err := credPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove credential file: %w", err)
	}
	return nil
}

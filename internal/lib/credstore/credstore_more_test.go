package credstore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// --- Credentials predicate helpers ---------------------------------------

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name string
		c    Credentials
		want bool
	}{
		{"zero value", Credentials{}, true},
		{"cookie only", Credentials{Cookie: "x"}, false},
		{"user-agent only", Credentials{UserAgent: "ua"}, false},
		{"both cookie+ua", Credentials{Cookie: "c", UserAgent: "u"}, false},
		// API tokens are tracked separately, so a creds set with only an API
		// token is still "empty" for the cookie-based view.
		{"api token only", Credentials{APIAccessToken: "tok"}, true},
		{"api refresh only", Credentials{APIRefreshToken: "ref"}, true},
		{"api expiry only", Credentials{APIExpiry: 1234}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsEmpty(); got != tc.want {
				t.Fatalf("IsEmpty() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasAPIToken(t *testing.T) {
	tests := []struct {
		name string
		c    Credentials
		want bool
	}{
		{"zero value", Credentials{}, false},
		{"access token", Credentials{APIAccessToken: "a"}, true},
		{"refresh token", Credentials{APIRefreshToken: "r"}, true},
		{"both tokens", Credentials{APIAccessToken: "a", APIRefreshToken: "r"}, true},
		// Expiry alone is not a token.
		{"expiry only", Credentials{APIExpiry: 99}, false},
		// Cookie-only is not an API token.
		{"cookie only", Credentials{Cookie: "c"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.HasAPIToken(); got != tc.want {
				t.Fatalf("HasAPIToken() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- Round-trip with API token fields ------------------------------------

func TestSaveLoadRoundTripWithAPITokens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	want := Credentials{
		Cookie:          "cf_clearance=abc; sessionid=xyz",
		UserAgent:       "Mozilla/5.0 (api)",
		APIAccessToken:  "access-123",
		APIRefreshToken: "refresh-456",
		APIExpiry:       1750000000,
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
	if !got.HasAPIToken() {
		t.Fatal("expected HasAPIToken() true after round-trip")
	}
}

// Unicode and large values must survive the encrypt/decrypt round-trip
// intact (no truncation, no mangling).
func TestSaveLoadUnicodeAndLarge(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	big := strings.Repeat("Ω≈ç√∫˜µ≤≥÷-", 5000) // multi-byte, ~110KB
	want := Credentials{
		Cookie:    "ключ=значение; 日本語=テスト; emoji=😀🎬",
		UserAgent: big,
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("unicode/large round-trip mismatch (cookie=%q, ua len got=%d want=%d)",
			got.Cookie, len(got.UserAgent), len(want.UserAgent))
	}
}

// Saving over an existing file must overwrite it cleanly.
func TestSaveOverwrites(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save(Credentials{Cookie: "first", UserAgent: "ua1"}); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second := Credentials{Cookie: "second", UserAgent: "ua2", APIAccessToken: "tok"}
	if err := Save(second); err != nil {
		t.Fatalf("Save second: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != second {
		t.Fatalf("expected overwrite to %+v, got %+v", second, got)
	}
}

// Two successive Saves of the same plaintext must produce different ciphertext
// (random nonce), guarding against accidental nonce reuse.
func TestSaveUsesFreshNonce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	creds := Credentials{Cookie: "same", UserAgent: "same-ua"}

	if err := Save(creds); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	p, _ := credPath()
	first, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read after save 1: %v", err)
	}
	if err := Save(creds); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	second, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read after save 2: %v", err)
	}
	if string(first) == string(second) {
		t.Fatal("expected different ciphertext across saves (nonce should be random)")
	}
	// But both must still decrypt to the same value.
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != creds {
		t.Fatalf("Load after re-save mismatch: got %+v want %+v", got, creds)
	}
}

// The encrypted file must not contain the secret values in plaintext.
func TestSaveCiphertextIsOpaque(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	creds := Credentials{Cookie: "SUPERSECRETCOOKIE", UserAgent: "SECRETUA"}
	if err := Save(creds); err != nil {
		t.Fatalf("Save: %v", err)
	}
	p, _ := credPath()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "SUPERSECRETCOOKIE") || strings.Contains(string(raw), "SECRETUA") {
		t.Fatal("plaintext secret leaked into the encrypted file")
	}
}

// The credential file must be written with owner-only permissions.
func TestSavePermissions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save(Credentials{Cookie: "c"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	p, _ := credPath()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("file perm = %o, want 0600", perm)
	}
}

// --- Corruption / error paths in Load ------------------------------------

// A file shorter than the GCM nonce must report a "too short" corruption error,
// not a panic or a slice out-of-range.
func TestLoadTooShortFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p, _ := credPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write fewer bytes than a GCM nonce (12 bytes).
	if err := os.WriteFile(p, []byte{1, 2, 3}, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for too-short file")
	}
	if !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("expected corruption error, got %v", err)
	}
}

// A file with a valid-length but garbage body must fail GCM authentication.
func TestLoadCorruptedGarbage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p, _ := credPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 64 bytes of zeros: long enough to pass the nonce-length check but
	// will fail GCM authentication.
	if err := os.WriteFile(p, make([]byte, 64), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load()
	if err == nil {
		t.Fatal("expected decryption error for garbage ciphertext")
	}
	if !strings.Contains(err.Error(), "decrypt") {
		t.Fatalf("expected decrypt error, got %v", err)
	}
}

// Tampering with a single ciphertext byte must be detected by GCM's auth tag.
func TestLoadTamperDetected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save(Credentials{Cookie: "c", UserAgent: "u"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	p, _ := credPath()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Flip the last byte (part of the GCM tag / ciphertext).
	raw[len(raw)-1] ^= 0xFF
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatalf("write tampered: %v", err)
	}
	_, err = Load()
	if err == nil {
		t.Fatal("expected GCM auth failure on tampered ciphertext")
	}
}

// An empty file (0 bytes) is shorter than the nonce and must be reported as
// corrupted rather than silently treated as empty credentials.
func TestLoadEmptyFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p, _ := credPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, nil, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for empty (0-byte) file")
	}
}

// --- Update read-modify-write -------------------------------------------

func TestUpdateAppliesMutation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Seed an initial value.
	if err := Save(Credentials{Cookie: "orig", UserAgent: "ua"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	err := Update(func(c *Credentials) {
		c.APIAccessToken = "newtoken"
		c.APIExpiry = 42
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Credentials{Cookie: "orig", UserAgent: "ua", APIAccessToken: "newtoken", APIExpiry: 42}
	if got != want {
		t.Fatalf("Update result mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// Update on a fresh store (no file) must treat the current creds as empty and
// persist whatever fn sets.
func TestUpdateFromEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var sawEmpty bool
	err := Update(func(c *Credentials) {
		sawEmpty = c.IsEmpty()
		c.Cookie = "fromempty"
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !sawEmpty {
		t.Fatal("expected fn to observe empty credentials when no file exists")
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Cookie != "fromempty" {
		t.Fatalf("expected cookie 'fromempty', got %q", got.Cookie)
	}
}

// Concurrent Updates must serialize under rmwMu so no increment is lost.
func TestUpdateConcurrentSerialization(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = Update(func(c *Credentials) {
				c.APIExpiry++
			})
		}()
	}
	wg.Wait()

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.APIExpiry != n {
		t.Fatalf("expected APIExpiry=%d after %d concurrent increments (lost update / race), got %d", n, n, got.APIExpiry)
	}
}

// --- Path resolution -----------------------------------------------------

func TestCredDirHonorsXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	dir, err := credDir()
	if err != nil {
		t.Fatalf("credDir: %v", err)
	}
	want := filepath.Join(xdg, "kinopub")
	if dir != want {
		t.Fatalf("credDir = %q, want %q", dir, want)
	}
}

func TestCredDirHomeFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := credDir()
	if err != nil {
		t.Fatalf("credDir: %v", err)
	}
	want := filepath.Join(home, ".config", "kinopub")
	if dir != want {
		t.Fatalf("credDir fallback = %q, want %q", dir, want)
	}
}

func TestCredPathFile(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	p, err := credPath()
	if err != nil {
		t.Fatalf("credPath: %v", err)
	}
	want := filepath.Join(xdg, "kinopub", "credentials.enc")
	if p != want {
		t.Fatalf("credPath = %q, want %q", p, want)
	}
}

// --- Key derivation ------------------------------------------------------

// deriveKey must be deterministic for a given seed, produce a 32-byte key,
// and differ for differing seeds.
func TestDeriveKey(t *testing.T) {
	k1 := deriveKey([]byte("seed-A"))
	k1b := deriveKey([]byte("seed-A"))
	k2 := deriveKey([]byte("seed-B"))

	if len(k1) != 32 {
		t.Fatalf("key length = %d, want 32", len(k1))
	}
	if string(k1) != string(k1b) {
		t.Fatal("deriveKey not deterministic for the same seed")
	}
	if string(k1) == string(k2) {
		t.Fatal("deriveKey produced identical keys for different seeds")
	}
}

// --- Clear edge cases ----------------------------------------------------

// Clear when the file was never created must be a no-op (no error).
func TestClearNoFileNoError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Clear(); err != nil {
		t.Fatalf("Clear with no file: %v", err)
	}
}

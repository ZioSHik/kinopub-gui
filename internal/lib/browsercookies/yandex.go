// Yandex Browser support. kooky has no Yandex finder, and on macOS Yandex
// encrypts its Chromium cookie store under a "Yandex Safe Storage" keychain
// entry that kooky's public API can't target. So we read the cookie DB directly
// (same pure-Go SQLite reader kooky uses) and decrypt with the Chromium scheme
// using a platform-supplied safe-storage password.
package browsercookies

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-sqlite/sqlite3"
	"golang.org/x/crypto/pbkdf2"
)

// BrowserYandex is the identifier for Yandex Browser.
const BrowserYandex = "yandex"

const (
	chromeSalt   = "saltysalt"
	chromeIV     = "                " // 16 spaces
	chromeKeyLen = 16
)

// loadYandex reads cookies for domainSuffix from Yandex Browser's cookie store
// and returns a Cookie header value.
func loadYandex(domainSuffix string) (string, error) {
	suffix := strings.ToLower(strings.TrimPrefix(domainSuffix, "."))

	paths := yandexCookiePaths()
	if len(paths) == 0 {
		return "", fmt.Errorf("Yandex Browser cookie store is not supported on this platform — paste the Cookie header manually")
	}

	password, iterations, err := yandexSafeStoragePassword()
	if err != nil {
		return "", err
	}
	key := pbkdf2.Key(password, []byte(chromeSalt), iterations, chromeKeyLen, sha1.New)

	type entry struct {
		value   string
		expires int64
	}
	collected := make(map[string]entry)

	var (
		anyFile bool
		readErr string
	)
	for _, p := range paths {
		if fi, statErr := os.Stat(p); statErr != nil || fi.IsDir() {
			continue
		}
		anyFile = true
		if rErr := readYandexDB(p, suffix, key, func(name, value string, expires int64) {
			if prev, ok := collected[name]; !ok || expires > prev.expires {
				collected[name] = entry{value: value, expires: expires}
			}
		}); rErr != nil {
			readErr = rErr.Error()
		}
	}

	if !anyFile {
		return "", fmt.Errorf("Yandex Browser cookie store not found — is Yandex Browser installed and logged in to kino.pub?")
	}
	if len(collected) == 0 {
		if readErr != "" {
			return "", fmt.Errorf("could not read Yandex Browser cookies: %s. Try pasting the Cookie header manually", readErr)
		}
		return "", fmt.Errorf("no cookies for %q found in Yandex Browser — log in there first, or paste the Cookie header manually", suffix)
	}

	names := make([]string, 0, len(collected))
	for n := range collected {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(n)
		b.WriteByte('=')
		b.WriteString(collected[n].value)
	}
	return b.String(), nil
}

// readYandexDB opens a Chromium cookie SQLite file (copied to a temp path so a
// running browser's lock doesn't block us) and yields decrypted cookies whose
// domain matches suffix.
func readYandexDB(path, suffix string, key []byte, yield func(name, value string, expires int64)) error {
	tmp, cleanup, err := copyToTemp(path)
	if err != nil {
		return err
	}
	defer cleanup()

	db, err := sqlite3.Open(tmp)
	if err != nil {
		return err
	}
	defer db.Close()

	dbVersion := chromeDBVersion(db)

	cols, ok := tableColumns(db, "cookies")
	if !ok {
		return fmt.Errorf("no cookies table")
	}
	iHost, okH := cols["host_key"]
	iName, okN := cols["name"]
	iEnc, okE := cols["encrypted_value"]
	iVal := cols["value"]
	iExp := cols["expires_utc"]
	if !okH || !okN || !okE {
		return fmt.Errorf("unexpected cookies schema")
	}

	return db.VisitTableRecords("cookies", func(_ *int64, rec sqlite3.Record) error {
		vals := rec.Values
		host := strings.ToLower(strings.TrimPrefix(asString(at(vals, iHost)), "."))
		if host != suffix && !strings.HasSuffix(host, "."+suffix) {
			return nil
		}
		name := asString(at(vals, iName))
		if name == "" {
			return nil
		}

		var value string
		enc := asBytes(at(vals, iEnc))
		if len(enc) > 0 {
			dec, derr := chromeDecrypt(enc, key, dbVersion)
			if derr != nil {
				return nil // skip undecryptable cookie
			}
			value = string(dec)
		} else {
			value = asString(at(vals, iVal))
		}

		yield(name, value, asInt64(at(vals, iExp)))
		return nil
	})
}

// chromeDecrypt decrypts a Chromium cookie value (AES-128-CBC, v10/v11 prefix).
// Mirrors kooky's decryptAESCBC, including the 32-byte plaintext prefix added in
// newer Chromium (schema version >= 24).
func chromeDecrypt(encrypted, key []byte, dbVersion int64) ([]byte, error) {
	if len(encrypted) <= 3 {
		return nil, fmt.Errorf("encrypted value too short")
	}
	// AES-256-GCM (v10 GCM, used on Linux/Windows) is not handled here.
	if !bytes.HasPrefix(encrypted, []byte("v10")) && !bytes.HasPrefix(encrypted, []byte("v11")) {
		return nil, fmt.Errorf("unsupported cookie encryption scheme")
	}
	ct := encrypted[3:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ct)%block.BlockSize() != 0 || len(ct) == 0 {
		return nil, fmt.Errorf("cipher input not full blocks")
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, []byte(chromeIV)).CryptBlocks(out, ct)

	// PKCS#7 unpad.
	pad := int(out[len(out)-1])
	if pad < 1 || pad > chromeKeyLen || pad > len(out) {
		return nil, fmt.Errorf("invalid padding")
	}
	out = out[:len(out)-pad]

	// Newer Chromium prepends a 32-byte SHA-256(domain) to the plaintext.
	if dbVersion >= 24 {
		if len(out) < 32 {
			return nil, fmt.Errorf("plaintext too short for prefix")
		}
		out = out[32:]
	}
	return out, nil
}

// chromeDBVersion reads the schema version from the meta table (0 if absent).
func chromeDBVersion(db *sqlite3.DbFile) int64 {
	cols, ok := tableColumns(db, "meta")
	if !ok {
		return 0
	}
	iKey, okK := cols["key"]
	iVal, okV := cols["value"]
	if !okK || !okV {
		return 0
	}
	var version int64
	_ = db.VisitTableRecords("meta", func(_ *int64, rec sqlite3.Record) error {
		if asString(at(rec.Values, iKey)) == "version" {
			version, _ = strconv.ParseInt(asString(at(rec.Values, iVal)), 10, 64)
		}
		return nil
	})
	return version
}

// tableColumns returns a column-name → index map for the named table.
func tableColumns(db *sqlite3.DbFile, name string) (map[string]int, bool) {
	for _, t := range db.Tables() {
		if t.Name() != name {
			continue
		}
		m := make(map[string]int)
		for i, c := range t.Columns() {
			if _, ok := m[c.Name()]; !ok {
				m[c.Name()] = i
			}
		}
		return m, true
	}
	return nil, false
}

func at(vals []interface{}, i int) interface{} {
	if i < 0 || i >= len(vals) {
		return nil
	}
	return vals[i]
}

func asString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	default:
		return ""
	}
}

func asBytes(v interface{}) []byte {
	switch x := v.(type) {
	case []byte:
		return x
	case string:
		return []byte(x)
	default:
		return nil
	}
}

func asInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case uint64:
		return int64(x)
	default:
		return 0
	}
}

// chromiumProfileCookiePaths returns candidate Cookies DB paths for every
// profile found under each Chromium-style user-data base directory. Newer
// Chromium keeps cookies in "<Profile>/Network/Cookies"; older in "<Profile>/Cookies".
func chromiumProfileCookiePaths(bases []string) []string {
	var out []string
	for _, base := range bases {
		profiles := map[string]bool{}
		if entries, err := os.ReadDir(base); err == nil {
			for _, e := range entries {
				if e.IsDir() && (e.Name() == "Default" || strings.HasPrefix(e.Name(), "Profile")) {
					profiles[e.Name()] = true
				}
			}
		}
		if len(profiles) == 0 {
			profiles["Default"] = true
		}
		for p := range profiles {
			out = append(out,
				filepath.Join(base, p, "Network", "Cookies"),
				filepath.Join(base, p, "Cookies"),
			)
		}
	}
	return out
}

// copyToTemp copies src to a temporary file and returns its path plus a cleanup
// func. Chromium keeps the live DB open; reading a copy avoids lock contention.
func copyToTemp(src string) (string, func(), error) {
	in, err := os.Open(src)
	if err != nil {
		return "", func() {}, err
	}
	defer in.Close()

	tmp, err := os.CreateTemp("", "kinopub-yandex-*.sqlite")
	if err != nil {
		return "", func() {}, err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", func() {}, err
	}
	tmp.Close()
	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

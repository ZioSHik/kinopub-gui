package gui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// updateRepo is the GitHub repository whose releases this binary updates from.
const updateRepo = "ZioSHik/kinopub-gui"

// UpdateStatus describes the result of an update check.
type UpdateStatus struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	Notes           string `json:"notes,omitempty"`
	AssetName       string `json:"assetName,omitempty"`
	Supported       bool   `json:"supported"` // false for dev builds / no matching asset
	Note            string `json:"note,omitempty"`
}

// ghRelease is the subset of the GitHub release API we use.
type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// assetName returns the release asset name for the running platform, e.g.
// "kinopub-gui-darwin-arm64" or "kinopub-gui-windows-amd64.exe".
func assetName() string {
	name := fmt.Sprintf("kinopub-gui-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// updateChecker fetches and briefly caches the latest release.
type updateChecker struct {
	current string
	mu      sync.Mutex
	cached  *UpdateStatus
	at      time.Time
	applyMu sync.Mutex // serializes apply, so two clicks can't race the replace
}

func newUpdateChecker(current string) *updateChecker {
	return &updateChecker{current: current}
}

func (u *updateChecker) httpClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second}
}

// isDevBuild reports whether the current version is not a real release tag, in
// which case updating is disabled (nothing sensible to compare or replace).
func (u *updateChecker) isDevBuild() bool {
	_, _, _, ok := parseVersion(u.current)
	return !ok
}

// status returns the cached update status, refreshing it when stale or forced.
func (u *updateChecker) status(ctx context.Context, force bool) UpdateStatus {
	u.mu.Lock()
	if !force && u.cached != nil && time.Since(u.at) < 10*time.Minute {
		st := *u.cached
		u.mu.Unlock()
		return st
	}
	u.mu.Unlock()

	st := u.fetch(ctx)

	u.mu.Lock()
	u.cached = &st
	u.at = time.Now()
	u.mu.Unlock()
	return st
}

func (u *updateChecker) fetch(ctx context.Context) UpdateStatus {
	st := UpdateStatus{Current: u.current, AssetName: assetName()}
	if u.isDevBuild() {
		st.Note = "development build — updates are only available for released versions"
		return st
	}

	rel, err := u.latestRelease(ctx)
	if err != nil {
		st.Note = "could not check for updates: " + err.Error()
		return st
	}
	st.Latest = rel.TagName
	st.ReleaseURL = rel.HTMLURL
	st.Notes = rel.Body

	// Find the asset for this platform.
	want := assetName()
	for _, a := range rel.Assets {
		if a.Name == want {
			st.Supported = true
			break
		}
	}
	if !st.Supported {
		st.Note = fmt.Sprintf("no %s asset in the latest release", want)
		return st
	}
	st.UpdateAvailable = isNewer(rel.TagName, u.current)
	return st
}

func (u *updateChecker) latestRelease(ctx context.Context) (*ghRelease, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kinopub-gui")
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// apply downloads the platform asset for the latest release, verifies its
// SHA-256 against the release checksums.txt (when present), and atomically
// replaces the running executable. It returns the version installed.
func (u *updateChecker) apply(ctx context.Context) (string, error) {
	if !u.applyMu.TryLock() {
		return "", fmt.Errorf("an update is already in progress")
	}
	defer u.applyMu.Unlock()
	if u.isDevBuild() {
		return "", fmt.Errorf("updates are only available for released builds")
	}
	rel, err := u.latestRelease(ctx)
	if err != nil {
		return "", err
	}
	if !isNewer(rel.TagName, u.current) {
		return "", fmt.Errorf("already up to date (%s)", u.current)
	}

	want := assetName()
	var assetURL string
	var assetSize int64
	checksumsURL := ""
	for _, a := range rel.Assets {
		switch a.Name {
		case want:
			assetURL = a.BrowserDownloadURL
			assetSize = a.Size
		case "checksums.txt":
			checksumsURL = a.BrowserDownloadURL
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("no %s asset in release %s", want, rel.TagName)
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	exePath, _ = filepath.EvalSymlinks(exePath)

	// Download to a temp file alongside the executable (same filesystem so the
	// replace is an atomic rename).
	tmp, err := os.CreateTemp(filepath.Dir(exePath), ".kinopub-gui-update-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op once renamed into place
	}()

	sum, err := downloadTo(ctx, u.httpClient(), assetURL, tmp)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if assetSize > 0 {
		if fi, statErr := os.Stat(tmpName); statErr == nil && fi.Size() != assetSize {
			return "", fmt.Errorf("download size mismatch (got %d, want %d)", fi.Size(), assetSize)
		}
	}

	// Verify against checksums.txt when the release provides one.
	if checksumsURL != "" {
		want, cerr := fetchChecksum(ctx, u.httpClient(), checksumsURL, assetName())
		if cerr != nil {
			return "", fmt.Errorf("checksums: %w", cerr)
		}
		if want != "" && !strings.EqualFold(want, sum) {
			return "", fmt.Errorf("checksum mismatch — refusing to install (got %s, want %s)", sum, want)
		}
	}

	if err := tmp.Chmod(0o755); err != nil {
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	if err := replaceExecutable(tmpName, exePath); err != nil {
		return "", fmt.Errorf("install: %w", err)
	}
	return rel.TagName, nil
}

// downloadTo streams src into w and returns the lowercase hex SHA-256 of the
// bytes written.
func downloadTo(ctx context.Context, client *http.Client, url string, w io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "kinopub-gui")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, 200<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fetchChecksum downloads a "sha256  filename" checksums file and returns the
// hash recorded for name (empty string if not listed).
func fetchChecksum(ctx context.Context, client *http.Client, url, name string) (string, error) {
	var buf strings.Builder
	if _, err := downloadTo(ctx, client, url, &buf); err != nil {
		return "", err
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && filepath.Base(fields[len(fields)-1]) == name {
			return fields[0], nil
		}
	}
	return "", nil
}

// parseVersion parses a "vMAJOR.MINOR.PATCH" tag (the leading v and any
// -prerelease/+build suffix are optional) into its numeric components.
func parseVersion(v string) (int, int, int, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return 0, 0, 0, false
	}
	parts := strings.Split(v, ".")
	var nums [3]int
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			break
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

// isNewer reports whether release version latest is strictly newer than current.
func isNewer(latest, current string) bool {
	lMa, lMi, lPa, ok1 := parseVersion(latest)
	cMa, cMi, cPa, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	switch {
	case lMa != cMa:
		return lMa > cMa
	case lMi != cMi:
		return lMi > cMi
	default:
		return lPa > cPa
	}
}

package gui

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Managed external tools (ffmpeg/ffprobe) are downloaded into <configDir>/bin so
// the app can run downloads without the user installing anything system-wide.
// That directory is prepended to PATH, so exec.LookPath and every child process
// pick the managed binaries up transparently.

const managedBinSubdir = "bin"

// managedBinDir returns <configDir>/bin (where downloaded tools live), or "".
func managedBinDir() string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, managedBinSubdir)
}

// exeName appends .exe on Windows.
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// ensureManagedBinOnPath prepends the managed bin dir to PATH (idempotently) so
// LookPath and child processes resolve downloaded tools. Safe to call repeatedly.
func ensureManagedBinOnPath() {
	dir := managedBinDir()
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir); err != nil {
		return
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == dir {
			return
		}
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// systemToolDirs lists well-known locations where CLI tools such as ffmpeg are
// installed. A GUI app launched from Finder/Spotlight/Dock does NOT inherit the
// shell PATH — LaunchServices hands it a stripped-down PATH (/usr/bin:/bin:…)
// that omits Homebrew, MacPorts, etc.
func systemToolDirs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}
	case "windows":
		return nil
	default: // linux, bsd
		return []string{"/usr/local/bin", "/snap/bin"}
	}
}

// ensureSystemToolsOnPath appends the standard tool directories (those that
// exist and aren't already on PATH) so a system-installed ffmpeg is found even
// when the process inherited the minimal PATH of a .app launch. Appended, not
// prepended, so a managed (downloaded) ffmpeg and the user's own PATH still win.
func ensureSystemToolsOnPath() {
	have := make(map[string]bool)
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		have[p] = true
	}
	var add []string
	for _, d := range systemToolDirs() {
		if have[d] {
			continue
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			continue
		}
		have[d] = true
		add = append(add, d)
	}
	if len(add) == 0 {
		return
	}
	sep := string(os.PathListSeparator)
	if cur := os.Getenv("PATH"); cur != "" {
		os.Setenv("PATH", cur+sep+strings.Join(add, sep))
	} else {
		os.Setenv("PATH", strings.Join(add, sep))
	}
}

// archiveKind is how a downloaded tool archive is packed.
type archiveKind int

const (
	kindZip archiveKind = iota
	kindTarXz
)

// depDownload is one archive to fetch and the tool basenames to extract from it.
type depDownload struct {
	url      string
	kind     archiveKind
	binaries []string // basenames without extension, e.g. "ffmpeg", "ffprobe"
}

// ffmpegInstallPlan returns the per-platform download plan and whether automatic
// install is supported for the running OS/arch. Sources are well-known static
// builds: gyan/BtbN (Windows, Linux) and evermeet.cx (macOS).
func ffmpegInstallPlan() ([]depDownload, bool) {
	const btbn = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/"
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			return []depDownload{{
				url:      btbn + "ffmpeg-master-latest-win64-gpl.zip",
				kind:     kindZip,
				binaries: []string{"ffmpeg", "ffprobe"},
			}}, true
		}
	case "darwin":
		// evermeet ships separate static (Intel) builds; they run natively on
		// Intel and via Rosetta on Apple Silicon.
		return []depDownload{
			{url: "https://evermeet.cx/ffmpeg/getrelease/ffmpeg/zip", kind: kindZip, binaries: []string{"ffmpeg"}},
			{url: "https://evermeet.cx/ffmpeg/getrelease/ffprobe/zip", kind: kindZip, binaries: []string{"ffprobe"}},
		}, true
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return []depDownload{{url: btbn + "ffmpeg-master-latest-linux64-gpl.tar.xz", kind: kindTarXz, binaries: []string{"ffmpeg", "ffprobe"}}}, true
		case "arm64":
			return []depDownload{{url: btbn + "ffmpeg-master-latest-linuxarm64-gpl.tar.xz", kind: kindTarXz, binaries: []string{"ffmpeg", "ffprobe"}}}, true
		}
	}
	return nil, false
}

// ffmpegSourceDesc is a short human description of where ffmpeg is downloaded
// from for the running platform (shown in the UI).
func ffmpegSourceDesc() string {
	switch runtime.GOOS {
	case "windows", "linux":
		return "BtbN/FFmpeg-Builds (GitHub)"
	case "darwin":
		return "evermeet.cx"
	}
	return ""
}

// toolInstaller serializes ffmpeg installs.
type toolInstaller struct {
	mu sync.Mutex
}

func (t *toolInstaller) installFFmpeg(ctx context.Context) error {
	if !t.mu.TryLock() {
		return fmt.Errorf("an install is already in progress")
	}
	defer t.mu.Unlock()

	plan, ok := ffmpegInstallPlan()
	if !ok {
		return fmt.Errorf("automatic ffmpeg install is not supported on %s/%s — please install ffmpeg manually",
			runtime.GOOS, runtime.GOARCH)
	}
	dir := managedBinDir()
	if dir == "" {
		return fmt.Errorf("cannot determine config directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	client := &http.Client{Timeout: 8 * time.Minute}
	for _, dl := range plan {
		if err := downloadAndExtract(ctx, client, dl, dir); err != nil {
			return err
		}
	}
	ensureManagedBinOnPath()

	// Sanity-check that what we installed actually runs on this machine.
	if err := verifyTool(ctx, filepath.Join(dir, exeName("ffmpeg"))); err != nil {
		return fmt.Errorf("ffmpeg was downloaded but could not be run: %w", err)
	}
	return nil
}

func downloadAndExtract(ctx context.Context, client *http.Client, dl depDownload, destDir string) error {
	tmp, err := os.CreateTemp("", "kinopub-tool-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { tmp.Close(); os.Remove(tmpName) }()

	if _, err := downloadTo(ctx, client, dl.url, tmp, 400<<20); err != nil {
		return fmt.Errorf("download %s: %w", dl.url, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	switch dl.kind {
	case kindZip:
		return extractZipBinaries(tmpName, destDir, dl.binaries)
	case kindTarXz:
		return extractTarXzBinaries(ctx, tmpName, destDir, dl.binaries)
	}
	return fmt.Errorf("unknown archive kind")
}

// extractZipBinaries copies the named tool binaries out of a zip into destDir.
func extractZipBinaries(zipPath, destDir string, binaries []string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	want := make(map[string]bool, len(binaries))
	for _, b := range binaries {
		want[exeName(b)] = true
	}
	found := make(map[string]bool, len(binaries))
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if f.FileInfo().IsDir() || !want[base] || found[base] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeExecutable(filepath.Join(destDir, base), rc)
		rc.Close()
		if err != nil {
			return err
		}
		found[base] = true
	}
	return missingBinariesErr(want, found)
}

// extractTarXzBinaries unpacks a .tar.xz via the system tar (universally present
// on Linux) and copies the named binaries into destDir.
func extractTarXzBinaries(ctx context.Context, archivePath, destDir string, binaries []string) error {
	tmpDir, err := os.MkdirTemp("", "kinopub-tar-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.CommandContext(ctx, "tar", "-xf", archivePath, "-C", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract tar.xz failed (%v): %s — is `tar`/`xz` installed?",
			err, strings.TrimSpace(string(out)))
	}

	want := make(map[string]bool, len(binaries))
	for _, b := range binaries {
		want[exeName(b)] = true
	}
	found := make(map[string]bool, len(binaries))
	walkErr := filepath.WalkDir(tmpDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if !want[base] || found[base] {
			return nil
		}
		in, oerr := os.Open(p)
		if oerr != nil {
			return oerr
		}
		werr := writeExecutable(filepath.Join(destDir, base), in)
		in.Close()
		if werr != nil {
			return werr
		}
		found[base] = true
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	return missingBinariesErr(want, found)
}

func missingBinariesErr(want, found map[string]bool) error {
	var missing []string
	for b := range want {
		if !found[b] {
			missing = append(missing, b)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("expected %s not found in the downloaded archive", strings.Join(missing, ", "))
	}
	return nil
}

// writeExecutable writes r to a fresh file at path with 0755 permissions,
// replacing any existing file.
func writeExecutable(path string, r io.Reader) error {
	tmp := path + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(r, 400<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Chmod(tmp, 0o755)
	return os.Rename(tmp, path)
}

// verifyTool runs "<path> -version" to confirm the binary executes here.
func verifyTool(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, path, "-version").Run()
}

// DepsView reports tool availability and whether an in-app install is possible.
type DepsView struct {
	FFmpeg           FFmpegStatus `json:"ffmpeg"`
	InstallSupported bool         `json:"installSupported"`
	Managed          bool         `json:"managed"` // ffmpeg resolves to the managed bin dir
	Source           string       `json:"source,omitempty"`
}

func depsView() DepsView {
	_, supported := ffmpegInstallPlan()
	st := ffmpegStatus()
	managed := false
	if dir := managedBinDir(); dir != "" && st.FFmpegPath != "" {
		if rel, err := filepath.Rel(dir, st.FFmpegPath); err == nil && rel == exeName("ffmpeg") {
			managed = true
		}
	}
	return DepsView{
		FFmpeg:           st,
		InstallSupported: supported,
		Managed:          managed,
		Source:           ffmpegSourceDesc(),
	}
}

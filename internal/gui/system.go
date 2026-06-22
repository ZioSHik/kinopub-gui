package gui

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// FFmpegStatus reports availability of ffmpeg/ffprobe.
type FFmpegStatus struct {
	FFmpegFound   bool   `json:"ffmpegFound"`
	FFmpegPath    string `json:"ffmpegPath,omitempty"`
	FFmpegVersion string `json:"ffmpegVersion,omitempty"`
	FFprobeFound  bool   `json:"ffprobeFound"`
	FFprobePath   string `json:"ffprobePath,omitempty"`
}

func ffmpegStatus() FFmpegStatus {
	var st FFmpegStatus
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		st.FFmpegFound = true
		st.FFmpegPath = p
		st.FFmpegVersion = binaryVersion(p)
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		st.FFprobeFound = true
		st.FFprobePath = p
	}
	return st
}

func binaryVersion(path string) string {
	ctx, cancel := contextWithTimeout(3 * time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-version").Output()
	if err != nil {
		return ""
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	return strings.TrimSpace(line)
}

// FSEntry is a directory entry for the output-folder picker.
type FSEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// FSListing is the response for the directory browser.
type FSListing struct {
	Path   string    `json:"path"`
	Parent string    `json:"parent"`
	Dirs   []FSEntry `json:"dirs"`
}

// listDir returns the sub-directories of path (for the output picker). An empty
// path starts at the user's home directory.
func listDir(path string) (FSListing, error) {
	if path == "" || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return FSListing{}, err
		}
		path = home
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return FSListing{}, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return FSListing{}, err
	}
	listing := FSListing{Path: abs, Parent: filepath.Dir(abs)}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		listing.Dirs = append(listing.Dirs, FSEntry{Name: e.Name(), Path: filepath.Join(abs, e.Name())})
	}
	sort.Slice(listing.Dirs, func(a, b int) bool {
		return strings.ToLower(listing.Dirs[a].Name) < strings.ToLower(listing.Dirs[b].Name)
	})
	return listing, nil
}

// openInOS opens a file or folder with the OS default handler. When reveal is
// true it selects/reveals the item in the file manager instead of opening it.
func openInOS(path string, reveal bool) error {
	ctx, cancel := contextWithTimeout(10 * time.Second)
	defer cancel()

	// Windows needs an explicit command line so that paths containing spaces or
	// cmd metacharacters (& ^ % ( ) !) are handled literally — see
	// openOnWindows. explorer also returns a non-zero exit code even on success,
	// which that helper ignores.
	if runtime.GOOS == "windows" {
		return openOnWindows(ctx, path, reveal)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if reveal {
			cmd = exec.CommandContext(ctx, "open", "-R", path)
		} else {
			cmd = exec.CommandContext(ctx, "open", path)
		}
	default: // linux, bsd, …
		target := path
		if reveal {
			target = filepath.Dir(path)
		}
		cmd = exec.CommandContext(ctx, "xdg-open", target)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not open %q: %w", path, err)
	}
	return nil
}

// proxyImage fetches a poster image and streams it back, so browser-side
// hotlink/referer restrictions don't block posters.
//
// Because this endpoint fetches a URL supplied by the page, it is hardened
// against SSRF: only http/https is allowed, connections to non-public addresses
// (loopback, private, link-local) are refused at dial time (which also defeats
// DNS-rebinding), and the authenticated kino.pub Cookie is attached only when
// the target host is kino.pub itself — never to arbitrary CDN/third-party hosts.
// The hotlink Referer is harmless and sent to all hosts so CDN posters load.
func proxyImage(w http.ResponseWriter, r *http.Request, rawURL string) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		writeErr(w, http.StatusBadRequest, "invalid image url")
		return
	}

	headers := map[string]string{"Referer": "https://kino.pub/"}
	ua := defaultUserAgent

	ctx, cancel := contextWithTimeout(15 * time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Header.Set("User-Agent", ua)
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := ssrfSafeImageClient().Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("upstream HTTP %d", resp.StatusCode))
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || !strings.HasPrefix(ct, "image/") {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 16<<20))
}

// isKinoPubHost reports whether host is kino.pub or a sub-domain of it.
func isKinoPubHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "kino.pub" || strings.HasSuffix(host, ".kino.pub")
}

// ssrfSafeImageClient returns an HTTP client whose dialer refuses to connect to
// non-public IP addresses. Resolving and validating happen in the dialer, on the
// exact address that is then dialed, so there is no rebinding TOCTOU window; it
// also re-validates on every redirect hop because each hop opens a new dial.
func ssrfSafeImageClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           ssrfSafeDial,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

// ssrfSafeDial resolves addr and dials only public IP addresses, refusing
// loopback, private, link-local and unspecified targets.
func ssrfSafeDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for _, ip := range ips {
		if !isPublicIP(ip.IP) {
			lastErr = fmt.Errorf("refusing to connect to non-public address %s", ip.IP)
			continue
		}
		conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no address found for %q", host)
	}
	return nil, lastErr
}

// isPublicIP reports whether ip is a routable public address (not loopback,
// private, link-local, multicast or unspecified).
func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	return true
}

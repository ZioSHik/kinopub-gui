package gui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseVersion_Components(t *testing.T) {
	cases := []struct {
		in         string
		ma, mi, pa int
		ok         bool
	}{
		{"v1.2.3", 1, 2, 3, true},
		{"1.2.3", 1, 2, 3, true},
		{"v2", 2, 0, 0, true},   // missing minor/patch default to 0
		{"v1.5", 1, 5, 0, true}, // missing patch
		{"v1.2.3-rc1", 1, 2, 3, true},
		{"v1.2.3+build5", 1, 2, 3, true},
		{"  v1.2.3  ", 1, 2, 3, true}, // surrounding whitespace
		{"dev", 0, 0, 0, false},
		{"", 0, 0, 0, false},
		{"v1.x.3", 0, 0, 0, false}, // non-numeric component
	}
	for _, c := range cases {
		ma, mi, pa, ok := parseVersion(c.in)
		if ok != c.ok || (ok && (ma != c.ma || mi != c.mi || pa != c.pa)) {
			t.Errorf("parseVersion(%q) = %d.%d.%d ok=%v, want %d.%d.%d ok=%v",
				c.in, ma, mi, pa, ok, c.ma, c.mi, c.pa, c.ok)
		}
	}
}

func TestIsDevBuild(t *testing.T) {
	if !newUpdateChecker("dev").isDevBuild() {
		t.Error("'dev' should be a dev build")
	}
	if newUpdateChecker("v1.0.0").isDevBuild() {
		t.Error("'v1.0.0' should not be a dev build")
	}
}

func TestAssetNameForPlatform(t *testing.T) {
	got := assetName()
	want := fmt.Sprintf("kinopub-gui-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got != want {
		t.Errorf("assetName() = %q, want %q", got, want)
	}
}

func TestDownloadTo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	}))
	defer ts.Close()

	var sb strings.Builder
	sum, err := downloadTo(context.Background(), ts.Client(), ts.URL, &sb, 1<<20)
	if err != nil {
		t.Fatalf("downloadTo: %v", err)
	}
	if sb.String() != "hello" {
		t.Errorf("body = %q, want hello", sb.String())
	}
	// SHA-256 of "hello".
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if sum != want {
		t.Errorf("sha256 = %q, want %q", sum, want)
	}
}

func TestDownloadTo_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	var sb strings.Builder
	if _, err := downloadTo(context.Background(), ts.Client(), ts.URL, &sb, 1<<20); err == nil {
		t.Error("expected error on HTTP 404")
	}
}

func TestDownloadTo_RespectsByteCap(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("A", 1000))
	}))
	defer ts.Close()
	var sb strings.Builder
	if _, err := downloadTo(context.Background(), ts.Client(), ts.URL, &sb, 10); err != nil {
		t.Fatalf("downloadTo: %v", err)
	}
	if sb.Len() != 10 {
		t.Errorf("cap not respected: read %d bytes, want 10", sb.Len())
	}
}

func TestFetchChecksum(t *testing.T) {
	body := strings.Join([]string{
		"abc123  kinopub-gui-linux-amd64",
		"def456  kinopub-gui-darwin-arm64",
		"# a comment line with too few fields",
		"",
	}, "\n")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	got, err := fetchChecksum(context.Background(), ts.Client(), ts.URL, "kinopub-gui-darwin-arm64")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}
	if got != "def456" {
		t.Errorf("checksum = %q, want def456", got)
	}
	// A name not listed returns "".
	got, err = fetchChecksum(context.Background(), ts.Client(), ts.URL, "kinopub-gui-windows-amd64.exe")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}
	if got != "" {
		t.Errorf("unlisted name should return empty, got %q", got)
	}
}

func TestFetchChecksum_MatchesByBasename(t *testing.T) {
	// The recorded path may include a directory prefix; matching is by basename.
	body := "deadbeef  dist/kinopub-gui-linux-amd64\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer ts.Close()
	got, err := fetchChecksum(context.Background(), ts.Client(), ts.URL, "kinopub-gui-linux-amd64")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}
	if got != "deadbeef" {
		t.Errorf("checksum = %q, want deadbeef", got)
	}
}

func TestUpdateStatus_DevBuild(t *testing.T) {
	u := newUpdateChecker("dev")
	st := u.status(context.Background(), true)
	if st.Current != "dev" || st.Supported || st.UpdateAvailable {
		t.Errorf("dev build status wrong: %+v", st)
	}
	if st.Note == "" {
		t.Error("dev build should carry an explanatory note")
	}
}

func TestUpdateStatus_Caching(t *testing.T) {
	u := newUpdateChecker("dev")
	// Prime the cache with a sentinel; a non-forced status should return it
	// without re-fetching.
	u.cached = &UpdateStatus{Current: "dev", Note: "cached-sentinel"}
	u.at = time.Now()
	st := u.status(context.Background(), false)
	if st.Note != "cached-sentinel" {
		t.Errorf("non-forced status should use the fresh cache, got %+v", st)
	}
}

// The binary download must NOT go through the API client: http.Client.Timeout
// bounds the whole request including the body, and 20s kills a ~10 MB asset on
// any route slower than ~0.5 MB/s ("context deadline exceeded while reading
// body"). The download client relies on the caller's context instead.
func TestDownloadClientHasNoTotalTimeout(t *testing.T) {
	u := newUpdateChecker("v1.0.0")
	if got := u.httpClient().Timeout; got == 0 {
		t.Error("API client must keep a total timeout (status endpoint responsiveness)")
	}
	if got := u.downloadClient().Timeout; got != 0 {
		t.Errorf("download client Timeout = %v, want 0 (bounded by ctx, not a total cap)", got)
	}
}

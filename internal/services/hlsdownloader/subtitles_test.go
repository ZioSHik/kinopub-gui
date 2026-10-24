package hlsdownloader

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestSubtitlesUseConfiguredTLSClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "WEBVTT\n\n")
	}))
	defer srv.Close()
	d := New(srv.Client(), domain.RequestAuth{}, nopLogger{})
	_, err := d.DownloadSubtitles(context.Background(), []domain.SubtitleSource{{URL: srv.URL + "/sub.vtt"}}, t.TempDir())
	if err != nil {
		t.Fatalf("configured TLS client was not used for subtitles: %v", err)
	}
}

func TestDefaultSubtitleClientPreservesProxy(t *testing.T) {
	requests := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Host != "subtitle.invalid" || r.Header.Get("Referer") != "https://kino.watch/" {
			t.Errorf("proxy lost subtitle target or auth: %s %v", r.URL, r.Header)
		}
		io.WriteString(w, "WEBVTT\n\n")
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	d := New(nil, domain.RequestAuth{Headers: map[string]string{"Referer": "https://kino.watch/"}}, nopLogger{}, WithProxy(proxyURL))
	_, err := d.DownloadSubtitles(context.Background(), []domain.SubtitleSource{{URL: "http://subtitle.invalid/sub.vtt"}}, t.TempDir())
	if err != nil || requests != 1 {
		t.Fatalf("configured proxy was not used for subtitles: requests=%d err=%v", requests, err)
	}
}

func TestDownloadSubtitlesPreservesLanguagesAndAuth(t *testing.T) {
	const text = "1\n00:00:01,000 --> 00:00:02,000\nSubtitle\n"
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Referer") != "https://kino.watch/" {
			t.Error("subtitle request lost the CDN Referer")
		}
		io.WriteString(w, text)
	}))
	defer srv.Close()
	d := newTestDownloader(t, srv.Client())
	d.auth = domain.RequestAuth{Headers: map[string]string{"Referer": "https://kino.watch/"}}
	sources := []domain.SubtitleSource{
		{URL: srv.URL + "/ru.srt?token=test", Language: "rus", Name: "Russian"},
		{URL: srv.URL + "/en.srt", Language: "eng", Name: "English"},
	}
	sources = append(sources, sources[0])
	tracks, err := d.DownloadSubtitles(context.Background(), sources, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 || requests != 2 {
		t.Fatalf("tracks/requests = %d/%d, want 2/2", len(tracks), requests)
	}
	for i, track := range tracks {
		data, err := os.ReadFile(track.Path)
		if err != nil || string(data) != text {
			t.Fatalf("subtitle data: %q, %v", data, err)
		}
		if track.Language != sources[i].Language || track.Name != sources[i].Name {
			t.Fatalf("subtitle metadata lost: %+v", track)
		}
	}
}

func TestDownloadSubtitlesFailsOnTransientAndSkipsPermanentErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"unavailable", 503, "retry", true},
		{"not found", 404, "", false},
		{"empty", 200, "", false},
		{"too large", 200, strings.Repeat("x", maxSubtitleBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			dir := t.TempDir()
			tracks, err := newTestDownloader(t, srv.Client()).DownloadSubtitles(context.Background(), []domain.SubtitleSource{{URL: srv.URL + "/sub.srt"}}, dir)
			if tc.wantErr && err == nil {
				t.Fatal("a transient subtitle failure must retry the episode, not complete it")
			}
			if !tc.wantErr && (err != nil || len(tracks) != 0) {
				t.Fatalf("a permanently broken subtitle must be skipped: tracks=%v err=%v", tracks, err)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatalf("left partial subtitle files: %v", files)
			}
		})
	}
}

func TestDownloadSubtitlesRetriesTransientCDNError(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "WEBVTT\n\n")
	}))
	defer srv.Close()
	_, err := newTestDownloader(t, srv.Client()).DownloadSubtitles(context.Background(), []domain.SubtitleSource{{URL: srv.URL + "/sub.vtt"}}, t.TempDir())
	if err != nil || requests != 2 {
		t.Fatalf("transient CDN error was not retried: requests=%d err=%v", requests, err)
	}
}

package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// recordingRun captures invocations of the RunFunc and optionally writes output.
type recordingRun struct {
	calls    int
	lastArgs []string
	write    string // content to write to temp path (last arg); empty = nothing
	err      error
}

func (r *recordingRun) fn(_ context.Context, _ string, args, _ []string, _ io.Writer) error {
	r.calls++
	r.lastArgs = args
	if r.write != "" {
		tempPath := args[len(args)-1]
		_ = os.WriteFile(tempPath, []byte(r.write), 0644)
	}
	return r.err
}

func TestDownloader_RemuxLocal_Success(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")
	rr := &recordingRun{write: "muxed"}

	d := New(rr.fn, &testProxy{}, testLogger{})
	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		OutPath: out,
	}
	if err := d.RemuxLocal(context.Background(), job, "/tmp/in.ts"); err != nil {
		t.Fatalf("RemuxLocal error: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output missing: %v", err)
	}
	// Verify it used BuildRemuxArgs shape: -map 0 and local input.
	if !containsPair(rr.lastArgs, "-map", "0") {
		t.Error("expected -map 0 for remux")
	}
	if !containsPair(rr.lastArgs, "-i", "/tmp/in.ts") {
		t.Error("expected local input")
	}
	// Temp file cleaned (renamed).
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp should be renamed away")
	}
}

func TestDownloader_RemuxLocal_FFmpegFails(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")
	rr := &recordingRun{write: "partial", err: errors.New("boom")}

	d := New(rr.fn, &testProxy{}, testLogger{})
	job := domain.Job{OutPath: out}
	err := d.RemuxLocal(context.Background(), job, "/tmp/in.ts")
	if !errors.Is(err, domain.ErrFFmpegFailed) {
		t.Fatalf("expected ErrFFmpegFailed, got %v", err)
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp should be removed on failure")
	}
}

func TestDownloader_RemuxLocal_EmptyOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")
	// write empty content via run that creates an empty file.
	run := func(_ context.Context, _ string, args, _ []string, _ io.Writer) error {
		f, _ := os.Create(args[len(args)-1])
		return f.Close()
	}
	d := New(run, &testProxy{}, testLogger{})
	job := domain.Job{OutPath: out}
	err := d.RemuxLocal(context.Background(), job, "/tmp/in.ts")
	if !errors.Is(err, domain.ErrEmptyOutput) {
		t.Fatalf("expected ErrEmptyOutput, got %v", err)
	}
}

func TestDownloader_MuxHLS_Success(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "o.mkv")
	rr := &recordingRun{write: "hlsmuxed"}
	d := New(rr.fn, &testProxy{}, testLogger{})
	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		OutPath: out,
	}
	hls := &domain.HLSDownloadResult{
		VideoPath:   "/tmp/v.ts",
		AudioTracks: []domain.HLSAudioTrack{{Path: "/tmp/a.ts", Name: "Dub", Language: "ru"}},
	}
	if err := d.MuxHLS(context.Background(), job, hls); err != nil {
		t.Fatalf("MuxHLS error: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if !containsPair(rr.lastArgs, "-map", "1:a:0") {
		t.Error("expected audio map from MuxHLS")
	}
}

func TestDownloader_MuxHLS_FFmpegFails(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "o.mkv")
	rr := &recordingRun{write: "x", err: errors.New("fail")}
	d := New(rr.fn, &testProxy{}, testLogger{})
	job := domain.Job{OutPath: out}
	hls := &domain.HLSDownloadResult{VideoPath: "/tmp/v.ts"}
	err := d.MuxHLS(context.Background(), job, hls)
	if !errors.Is(err, domain.ErrFFmpegFailed) {
		t.Fatalf("expected ErrFFmpegFailed, got %v", err)
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp should be removed")
	}
}

func TestDownloader_Download_ChunkedPath(t *testing.T) {
	// Serve a progressive file; chunked downloader fetches it, then ffmpeg remux
	// is faked by the RunFunc producing the final file.
	body := []byte(strings.Repeat("Z", 4096))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			return
		}
		http.ServeContent(w, r, "v.mp4", testModTime, bytesReadSeeker(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")

	rr := &recordingRun{write: "remuxed"}
	d := New(rr.fn, &testProxy{}, testLogger{},
		WithHTTPClient(&http.Client{}),
		WithAuth(domain.RequestAuth{UserAgent: "UA"}),
	)

	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{Kind: domain.MediaProgressive, URL: srv.URL + "/v.mp4"},
			Video:  domain.VideoTrack{Index: 0},
		},
		OutPath: out,
	}
	if err := d.Download(context.Background(), job, nil); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	if rr.calls != 1 {
		t.Errorf("expected exactly 1 ffmpeg remux call, got %d", rr.calls)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("final output missing: %v", err)
	}
	// .raw intermediate cleaned up.
	if _, err := os.Stat(out + ".raw"); !os.IsNotExist(err) {
		t.Error(".raw intermediate should be removed after remux")
	}
	// Remux used the local .raw file (RemuxLocal → -map 0).
	if !containsPair(rr.lastArgs, "-map", "0") {
		t.Error("expected remux to use -map 0 on local raw file")
	}
}

func TestDownloader_Download_ChunkedFallsBackToDirect(t *testing.T) {
	// HEAD returns 403 → chunked probe fails → fall back to direct ffmpeg.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")
	rr := &recordingRun{write: "direct-output"}
	d := New(rr.fn, &testProxy{}, testLogger{}, WithHTTPClient(&http.Client{}))

	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{Kind: domain.MediaProgressive, URL: srv.URL + "/v.mp4"},
			Video:  domain.VideoTrack{Index: 0},
		},
		OutPath: out,
	}
	if err := d.Download(context.Background(), job, nil); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output missing after fallback: %v", err)
	}
	// Direct path builds full ffmpeg args (has -progress pipe:1).
	if !containsPair(rr.lastArgs, "-progress", "pipe:1") {
		t.Error("expected direct ffmpeg path (with -progress) after fallback")
	}
}

func TestDownloader_Download_ExtraArgsSkipsChunked(t *testing.T) {
	// Even with an http client, extra args force the direct ffmpeg path.
	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")
	rr := &recordingRun{write: "out"}
	d := New(rr.fn, &testProxy{}, testLogger{},
		WithHTTPClient(&http.Client{}),
		WithExtraArgs([]string{"-c:v", "libx265"}),
	)
	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{Kind: domain.MediaProgressive, URL: "https://cdn/v.mp4"},
			Video:  domain.VideoTrack{Index: 0},
		},
		OutPath: out,
	}
	if err := d.Download(context.Background(), job, nil); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	// Direct path: extra args present, -progress present, no .raw created.
	if !containsPair(rr.lastArgs, "-c:v", "libx265") {
		t.Error("expected extra args in command")
	}
	if !containsPair(rr.lastArgs, "-progress", "pipe:1") {
		t.Error("expected direct path (with -progress)")
	}
}

func TestDownloader_Download_NoChunkedOptionForcesDirect(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")
	rr := &recordingRun{write: "out"}
	d := New(rr.fn, &testProxy{}, testLogger{},
		WithHTTPClient(&http.Client{}),
		WithNoChunked(true),
	)
	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{Kind: domain.MediaProgressive, URL: "https://cdn/v.mp4"},
			Video:  domain.VideoTrack{Index: 0},
		},
		OutPath: out,
	}
	if err := d.Download(context.Background(), job, nil); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	if !containsPair(rr.lastArgs, "-progress", "pipe:1") {
		t.Error("expected direct ffmpeg path with NoChunked")
	}
}

func TestDownloader_Download_TruncationDetected(t *testing.T) {
	// ffmpeg succeeds and writes a file, but only reports 50% progress → the
	// downloader must treat it as truncated and fail.
	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")
	run := func(_ context.Context, _ string, args, _ []string, stdout io.Writer) error {
		_ = os.WriteFile(args[len(args)-1], []byte("short content"), 0644)
		if stdout != nil {
			// 1 minute out of 4 minutes = 25%.
			fmt.Fprint(stdout, "out_time=00:01:00.000000\nprogress=end\n")
		}
		return nil
	}
	d := New(run, &testProxy{}, testLogger{})
	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		Media: domain.ResolvedMedia{
			Source:   domain.MediaSource{Kind: domain.MediaProgressive, URL: "https://cdn/v.mp4"},
			Video:    domain.VideoTrack{Index: 0},
			Duration: 4 * time.Minute,
		},
		OutPath: out,
	}
	// Force direct path (no http client).
	err := d.Download(context.Background(), job, &testProgressSink{})
	if err == nil {
		t.Fatal("expected truncation error")
	}
	if !errors.Is(err, domain.ErrFFmpegFailed) {
		t.Errorf("expected ErrFFmpegFailed wrapper, got %v", err)
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("expected truncated message, got %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("truncated output must be removed")
	}
}

func TestDownloader_Download_LocalFileSkipsChunked(t *testing.T) {
	// A non-http URL (local file) must not use chunked even with http client.
	dir := t.TempDir()
	out := filepath.Join(dir, "S01E01.mkv")
	rr := &recordingRun{write: "out"}
	d := New(rr.fn, &testProxy{}, testLogger{}, WithHTTPClient(&http.Client{}))
	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{Kind: domain.MediaProgressive, URL: "/local/path/v.mp4"},
			Video:  domain.VideoTrack{Index: 0},
		},
		OutPath: out,
	}
	if err := d.Download(context.Background(), job, nil); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	if !containsPair(rr.lastArgs, "-progress", "pipe:1") {
		t.Error("local file should go through direct ffmpeg path")
	}
}

func TestNoopSink_TrackProgress(t *testing.T) {
	// Just exercise the no-panic path.
	var s noopSink
	s.TrackProgress(domain.EpisodeKey{Season: 1, Episode: 1}, domain.TrackRef{}, 50)
}

func TestEstimateDuration(t *testing.T) {
	job := domain.Job{Media: domain.ResolvedMedia{Duration: 90 * time.Second}}
	if got := estimateDuration(job); got != 90*time.Second {
		t.Errorf("estimateDuration = %v, want 90s", got)
	}
	if got := estimateDuration(domain.Job{}); got != 0 {
		t.Errorf("estimateDuration empty = %v, want 0", got)
	}
}

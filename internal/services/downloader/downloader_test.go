package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// testLogger is a no-op logger for tests.
type testLogger struct{}

func (testLogger) Debug(_ string, _ ...domain.Field) {}
func (testLogger) Info(_ string, _ ...domain.Field)  {}
func (testLogger) Warn(_ string, _ ...domain.Field)  {}
func (testLogger) Error(_ string, _ ...domain.Field) {}
func (l testLogger) With(_ ...domain.Field) domain.Logger  { return l }
func (l testLogger) Component(_ string) domain.Logger      { return l }

// testProxy is a minimal ProxyProvider for tests.
type testProxy struct {
	env []string
	err error
}

func (p *testProxy) HTTPClient() *http.Client      { return nil }
func (p *testProxy) FFmpegEnv() ([]string, error)  { return p.env, p.err }
func (p *testProxy) Mode() domain.ProxyMode        { return domain.ProxyDirect }

// testProgressSink records progress updates.
type testProgressSink struct {
	updates []progressUpdate
}

type progressUpdate struct {
	key     domain.EpisodeKey
	track   domain.TrackRef
	percent int
}

func (s *testProgressSink) TrackProgress(key domain.EpisodeKey, track domain.TrackRef, percent int) {
	s.updates = append(s.updates, progressUpdate{key: key, track: track, percent: percent})
}

func TestDownloader_Download_Success(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "S01E01.mkv")

	// RunFunc that writes content to the temp file (simulating ffmpeg).
	run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
		// Find the temp path (last arg).
		tempPath := args[len(args)-1]
		// Write some content to simulate ffmpeg output.
		if err := os.WriteFile(tempPath, []byte("fake video content"), 0644); err != nil {
			return err
		}
		// Write some progress to stdout if available.
		if stdout != nil {
			fmt.Fprint(stdout, "out_time=00:02:00.000000\nprogress=end\n")
		}
		return nil
	}

	proxy := &testProxy{}
	logger := testLogger{}

	d := New(run, proxy, logger)

	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video:    domain.VideoTrack{Index: 0},
			Duration: 2 * time.Minute,
		},
		OutPath: outPath,
	}

	sink := &testProgressSink{}
	err := d.Download(context.Background(), job, sink)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	// Verify final file exists.
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}

	// Verify temp file is gone.
	tempPath := outPath + ".tmp"
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful download")
	}

	// Verify progress was reported.
	if len(sink.updates) == 0 {
		t.Error("expected progress updates")
	}
	// 2 minutes out of 2 minutes = 100%
	found100 := false
	for _, u := range sink.updates {
		if u.percent == 100 {
			found100 = true
		}
	}
	if !found100 {
		t.Errorf("expected 100%% progress, got updates: %v", sink.updates)
	}
}

func TestDownloader_Download_FFmpegFails(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "S01E01.mkv")

	// RunFunc that creates a temp file but returns an error.
	run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
		tempPath := args[len(args)-1]
		// Write partial content.
		_ = os.WriteFile(tempPath, []byte("partial"), 0644)
		return errors.New("exit status 1")
	}

	proxy := &testProxy{}
	logger := testLogger{}

	d := New(run, proxy, logger)

	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
		OutPath: outPath,
	}

	err := d.Download(context.Background(), job, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrFFmpegFailed) {
		t.Errorf("expected ErrFFmpegFailed, got: %v", err)
	}

	// Verify temp file was cleaned up (Req 7.4).
	tempPath := outPath + ".tmp"
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("temp file should be deleted on failure")
	}

	// Verify final file was not created.
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Error("output file should not exist on failure")
	}
}

func TestDownloader_Download_EmptyOutput(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "S01E01.mkv")

	// RunFunc that creates an empty temp file (simulating ffmpeg success but no output).
	run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
		tempPath := args[len(args)-1]
		// Create empty file.
		f, err := os.Create(tempPath)
		if err != nil {
			return err
		}
		return f.Close()
	}

	proxy := &testProxy{}
	logger := testLogger{}

	d := New(run, proxy, logger)

	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
		OutPath: outPath,
	}

	err := d.Download(context.Background(), job, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrEmptyOutput) {
		t.Errorf("expected ErrEmptyOutput, got: %v", err)
	}

	// Verify temp file was cleaned up (Req 7.7).
	tempPath := outPath + ".tmp"
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("temp file should be deleted when output is empty")
	}
}

func TestDownloader_Download_MissingTempFile(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "S01E01.mkv")

	// RunFunc that doesn't create any file (simulating ffmpeg success but no file).
	run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
		return nil
	}

	proxy := &testProxy{}
	logger := testLogger{}

	d := New(run, proxy, logger)

	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
		OutPath: outPath,
	}

	err := d.Download(context.Background(), job, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrEmptyOutput) {
		t.Errorf("expected ErrEmptyOutput, got: %v", err)
	}
}

func TestDownloader_Download_ProxyError(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "S01E01.mkv")

	run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
		t.Fatal("run should not be called when proxy fails")
		return nil
	}

	proxy := &testProxy{err: fmt.Errorf("%w: socks5 not supported", domain.ErrProxyUnsupportedFFmpeg)}
	logger := testLogger{}

	d := New(run, proxy, logger)

	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
		OutPath: outPath,
	}

	err := d.Download(context.Background(), job, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrProxyUnsupportedFFmpeg) {
		t.Errorf("expected ErrProxyUnsupportedFFmpeg, got: %v", err)
	}
}

func TestDownloader_Execute_DelegatesToDownload(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "S01E01.mkv")

	run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
		tempPath := args[len(args)-1]
		return os.WriteFile(tempPath, []byte("content"), 0644)
	}

	proxy := &testProxy{}
	logger := testLogger{}

	d := New(run, proxy, logger)

	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
		OutPath: outPath,
	}

	err := d.Execute(context.Background(), job)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify file was created.
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("output file not found: %v", err)
	}
}

func TestDownloader_WithFFmpegPath(t *testing.T) {
	var capturedName string
	run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
		capturedName = name
		tempPath := args[len(args)-1]
		return os.WriteFile(tempPath, []byte("content"), 0644)
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "S01E01.mkv")

	proxy := &testProxy{}
	logger := testLogger{}

	d := New(run, proxy, logger, WithFFmpegPath("/usr/local/bin/ffmpeg"))

	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
		OutPath: outPath,
	}

	_ = d.Execute(context.Background(), job)

	if capturedName != "/usr/local/bin/ffmpeg" {
		t.Errorf("expected ffmpeg path /usr/local/bin/ffmpeg, got %q", capturedName)
	}
}

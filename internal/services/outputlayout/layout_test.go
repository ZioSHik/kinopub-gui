package outputlayout

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Compile-time interface satisfaction check.
var _ domain.OutputLayout = (*Layout)(nil)

func TestEpisodePath_Basic(t *testing.T) {
	l := New(domain.ContainerMKV)

	series := domain.Series{
		ID:    "12345",
		Title: "Тест Сериал",
	}
	ep := domain.Episode{
		Key: domain.EpisodeKey{
			Series:  "12345",
			Season:  1,
			Episode: 8,
		},
	}

	got, err := l.EpisodePath("/output", series, ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join("/output", "Тест Сериал", "Season 01", "S01E08.mkv")
	if got != want {
		t.Errorf("EpisodePath = %q, want %q", got, want)
	}
}

func TestEpisodePath_MP4Container(t *testing.T) {
	l := New(domain.ContainerMP4)

	series := domain.Series{
		ID:    "99",
		Title: "Show",
	}
	ep := domain.Episode{
		Key: domain.EpisodeKey{
			Series:  "99",
			Season:  12,
			Episode: 3,
		},
	}

	got, err := l.EpisodePath("/root", series, ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join("/root", "Show", "Season 12", "S12E03.mp4")
	if got != want {
		t.Errorf("EpisodePath = %q, want %q", got, want)
	}
}

func TestEpisodePath_SanitizesTitle(t *testing.T) {
	l := New(domain.ContainerMKV)

	series := domain.Series{
		ID:    "42",
		Title: "Bad/Title:With*Chars",
	}
	ep := domain.Episode{
		Key: domain.EpisodeKey{
			Series:  "42",
			Season:  2,
			Episode: 5,
		},
	}

	got, err := l.EpisodePath("/out", series, ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join("/out", "Bad_Title_With_Chars", "Season 02", "S02E05.mkv")
	if got != want {
		t.Errorf("EpisodePath = %q, want %q", got, want)
	}
}

func TestEpisodePath_EmptyTitleFallback(t *testing.T) {
	l := New(domain.ContainerMKV)

	series := domain.Series{
		ID:    "777",
		Title: "", // empty title → fallback
	}
	ep := domain.Episode{
		Key: domain.EpisodeKey{
			Series:  "777",
			Season:  1,
			Episode: 1,
		},
	}

	got, err := l.EpisodePath("/out", series, ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join("/out", "series_777", "Season 01", "S01E01.mkv")
	if got != want {
		t.Errorf("EpisodePath = %q, want %q", got, want)
	}
}

func TestEpisodePath_ZeroPadding(t *testing.T) {
	l := New(domain.ContainerMKV)

	series := domain.Series{
		ID:    "1",
		Title: "X",
	}
	ep := domain.Episode{
		Key: domain.EpisodeKey{
			Series:  "1",
			Season:  3,
			Episode: 14,
		},
	}

	got, err := l.EpisodePath("/", series, ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join("/", "X", "Season 03", "S03E14.mkv")
	if got != want {
		t.Errorf("EpisodePath = %q, want %q", got, want)
	}
}

func TestEnsureDirs_CreatesDirectories(t *testing.T) {
	tmp := t.TempDir()
	l := New(domain.ContainerMKV)

	path := filepath.Join(tmp, "a", "b", "c", "file.mkv")
	if err := l.EnsureDirs(path); err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}

	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected directory, got file")
	}
}

func TestEnsureDirs_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	l := New(domain.ContainerMKV)

	path := filepath.Join(tmp, "x", "y", "file.mkv")

	// Call twice — second call should not error.
	if err := l.EnsureDirs(path); err != nil {
		t.Fatalf("first EnsureDirs failed: %v", err)
	}
	if err := l.EnsureDirs(path); err != nil {
		t.Fatalf("second EnsureDirs failed: %v", err)
	}
}

func TestEnsureDirs_UnwritableError(t *testing.T) {
	// Use a path that cannot be created (e.g., under /dev/null on Unix).
	l := New(domain.ContainerMKV)

	path := "/dev/null/impossible/path/file.mkv"
	err := l.EnsureDirs(path)
	if err == nil {
		t.Fatal("expected error for unwritable path, got nil")
	}
	if !errors.Is(err, domain.ErrOutputDirUnwritable) {
		t.Errorf("expected ErrOutputDirUnwritable, got: %v", err)
	}
}

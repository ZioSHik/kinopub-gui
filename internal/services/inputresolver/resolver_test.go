package inputresolver

import (
	"context"
	"errors"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// stubLogger is a minimal logger for tests.
type stubLogger struct{}

func (stubLogger) Debug(string, ...domain.Field) {}
func (stubLogger) Info(string, ...domain.Field)  {}
func (stubLogger) Warn(string, ...domain.Field)  {}
func (stubLogger) Error(string, ...domain.Field) {}
func (l stubLogger) With(...domain.Field) domain.Logger   { return l }
func (l stubLogger) Component(string) domain.Logger       { return l }

func TestClassify_PodcastFeed(t *testing.T) {
	r := New(stubLogger{})

	tests := []struct {
		name string
		url  string
	}{
		{"basic feed", "https://kino.pub/podcast/get/12345/abctoken"},
		{"http scheme", "http://kino.pub/podcast/get/1/tok"},
		{"long id and token", "https://kino.pub/podcast/get/999999/some-long-token-value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, err := r.Classify(tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if class != domain.ClassPodcastFeed {
				t.Fatalf("expected ClassPodcastFeed, got %d", class)
			}
		})
	}
}

func TestClassify_PageLink(t *testing.T) {
	r := New(stubLogger{})

	tests := []struct {
		name string
		url  string
	}{
		{"basic page link", "https://kino.pub/item/view/12345"},
		{"page link with slug", "https://kino.pub/item/view/12345/some-title-slug"},
		{"http scheme", "http://kino.pub/item/view/1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, err := r.Classify(tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if class != domain.ClassPageLink {
				t.Fatalf("expected ClassPageLink, got %d", class)
			}
		})
	}
}

func TestClassify_Invalid(t *testing.T) {
	r := New(stubLogger{})

	tests := []struct {
		name string
		url  string
	}{
		{"empty string", ""},
		{"no scheme", "kino.pub/podcast/get/1/tok"},
		{"ftp scheme", "ftp://kino.pub/podcast/get/1/tok"},
		{"wrong host", "https://example.com/podcast/get/1/tok"},
		{"unclassified path", "https://kino.pub/some/other/path"},
		{"podcast path missing token", "https://kino.pub/podcast/get/123"},
		{"item view non-numeric id", "https://kino.pub/item/view/abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, err := r.Classify(tt.url)
			if !errors.Is(err, domain.ErrInvalidInputURL) {
				t.Fatalf("expected ErrInvalidInputURL, got: %v", err)
			}
			if class != domain.ClassUnclassified {
				t.Fatalf("expected ClassUnclassified, got %d", class)
			}
		})
	}
}

func TestResolve_PodcastFeed(t *testing.T) {
	r := New(stubLogger{})
	ctx := context.Background()

	src, err := r.Resolve(ctx, "https://kino.pub/podcast/get/42/mytoken123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.ID != "42" {
		t.Fatalf("expected ID=42, got %q", src.ID)
	}
	if src.Token != "mytoken123" {
		t.Fatalf("expected Token=mytoken123, got %q", src.Token)
	}
}

func TestResolve_PageLink_ReturnsErrFeedTokenUnavailable(t *testing.T) {
	r := New(stubLogger{})
	ctx := context.Background()

	src, err := r.Resolve(ctx, "https://kino.pub/item/view/999")
	if !errors.Is(err, domain.ErrFeedTokenUnavailable) {
		t.Fatalf("expected ErrFeedTokenUnavailable, got: %v", err)
	}
	if src != (domain.FeedSource{}) {
		t.Fatalf("expected empty FeedSource, got %+v", src)
	}
}

func TestResolve_Invalid_ReturnsErrInvalidInputURL(t *testing.T) {
	r := New(stubLogger{})
	ctx := context.Background()

	tests := []string{
		"",
		"not-a-url",
		"https://example.com/podcast/get/1/tok",
		"https://kino.pub/unknown/path",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			src, err := r.Resolve(ctx, rawURL)
			if !errors.Is(err, domain.ErrInvalidInputURL) {
				t.Fatalf("expected ErrInvalidInputURL for %q, got: %v", rawURL, err)
			}
			if src != (domain.FeedSource{}) {
				t.Fatalf("expected empty FeedSource for %q, got %+v", rawURL, src)
			}
		})
	}
}

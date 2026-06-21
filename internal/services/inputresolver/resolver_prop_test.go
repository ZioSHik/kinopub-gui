package inputresolver

import (
	"context"
	"fmt"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"

	"pgregory.net/rapid"
)

// **Validates: Requirements 1.1, 1.2, 1.5, 1.7**

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// genNumericID generates a numeric ID string (1 to 9 digits).
func genNumericID() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(1, 999999999).Draw(t, "id")
		return fmt.Sprintf("%d", n)
	})
}

// genToken generates a non-empty token string that does not contain '/'.
func genToken() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		// Token: alphanumeric + dashes, 1-64 chars
		chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
		length := rapid.IntRange(1, 64).Draw(t, "tokenLen")
		buf := make([]byte, length)
		for i := range buf {
			buf[i] = chars[rapid.IntRange(0, len(chars)-1).Draw(t, "ch")]
		}
		return string(buf)
	})
}

// genSlug generates an optional slug segment (may be empty).
func genSlug() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		hasSlug := rapid.Bool().Draw(t, "hasSlug")
		if !hasSlug {
			return ""
		}
		chars := "abcdefghijklmnopqrstuvwxyz0123456789-"
		length := rapid.IntRange(1, 30).Draw(t, "slugLen")
		buf := make([]byte, length)
		for i := range buf {
			buf[i] = chars[rapid.IntRange(0, len(chars)-1).Draw(t, "sch")]
		}
		return "/" + string(buf)
	})
}

// genScheme generates http or https.
func genScheme() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"http", "https"})
}

// genPodcastFeedURL generates a valid podcast feed URL on kino.pub.
func genPodcastFeedURL() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		scheme := genScheme().Draw(t, "scheme")
		id := genNumericID().Draw(t, "id")
		token := genToken().Draw(t, "token")
		return fmt.Sprintf("%s://kino.pub/podcast/get/%s/%s", scheme, id, token)
	})
}

// genPageLinkURL generates a valid page link URL on kino.pub.
func genPageLinkURL() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		scheme := genScheme().Draw(t, "scheme")
		id := genNumericID().Draw(t, "id")
		slug := genSlug().Draw(t, "slug")
		return fmt.Sprintf("%s://kino.pub/item/view/%s%s", scheme, id, slug)
	})
}

// genInvalidURL generates URLs that should be classified as invalid:
// empty, non-HTTP(S), wrong host, or unclassified path on kino.pub.
func genInvalidURL() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		kind := rapid.IntRange(0, 4).Draw(t, "invalidKind")
		switch kind {
		case 0:
			// Empty string
			return ""
		case 1:
			// Non-HTTP(S) scheme
			schemes := []string{"ftp", "file", "gopher", "ws", "wss", "ssh", "telnet"}
			scheme := rapid.SampledFrom(schemes).Draw(t, "badScheme")
			return fmt.Sprintf("%s://kino.pub/podcast/get/1/tok", scheme)
		case 2:
			// Wrong host with valid path
			hosts := []string{"example.com", "notkinopub.org", "kino.pub.evil.com", "kinopub.com"}
			host := rapid.SampledFrom(hosts).Draw(t, "badHost")
			return fmt.Sprintf("https://%s/podcast/get/1/tok", host)
		case 3:
			// kino.pub with unclassified path
			paths := []string{"/", "/about", "/some/random/path", "/podcast", "/podcast/get",
				"/podcast/get/abc/tok", "/item", "/item/view", "/item/view/abc"}
			path := rapid.SampledFrom(paths).Draw(t, "badPath")
			return fmt.Sprintf("https://kino.pub%s", path)
		default:
			// Random junk (not a URL at all)
			chars := "abcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*() "
			length := rapid.IntRange(1, 50).Draw(t, "junkLen")
			buf := make([]byte, length)
			for i := range buf {
				buf[i] = chars[rapid.IntRange(0, len(chars)-1).Draw(t, "jc")]
			}
			return string(buf)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 1: URL classification matches input family
// ---------------------------------------------------------------------------

// For any generated URL built from the podcast-feed template, Classify returns
// ClassPodcastFeed. For page-link template, returns ClassPageLink. For random
// junk or wrong host, returns ClassUnclassified with error. Only kino.pub-hosted
// URLs are ever classified as feed or page.

func TestProperty1_PodcastFeedURLClassifiedCorrectly(t *testing.T) {
	r := New(stubLogger{})

	rapid.Check(t, func(t *rapid.T) {
		url := genPodcastFeedURL().Draw(t, "url")
		class, err := r.Classify(url)
		if err != nil {
			t.Fatalf("Classify(%q) returned unexpected error: %v", url, err)
		}
		if class != domain.ClassPodcastFeed {
			t.Fatalf("Classify(%q) = %d, want ClassPodcastFeed (%d)", url, class, domain.ClassPodcastFeed)
		}
	})
}

func TestProperty1_PageLinkURLClassifiedCorrectly(t *testing.T) {
	r := New(stubLogger{})

	rapid.Check(t, func(t *rapid.T) {
		url := genPageLinkURL().Draw(t, "url")
		class, err := r.Classify(url)
		if err != nil {
			t.Fatalf("Classify(%q) returned unexpected error: %v", url, err)
		}
		if class != domain.ClassPageLink {
			t.Fatalf("Classify(%q) = %d, want ClassPageLink (%d)", url, class, domain.ClassPageLink)
		}
	})
}

func TestProperty1_InvalidURLClassifiedAsUnclassified(t *testing.T) {
	r := New(stubLogger{})

	rapid.Check(t, func(t *rapid.T) {
		url := genInvalidURL().Draw(t, "url")
		class, err := r.Classify(url)
		if err == nil {
			t.Fatalf("Classify(%q) returned nil error, want non-nil for invalid URL", url)
		}
		if class != domain.ClassUnclassified {
			t.Fatalf("Classify(%q) = %d, want ClassUnclassified (%d)", url, class, domain.ClassUnclassified)
		}
	})
}

func TestProperty1_OnlyKinoPubHostClassifiedAsFeedOrPage(t *testing.T) {
	r := New(stubLogger{})

	rapid.Check(t, func(t *rapid.T) {
		// Generate a URL with a non-kino.pub host but valid path patterns
		hosts := []string{"example.com", "other.org", "kino.pub.fake.com", "notkino.pub", "pub.kino"}
		host := rapid.SampledFrom(hosts).Draw(t, "host")
		scheme := genScheme().Draw(t, "scheme")

		// Try both feed and page paths on wrong host
		pathKind := rapid.IntRange(0, 1).Draw(t, "pathKind")
		var url string
		if pathKind == 0 {
			id := genNumericID().Draw(t, "id")
			token := genToken().Draw(t, "token")
			url = fmt.Sprintf("%s://%s/podcast/get/%s/%s", scheme, host, id, token)
		} else {
			id := genNumericID().Draw(t, "id")
			url = fmt.Sprintf("%s://%s/item/view/%s", scheme, host, id)
		}

		class, _ := r.Classify(url)
		if class == domain.ClassPodcastFeed || class == domain.ClassPageLink {
			t.Fatalf("Classify(%q) = %d, non-kino.pub host should never be classified as feed or page", url, class)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 2: Feed source preserves feed identity
// ---------------------------------------------------------------------------

// For any podcast feed URL with generated numeric id and token, resolving it
// yields a FeedSource that encodes the same numeric id and token as the input URL.

func TestProperty2_FeedSourcePreservesFeedIdentity(t *testing.T) {
	r := New(stubLogger{})
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		scheme := genScheme().Draw(t, "scheme")
		id := genNumericID().Draw(t, "id")
		token := genToken().Draw(t, "token")
		url := fmt.Sprintf("%s://kino.pub/podcast/get/%s/%s", scheme, id, token)

		src, err := r.Resolve(ctx, url)
		if err != nil {
			t.Fatalf("Resolve(%q) returned unexpected error: %v", url, err)
		}
		if src.ID != id {
			t.Fatalf("Resolve(%q).ID = %q, want %q", url, src.ID, id)
		}
		if src.Token != token {
			t.Fatalf("Resolve(%q).Token = %q, want %q", url, src.Token, token)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 3: Errors never produce a Feed_Source
// ---------------------------------------------------------------------------

// For any invalid input URL (empty, non-HTTP(S), wrong host, or unclassified
// path), Resolve returns a non-nil error and the returned FeedSource is empty.

func TestProperty3_ErrorsNeverProduceFeedSource(t *testing.T) {
	r := New(stubLogger{})
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		url := genInvalidURL().Draw(t, "url")

		src, err := r.Resolve(ctx, url)
		if err == nil {
			t.Fatalf("Resolve(%q) returned nil error, want non-nil for invalid URL", url)
		}
		if src != (domain.FeedSource{}) {
			t.Fatalf("Resolve(%q) returned non-empty FeedSource %+v on error", url, src)
		}
	})
}

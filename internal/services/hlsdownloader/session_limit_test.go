package hlsdownloader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestCDNSessionLimitResponse(t *testing.T) {
	for _, tc := range []struct {
		code    int
		body    string
		limited bool
	}{
		{403, "user session limit reached\n", true},
		{403, "Forbidden", false},
		{401, "user session limit reached", false},
	} {
		err := cdnStatusError(&http.Response{StatusCode: tc.code, Header: http.Header{"Retry-After": {"120"}}, Body: io.NopCloser(strings.NewReader(tc.body))})
		if errors.Is(err, domain.ErrCDNSessionLimit) != tc.limited {
			t.Fatalf("%+v: %v", tc, err)
		}
		if err.RetryAfter != 2*time.Minute {
			t.Fatalf("Retry-After lost: %v", err.RetryAfter)
		}
		if !tc.limited && strings.Contains(err.Error(), tc.body) {
			t.Fatal("unrecognized upstream body exposed")
		}
	}
}

func TestSessionLimitStopsManifestRetries(t *testing.T) {
	for _, master := range []bool{false, true} {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			http.Error(w, "user session limit reached", http.StatusForbidden)
		}))
		var err error
		if master {
			_, err = FetchMasterPlaylist(context.Background(), srv.Client(), srv.URL, domain.RequestAuth{}, nopLogger{})
		} else {
			_, err = FetchMediaPlaylist(context.Background(), srv.Client(), srv.URL, domain.RequestAuth{})
		}
		srv.Close()
		if !errors.Is(err, domain.ErrCDNSessionLimit) || hits.Load() != 1 {
			t.Fatalf("master=%v: hits=%d err=%v", master, hits.Load(), err)
		}
	}
}

func TestSessionLimitPausesOtherDownloadersAndRemainsCancelable(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "user session limit reached", http.StatusForbidden)
			return
		}
		io.WriteString(w, "segment")
	}))
	defer srv.Close()
	lim := NewLimiter(2, nopLogger{})
	now := time.Now()
	lim.now = func() time.Time { return now }
	a := New(srv.Client(), domain.RequestAuth{}, nopLogger{}, WithLimiter(lim))
	b := New(srv.Client(), domain.RequestAuth{}, nopLogger{}, WithLimiter(lim))
	out := filepath.Join(t.TempDir(), "segment.ts")
	_, err := a.downloadSegment(context.Background(), Segment{URL: srv.URL}, out, lim)
	if !errors.Is(err, domain.ErrCDNSessionLimit) || hits.Load() != 1 {
		t.Fatalf("session limit retried: hits=%d err=%v", hits.Load(), err)
	}
	if !lim.Stats().Throttled {
		t.Fatal("shared cooldown not visible")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = b.fetchSegment(ctx, srv.URL, out)
	if !errors.Is(err, context.DeadlineExceeded) || hits.Load() != 1 {
		t.Fatalf("request bypassed cooldown: hits=%d err=%v", hits.Load(), err)
	}
	now = now.Add(time.Minute + time.Second)
	if _, err = b.fetchSegment(context.Background(), srv.URL, out); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected one request after cooldown: %d", hits.Load())
	}
}

func TestSessionLimitHonorsLongerRetryAfter(t *testing.T) {
	lim := NewLimiter(1, nopLogger{})
	now := time.Now()
	lim.now = func() time.Time { return now }
	lim.sessionLimited(2 * time.Minute)
	if lim.sessionUntil.Sub(now) != 2*time.Minute {
		t.Fatal("Retry-After shortened")
	}
	lim.sessionLimited(0)
	if lim.sessionUntil.Sub(now) != 2*time.Minute {
		t.Fatal("existing cooldown shortened")
	}
}

func TestExplicitConcurrencyCeilingIncludesAdditionalTracks(t *testing.T) {
	lim := NewLimiterWithCeiling(16, 2, nopLogger{})
	lim.addTracks(8)
	lim.mu.Lock()
	for i := 0; i < 20; i++ {
		lim.liftCeilingLocked()
	}
	lim.mu.Unlock()
	stats := lim.Stats()
	if stats.Limit > 2 || stats.Ceiling > 2 {
		t.Fatalf("session budget exceeded: %+v", stats)
	}
}

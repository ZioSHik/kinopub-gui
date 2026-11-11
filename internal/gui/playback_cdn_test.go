package gui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/lib/logx"
	"github.com/ZioSHik/kinopub-gui/internal/services/hlsdownloader"
)

func playerCDNServer(rt auditRoundTripper) *Server {
	s := newHLSServer()
	s.settings = &settingsStore{cur: Settings{}}
	s.mgr = &JobManager{limiter: hlsdownloader.NewCDNLimiter(logx.New(nil))}
	s.hlsClientCached = &http.Client{Transport: rt}
	return s
}

func TestPlayerSharesSessionCooldownWithoutImmediateRetry(t *testing.T) {
	var hits atomic.Int32
	s := playerCDNServer(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		return &http.Response{StatusCode: 403, Header: http.Header{"Retry-After": {"120"}}, Body: io.NopCloser(strings.NewReader("user session limit reached"))}, nil
	})
	raw := "https://cdn.example/fragment.ts"
	w := httptest.NewRecorder()
	s.handleHLSProxy(w, httptest.NewRequest("GET", s.proxiedHLSURL(raw), nil))
	if w.Code != 403 || w.Body.String() != "user session limit reached" || w.Header().Get("Retry-After") != "120" {
		t.Fatalf("upstream error not preserved: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	if hits.Load() != 1 || !s.mgr.limiter.Stats().Throttled {
		t.Fatal("player bypassed global session protection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := s.fetchHLS(ctx, raw, "")
	if !errors.Is(err, context.DeadlineExceeded) || hits.Load() != 1 {
		t.Fatal("player retried during cooldown", err)
	}
	if stats := s.mgr.limiter.Stats(); stats.InFlight != 0 {
		t.Fatalf("leaked player request slot: %+v", stats)
	}
}

func TestPlayerBodyHoldsSharedSlotAndForwardsRange(t *testing.T) {
	s := playerCDNServer(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Range") != "bytes=10-19" {
			t.Error("Range not forwarded")
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": {"bytes 10-19/100"}}, Body: io.NopCloser(strings.NewReader("0123456789"))}, nil
	})
	resp, err := s.fetchHLS(context.Background(), "https://cdn.example/fragment.ts", "bytes=10-19")
	if err != nil {
		t.Fatal(err)
	}
	if stats := s.mgr.limiter.Stats(); stats.InFlight != 1 || !stats.Throttled {
		t.Fatalf("body lost slot/priority: %+v", stats)
	}
	if resp.StatusCode != 206 || resp.Header.Get("Content-Range") != "bytes 10-19/100" {
		t.Fatal("seek response changed")
	}
	resp.Body.Close()
	resp.Body.Close()
	if stats := s.mgr.limiter.Stats(); stats.InFlight != 0 || stats.Throttled {
		t.Fatalf("body close leaked priority: %+v", stats)
	}
}

func TestPlayerRateLimitRetriesWaitForSharedCooldown(t *testing.T) {
	for _, code := range []int{429, 503} {
		var hits atomic.Int32
		s := playerCDNServer(func(r *http.Request) (*http.Response, error) {
			hits.Add(1)
			return &http.Response{StatusCode: code, Header: http.Header{"Retry-After": {"120"}}, Body: io.NopCloser(strings.NewReader("busy"))}, nil
		})
		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
		_, err := s.fetchHLS(ctx, "https://cdn.example/video.ts", "")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) || hits.Load() != 1 || !s.mgr.limiter.Stats().Throttled {
			t.Fatalf("HTTP %d retry bypassed cooldown: hits=%d err=%v", code, hits.Load(), err)
		}
		if s.mgr.limiter.Stats().InFlight != 0 {
			t.Fatal("canceled retry kept its slot")
		}
	}
}

func TestPlayerLeaseLifecycleAndOriginGuard(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := NewServer("test", nil)
	request := func(body, origin string) int {
		r := httptest.NewRequest("POST", "http://127.0.0.1/api/player/lease", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if got := request(`{"id":"test-player","active":true}`, "https://foreign.example"); got != 403 {
		t.Fatal("foreign page obtained playback priority", got)
	}
	if got := request(`{"id":"bad","active":true}`, ""); got != 400 {
		t.Fatal("invalid lease accepted", got)
	}
	if s.mgr.limiter.Stats().Throttled {
		t.Fatal("invalid request changed priority")
	}
	if got := request(`{"id":"test-player","active":true}`, ""); got != 200 || !s.mgr.limiter.Stats().Throttled {
		t.Fatal("lease did not park downloads", got)
	}
	if got := request(`{"id":"test-player","active":false}`, ""); got != 200 || s.mgr.limiter.Stats().Throttled {
		t.Fatal("closing player did not resume downloads", got)
	}
}

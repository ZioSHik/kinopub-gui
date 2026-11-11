package hlsdownloader

import (
	"context"
	"errors"
	"fmt"
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

func TestCDNPolicyStartsFastAndRecoversAfterSessionLimit(t *testing.T) {
	l := NewCDNLimiter(nopLogger{})
	now := time.Unix(1700000000, 0)
	l.now = func() time.Time { return now }
	if l.current() != 4 {
		t.Fatal("healthy CDN should start with four requests")
	}
	l.addTracks(20)
	if l.current() != 4 || l.min != 2 {
		t.Fatal("track count forced unmeasured concurrency")
	}
	l.limit = 12
	l.sessionLimited(120 * time.Second)
	if l.current() != 2 || !l.Stats().Throttled {
		t.Fatal("session limit did not reset concurrency and pause")
	}
	now = now.Add(61 * time.Second)
	if !l.Stats().Throttled {
		t.Fatal("Retry-After was shortened")
	}
	now = now.Add(60 * time.Second)
	if l.Stats().Throttled {
		t.Fatal("cooldown did not expire")
	}
	l.advanceLocked(100, now)
	l.advanceLocked(150, now.Add(probeWindow))
	l.advanceLocked(200, now.Add(2*probeWindow))
	if l.current() <= 4 {
		t.Fatalf("healthy throughput stayed capped after cooldown: %d", l.current())
	}
}

func TestCDNSettledRateIsReprobedAndUnhelpfulProbeIsRolledBack(t *testing.T) {
	for _, improved := range []bool{false, true} {
		t.Run(fmt.Sprint(improved), func(t *testing.T) {
			l, advance := newTestLimiter(4, 1)
			l.advanceLocked(100, l.now()) // initial probe: five
			l.advanceLocked(100, l.now()) // plateau
			base := l.current()
			advance(settleFor + time.Second)
			l.advanceLocked(100, l.now())
			if l.current() != base+1 {
				t.Fatal("constant throughput prevented re-probing forever")
			}
			rate := 100.0
			if improved {
				rate = 150
			}
			l.advanceLocked(rate, l.now().Add(probeWindow))
			if improved && l.current() <= base+1 {
				t.Fatal("useful extra capacity was discarded")
			}
			if !improved && l.current() != base {
				t.Fatal("extra sockets bought no speed but were kept")
			}
		})
	}
}

func TestPlaybackDoesNotDeadlockAlreadyAdmittedRequests(t *testing.T) {
	l := NewCDNLimiter(nopLogger{})
	l.limit = 1
	if err := l.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.SetPlaybackLease("player-one", true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// This request already owns the only slot; it must be allowed to finish.
	if err := l.waitSession(ctx); err != nil {
		t.Fatal("player and in-flight download deadlocked", err)
	}
	l.done(100)
	release, err := l.AcquirePlayback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release() // body may be closed twice
	blocked, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := l.acquire(blocked); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("download bypassed player lease: %v", err)
	}
	l.SetPlaybackLease("player-one", false)
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	l.done(100)
	if stats := l.Stats(); stats.InFlight != 0 || stats.Throttled {
		t.Fatalf("leaked playback slot: %+v", stats)
	}
}

func TestPlaybackLeasesExpireIndependently(t *testing.T) {
	l, advance := newTestLimiter(4, 1)
	l.SetPlaybackLease("one", true)
	advance(20 * time.Second)
	l.SetPlaybackLease("two", true)
	l.SetPlaybackLease("one", false)
	if !l.Stats().Throttled {
		t.Fatal("closing one tab resumed downloads under another")
	}
	advance(playbackLeaseTTL)
	if l.Stats().Throttled {
		t.Fatal("crashed tab parked downloads forever")
	}
	if len(l.playbackLeases) != 0 {
		t.Fatal("expired leases retained")
	}
}

func TestPlaybackCancellationDoesNotLeakPriority(t *testing.T) {
	l := NewCDNLimiter(nopLogger{})
	l.sessionLimited(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.AcquirePlayback(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if l.playbackRequests != 0 || l.Stats().InFlight != 0 {
		t.Fatal("canceled playback kept priority/slot")
	}
}

func TestCDNResponseClassificationSharesFullCooldownAndPreservesBody(t *testing.T) {
	for _, tc := range []struct {
		code    int
		body    string
		limited bool
	}{
		{403, "user session limit reached", true}, {403, "Forbidden", false},
		{401, "Unauthorized", false}, {429, "Too many requests", true}, {503, "Unavailable", true},
	} {
		t.Run(fmt.Sprint(tc.code)+tc.body, func(t *testing.T) {
			l, advance := newTestLimiter(8, 1)
			resp := &http.Response{StatusCode: tc.code, Header: http.Header{"Retry-After": {"120"}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			l.ObserveResponse(resp)
			body, _ := io.ReadAll(resp.Body)
			if string(body) != tc.body {
				t.Fatal("classification consumed the proxy response")
			}
			if l.Stats().Throttled != tc.limited {
				t.Fatal("wrong cooldown classification")
			}
			advance(119 * time.Second)
			if l.Stats().Throttled != tc.limited {
				t.Fatal("Retry-After shortened")
			}
			advance(2 * time.Second)
			if l.Stats().Throttled {
				t.Fatal("cooldown did not expire")
			}
		})
	}
}

func TestCDNDownloadsSerializeEpisodesWithoutCappingFragmentsAtTwo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	arrived := make(chan struct{}, 8)
	release := make(chan struct{})
	var secondMaster atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/one.m3u8", "/two.m3u8":
			if r.URL.Path == "/two.m3u8" {
				secondMaster.Add(1)
			}
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000,RESOLUTION=1280x720\n/video.m3u8\n")
		case "/video.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\n/1.ts\n#EXTINF:1,\n/2.ts\n#EXTINF:1,\n/3.ts\n#EXTINF:1,\n/4.ts\n#EXT-X-ENDLIST\n")
		default:
			arrived <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			case <-ctx.Done():
				return
			}
			fmt.Fprint(w, "fragment")
		}
	}))
	defer srv.Close()
	defer cancel()
	l := NewCDNLimiter(nopLogger{})
	done := make(chan error, 2)
	dir := t.TempDir()
	start := func(name string) {
		go func() {
			// Separate Downloader instances model two independently scheduled jobs.
			d := newTestDownloader(t, srv.Client(), WithLimiter(l))
			_, err := d.DownloadEpisode(ctx, srv.URL+"/"+name+".m3u8", "max", filepath.Join(dir, name+".ts"), domain.EpisodeKey{}, nil)
			done <- err
		}()
	}
	start("one")
	for i := 0; i < 4; i++ {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("healthy download did not use four simultaneous requests")
		}
	}
	start("two")
	// Audio discovery is also forbidden from opening another session.
	probeCtx, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stop()
	d := newTestDownloader(t, srv.Client(), WithLimiter(l))
	if _, err := d.ListAudioTracks(probeCtx, srv.URL+"/two.m3u8", "max"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("discovery bypassed episode admission", err)
	}
	if secondMaster.Load() != 0 {
		t.Fatal("second CDN episode started before first completed")
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if secondMaster.Load() != 1 {
		t.Fatal("second episode did not resume")
	}
	if l.Stats().InFlight != 0 || l.episodeActive {
		t.Fatal("download admission leaked")
	}
}

func TestSegmentRetryReturnsSlotDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		http.Error(w, "busy", 429)
	}))
	defer srv.Close()
	l := NewCDNLimiter(nopLogger{})
	d := newTestDownloader(t, srv.Client(), WithLimiter(l))
	done := make(chan error, 1)
	path := filepath.Join(t.TempDir(), "segment.ts")
	go func() { _, err := d.downloadSegment(ctx, Segment{URL: srv.URL}, path, l); done <- err }()
	for !l.Stats().Throttled || l.Stats().InFlight != 0 {
		select {
		case <-ctx.Done():
			t.Fatal("retry kept a network slot during backoff")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestAllManifestAndSubtitleRequestsShareRateLimit(t *testing.T) {
	for _, stage := range []string{"master", "media", "external subtitles", "subtitle playlist", "subtitle segment"} {
		t.Run(stage, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Retry-After", "120")
				http.Error(w, "busy", 429)
			}))
			defer srv.Close()
			l := NewCDNLimiter(nopLogger{})
			d := newTestDownloader(t, srv.Client(), WithLimiter(l))
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			var err error
			switch stage {
			case "master":
				_, err = d.ListAudioTracks(ctx, srv.URL, "max")
			case "media":
				_, err = fetchMediaPlaylist(ctx, srv.Client(), srv.URL, domain.RequestAuth{}, l)
			case "external subtitles":
				_, err = d.DownloadSubtitles(ctx, []domain.SubtitleSource{{URL: srv.URL + "/sub.vtt"}}, t.TempDir())
			case "subtitle playlist":
				_, err = d.downloadSubtitleRendition(ctx, SubtitleRendition{URI: srv.URL}, t.TempDir(), 0)
			case "subtitle segment":
				err = d.downloadSubtitle(ctx, srv.URL, filepath.Join(t.TempDir(), "sub.vtt"))
			}
			if err == nil || hits.Load() != 1 || !l.Stats().Throttled || l.Stats().InFlight != 0 {
				t.Fatalf("request did not share cooldown/release slot: err=%v hits=%d stats=%+v", err, hits.Load(), l.Stats())
			}
		})
	}
}

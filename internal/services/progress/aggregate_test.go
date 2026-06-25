package progress

import (
	"strings"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// ---------------------------------------------------------------------------
// seriesPercent
// ---------------------------------------------------------------------------

func TestSeriesPercent(t *testing.T) {
	tests := []struct {
		name           string
		total          int
		completedTotal int
		want           int
	}{
		{"zero total", 0, 0, 0},
		{"zero total with completed", 0, 5, 0},
		{"none done", 10, 0, 0},
		{"half", 10, 5, 50},
		{"floor", 3, 1, 33},
		{"all", 10, 10, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &LiveReporter{
				plan:           domain.SeriesPlan{Total: tt.total},
				completedTotal: tt.completedTotal,
			}
			if got := r.seriesPercent(); got != tt.want {
				t.Fatalf("seriesPercent=%d want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// episodePercent — averages track progress
// ---------------------------------------------------------------------------

func TestEpisodePercent(t *testing.T) {
	r := &LiveReporter{}

	t.Run("no tracks returns 0", func(t *testing.T) {
		ep := &episodeState{tracks: map[domain.TrackRef]int{}}
		if got := r.episodePercent(ep); got != 0 {
			t.Fatalf("got %d want 0", got)
		}
	})

	t.Run("single track", func(t *testing.T) {
		ep := &episodeState{tracks: map[domain.TrackRef]int{
			{Kind: domain.TrackVideo}: 50,
		}}
		if got := r.episodePercent(ep); got != 50 {
			t.Fatalf("got %d want 50", got)
		}
	})

	t.Run("two tracks averaged", func(t *testing.T) {
		ep := &episodeState{tracks: map[domain.TrackRef]int{
			{Kind: domain.TrackVideo}:           100,
			{Kind: domain.TrackAudio, Index: 0}: 50,
		}}
		// sum=150, total=200 -> 75
		if got := r.episodePercent(ep); got != 75 {
			t.Fatalf("got %d want 75", got)
		}
	})

	t.Run("three tracks floor", func(t *testing.T) {
		ep := &episodeState{tracks: map[domain.TrackRef]int{
			{Kind: domain.TrackVideo}:           100,
			{Kind: domain.TrackAudio, Index: 0}: 100,
			{Kind: domain.TrackAudio, Index: 1}: 0,
		}}
		// sum=200, total=300 -> floor(66.66)=66
		if got := r.episodePercent(ep); got != 66 {
			t.Fatalf("got %d want 66", got)
		}
	})

	t.Run("all complete", func(t *testing.T) {
		ep := &episodeState{tracks: map[domain.TrackRef]int{
			{Kind: domain.TrackVideo}:           100,
			{Kind: domain.TrackAudio, Index: 0}: 100,
		}}
		if got := r.episodePercent(ep); got != 100 {
			t.Fatalf("got %d want 100", got)
		}
	})
}

// ---------------------------------------------------------------------------
// episodeStats — composition of segments, speed, size, ETA
// ---------------------------------------------------------------------------

func TestEpisodeStats(t *testing.T) {
	r := &LiveReporter{}

	t.Run("empty episode no parts", func(t *testing.T) {
		ep := &episodeState{}
		if got := r.episodeStats(ep, 0); got != "" {
			t.Fatalf("want empty, got %q", got)
		}
	})

	t.Run("segments only", func(t *testing.T) {
		ep := &episodeState{doneSegments: 3, totalSegments: 10}
		got := r.episodeStats(ep, 30)
		if !strings.Contains(got, "3/10 seg") {
			t.Fatalf("missing segments: %q", got)
		}
	})

	t.Run("speed shown when positive", func(t *testing.T) {
		ep := &episodeState{speed: 2 * 1024 * 1024} // 2 MB/s
		got := r.episodeStats(ep, 10)
		if !strings.Contains(got, "2.0M/s") {
			t.Fatalf("missing speed: %q", got)
		}
	})

	t.Run("speed zero not shown", func(t *testing.T) {
		ep := &episodeState{speed: 0}
		got := r.episodeStats(ep, 10)
		if strings.Contains(got, "/s") {
			t.Fatalf("speed should be hidden: %q", got)
		}
	})

	t.Run("exact total size", func(t *testing.T) {
		ep := &episodeState{downloadedBytes: 1024 * 1024, totalBytes: 4 * 1024 * 1024}
		got := r.episodeStats(ep, 25)
		if !strings.Contains(got, "1.0M/4.0M") {
			t.Fatalf("missing size: %q", got)
		}
		if strings.Contains(got, "~") {
			t.Fatalf("exact size should not have ~: %q", got)
		}
	})

	t.Run("approx total size has tilde", func(t *testing.T) {
		ep := &episodeState{
			downloadedBytes: 1024 * 1024,
			totalBytes:      4 * 1024 * 1024,
			sizeIsApprox:    true,
		}
		got := r.episodeStats(ep, 25)
		if !strings.Contains(got, "1.0M/~4.0M") {
			t.Fatalf("approx size missing tilde: %q", got)
		}
	})

	t.Run("downloaded only when total unknown", func(t *testing.T) {
		ep := &episodeState{downloadedBytes: 512 * 1024}
		got := r.episodeStats(ep, 10)
		if !strings.Contains(got, "512K") {
			t.Fatalf("missing downloaded: %q", got)
		}
		if strings.Contains(got, "/") {
			t.Fatalf("should not have ratio: %q", got)
		}
	})

	t.Run("ETA included via speed", func(t *testing.T) {
		ep := &episodeState{
			downloadedBytes: 1024 * 1024,
			totalBytes:      3 * 1024 * 1024,
			speed:           1024 * 1024, // 1 MB/s, 2MB remaining -> 2s
		}
		got := r.episodeStats(ep, 33)
		if !strings.Contains(got, "ETA 2s") {
			t.Fatalf("ETA missing/wrong: %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// episodeETA
// ---------------------------------------------------------------------------

func TestEpisodeETA(t *testing.T) {
	r := &LiveReporter{}

	t.Run("speed-based estimate", func(t *testing.T) {
		ep := &episodeState{
			downloadedBytes: 1024 * 1024,
			totalBytes:      11 * 1024 * 1024, // 10MB remaining
			speed:           1024 * 1024,      // 1 MB/s -> 10s
		}
		if got := r.episodeETA(ep, 9); got != "10s" {
			t.Fatalf("got %q want 10s", got)
		}
	})

	t.Run("at 100 percent no ETA via speed", func(t *testing.T) {
		ep := &episodeState{
			downloadedBytes: 10 * 1024 * 1024,
			totalBytes:      10 * 1024 * 1024,
			speed:           1024 * 1024,
		}
		if got := r.episodeETA(ep, 100); got != "" {
			t.Fatalf("got %q want empty at 100pct", got)
		}
	})

	t.Run("no speed no bytes returns empty", func(t *testing.T) {
		ep := &episodeState{}
		if got := r.episodeETA(ep, 50); got != "" {
			t.Fatalf("got %q want empty", got)
		}
	})

	t.Run("time-based fallback", func(t *testing.T) {
		// firstProgressAt was 10s ago at 10%, now at 20% -> 10% took 10s,
		// remaining 80% -> ~80s total estimate; remaining = 90s total - 10s = ...
		// totalEstimate = elapsed * (100 - firstPct) / pctDone
		//   = 10s * (100-10) / (20-10) = 10s * 90/10 = 90s; remaining = 90-10 = 80s
		ep := &episodeState{
			firstProgressAt:  time.Now().Add(-10 * time.Second),
			firstProgressPct: 10,
		}
		got := r.episodeETA(ep, 20)
		// Allow small jitter; expect around 80s (formatDuration -> "1m20s" or close)
		if got == "" {
			t.Fatalf("expected non-empty time-based ETA")
		}
		// Should be roughly a minute+ given the math.
		if !strings.Contains(got, "m") && !strings.HasSuffix(got, "s") {
			t.Fatalf("unexpected ETA format: %q", got)
		}
	})

	t.Run("time-based skipped at low percent", func(t *testing.T) {
		ep := &episodeState{
			firstProgressAt:  time.Now().Add(-10 * time.Second),
			firstProgressPct: 1,
		}
		// epPct=2 means epPct>2 is false -> no time-based ETA, no speed -> empty
		if got := r.episodeETA(ep, 2); got != "" {
			t.Fatalf("got %q want empty at pct=2", got)
		}
	})
}

// ---------------------------------------------------------------------------
// trackStats
// ---------------------------------------------------------------------------

func TestTrackStats(t *testing.T) {
	r := &LiveReporter{}

	t.Run("segments and approx bytes", func(t *testing.T) {
		ti := domain.TrackProgressInfo{
			Label:            "Video",
			DoneSegments:     5,
			TotalSegments:    10,
			DownloadedBytes:  2 * 1024 * 1024,
			ApproxTotalBytes: 4 * 1024 * 1024,
		}
		got := r.trackStats(ti)
		if !strings.Contains(got, "5/10 seg") {
			t.Fatalf("missing seg: %q", got)
		}
		if !strings.Contains(got, "2.0M/~4.0M") {
			t.Fatalf("missing approx bytes: %q", got)
		}
	})

	t.Run("downloaded only when no approx total", func(t *testing.T) {
		ti := domain.TrackProgressInfo{
			DoneSegments:    1,
			TotalSegments:   3,
			DownloadedBytes: 512 * 1024,
		}
		got := r.trackStats(ti)
		if !strings.Contains(got, "1/3 seg") {
			t.Fatalf("missing seg: %q", got)
		}
		if !strings.Contains(got, "512K") || strings.Contains(got, "/~") {
			t.Fatalf("downloaded-only wrong: %q", got)
		}
	})

	t.Run("no bytes shows only segments", func(t *testing.T) {
		ti := domain.TrackProgressInfo{DoneSegments: 0, TotalSegments: 8}
		got := r.trackStats(ti)
		if got != "0/8 seg" {
			t.Fatalf("got %q want '0/8 seg'", got)
		}
	})
}

package progress

import (
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// liveWithEpisode returns a non-ticking LiveReporter (Start not called, so no
// goroutine) seeded with a single in-progress episode, ready for direct
// speed-math testing. We avoid Start()/Stop() here so the tick loop does not
// race with our deterministic manipulation of lastSpeedTime.
func liveWithEpisode(key domain.EpisodeKey) (*LiveReporter, *episodeState) {
	ep := &episodeState{
		tracks:    map[domain.TrackRef]int{},
		startTime: time.Now(),
	}
	r := &LiveReporter{
		isTTY:           false,
		currentEpisodes: map[domain.EpisodeKey]*episodeState{key: ep},
	}
	return r, ep
}

// ---------------------------------------------------------------------------
// ByteProgress — speed calculation
// ---------------------------------------------------------------------------

func TestByteProgress_FirstCallSeedsBaseline(t *testing.T) {
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r, ep := liveWithEpisode(key)

	r.ByteProgress(key, 1000, 5000)

	if ep.downloadedBytes != 1000 || ep.totalBytes != 5000 {
		t.Fatalf("bytes not recorded: %d/%d", ep.downloadedBytes, ep.totalBytes)
	}
	// First call only seeds the speed baseline; speed stays 0.
	if ep.speed != 0 {
		t.Fatalf("speed should be 0 after first sample, got %v", ep.speed)
	}
	if ep.lastSpeedBytes != 1000 {
		t.Fatalf("lastSpeedBytes=%d want 1000", ep.lastSpeedBytes)
	}
	if ep.lastSpeedTime.IsZero() {
		t.Fatalf("lastSpeedTime should be set")
	}
}

func TestByteProgress_ComputesSpeedAfterOneSecond(t *testing.T) {
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r, ep := liveWithEpisode(key)

	// Seed baseline manually: 2s ago at 0 bytes.
	ep.lastSpeedTime = time.Now().Add(-2 * time.Second)
	ep.lastSpeedBytes = 0

	// Now downloaded 2,000,000 bytes over ~2s -> ~1,000,000 B/s.
	r.ByteProgress(key, 2_000_000, 10_000_000)

	if ep.speed <= 0 {
		t.Fatalf("speed should be positive, got %v", ep.speed)
	}
	// First EMA: speed was 0 so it takes the instant value (~1MB/s).
	if ep.speed < 900_000 || ep.speed > 1_100_000 {
		t.Fatalf("speed=%v out of expected ~1MB/s range", ep.speed)
	}
	// Baseline advanced.
	if ep.lastSpeedBytes != 2_000_000 {
		t.Fatalf("lastSpeedBytes=%d want 2000000", ep.lastSpeedBytes)
	}
}

func TestByteProgress_EMASmoothing(t *testing.T) {
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r, ep := liveWithEpisode(key)

	// Pre-existing smoothed speed.
	ep.speed = 1_000_000
	ep.lastSpeedTime = time.Now().Add(-1 * time.Second)
	ep.lastSpeedBytes = 0

	// 2,000,000 bytes in ~1s -> instant 2MB/s. EMA: 0.7*1M + 0.3*2M = 1.3M.
	r.ByteProgress(key, 2_000_000, 10_000_000)

	if ep.speed < 1_200_000 || ep.speed > 1_400_000 {
		t.Fatalf("EMA speed=%v want ~1.3MB/s", ep.speed)
	}
}

func TestByteProgress_StallDecaysSpeedToZero(t *testing.T) {
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r, ep := liveWithEpisode(key)

	// Small existing speed, no bytes transferred since last sample.
	ep.speed = 2000 // below the 1024*... but >1024 so decay applies
	ep.lastSpeedTime = time.Now().Add(-1 * time.Second)
	ep.lastSpeedBytes = 5000

	// Same byte count -> byteDiff == 0 -> decay *0.3 -> 600 < 1024 -> 0.
	r.ByteProgress(key, 5000, 10000)

	if ep.speed != 0 {
		t.Fatalf("stalled speed should decay to 0, got %v", ep.speed)
	}
}

func TestByteProgress_NoRecomputeWithinOneSecond(t *testing.T) {
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r, ep := liveWithEpisode(key)

	ep.speed = 500_000
	now := time.Now()
	ep.lastSpeedTime = now.Add(-100 * time.Millisecond) // < 1s
	ep.lastSpeedBytes = 1000

	r.ByteProgress(key, 9000, 10000)

	// Speed unchanged because elapsed < 1s; baseline not advanced.
	if ep.speed != 500_000 {
		t.Fatalf("speed should not recompute under 1s, got %v", ep.speed)
	}
	if ep.lastSpeedBytes != 1000 {
		t.Fatalf("baseline should not advance, got %d", ep.lastSpeedBytes)
	}
	// But byte counters do update.
	if ep.downloadedBytes != 9000 || ep.totalBytes != 10000 {
		t.Fatalf("byte counters not updated: %d/%d", ep.downloadedBytes, ep.totalBytes)
	}
}

// ---------------------------------------------------------------------------
// SegmentProgress — speed calc + segment tracking + approx flag
// ---------------------------------------------------------------------------

func TestSegmentProgress_RecordsSegmentsAndApproxFlag(t *testing.T) {
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r, ep := liveWithEpisode(key)

	r.SegmentProgress(key, 3, 12, 300_000, 1_200_000)

	if ep.doneSegments != 3 || ep.totalSegments != 12 {
		t.Fatalf("segments wrong: %d/%d", ep.doneSegments, ep.totalSegments)
	}
	if !ep.sizeIsApprox {
		t.Fatalf("sizeIsApprox should be true for segment progress")
	}
	if ep.downloadedBytes != 300_000 || ep.totalBytes != 1_200_000 {
		t.Fatalf("bytes wrong: %d/%d", ep.downloadedBytes, ep.totalBytes)
	}
	// First call seeds baseline, no speed yet.
	if ep.speed != 0 {
		t.Fatalf("speed should be 0 after first segment sample, got %v", ep.speed)
	}
}

func TestSegmentProgress_ComputesSpeed(t *testing.T) {
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r, ep := liveWithEpisode(key)

	ep.lastSpeedTime = time.Now().Add(-1 * time.Second)
	ep.lastSpeedBytes = 0
	ep.sizeIsApprox = true

	r.SegmentProgress(key, 5, 10, 1_000_000, 2_000_000)

	if ep.speed < 900_000 || ep.speed > 1_100_000 {
		t.Fatalf("segment speed=%v want ~1MB/s", ep.speed)
	}
}

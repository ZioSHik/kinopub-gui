package downloader

import (
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestParseOutTime(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"00:00:00.000000", 0},
		{"00:01:00.000000", 1 * time.Minute},
		{"01:00:00.000000", 1 * time.Hour},
		{"01:23:45.678900", 1*time.Hour + 23*time.Minute + 45*time.Second + 678900*time.Microsecond},
		{"00:00:30.500000", 30*time.Second + 500000*time.Microsecond},
		{"00:00:00.100000", 100 * time.Millisecond},
		{"02:30:00.000000", 2*time.Hour + 30*time.Minute},
		// Edge cases.
		{"", 0},
		{"invalid", 0},
		{"00:00", 0},
		{"aa:bb:cc.dddddd", 0},
		// Without fractional part.
		{"00:01:30", 1*time.Minute + 30*time.Second},
		// Short fractional part (should be padded).
		{"00:00:01.5", 1*time.Second + 500000*time.Microsecond},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseOutTime(tt.input)
			if got != tt.want {
				t.Errorf("parseOutTime(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestProgressParser_ParsesOutTime(t *testing.T) {
	sink := &testProgressSink{}
	key := domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}
	duration := 2 * time.Minute

	parser := newProgressParser(sink, key, track, duration)

	// Write ffmpeg -progress output.
	input := "out_time_us=60000000\nout_time=00:01:00.000000\nprogress=continue\n"
	_, err := parser.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Close to flush.
	if err := parser.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Should have received progress updates.
	if len(sink.updates) == 0 {
		t.Fatal("expected progress updates")
	}

	// 60s out of 120s = 50%.
	found50 := false
	for _, u := range sink.updates {
		if u.percent == 50 {
			found50 = true
			if u.key != key {
				t.Errorf("wrong key: got %v, want %v", u.key, key)
			}
			if u.track != track {
				t.Errorf("wrong track: got %v, want %v", u.track, track)
			}
		}
	}
	if !found50 {
		t.Errorf("expected 50%% progress, got: %v", sink.updates)
	}
}

func TestProgressParser_ClampsTo100(t *testing.T) {
	sink := &testProgressSink{}
	key := domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}
	duration := 1 * time.Minute

	parser := newProgressParser(sink, key, track, duration)

	// Report time beyond duration.
	input := "out_time=00:02:00.000000\nprogress=continue\n"
	_, err := parser.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if err := parser.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Should be clamped to 100%.
	if len(sink.updates) == 0 {
		t.Fatal("expected progress updates")
	}
	for _, u := range sink.updates {
		if u.percent > 100 {
			t.Errorf("percent should be clamped to 100, got %d", u.percent)
		}
	}
}

func TestProgressParser_ZeroDuration_NoUpdates(t *testing.T) {
	sink := &testProgressSink{}
	key := domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}

	parser := newProgressParser(sink, key, track, 0)

	input := "out_time=00:01:00.000000\nprogress=continue\n"
	_, err := parser.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if err := parser.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// No updates should be emitted when duration is 0.
	if len(sink.updates) != 0 {
		t.Errorf("expected no updates with zero duration, got %d", len(sink.updates))
	}
}

func TestProgressParser_NilSink_NoPanic(t *testing.T) {
	key := domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}

	parser := newProgressParser(nil, key, track, 2*time.Minute)

	input := "out_time=00:01:00.000000\nprogress=continue\n"
	_, err := parser.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if err := parser.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	// Should not panic.
}

func TestProgressParser_OutTimeUS(t *testing.T) {
	sink := &testProgressSink{}
	key := domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}
	duration := 100 * time.Second

	parser := newProgressParser(sink, key, track, duration)

	// 25 seconds in microseconds.
	input := "out_time_us=25000000\nprogress=continue\n"
	_, err := parser.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if err := parser.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// 25s / 100s = 25%.
	found25 := false
	for _, u := range sink.updates {
		if u.percent == 25 {
			found25 = true
		}
	}
	if !found25 {
		t.Errorf("expected 25%% progress, got: %v", sink.updates)
	}
}

func TestProgressParser_MultipleBlocks(t *testing.T) {
	sink := &testProgressSink{}
	key := domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}
	duration := 4 * time.Minute

	parser := newProgressParser(sink, key, track, duration)

	// Simulate multiple progress blocks.
	blocks := []string{
		"out_time=00:01:00.000000\nprogress=continue\n",
		"out_time=00:02:00.000000\nprogress=continue\n",
		"out_time=00:03:00.000000\nprogress=continue\n",
		"out_time=00:04:00.000000\nprogress=end\n",
	}

	for _, block := range blocks {
		_, err := parser.Write([]byte(block))
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}

	if err := parser.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Should have received multiple updates: 25%, 50%, 75%, 100%.
	percentsSeen := make(map[int]bool)
	for _, u := range sink.updates {
		percentsSeen[u.percent] = true
	}

	for _, expected := range []int{25, 50, 75, 100} {
		if !percentsSeen[expected] {
			t.Errorf("expected %d%% progress, not found in updates", expected)
		}
	}
}

func TestProgressParser_IgnoresUnknownKeys(t *testing.T) {
	sink := &testProgressSink{}
	key := domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}
	duration := 2 * time.Minute

	parser := newProgressParser(sink, key, track, duration)

	// Mix of known and unknown keys.
	input := "bitrate=1234.5kbits/s\ntotal_size=12345\nout_time=00:01:00.000000\nspeed=1.5x\nprogress=continue\n"
	_, err := parser.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if err := parser.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Should still get the 50% update from out_time.
	if len(sink.updates) == 0 {
		t.Fatal("expected progress updates")
	}
	found50 := false
	for _, u := range sink.updates {
		if u.percent == 50 {
			found50 = true
		}
	}
	if !found50 {
		t.Errorf("expected 50%% progress from out_time")
	}
}

package domain

import (
	"testing"
	"time"
)

func TestSelection_Matches(t *testing.T) {
	tests := []struct {
		name string
		sel  Selection
		n    int
		want bool
	}{
		{"empty selection matches nothing", Selection{}, 1, false},
		{"empty selection matches nothing (zero)", Selection{}, 0, false},
		{"All matches any positive", Selection{All: true}, 5, true},
		{"All matches zero", Selection{All: true}, 0, true},
		{"All matches negative", Selection{All: true}, -3, true},
		{"value present", Selection{Values: map[int]bool{2: true}}, 2, true},
		{"value absent", Selection{Values: map[int]bool{2: true}}, 3, false},
		{"value explicitly false", Selection{Values: map[int]bool{2: false}}, 2, false},
		{"in range lo", Selection{Ranges: []SelectionRange{{Lo: 3, Hi: 5}}}, 3, true},
		{"in range hi", Selection{Ranges: []SelectionRange{{Lo: 3, Hi: 5}}}, 5, true},
		{"in range mid", Selection{Ranges: []SelectionRange{{Lo: 3, Hi: 5}}}, 4, true},
		{"below range", Selection{Ranges: []SelectionRange{{Lo: 3, Hi: 5}}}, 2, false},
		{"above range", Selection{Ranges: []SelectionRange{{Lo: 3, Hi: 5}}}, 6, false},
		{"single-point range", Selection{Ranges: []SelectionRange{{Lo: 7, Hi: 7}}}, 7, true},
		{"inverted range matches nothing", Selection{Ranges: []SelectionRange{{Lo: 5, Hi: 3}}}, 4, false},
		{"multiple ranges first", Selection{Ranges: []SelectionRange{{Lo: 1, Hi: 2}, {Lo: 8, Hi: 9}}}, 1, true},
		{"multiple ranges second", Selection{Ranges: []SelectionRange{{Lo: 1, Hi: 2}, {Lo: 8, Hi: 9}}}, 9, true},
		{"multiple ranges gap", Selection{Ranges: []SelectionRange{{Lo: 1, Hi: 2}, {Lo: 8, Hi: 9}}}, 5, false},
		{"value and range combined hits value", Selection{Values: map[int]bool{100: true}, Ranges: []SelectionRange{{Lo: 1, Hi: 2}}}, 100, true},
		{"value and range combined hits range", Selection{Values: map[int]bool{100: true}, Ranges: []SelectionRange{{Lo: 1, Hi: 2}}}, 2, true},
		{"All overrides empty values/ranges", Selection{All: true, Values: map[int]bool{}, Ranges: nil}, 42, true},
		{"All true even if value not present", Selection{All: true, Values: map[int]bool{1: true}}, 99, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sel.Matches(tt.n); got != tt.want {
				t.Errorf("Selection.Matches(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}

func TestRequestAuth_IsZero(t *testing.T) {
	tests := []struct {
		name string
		auth RequestAuth
		want bool
	}{
		{"zero value", RequestAuth{}, true},
		{"empty headers map still zero", RequestAuth{Headers: map[string]string{}}, true},
		{"nil headers zero", RequestAuth{Headers: nil}, true},
		{"cookie set", RequestAuth{Cookie: "x=1"}, false},
		{"user-agent set", RequestAuth{UserAgent: "Mozilla"}, false},
		{"headers set", RequestAuth{Headers: map[string]string{"X-Foo": "bar"}}, false},
		{"all set", RequestAuth{Cookie: "c", UserAgent: "ua", Headers: map[string]string{"a": "b"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.auth.IsZero(); got != tt.want {
				t.Errorf("RequestAuth.IsZero() = %v, want %v (auth=%+v)", got, tt.want, tt.auth)
			}
		})
	}
}

func TestField_F(t *testing.T) {
	f := F("count", 7)
	if f.Key != "count" {
		t.Errorf("F key = %q, want %q", f.Key, "count")
	}
	if v, ok := f.Value.(int); !ok || v != 7 {
		t.Errorf("F value = %v (%T), want int 7", f.Value, f.Value)
	}
	// nil value is preserved.
	fn := F("k", nil)
	if fn.Value != nil {
		t.Errorf("F(nil) value = %v, want nil", fn.Value)
	}
	// string value.
	fs := F("name", "abc")
	if fs.Value.(string) != "abc" {
		t.Errorf("F string value = %v, want abc", fs.Value)
	}
}

// Pin the iota-based enum constants so an accidental reorder is caught.
func TestEnumConstants(t *testing.T) {
	// VerbosityNormal must stay the zero value so an unset config defaults to
	// normal and an explicit "quiet" is not mistaken for "unset" (see config.go).
	if VerbosityNormal != 0 || VerbosityQuiet != 1 || VerbosityVerbose != 2 {
		t.Errorf("Verbosity constants drifted: normal=%d quiet=%d verbose=%d", VerbosityNormal, VerbosityQuiet, VerbosityVerbose)
	}
	if ProxyDirect != 0 || ProxySystem != 1 || ProxyExplicit != 2 {
		t.Errorf("ProxyMode constants drifted: %d %d %d", ProxyDirect, ProxySystem, ProxyExplicit)
	}
	if ContainerMKV != 0 || ContainerMP4 != 1 {
		t.Errorf("Container constants drifted: %d %d", ContainerMKV, ContainerMP4)
	}
	if LevelDebug != 0 || LevelInfo != 1 || LevelWarn != 2 || LevelError != 3 {
		t.Errorf("Level constants drifted: %d %d %d %d", LevelDebug, LevelInfo, LevelWarn, LevelError)
	}
	if ClassNonRetryable != 0 || ClassRetryable != 1 || ClassAuth != 2 {
		t.Errorf("ErrorClass constants drifted: %d %d %d", ClassNonRetryable, ClassRetryable, ClassAuth)
	}
	if MediaHLS != 0 || MediaProgressive != 1 {
		t.Errorf("MediaKind constants drifted: %d %d", MediaHLS, MediaProgressive)
	}
	if TrackVideo != 0 || TrackAudio != 1 || TrackSubtitle != 2 {
		t.Errorf("TrackKind constants drifted: %d %d %d", TrackVideo, TrackAudio, TrackSubtitle)
	}
	if EpisodePending != 0 || EpisodeRunning != 1 || EpisodeCompleted != 2 || EpisodeFailed != 3 {
		t.Errorf("EpisodeState constants drifted: %d %d %d %d", EpisodePending, EpisodeRunning, EpisodeCompleted, EpisodeFailed)
	}
}

// Quality zero value means auto/highest per its doc comment.
func TestQuality_ZeroValue(t *testing.T) {
	var q Quality
	if q != "" {
		t.Errorf("zero Quality = %q, want empty", q)
	}
}

func TestRunConfig_GracePeriodType(t *testing.T) {
	// Ensure GracePeriod is a time.Duration (compile-time guard + simple value check).
	cfg := RunConfig{GracePeriod: 30 * time.Second}
	if cfg.GracePeriod != 30*time.Second {
		t.Errorf("GracePeriod = %v", cfg.GracePeriod)
	}
}

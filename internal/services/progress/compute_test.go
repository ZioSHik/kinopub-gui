package progress

import (
	"strings"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// ---------------------------------------------------------------------------
// computePercent
// ---------------------------------------------------------------------------

func TestComputePercent(t *testing.T) {
	tests := []struct {
		name      string
		completed int
		total     int
		want      int
	}{
		{"zero total returns 0", 5, 0, 0},
		{"negative total returns 0", 5, -10, 0},
		{"none done", 0, 10, 0},
		{"half done floors", 1, 3, 33},  // floor(33.33)
		{"two thirds floors", 2, 3, 66}, // floor(66.66)
		{"all done", 10, 10, 100},
		{"over 100 clamps", 15, 10, 100},
		{"negative completed clamps to 0", -5, 10, 0},
		{"single episode", 1, 1, 100},
		{"large counts floor", 999, 1000, 99},
		{"exactly 1 percent", 1, 100, 1},
		{"just under 1 percent floors to 0", 9, 1000, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computePercent(tt.completed, tt.total); got != tt.want {
				t.Fatalf("computePercent(%d,%d)=%d want %d", tt.completed, tt.total, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// clampPercent
// ---------------------------------------------------------------------------

func TestClampPercent(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{-1000, 0},
		{-1, 0},
		{0, 0},
		{50, 50},
		{100, 100},
		{101, 100},
		{99999, 100},
	}
	for _, tt := range tests {
		if got := clampPercent(tt.in); got != tt.want {
			t.Fatalf("clampPercent(%d)=%d want %d", tt.in, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// clampInt
// ---------------------------------------------------------------------------

func TestClampInt(t *testing.T) {
	tests := []struct {
		v, lo, hi, want int
	}{
		{5, 0, 10, 5},
		{-5, 0, 10, 0},
		{15, 0, 10, 10},
		{0, 0, 10, 0},
		{10, 0, 10, 10},
		{14, 14, 42, 14}, // boundary used by computeLayout
	}
	for _, tt := range tests {
		if got := clampInt(tt.v, tt.lo, tt.hi); got != tt.want {
			t.Fatalf("clampInt(%d,%d,%d)=%d want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// min
// ---------------------------------------------------------------------------

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Fatal("min(3,5) should be 3")
	}
	if min(5, 3) != 3 {
		t.Fatal("min(5,3) should be 3")
	}
	if min(4, 4) != 4 {
		t.Fatal("min(4,4) should be 4")
	}
	if min(-1, 0) != -1 {
		t.Fatal("min(-1,0) should be -1")
	}
}

// ---------------------------------------------------------------------------
// formatBytesShort
// ---------------------------------------------------------------------------

func TestFormatBytesShort(t *testing.T) {
	const (
		kb = 1024.0
		mb = 1024.0 * kb
		gb = 1024.0 * mb
	)
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"zero bytes", 0, "0B"},
		{"small bytes", 512, "512B"},
		{"just under KB", 1023, "1023B"},
		{"exactly 1KB", kb, "1K"},
		{"1.5KB rounds (no decimals)", 1536, "2K"}, // %.0f rounds 1.5 -> 2
		{"exactly 1MB", mb, "1.0M"},
		{"1.5MB", 1.5 * mb, "1.5M"},
		{"exactly 1GB", gb, "1.0G"},
		{"2.25GB", 2.25 * gb, "2.2G"}, // %.1f of 2.25 -> 2.2
		{"negative falls to bytes", -100, "-100B"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatBytesShort(tt.in); got != tt.want {
				t.Fatalf("formatBytesShort(%v)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatDuration
// ---------------------------------------------------------------------------

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"negative", -5 * time.Second, "0s"},
		{"zero", 0, "0s"},
		{"sub-second rounds to 0s", 400 * time.Millisecond, "0s"},
		{"rounds up to 1s", 600 * time.Millisecond, "1s"},
		{"45 seconds", 45 * time.Second, "45s"},
		{"59 seconds", 59 * time.Second, "59s"},
		{"exactly 1 minute", 60 * time.Second, "1m"},
		{"1m30s", 90 * time.Second, "1m30s"},
		{"2m", 120 * time.Second, "2m"},
		{"59m59s", 59*time.Minute + 59*time.Second, "59m59s"},
		{"exactly 1 hour", time.Hour, "1h0m"},
		{"1h30m", time.Hour + 30*time.Minute, "1h30m"},
		{"2h5m", 2*time.Hour + 5*time.Minute, "2h5m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.d); got != tt.want {
				t.Fatalf("formatDuration(%v)=%q want %q", tt.d, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// padOrClip
// ---------------------------------------------------------------------------

func TestPadOrClip(t *testing.T) {
	tests := []struct {
		name string
		s    string
		cols int
		want string
	}{
		{"zero cols", "hello", 0, ""},
		{"negative cols", "hello", -3, ""},
		{"exact fit", "abc", 3, "abc"},
		{"pad right", "ab", 5, "ab   "},
		{"empty padded", "", 3, "   "},
		{"clip with ellipsis", "abcdef", 4, "abc…"},
		{"clip to 1 col is ellipsis", "abcdef", 1, "…"},
		{"clip to 2 cols", "abcdef", 2, "a…"},
		{"unicode pad counts runes", "ab", 4, "ab  "},
		{"cyrillic exact", "абв", 3, "абв"},
		{"cyrillic clip", "абвгд", 3, "аб…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := padOrClip(tt.s, tt.cols); got != tt.want {
				t.Fatalf("padOrClip(%q,%d)=%q want %q", tt.s, tt.cols, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// truncateText
// ---------------------------------------------------------------------------

func TestTruncateText(t *testing.T) {
	tests := []struct {
		name string
		s    string
		cols int
		want string
	}{
		{"zero cols", "hello", 0, ""},
		{"negative cols", "hello", -1, ""},
		{"fits exactly", "abc", 3, "abc"},
		{"shorter fits", "ab", 5, "ab"},
		{"truncate with ellipsis", "abcdef", 4, "abc…"},
		{"truncate to 1", "abcdef", 1, "…"},
		{"empty string", "", 5, ""},
		{"cyrillic fits", "привет", 6, "привет"},
		{"cyrillic truncated", "привет", 4, "при…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateText(tt.s, tt.cols); got != tt.want {
				t.Fatalf("truncateText(%q,%d)=%q want %q", tt.s, tt.cols, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// displayWidth
// ---------------------------------------------------------------------------

func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"привет", 6}, // 6 cyrillic runes, single-width each
		{"a b", 3},
	}
	for _, tt := range tests {
		if got := displayWidth(tt.s); got != tt.want {
			t.Fatalf("displayWidth(%q)=%d want %d", tt.s, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// trackLabel
// ---------------------------------------------------------------------------

func TestTrackLabel(t *testing.T) {
	tests := []struct {
		name string
		ref  domain.TrackRef
		want string
	}{
		{"video", domain.TrackRef{Kind: domain.TrackVideo, Index: 0}, "Video"},
		{"audio 0", domain.TrackRef{Kind: domain.TrackAudio, Index: 0}, "Audio[0]"},
		{"audio 2", domain.TrackRef{Kind: domain.TrackAudio, Index: 2}, "Audio[2]"},
		{"sub 1", domain.TrackRef{Kind: domain.TrackSubtitle, Index: 1}, "Sub[1]"},
		{"unknown kind", domain.TrackRef{Kind: domain.TrackKind(99), Index: 3}, "Track[3]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trackLabel(tt.ref); got != tt.want {
				t.Fatalf("trackLabel(%+v)=%q want %q", tt.ref, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// computeLayout
// ---------------------------------------------------------------------------

func TestComputeLayout(t *testing.T) {
	tests := []struct {
		name         string
		termWidth    int
		wantLabelCol int
		wantBarWidth int
	}{
		// labelCol = clamp(w*2/5, 14, 42); barWidth = clamp(w/5, 10, 20)
		{"very narrow clamps to min", 10, 14, 10},
		{"narrow", 40, 16, 10},           // 40*2/5=16, 40/5=8 -> 10
		{"medium", 80, 32, 16},           // 80*2/5=32, 80/5=16
		{"wide clamps max", 200, 42, 20}, // 200*2/5=80->42, 200/5=40->20
		{"zero width", 0, 14, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lay := computeLayout(tt.termWidth)
			if lay.width != tt.termWidth {
				t.Errorf("width=%d want %d", lay.width, tt.termWidth)
			}
			if lay.labelCol != tt.wantLabelCol {
				t.Errorf("labelCol=%d want %d", lay.labelCol, tt.wantLabelCol)
			}
			if lay.barWidth != tt.wantBarWidth {
				t.Errorf("barWidth=%d want %d", lay.barWidth, tt.wantBarWidth)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// progressBar (non-TTY path: no color codes)
// ---------------------------------------------------------------------------

func TestProgressBar(t *testing.T) {
	r := &LiveReporter{isTTY: false}
	tests := []struct {
		name      string
		percent   int
		width     int
		wantFill  int // number of █
		wantEmpty int // number of ░
		wantPct   string
	}{
		{"zero percent", 0, 10, 0, 10, "   0%"},
		{"full", 100, 10, 10, 0, " 100%"},
		{"half", 50, 10, 5, 5, "  50%"},
		{"45 of 20 width", 45, 20, 9, 11, "  45%"}, // 45*20/100 = 9
		{"negative clamps to 0", -10, 10, 0, 10, "   0%"},
		{"over 100 clamps", 150, 10, 10, 0, " 100%"},
		{"floor partial", 19, 10, 1, 9, "  19%"}, // 19*10/100=1
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.progressBar(tt.percent, tt.width, "")
			gotFill := strings.Count(got, "█")
			gotEmpty := strings.Count(got, "░")
			if gotFill != tt.wantFill {
				t.Errorf("filled=%d want %d (out=%q)", gotFill, tt.wantFill, got)
			}
			if gotEmpty != tt.wantEmpty {
				t.Errorf("empty=%d want %d (out=%q)", gotEmpty, tt.wantEmpty, got)
			}
			if !strings.HasSuffix(got, tt.wantPct) {
				t.Errorf("pct suffix: got %q want suffix %q", got, tt.wantPct)
			}
			if !strings.HasPrefix(got, "[") || !strings.Contains(got, "]") {
				t.Errorf("missing brackets: %q", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// colorize / repeatChar
// ---------------------------------------------------------------------------

func TestColorize(t *testing.T) {
	rOn := &LiveReporter{isTTY: true}
	rOff := &LiveReporter{isTTY: false}
	if got := rOff.colorize("\033[31m", "hi"); got != "hi" {
		t.Errorf("non-TTY colorize should be plain, got %q", got)
	}
	got := rOn.colorize("\033[31m", "hi")
	if !strings.HasPrefix(got, "\033[31m") || !strings.HasSuffix(got, "\033[0m") || !strings.Contains(got, "hi") {
		t.Errorf("TTY colorize wrong: %q", got)
	}
}

func TestRepeatChar(t *testing.T) {
	r := &LiveReporter{}
	if got := r.repeatChar('-', 0); got != "" {
		t.Errorf("repeatChar n=0 want empty, got %q", got)
	}
	if got := r.repeatChar('-', -5); got != "" {
		t.Errorf("repeatChar n<0 want empty, got %q", got)
	}
	if got := r.repeatChar('x', 3); got != "xxx" {
		t.Errorf("repeatChar 'x' 3 want xxx, got %q", got)
	}
	if got := r.repeatChar('─', 2); got != "──" {
		t.Errorf("repeatChar unicode want ──, got %q", got)
	}
}

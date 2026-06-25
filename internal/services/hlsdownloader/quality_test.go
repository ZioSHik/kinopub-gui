package hlsdownloader

import (
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// --- Variant helpers -----------------------------------------------------

func TestVariant_CodecPredicates(t *testing.T) {
	tests := []struct {
		codecs   string
		wantH265 bool
		wantH264 bool
	}{
		{"avc1.640028,mp4a.40.2", false, true},
		{"hvc1.2.4.L150.B0", true, false},
		{"hev1.1.6.L93.B0", true, false},
		{"hevc", true, false},
		{"", false, true}, // empty defaults to h264 per doc
		{"mp4a.40.2", false, false},
	}
	for _, tt := range tests {
		v := Variant{Codecs: tt.codecs}
		if v.IsH265() != tt.wantH265 {
			t.Errorf("IsH265(%q) = %v, want %v", tt.codecs, v.IsH265(), tt.wantH265)
		}
		if v.IsH264() != tt.wantH264 {
			t.Errorf("IsH264(%q) = %v, want %v", tt.codecs, v.IsH264(), tt.wantH264)
		}
	}
}

func TestVariant_BitrateKbpsAndLabel(t *testing.T) {
	v := Variant{Bandwidth: 2_500_000, Height: 1080, Codecs: "avc1.640028"}
	if v.BitrateKbps() != 2500 {
		t.Errorf("BitrateKbps = %d, want 2500", v.BitrateKbps())
	}
	if got, want := v.Label(), "1080p/h264 (2500 kbps)"; got != want {
		t.Errorf("Label = %q, want %q", got, want)
	}
	vh := Variant{Bandwidth: 12_000_000, Height: 2160, Codecs: "hvc1.2"}
	if got, want := vh.Label(), "2160p/h265 (12000 kbps)"; got != want {
		t.Errorf("Label = %q, want %q", got, want)
	}
}

// --- SelectVariant -------------------------------------------------------

func sampleVariants() []Variant {
	return []Variant{
		{Bandwidth: 800_000, Height: 360, Width: 640, Resolution: "640x360", Codecs: "avc1.42c01e"},
		{Bandwidth: 1_400_000, Height: 480, Width: 854, Resolution: "854x480", Codecs: "avc1.4d401e"},
		{Bandwidth: 2_400_000, Height: 720, Width: 1280, Resolution: "1280x720", Codecs: "avc1.4d401f"},
		{Bandwidth: 2_800_000, Height: 1080, Width: 1920, Resolution: "1920x1080", Codecs: "avc1.640028"},
		{Bandwidth: 8_000_000, Height: 2160, Width: 3840, Resolution: "3840x2160", Codecs: "hvc1.2.4.L150"},
	}
}

func TestSelectVariant_EmptyError(t *testing.T) {
	if _, err := SelectVariant(nil, ""); err == nil {
		t.Fatal("expected an error for no variants")
	}
}

func TestSelectVariant_Max(t *testing.T) {
	got, err := SelectVariant(sampleVariants(), "max")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Bandwidth != 8_000_000 {
		t.Errorf("max selected bandwidth %d, want 8000000", got.Bandwidth)
	}
}

func TestSelectVariant_OptimalPrefers1080pUnder3000(t *testing.T) {
	for _, pref := range []domain.Quality{"", "optimal", "OPTIMAL", "  optimal  "} {
		got, err := SelectVariant(sampleVariants(), pref)
		if err != nil {
			t.Fatalf("pref %q err: %v", pref, err)
		}
		if got.Height != 1080 || got.Bandwidth != 2_800_000 {
			t.Errorf("pref %q: selected %+v, want the 1080p/2.8Mbps h264", pref, got)
		}
	}
}

func TestSelectVariant_OptimalClosestTo2500Among1080p(t *testing.T) {
	// Two 1080p h264 under 3000 kbps: 2200 and 2900. 2500 is closer to 2200? no,
	// |2500-2200|=300, |2500-2900|=400 → pick 2200.
	vs := []Variant{
		{Bandwidth: 2_200_000, Height: 1080, Codecs: "avc1.640028"},
		{Bandwidth: 2_900_000, Height: 1080, Codecs: "avc1.640028"},
		{Bandwidth: 2_400_000, Height: 720, Codecs: "avc1.4d401f"},
	}
	got, _ := SelectVariant(vs, "optimal")
	if got.Bandwidth != 2_200_000 {
		t.Errorf("optimal picked %d, want 2200000 (closest to 2500)", got.Bandwidth)
	}
}

func TestSelectVariant_OptimalFallsBackTo720pHighestBitrate(t *testing.T) {
	// No 1080p h264 ≤3000; should pick highest-bitrate 720p h264.
	vs := []Variant{
		{Bandwidth: 1_500_000, Height: 720, Codecs: "avc1.4d401f"},
		{Bandwidth: 2_900_000, Height: 720, Codecs: "avc1.4d401f"},
		{Bandwidth: 9_000_000, Height: 1080, Codecs: "hvc1.2"}, // 1080 but h265, ineligible
	}
	got, _ := SelectVariant(vs, "optimal")
	if got.Height != 720 || got.Bandwidth != 2_900_000 {
		t.Errorf("optimal picked %+v, want highest-bitrate 720p", got)
	}
}

func TestSelectVariant_OptimalFallbackClosestTo2500(t *testing.T) {
	// Neither 1080p≤3000 nor 720p available → closest to 2500 kbps overall.
	vs := []Variant{
		{Bandwidth: 800_000, Height: 360, Codecs: "avc1"},
		{Bandwidth: 2_600_000, Height: 540, Codecs: "avc1"},
		{Bandwidth: 9_000_000, Height: 2160, Codecs: "hvc1"},
	}
	got, _ := SelectVariant(vs, "optimal")
	if got.Bandwidth != 2_600_000 {
		t.Errorf("fallback picked %d, want 2600000 (closest to 2500)", got.Bandwidth)
	}
}

func TestSelectVariant_OptimalExcludes1080pH265(t *testing.T) {
	// 1080p but HEVC must not satisfy the 1080p-h264 sweet-spot branch.
	vs := []Variant{
		{Bandwidth: 2_500_000, Height: 1080, Codecs: "hvc1.2"}, // h265, ineligible for branch 1
		{Bandwidth: 2_000_000, Height: 720, Codecs: "avc1.4d401f"},
	}
	got, _ := SelectVariant(vs, "optimal")
	if got.Height != 720 {
		t.Errorf("optimal picked %+v, want the 720p h264 (1080p was h265)", got)
	}
}

func TestSelectVariant_ExplicitHeights(t *testing.T) {
	vs := sampleVariants()
	tests := []struct {
		pref       domain.Quality
		wantHeight int
	}{
		{"1080p", 1080},
		{"1080", 1080},
		{"720p", 720},
		{"480p", 480},
		{"360p", 360},
		{"2160p", 2160},
		{"4k", 2160},
	}
	for _, tt := range tests {
		got, err := SelectVariant(vs, tt.pref)
		if err != nil {
			t.Fatalf("pref %q err: %v", tt.pref, err)
		}
		if got.Height != tt.wantHeight {
			t.Errorf("pref %q selected height %d, want %d", tt.pref, got.Height, tt.wantHeight)
		}
	}
}

func TestSelectVariant_ExplicitPicksLowestBitrateAmongMatches(t *testing.T) {
	// Two 1080p variants; explicit "1080p" should take the LOWEST bitrate (most
	// efficient), per the doc comment.
	vs := []Variant{
		{Bandwidth: 5_000_000, Height: 1080, Codecs: "avc1.640028"},
		{Bandwidth: 3_000_000, Height: 1080, Codecs: "avc1.640028"},
		{Bandwidth: 2_400_000, Height: 720, Codecs: "avc1.4d401f"},
	}
	got, _ := SelectVariant(vs, "1080p")
	if got.Bandwidth != 3_000_000 {
		t.Errorf("explicit 1080p picked %d, want 3000000 (lowest bitrate match)", got.Bandwidth)
	}
}

func TestSelectVariant_ExplicitCodecFilter(t *testing.T) {
	vs := []Variant{
		{Bandwidth: 3_000_000, Height: 1080, Codecs: "avc1.640028"},
		{Bandwidth: 2_500_000, Height: 1080, Codecs: "hvc1.2"},
	}
	gotH265, _ := SelectVariant(vs, "1080p-h265")
	if !gotH265.IsH265() {
		t.Errorf("1080p-h265 picked %+v, want the HEVC variant", gotH265)
	}
	gotH264, _ := SelectVariant(vs, "1080p-h264")
	if !gotH264.IsH264() || gotH264.IsH265() {
		t.Errorf("1080p-h264 picked %+v, want the AVC variant", gotH264)
	}
}

func TestSelectVariant_ExplicitNoMatchClosestHeight(t *testing.T) {
	// Ask for 1080p when only 480 and 720 exist → closest height is 720.
	vs := []Variant{
		{Bandwidth: 1_400_000, Height: 480, Codecs: "avc1"},
		{Bandwidth: 2_400_000, Height: 720, Codecs: "avc1"},
	}
	got, err := SelectVariant(vs, "1080p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Height != 720 {
		t.Errorf("closest-height picked %d, want 720", got.Height)
	}
}

func TestSelectVariant_ClosestHeightTieBreaksOnHigherBandwidth(t *testing.T) {
	// Two variants equidistant from a target height; the tie should break toward
	// the higher bandwidth per closestToHeight.
	vs := []Variant{
		{Bandwidth: 1_000_000, Height: 480, Codecs: "avc1"},
		{Bandwidth: 2_000_000, Height: 480, Codecs: "avc1"},
	}
	// "1080p" has no match → closestToHeight(target=1080); both height 480 are
	// equidistant → pick higher bandwidth.
	got, _ := SelectVariant(vs, "1080p")
	if got.Bandwidth != 2_000_000 {
		t.Errorf("tie-break picked %d, want 2000000 (higher bandwidth)", got.Bandwidth)
	}
}

func TestSelectVariant_ExplicitUnparseableHeightKeepsAllLowestBitrate(t *testing.T) {
	// An unrecognized preference parses wantHeight=0, so no height filter applies;
	// among all variants the lowest bitrate is chosen.
	vs := sampleVariants()
	got, err := SelectVariant(vs, "garbage")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Bandwidth != 800_000 {
		t.Errorf("unparseable pref picked %d, want 800000 (lowest bitrate, no height filter)", got.Bandwidth)
	}
}

func TestSelectVariant_ExplicitNumericFormats(t *testing.T) {
	vs := sampleVariants()
	// "1080" without p, "1080p" with p, both map to 1080.
	for _, pref := range []domain.Quality{"1080", "1080p"} {
		got, _ := SelectVariant(vs, pref)
		if got.Height != 1080 {
			t.Errorf("pref %q height %d, want 1080", pref, got.Height)
		}
	}
}

func TestAbs(t *testing.T) {
	cases := map[int]int{-5: 5, 0: 0, 7: 7}
	for in, want := range cases {
		if got := abs(in); got != want {
			t.Errorf("abs(%d) = %d, want %d", in, got, want)
		}
	}
}

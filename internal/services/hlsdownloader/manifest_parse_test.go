package hlsdownloader

import (
	"reflect"
	"strings"
	"testing"
)

// --- parseHLSAttributes --------------------------------------------------

func TestParseHLSAttributes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{
			name: "quoted and bare values mixed",
			in:   `BANDWIDTH=2500000,RESOLUTION=1920x1080,CODECS="avc1.640028,mp4a.40.2",AUDIO="aud1"`,
			want: map[string]string{
				"BANDWIDTH":  "2500000",
				"RESOLUTION": "1920x1080",
				"CODECS":     "avc1.640028,mp4a.40.2", // commas inside quotes preserved
				"AUDIO":      "aud1",
			},
		},
		{
			name: "leading whitespace trimmed",
			in:   `  TYPE=AUDIO , GROUP-ID="a"`,
			want: map[string]string{"TYPE": "AUDIO", "GROUP-ID": "a"},
		},
		{
			name: "unterminated quote takes rest of string",
			in:   `URI="no-close`,
			want: map[string]string{"URI": "no-close"},
		},
		{
			name: "empty input",
			in:   ``,
			want: map[string]string{},
		},
		{
			name: "trailing comma after quote",
			in:   `A="x",`,
			want: map[string]string{"A": "x"},
		},
		{
			name: "value with no equals stops parse",
			in:   `A=1,JUNK`,
			want: map[string]string{"A": "1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHLSAttributes(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseHLSAttributes(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

// --- resolveURL ----------------------------------------------------------

func TestResolveURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		ref  string
		want string
	}{
		{"empty ref", "https://cdn.example/v/master.m3u8", "", ""},
		{"absolute https ref", "https://cdn.example/v/master.m3u8", "https://other/x.m3u8", "https://other/x.m3u8"},
		{"absolute http ref", "https://cdn.example/v/master.m3u8", "http://other/x.m3u8", "http://other/x.m3u8"},
		{"relative sibling", "https://cdn.example/v/master.m3u8", "media.m3u8", "https://cdn.example/v/media.m3u8"},
		{"relative subdir", "https://cdn.example/v/master.m3u8", "720/media.m3u8", "https://cdn.example/v/720/media.m3u8"},
		{"absolute path on host", "https://cdn.example/v/master.m3u8", "/abs/media.m3u8", "https://cdn.example/abs/media.m3u8"},
		{"parent dir", "https://cdn.example/v/sub/master.m3u8", "../media.m3u8", "https://cdn.example/v/media.m3u8"},
		{"query preserved on relative", "https://cdn.example/v/master.m3u8?t=1", "seg.ts", "https://cdn.example/v/seg.ts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveURL(tt.base, tt.ref); got != tt.want {
				t.Errorf("resolveURL(%q,%q) = %q, want %q", tt.base, tt.ref, got, tt.want)
			}
		})
	}
}

// --- parseMasterPlaylist -------------------------------------------------

func TestParseMasterPlaylist_VariantsAndAudio(t *testing.T) {
	const m3u8 = `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="Дубляж (RUS)",LANGUAGE="rus",URI="audio/rus.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="Original (ENG)",LANGUAGE="eng",URI="audio/eng.m3u8"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="ru",URI="subs/ru.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,CODECS="avc1.640028,mp4a.40.2",AUDIO="aud"
1080/media.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,CODECS="avc1.4d401f",AUDIO="aud"
720/media.m3u8
`
	master, err := parseMasterPlaylist(strings.NewReader(m3u8), "https://cdn.example/v/master.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(master.Variants) != 2 {
		t.Fatalf("got %d variants, want 2", len(master.Variants))
	}
	v0 := master.Variants[0]
	if v0.Bandwidth != 5000000 || v0.Width != 1920 || v0.Height != 1080 {
		t.Errorf("variant[0] bandwidth/res wrong: %+v", v0)
	}
	if v0.Resolution != "1920x1080" {
		t.Errorf("variant[0] Resolution = %q", v0.Resolution)
	}
	if v0.Codecs != "avc1.640028,mp4a.40.2" {
		t.Errorf("variant[0] Codecs = %q", v0.Codecs)
	}
	if v0.AudioGroup != "aud" {
		t.Errorf("variant[0] AudioGroup = %q", v0.AudioGroup)
	}
	if v0.URL != "https://cdn.example/v/1080/media.m3u8" {
		t.Errorf("variant[0] URL = %q", v0.URL)
	}

	// Only AUDIO media entries are captured; SUBTITLES ignored.
	if len(master.Audio) != 2 {
		t.Fatalf("got %d audio renditions, want 2 (subtitles ignored)", len(master.Audio))
	}
	if master.Audio[0].Name != "Дубляж (RUS)" || master.Audio[0].Language != "rus" {
		t.Errorf("audio[0] = %+v", master.Audio[0])
	}
	if master.Audio[0].URI != "https://cdn.example/v/audio/rus.m3u8" {
		t.Errorf("audio[0] URI = %q, want resolved", master.Audio[0].URI)
	}
	if master.Audio[0].GroupID != "aud" {
		t.Errorf("audio[0] GroupID = %q", master.Audio[0].GroupID)
	}
}

func TestParseMasterPlaylist_AbsoluteVariantURL(t *testing.T) {
	const m3u8 = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=854x480
https://other-cdn.example/abs/media.m3u8
`
	master, err := parseMasterPlaylist(strings.NewReader(m3u8), "https://cdn.example/v/master.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(master.Variants) != 1 {
		t.Fatalf("got %d variants, want 1", len(master.Variants))
	}
	if master.Variants[0].URL != "https://other-cdn.example/abs/media.m3u8" {
		t.Errorf("URL = %q, want absolute left untouched", master.Variants[0].URL)
	}
}

func TestParseMasterPlaylist_StreamInfWithoutURIDropped(t *testing.T) {
	// A STREAM-INF line whose URI is missing (immediately followed by another
	// tag) must not produce a half-built variant.
	const m3u8 = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=854x480
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720
720/media.m3u8
`
	master, err := parseMasterPlaylist(strings.NewReader(m3u8), "https://cdn.example/v/master.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(master.Variants) != 1 {
		t.Fatalf("got %d variants, want 1 (the orphan STREAM-INF dropped)", len(master.Variants))
	}
	if master.Variants[0].Bandwidth != 2000000 {
		t.Errorf("kept variant should be the 2nd (it got the URI), got %+v", master.Variants[0])
	}
}

func TestParseMasterPlaylist_Empty(t *testing.T) {
	master, err := parseMasterPlaylist(strings.NewReader("#EXTM3U\n"), "https://cdn.example/master.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(master.Variants) != 0 || len(master.Audio) != 0 {
		t.Errorf("expected empty master, got %+v", master)
	}
}

func TestParseMasterPlaylist_AudioWithoutURI(t *testing.T) {
	// A muxed-only audio entry (no URI) is still recorded with empty URI; the
	// downloader filters those out later in audioRenditionsFor.
	const m3u8 = `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="Muxed",DEFAULT=YES
#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=854x480,AUDIO="aud"
media.m3u8
`
	master, err := parseMasterPlaylist(strings.NewReader(m3u8), "https://cdn.example/v/master.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(master.Audio) != 1 {
		t.Fatalf("got %d audio, want 1", len(master.Audio))
	}
	if master.Audio[0].URI != "" {
		t.Errorf("URI = %q, want empty (no URI attr)", master.Audio[0].URI)
	}
}

// --- parseMediaPlaylist --------------------------------------------------

func TestParseMediaPlaylist_DurationsAndOrder(t *testing.T) {
	const m3u8 = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:9.009,
seg_0.ts
#EXTINF:8.5,with a title here
seg_1.ts
#EXTINF:10,
seg_2.ts
#EXT-X-ENDLIST
`
	pl, err := parseMediaPlaylist(strings.NewReader(m3u8), "https://cdn.example/v/720/media.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pl.Segments) != 3 {
		t.Fatalf("got %d segments, want 3", len(pl.Segments))
	}
	wantDur := []float64{9.009, 8.5, 10}
	for i, seg := range pl.Segments {
		if seg.Index != i {
			t.Errorf("segment[%d].Index = %d, want %d", i, seg.Index, i)
		}
		if seg.Duration != wantDur[i] {
			t.Errorf("segment[%d].Duration = %v, want %v", i, seg.Duration, wantDur[i])
		}
	}
	if pl.Segments[1].URL != "https://cdn.example/v/720/seg_1.ts" {
		t.Errorf("segment[1].URL = %q", pl.Segments[1].URL)
	}
	const wantTotal = 9.009 + 8.5 + 10
	if pl.TotalDuration < wantTotal-1e-9 || pl.TotalDuration > wantTotal+1e-9 {
		t.Errorf("TotalDuration = %v, want %v", pl.TotalDuration, wantTotal)
	}
}

func TestParseMediaPlaylist_InvalidDurationErrors(t *testing.T) {
	const m3u8 = `#EXTM3U
#EXTINF:notanumber,
seg_0.ts
`
	_, err := parseMediaPlaylist(strings.NewReader(m3u8), "https://cdn.example/m.m3u8")
	if err == nil {
		t.Fatal("expected an error for non-numeric EXTINF duration")
	}
	if !strings.Contains(err.Error(), "invalid EXTINF duration") {
		t.Errorf("error = %v, want it to mention invalid EXTINF duration", err)
	}
}

func TestParseMediaPlaylist_BareURLWithoutExtinfIgnored(t *testing.T) {
	// A media URI line without a preceding #EXTINF must not become a segment;
	// the parser keys segment emission off sawExtinf.
	const m3u8 = `#EXTM3U
not-a-segment.ts
#EXTINF:4.0,
real_seg.ts
`
	pl, err := parseMediaPlaylist(strings.NewReader(m3u8), "https://cdn.example/m.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pl.Segments) != 1 {
		t.Fatalf("got %d segments, want 1 (bare URL skipped)", len(pl.Segments))
	}
	if !strings.HasSuffix(pl.Segments[0].URL, "real_seg.ts") {
		t.Errorf("segment[0].URL = %q, want the real_seg", pl.Segments[0].URL)
	}
}

func TestParseMediaPlaylist_Empty(t *testing.T) {
	pl, err := parseMediaPlaylist(strings.NewReader("#EXTM3U\n#EXT-X-ENDLIST\n"), "https://cdn.example/m.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pl.Segments) != 0 {
		t.Errorf("got %d segments, want 0", len(pl.Segments))
	}
	if pl.TotalDuration != 0 {
		t.Errorf("TotalDuration = %v, want 0", pl.TotalDuration)
	}
}

func TestParseMediaPlaylist_AbsoluteSegmentURLs(t *testing.T) {
	const m3u8 = `#EXTM3U
#EXTINF:6.0,
https://seg-cdn.example/a/seg_0.ts
#EXTINF:6.0,
seg_1.ts
`
	pl, err := parseMediaPlaylist(strings.NewReader(m3u8), "https://cdn.example/v/media.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pl.Segments[0].URL != "https://seg-cdn.example/a/seg_0.ts" {
		t.Errorf("absolute segment URL changed: %q", pl.Segments[0].URL)
	}
	if pl.Segments[1].URL != "https://cdn.example/v/seg_1.ts" {
		t.Errorf("relative segment URL not resolved: %q", pl.Segments[1].URL)
	}
}

func TestParseMediaPlaylist_MapWithoutURIIgnored(t *testing.T) {
	// An EXT-X-MAP without a URI attribute must leave InitURI empty.
	const m3u8 = `#EXTM3U
#EXT-X-MAP:BYTERANGE="100@0"
#EXTINF:6.0,
seg_0.m4s
`
	pl, err := parseMediaPlaylist(strings.NewReader(m3u8), "https://cdn.example/v/media.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pl.InitURI != "" {
		t.Errorf("InitURI = %q, want empty when MAP has no URI", pl.InitURI)
	}
	if len(pl.Segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(pl.Segments))
	}
}

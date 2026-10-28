package hlsdownloader

import (
	"math"
	"strings"
	"testing"
)

func FuzzHLSManifests(f *testing.F) {
	for _, seed := range []string{"#EXTM3U\n#EXTINF:1,\n/a.ts\n#EXT-X-ENDLIST\n", "#EXTM3U\n#EXTINF:NaN,\n/a.ts\n", "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000,RESOLUTION=640x360\n/v.m3u8\n", "TYPE=AUDIO, NAME=\"A,B\" , LANGUAGE=\"eng\" , URI=\"a.m3u8\""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 65536 {
			t.Skip()
		}
		parseMasterPlaylist(strings.NewReader(input), "https://example.com/master.m3u8")
		parseHLSAttributes(input)
		media, err := parseMediaPlaylist(strings.NewReader(input), "https://example.com/video.m3u8")
		if err == nil {
			if math.IsNaN(media.TotalDuration) || math.IsInf(media.TotalDuration, 0) || media.TotalDuration < 0 {
				t.Fatalf("invalid duration: %v", media.TotalDuration)
			}
			for index, segment := range media.Segments {
				if segment.Index != index {
					t.Fatal("unstable segment indexing")
				}
			}
		}
	})
}

func TestMediaPlaylistRejectsUnsupportedOrInvalidData(t *testing.T) {
	for _, tag := range []string{"#EXT-X-BYTERANGE:100@0", "#EXT-X-MAP:URI=\"media.mp4\",BYTERANGE=\"100@0\"", "#EXT-X-KEY:METHOD=AES-128,URI=\"key\"", "#EXTINF:NaN,", "#EXTINF:+Inf,", "#EXTINF:-1,"} {
		if _, err := parseMediaPlaylist(strings.NewReader("#EXTM3U\n"+tag+"\n#EXTINF:1,\n/a.ts\n"), "https://example.com/v.m3u8"); err == nil {
			t.Errorf("silently accepted %s", tag)
		}
	}
}

func TestHLSAttributesAllowWhitespaceAfterQuotedValues(t *testing.T) {
	attrs := parseHLSAttributes(`TYPE=AUDIO , NAME="Studio, A" , LANGUAGE="rus" , URI="a.m3u8"`)
	if attrs["NAME"] != "Studio, A" || attrs["LANGUAGE"] != "rus" || attrs["URI"] != "a.m3u8" {
		t.Fatalf("valid spaced attributes lost: %v", attrs)
	}
}

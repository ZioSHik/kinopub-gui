package hlsdownloader

import (
	"strings"
	"testing"
)

// kino.pub serves fMP4/CMAF media playlists (4K/HEVC) that begin with an
// EXT-X-MAP init segment carrying the ftyp+moov header. parseMediaPlaylist must
// capture and resolve its URI so the downloader can prepend it; without the init
// segment the concatenated fragments are headless and ffmpeg fails to demux them.
func TestParseMediaPlaylist_ExtXMapInitSegment(t *testing.T) {
	const m3u8 = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:6
#EXT-X-MAP:URI="init.mp4"
#EXTINF:6.0,
seg_0.m4s
#EXTINF:6.0,
seg_1.m4s
#EXT-X-ENDLIST
`
	pl, err := parseMediaPlaylist(strings.NewReader(m3u8), "https://cdn.kino.pub/v/720/media.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pl.InitURI != "https://cdn.kino.pub/v/720/init.mp4" {
		t.Errorf("InitURI = %q, want the resolved init.mp4 URL", pl.InitURI)
	}
	if len(pl.Segments) != 2 {
		t.Fatalf("got %d segments, want 2", len(pl.Segments))
	}
	if pl.Segments[0].URL != "https://cdn.kino.pub/v/720/seg_0.m4s" {
		t.Errorf("segment[0] = %q, want resolved seg_0.m4s", pl.Segments[0].URL)
	}
}

// Plain MPEG-TS playlists have no EXT-X-MAP; InitURI must stay empty so the
// downloader concatenates segments directly (the long-standing TS behaviour).
func TestParseMediaPlaylist_NoInitForPlainTS(t *testing.T) {
	const m3u8 = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
seg_0.ts
#EXTINF:10.0,
seg_1.ts
#EXT-X-ENDLIST
`
	pl, err := parseMediaPlaylist(strings.NewReader(m3u8), "https://cdn.kino.pub/v/480/media.m3u8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pl.InitURI != "" {
		t.Errorf("InitURI = %q, want empty for a plain TS playlist", pl.InitURI)
	}
	if len(pl.Segments) != 2 {
		t.Fatalf("got %d segments, want 2", len(pl.Segments))
	}
}

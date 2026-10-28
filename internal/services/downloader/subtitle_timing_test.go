package downloader

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestMuxHLSAlignsMappedSubtitlesWithoutChangingExternalOnes(t *testing.T) {
	base := time.Duration(0)
	hls := &domain.HLSDownloadResult{VideoPath: "video.ts", SubtitleTracks: []domain.HLSSubtitleTrack{
		{Path: "hls.vtt", TimestampOffset: &base},
		{Path: "external.srt"},
	}}
	output := filepath.Join(t.TempDir(), "out.mkv")
	var calls []string
	run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
		calls = append(calls, name)
		if name == "ffprobe" {
			_, err := io.WriteString(stdout, `{"format":{"start_time":"10.000000"}}`)
			return err
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-itsoffset -10.000000 -i hls.vtt -i external.srt") {
			t.Errorf("incorrect subtitle origins: %s", joined)
		}
		return os.WriteFile(output+".tmp", []byte("muxed"), 0600)
	}
	d := New(run, nil, testLogger{})
	if err := d.MuxHLS(context.Background(), domain.Job{OutPath: output}, hls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || hls.VideoStartTime != 0 || *hls.SubtitleTracks[0].TimestampOffset != 0 {
		t.Fatalf("unexpected calls or mutation: calls=%v hls=%+v", calls, hls)
	}
}

func TestMuxHLSRejectsUnknownVideoOrigin(t *testing.T) {
	for _, output := range []string{`{"format":{}}`, `{"format":{"start_time":"NaN"}}`, `invalid json`, ""} {
		t.Run(output, func(t *testing.T) {
			base := time.Duration(0)
			hls := &domain.HLSDownloadResult{VideoPath: "video.ts", SubtitleTracks: []domain.HLSSubtitleTrack{{Path: "sub.vtt", TimestampOffset: &base}}}
			run := func(_ context.Context, name string, args, env []string, stdout io.Writer) error {
				if name != "ffprobe" {
					t.Fatal("mux started without a known subtitle origin")
				}
				if output == "" {
					return errors.New("probe unavailable")
				}
				_, err := io.WriteString(stdout, output)
				return err
			}
			if err := New(run, nil, testLogger{}).MuxHLS(context.Background(), domain.Job{}, hls); err == nil {
				t.Fatal("accepted an unknown video origin")
			}
		})
	}
}

func TestSubtitleInputOffsetAcrossMPEGTSWrap(t *testing.T) {
	const wrap = time.Duration(1<<33) * time.Second / 90000
	for _, tc := range []struct{ base, video, want time.Duration }{
		{10 * time.Second, 10 * time.Second, 0},
		{0, 10 * time.Second, -10 * time.Second},
		{wrap - time.Second, -time.Second, 0},
		{time.Second, wrap - time.Second, 2 * time.Second},
	} {
		if got := subtitleInputOffset(tc.base, tc.video); got != tc.want {
			t.Errorf("base=%v video=%v: got=%v want=%v", tc.base, tc.video, got, tc.want)
		}
	}
}

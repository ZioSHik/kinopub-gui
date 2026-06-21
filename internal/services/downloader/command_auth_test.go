package downloader

import (
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestBuildFFmpegArgs_InjectsAuthBeforeEachInput(t *testing.T) {
	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Series: "s", Season: 1, Episode: 1}},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{Kind: domain.MediaHLS, URL: "https://cdn/master.m3u8"},
			Video:  domain.VideoTrack{Index: 0},
			Audio: []domain.AudioTrack{
				{Index: 0, Language: "ru", Studio: "Studio"},
			},
		},
		OutPath: "/out/S01E01.mkv",
	}

	auth := domain.RequestAuth{
		Cookie:    "cf_clearance=abc",
		UserAgent: "Mozilla/5.0 (Test)",
		Headers:   map[string]string{"X-Extra": "1"},
	}

	args := BuildFFmpegArgs(job, nil, auth, "/tmp/out.mkv.tmp", nil)

	// Count inputs (-i) and auth option groups (-user_agent).
	var inputs, uaOpts, headerOpts int
	for i, a := range args {
		switch a {
		case "-i":
			inputs++
		case "-user_agent":
			uaOpts++
			if i+1 >= len(args) || args[i+1] != auth.UserAgent {
				t.Errorf("-user_agent not followed by expected UA")
			}
		case "-headers":
			headerOpts++
			if i+1 >= len(args) {
				t.Fatal("-headers missing value")
			}
			val := args[i+1]
			if !strings.Contains(val, "Cookie: cf_clearance=abc") {
				t.Errorf("-headers missing Cookie line: %q", val)
			}
			if !strings.Contains(val, "X-Extra: 1") {
				t.Errorf("-headers missing extra header: %q", val)
			}
			if !strings.HasSuffix(val, "\r\n") {
				t.Errorf("-headers value must end with CRLF: %q", val)
			}
		}
	}

	// HLS with 1 audio → 2 inputs (video + audio), each preceded by auth.
	if inputs != 2 {
		t.Fatalf("expected 2 inputs, got %d", inputs)
	}
	if uaOpts != inputs {
		t.Errorf("expected one -user_agent per input (%d), got %d", inputs, uaOpts)
	}
	if headerOpts != inputs {
		t.Errorf("expected one -headers per input (%d), got %d", inputs, headerOpts)
	}
}

func TestBuildFFmpegArgs_NoAuthNoExtraOpts(t *testing.T) {
	job := domain.Job{
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{Kind: domain.MediaProgressive, URL: "https://cdn/v.mp4"},
			Video:  domain.VideoTrack{Index: 0},
		},
	}

	args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/out.mkv.tmp", nil)

	for _, a := range args {
		if a == "-user_agent" || a == "-headers" {
			t.Errorf("did not expect auth option %q with empty auth", a)
		}
	}
}

package audiomenu

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

var sampleTracks = []domain.AudioTrackInfo{
	{Index: 0, Name: "01. Многоголосый. AniLibria (RUS)", Language: "rus"},
	{Index: 1, Name: "02. Двухголосый (RUS)", Language: "rus"},
	{Index: 2, Name: "03. Оригинал (JPN)", Language: "jpn"},
}

func TestChooseAudio_Selection(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []int
	}{
		{"single", "1\n", []int{0}},
		{"multi", "1,3\n", []int{0, 2}},
		{"range", "1-2\n", []int{0, 1}},
		{"all keyword", "all\n", nil},
		{"empty line", "\n", nil},
		{"whitespace", "  2  \n", []int{1}},
		{"out of order dedup", "3,1,1\n", []int{0, 2}},
		{"invalid keeps all", "abc\n", nil},
		{"out of range keeps all", "9\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(strings.NewReader(tt.input), &bytes.Buffer{}, true)
			got, err := c.ChooseAudio(sampleTracks, time.Second)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ChooseAudio(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestChooseAudio_Timeout(t *testing.T) {
	// A reader that never delivers a newline simulates an idle user.
	pr, _ := newBlockingReader()
	c := New(pr, &bytes.Buffer{}, true)
	start := time.Now()
	got, err := c.ChooseAudio(sampleTracks, 80*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("timeout should keep all (nil), got %v", got)
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Errorf("returned too early (%s); expected to wait for timeout", elapsed)
	}
}

func TestChooseAudio_NonInteractiveKeepsAll(t *testing.T) {
	c := New(strings.NewReader("1\n"), &bytes.Buffer{}, false)
	got, err := c.ChooseAudio(sampleTracks, time.Second)
	if err != nil || got != nil {
		t.Fatalf("non-interactive should keep all: got %v err %v", got, err)
	}
}

func TestChooseAudio_SingleTrackSkipsPrompt(t *testing.T) {
	one := sampleTracks[:1]
	c := New(strings.NewReader("1\n"), &bytes.Buffer{}, true)
	got, err := c.ChooseAudio(one, time.Second)
	if err != nil || got != nil {
		t.Fatalf("single track should keep all without prompting: got %v err %v", got, err)
	}
}

func TestChooseAudio_RendersTracks(t *testing.T) {
	out := &bytes.Buffer{}
	c := New(strings.NewReader("1\n"), out, true)
	_, _ = c.ChooseAudio(sampleTracks, time.Second)
	rendered := out.String()
	for _, want := range []string{"AniLibria", "Двухголосый", "Оригинал", "1.", "2.", "3."} {
		if !strings.Contains(rendered, want) {
			t.Errorf("menu output missing %q; got:\n%s", want, rendered)
		}
	}
}

func TestParseIndexSelection(t *testing.T) {
	tests := []struct {
		in      string
		n       int
		want    []int
		wantErr bool
	}{
		{"1,3", 3, []int{0, 2}, false},
		{"1-3", 3, []int{0, 1, 2}, false},
		{"2-1", 3, []int{0, 1}, false}, // reversed range tolerated
		{"3,1,2", 3, []int{0, 1, 2}, false},
		{"0", 3, nil, true},
		{"4", 3, nil, true},
		{"x", 3, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseIndexSelection(tt.in, tt.n)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIndexSelection(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// blockingReader blocks on Read until closed, never returning data. Used to
// simulate a user who never types anything.
type blockingReader struct {
	ch chan struct{}
}

func newBlockingReader() (*blockingReader, func()) {
	r := &blockingReader{ch: make(chan struct{})}
	return r, func() { close(r.ch) }
}

func (r *blockingReader) Read(p []byte) (int, error) {
	<-r.ch
	return 0, nil
}

func TestDecodeKeystrokes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLine string
		wantOK   bool
	}{
		{"tab auto-continues", "\t", "", true},
		{"tab ignores prior typing", "12\t", "", true},
		{"enter submits typed", "1,3\r", "1,3", true},
		{"enter newline submits", "2\n", "2", true},
		{"enter on empty line", "\r", "", true},
		{"ctrl-c cancels", "\x03", "", false},
		{"ctrl-d cancels", "\x04", "", false},
		{"backspace erases", "12\x7f3\r", "13", true},
		{"eof returns typed", "1", "1", true},
		{"eof empty not ok", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLine, gotOK := decodeKeystrokes(strings.NewReader(tt.input), &bytes.Buffer{})
			if gotLine != tt.wantLine || gotOK != tt.wantOK {
				t.Errorf("decodeKeystrokes(%q) = (%q, %v), want (%q, %v)",
					tt.input, gotLine, gotOK, tt.wantLine, tt.wantOK)
			}
		})
	}
}

func TestDecodeKeystrokes_EchoesPrintable(t *testing.T) {
	var out bytes.Buffer
	decodeKeystrokes(strings.NewReader("1,3\r"), &out)
	if got := out.String(); got != "1,3" {
		t.Errorf("echo = %q, want %q", got, "1,3")
	}
}

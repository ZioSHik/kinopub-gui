package audiomenu

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// twoTracks is the minimum interactive prompt (>1 track) used by tests that
// exercise the ChooseAudio keyword/selection branches.
var twoTracks = []domain.AudioTrackInfo{
	{Index: 0, Name: "Track A", Language: "rus"},
	{Index: 1, Name: "Track B", Language: "eng"},
}

// TestChooseAudio_Keywords pins the keyword branch: "all", "*", "none", and the
// empty string all keep everything (nil). "none" is deliberately treated as
// "all" so the output always carries audio (see the doc comment). Case and
// surrounding whitespace are normalized before matching.
func TestChooseAudio_Keywords(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"all lower", "all\n"},
		{"all upper", "ALL\n"},
		{"all padded", "  all  \n"},
		{"star", "*\n"},
		{"none keeps all", "none\n"},
		{"none mixed case", "NoNe\n"},
		{"empty", "\n"},
		{"whitespace only", "   \n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			c := New(strings.NewReader(tt.input), out, true)
			got, err := c.ChooseAudio(twoTracks, time.Second)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != nil {
				t.Fatalf("keyword %q should keep all (nil), got %v", tt.input, got)
			}
		})
	}
}

// TestChooseAudio_DefaultTimeout exercises the timeout<=0 branch which should
// substitute DefaultTimeout. The render line embeds the rounded timeout, so we
// can assert the prompt reflects DefaultTimeout rather than a zero duration.
func TestChooseAudio_DefaultTimeout(t *testing.T) {
	out := &bytes.Buffer{}
	c := New(strings.NewReader("all\n"), out, true)
	got, err := c.ChooseAudio(twoTracks, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
	want := DefaultTimeout.Round(time.Second).String()
	if !strings.Contains(out.String(), want) {
		t.Errorf("prompt should embed default timeout %q; got:\n%s", want, out.String())
	}
}

// TestChooseAudio_NegativeTimeout is the boundary sibling of the above: a
// negative timeout must also fall back to DefaultTimeout, not panic or return
// instantly with an error.
func TestChooseAudio_NegativeTimeout(t *testing.T) {
	out := &bytes.Buffer{}
	c := New(strings.NewReader("all\n"), out, true)
	got, err := c.ChooseAudio(twoTracks, -5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
	if !strings.Contains(out.String(), DefaultTimeout.Round(time.Second).String()) {
		t.Errorf("negative timeout should fall back to default; got:\n%s", out.String())
	}
}

// TestChooseAudio_ZeroTracks covers the len(tracks) <= 1 short-circuit at the
// nil/empty boundary: zero tracks keeps all without prompting (no panic, no
// output).
func TestChooseAudio_ZeroTracks(t *testing.T) {
	out := &bytes.Buffer{}
	c := New(strings.NewReader("1\n"), out, true)
	got, err := c.ChooseAudio(nil, time.Second)
	if err != nil || got != nil {
		t.Fatalf("zero tracks should keep all: got %v err %v", got, err)
	}
	if out.Len() != 0 {
		t.Errorf("zero tracks should not render a prompt; got:\n%s", out.String())
	}
}

// TestChooseAudio_AllIndicesSelected verifies a selection that names every
// track returns those indices (non-nil), distinct from the "keep all" nil
// sentinel.
func TestChooseAudio_AllIndicesSelected(t *testing.T) {
	out := &bytes.Buffer{}
	c := New(strings.NewReader("1-2\n"), out, true)
	got, err := c.ChooseAudio(twoTracks, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []int{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestChooseAudio_InvalidReportsError checks the error message is surfaced to
// the user (and all tracks kept) when the selection is malformed.
func TestChooseAudio_InvalidReportsError(t *testing.T) {
	out := &bytes.Buffer{}
	c := New(strings.NewReader("9\n"), out, true)
	got, err := c.ChooseAudio(twoTracks, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("out-of-range should keep all, got %v", got)
	}
	if !strings.Contains(out.String(), "Invalid selection") {
		t.Errorf("expected an Invalid selection notice; got:\n%s", out.String())
	}
}

// TestRender_LabelFallbacks pins the label-construction logic in render across
// its branches: name-only, language-only (empty name), both-empty ("Audio"),
// and the bracket-append behavior when the name does not already mention the
// language.
func TestRender_LabelFallbacks(t *testing.T) {
	tracks := []domain.AudioTrackInfo{
		{Name: "Multivoice Dub", Language: "rus"},         // label lacks "rus" substring → append [rus]
		{Name: "", Language: "eng"},                       // empty name → falls back to language
		{Name: "", Language: ""},                          // both empty → "Audio"
		{Name: "English Original (eng)", Language: "eng"}, // name already mentions eng → no append
	}
	out := &bytes.Buffer{}
	c := New(strings.NewReader("all\n"), out, true)
	if _, err := c.ChooseAudio(tracks, time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rendered := out.String()

	wantLines := []string{
		"1. Multivoice Dub [rus]",
		"2. eng",
		"3. Audio",
		"4. English Original (eng)",
	}
	for _, w := range wantLines {
		if !strings.Contains(rendered, w) {
			t.Errorf("rendered menu missing %q; got:\n%s", w, rendered)
		}
	}
	// The last entry must NOT have a doubled language bracket appended.
	if strings.Contains(rendered, "English Original (eng) [eng]") {
		t.Errorf("language wrongly re-appended; got:\n%s", rendered)
	}
}

// TestRender_LabelCaseInsensitiveLanguage ensures the "does the label already
// contain the language" check is case-insensitive: a name containing the
// language in different case must NOT get a redundant bracket.
func TestRender_LabelCaseInsensitiveLanguage(t *testing.T) {
	tracks := []domain.AudioTrackInfo{
		{Name: "RUS multi", Language: "rus"}, // contains "rus" case-insensitively
		{Name: "Other", Language: "jpn"},
	}
	out := &bytes.Buffer{}
	c := New(strings.NewReader("all\n"), out, true)
	if _, err := c.ChooseAudio(tracks, time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "RUS multi [rus]") {
		t.Errorf("language bracket should not be appended when label already contains it (case-insensitive); got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "1. RUS multi") {
		t.Errorf("expected plain 'RUS multi' label; got:\n%s", rendered)
	}
}

// TestParseIndexSelection_Extra extends coverage of parseIndexSelection across
// edge/error branches not exercised by the existing table: empty parts, bad
// range halves, out-of-range inside a range, and dedup across overlapping
// ranges. Asserts the documented behavior (sorted, de-duplicated, 0-based).
func TestParseIndexSelection_Extra(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		n       int
		want    []int
		wantErr bool
	}{
		{"trailing comma ignored", "1,", 3, []int{0}, false},
		{"leading comma ignored", ",2", 3, []int{1}, false},
		{"double comma", "1,,3", 3, []int{0, 2}, false},
		{"only commas", ",,,", 3, []int{}, false},
		{"spaces in range", " 1 - 3 ", 3, []int{0, 1, 2}, false},
		{"overlapping ranges dedup", "1-2,2-3", 3, []int{0, 1, 2}, false},
		{"single element range", "2-2", 3, []int{1}, false},
		{"bad range low", "x-2", 3, nil, true},
		{"bad range high", "1-y", 3, nil, true},
		{"range out of range high", "1-9", 3, nil, true},
		{"range out of range low (reversed)", "3-0", 3, nil, true},
		{"negative index via minus prefix is range", "-1", 3, nil, true},
		{"empty string yields empty", "", 3, []int{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIndexSelection(tt.in, tt.n)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIndexSelection(%q, %d) = %v, want %v", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

// TestRawTTY covers both branches of rawTTY: a non-*os.File reader is never a
// raw TTY, and a real *os.File that is not a terminal (a regular temp file)
// also reports false.
func TestRawTTY(t *testing.T) {
	// Non-*os.File reader.
	c := New(strings.NewReader(""), &bytes.Buffer{}, true)
	if _, ok := c.rawTTY(); ok {
		t.Errorf("strings.Reader must not be reported as a raw TTY")
	}

	// Real *os.File that is a regular file, not a terminal.
	path := filepath.Join(t.TempDir(), "not-a-tty")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close()
	c2 := New(f, &bytes.Buffer{}, true)
	if _, ok := c2.rawTTY(); ok {
		t.Errorf("regular file must not be reported as a raw TTY")
	}
}

// TestReadSelection_NonTerminalFallsBack confirms readSelection routes
// non-terminal input (a regular *os.File) through buffered line reading and
// returns the typed line.
func TestReadSelection_NonTerminalFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte("1,3\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	c := New(f, &bytes.Buffer{}, true)
	line, ok := c.readSelection(time.Second)
	if !ok {
		t.Fatalf("expected ok reading from a file")
	}
	if strings.TrimSpace(line) != "1,3" {
		t.Errorf("line = %q, want %q", line, "1,3")
	}
}

// TestReadLineWithTimeout_Timeout exercises the timeout branch of
// readLineWithTimeout directly with a reader that never yields a line.
func TestReadLineWithTimeout_Timeout(t *testing.T) {
	br, _ := newBlockingReader()
	line, ok := readLineWithTimeout(br, 30*time.Millisecond)
	if ok || line != "" {
		t.Errorf("expected ('', false) on timeout, got (%q, %v)", line, ok)
	}
}

// TestReadLineWithTimeout_EOFEmpty: an immediately-EOF reader with no data
// returns ("", false) — the err!=nil && line=="" branch.
func TestReadLineWithTimeout_EOFEmpty(t *testing.T) {
	line, ok := readLineWithTimeout(strings.NewReader(""), time.Second)
	if ok || line != "" {
		t.Errorf("empty EOF should be ('', false), got (%q, %v)", line, ok)
	}
}

// TestReadLineWithTimeout_NoTrailingNewline: a reader that yields data but EOFs
// before a newline still returns the partial line (err!=nil but line!="").
func TestReadLineWithTimeout_NoTrailingNewline(t *testing.T) {
	line, ok := readLineWithTimeout(strings.NewReader("1,2"), time.Second)
	if !ok {
		t.Fatalf("expected ok for non-empty partial line")
	}
	if line != "1,2" {
		t.Errorf("line = %q, want %q", line, "1,2")
	}
}

// TestDecodeKeystrokes_BackspaceUnderflow checks backspace on an empty buffer
// is a no-op (no panic, nothing erased) and does not emit an erase sequence.
func TestDecodeKeystrokes_BackspaceUnderflow(t *testing.T) {
	var out bytes.Buffer
	line, ok := decodeKeystrokes(strings.NewReader("\x7f\x7fA\r"), &out)
	if !ok || line != "A" {
		t.Fatalf("got (%q, %v), want (%q, true)", line, ok, "A")
	}
	// Only the printable 'A' should have been echoed; no erase sequences.
	if out.String() != "A" {
		t.Errorf("echo = %q, want %q (backspace on empty buffer must not emit erase)", out.String(), "A")
	}
}

// TestDecodeKeystrokes_BackspaceEmitsErase confirms a backspace that actually
// removes a character emits the visual erase sequence "\b \b".
func TestDecodeKeystrokes_BackspaceEmitsErase(t *testing.T) {
	var out bytes.Buffer
	line, ok := decodeKeystrokes(strings.NewReader("A\x7f\r"), &out)
	if !ok || line != "" {
		t.Fatalf("got (%q, %v), want ('', true)", line, ok)
	}
	if out.String() != "A\b \b" {
		t.Errorf("echo = %q, want %q", out.String(), "A\b \b")
	}
}

// TestDecodeKeystrokes_IgnoresControlBytes verifies non-terminating control
// bytes below 0x20 (other than handled ones) are silently dropped and not
// accumulated into the line.
func TestDecodeKeystrokes_IgnoresControlBytes(t *testing.T) {
	// 0x01 (Ctrl-A) and 0x1b (ESC) are ignored; surrounding printables kept.
	line, ok := decodeKeystrokes(strings.NewReader("1\x012\x1b3\r"), &bytes.Buffer{})
	if !ok || line != "123" {
		t.Errorf("got (%q, %v), want (%q, true)", line, ok, "123")
	}
}

// TestDecodeKeystrokes_CtrlCAfterTyping: Ctrl-C cancels even after characters
// were typed, discarding the typed line.
func TestDecodeKeystrokes_CtrlCAfterTyping(t *testing.T) {
	line, ok := decodeKeystrokes(strings.NewReader("12\x03"), &bytes.Buffer{})
	if ok || line != "" {
		t.Errorf("Ctrl-C after typing should cancel: got (%q, %v)", line, ok)
	}
}

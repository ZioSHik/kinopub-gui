// Package audiomenu provides an interactive, time-boxed CLI picker for audio
// tracks. It implements domain.AudioChooser: the user is shown the available
// tracks and given a bounded window to pick which to keep. If they make no
// choice in time (or input is not a terminal), all tracks are kept.
package audiomenu

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// DefaultTimeout is the window the picker waits for input before defaulting to
// "keep all".
const DefaultTimeout = 90 * time.Second

// Chooser renders an interactive audio picker over in/out. It is constructed
// with the program's stdin and a writer for the menu (usually stderr).
type Chooser struct {
	in          io.Reader
	out         io.Writer
	interactive bool
}

// New builds a Chooser. interactive should be true only when in is a real TTY;
// when false, ChooseAudio immediately keeps all tracks without prompting.
func New(in io.Reader, out io.Writer, interactive bool) *Chooser {
	return &Chooser{in: in, out: out, interactive: interactive}
}

// rawTTY reports whether in is a real terminal whose file descriptor we can
// switch into raw mode for single-keystroke input (so TAB is seen immediately
// without waiting for Enter). When in is a pipe, a *strings.Reader (tests), or
// any non-terminal source, this returns (nil, false) and the caller falls back
// to buffered line reading.
func (c *Chooser) rawTTY() (*os.File, bool) {
	f, ok := c.in.(*os.File)
	if !ok {
		return nil, false
	}
	if !term.IsTerminal(int(f.Fd())) {
		return nil, false
	}
	return f, true
}

// ChooseAudio implements domain.AudioChooser. It prints the track list and
// reads a selection line from in, waiting at most timeout. The selection
// syntax mirrors the season/episode selectors: comma-separated 1-based indices
// and ranges, e.g. "1,3" or "1-2". "all" (or empty input / timeout) keeps
// everything; "none" is treated as "all" so the output always carries audio.
//
// Returned indices are 0-based (into tracks). A nil result means "keep all".
func (c *Chooser) ChooseAudio(tracks []domain.AudioTrackInfo, timeout time.Duration) ([]int, error) {
	if len(tracks) <= 1 || !c.interactive {
		return nil, nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	c.render(tracks, timeout)

	line, ok := c.readSelection(timeout)
	if !ok {
		fmt.Fprintln(c.out, "\nNo selection — keeping all audio tracks.")
		return nil, nil
	}

	sel := strings.ToLower(strings.TrimSpace(line))
	switch sel {
	case "", "all", "*", "none":
		fmt.Fprintln(c.out, "Keeping all audio tracks.")
		return nil, nil
	}

	idx, err := parseIndexSelection(sel, len(tracks))
	if err != nil {
		fmt.Fprintf(c.out, "Invalid selection (%v) — keeping all audio tracks.\n", err)
		return nil, nil
	}
	if len(idx) == 0 {
		return nil, nil
	}
	return idx, nil
}

// render prints the prompt and track list.
func (c *Chooser) render(tracks []domain.AudioTrackInfo, timeout time.Duration) {
	fmt.Fprintf(c.out, "\nAvailable audio tracks (choose within %s, Enter or TAB = all):\n", timeout.Round(time.Second))
	for i, t := range tracks {
		label := t.Name
		if label == "" {
			label = t.Language
		}
		if label == "" {
			label = "Audio"
		}
		if t.Language != "" && !strings.Contains(strings.ToLower(label), strings.ToLower(t.Language)) {
			label = fmt.Sprintf("%s [%s]", label, t.Language)
		}
		fmt.Fprintf(c.out, "  %d. %s\n", i+1, label)
	}
	fmt.Fprint(c.out, "Selection (e.g. 1,3 or 2-3; Enter/TAB or 'all' to keep everything): ")
}

// readSelection reads the user's selection within timeout. When in is a real
// terminal it switches to raw mode and reads keystrokes one at a time, so a
// single TAB (or Enter on an empty line) immediately accepts the default and
// auto-continues with all tracks — no Enter required. For non-terminal input
// (pipes, tests) it falls back to buffered line reading.
//
// It returns (line, true) when input was gathered, or ("", false) on timeout.
// A TAB keystroke is reported as an empty line, which the caller treats as
// "keep all".
func (c *Chooser) readSelection(timeout time.Duration) (string, bool) {
	if f, ok := c.rawTTY(); ok {
		return readKeysWithTimeout(f, c.out, timeout)
	}
	return readLineWithTimeout(c.in, timeout)
}

// readLineWithTimeout reads a single line from r, returning (line, true) if a
// line arrives within d, or ("", false) on timeout. The background read may
// outlive the timeout; that is acceptable for a short-lived CLI prompt.
func readLineWithTimeout(r io.Reader, d time.Duration) (string, bool) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(r)
		line, err := reader.ReadString('\n')
		ch <- result{line: line, err: err}
	}()

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case res := <-ch:
		if res.err != nil && res.line == "" {
			return "", false
		}
		return res.line, true
	case <-timer.C:
		return "", false
	}
}

// readKeysWithTimeout reads keystrokes from a terminal in raw mode so that a
// single TAB or Enter is acted on immediately, without the user pressing Enter.
//
// Behavior:
//   - TAB              → auto-continue, returns ("", true) → caller keeps all.
//   - Enter (CR/LF)    → returns the line typed so far.
//   - Ctrl-C / Ctrl-D  → returns ("", false) → caller keeps all (treated like
//     "no selection").
//   - Backspace/DEL    → erases the last typed character.
//   - Printable bytes  → appended to the line and echoed.
//
// On timeout it returns ("", false). The terminal is always restored to its
// prior mode before returning.
func readKeysWithTimeout(f *os.File, out io.Writer, d time.Duration) (string, bool) {
	fd := int(f.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Can't enter raw mode — fall back to line-buffered reading.
		return readLineWithTimeout(f, d)
	}
	defer term.Restore(fd, oldState)

	type result struct {
		line string
		ok   bool
	}
	ch := make(chan result, 1)

	go func() {
		line, ok := decodeKeystrokes(f, out)
		ch <- result{line: line, ok: ok}
	}()

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case res := <-ch:
		// Move to a fresh line so subsequent output isn't appended to the
		// raw-mode prompt line.
		fmt.Fprint(out, "\r\n")
		return res.line, res.ok
	case <-timer.C:
		return "", false
	}
}

// decodeKeystrokes consumes one keystroke at a time from r (expected to be a
// terminal in raw mode) and resolves the user's intent, echoing printable
// input to out. It returns when a terminating key is seen:
//
//   - TAB              → ("", true)  — auto-continue with the default.
//   - Enter (CR/LF)    → (typed, true).
//   - Ctrl-C / Ctrl-D  → ("", false) — cancel / no selection.
//   - EOF or error     → (typed, len(typed) > 0).
//
// Backspace/DEL erases the last character; other control bytes are ignored.
// Splitting this out from terminal setup keeps the decode logic unit-testable
// without a real TTY.
func decodeKeystrokes(r io.Reader, out io.Writer) (string, bool) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			switch b := buf[0]; b {
			case '\t': // TAB → auto-continue with the default.
				return "", true
			case '\r', '\n': // Enter → submit what was typed.
				return string(line), true
			case 0x03, 0x04: // Ctrl-C / Ctrl-D → no selection.
				return "", false
			case 0x7f, '\b': // Backspace / DEL.
				if len(line) > 0 {
					line = line[:len(line)-1]
					// Erase the character visually: back up, space, back up.
					fmt.Fprint(out, "\b \b")
				}
			default:
				if b >= 0x20 { // Printable — echo and accumulate.
					line = append(line, b)
					out.Write([]byte{b})
				}
			}
		}
		if rerr != nil {
			return string(line), len(line) > 0
		}
	}
}

// parseIndexSelection parses a 1-based selection like "1,3-4" into sorted,
// de-duplicated 0-based indices. Out-of-range values are an error.
func parseIndexSelection(s string, n int) ([]int, error) {
	seen := make(map[int]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i := strings.Index(part, "-"); i >= 0 {
			lo, err := strconv.Atoi(strings.TrimSpace(part[:i]))
			if err != nil {
				return nil, fmt.Errorf("bad range %q", part)
			}
			hi, err := strconv.Atoi(strings.TrimSpace(part[i+1:]))
			if err != nil {
				return nil, fmt.Errorf("bad range %q", part)
			}
			if lo > hi {
				lo, hi = hi, lo
			}
			for v := lo; v <= hi; v++ {
				if v < 1 || v > n {
					return nil, fmt.Errorf("index %d out of range [1,%d]", v, n)
				}
				seen[v-1] = true
			}
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("bad index %q", part)
		}
		if v < 1 || v > n {
			return nil, fmt.Errorf("index %d out of range [1,%d]", v, n)
		}
		seen[v-1] = true
	}

	out := make([]int, 0, len(seen))
	for v := 0; v < n; v++ {
		if seen[v] {
			out = append(out, v)
		}
	}
	return out, nil
}

// Verify Chooser satisfies the port at compile time.
var _ domain.AudioChooser = (*Chooser)(nil)

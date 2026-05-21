package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StatusLine renders a single rewritable progress line to stderr while a
// long-running step is in flight, then rewrites it with a final summary on
// completion. The spinner ticks every ~120ms so the operator always sees the
// step is alive — silence during a long step is a regression.
//
// Output shape during the step:
//
//	  ⠋ Stripe: fetching page 3/12         1.2s
//
// On Final:
//
//	  ✓ Stripe: 12 new, 3 updated          1.4s
//
// Stderr is used so the compact `chb pull` can still silence each sub-sync's
// stdout for layout without losing the status line.
type StatusLine struct {
	enabled  bool // false in --json / non-TTY mode
	label    string
	started  time.Time
	subtask  atomic.Pointer[string]
	stopCh   chan struct{}
	doneCh   chan struct{}
	mu       sync.Mutex // guards write/clear (avoid interleaving with Warnf)
	lastLen  int
	finished bool
}

// stderrIsTTY caches whether stderr is a terminal — needed because the
// spinner would dump escape codes into a pipe otherwise.
var stderrIsTTY = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}()

// activeStatusLine is the (optional) reporter the current step has installed.
// Sub-syncs call Progress(msg) which is a no-op when nothing is installed
// (e.g. running `chb providers stripe pull` directly).
var (
	activeStatusMu sync.Mutex
	activeStatus   *StatusLine
)

// SetActiveStatusLine installs s as the active reporter. Pass nil to clear.
func SetActiveStatusLine(s *StatusLine) {
	activeStatusMu.Lock()
	activeStatus = s
	activeStatusMu.Unlock()
}

// Progress reports a short subtask label ("fetching page 3/12") for whatever
// step the caller is inside. No-op when no StatusLine is active so direct
// commands keep working unchanged.
func Progress(msg string) {
	activeStatusMu.Lock()
	s := activeStatus
	activeStatusMu.Unlock()
	if s == nil {
		return
	}
	s.SetSubtask(msg)
}

// NewStatusLine starts a status line for label. The label is printed
// immediately (before any I/O happens in the caller) so the operator sees
// `  ⟳ Stripe…` the moment the step begins.
func NewStatusLine(label string) *StatusLine {
	s := &StatusLine{
		label:   label,
		started: time.Now(),
		enabled: stderrIsTTY,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	empty := ""
	s.subtask.Store(&empty)
	if !s.enabled {
		// Non-TTY: print start marker once, no spinner.
		fmt.Fprintf(os.Stderr, "  ⟳ %s…\n", label)
		close(s.doneCh)
		return s
	}
	go s.spin()
	return s
}

// SetSubtask updates the in-flight subtask message. The next spinner tick
// will redraw with the new message.
func (s *StatusLine) SetSubtask(msg string) {
	if s == nil {
		return
	}
	m := msg
	s.subtask.Store(&m)
}

// Final rewrites the status line with the terminal mark + summary +
// elapsed, then advances to a new line. After Final, the spinner is stopped
// and further SetSubtask calls are ignored.
func (s *StatusLine) Final(mark, summary string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	s.mu.Unlock()

	if s.enabled {
		close(s.stopCh)
		<-s.doneCh
	}

	elapsed := time.Since(s.started).Round(100 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enabled {
		s.clear()
	}
	line := s.formatFinal(mark, summary, elapsed)
	fmt.Fprintln(os.Stderr, line)
}

// spin redraws the status line every ~120ms while the step is running.
func (s *StatusLine) spin() {
	defer close(s.doneCh)
	frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
	t := time.NewTicker(120 * time.Millisecond)
	defer t.Stop()
	i := 0
	s.draw(string(frames[i]))
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			i = (i + 1) % len(frames)
			s.draw(string(frames[i]))
		}
	}
}

func (s *StatusLine) draw(spinner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	sub := ""
	if p := s.subtask.Load(); p != nil {
		sub = *p
	}
	elapsed := time.Since(s.started).Round(100 * time.Millisecond)
	line := s.format(spinner, sub, elapsed)
	s.clear()
	_, _ = fmt.Fprint(os.Stderr, line)
	s.lastLen = visibleLen(line)
}

// clear wipes the previously drawn line with spaces and returns the cursor
// to column 0 so the next write starts clean.
func (s *StatusLine) clear() {
	if s.lastLen == 0 {
		return
	}
	_, _ = fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", s.lastLen)+"\r")
	s.lastLen = 0
}

// statusBodyMaxWidth returns the printable width of the "label:
// subtask" body column. The body is right-padded to this width on
// every row so the elapsed-time column stays at a constant horizontal
// position — otherwise the timer jumps left/right every time the
// subtask label changes length.
//
// Reserved columns on the row:
//
//	2 (gutter) + 1 (mark) + 1 (space) + body + 2 (spaces) + 5 (4.1s)
//
// We pick `terminalWidth − 20` for the body cap (overhead of 11 plus a
// 9-column safety margin) so the line still fits on one physical row
// even when the body contains wide characters. `displayWidth` is
// rune-based and undercounts wide runes like emoji (💶 is one rune
// but two columns); without the safety margin a body that hits the
// cap can wrap, and `\r` only erases the last wrapped line — which is
// what produced the staircase of repeated spinner frames in the
// scrollback. Floor 40 so even narrow terminals keep a usable column.
func statusBodyMaxWidth() int {
	w := TerminalWidth()
	body := w - 20
	if body < 40 {
		body = 40
	}
	return body
}

// fitBody pads or truncates body to exactly statusBodyMaxWidth()
// columns of visible width. The padding is what keeps the elapsed-
// time column from jumping; the truncation is what keeps the row
// from wrapping. Critically, the measurement strips ANSI colour
// sequences before counting — otherwise a coloured body string (e.g.
// a red error summary on a ✗ row) gets less padding than an uncoloured
// one, which is exactly what made the timer jitter horizontally
// between rows.
func fitBody(body string) string {
	max := statusBodyMaxWidth()
	w := visibleLen(body)
	if w == max {
		return body
	}
	if w < max {
		return body + strings.Repeat(" ", max-w)
	}
	// Too wide: truncate to max-1 visible columns and append "…". ANSI
	// escapes are passed through (with their counters preserved) so
	// colours don't bleed past the truncation point.
	if max <= 1 {
		return "…"
	}
	out := make([]rune, 0, len(body))
	used := 0
	inEsc := false
	for _, r := range body {
		if inEsc {
			out = append(out, r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			out = append(out, r)
			inEsc = true
			continue
		}
		if used >= max-1 {
			break
		}
		out = append(out, r)
		used++
	}
	return string(out) + "…"
}

func (s *StatusLine) format(spinner, sub string, elapsed time.Duration) string {
	body := s.label
	if sub != "" {
		body = s.label + ": " + sub
	}
	return fmt.Sprintf("  %s %s  %s%s%s",
		spinner,
		fitBody(body),
		Fmt.Dim, FormatElapsedFixed(elapsed), Fmt.Reset)
}

func (s *StatusLine) formatFinal(mark, summary string, elapsed time.Duration) string {
	body := s.label
	if summary != "" {
		body = s.label + ": " + summary
	}
	return fmt.Sprintf("  %s %s  %s%s%s",
		mark,
		fitBody(body),
		Fmt.Dim, FormatElapsedFixed(elapsed), Fmt.Reset)
}

// FormatElapsedFixed renders a duration as "X.Ys" with always one decimal
// digit, so the column doesn't jump from "2.0s" → "2s" → "2.1s" as
// time.Duration.String() would naturally do. Sub-millisecond durations
// round up to 0.0s.
func FormatElapsedFixed(d time.Duration) string {
	secs := d.Seconds()
	if secs < 0 {
		secs = 0
	}
	return fmt.Sprintf("%4.1fs", secs)
}

// fitWidth pads s with spaces on the right to exactly n visible columns,
// truncating with "…" if it's wider.
func fitWidth(s string, n int) string {
	w := displayWidth(s)
	if w == n {
		return s
	}
	if w < n {
		return s + strings.Repeat(" ", n-w)
	}
	// Truncate by display width — assume one rune per column for ASCII;
	// status lines are short and we control their content.
	if n <= 1 {
		return "…"
	}
	out := []rune{}
	used := 0
	for _, r := range s {
		if used >= n-1 {
			break
		}
		out = append(out, r)
		used++
	}
	return string(out) + "…"
}

// visibleLen returns the printable width of s, ignoring ANSI escape sequences.
// Good enough for our short status lines.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		n++
	}
	return n
}

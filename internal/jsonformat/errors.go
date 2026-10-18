package jsonformat

import (
	"fmt"
	"io"
	"strings"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// SyntaxError describes malformed JSON: where it is, what is wrong and the
// source lines around it.
type SyntaxError struct {
	// Pos is the position of the offending character (or of the end of the
	// input for unexpected EOF).
	Pos Pos
	// Msg is a human-friendly description of the problem.
	Msg string
	// Hint is an optional suggestion for fixing the problem.
	Hint string

	lines []srcLine // context lines; the last one is the error line
}

type srcLine struct {
	num      int
	text     []rune
	firstCol int // column of text[0]
}

// Error implements error as "line:col: message".
func (e *SyntaxError) Error() string { return e.Pos.String() + ": " + e.Msg }

const (
	maxLineCols   = 120 // longer lines are windowed around the error
	windowCols    = 100 // width of the window for long lines
	windowLeading = 60  // columns kept before the error inside the window
	ellipsis      = "…"
)

// Header returns the first line of a report: "file:line:col: error: msg".
func (e *SyntaxError) Header(file string, th *ui.Theme) string {
	return th.Bold(file+":"+e.locString()+":") + " " + th.Error("error:") + " " + e.Msg
}

func (e *SyntaxError) locString() string {
	if e.Pos.Col == 0 {
		return fmt.Sprint(e.Pos.Line)
	}
	return e.Pos.String()
}

// Render writes the full report to w: the header line produced by header,
// the source snippet with a caret under the error column and the hint.
func (e *SyntaxError) Render(w io.Writer, th *ui.Theme, header string) error {
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(e.Snippet(th))
	if e.Hint != "" {
		b.WriteString(th.Muted("  hint: "+e.Hint) + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Snippet renders the context lines with a line-number gutter and a caret
// under the error column. It returns "" when no context is available.
func (e *SyntaxError) Snippet(th *ui.Theme) string {
	if len(e.lines) == 0 {
		return ""
	}
	errLine := e.lines[len(e.lines)-1]
	caretCol := e.Pos.Col // absolute column of the caret

	// Window, in absolute columns [winStart, winEnd), around the caret.
	lineEndCol := errLine.firstCol + len(errLine.text)
	winStart, winEnd := 1, lineEndCol+1
	for _, l := range e.lines {
		if end := l.firstCol + len(l.text); end > winEnd-1 {
			winEnd = end + 1
		}
	}
	if winEnd-1 > maxLineCols || errLine.firstCol > 1 {
		winStart = max(1, caretCol-windowLeading)
		winEnd = winStart + windowCols
		if winEnd > lineEndCol+1 {
			winEnd = lineEndCol + 1
			winStart = max(1, min(winStart, winEnd-windowCols))
		}
		winStart = max(winStart, errLine.firstCol)
		if winEnd <= caretCol {
			winEnd = caretCol + 1
		}
	}
	lead := winStart > 1

	width := max(4, len(fmt.Sprint(errLine.num)))
	gutter := func(n int) string { return th.Muted(fmt.Sprintf(" %*d | ", width, n)) }
	blank := th.Muted(" " + strings.Repeat(" ", width) + " | ")

	var b strings.Builder
	for _, l := range e.lines {
		b.WriteString(gutter(l.num))
		b.WriteString(windowText(l, winStart, winEnd, lead))
		b.WriteByte('\n')
	}
	// caret line: reproduce tabs and display widths of the text before it
	b.WriteString(blank)
	if lead {
		b.WriteByte(' ')
	}
	from := max(0, winStart-errLine.firstCol)
	to := min(len(errLine.text), caretCol-errLine.firstCol)
	if to > from {
		b.WriteString(padFor(errLine.text[from:to]))
	}
	b.WriteString(th.Error("^"))
	b.WriteByte('\n')
	return b.String()
}

// windowText returns the part of l visible in [winStart, winEnd), marking
// cut-off text with an ellipsis.
func windowText(l srcLine, winStart, winEnd int, lead bool) string {
	from := winStart - l.firstCol
	to := winEnd - l.firstCol
	n := len(l.text)
	var b strings.Builder
	if lead {
		if from > 0 && n > 0 {
			b.WriteString(ellipsis)
		} else {
			b.WriteByte(' ')
		}
	}
	from = max(0, min(from, n))
	to = max(from, min(to, n))
	b.WriteString(string(l.text[from:to]))
	if to < n {
		b.WriteString(ellipsis)
	}
	return strings.TrimRight(b.String(), " ")
}

// padFor returns whitespace occupying the same display columns as text:
// tabs are kept as tabs so the caret lines up whatever the tab width.
func padFor(text []rune) string {
	var b strings.Builder
	for i, seg := range strings.Split(string(text), "\t") {
		if i > 0 {
			b.WriteByte('\t')
		}
		b.WriteString(strings.Repeat(" ", ui.Width(seg)))
	}
	return b.String()
}

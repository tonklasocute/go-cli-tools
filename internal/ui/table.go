package ui

import (
	"io"
	"strings"
)

// Table is a minimal, ANSI-aware text table with a bold header and a rule
// underneath it. Cells may already contain color codes.
type Table struct {
	Header []string
	Rows   [][]string
	// RightAlign marks column indexes whose cells are right-aligned
	// (useful for numbers).
	RightAlign map[int]bool
	// MaxWidth caps individual columns; longer cells are truncated with "…".
	// Zero means unlimited.
	MaxWidth map[int]int
}

// NewTable creates a table with the given header.
func NewTable(header ...string) *Table {
	return &Table{Header: header, RightAlign: map[int]bool{}, MaxWidth: map[int]int{}}
}

// AddRow appends a row. Missing cells are rendered empty.
func (t *Table) AddRow(cells ...string) { t.Rows = append(t.Rows, cells) }

// Render writes the table to w.
func (t *Table) Render(w io.Writer, th *Theme) error {
	cols := len(t.Header)
	for _, r := range t.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return nil
	}
	cell := func(row []string, i int) string {
		if i >= len(row) {
			return ""
		}
		s := row[i]
		if m := t.MaxWidth[i]; m > 0 {
			s = Truncate(s, m)
		}
		return s
	}
	widths := make([]int, cols)
	for i := 0; i < cols; i++ {
		widths[i] = Width(cell(t.Header, i))
		for _, r := range t.Rows {
			if cw := Width(cell(r, i)); cw > widths[i] {
				widths[i] = cw
			}
		}
	}

	var b strings.Builder
	writeRow := func(row []string, style func(string) string) {
		var line strings.Builder
		for i := 0; i < cols; i++ {
			c := cell(row, i)
			pad := strings.Repeat(" ", widths[i]-Width(c))
			if style != nil {
				c = style(c)
			}
			last := i == cols-1
			switch {
			case t.RightAlign[i]:
				line.WriteString(pad + c)
			case last:
				line.WriteString(c)
			default:
				line.WriteString(c + pad)
			}
			if !last {
				line.WriteString("  ")
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteString("\n")
	}

	if len(t.Header) > 0 {
		writeRow(t.Header, func(s string) string { return th.Paint(RoleHeader, s) })
		rule := make([]string, cols)
		for i := range rule {
			rule[i] = strings.Repeat("─", widths[i])
		}
		b.WriteString(th.Muted(strings.Join(rule, "  ")) + "\n")
	}
	for _, r := range t.Rows {
		writeRow(r, nil)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Truncate shortens s to at most n printable columns, appending "…" when cut.
// Strings containing ANSI codes are returned unchanged when they fit.
func Truncate(s string, n int) string {
	if n <= 0 || Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := Width(string(r))
		if w+rw > n-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

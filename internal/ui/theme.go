// Package ui contains the terminal presentation helpers shared by every tool:
// color handling, tables, bars, spinners, progress indicators and
// human-readable formatting of sizes and durations.
package ui

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Role identifies the semantic meaning of a piece of text so that every tool
// colors things the same way.
type Role int

// Semantic roles understood by Theme.Paint.
const (
	RolePlain Role = iota
	RoleSuccess
	RoleWarn
	RoleError
	RoleInfo
	RoleMuted
	RoleBold
	RoleAccent
	RoleHeader
	// JSON syntax highlighting roles.
	RoleKey
	RoleString
	RoleNumber
	RoleBool
	RoleNull
	RolePunct
)

// Theme renders styled text. A Theme with color disabled returns its input
// unchanged, so callers never need to branch on whether color is on.
type Theme struct {
	color  bool
	styles map[Role]lipgloss.Style
}

// NewTheme returns a theme that emits ANSI colors only when color is true.
func NewTheme(color bool) *Theme {
	r := lipgloss.NewRenderer(io.Discard)
	if color {
		r.SetColorProfile(termenv.ANSI256)
	} else {
		r.SetColorProfile(termenv.Ascii)
	}
	s := func() lipgloss.Style { return r.NewStyle() }
	return &Theme{
		color: color,
		styles: map[Role]lipgloss.Style{
			RolePlain:   s(),
			RoleSuccess: s().Foreground(lipgloss.Color("2")),
			RoleWarn:    s().Foreground(lipgloss.Color("3")),
			RoleError:   s().Foreground(lipgloss.Color("1")).Bold(true),
			RoleInfo:    s().Foreground(lipgloss.Color("6")),
			RoleMuted:   s().Foreground(lipgloss.Color("8")),
			RoleBold:    s().Bold(true),
			RoleAccent:  s().Foreground(lipgloss.Color("5")).Bold(true),
			RoleHeader:  s().Bold(true).Underline(false).Foreground(lipgloss.Color("4")),
			RoleKey:     s().Foreground(lipgloss.Color("4")).Bold(true),
			RoleString:  s().Foreground(lipgloss.Color("2")),
			RoleNumber:  s().Foreground(lipgloss.Color("3")),
			RoleBool:    s().Foreground(lipgloss.Color("5")),
			RoleNull:    s().Foreground(lipgloss.Color("8")).Italic(true),
			RolePunct:   s().Foreground(lipgloss.Color("7")),
		},
	}
}

// Color reports whether the theme emits ANSI escape sequences.
func (t *Theme) Color() bool { return t != nil && t.color }

// Paint styles s according to role. It is a no-op when color is disabled.
func (t *Theme) Paint(role Role, s string) string {
	if !t.Color() || s == "" {
		return s
	}
	st, ok := t.styles[role]
	if !ok {
		return s
	}
	return st.Render(s)
}

// Success paints s green.
func (t *Theme) Success(s string) string { return t.Paint(RoleSuccess, s) }

// Warn paints s yellow.
func (t *Theme) Warn(s string) string { return t.Paint(RoleWarn, s) }

// Error paints s bold red.
func (t *Theme) Error(s string) string { return t.Paint(RoleError, s) }

// Info paints s cyan.
func (t *Theme) Info(s string) string { return t.Paint(RoleInfo, s) }

// Muted paints s dim gray.
func (t *Theme) Muted(s string) string { return t.Paint(RoleMuted, s) }

// Bold renders s in bold.
func (t *Theme) Bold(s string) string { return t.Paint(RoleBold, s) }

// Accent paints s in the bold accent color.
func (t *Theme) Accent(s string) string { return t.Paint(RoleAccent, s) }

// Status labels used across tools for consistent badges.
const (
	SymOK   = "✔"
	SymWarn = "▲"
	SymFail = "✘"
	SymInfo = "•"
)

// Badge renders a status word prefixed by its symbol, colored by role.
func (t *Theme) Badge(role Role, word string) string {
	sym := SymInfo
	switch role {
	case RoleSuccess:
		sym = SymOK
	case RoleWarn:
		sym = SymWarn
	case RoleError:
		sym = SymFail
	}
	return t.Paint(role, sym+" "+word)
}

// IsTerminal reports whether w is a character device such as an interactive
// terminal. Pipes, files and buffers return false.
func IsTerminal(w any) bool {
	f, ok := w.(*os.File)
	if !ok || f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// ColorEnabled decides whether output written to w should be colored.
//
// Precedence: the --no-color flag, then NO_COLOR (https://no-color.org),
// then FORCE_COLOR, then TERM=dumb, and finally whether w is a terminal.
func ColorEnabled(w io.Writer, noColorFlag bool) bool {
	if noColorFlag {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminal(w)
}

// Width returns the printable width of s, ignoring ANSI escape sequences and
// accounting for wide runes.
func Width(s string) int { return lipgloss.Width(s) }

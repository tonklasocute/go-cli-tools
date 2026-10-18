// Package output switches a command's result between human-readable text and
// machine-readable JSON, so every tool behaves the same under --json.
package output

import (
	"encoding/json"
	"io"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// Printer emits a result either as indented JSON or via a text renderer.
type Printer struct {
	W     io.Writer
	JSON  bool
	Theme *ui.Theme
}

// TextFunc renders a result for humans.
type TextFunc func(w io.Writer, th *ui.Theme) error

// Emit writes v as JSON when p.JSON is set, otherwise calls text.
func (p Printer) Emit(v any, text TextFunc) error {
	if p.JSON {
		return JSON(p.W, v)
	}
	th := p.Theme
	if th == nil {
		th = ui.NewTheme(false)
	}
	return text(p.W, th)
}

// JSON writes v to w as two-space indented JSON followed by a newline.
// HTML escaping is disabled so URLs and comparison operators stay readable.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

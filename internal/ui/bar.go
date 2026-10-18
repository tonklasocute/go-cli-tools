package ui

import "strings"

// partial block characters, index = eighths filled.
var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// Bar renders a horizontal bar of the given width filled to frac (0..1),
// using Unicode eighth-blocks for sub-cell precision. The unfilled part is
// padded with spaces so bars line up in columns.
func Bar(frac float64, width int) string {
	if width <= 0 {
		return ""
	}
	if frac < 0 || frac != frac { // negative or NaN
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	total := int(frac*float64(width*8) + 0.5)
	full, rem := total/8, total%8
	var b strings.Builder
	b.WriteString(strings.Repeat("█", full))
	cells := full
	if rem > 0 && full < width {
		b.WriteString(eighths[rem])
		cells++
	}
	b.WriteString(strings.Repeat(" ", width-cells))
	return b.String()
}

// ProgressBar renders "[████░░░░] 42%" style progress for done/total.
func ProgressBar(th *Theme, done, total int64, width int) string {
	frac := 0.0
	if total > 0 {
		frac = float64(done) / float64(total)
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac * float64(width))
	bar := th.Info(strings.Repeat("█", filled)) + th.Muted(strings.Repeat("░", width-filled))
	return bar + " " + Percent(done, total)
}

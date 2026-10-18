package envcheck

import (
	"fmt"
	"io"
	"strings"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// valueWidth caps the VALUE column of --show-values.
const valueWidth = 40

var controlEscaper = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`)

// renderReport writes the human-readable table and summary line.
func renderReport(w io.Writer, th *ui.Theme, rep *Report, showValues bool) error {
	header := []string{"KEY", "STATUS"}
	if showValues {
		header = append(header, "VALUE")
	}
	t := ui.NewTable(append(header, "MESSAGE")...)
	for _, r := range rep.Results {
		row := []string{renderKey(th, r), renderStatus(th, r.Status)}
		if showValues {
			row = append(row, renderValue(th, r.Value))
		}
		t.AddRow(append(row, renderMessage(th, r))...)
	}
	if len(rep.Results) > 0 {
		if err := t.Render(w, th); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, summaryLine(th, rep)+"\n")
	return err
}

func renderKey(th *ui.Theme, r Result) string {
	if r.Key == "" {
		return th.Muted(fmt.Sprintf("line %d", r.Line))
	}
	if r.Status == StatusSkip {
		return th.Muted(r.Key)
	}
	return r.Key
}

func renderStatus(th *ui.Theme, s Status) string {
	switch s {
	case StatusOK:
		return th.Badge(ui.RoleSuccess, "ok")
	case StatusWarn:
		return th.Badge(ui.RoleWarn, "warn")
	case StatusError:
		return th.Badge(ui.RoleError, "error")
	}
	return th.Badge(ui.RoleMuted, "skip")
}

func renderValue(th *ui.Theme, v *string) string {
	switch {
	case v == nil:
		return ""
	case *v == "":
		return th.Muted(`""`)
	case *v == Mask:
		return th.Muted(Mask)
	}
	return ui.Truncate(controlEscaper.Replace(*v), valueWidth)
}

func renderMessage(th *ui.Theme, r Result) string {
	msg := r.Message
	switch r.Status {
	case StatusOK, StatusSkip:
		return th.Muted(msg)
	}
	if r.Line > 0 && !r.lineInMsg {
		msg += " " + th.Muted(fmt.Sprintf("(line %d)", r.Line))
	}
	return msg
}

// summaryLine renders e.g. "✘ 3 errors, 2 warnings, 9 ok — .env vs .env.example".
func summaryLine(th *ui.Theme, rep *Report) string {
	s := rep.Summary
	parts := []string{
		ui.Plural(s.Errors, "error", "errors"),
		ui.Plural(s.Warnings, "warning", "warnings"),
		fmt.Sprintf("%d ok", s.OK),
	}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", s.Skipped))
	}
	role := ui.RoleSuccess
	switch {
	case s.Errors > 0 || !rep.OK:
		role = ui.RoleError
	case s.Warnings > 0:
		role = ui.RoleWarn
	}
	line := th.Badge(role, strings.Join(parts, ", ")) + th.Muted(" — "+rep.EnvFile+" vs "+rep.SchemaFile)
	if rep.Strict && s.Warnings > 0 && s.Errors == 0 {
		line += th.Muted(" (--strict: warnings fail)")
	}
	return line
}

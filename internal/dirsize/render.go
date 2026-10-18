package dirsize

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// barWidth is the width of the share bar column.
const barWidth = 24

// RenderText writes the human-readable report.
func RenderText(w io.Writer, th *ui.Theme, r *Report) error {
	var b strings.Builder
	b.WriteString(header(th, r) + "\n\n")

	if len(r.Entries) == 0 && r.hidden == 0 {
		b.WriteString("  " + th.Muted("(nothing to show)") + "\n")
	} else {
		tbl := r.table(th)
		var buf bytes.Buffer
		if err := tbl.Render(&buf, th); err != nil {
			return err
		}
		for _, line := range strings.SplitAfter(buf.String(), "\n") {
			if line != "" {
				b.WriteString("  " + line)
			}
		}
	}
	b.WriteString("\n  " + r.totalLine(th) + "\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func displayRoot(root string, isDir bool) string {
	s := strings.ReplaceAll(root, "\\", "/")
	if isDir && !strings.HasSuffix(s, "/") {
		s += "/"
	}
	return s
}

func header(th *ui.Theme, r *Report) string {
	o := r.opts
	parts := []string{}
	if o.Files {
		parts = append(parts, "largest files")
	} else {
		parts = append(parts, "depth "+strconv.Itoa(o.Depth))
	}
	if o.Top > 0 {
		parts = append(parts, "top "+strconv.Itoa(o.Top))
	}
	if o.MinSize > 0 {
		label := o.MinSizeLabel
		if label == "" {
			label = ui.HumanBytes(o.MinSize)
		}
		parts = append(parts, "≥ "+label)
	}
	if ex := nonEmpty(o.Exclude); len(ex) > 0 {
		parts = append(parts, "excluding "+strings.Join(ex, ", "))
	}
	return th.Bold("dirsize") + " " + th.Accent(displayRoot(r.Root, r.isDir)) +
		th.Muted(" — "+strings.Join(parts, " · "))
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return slices.Compact(out)
}

func (r *Report) table(th *ui.Theme) *ui.Table {
	files := r.opts.Files
	var tbl *ui.Table
	if files {
		tbl = ui.NewTable("SIZE", "%", "", "PATH")
	} else {
		tbl = ui.NewTable("SIZE", "%", "", "FILES", "NAME")
		tbl.RightAlign[3] = true
	}
	tbl.RightAlign[0] = true
	tbl.RightAlign[1] = true
	for _, e := range r.Entries {
		frac := 0.0
		if r.TotalBytes > 0 {
			frac = float64(e.Bytes) / float64(r.TotalBytes)
		}
		cells := []string{
			ui.HumanBytes(e.Bytes),
			ui.Percent(e.Bytes, r.TotalBytes),
			bar(th, frac),
		}
		if !files {
			count := ""
			if e.IsDir {
				count = commas(e.Files)
			}
			cells = append(cells, count)
		}
		cells = append(cells, entryName(th, e))
		tbl.AddRow(cells...)
	}
	if r.hidden > 0 {
		cells := []string{
			th.Muted(ui.HumanBytes(r.hiddenBytes)),
			th.Muted(ui.Percent(r.hiddenBytes, r.TotalBytes)),
			"",
		}
		if !files {
			cells = append(cells, "")
		}
		cells = append(cells, th.Muted("… and "+commas(r.hidden)+" more"))
		tbl.AddRow(cells...)
	}
	return tbl
}

// bar renders the share bar colored by how large the share is, with a
// muted track for the remainder.
func bar(th *ui.Theme, frac float64) string {
	full := strings.TrimRight(ui.Bar(frac, barWidth), " ")
	track := strings.Repeat("░", barWidth-ui.Width(full))
	role := ui.RoleInfo
	switch {
	case frac >= 0.40:
		role = ui.RoleError
	case frac >= 0.15:
		role = ui.RoleWarn
	case frac >= 0.05:
		role = ui.RoleSuccess
	}
	return th.Paint(role, full) + th.Muted(track)
}

// entryName shows the parent path muted and the base name highlighted;
// directories get a trailing slash in the accent color.
func entryName(th *ui.Theme, e Entry) string {
	parent := ""
	if i := strings.LastIndexByte(e.Path, '/'); i >= 0 {
		parent = e.Path[:i+1]
	}
	name := e.Name
	if e.IsDir {
		name = th.Accent(name + "/")
	}
	return th.Muted(parent) + name
}

func (r *Report) totalLine(th *ui.Theme) string {
	sep := th.Muted(" · ")
	s := th.Bold("Total") + " " + th.Bold(ui.HumanBytes(r.TotalBytes)) +
		" in " + count(r.Files, "file", "files") + ", " + count(r.Dirs, "dir", "dirs") +
		sep + th.Muted("scanned in ") + ui.HumanDuration(r.elapsed)
	if r.Skipped > 0 {
		s += sep + th.Warn(count(int64(r.Skipped), "entry", "entries")+" skipped")
	}
	return s
}

// WriteSkipped writes a short diagnostic about skipped entries, e.g.
// "▲ skipped 3 entries (permission denied)", followed by up to limit paths.
func WriteSkipped(w io.Writer, th *ui.Theme, r *Report, limit int) error {
	if r.Skipped == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(th.Badge(ui.RoleWarn, "skipped "+count(int64(r.Skipped), "entry", "entries")))
	reasons, distinct := summarizeReasons(r.Errors)
	if reasons != "" {
		b.WriteString(" " + th.Muted("("+reasons+")"))
	}
	b.WriteString("\n")
	for i, e := range r.Errors {
		if i >= limit {
			break
		}
		if distinct > 1 {
			fmt.Fprintf(&b, "  %s%s\n", e.Path, th.Muted(": "+e.Error))
		} else {
			fmt.Fprintf(&b, "  %s\n", e.Path)
		}
	}
	if rest := r.Skipped - min(limit, len(r.Errors)); rest > 0 {
		b.WriteString("  " + th.Muted("… and "+commas(int64(rest))+" more (use --json for the full list)") + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// summarizeReasons groups errors by message: "2 permission denied, 1 ...".
// A single distinct reason is shown without a count. It also returns the
// number of distinct reasons.
func summarizeReasons(errs []EntryError) (summary string, distinct int) {
	counts := map[string]int{}
	var order []string
	for _, e := range errs {
		if counts[e.Error] == 0 {
			order = append(order, e.Error)
		}
		counts[e.Error]++
	}
	if len(order) <= 1 {
		return strings.Join(order, ""), len(order)
	}
	slices.SortStableFunc(order, func(a, b string) int { return counts[b] - counts[a] })
	parts := make([]string, len(order))
	for i, k := range order {
		parts[i] = strconv.Itoa(counts[k]) + " " + k
	}
	return strings.Join(parts, ", "), len(order)
}

func count(n int64, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return commas(n) + " " + plural
}

// commas formats n with thousands separators: 12345 → "12,345".
func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

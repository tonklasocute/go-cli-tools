package reqping

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// JSONReport is the --json document for one round of checks.
type JSONReport struct {
	Healthy   bool         `json:"healthy"`
	CheckedAt string       `json:"checked_at"`
	ElapsedMS float64      `json:"elapsed_ms"`
	Targets   []JSONTarget `json:"targets"`
}

// JSONTarget is the --json representation of one target's result. Fields
// that do not apply are null rather than omitted, so the shape is stable.
type JSONTarget struct {
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	Method        string    `json:"method"`
	Healthy       bool      `json:"healthy"`
	StatusCode    *int      `json:"status_code"`
	Expected      string    `json:"expected"`
	LatencyMS     *float64  `json:"latency_ms"`
	Error         *string   `json:"error"`
	Warning       *string   `json:"warning"`
	TLSExpiryDays *int      `json:"tls_expiry_days"`
	TLSExpiresAt  *string   `json:"tls_expires_at"`
	RedirectedTo  *string   `json:"redirected_to"`
	Stats         JSONStats `json:"stats"`
}

// JSONStats holds the per-target request statistics.
type JSONStats struct {
	Count int      `json:"count"`
	OK    int      `json:"ok"`
	MinMS *float64 `json:"min_ms"`
	AvgMS *float64 `json:"avg_ms"`
	P95MS *float64 `json:"p95_ms"`
	MaxMS *float64 `json:"max_ms"`
}

// ToJSON converts a report into its JSON representation.
func (r *Report) ToJSON() JSONReport {
	out := JSONReport{
		Healthy:   r.Healthy(),
		CheckedAt: r.CheckedAt.Format(time.RFC3339),
		ElapsedMS: ms(r.Elapsed),
		Targets:   make([]JSONTarget, len(r.Results)),
	}
	for i := range r.Results {
		res := &r.Results[i]
		jt := JSONTarget{
			Name:     res.Target.Name,
			URL:      res.Target.URL,
			Method:   res.Method,
			Healthy:  res.Healthy,
			Expected: res.Target.Expect.String(),
			Error:    strPtr(res.Problem),
			Warning:  strPtr(res.Warning),
			Stats:    JSONStats{Count: res.Count, OK: res.OK},
		}
		if res.StatusCode != 0 {
			jt.StatusCode = ptr(res.StatusCode)
		}
		if res.Latency.N > 0 {
			jt.LatencyMS = ptr(ms(res.Latency.Avg))
			jt.Stats.MinMS = ptr(ms(res.Latency.Min))
			jt.Stats.AvgMS = ptr(ms(res.Latency.Avg))
			jt.Stats.P95MS = ptr(ms(res.Latency.P95))
			jt.Stats.MaxMS = ptr(ms(res.Latency.Max))
		}
		if res.HasTLS() {
			jt.TLSExpiryDays = ptr(res.TLSDays)
			jt.TLSExpiresAt = ptr(res.TLSExpires.UTC().Format(time.RFC3339))
		}
		jt.RedirectedTo = strPtr(res.RedirectedTo)
		out.Targets[i] = jt
	}
	return out
}

func ptr[T any](v T) *T { return &v }

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ms converts d to milliseconds rounded to two decimals.
func ms(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Millisecond)*100) / 100
}

// RenderText writes the results table followed by the summary line.
// withStats selects the MIN / AVG / P95 / MAX columns used by --count.
func RenderText(w io.Writer, th *ui.Theme, r *Report, withStats bool) error {
	var tb *ui.Table
	if withStats {
		tb = ui.NewTable("NAME", "STATUS", "OK", "MIN", "AVG", "P95", "MAX", "TLS", "NOTE")
		for _, i := range []int{2, 3, 4, 5, 6} {
			tb.RightAlign[i] = true
		}
	} else {
		tb = ui.NewTable("NAME", "STATUS", "LATENCY", "TLS", "NOTE")
		tb.RightAlign[2] = true
	}
	tb.MaxWidth[0] = 32
	for i := range r.Results {
		res := &r.Results[i]
		row := []string{res.Target.Name, statusCell(th, res)}
		if withStats {
			okCell := fmt.Sprintf("%d/%d", res.OK, res.Count)
			if res.OK < res.Count {
				okCell = th.Error(okCell)
			}
			row = append(row, okCell,
				latencyCell(res, res.Latency.Min), latencyCell(res, res.Latency.Avg),
				latencyCell(res, res.Latency.P95), latencyCell(res, res.Latency.Max))
		} else {
			row = append(row, latencyCell(res, res.Latency.Avg))
		}
		row = append(row, tlsCell(th, res), noteCell(th, res))
		tb.AddRow(row...)
	}
	if err := tb.Render(w, th); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, Summary(th, r))
	return err
}

// Summary renders "✔ 4 healthy, ✘ 1 unhealthy · 1.23s" (plus a warning count
// when certificates are about to expire).
func Summary(th *ui.Theme, r *Report) string {
	healthy, unhealthy, warned := r.Counts()
	parts := []string{th.Success(fmt.Sprintf("%s %d healthy", ui.SymOK, healthy))}
	if warned > 0 {
		parts = append(parts, th.Warn(fmt.Sprintf("%s %s", ui.SymWarn, ui.Plural(warned, "warning", "warnings"))))
	}
	bad := fmt.Sprintf("%s %d unhealthy", ui.SymFail, unhealthy)
	if unhealthy > 0 {
		bad = th.Error(bad)
	} else {
		bad = th.Muted(bad)
	}
	parts = append(parts, bad)
	return strings.Join(parts, ", ") + th.Muted(" · "+ui.HumanDuration(r.Elapsed))
}

func statusCell(th *ui.Theme, res *Result) string {
	code := "ERR"
	if res.StatusCode != 0 {
		code = strconv.Itoa(res.StatusCode)
	}
	if res.Healthy {
		return th.Badge(ui.RoleSuccess, code)
	}
	return th.Badge(ui.RoleError, code)
}

func latencyCell(res *Result, d time.Duration) string {
	switch {
	case res.Latency.N == 0:
		return "—"
	case d == 0:
		// Coarse monotonic clocks (notably Windows) can measure 0 for
		// loopback requests; "0ns" would look like a bug.
		return "<1ms"
	}
	return ui.HumanDuration(d)
}

func tlsCell(th *ui.Theme, res *Result) string {
	switch {
	case !res.HasTLS():
		return th.Muted("—")
	case res.TLSExpired:
		return th.Badge(ui.RoleError, "expired")
	case res.Warning != "":
		return th.Badge(ui.RoleWarn, strconv.Itoa(res.TLSDays)+"d")
	default:
		return strconv.Itoa(res.TLSDays) + "d"
	}
}

// maxNote caps each part of the NOTE column, in columns.
const maxNote = 60

func noteCell(th *ui.Theme, res *Result) string {
	// Truncate before painting: ui.Truncate is not ANSI-aware.
	var notes []string
	if res.Problem != "" {
		notes = append(notes, th.Error(ui.Truncate(res.Problem, maxNote)))
	}
	if res.Warning != "" {
		notes = append(notes, th.Warn(res.Warning))
	}
	if res.RedirectedTo != "" {
		notes = append(notes, th.Muted(ui.Truncate("redirected to "+res.RedirectedTo, maxNote)))
	}
	return strings.Join(notes, th.Muted(" · "))
}

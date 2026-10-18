package dirsize

import (
	"cmp"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ReportOptions controls how a Result is turned into a Report.
type ReportOptions struct {
	// Root is the path as the user typed it (used for display).
	Root string
	// Depth is the aggregation depth that was scanned.
	Depth int
	// Top keeps only the N largest entries; 0 keeps all.
	Top int
	// MinSize hides entries smaller than this many bytes.
	MinSize int64
	// MinSizeLabel is how the user spelled MinSize (e.g. "10MB"), shown in
	// the text header; empty means HumanBytes(MinSize).
	MinSizeLabel string
	// Exclude is echoed in the text header.
	Exclude []string
	// Files reports that the Result lists individual files.
	Files bool
}

// Report is the final, sorted and filtered view of a scan. It is also the
// --json document.
type Report struct {
	Root       string       `json:"root"`
	Depth      int          `json:"depth"`
	TotalBytes int64        `json:"total_bytes"`
	Files      int64        `json:"files"`
	Dirs       int64        `json:"dirs"`
	ElapsedMS  int64        `json:"elapsed_ms"`
	Skipped    int          `json:"skipped"`
	Errors     []EntryError `json:"errors"`
	Entries    []Entry      `json:"entries"`

	opts        ReportOptions
	isDir       bool
	elapsed     time.Duration
	hidden      int64 // entries (or files) not shown
	hiddenBytes int64
}

// compareEntries orders by size descending, then path ascending.
func compareEntries(a, b Entry) int {
	if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
		return c
	}
	return strings.Compare(a.Path, b.Path)
}

// BuildReport sorts res.Entries by size (then path), applies the min-size
// and top-N filters, computes percentages and the "and N more" remainder.
func BuildReport(res *Result, o ReportOptions) *Report {
	r := &Report{
		Root:       filepath.Clean(o.Root),
		Depth:      o.Depth,
		TotalBytes: res.TotalBytes,
		Files:      res.Files,
		Dirs:       res.Dirs,
		ElapsedMS:  res.Elapsed.Milliseconds(),
		Skipped:    res.Skipped,
		Errors:     res.Errors,
		Entries:    []Entry{},
		opts:       o,
		isDir:      res.IsDir,
		elapsed:    res.Elapsed,
	}
	if r.Errors == nil {
		r.Errors = []EntryError{}
	}
	all := slices.Clone(res.Entries)
	slices.SortFunc(all, compareEntries)
	var shownBytes int64
	for _, e := range all {
		if e.Bytes < o.MinSize {
			continue
		}
		if o.Top > 0 && len(r.Entries) >= o.Top {
			break
		}
		e.Percent = percent(e.Bytes, res.TotalBytes)
		r.Entries = append(r.Entries, e)
		shownBytes += e.Bytes
	}
	population := int64(len(all))
	if o.Files {
		population = res.Files
	}
	r.hidden = max(population-int64(len(r.Entries)), 0)
	if r.hidden > 0 {
		r.hiddenBytes = max(res.TotalBytes-shownBytes, 0)
	}
	return r
}

// percent returns part/total*100 rounded to two decimals.
func percent(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round(float64(part)*10000/float64(total)) / 100
}

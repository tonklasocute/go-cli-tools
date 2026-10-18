package ui

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{10 * 1024 * 1024, "10.0 MiB"},
		{150 * 1024 * 1024, "150 MiB"},
		{5 << 40, "5.0 TiB"},
		{-2048, "-2.0 KiB"},
		{math.MaxInt64, "8.0 EiB"},
	}
	for _, tt := range tests {
		if got := HumanBytes(tt.in); got != tt.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"512", 512, false},
		{"10MB", 10_000_000, false},
		{"10mb", 10_000_000, false},
		{"10M", 10 << 20, false},
		{"1.5GiB", 3 << 29, false},
		{" 2 KiB ", 2048, false},
		{"200k", 200 << 10, false},
		{"1TB", 1e12, false},
		{"0B", 0, false},
		{"", 0, true},
		{"abc", 0, true},
		{"10XB", 0, true},
		{"-5MB", 0, true},
		{"1.2.3", 0, true},
		{"99999999999EB", 0, true},
		{"99999999999999999TB", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseSize(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseSize(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if err != nil && !errors.Is(err, ErrInvalidSize) {
			t.Errorf("ParseSize(%q) err should wrap ErrInvalidSize: %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{500 * time.Nanosecond, "500ns"},
		{850 * time.Microsecond, "850µs"},
		{2500 * time.Microsecond, "2.5ms"},
		{120 * time.Millisecond, "120ms"},
		{1240 * time.Millisecond, "1.24s"},
		{125 * time.Second, "2m05s"},
		{3*time.Hour + 4*time.Minute, "3h04m"},
		{-120 * time.Millisecond, "-120ms"},
	}
	for _, tt := range tests {
		if got := HumanDuration(tt.in); got != tt.want {
			t.Errorf("HumanDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPercentPlural(t *testing.T) {
	if got := Percent(1, 8); got != "12.5%" {
		t.Errorf("Percent = %q", got)
	}
	if got := Percent(1, 0); got != "0.0%" {
		t.Errorf("Percent zero total = %q", got)
	}
	if got := Plural(1, "file", "files"); got != "1 file" {
		t.Errorf("Plural = %q", got)
	}
	if got := Plural(3, "file", "files"); got != "3 files" {
		t.Errorf("Plural = %q", got)
	}
}

func TestBar(t *testing.T) {
	tests := []struct {
		frac  float64
		width int
		want  string
	}{
		{0, 4, "    "},
		{1, 4, "████"},
		{0.5, 4, "██  "},
		{1.0 / 16, 2, "▏ "},
		{0.25, 2, "▌ "},
		{2, 3, "███"},
		{-1, 3, "   "},
		{math.NaN(), 2, "  "},
		{0.5, 0, ""},
	}
	for _, tt := range tests {
		got := Bar(tt.frac, tt.width)
		if got != tt.want {
			t.Errorf("Bar(%v,%d) = %q, want %q", tt.frac, tt.width, got, tt.want)
		}
		if tt.width > 0 && Width(got) != tt.width {
			t.Errorf("Bar(%v,%d) width = %d", tt.frac, tt.width, Width(got))
		}
	}
}

func TestProgressBar(t *testing.T) {
	th := NewTheme(false)
	tests := []struct {
		done, total int64
		want        string
	}{
		{5, 10, "█████░░░░░ 50.0%"},
		{0, 0, "░░░░░░░░░░ 0.0%"},
		{20, 10, "██████████ 200.0%"},
	}
	for _, tt := range tests {
		if got := ProgressBar(th, tt.done, tt.total, 10); got != tt.want {
			t.Errorf("ProgressBar(%d,%d) = %q, want %q", tt.done, tt.total, got, tt.want)
		}
	}
}

func TestThemePaint(t *testing.T) {
	plain := NewTheme(false)
	color := NewTheme(true)
	if plain.Color() || !color.Color() {
		t.Fatal("Color() mismatch")
	}
	var nilTheme *Theme
	if nilTheme.Color() || nilTheme.Paint(RoleError, "x") != "x" {
		t.Fatal("nil theme must be a no-op")
	}
	fns := map[string]func(string) string{
		"success": color.Success, "warn": color.Warn, "error": color.Error,
		"info": color.Info, "muted": color.Muted, "bold": color.Bold, "accent": color.Accent,
	}
	for name, fn := range fns {
		got := fn("hi")
		if !strings.Contains(got, "\x1b[") || !strings.Contains(got, "hi") {
			t.Errorf("%s: expected ANSI output, got %q", name, got)
		}
		if Width(got) != 2 {
			t.Errorf("%s: Width = %d, want 2", name, Width(got))
		}
	}
	if got := plain.Error("hi"); got != "hi" {
		t.Errorf("plain theme added escapes: %q", got)
	}
	if got := color.Paint(RoleError, ""); got != "" {
		t.Errorf("empty string should stay empty, got %q", got)
	}
	if got := color.Paint(Role(999), "x"); got != "x" {
		t.Errorf("unknown role should be unstyled, got %q", got)
	}
	badges := map[Role]string{RoleSuccess: SymOK, RoleWarn: SymWarn, RoleError: SymFail, RoleInfo: SymInfo}
	for role, sym := range badges {
		if got := plain.Badge(role, "word"); got != sym+" word" {
			t.Errorf("Badge(%v) = %q", role, got)
		}
	}
}

func TestColorEnabled(t *testing.T) {
	var buf bytes.Buffer
	tests := []struct {
		name               string
		noColor, forceColr string
		term               string
		flag               bool
		want               bool
	}{
		{name: "flag wins", forceColr: "1", flag: true, want: false},
		{name: "NO_COLOR", noColor: "1", forceColr: "1", want: false},
		{name: "FORCE_COLOR", forceColr: "1", want: true},
		{name: "dumb term", term: "dumb", want: false},
		{name: "buffer is not a tty", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("FORCE_COLOR", tt.forceColr)
			t.Setenv("TERM", tt.term)
			if got := ColorEnabled(&buf, tt.flag); got != tt.want {
				t.Fatalf("ColorEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("regular file reported as terminal")
	}
	if IsTerminal(&bytes.Buffer{}) {
		t.Error("buffer reported as terminal")
	}
	var nilFile *os.File
	if IsTerminal(nilFile) {
		t.Error("nil file reported as terminal")
	}
	closed, _ := os.Create(filepath.Join(t.TempDir(), "g"))
	closed.Close()
	if IsTerminal(closed) {
		t.Error("closed file reported as terminal")
	}
}

func TestTable(t *testing.T) {
	th := NewTheme(false)
	tbl := NewTable("KEY", "SIZE", "NOTE")
	tbl.RightAlign[1] = true
	tbl.MaxWidth[2] = 6
	tbl.AddRow("a", "1", "short")
	tbl.AddRow("longer", "100", "this is long")
	tbl.AddRow("x") // missing cells
	var buf bytes.Buffer
	if err := tbl.Render(&buf, th); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"KEY     SIZE  NOTE\n" +
		"──────  ────  ──────\n" +
		"a          1  short\n" +
		"longer   100  this …\n" +
		"x\n"
	if got := buf.String(); got != want {
		t.Fatalf("table mismatch\n got:\n%s\nwant:\n%s", got, want)
	}

	// colored cells must not break alignment
	ct := NewTable("A", "B")
	ct.AddRow(NewTheme(true).Error("red"), "x")
	ct.AddRow("plain", "y")
	buf.Reset()
	_ = ct.Render(&buf, NewTheme(true))
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if Width(lines[2]) != Width(lines[3]) {
		t.Errorf("colored row misaligned: %q vs %q", lines[2], lines[3])
	}

	buf.Reset()
	if err := (&Table{}).Render(&buf, th); err != nil || buf.Len() != 0 {
		t.Errorf("empty table should render nothing")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "hel…"},
		{"hello", 1, "…"},
		{"hello", 0, "hello"},
		{"สวัสดีครับ", 4, ""}, // combining marks: only the width is asserted
	}
	for _, tt := range tests {
		got := Truncate(tt.in, tt.n)
		if tt.n > 0 && Width(got) > tt.n {
			t.Errorf("Truncate(%q,%d) = %q exceeds width", tt.in, tt.n, got)
		}
		if tt.want != "" && got != tt.want {
			t.Errorf("Truncate(%q,%d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestLive(t *testing.T) {
	th := NewTheme(false)
	var buf bytes.Buffer

	sp := NewSpinner(&buf, th, "working")
	if sp.Enabled() {
		t.Fatal("spinner must be disabled for non-terminal writers")
	}
	sp.Start()
	sp.Stop()
	if buf.Len() != 0 {
		t.Fatalf("disabled spinner wrote %q", buf.String())
	}
	if line := sp.Line(); !strings.HasSuffix(line, " working") {
		t.Errorf("spinner line = %q", line)
	}
	sp.SetLabel("other")
	if line := sp.Line(); !strings.HasSuffix(line, " other") {
		t.Errorf("spinner relabel = %q", line)
	}

	pr := NewProgress(&buf, th, "scan", 4)
	pr.Add(2)
	if line := pr.Line(); !strings.Contains(line, "scan") || !strings.Contains(line, "50.0%") || !strings.Contains(line, "2/4") {
		t.Errorf("progress line = %q", line)
	}

	// Force-enable to exercise the drawing goroutine.
	pr.enabled = true
	pr.interval = time.Millisecond
	pr.Start()
	pr.Start() // idempotent
	time.Sleep(5 * time.Millisecond)
	pr.Stop()
	pr.Stop() // idempotent
	if !strings.Contains(buf.String(), "\r\x1b[2K") {
		t.Errorf("expected line clears in output, got %q", buf.String())
	}
}

package dirsize

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

func runCLI(t *testing.T, ctx context.Context, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := NewCommand(cliutil.IO{In: strings.NewReader(""), Out: &out, Err: &errb})
	code = cliutil.Run(ctx, cmd, args)
	return out.String(), errb.String(), code
}

func TestCLIText(t *testing.T) {
	root := newSample(t)
	out, errOut, code := runCLI(t, context.Background(), root, "--no-color", "--top", "3")
	if code != cliutil.ExitOK {
		t.Fatalf("code = %d, stderr %s", code, errOut)
	}
	for _, want := range []string{
		"dirsize " + filepath.ToSlash(root) + "/ — depth 1 · top 3",
		"SIZE", "FILES", "NAME",
		"4.9 KiB", "50.8%", "node_modules/",
		"3.4 KiB", "a/",
		"… and 3 more",
		"Total 9.6 KiB in 9 files, 9 dirs · scanned in",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("ANSI codes with --no-color")
	}
	if errOut != "" {
		t.Errorf("unexpected stderr: %q", errOut)
	}
}

func TestCLIFilesAndFilters(t *testing.T) {
	root := newSample(t)
	out, _, code := runCLI(t, context.Background(), root, "--no-color", "--files", "--min-size", "600",
		"-x", "node_modules", "--exclude", "*.log")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{"largest files", "≥ 600 ·", "excluding node_modules, *.log", "PATH", "a/sub/z.bin", "a/x.bin", "… and 4 more"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "index.js") || strings.Contains(out, "FILES") {
		t.Errorf("excluded file or FILES column shown:\n%s", out)
	}
}

func TestCLIEmptyDir(t *testing.T) {
	out, _, code := runCLI(t, context.Background(), t.TempDir(), "--no-color")
	if code != 0 || !strings.Contains(out, "(nothing to show)") || !strings.Contains(out, "Total 0 B in 0 files, 0 dirs") {
		t.Errorf("code=%d out=\n%s", code, out)
	}
}

func TestCLIJSON(t *testing.T) {
	root := newSample(t)
	out, _, code := runCLI(t, context.Background(), root, "--json", "--depth", "2", "--top", "2")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("ANSI in JSON")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	for _, k := range []string{"root", "depth", "total_bytes", "files", "dirs", "elapsed_ms", "skipped", "errors", "entries"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	var rep struct {
		Root       string       `json:"root"`
		Depth      int          `json:"depth"`
		TotalBytes int64        `json:"total_bytes"`
		Files      int64        `json:"files"`
		Dirs       int64        `json:"dirs"`
		Skipped    int          `json:"skipped"`
		Errors     []EntryError `json:"errors"`
		Entries    []Entry      `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Root != filepath.Clean(root) || rep.Depth != 2 || rep.TotalBytes != 9850 || rep.Files != 9 || rep.Dirs != 9 {
		t.Errorf("rep = %+v", rep)
	}
	if rep.Errors == nil || rep.Skipped != 0 {
		t.Errorf("errors should be an empty array: %+v", rep.Errors)
	}
	want := []Entry{
		{Path: "node_modules/pkg", Name: "pkg", IsDir: true, Bytes: 5000, Files: 1, Percent: 50.76},
		{Path: "a/sub", Name: "sub", IsDir: true, Bytes: 2000, Files: 1, Percent: 20.3},
	}
	if len(rep.Entries) != 2 || rep.Entries[0] != want[0] || rep.Entries[1] != want[1] {
		t.Errorf("entries = %+v", rep.Entries)
	}
}

func TestCLIUsageErrors(t *testing.T) {
	root := newSample(t)
	tests := [][]string{
		{"--depth", "-1"},
		{"--top", "-2"},
		{"--workers", "0"},
		{"--min-size", "lots"},
		{"--exclude", "[bad"},
		{"--bogus"},
		{root, root},
	}
	for _, args := range tests {
		_, errOut, code := runCLI(t, context.Background(), args...)
		if code != cliutil.ExitUsage || !strings.Contains(errOut, "error:") {
			t.Errorf("%v: code=%d stderr=%q", args, code, errOut)
		}
	}
}

func TestCLIMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	_, errOut, code := runCLI(t, context.Background(), missing)
	if code != cliutil.ExitUsage || !strings.Contains(errOut, "cannot scan") || !strings.Contains(errOut, "no such file or directory") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestCLIInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, errOut, code := runCLI(t, ctx, newSample(t))
	if code != cliutil.ExitInterrupted || !strings.Contains(errOut, "interrupted") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestCLIHelpAndVersion(t *testing.T) {
	out, _, code := runCLI(t, context.Background(), "--help")
	if code != 0 || !strings.Contains(out, "Examples:") || !strings.Contains(out, "--exclude node_modules") ||
		!strings.Contains(out, "Exit codes:") || !strings.Contains(out, "--workers") {
		t.Errorf("help (code %d):\n%s", code, out)
	}
	out, _, code = runCLI(t, context.Background(), "--version")
	if code != 0 || !strings.Contains(out, "dirsize") {
		t.Errorf("version (code %d): %q", code, out)
	}
}

func TestRenderSkippedAndColor(t *testing.T) {
	res := &Result{
		IsDir: true, TotalBytes: 1000, Files: 3, Dirs: 1, Skipped: 8,
		Entries: []Entry{
			{Path: "big", Name: "big", IsDir: true, Bytes: 600, Files: 2},
			{Path: "mid.bin", Name: "mid.bin", Bytes: 200, Files: 1},
			{Path: "x/small.bin", Name: "small.bin", Bytes: 100, Files: 1},
			{Path: "tiny", Name: "tiny", Bytes: 30, Files: 1},
			{Path: "dust", Name: "dust", Bytes: 1, Files: 1},
		},
		Errors: []EntryError{
			{Path: "p1", Error: "permission denied"}, {Path: "p2", Error: "permission denied"},
			{Path: "p3", Error: "permission denied"}, {Path: "p4", Error: "permission denied"},
			{Path: "p5", Error: "permission denied"}, {Path: "p6", Error: "no such file or directory"},
		},
	}
	rep := BuildReport(res, ReportOptions{Root: `C:\data`, Depth: 1})

	var out bytes.Buffer
	if err := RenderText(&out, ui.NewTheme(true), rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[") || !strings.Contains(out.String(), "8 entries skipped") {
		t.Errorf("colored output:\n%s", out.String())
	}
	if !strings.Contains(ui.NewTheme(false).Muted("x/")+"small.bin", "x/small.bin") {
		t.Error("entry name")
	}

	var errb bytes.Buffer
	if err := WriteSkipped(&errb, ui.NewTheme(false), rep, 3); err != nil {
		t.Fatal(err)
	}
	got := errb.String()
	for _, want := range []string{
		"▲ skipped 8 entries (5 permission denied, 1 no such file or directory)",
		"  p1: permission denied\n", "  p3: permission denied\n",
		"… and 5 more (use --json for the full list)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("skipped output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "p4") {
		t.Errorf("limit not honored:\n%s", got)
	}

	errb.Reset()
	single := BuildReport(&Result{Skipped: 1, Errors: []EntryError{{Path: "d", Error: "permission denied"}}}, ReportOptions{Root: "."})
	if err := WriteSkipped(&errb, ui.NewTheme(false), single, 5); err != nil {
		t.Fatal(err)
	}
	if errb.String() != "▲ skipped 1 entry (permission denied)\n  d\n" {
		t.Errorf("single: %q", errb.String())
	}
	errb.Reset()
	if err := WriteSkipped(&errb, ui.NewTheme(false), BuildReport(&Result{}, ReportOptions{}), 5); err != nil || errb.Len() != 0 {
		t.Errorf("no skips should print nothing, got %q", errb.String())
	}
}

func TestDisplayRootAndCommas(t *testing.T) {
	if got := displayRoot(`C:\x\`, true); got != "C:/x/" {
		t.Errorf("displayRoot = %q", got)
	}
	if got := displayRoot("f.txt", false); got != "f.txt" {
		t.Errorf("displayRoot file = %q", got)
	}
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -12345: "-12,345"} {
		if got := commas(in); got != want {
			t.Errorf("commas(%d) = %q, want %q", in, got, want)
		}
	}
	if got := count(1, "file", "files"); got != "1 file" {
		t.Errorf("count = %q", got)
	}
}

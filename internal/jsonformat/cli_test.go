package jsonformat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
)

type cliResult struct {
	code           int
	stdout, stderr string
}

func runCLI(t *testing.T, stdin io.Reader, args ...string) cliResult {
	t.Helper()
	return runCLICtx(t, context.Background(), stdin, args...)
}

func runCLICtx(t *testing.T, ctx context.Context, stdin io.Reader, args ...string) cliResult {
	t.Helper()
	var out, errb bytes.Buffer
	code := cliutil.Run(ctx, NewCommand(cliutil.IO{In: stdin, Out: &out, Err: &errb}), args)
	return cliResult{code: code, stdout: out.String(), stderr: errb.String()}
}

// plainEnv makes color decisions deterministic.
func plainEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("TERM", "xterm")
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const brokenDoc = "{\n  \"service\": \"api\",\n  \"port\": 8080\n  \"debug\": true\n}\n"

func TestCLIFormat(t *testing.T) {
	plainEnv(t)
	dir := t.TempDir()
	good := writeFile(t, dir, "good.json", `{"b":[1,2],"a":{"x":null}}`)
	bad := writeFile(t, dir, "bad.json", brokenDoc)

	tests := []struct {
		name      string
		stdin     string
		args      []string
		wantCode  int
		wantOut   string
		outHas    []string
		errHas    []string
		wantNoErr bool
		wantNoOut bool
	}{
		{name: "pretty file", args: []string{good}, wantOut: "{\n  \"b\": [\n    1,\n    2\n  ],\n  \"a\": {\n    \"x\": null\n  }\n}\n", wantNoErr: true},
		{name: "minify", args: []string{"-m", good}, wantOut: `{"b":[1,2],"a":{"x":null}}` + "\n"},
		{name: "indent 0 is minify", args: []string{"--indent", "0", good}, wantOut: `{"b":[1,2],"a":{"x":null}}` + "\n"},
		{name: "indent 4 sort keys", args: []string{"--indent", "4", "-S", good}, wantOut: "{\n    \"a\": {\n        \"x\": null\n    },\n    \"b\": [\n        1,\n        2\n    ]\n}\n"},
		{name: "tab", args: []string{"--tab", "-q", "a", good}, wantOut: "{\n\t\"x\": null\n}\n"},
		{name: "stdin implicit", stdin: `[1, 2]`, args: []string{"-m"}, wantOut: "[1,2]\n"},
		{name: "stdin dash", stdin: `{"k":"v"}`, args: []string{"-q", "k", "-r", "-"}, wantOut: "v\n"},
		{name: "json flag only disables color", args: []string{"--json", "-m", good}, wantOut: `{"b":[1,2],"a":{"x":null}}` + "\n"},
		{name: "multiple files get headers", args: []string{"-m", good, good}, wantOut: "==> " + good + " <==\n" + `{"b":[1,2],"a":{"x":null}}` + "\n\n==> " + good + " <==\n" + `{"b":[1,2],"a":{"x":null}}` + "\n"},
		{name: "invalid file", args: []string{bad}, wantCode: 1, wantNoOut: true,
			errHas: []string{bad + ":4:3: error: expected ',' or '}' after object value, found '\"'", "    3 |   \"port\": 8080\n", "      |   ^\n", "hint: is a ',' missing at the end of line 3?"}},
		{name: "invalid stdin", stdin: `{"a": tru}`, wantCode: 1, errHas: []string{"<stdin>:1:7: error: invalid literal 'tru'"}},
		{name: "invalid then valid continues", args: []string{"-m", bad, good}, wantCode: 1,
			outHas: []string{"==> " + good + " <==\n{\"b\""}, errHas: []string{"bad.json:4:3"}},
		{name: "missing file", args: []string{filepath.Join(dir, "nope.json")}, wantCode: 2, errHas: []string{"jsonformat: error: open ", "nope.json"}},
		{name: "missing plus invalid is exit 2", args: []string{bad, filepath.Join(dir, "nope.json")}, wantCode: 2},
		{name: "query", args: []string{"-q", "b[1]", good}, wantOut: "2\n"},
		{name: "query missing", args: []string{"-q", "b[3]", good}, wantCode: 1, wantNoOut: true, errHas: []string{"jsonformat: error: path b[3]: index out of range (len 2)"}},
		{name: "query missing multi names file", args: []string{"-q", "zz", good, good}, wantCode: 1, errHas: []string{good + ": path zz: key not found"}},
		{name: "query syntax", args: []string{"-q", "a..b", good}, wantCode: 2, errHas: []string{"invalid query", "Run 'jsonformat --help'"}},
		{name: "raw needs query", args: []string{"-r", good}, wantCode: 2, errHas: []string{"--raw requires --query"}},
		{name: "minify with indent", args: []string{"-m", "--indent", "4", good}, wantCode: 2, errHas: []string{"--minify and --indent"}},
		{name: "minify with tab", args: []string{"-m", "--tab", good}, wantCode: 2, errHas: []string{"--minify and --tab"}},
		{name: "tab with indent", args: []string{"--tab", "--indent", "2", good}, wantCode: 2, errHas: []string{"--tab and --indent"}},
		{name: "indent range", args: []string{"--indent", "9", good}, wantCode: 2, errHas: []string{"between 0 and 8"}},
		{name: "check with write", args: []string{"--check", "-w", good}, wantCode: 2, errHas: []string{"--check only validates"}},
		{name: "write with query", args: []string{"-w", "-q", "a", good}, wantCode: 2, errHas: []string{"--write cannot be combined"}},
		{name: "write with stdin", stdin: "{}", args: []string{"-w"}, wantCode: 2, errHas: []string{"cannot rewrite standard input"}},
		{name: "write with dash", stdin: "{}", args: []string{"-w", good, "-"}, wantCode: 2},
		{name: "unknown flag", args: []string{"--nope"}, wantCode: 2, errHas: []string{"unknown flag"}},
		{name: "bad indent value", args: []string{"--indent", "x"}, wantCode: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var in io.Reader
			if tt.stdin != "" {
				in = strings.NewReader(tt.stdin)
			}
			r := runCLI(t, in, tt.args...)
			if r.code != tt.wantCode {
				t.Fatalf("code = %d, want %d\nstdout: %s\nstderr: %s", r.code, tt.wantCode, r.stdout, r.stderr)
			}
			if tt.wantOut != "" && r.stdout != tt.wantOut {
				t.Errorf("stdout:\n%q\nwant:\n%q", r.stdout, tt.wantOut)
			}
			for _, s := range tt.outHas {
				if !strings.Contains(r.stdout, s) {
					t.Errorf("stdout missing %q:\n%s", s, r.stdout)
				}
			}
			for _, s := range tt.errHas {
				if !strings.Contains(r.stderr, s) {
					t.Errorf("stderr missing %q:\n%s", s, r.stderr)
				}
			}
			if tt.wantNoErr && r.stderr != "" {
				t.Errorf("unexpected stderr: %s", r.stderr)
			}
			if tt.wantNoOut && r.stdout != "" {
				t.Errorf("unexpected stdout: %s", r.stdout)
			}
			if strings.Contains(r.stdout+r.stderr, "\x1b[") {
				t.Error("ANSI codes in non-TTY output")
			}
		})
	}
}

func TestCLINoInput(t *testing.T) {
	plainEnv(t)
	r := runCLI(t, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "no input") {
		t.Fatalf("%+v", r)
	}
}

func TestCLICheck(t *testing.T) {
	plainEnv(t)
	dir := t.TempDir()
	good := writeFile(t, dir, "good.json", `{"a":1}`)
	bad := writeFile(t, dir, "bad.json", brokenDoc)
	missing := filepath.Join(dir, "missing.json")

	r := runCLI(t, nil, "--check", good)
	if r.code != 0 || r.stdout != "✔ "+good+": valid JSON\n" {
		t.Fatalf("%+v", r)
	}

	r = runCLI(t, nil, "--check", good, bad)
	if r.code != 1 {
		t.Fatalf("code %d", r.code)
	}
	for _, s := range []string{"✔ " + good + ": valid JSON\n", "✘ " + bad + ":4:3: expected ',' or '}'", "    4 |   \"debug\": true\n", "2 files checked: 1 valid, 1 invalid"} {
		if !strings.Contains(r.stdout, s) {
			t.Errorf("missing %q in:\n%s", s, r.stdout)
		}
	}

	r = runCLI(t, nil, "--check", good, missing)
	if r.code != 2 || !strings.Contains(r.stdout, "✘ "+missing+": ") {
		t.Fatalf("%+v", r)
	}

	r = runCLI(t, strings.NewReader("[1,]"), "--check")
	if r.code != 1 || !strings.Contains(r.stdout, "✘ <stdin>:1:3: trailing comma before ']'") {
		t.Fatalf("%+v", r)
	}
}

func TestCLICheckJSON(t *testing.T) {
	plainEnv(t)
	dir := t.TempDir()
	good := writeFile(t, dir, "good.json", `{"a":1}`)
	bad := writeFile(t, dir, "bad.json", brokenDoc)
	missing := filepath.Join(dir, "missing.json")
	r := runCLI(t, nil, "--check", "--json", good, bad, missing)
	if r.code != 2 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	var rep struct {
		Files []struct {
			File  string `json:"file"`
			Valid bool   `json:"valid"`
			Error *struct {
				Line    int    `json:"line"`
				Col     int    `json:"col"`
				Offset  *int64 `json:"offset"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatalf("invalid JSON report: %v\n%s", err, r.stdout)
	}
	if len(rep.Files) != 3 {
		t.Fatalf("files: %+v", rep.Files)
	}
	if f := rep.Files[0]; !f.Valid || f.Error != nil || f.File != good {
		t.Errorf("good: %+v", f)
	}
	f := rep.Files[1]
	if f.Valid || f.Error == nil || f.Error.Line != 4 || f.Error.Col != 3 || f.Error.Offset == nil || *f.Error.Offset != 39 ||
		!strings.HasPrefix(f.Error.Message, "expected ',' or '}'") {
		t.Errorf("bad: %+v %+v", f, f.Error)
	}
	if f := rep.Files[2]; f.Valid || f.Error == nil || f.Error.Offset != nil || f.Error.Message == "" {
		t.Errorf("missing: %+v", f)
	}
	if strings.Contains(r.stdout, "\x1b") || r.stderr != "" {
		t.Errorf("unexpected color or stderr: %q", r.stderr)
	}
	// stdin is reported as <stdin>
	r = runCLI(t, strings.NewReader("{}"), "--check", "--json")
	if r.code != 0 || !strings.Contains(r.stdout, `"file": "<stdin>"`) {
		t.Fatalf("%+v", r)
	}
}

func TestCLIWrite(t *testing.T) {
	plainEnv(t)
	dir := t.TempDir()
	a := writeFile(t, dir, "a.json", `{"b":1,"a":2}`)
	b := writeFile(t, dir, "b.json", "{\n  \"x\": 1\n}\n")
	bad := writeFile(t, dir, "bad.json", brokenDoc)

	r := runCLI(t, nil, "-w", "-S", a, b)
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if want := "✔ formatted " + a + "\n• " + b + " already formatted\n"; r.stdout != want {
		t.Errorf("stdout %q want %q", r.stdout, want)
	}
	if got, _ := os.ReadFile(a); string(got) != "{\n  \"a\": 2,\n  \"b\": 1\n}\n" {
		t.Errorf("a.json = %q", got)
	}

	r = runCLI(t, nil, "-w", "--minify", bad, a)
	if r.code != 1 || !strings.Contains(r.stderr, bad+":4:3: error:") || !strings.Contains(r.stdout, "✔ formatted "+a) {
		t.Fatalf("%+v", r)
	}
	if got, _ := os.ReadFile(bad); string(got) != brokenDoc {
		t.Error("invalid file was modified")
	}

	r = runCLI(t, nil, "-w", "--json", a, bad, filepath.Join(dir, "missing.json"))
	if r.code != 2 {
		t.Fatalf("%+v", r)
	}
	var rep report
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatalf("%v\n%s", err, r.stdout)
	}
	if len(rep.Files) != 3 || rep.Files[0].Changed == nil || !*rep.Files[0].Changed || !rep.Files[0].Valid ||
		rep.Files[1].Valid || rep.Files[1].Error.Line != 4 || rep.Files[2].Error == nil ||
		!strings.HasPrefix(rep.Files[2].Error.Message, "open ") {
		t.Errorf("report: %s", r.stdout)
	}
	if r.stderr != "" {
		t.Errorf("stderr with --json: %s", r.stderr)
	}
}

func TestCLIColor(t *testing.T) {
	plainEnv(t)
	t.Setenv("FORCE_COLOR", "1")
	dir := t.TempDir()
	f := writeFile(t, dir, "a.json", `{"a":[1,true,null,"s"]}`)

	r := runCLI(t, nil, f)
	if !strings.Contains(r.stdout, "\x1b[") {
		t.Fatalf("expected color: %q", r.stdout)
	}
	if ansiRE.ReplaceAllString(r.stdout, "") != "{\n  \"a\": [\n    1,\n    true,\n    null,\n    \"s\"\n  ]\n}\n" {
		t.Errorf("stripped output differs: %q", r.stdout)
	}
	for _, args := range [][]string{{"--no-color", f}, {"--json", f}} {
		if r := runCLI(t, nil, args...); strings.Contains(r.stdout, "\x1b") {
			t.Errorf("%v: unexpected color", args)
		}
	}
	// -w never writes color into files
	if r := runCLI(t, nil, "-w", f); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if got, _ := os.ReadFile(f); bytes.Contains(got, []byte("\x1b")) {
		t.Errorf("color written to file: %q", got)
	}
	// the caret is red on a color stderr
	r = runCLI(t, strings.NewReader("[1 2]"))
	if !strings.Contains(r.stderr, "\x1b[") || !strings.Contains(ansiRE.ReplaceAllString(r.stderr, ""), "^") {
		t.Errorf("stderr: %q", r.stderr)
	}
}

func TestCLIHelpAndVersion(t *testing.T) {
	plainEnv(t)
	r := runCLI(t, nil, "--help")
	for _, s := range []string{"Examples:", "jsonformat --check configs/*.json", "Exit codes:", "130", "--sort-keys", "--query path"} {
		if !strings.Contains(r.stdout, s) {
			t.Errorf("help missing %q", s)
		}
	}
	if r.code != 0 {
		t.Errorf("help code %d", r.code)
	}
	r = runCLI(t, nil, "--version")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "jsonformat ") {
		t.Errorf("version: %+v", r)
	}
}

func TestCLIInterrupted(t *testing.T) {
	plainEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	f := writeFile(t, dir, "a.json", `[1]`)
	for _, args := range [][]string{{f}, {"--check", f}, {"-w", "-m", f}} {
		r := runCLICtx(t, ctx, nil, args...)
		if r.code != cliutil.ExitInterrupted {
			t.Errorf("%v: code %d (%s)", args, r.code, r.stderr)
		}
	}
}

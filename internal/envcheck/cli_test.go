package envcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
)

var (
	sampleEnv    = filepath.Join("..", "..", "docs", "samples", ".env")
	sampleSchema = filepath.Join("..", "..", "docs", "samples", ".env.example")
)

// sampleSecrets are values in docs/samples/.env that must never be printed.
var sampleSecrets = []string{"ak_live_4f9Q2xV7mN1pR8sT3wY6zB0c", "s3cr3t-pa55", "postgres://orders"}

type cliResult struct {
	code     int
	out, err string
}

func runCLI(t *testing.T, stdin string, args ...string) cliResult {
	t.Helper()
	return runCLIContext(context.Background(), t, stdin, args...)
}

func runCLIContext(ctx context.Context, t *testing.T, stdin string, args ...string) cliResult {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	var out, errb bytes.Buffer
	code := cliutil.Run(ctx, NewCommand(cliutil.IO{In: strings.NewReader(stdin), Out: &out, Err: &errb}), args)
	return cliResult{code, out.String(), errb.String()}
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertNoSecrets(t *testing.T, s string, secrets ...string) {
	t.Helper()
	for _, sec := range secrets {
		if strings.Contains(s, sec) {
			t.Errorf("output leaks secret %q:\n%s", sec, s)
		}
	}
}

func TestCLISampleText(t *testing.T) {
	r := runCLI(t, "", "--env", sampleEnv, "--schema", sampleSchema)
	if r.code != cliutil.ExitFail {
		t.Fatalf("code = %d, stderr=%q\n%s", r.code, r.err, r.out)
	}
	for _, want := range []string{
		"KEY", "STATUS", "MESSAGE",
		`✘ error  "70000" is above the maximum 65535 (line 3)`,
		`"verbose" is not one of debug|info|warn|error (line 5)`,
		`"ops@@example.com" is not a valid email address (line 12)`,
		"JWT_SECRET       ✘ error  required but not set",
		"FEATURE_FLAGS    ▲ warn   not declared in the schema (line 16)",
		"duplicate key on lines 10, 17; the last one (line 17) wins",
		"REDIS_URL        • skip   optional, not set",
		"DATABASE_URL     ✔ ok     url, secret",
		"✘ 4 errors, 2 warnings, 8 ok, 1 skipped — " + sampleEnv + " vs " + sampleSchema,
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output missing %q:\n%s", want, r.out)
		}
	}
	if r.err != "" {
		t.Errorf("unexpected stderr %q", r.err)
	}
	assertNoSecrets(t, r.out, sampleSecrets...)
}

func TestCLISampleShowValuesMasksSecrets(t *testing.T) {
	r := runCLI(t, "", "--env", sampleEnv, "--schema", sampleSchema, "--show-values")
	if r.code != cliutil.ExitFail {
		t.Fatalf("code = %d", r.code)
	}
	if !strings.Contains(r.out, "VALUE") || !strings.Contains(r.out, "orders-api") || !strings.Contains(r.out, Mask) {
		t.Errorf("value column missing:\n%s", r.out)
	}
	assertNoSecrets(t, r.out, sampleSecrets...)
}

func TestCLISampleJSON(t *testing.T) {
	for _, extra := range [][]string{nil, {"--show-values"}} {
		args := append([]string{"--env", sampleEnv, "--schema", sampleSchema, "--json"}, extra...)
		r := runCLI(t, "", args...)
		if r.code != cliutil.ExitFail {
			t.Fatalf("code = %d stderr=%q", r.code, r.err)
		}
		assertNoSecrets(t, r.out, sampleSecrets...)
		if strings.Contains(r.out, "\x1b[") {
			t.Error("JSON contains ANSI codes")
		}
		var rep struct {
			EnvFile    string         `json:"env_file"`
			SchemaFile string         `json:"schema_file"`
			OK         bool           `json:"ok"`
			Summary    map[string]int `json:"summary"`
			Results    []struct {
				Key     string  `json:"key"`
				Status  string  `json:"status"`
				Message string  `json:"message"`
				Line    int     `json:"line"`
				Value   *string `json:"value"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(r.out), &rep); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, r.out)
		}
		if rep.OK || rep.EnvFile != sampleEnv || rep.SchemaFile != sampleSchema {
			t.Errorf("report header = %+v", rep)
		}
		if rep.Summary["errors"] != 4 || rep.Summary["warnings"] != 2 || rep.Summary["ok"] != 8 || rep.Summary["skipped"] != 1 {
			t.Errorf("summary = %v", rep.Summary)
		}
		if len(rep.Results) != 15 || rep.Results[1].Key != "PORT" || rep.Results[1].Status != "error" || rep.Results[1].Line != 3 {
			t.Errorf("results = %+v", rep.Results)
		}
		withValues := extra != nil
		if (rep.Results[0].Value != nil) != withValues {
			t.Errorf("value presence = %v, want %v", rep.Results[0].Value != nil, withValues)
		}
	}
}

func TestCLIValidAndStrict(t *testing.T) {
	dir := t.TempDir()
	schema := writeFile(t, dir, "schema", "PORT=  # required,int\nNAME=\n")
	clean := writeFile(t, dir, "clean.env", "PORT=8080\r\nNAME=web\r\n")
	extra := writeFile(t, dir, "extra.env", "PORT=8080\nEXTRA=1\n")

	r := runCLI(t, "", "--schema", schema, clean)
	if r.code != cliutil.ExitOK || !strings.Contains(r.out, "✔ 0 errors, 0 warnings, 2 ok — "+clean) {
		t.Fatalf("clean: code=%d\n%s%s", r.code, r.out, r.err)
	}
	r = runCLI(t, "", "--schema", schema, "-e", extra)
	if r.code != cliutil.ExitOK || !strings.Contains(r.out, "▲ 0 errors, 1 warning, 1 ok, 1 skipped") {
		t.Fatalf("warnings without strict: code=%d\n%s", r.code, r.out)
	}
	r = runCLI(t, "", "--schema", schema, "-e", extra, "--strict")
	if r.code != cliutil.ExitFail || !strings.Contains(r.out, "(--strict: warnings fail)") || !strings.Contains(r.out, "✘ 0 errors") {
		t.Fatalf("strict: code=%d\n%s", r.code, r.out)
	}
	r = runCLI(t, "", "--schema", schema, "-e", extra, "--strict", "--json")
	if r.code != cliutil.ExitFail || !strings.Contains(r.out, `"ok": false`) || !strings.Contains(r.out, `"strict": true`) {
		t.Fatalf("strict json: code=%d\n%s", r.code, r.out)
	}
}

func TestCLIStdin(t *testing.T) {
	dir := t.TempDir()
	schema := writeFile(t, dir, "schema", "PORT=  # required,int\n")
	env := writeFile(t, dir, "env", "PORT=1\n")

	r := runCLI(t, "PORT=abc\n", "--env", "-", "--schema", schema)
	if r.code != cliutil.ExitFail || !strings.Contains(r.out, `"abc" is not an integer`) || !strings.Contains(r.out, "<stdin> vs") {
		t.Fatalf("env from stdin: code=%d\n%s", r.code, r.out)
	}
	r = runCLI(t, "PORT= # required,int,min=5\n", "--env", env, "--schema", "-")
	if r.code != cliutil.ExitFail || !strings.Contains(r.out, "below the minimum 5") {
		t.Fatalf("schema from stdin: code=%d\n%s%s", r.code, r.out, r.err)
	}
	// No .env in the working directory and data piped in: read stdin.
	r = runCLI(t, "PORT=7\n", "--schema", schema)
	if r.code != cliutil.ExitOK || !strings.Contains(r.out, "<stdin>") {
		t.Fatalf("stdin fallback: code=%d\n%s%s", r.code, r.out, r.err)
	}
	// ...but empty stdin keeps the original "no .env" error.
	r = runCLI(t, "", "--schema", schema)
	if r.code != cliutil.ExitUsage || !strings.Contains(r.err, ".env") {
		t.Fatalf("empty stdin fallback: code=%d\n%s", r.code, r.err)
	}
}

func TestCLIUsageAndIOErrors(t *testing.T) {
	dir := t.TempDir()
	schema := writeFile(t, dir, "schema", "A=\n")
	badSchema := writeFile(t, dir, "bad", "A=1\nB= # required,nope\nC= # regex=(\n")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"both positional and flag", []string{"--env", "x", "y"}, "not both"},
		{"too many args", []string{"a", "b"}, "accepts at most 1 arg"},
		{"both stdin", []string{"--env", "-", "--schema", "-"}, "only one of --env and --schema"},
		{"empty env", []string{"--env", "", "--schema", schema}, "must not be empty"},
		{"unknown flag", []string{"--bogus"}, "unknown flag"},
		{"default schema missing", []string{"--env", schema}, "create one with 'envcheck init > .env.example'"},
		{"explicit schema missing", []string{"--schema", filepath.Join(dir, "nope")}, "nope"},
		{"env missing", []string{"--schema", schema, filepath.Join(dir, "missing.env")}, "missing.env"},
		{"env is a directory", []string{"--schema", schema, dir}, "envcheck: error:"},
		{"schema error", []string{"--schema", badSchema, schema}, "bad:2: B: unknown rule \"nope\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runCLI(t, "", tt.args...)
			if r.code != cliutil.ExitUsage {
				t.Fatalf("code = %d, want 2\nstdout=%s\nstderr=%s", r.code, r.out, r.err)
			}
			if !strings.Contains(r.err, tt.want) {
				t.Fatalf("stderr = %q, want %q", r.err, tt.want)
			}
		})
	}
	r := runCLI(t, "", "--schema", badSchema, schema)
	if !strings.Contains(r.err, "bad:3: C: invalid regex") {
		t.Errorf("all schema problems should be listed: %q", r.err)
	}
}

func TestCLIMalformedAndSecretsInMessages(t *testing.T) {
	dir := t.TempDir()
	schema := writeFile(t, dir, "schema", "TOKEN=  # required,secret,int\nPIN=  # secret,min=6\nOK=\n")
	env := writeFile(t, dir, "env", "TOKEN=topsecretvalue\nPIN='12'\nOK=\"unterminated\nbad-key=1\n")
	for _, extra := range []string{"", "--json"} {
		args := []string{"--schema", schema, "--show-values", env}
		if extra != "" {
			args = append(args, extra)
		}
		r := runCLI(t, "", args...)
		if r.code != cliutil.ExitFail {
			t.Fatalf("code = %d\n%s%s", r.code, r.out, r.err)
		}
		assertNoSecrets(t, r.out+r.err, "topsecretvalue", "'12'", `"12"`)
		for _, want := range []string{Mask + " is not an integer", "value is shorter than 6 characters", "unterminated double-quoted value for OK"} {
			if !strings.Contains(r.out, want) {
				t.Errorf("%s output missing %q:\n%s", extra, want, r.out)
			}
		}
		if extra == "" && !strings.Contains(r.out, "line 3  ✘ error") {
			t.Errorf("malformed row should be keyed by line:\n%s", r.out)
		}
	}
}

func TestCLIColor(t *testing.T) {
	dir := t.TempDir()
	schema := writeFile(t, dir, "schema", "A=  # required\n")
	env := writeFile(t, dir, "env", "A=\n")
	var out, errb bytes.Buffer
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	code := cliutil.Run(context.Background(), NewCommand(cliutil.IO{In: strings.NewReader(""), Out: &out, Err: &errb}),
		[]string{"--schema", schema, env})
	if code != cliutil.ExitFail || !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("expected colored output, code=%d: %q", code, out.String())
	}
	out.Reset()
	cliutil.Run(context.Background(), NewCommand(cliutil.IO{In: strings.NewReader(""), Out: &out, Err: &errb}),
		[]string{"--schema", schema, env, "--no-color"})
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("--no-color output has ANSI: %q", out.String())
	}
}

func TestCLIEmptySchemaAndEnv(t *testing.T) {
	dir := t.TempDir()
	schema := writeFile(t, dir, "schema", "# nothing\n")
	env := writeFile(t, dir, "env", "")
	r := runCLI(t, "", "--schema", schema, env)
	if r.code != cliutil.ExitOK || strings.Contains(r.out, "KEY") || !strings.Contains(r.out, "✔ 0 errors, 0 warnings, 0 ok") {
		t.Fatalf("code=%d\n%s", r.code, r.out)
	}
	r = runCLI(t, "", "--schema", schema, env, "--json")
	if !strings.Contains(r.out, `"results": []`) {
		t.Fatalf("results must be an empty array: %s", r.out)
	}
}

func TestCLIHelpVersionInterrupt(t *testing.T) {
	r := runCLI(t, "", "--help")
	for _, want := range []string{"Examples:", "envcheck --env .env.production", "Exit codes:", "regex= consumes the rest", "Variable expansion"} {
		if r.code != 0 || !strings.Contains(r.out, want) {
			t.Errorf("help missing %q (code %d)", want, r.code)
		}
	}
	r = runCLI(t, "", "init", "--help")
	if r.code != 0 || !strings.Contains(r.out, "envcheck init > .env.example") || !strings.Contains(r.out, "Exit codes:") {
		t.Errorf("init help: %s", r.out)
	}
	r = runCLI(t, "", "--version")
	if r.code != 0 || !strings.HasPrefix(r.out, "envcheck ") {
		t.Errorf("version: %q", r.out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = runCLIContext(ctx, t, "A=1", "--env", "-", "--schema", sampleSchema)
	if r.code != cliutil.ExitInterrupted {
		t.Errorf("canceled: code = %d, stderr=%q", r.code, r.err)
	}
}

func TestCLIInit(t *testing.T) {
	dir := t.TempDir()
	env := writeFile(t, dir, "env", "# Server\nPORT=3000\nAPI_KEY=sk-123 # prod key\nbroken\n")
	out := filepath.Join(dir, "schema")

	r := runCLI(t, "", "init", "--env", env)
	if r.code != 0 || !strings.Contains(r.out, "PORT=     # required,int") || !strings.Contains(r.out, "API_KEY=  # required,secret") {
		t.Fatalf("init stdout: code=%d\n%s", r.code, r.out)
	}
	assertNoSecrets(t, r.out+r.err, "sk-123", "3000", "prod key")
	if !strings.Contains(r.err, ":4: skipped malformed line") {
		t.Errorf("malformed line warning missing: %q", r.err)
	}

	r = runCLI(t, "", "init", env, "-o", out)
	if r.code != 0 || !strings.Contains(r.out, "✔ wrote 2 keys to "+out) {
		t.Fatalf("init -o: code=%d\n%s%s", r.code, r.out, r.err)
	}
	data, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(data), "# Server\nPORT=") {
		t.Fatalf("written schema = %q, %v", data, err)
	}

	r = runCLI(t, "", "init", env, "-o", out)
	if r.code != cliutil.ExitUsage || !strings.Contains(r.err, "already exists (use --force to overwrite)") {
		t.Fatalf("overwrite refused: code=%d %q", r.code, r.err)
	}
	// A marker that cannot appear in the generated header (which embeds the
	// temp path, e.g. macOS /var/folders/...).
	const stale = "STALE-SCHEMA-MARKER"
	if err := os.WriteFile(out, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	r = runCLI(t, "", "init", env, "-o", out, "--force")
	if data, _ := os.ReadFile(out); r.code != 0 || strings.Contains(string(data), stale) {
		t.Fatalf("--force: code=%d data=%q", r.code, data)
	}

	r = runCLI(t, "X=1\n", "init", "--env", "-", "--json")
	var rep InitReport
	if err := json.Unmarshal([]byte(r.out), &rep); err != nil || r.code != 0 {
		t.Fatalf("init json: %v code=%d\n%s", err, r.code, r.out)
	}
	if rep.EnvFile != "<stdin>" || len(rep.Keys) != 1 || rep.Keys[0].Rules != "required,int" || !strings.Contains(rep.Schema, "X=") || rep.Skipped == nil {
		t.Errorf("init json = %+v", rep)
	}

	r = runCLI(t, "", "init", "--env", env, "-o", "-")
	if r.code != 0 || !strings.Contains(r.out, "PORT=") {
		t.Errorf("-o - should write stdout: %s", r.out)
	}
	r = runCLI(t, "", "init", "--env", filepath.Join(dir, "missing"))
	if r.code != cliutil.ExitUsage {
		t.Errorf("missing env: code=%d", r.code)
	}
	r = runCLI(t, "", "init", "--env", env, "other")
	if r.code != cliutil.ExitUsage || !strings.Contains(r.err, "not both") {
		t.Errorf("both: code=%d %q", r.code, r.err)
	}
	r = runCLI(t, "", "init", "--env", "")
	if r.code != cliutil.ExitUsage {
		t.Errorf("empty env: code=%d", r.code)
	}
	r = runCLI(t, "\n", "init", "--env", "-")
	if r.code != 0 || !strings.Contains(r.err, "no keys found") {
		t.Errorf("empty init: code=%d %q", r.code, r.err)
	}
	r = runCLI(t, "", "init", "--env", env, "-o", filepath.Join(dir, "no", "such", "dir"))
	if r.code != cliutil.ExitUsage {
		t.Errorf("unwritable output: code=%d", r.code)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'A'
	}
	return len(p), nil
}

type blockingReader struct{ ch chan struct{} }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.ch
	return 0, io.EOF
}

func TestReadInputLimitsAndCancel(t *testing.T) {
	if _, _, err := readInput(context.Background(), zeroReader{}, "-"); !errors.Is(err, errTooLarge) {
		t.Errorf("oversized stdin: err = %v", err)
	}
	if _, _, err := readInput(context.Background(), nil, "-"); err == nil {
		t.Error("nil stdin should fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	br := blockingReader{ch: make(chan struct{})}
	defer close(br.ch)
	if _, _, err := readInput(ctx, br, "-"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled read: err = %v", err)
	}
}

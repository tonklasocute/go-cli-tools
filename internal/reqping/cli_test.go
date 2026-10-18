package reqping

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
)

type cliResult struct {
	code     int
	out, err string
}

func runCLI(t *testing.T, stdin io.Reader, args ...string) cliResult {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	var out, errb bytes.Buffer
	code := cliutil.Run(context.Background(), NewCommand(cliutil.IO{In: stdin, Out: &out, Err: &errb}), args)
	return cliResult{code, out.String(), errb.String()}
}

// testSite serves /ok (200 "all ok"), /missing (404), /hdr (200 only with
// X-Token: secret) and /moved (302 → /ok).
func testSite(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "all ok") })
	mux.HandleFunc("/missing", http.NotFound)
	mux.HandleFunc("/hdr", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") != "secret" {
			w.WriteHeader(http.StatusForbidden)
		}
	})
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusFound) })
	return newServer(t, mux.ServeHTTP)
}

func TestCLIText(t *testing.T) {
	srv := testSite(t)
	r := runCLI(t, nil, srv.URL+"/ok")
	if r.code != 0 || !strings.Contains(r.out, "✔ 200") || !strings.Contains(r.out, "✔ 1 healthy, ✘ 0 unhealthy") {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "NAME") || !strings.Contains(r.out, "LATENCY") {
		t.Errorf("missing header: %q", r.out)
	}

	r = runCLI(t, nil, srv.URL+"/ok", srv.URL+"/missing")
	if r.code != cliutil.ExitFail || !strings.Contains(r.out, "expected 2xx, got 404") || r.err != "" {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.err)
	}
}

func TestCLIFlags(t *testing.T) {
	srv := testSite(t)
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{"contains hit", []string{"--contains", "all ok", srv.URL + "/ok"}, 0, "✔ 200"},
		{"contains miss", []string{"--contains", "nope", srv.URL + "/ok"}, 1, `body missing "nope"`},
		{"header", []string{"-H", "X-Token: secret", srv.URL + "/hdr"}, 0, "✔ 200"},
		{"header missing", []string{srv.URL + "/hdr"}, 1, "expected 2xx, got 403"},
		{"status flag", []string{"--status", "404", srv.URL + "/missing"}, 0, "✔ 404"},
		{"follow", []string{srv.URL + "/moved"}, 0, "redirected to " + srv.URL + "/ok"},
		{"no-follow", []string{"--no-follow", srv.URL + "/moved"}, 0, "✔ 302"},
		{"head", []string{"--method", "head", srv.URL + "/ok"}, 0, "✔ 200"},
		{"count", []string{"--count", "3", srv.URL + "/ok"}, 0, "3/3"},
		{"timeout ok", []string{"--timeout", "5s", "--concurrency", "1", srv.URL + "/ok"}, 0, "✔ 1 healthy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runCLI(t, nil, tt.args...)
			if r.code != tt.wantCode || !strings.Contains(r.out, tt.wantOut) {
				t.Fatalf("code=%d (want %d) out=%q err=%q", r.code, tt.wantCode, r.out, r.err)
			}
		})
	}
	r := runCLI(t, nil, "--count", "2", srv.URL+"/ok")
	for _, col := range []string{"MIN", "AVG", "P95", "MAX"} {
		if !strings.Contains(r.out, col) {
			t.Errorf("--count output missing %s column: %q", col, r.out)
		}
	}
}

func TestCLIJSON(t *testing.T) {
	srv := testSite(t)
	r := runCLI(t, nil, "--json", srv.URL+"/ok", srv.URL+"/missing")
	if r.code != 1 {
		t.Fatalf("code=%d err=%q", r.code, r.err)
	}
	if strings.Contains(r.out, "\x1b[") {
		t.Error("JSON contains ANSI codes")
	}
	var rep JSONReport
	if err := json.Unmarshal([]byte(r.out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, r.out)
	}
	if rep.Healthy || len(rep.Targets) != 2 {
		t.Fatalf("report %+v", rep)
	}
	ok, missing := rep.Targets[0], rep.Targets[1]
	if !ok.Healthy || *ok.StatusCode != 200 || ok.Method != http.MethodGet || ok.Expected != "2xx" || ok.Error != nil || ok.Stats.Count != 1 {
		t.Errorf("ok target %+v", ok)
	}
	if missing.Healthy || *missing.StatusCode != 404 || missing.Error == nil || *missing.Error != "expected 2xx, got 404" {
		t.Errorf("missing target %+v", missing)
	}
}

func TestCLITargetsFile(t *testing.T) {
	srv := testSite(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "targets.txt")
	content := fmt.Sprintf("# test\nhome %s/ok\nnf %s/missing 404\n", srv.URL, srv.URL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r := runCLI(t, nil, "-f", path)
	if r.code != 0 || !strings.Contains(r.out, "home") || !strings.Contains(r.out, "nf") {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.err)
	}
	// Args come first, then the file, in input order.
	r = runCLI(t, nil, "--json", "-f", path, srv.URL+"/ok")
	var rep JSONReport
	if err := json.Unmarshal([]byte(r.out), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Targets) != 3 || rep.Targets[1].Name != "home" || rep.Targets[2].Name != "nf" {
		t.Fatalf("targets %+v", rep.Targets)
	}

	// Stdin via "-f -" and implicitly when piped.
	for _, args := range [][]string{{"-f", "-"}, {}} {
		r = runCLI(t, strings.NewReader(content), args...)
		if r.code != 0 || !strings.Contains(r.out, "✔ 2 healthy") {
			t.Fatalf("stdin %v: code=%d out=%q err=%q", args, r.code, r.out, r.err)
		}
	}
}

func TestCLIUsageErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte("ok https://a.example\nnot-a-url\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("# nothing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		stdin   io.Reader
		args    []string
		wantErr string
	}{
		{"no targets", nil, nil, "no targets"},
		{"bad url", nil, []string{"go.dev"}, "missing scheme"},
		{"ftp url", nil, []string{"ftp://go.dev"}, "unsupported scheme"},
		{"bad timeout", nil, []string{"--timeout", "soon", "https://go.dev"}, "invalid argument"},
		{"zero timeout", nil, []string{"--timeout", "0s", "https://go.dev"}, "--timeout"},
		{"bad method", nil, []string{"--method", "POST", "https://go.dev"}, "invalid --method"},
		{"bad status", nil, []string{"--status", "abc", "https://go.dev"}, "invalid --status"},
		{"bad header", nil, []string{"-H", "NoColon", "https://go.dev"}, `invalid header "NoColon"`},
		{"bad header name", nil, []string{"-H", "Bad Key: v", "https://go.dev"}, "invalid header"},
		{"bad count", nil, []string{"--count", "0", "https://go.dev"}, "--count"},
		{"bad concurrency", nil, []string{"--concurrency", "0", "https://go.dev"}, "--concurrency"},
		{"negative watch", nil, []string{"--watch", "-1s", "https://go.dev"}, "--watch"},
		{"follow conflict", nil, []string{"--follow", "--no-follow", "https://go.dev"}, "mutually exclusive"},
		{"contains with head", nil, []string{"--method", "HEAD", "--contains", "x", "https://go.dev"}, "--contains"},
		{"missing file", nil, []string{"-f", filepath.Join(dir, "nope.txt")}, "cannot read targets file"},
		{"malformed file", nil, []string{"-f", bad}, bad + `:2: invalid URL "not-a-url"`},
		{"empty file", nil, []string{"-f", empty}, "no targets found in " + empty},
		{"stdin twice", strings.NewReader("https://go.dev\n"), []string{"-f", "-", "-f", "-"}, "only be read once"},
		{"empty stdin", strings.NewReader(""), nil, "no targets found in <stdin>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runCLI(t, tt.stdin, tt.args...)
			if r.code != cliutil.ExitUsage || !strings.Contains(r.err, tt.wantErr) {
				t.Fatalf("code=%d err=%q, want 2 and %q", r.code, r.err, tt.wantErr)
			}
		})
	}
}

func TestCLIInsecureWarns(t *testing.T) {
	srv := newQuietTLSServer(t, nil)
	r := runCLI(t, nil, "--insecure", srv.URL)
	if r.code != 0 || !strings.Contains(r.err, "warning: --insecure") {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.err)
	}
	if !regexp.MustCompile(`✔ 200\s+\S+\s+\d+d`).MatchString(r.out) {
		t.Errorf("expected TLS days column: %q", r.out)
	}
	// Without --insecure the self-signed test certificate is rejected.
	r = runCLI(t, nil, srv.URL)
	if r.code != 1 || !strings.Contains(r.out, "tls:") {
		t.Fatalf("code=%d out=%q", r.code, r.out)
	}
}

func TestCLIHelpAndVersion(t *testing.T) {
	r := runCLI(t, nil, "--help")
	if r.code != 0 {
		t.Fatalf("code=%d", r.code)
	}
	for _, want := range []string{"Examples:", "reqping -f docs/samples/targets.txt", "Exit codes:", "130", "--watch", "--insecure"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("help missing %q", want)
		}
	}
	r = runCLI(t, nil, "--version")
	if r.code != 0 || !strings.HasPrefix(r.out, "reqping ") {
		t.Fatalf("version: code=%d out=%q", r.code, r.out)
	}
}

// cancelAfter is a writer that cancels a context once marker has been
// written n times. Writes come from the command goroutine only.
// The buffer is a named field (not embedded) so io.WriteString cannot bypass
// Write through a promoted WriteString method.
type cancelAfter struct {
	buf    bytes.Buffer
	marker string
	n      int
	cancel context.CancelFunc
}

func (w *cancelAfter) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	if strings.Count(w.buf.String(), w.marker) >= w.n {
		w.cancel()
	}
	return n, err
}

func (w *cancelAfter) String() string { return w.buf.String() }

func runWatchCLI(t *testing.T, marker string, args ...string) (int, string) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &cancelAfter{marker: marker, n: 2, cancel: cancel}
	var errb bytes.Buffer
	code := cliutil.Run(ctx, NewCommand(cliutil.IO{Out: out, Err: &errb}), args)
	return code, out.String()
}

func TestCLIWatchText(t *testing.T) {
	srv := testSite(t)
	code, out := runWatchCLI(t, "unhealthy ·", "--watch", "10ms", srv.URL+"/ok")
	if code != cliutil.ExitInterrupted {
		t.Fatalf("code = %d, want 130", code)
	}
	if n := strings.Count(out, "── 20"); n != 2 {
		t.Fatalf("timestamp lines = %d, want 2:\n%s", n, out)
	}
	if strings.Count(out, "✔ 1 healthy") != 2 || strings.Contains(out, "\x1b[") {
		t.Fatalf("unexpected output:\n%q", out)
	}
}

func TestCLIWatchJSON(t *testing.T) {
	srv := testSite(t)
	code, out := runWatchCLI(t, "\n", "--watch", "10ms", "--json", srv.URL+"/missing")
	if code != cliutil.ExitInterrupted {
		t.Fatalf("code = %d, want 130", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("NDJSON lines = %d, want 2:\n%s", len(lines), out)
	}
	for _, l := range lines {
		var rep JSONReport
		if err := json.Unmarshal([]byte(l), &rep); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		if rep.Healthy || len(rep.Targets) != 1 {
			t.Errorf("round %+v", rep)
		}
	}
}

func TestCLIInterruptedBeforeRun(t *testing.T) {
	srv := testSite(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errb bytes.Buffer
	code := cliutil.Run(ctx, NewCommand(cliutil.IO{Out: &out, Err: &errb}), []string{srv.URL + "/ok"})
	if code != cliutil.ExitInterrupted || out.Len() != 0 {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}

func TestParseHeaders(t *testing.T) {
	h, err := parseHeaders([]string{"X-A: 1", "x-a:2", "Authorization:   Bearer x  "})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Values("X-A"); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("X-A = %v", got)
	}
	if h.Get("Authorization") != "Bearer x" {
		t.Errorf("Authorization = %q", h.Get("Authorization"))
	}
	for _, bad := range []string{": v", "a b: c", "k: v\r\nx: y", "noval"} {
		if _, err := parseHeaders([]string{bad}); err == nil {
			t.Errorf("parseHeaders(%q) succeeded", bad)
		}
	}
}

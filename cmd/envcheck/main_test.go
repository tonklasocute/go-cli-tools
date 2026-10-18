package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/testutil"
)

var (
	sampleEnv    = filepath.Join("..", "..", "docs", "samples", ".env")
	sampleSchema = filepath.Join("..", "..", "docs", "samples", ".env.example")
)

func TestBinary(t *testing.T) {
	bin := testutil.BuildBinary(t, ".", "envcheck")
	dir := t.TempDir()

	t.Run("sample fails with table", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--env", sampleEnv, "--schema", sampleSchema)
		if r.Code != 1 {
			t.Fatalf("code = %d stderr=%q", r.Code, r.Stderr)
		}
		for _, want := range []string{"PORT", "✘ error", "JWT_SECRET", "✘ 4 errors, 2 warnings, 8 ok, 1 skipped"} {
			if !strings.Contains(r.Stdout, want) {
				t.Errorf("stdout missing %q:\n%s", want, r.Stdout)
			}
		}
		if strings.Contains(r.Stdout, "\x1b[") {
			t.Error("NO_COLOR output contains ANSI codes")
		}
	})

	t.Run("json masks secrets", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--env", sampleEnv, "--schema", sampleSchema, "--json", "--show-values")
		var rep struct {
			OK      bool `json:"ok"`
			Results []struct {
				Key   string  `json:"key"`
				Value *string `json:"value"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil || r.Code != 1 || rep.OK {
			t.Fatalf("code=%d err=%v\n%s", r.Code, err, r.Stdout)
		}
		if strings.Contains(r.Stdout, "ak_live_") || strings.Contains(r.Stdout, "s3cr3t") {
			t.Errorf("secret leaked:\n%s", r.Stdout)
		}
	})

	t.Run("init then validate from stdin", func(t *testing.T) {
		schema := filepath.Join(dir, "generated.env.example")
		r := testutil.RunBinary(t, bin, nil, "init", "--env", sampleEnv, "-o", schema)
		if r.Code != 0 {
			t.Fatalf("init code=%d %s", r.Code, r.Stderr)
		}
		env, err := os.ReadFile(sampleEnv)
		if err != nil {
			t.Fatal(err)
		}
		r = testutil.RunBinary(t, bin, strings.NewReader(string(env)), "--env", "-", "--schema", schema)
		if r.Code != 0 || !strings.Contains(r.Stdout, "1 warning") {
			t.Fatalf("validate generated: code=%d\n%s%s", r.Code, r.Stdout, r.Stderr)
		}
		r = testutil.RunBinary(t, bin, nil, "init", "--env", sampleEnv, "-o", schema)
		if r.Code != 2 || !strings.Contains(r.Stderr, "already exists") {
			t.Fatalf("overwrite: code=%d %q", r.Code, r.Stderr)
		}
	})

	t.Run("usage error", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--strict=maybe")
		if r.Code != 2 || !strings.Contains(r.Stderr, "envcheck: error:") || !strings.Contains(r.Stderr, "Run 'envcheck --help'") {
			t.Fatalf("code=%d stderr=%q", r.Code, r.Stderr)
		}
	})

	t.Run("version", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--version")
		if r.Code != 0 || !strings.HasPrefix(r.Stdout, "envcheck ") {
			t.Fatalf("code=%d %q", r.Code, r.Stdout)
		}
	})
}

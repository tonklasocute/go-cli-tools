package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/testutil"
)

func writeFile(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBinary(t *testing.T) {
	bin := testutil.BuildBinary(t, ".", "dirsize")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main.go"), 3000)
	writeFile(t, filepath.Join(root, "src", "util", "util.go"), 1000)
	writeFile(t, filepath.Join(root, "assets", "logo.png"), 20000)
	writeFile(t, filepath.Join(root, "node_modules", "dep", "index.js"), 90000)
	writeFile(t, filepath.Join(root, "README.md"), 500)

	t.Run("text", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, root, "--exclude", "node_modules")
		if r.Code != 0 {
			t.Fatalf("code %d: %s", r.Code, r.Stderr)
		}
		for _, want := range []string{"depth 1", "assets/", "src/", "README.md", "Total 23.9 KiB in 4 files, 3 dirs"} {
			if !strings.Contains(r.Stdout, want) {
				t.Errorf("missing %q:\n%s", want, r.Stdout)
			}
		}
		if strings.Contains(r.Stdout, "node_modules") && !strings.Contains(r.Stdout, "excluding node_modules") {
			t.Errorf("excluded dir listed:\n%s", r.Stdout)
		}
		if strings.Contains(r.Stdout, "\x1b[") {
			t.Error("ANSI codes with NO_COLOR")
		}
	})

	t.Run("json files", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, root, "--files", "--top", "2", "--json")
		if r.Code != 0 {
			t.Fatalf("code %d: %s", r.Code, r.Stderr)
		}
		var doc struct {
			TotalBytes int64 `json:"total_bytes"`
			Entries    []struct {
				Path  string `json:"path"`
				Bytes int64  `json:"bytes"`
				IsDir bool   `json:"is_dir"`
			} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &doc); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, r.Stdout)
		}
		if doc.TotalBytes != 114500 || len(doc.Entries) != 2 ||
			doc.Entries[0].Path != "node_modules/dep/index.js" || doc.Entries[1].Path != "assets/logo.png" || doc.Entries[0].IsDir {
			t.Errorf("doc = %+v", doc)
		}
	})

	t.Run("depth 2 min size", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, root, "-d", "2", "--min-size", "2K")
		if r.Code != 0 || !strings.Contains(r.Stdout, "node_modules/dep/") || !strings.Contains(r.Stdout, "src/main.go") ||
			strings.Contains(r.Stdout, "util/") || !strings.Contains(r.Stdout, "… and 2 more") {
			t.Errorf("code %d:\n%s", r.Code, r.Stdout)
		}
	})

	t.Run("missing root", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, filepath.Join(root, "missing"))
		if r.Code != 2 || !strings.Contains(r.Stderr, "cannot scan") {
			t.Errorf("code %d stderr %q", r.Code, r.Stderr)
		}
	})

	t.Run("bad flag", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--min-size", "huge")
		if r.Code != 2 || !strings.Contains(r.Stderr, "--help") {
			t.Errorf("code %d stderr %q", r.Code, r.Stderr)
		}
	})
}

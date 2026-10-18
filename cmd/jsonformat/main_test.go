package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/testutil"
)

func TestBinary(t *testing.T) {
	bin := testutil.BuildBinary(t, ".", "jsonformat")
	samples := filepath.Join("..", "..", "docs", "samples")
	data := filepath.Join(samples, "data.json")
	broken := filepath.Join(samples, "broken.json")

	t.Run("pretty sample preserves precision and unicode", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, data)
		if r.Code != 0 || r.Stderr != "" {
			t.Fatalf("code %d stderr %s", r.Code, r.Stderr)
		}
		for _, s := range []string{`"downloads": 12345678901234567890,`, `"precision": 1.000000000000000000001,`,
			`"greeting": "สวัสดี, world <3",`, `?lang=go&sort=stars`, `"changelog": [],`, `"flags": {}`} {
			if !strings.Contains(r.Stdout, s) {
				t.Errorf("missing %q", s)
			}
		}
		if strings.Contains(r.Stdout, "\x1b") {
			t.Error("color with NO_COLOR")
		}
	})

	t.Run("broken sample shows caret", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, broken)
		if r.Code != 1 || r.Stdout != "" {
			t.Fatalf("code %d stdout %q", r.Code, r.Stdout)
		}
		want := broken + ":8:5: error: expected ',' or '}' after object value, found '\"'\n" +
			"    6 |     \"user\": \"billing\",\n" +
			"    7 |     \"pool_size\": 20\n" +
			"    8 |     \"timeout\": \"5s\"\n" +
			"      |     ^\n" +
			"  hint: is a ',' missing at the end of line 7?\n"
		if r.Stderr != want {
			t.Errorf("stderr:\n%s\nwant:\n%s", r.Stderr, want)
		}
	})

	t.Run("stdin query raw", func(t *testing.T) {
		in, err := os.Open(data)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		r := testutil.RunBinary(t, bin, in, "--query", "tools[0].tags[-1]", "-r")
		if r.Code != 0 || r.Stdout != "validate\n" {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("query missing path", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--query", "maintainers[5]", data)
		if r.Code != 1 || !strings.Contains(r.Stderr, "path maintainers[5]: index out of range (len 2)") {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("check json report", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--check", "--json", data, broken)
		if r.Code != 1 {
			t.Fatalf("%+v", r)
		}
		var rep struct {
			Files []struct {
				Valid bool `json:"valid"`
				Error *struct {
					Line, Col int
				} `json:"error"`
			} `json:"files"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
			t.Fatal(err)
		}
		if len(rep.Files) != 2 || !rep.Files[0].Valid || rep.Files[1].Valid || rep.Files[1].Error.Line != 8 || rep.Files[1].Error.Col != 5 {
			t.Fatalf("%s", r.Stdout)
		}
	})

	t.Run("write in place", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "x.json")
		if err := os.WriteFile(f, []byte(`{"b":2,"a":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
		r := testutil.RunBinary(t, bin, nil, "-w", "--sort-keys", "--tab", f)
		if r.Code != 0 {
			t.Fatalf("%+v", r)
		}
		got, _ := os.ReadFile(f)
		if string(got) != "{\n\t\"a\": 1,\n\t\"b\": 2\n}\n" {
			t.Fatalf("%q", got)
		}
		if r := testutil.RunBinary(t, bin, strings.NewReader("{}"), "-w"); r.Code != 2 {
			t.Fatalf("-w with stdin: %+v", r)
		}
	})
}

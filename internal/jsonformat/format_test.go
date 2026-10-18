package jsonformat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

func format(t *testing.T, in string, opt Options) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Format(context.Background(), strings.NewReader(in), &out, opt)
	return out.String(), err
}

func TestFormat(t *testing.T) {
	two := Options{Indent: "  "}
	tests := []struct {
		name string
		in   string
		opt  Options
		want string
	}{
		{"pretty object", `{"a":1,"b":[true,false,null],"c":{"d":"e"}}`, two,
			"{\n  \"a\": 1,\n  \"b\": [\n    true,\n    false,\n    null\n  ],\n  \"c\": {\n    \"d\": \"e\"\n  }\n}\n"},
		{"minify", "{\n  \"a\" : [ 1 , 2 ] ,\n  \"b\" : { }\n}", Options{}, `{"a":[1,2],"b":{}}` + "\n"},
		{"indent 4", `{"a":[1]}`, Options{Indent: "    "}, "{\n    \"a\": [\n        1\n    ]\n}\n"},
		{"indent 1", `[1,[2]]`, Options{Indent: " "}, "[\n 1,\n [\n  2\n ]\n]\n"},
		{"tab", `{"a":{"b":1}}`, Options{Indent: "\t"}, "{\n\t\"a\": {\n\t\t\"b\": 1\n\t}\n}\n"},
		{"empty containers stay compact", `{"o":{},"a":[],"n":[{},[]]}`, two,
			"{\n  \"o\": {},\n  \"a\": [],\n  \"n\": [\n    {},\n    []\n  ]\n}\n"},
		{"top-level empties", `{} []`, two, "{}\n[]\n"},
		{"number precision", `[12345678901234567890, 1.000000000000000000001, -0.0, 1E+400, 2e-7]`, Options{},
			"[12345678901234567890,1.000000000000000000001,-0.0,1E+400,2e-7]\n"},
		{"unicode kept literal", `{"greeting":"สวัสดี","emoji":"😀","esc":"éก"}`, Options{},
			`{"greeting":"สวัสดี","emoji":"😀","esc":"éก"}` + "\n"},
		{"html chars not escaped", `{"url":"https://x.test/?a=1&b=<2>","esc":"<&>"}`, Options{},
			`{"url":"https://x.test/?a=1&b=<2>","esc":"<&>"}` + "\n"},
		{"escapes re-encoded", `["\"\\\/\b\f\n\r\t\u0001\u007f  "]`, Options{},
			`["\"\\/\b\f\n\r\t\u0001` + "\x7f" + `  "]` + "\n"},
		{"invalid utf8 replaced", "[\"a\xffb\"]", Options{}, `["a�b"]` + "\n"},
		{"ndjson pretty", "{\"a\":1}\n{\"a\":2}\n", two, "{\n  \"a\": 1\n}\n{\n  \"a\": 2\n}\n"},
		{"ndjson minify", "{\"a\": 1}\n\n[ 2 ]\n\"s\" 3 true null", Options{}, "{\"a\":1}\n[2]\n\"s\"\n3\ntrue\nnull\n"},
		{"concatenated without whitespace", `{"a":1}{"b":2}[3]`, Options{}, "{\"a\":1}\n{\"b\":2}\n[3]\n"},
		{"scalar", ` 42 `, two, "42\n"},
		{"bom skipped", "\xEF\xBB\xBF{\"a\":1}", Options{}, "{\"a\":1}\n"},
		{"sort keys", `{"b":1,"a":{"z":1,"y":[{"d":1,"c":2}]},"A":0}`, Options{SortKeys: true},
			`{"A":0,"a":{"y":[{"c":2,"d":1}],"z":1},"b":1}` + "\n"},
		{"sort keys pretty", `{"b":[],"a":{}}`, Options{Indent: "  ", SortKeys: true}, "{\n  \"a\": {},\n  \"b\": []\n}\n"},
		{"sort keys stable duplicates", `{"b":1,"a":1,"b":2}`, Options{SortKeys: true}, `{"a":1,"b":1,"b":2}` + "\n"},
		{"sort keys ndjson", "{\"b\":1,\"a\":2}\n{\"d\":1,\"c\":2}", Options{SortKeys: true}, "{\"a\":2,\"b\":1}\n{\"c\":2,\"d\":1}\n"},
		{"sort keys preserves numbers", `{"n":12345678901234567890.5}`, Options{SortKeys: true}, `{"n":12345678901234567890.5}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := format(t, tt.in, tt.opt)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			// the same output must come out when reading one byte at a time
			var out bytes.Buffer
			if err := Format(context.Background(), iotest.OneByteReader(strings.NewReader(tt.in)), &out, tt.opt); err != nil || out.String() != tt.want {
				t.Errorf("one-byte reads: %v %q", err, out.String())
			}
		})
	}
}

func TestFormatIsIdempotent(t *testing.T) {
	in := `{"a":[1,{"b":"<x>"},[]],"c":{},"d":"สวัสดี"}`
	for _, ind := range []string{"", " ", "  ", "\t"} {
		once, err := format(t, in, Options{Indent: ind})
		if err != nil {
			t.Fatal(err)
		}
		twice, err := format(t, once, Options{Indent: ind})
		if err != nil || once != twice {
			t.Errorf("indent %q: not idempotent:\n%s\n%s", ind, once, twice)
		}
	}
}

func TestFormatQuery(t *testing.T) {
	in := `{"user":{"name":"Ada <dev>","tags":["a","b"],"n":12345678901234567890},"items":[{"id":1},{"id":2}]}`
	tests := []struct {
		query string
		raw   bool
		want  string
	}{
		{"user.name", false, "\"Ada <dev>\"\n"},
		{"user.name", true, "Ada <dev>\n"},
		{".user.tags", false, "[\n  \"a\",\n  \"b\"\n]\n"},
		{"user.tags", true, "[\n  \"a\",\n  \"b\"\n]\n"},
		{"user.n", true, "12345678901234567890\n"},
		{"items[-1].id", false, "2\n"},
		{`["items"][0]`, false, "{\n  \"id\": 1\n}\n"},
		{".", false, "{\n  \"user\": {\n    \"name\": \"Ada <dev>\",\n    \"tags\": [\n      \"a\",\n      \"b\"\n    ],\n    \"n\": 12345678901234567890\n  },\n  \"items\": [\n    {\n      \"id\": 1\n    },\n    {\n      \"id\": 2\n    }\n  ]\n}\n"},
	}
	for _, tt := range tests {
		p, err := ParsePath(tt.query)
		if err != nil {
			t.Fatal(err)
		}
		got, err := format(t, in, Options{Indent: "  ", Query: p, Raw: tt.raw})
		if err != nil || got != tt.want {
			t.Errorf("%s raw=%v: got %q, %v; want %q", tt.query, tt.raw, got, err, tt.want)
		}
	}
	// query applies to each NDJSON value; raw output of escapes is literal
	p, _ := ParsePath("v")
	got, err := format(t, "{\"v\":\"a\\nb\"}\n{\"v\":\"c\"}", Options{Query: p, Raw: true})
	if err != nil || got != "a\nb\nc\n" {
		t.Errorf("ndjson query: %q %v", got, err)
	}
	// a missing path is a QueryError, earlier values are still printed
	got, err = format(t, "{\"v\":1}\n{\"w\":2}", Options{Query: p})
	var qe *QueryError
	if !errors.As(err, &qe) || got != "1\n" {
		t.Errorf("missing path: %q %v", got, err)
	}
}

func TestFormatErrorDiscardsPartialValue(t *testing.T) {
	got, err := format(t, "{\"ok\":1}\n{\"a\":[1,2 3]}", Options{Indent: "  "})
	asSyntax(t, err)
	if got != "{\n  \"ok\": 1\n}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatHugeInvalidValueSpills(t *testing.T) {
	// Larger than holdLimit: the partial output has already been written and
	// must be terminated with a newline.
	n := holdLimit/2 + 10
	in := "[" + strings.Repeat("1,", n) + "x]"
	got, err := format(t, in, Options{})
	asSyntax(t, err)
	if !strings.HasPrefix(got, "[1,1,") || !strings.HasSuffix(got, "1\n") {
		t.Fatalf("unexpected partial output (%d bytes) ending %q", len(got), got[max(0, len(got)-10):])
	}
}

func TestFormatLargeStreams(t *testing.T) {
	// More than one flush worth of output, split across many values.
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&b, "{\"i\":%d,\"s\":\"%s\"}\n", i, strings.Repeat("x", 10))
	}
	got, err := format(t, b.String(), Options{})
	if err != nil || got != b.String() {
		t.Fatalf("round trip failed: %v (len %d vs %d)", err, len(got), b.Len())
	}
}

type errWriter struct{ n int }

func (w *errWriter) Write(p []byte) (int, error) {
	w.n++
	return 0, errors.New("disk full")
}

func TestFormatWriteError(t *testing.T) {
	err := Format(context.Background(), strings.NewReader(`[1]`), &errWriter{}, Options{})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("got %v", err)
	}
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestHighlight(t *testing.T) {
	in := `{"str":"v","num":-1.5e3,"t":true,"f":false,"n":null,"arr":[{}],"e":[]}`
	for _, opt := range []Options{{Indent: "  "}, {}, {SortKeys: true, Indent: "\t"}} {
		plain, err := format(t, in, opt)
		if err != nil {
			t.Fatal(err)
		}
		opt.Theme = ui.NewTheme(true)
		colored, err := format(t, in, opt)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(colored, "\x1b[") {
			t.Fatalf("no ANSI codes in %q", colored)
		}
		if stripped := ansiRE.ReplaceAllString(colored, ""); stripped != plain {
			t.Errorf("stripped colored output differs:\n%q\n%q", stripped, plain)
		}
		// every role is painted with its own color
		th := ui.NewTheme(true)
		for _, want := range []string{
			th.Paint(ui.RoleKey, `"str"`), th.Paint(ui.RoleString, `"v"`), th.Paint(ui.RoleNumber, "-1.5e3"),
			th.Paint(ui.RoleBool, "true"), th.Paint(ui.RoleBool, "false"), th.Paint(ui.RoleNull, "null"),
			th.Paint(ui.RolePunct, "{"),
		} {
			if !strings.Contains(colored, want) {
				t.Errorf("missing %q in %q", want, colored)
			}
		}
	}
	// a colorless theme is plain
	got, _ := format(t, in, Options{Theme: ui.NewTheme(false)})
	if strings.Contains(got, "\x1b") {
		t.Fatal("colorless theme produced ANSI")
	}
}

func TestAppendQuoted(t *testing.T) {
	tests := map[string]string{
		"":             `""`,
		"a\"b\\c":      `"a\"b\\c"`,
		"\x00\x1f":     `"\u0000\u001f"`,
		"<>&":          `"<>&"`,
		"é ":           `"é "`,
		"\xc3":         `"�"`,
		"ok\xe0\x80ok": `"ok��ok"`,
	}
	for in, want := range tests {
		if got := string(appendQuoted(nil, in)); got != want {
			t.Errorf("%q: got %s want %s", in, got, want)
		}
		if got := string(appendQuoted(nil, []byte(in))); got != want {
			t.Errorf("%q bytes: got %s want %s", in, got, want)
		}
	}
}

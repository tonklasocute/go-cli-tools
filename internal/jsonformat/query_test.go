package jsonformat

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		in    string
		want  Path
		canon string
	}{
		{"", Path{}, "."},
		{".", Path{}, "."},
		{"a", Path{{Key: "a"}}, "a"},
		{".a.b", Path{{Key: "a"}, {Key: "b"}}, "a.b"},
		{"a.b[0].c", Path{{Key: "a"}, {Key: "b"}, {Index: 0, IsIndex: true}, {Key: "c"}}, "a.b[0].c"},
		{"[2][-1]", Path{{Index: 2, IsIndex: true}, {Index: -1, IsIndex: true}}, "[2][-1]"},
		{".[0]", Path{{Index: 0, IsIndex: true}}, "[0]"},
		{"a.[1]", Path{{Key: "a"}, {Index: 1, IsIndex: true}}, "a[1]"},
		{`["key.with.dots"]`, Path{{Key: "key.with.dots"}}, `["key.with.dots"]`},
		{`a["b c"].d`, Path{{Key: "a"}, {Key: "b c"}, {Key: "d"}}, `a["b c"].d`},
		{`["q\"uote"]`, Path{{Key: `q"uote`}}, `["q\"uote"]`},
		{`[""]`, Path{{Key: ""}}, `[""]`},
		{"ชื่อ.ไทย", Path{{Key: "ชื่อ"}, {Key: "ไทย"}}, "ชื่อ.ไทย"},
		{"[ 3 ]", Path{{Index: 3, IsIndex: true}}, "[3]"},
	}
	for _, tt := range tests {
		got, err := ParsePath(tt.in)
		if err != nil {
			t.Errorf("%q: %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q: got %#v want %#v", tt.in, got, tt.want)
		}
		if s := got.String(); s != tt.canon {
			t.Errorf("%q: String() = %q want %q", tt.in, s, tt.canon)
		}
	}
}

func TestParsePathErrors(t *testing.T) {
	tests := map[string]string{
		"a..b":                   "empty key",
		"..":                     "empty key",
		"a.":                     "trailing '.'",
		"a[":                     "missing ']'",
		"a[x]":                   "is not an integer",
		"a[1]b":                  "expected '.' or '['",
		`a["b`:                   "unterminated quoted key",
		`a["b"`:                  "expected ']' after quoted key",
		`a["\q"]`:                "bad quoted key",
		"a]":                     "unexpected ']'",
		`a"b`:                    "unexpected '\"'",
		"[1.5]":                  "is not an integer",
		"[99999999999999999999]": "is not an integer",
	}
	for in, want := range tests {
		_, err := ParsePath(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want error containing %q", in, err, want)
		}
	}
}

func readTree(t *testing.T, in string) *Value {
	t.Helper()
	v, err := ReadValue(NewScanner(context.Background(), strings.NewReader(in)))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLookup(t *testing.T) {
	doc := readTree(t, `{"a":{"b":[{"c":"deep"},{"c":2}],"s":"str","n":null,"e":{}},"dup":1,"dup":2,"x.y":true}`)
	tests := []struct {
		path string
		kind Kind
		text string
		err  string
	}{
		{"a.b[0].c", String, "deep", ""},
		{"a.b[-1].c", Number, "2", ""},
		{"a.b[-2].c", String, "deep", ""},
		{"dup", Number, "2", ""},
		{`["x.y"]`, True, "", ""},
		{".", BeginObject, "", ""},
		{"a.b[3]", 0, "", "path a.b[3]: index out of range (len 2)"},
		{"a.b[-3]", 0, "", "path a.b[-3]: index out of range (len 2)"},
		{"a.zz", 0, "", `path a.zz: key not found (available: b, s, n, e)`},
		{"a.e.k", 0, "", "path a.e.k: key not found (object is empty)"},
		{"a.s[0]", 0, "", "path a.s[0]: cannot index a string with [0]"},
		{"a.b.c", 0, "", `path a.b.c: cannot look up key "c" in an array`},
		{"a.n.x", 0, "", `path a.n.x: cannot look up key "x" in null`},
		{"a.b[0].c.d", 0, "", `in a string`},
		{"dup.x", 0, "", "in a number"},
		{`["x.y"].z`, 0, "", `path ["x.y"].z: cannot look up key "z" in a boolean`},
	}
	for _, tt := range tests {
		p, err := ParsePath(tt.path)
		if err != nil {
			t.Fatal(err)
		}
		v, err := Lookup(doc, p)
		if tt.err != "" {
			var qe *QueryError
			if !errors.As(err, &qe) || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%s: got %v, want %q", tt.path, err, tt.err)
			}
			continue
		}
		if err != nil || v.Kind != tt.kind || v.Text != tt.text {
			t.Errorf("%s: got %+v %v", tt.path, v, err)
		}
	}
}

func TestAvailableKeysTruncated(t *testing.T) {
	doc := readTree(t, `{"k1":1,"k2":2,"k3":3,"k4":4,"k5":5,"k6":6,"k7":7,"k8":8,"k9":9,"a b":0}`)
	_, err := Lookup(doc, Path{{Key: "nope"}})
	if err == nil || !strings.Contains(err.Error(), "k8, …)") {
		t.Fatalf("got %v", err)
	}
	doc = readTree(t, `{"a b":0}`)
	_, err = Lookup(doc, Path{{Key: "nope"}})
	if err == nil || !strings.Contains(err.Error(), `(available: ["a b"])`) {
		t.Fatalf("got %v", err)
	}
}

func TestReadValueErrors(t *testing.T) {
	for _, in := range []string{`{"a":`, `[1,`, `{"a":[}`} {
		_, err := ReadValue(NewScanner(context.Background(), strings.NewReader(in)))
		asSyntax(t, err)
	}
}

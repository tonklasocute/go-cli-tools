package envcheck

import (
	"reflect"
	"strings"
	"testing"
)

type kv struct {
	key, val string
	line     int
}

func entriesOf(f *File) []kv {
	var out []kv
	for _, e := range f.Entries {
		out = append(out, kv{e.Key, e.Value, e.Line})
	}
	return out
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []kv
		errs    []int // lines expected to be malformed
		comment string
	}{
		{name: "simple", in: "A=1\nB=two", want: []kv{{"A", "1", 1}, {"B", "two", 2}}},
		{name: "empty value", in: "A=\nB=   \n", want: []kv{{"A", "", 1}, {"B", "", 2}}},
		{name: "spaces around equals", in: "  A  =  hello world  \n", want: []kv{{"A", "hello world", 1}}},
		{name: "export prefix", in: "export A=1\nexport\tB=2", want: []kv{{"A", "1", 1}, {"B", "2", 2}}},
		{name: "key named export", in: "export=1\nexported=2", want: []kv{{"export", "1", 1}, {"exported", "2", 2}}},
		{name: "full-line and indented comments", in: "# c\n   # d\nA=1\n\n", want: []kv{{"A", "1", 3}}},
		{name: "inline comment", in: "A=1 # one\nB=x\t#tab", want: []kv{{"A", "1", 1}, {"B", "x", 2}}, comment: "one"},
		{name: "hash without space is value", in: "A=a#b\nB=#nocomment", want: []kv{{"A", "a#b", 1}, {"B", "#nocomment", 2}}},
		{name: "comment only after equals", in: "A=   # just a comment", want: []kv{{"A", "", 1}}, comment: "just a comment"},
		{name: "single quotes literal", in: `A='x \n $HOME "q" # not comment'`, want: []kv{{"A", `x \n $HOME "q" # not comment`, 1}}},
		{name: "double quote escapes", in: `A="a\nb\tc\rd\"e\\f\$g\qh"`, want: []kv{{"A", "a\nb\tc\rd\"e\\f$g\\qh", 1}}},
		{name: "backticks", in: "A=`it's \"fine\"`", want: []kv{{"A", `it's "fine"`, 1}}},
		{name: "quoted with comment", in: `A="v" # c`, want: []kv{{"A", "v", 1}}, comment: "c"},
		{name: "quoted comment no space", in: `A="v"#c`, want: []kv{{"A", "v", 1}}, comment: "c"},
		{name: "empty quotes", in: `A=""` + "\nB=''", want: []kv{{"A", "", 1}, {"B", "", 2}}},
		{
			name: "multiline double", in: "A=\"line1\nline2\nline3\"\nB=2",
			want: []kv{{"A", "line1\nline2\nline3", 1}, {"B", "2", 4}},
		},
		{
			name: "multiline single", in: "KEY='-----BEGIN-----\r\nabc\r\n-----END-----'\r\nNEXT=1\r\n",
			want: []kv{{"KEY", "-----BEGIN-----\nabc\n-----END-----", 1}, {"NEXT", "1", 4}},
		},
		{name: "crlf", in: "A=1\r\nB=2 # c\r\n", want: []kv{{"A", "1", 1}, {"B", "2", 2}}},
		{name: "lone cr trimmed", in: "A=1\r", want: []kv{{"A", "1", 1}}},
		{name: "bom", in: "\xef\xbb\xbfA=1\n", want: []kv{{"A", "1", 1}}},
		{name: "dotted key", in: "app.name=x\n_UNDER=1", want: []kv{{"app.name", "x", 1}, {"_UNDER", "1", 2}}},
		{name: "duplicates kept", in: "A=1\nA=2", want: []kv{{"A", "1", 1}, {"A", "2", 2}}},
		{name: "no equals", in: "JUSTAKEY\nA=1", want: []kv{{"A", "1", 2}}, errs: []int{1}},
		{name: "garbage", in: "this is not valid\n", errs: []int{1}},
		{name: "invalid key chars", in: "MY-KEY=1\n1ABC=2\n.A=3", errs: []int{1, 2, 3}},
		{name: "missing key", in: "=value", errs: []int{1}},
		{name: "space in key", in: "MY KEY=1", errs: []int{1}},
		{name: "export only", in: "export", errs: []int{1}},
		{name: "text after quote", in: `A="x" y`, errs: []int{1}},
		{
			name: "unterminated double resumes next line", in: "A=\"open\nB=2\nC=3",
			want: []kv{{"B", "2", 2}, {"C", "3", 3}}, errs: []int{1},
		},
		{name: "unterminated single", in: "X=1\nA='open", want: []kv{{"X", "1", 1}}, errs: []int{2}},
		{name: "trailing backslash in double", in: "A=\"a\\\nb\"", want: []kv{{"A", "a\\\nb", 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(tt.in))
			if got := entriesOf(f); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("entries = %#v, want %#v", got, tt.want)
			}
			var lines []int
			for _, e := range f.Errors {
				lines = append(lines, e.Line)
			}
			if !reflect.DeepEqual(lines, tt.errs) {
				t.Errorf("error lines = %v, want %v (%v)", lines, tt.errs, f.Errors)
			}
			if tt.comment != "" {
				found := false
				for _, e := range f.Entries {
					found = found || e.Comment == tt.comment
				}
				if !found {
					t.Errorf("no entry with comment %q: %+v", tt.comment, f.Entries)
				}
			}
		})
	}
}

func TestParseDetails(t *testing.T) {
	f := Parse([]byte("# top\nexport A=\"x\ny\" # c\n#  indented text  \nA=2\n"))
	if len(f.Comments) != 2 || f.Comments[0] != (Comment{Line: 1, Text: " top"}) || f.Comments[1].Text != "  indented text" {
		t.Fatalf("comments = %+v", f.Comments)
	}
	e := f.Entries[0]
	if !e.Exported || e.Line != 2 || e.EndLine != 3 || e.Comment != "c" {
		t.Fatalf("entry = %+v", e)
	}
	got, lines, ok := f.Lookup("A")
	if !ok || got.Value != "2" || !reflect.DeepEqual(lines, []int{2, 5}) {
		t.Fatalf("Lookup = %+v %v %v", got, lines, ok)
	}
	if _, _, ok := f.Lookup("MISSING"); ok {
		t.Fatal("Lookup of missing key reported ok")
	}
}

func TestParseErrorMessagesDoNotLeakValues(t *testing.T) {
	f := Parse([]byte("not a line hunter3\nBAD-KEY=hunter4\nA=\"x\" hunter5\nTOKEN=\"hunter2-very-secret"))
	if len(f.Errors) != 4 {
		t.Fatalf("errors = %v", f.Errors)
	}
	for _, e := range f.Errors {
		if strings.Contains(e.Error(), "hunter") {
			t.Errorf("error leaks value: %v", e)
		}
		if !strings.HasPrefix(e.Error(), "line ") {
			t.Errorf("error should start with line: %v", e)
		}
	}
	if !strings.Contains(f.Errors[3].Msg, "unterminated double-quoted value for TOKEN") || f.Errors[3].Line != 4 {
		t.Errorf("err = %+v", f.Errors[3])
	}
	if !strings.Contains(f.Errors[1].Msg, `invalid key "BAD-KEY"`) {
		t.Errorf("msg = %q", f.Errors[1].Msg)
	}
}

func TestValidKeyAndShorten(t *testing.T) {
	for k, want := range map[string]bool{"A": true, "_": true, "a1.b_2": true, "": false, "1A": false, "A-B": false, "ä": false} {
		if got := validKey(k); got != want {
			t.Errorf("validKey(%q) = %v", k, got)
		}
	}
	if got := shorten("abcdef", 4); got != "abc…" {
		t.Errorf("shorten = %q", got)
	}
	if got := shorten("abc", 4); got != "abc" {
		t.Errorf("shorten = %q", got)
	}
}

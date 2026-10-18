package jsonformat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

func validate(t *testing.T, input string, oneByte bool) error {
	t.Helper()
	var r io.Reader = strings.NewReader(input)
	if oneByte {
		r = iotest.OneByteReader(r)
	}
	return Validate(context.Background(), r)
}

func asSyntax(t *testing.T, err error) *SyntaxError {
	t.Helper()
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("want *SyntaxError, got %T %v", err, err)
	}
	return se
}

func TestSyntaxErrorPositions(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		line, col int
		offset    int64 // -1: don't check
		msg       string
		hint      string
	}{
		{"missing comma", "{\n  \"a\": 1\n  \"b\": 2\n}", 3, 3, 13, `expected ',' or '}' after object value, found '"'`, "end of line 2"},
		{"missing comma same line", `{"a": 1 "b": 2}`, 1, 9, 8, `expected ',' or '}' after object value`, "missing at 1:8"},
		{"missing comma array", `[1 2]`, 1, 4, 3, `expected ',' or ']' after array element, found '2'`, "missing"},
		{"trailing comma object", `{"a": 1,}`, 1, 8, 7, "trailing comma before '}'", "trailing commas"},
		{"trailing comma array multiline", "[\n  1,\n  2,\n]", 3, 4, 10, "trailing comma before ']'", ""},
		{"unclosed object", "{\n  \"a\": 1\n", 2, 9, 10, "unexpected end of input: unclosed object opened at 1:1", ""},
		{"unclosed nested", "{\"a\": [1, 2", 1, 12, 11, "unclosed array opened at 1:7 (1 more enclosing container still open)", ""},
		{"unclosed after key", `{"a"`, 1, 5, 4, "unclosed object opened at 1:1", ""},
		{"unclosed after comma", "[1,\n", 1, 4, 3, "unclosed array", ""},
		{"empty input", "", 1, 1, 0, "unexpected end of input: expected a JSON value", ""},
		{"whitespace input", "  \n\t ", 1, 1, 0, "expected a JSON value", ""},
		{"bad literal prefix", `[tru]`, 1, 2, 1, "invalid literal 'tru' (did you mean 'true'?)", ""},
		{"bad literal case", `{"a": True}`, 1, 7, 6, "JSON literals are lowercase (true)", ""},
		{"bad literal word", `[nulls]`, 1, 2, 1, "unexpected 'nulls'", ""},
		{"NaN", `[NaN]`, 1, 2, 1, "'NaN' is not valid JSON", ""},
		{"undefined", `{"a": undefined}`, 1, 7, 6, "use null", ""},
		{"invalid escape", `"ab\x"`, 1, 4, 3, `invalid escape sequence '\x' in string`, "valid escapes"},
		{"bad unicode escape", `"\u12G4"`, 1, 2, 1, `invalid \u escape`, ""},
		{"short unicode escape at EOF", `"\u12`, 1, 2, 1, `invalid \u escape`, ""},
		{"escape at EOF", `"abc\`, 1, 6, 5, "unterminated string starting at 1:1", ""},
		{"error on first char", `}`, 1, 1, 0, "unexpected '}' with no open object or array", ""},
		{"word at start", `hello`, 1, 1, 0, "unexpected 'hello'", ""},
		{"error after tabs", "{\n\t\t\"a\" 1}", 2, 7, 8, "expected ':' after object key, found '1'", ""},
		{"crlf", "{\r\n  \"a\": 1\r\n  \"b\": 2\r\n}", 3, 3, 15, "expected ',' or '}'", ""},
		{"multibyte before error", `{"ชื่อ": 1 "x": 2}`, 1, 12, 19, "expected ',' or '}'", ""},
		{"emoji before error", `["😀" 1]`, 1, 6, 8, "expected ',' or ']'", ""},
		{"unterminated string EOF", `{"a": "abc`, 1, 11, 10, "unterminated string starting at 1:7", ""},
		{"newline in string", "{\"a\": \"abc\n}", 1, 11, 10, "missing closing '\"' before end of line", "starts at 1:7"},
		{"crlf in string", "\"abc\r\n\"", 1, 5, 4, "missing closing", ""},
		{"control char", "\"a\x01\"", 1, 3, 2, `invalid control character U+0001 in string (escape it as \u0001)`, ""},
		{"raw tab in string", "\"a\tb\"", 1, 3, 2, `escape it as \t`, ""},
		{"leading zero", `[01]`, 1, 2, 1, "leading zeros are not allowed", ""},
		{"negative leading zero", `[-01]`, 1, 2, 1, "leading zeros", ""},
		{"minus alone", `[-]`, 1, 3, 2, "expected a digit after '-'", ""},
		{"minus at EOF", `-`, 1, 2, 1, "expected a digit after '-'", ""},
		{"dot no digit", `[1.]`, 1, 4, 3, "expected a digit after '.'", ""},
		{"exponent no digit", `[1e+]`, 1, 5, 4, "expected a digit in exponent", ""},
		{"hex number", `[0x1F]`, 1, 2, 1, "invalid number '0x1F'", ""},
		{"double dot number", `[1.5.3]`, 1, 2, 1, "invalid number '1.5.3'", ""},
		{"plus number", `+1`, 1, 1, 0, "numbers cannot start with '+'", ""},
		{"dot number", `[.5]`, 1, 2, 1, "expected a digit before '.'", ""},
		{"single quoted key", `{'a': 1}`, 1, 2, 1, "object keys must be enclosed in double quotes", ""},
		{"single quoted value", `['a']`, 1, 2, 1, "strings must be enclosed in double quotes", ""},
		{"unquoted key", `{name: 1}`, 1, 2, 1, "object keys must be double-quoted strings, found 'name'", `write it as "name"`},
		{"number key", `{1: 1}`, 1, 2, 1, "expected a string key or '}', found '1'", ""},
		{"key after comma", `{"a":1, 2}`, 1, 9, 8, "expected a string key after ','", ""},
		{"comment", "{// note\n}", 1, 2, 1, "comments are not allowed in JSON", ""},
		{"block comment value", `[/* x */ 1]`, 1, 2, 1, "comments are not allowed", ""},
		{"mismatched array", `[1}`, 1, 3, 2, "mismatched '}': array opened at 1:1 must be closed with ']'", ""},
		{"mismatched object", `{"a":1]`, 1, 7, 6, "mismatched ']': object opened at 1:1 must be closed with '}'", ""},
		{"mismatched empty object", `{]`, 1, 2, 1, "mismatched ']'", ""},
		{"missing colon", `{"a" 1}`, 1, 6, 5, "expected ':' after object key", ""},
		{"missing value", `{"a": }`, 1, 7, 6, "expected a value after ':', found '}'", ""},
		{"double comma", `[1,,2]`, 1, 4, 3, "expected a value after ',', found ','", ""},
		{"array close after comma in object", `[1,}`, 1, 4, 3, "expected a value after ','", ""},
		{"empty array bad", `[:]`, 1, 2, 1, "expected a value or ']'", ""},
		{"comma after top-level", `{} ,`, 1, 4, 3, "unexpected ',' after top-level value", ""},
		{"invalid utf8 outside string", "[\xff]", 1, 2, 1, "invalid UTF-8 byte 0xFF", ""},
		{"control char outside string", "[\x01]", 1, 2, 1, "control character U+0001", ""},
		{"bom then error", "\xEF\xBB\xBF{\"a\" 1}", 1, 6, 8, "expected ':'", ""},
		{"stray bom", "[\xEF\xBB\xBF]", 1, 2, 1, "byte order mark", ""},
		{"thai found char", `[1 ก]`, 1, 4, 3, "found 'ก'", ""},
	}
	for _, tt := range tests {
		for _, oneByte := range []bool{false, true} {
			name := tt.name
			if oneByte {
				name += "/one-byte-reads"
			}
			t.Run(name, func(t *testing.T) {
				se := asSyntax(t, validate(t, tt.in, oneByte))
				if se.Pos.Line != tt.line || se.Pos.Col != tt.col {
					t.Errorf("pos = %d:%d, want %d:%d (%s)", se.Pos.Line, se.Pos.Col, tt.line, tt.col, se.Msg)
				}
				if tt.offset >= 0 && se.Pos.Offset != tt.offset {
					t.Errorf("offset = %d, want %d", se.Pos.Offset, tt.offset)
				}
				if !strings.Contains(se.Msg, tt.msg) {
					t.Errorf("msg = %q, want it to contain %q", se.Msg, tt.msg)
				}
				if !strings.Contains(se.Hint, tt.hint) {
					t.Errorf("hint = %q, want it to contain %q", se.Hint, tt.hint)
				}
				if !strings.HasPrefix(se.Error(), se.Pos.String()+": ") {
					t.Errorf("Error() = %q", se.Error())
				}
			})
		}
	}
}

func TestNestingTooDeep(t *testing.T) {
	se := asSyntax(t, validate(t, strings.Repeat("[", MaxDepth+1), false))
	if se.Pos.Col != MaxDepth+1 || !strings.Contains(se.Msg, "nesting too deep") {
		t.Fatalf("got %v", se)
	}
	if err := validate(t, strings.Repeat("[", MaxDepth)+strings.Repeat("]", MaxDepth), false); err != nil {
		t.Fatalf("max depth should be accepted: %v", err)
	}
}

func TestSnippetRendering(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "missing comma with context",
			in:   "{\n  \"service\": \"api\",\n  \"port\": 8080\n  \"debug\": true\n}\n",
			want: "x:4:3: error: expected ',' or '}' after object value, found '\"'\n" +
				"    2 |   \"service\": \"api\",\n" +
				"    3 |   \"port\": 8080\n" +
				"    4 |   \"debug\": true\n" +
				"      |   ^\n" +
				"  hint: is a ',' missing at the end of line 3?\n",
		},
		{
			name: "tabs are reproduced in the caret line",
			in:   "{\n\t\t\"a\" 1}",
			want: "x:2:7: error: expected ':' after object key, found '1'\n" +
				"    1 | {\n" +
				"    2 | \t\t\"a\" 1}\n" +
				"      | \t\t    ^\n",
		},
		{
			name: "wide runes",
			in:   `{"名前": 1 "x": 2}`,
			want: "x:1:10: error: expected ',' or '}' after object value, found '\"'\n" +
				"    1 | {\"名前\": 1 \"x\": 2}\n" +
				"      |            ^\n" +
				"  hint: is a ',' missing at 1:9?\n",
		},
		{
			name: "eof points after the last token",
			in:   "[\n  1,\n  2\n\n",
			want: "x:3:4: error: unexpected end of input: unclosed array opened at 1:1\n" +
				"    1 | [\n" +
				"    2 |   1,\n" +
				"    3 |   2\n" +
				"      |    ^\n",
		},
		{
			name: "crlf lines are shown without CR",
			in:   "{\r\n\"a\": tru\r\n}",
			want: "x:2:6: error: invalid literal 'tru' (did you mean 'true'?)\n" +
				"    1 | {\n" +
				"    2 | \"a\": tru\n" +
				"      |      ^\n",
		},
		{
			name: "control characters are masked",
			in:   "\"a\x01b\"",
			want: "x:1:3: error: invalid control character U+0001 in string (escape it as \\u0001)\n" +
				"    1 | \"a\uFFFDb\"\n" +
				"      |   ^\n",
		},
	}
	th := ui.NewTheme(false)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			se := asSyntax(t, validate(t, tt.in, false))
			var b strings.Builder
			if err := se.Render(&b, th, se.Header("x", th)); err != nil {
				t.Fatal(err)
			}
			if b.String() != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", b.String(), tt.want)
			}
		})
	}
}

func TestSnippetLongLineIsWindowed(t *testing.T) {
	in := "[" + strings.Repeat("1,", 1000) + "1 2]"
	se := asSyntax(t, validate(t, in, false))
	wantCol := 1 + 2000 + 2 + 1 // '[' + "1,"*1000 + "1 " + the offending '2'
	if se.Pos.Line != 1 || se.Pos.Col != wantCol {
		t.Fatalf("pos = %v, want 1:%d", se.Pos, wantCol)
	}
	snip := se.Snippet(ui.NewTheme(false))
	lines := strings.Split(strings.TrimRight(snip, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %q", snip)
	}
	src, caret := lines[0], lines[1]
	if !strings.Contains(src, "…") {
		t.Errorf("long line not windowed: %q", src)
	}
	if w := ui.Width(src); w > maxLineCols+20 {
		t.Errorf("windowed line too wide (%d): %q", w, src)
	}
	// the caret sits under the '2'
	idx := ui.Width(caret) - 1
	if r := []rune(src)[idx]; r != '2' {
		t.Errorf("caret under %q, want '2'\n%s\n%s", r, src, caret)
	}
}

func TestErrorAfterBufferShifts(t *testing.T) {
	// Many lines so the scanner shifts its buffer several times before the
	// error; the context lines must still be available.
	var b strings.Builder
	b.WriteString("[\n")
	const n = 60000
	for i := 0; i < n; i++ {
		b.WriteString("  \"สวัสดี\",\n")
	}
	b.WriteString("  1 2\n]")
	for _, oneByte := range []bool{false, true} {
		se := asSyntax(t, validate(t, b.String(), oneByte))
		if se.Pos.Line != n+2 || se.Pos.Col != 5 {
			t.Fatalf("pos = %v, want %d:5", se.Pos, n+2)
		}
		snip := se.Snippet(ui.NewTheme(false))
		if !strings.Contains(snip, "\"สวัสดี\",") || !strings.Contains(snip, "  1 2") {
			t.Fatalf("missing context:\n%s", snip)
		}
	}
}

func TestErrorInHugeSingleLine(t *testing.T) {
	// A single line much longer than the buffer: the column must still be
	// exact and the snippet windowed around it.
	elems := 300000
	in := "[" + strings.Repeat("\"é\",", elems) + "x]"
	se := asSyntax(t, validate(t, in, false))
	wantCol := 1 + 4*elems + 1
	if se.Pos.Line != 1 || se.Pos.Col != wantCol {
		t.Fatalf("pos = %v, want 1:%d", se.Pos, wantCol)
	}
	if want := int64(1 + 5*elems); se.Pos.Offset != want {
		t.Fatalf("offset = %d, want %d", se.Pos.Offset, want)
	}
	snip := se.Snippet(ui.NewTheme(false))
	if !strings.Contains(snip, "…,\"é\"") || !strings.Contains(snip, "\"é\",x]") {
		t.Fatalf("bad snippet:\n%s", snip)
	}
}

func TestUnclosedContainerFarAway(t *testing.T) {
	in := "{\"items\": [\n" + strings.Repeat("  {\"id\": 1},\n", 50000) + "  {\"id\": 2}\n"
	se := asSyntax(t, validate(t, in, false))
	if !strings.Contains(se.Msg, "unclosed array opened at 1:11") {
		t.Fatalf("msg = %q", se.Msg)
	}
	if se.Pos.Line != 50002 || se.Pos.Col != 12 {
		t.Fatalf("pos = %v", se.Pos)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestScannerReadErrors(t *testing.T) {
	boom := errors.New("boom")
	inputs := []string{`{"a": "abc`, `[1, -`, `{"a": tr`, `"\`, `"\u00`, `[1`}
	for _, in := range inputs {
		r := io.MultiReader(strings.NewReader(in), failingReader{boom})
		if err := Validate(context.Background(), r); !errors.Is(err, boom) {
			t.Errorf("%q: got %v, want read error", in, err)
		}
	}
	// zero-byte reads forever are reported instead of spinning
	if err := Validate(context.Background(), zeroReader{}); !errors.Is(err, io.ErrNoProgress) {
		t.Errorf("zero reader: %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read([]byte) (int, error) { return 0, nil }

func TestScannerCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Validate(ctx, strings.NewReader(`{"a": 1}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestScannerTokens(t *testing.T) {
	sc := NewScanner(context.TODO(), strings.NewReader(`{"k": [1, "s\n", true, false, null]} 7`))
	var got []string
	for {
		tok, err := sc.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		s := string(tok.Text)
		if tok.Key {
			s = "key:" + s
		}
		got = append(got, s)
		if sc.Depth() < 0 {
			t.Fatal("negative depth")
		}
	}
	want := []string{"", "key:k", "", "1", "s\n", "", "", "", "", "", "7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if sc.Values() != 2 {
		t.Fatalf("values = %d", sc.Values())
	}
	// errors are sticky
	if _, err := sc.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("after EOF: %v", err)
	}
}

func TestStringDecoding(t *testing.T) {
	tests := map[string]string{
		`"plain"`:            "plain",
		`"\"\\\/\b\f\n\r\t"`: "\"\\/\b\f\n\r\t",
		`"\u00e9\u0E01"`:     "éก",
		`"\uD83D\uDE00"`:     "😀",
		`"\uD83D"`:           "\uFFFD",
		`"\uD83Dx"`:          "\uFFFDx",
		`"\uDE00"`:           "\uFFFD",
		`"\uD83D\u0041"`:     "\uFFFDA",
		`"สวัสดี"`:           "สวัสดี",
	}
	for in, want := range tests {
		for _, oneByte := range []bool{false, true} {
			var r io.Reader = strings.NewReader(in)
			if oneByte {
				r = iotest.OneByteReader(r)
			}
			sc := NewScanner(context.Background(), r)
			tok, err := sc.Next()
			if err != nil || string(tok.Text) != want {
				t.Errorf("%s: got %q, %v; want %q", in, tok.Text, err, want)
			}
		}
	}
}

// TestAgreesWithEncodingJSON checks validity decisions against
// encoding/json for single-value inputs.
func TestAgreesWithEncodingJSON(t *testing.T) {
	inputs := []string{
		`0`, `-0`, `-0.0e+0`, `1E9`, `1e-9`, `123.456`, `-`, `01`, `1.`, `.1`, `1e`, `1e+`, `--1`,
		`""`, `"\u0000"`, `"\uFFFF"`, `"\z"`, `"a`, "\"\x7f\"", "\"\xff\"",
		`[]`, `[[]]`, `[1,]`, `[,1]`, `[1 2]`, `{}`, `{"a":{}}`, `{"a"}`, `{"a":}`, `{,}`, `{"a":1,"a":2}`,
		`true`, `false`, `null`, `nul`, `True`, ` [ 1 , 2 ] `, "\t\r\n{}\n", `{"a":[{"b":[null]}]}`,
	}
	for _, in := range inputs {
		ours := validate(t, in, false) == nil
		if theirs := json.Valid([]byte(in)); ours != theirs {
			t.Errorf("%q: ours=%v encoding/json=%v", in, ours, theirs)
		}
	}
}

func FuzzFormatRoundTrip(f *testing.F) {
	for _, s := range []string{
		`{"a":[1,2,{"b":null}],"c":"\u00e9<&>"}`, `[1e10, -0.5, 12345678901234567890]`, `"\uD83D\uDE00"`,
		"{\"x\":\"\xff\"}", `{} [] "s" 1 true`, `{"a" 1}`, `[`, `"\u12`, "\xEF\xBB\xBF[1]",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ourErr := Validate(context.Background(), bytes.NewReader(data))
		hasBOM := bytes.HasPrefix(data, []byte("\xEF\xBB\xBF"))
		if json.Valid(data) && !hasBOM && ourErr != nil {
			t.Fatalf("encoding/json accepts %q but we reject: %v", data, ourErr)
		}
		if ourErr != nil {
			// rendering any error must not panic and must point at a real line
			var se *SyntaxError
			if errors.As(ourErr, &se) {
				th := ui.NewTheme(true)
				if err := se.Render(io.Discard, th, se.Header("f", th)); err != nil {
					t.Fatal(err)
				}
				if se.Pos.Line < 1 || se.Pos.Offset < 0 || se.Pos.Offset > int64(len(data)) {
					t.Fatalf("bad position %+v for %q", se.Pos, data)
				}
			}
			return
		}
		var out bytes.Buffer
		if err := Format(context.Background(), bytes.NewReader(data), &out, Options{Indent: "  "}); err != nil {
			t.Fatalf("validate ok but format failed: %v", err)
		}
		if hasBOM {
			data = data[3:]
		}
		a, errA := decodeAll(data)
		b, errB := decodeAll(out.Bytes())
		if errA != nil || errB != nil {
			// e.g. "truefalse" that we split differently; must not happen for formatted output
			if errB != nil {
				t.Fatalf("formatted output is not valid JSON: %v\n%s", errB, out.Bytes())
			}
			return
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("round trip changed value:\n in: %q\nout: %q", data, out.Bytes())
		}
	})
}

func decodeAll(data []byte) ([]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var vals []any
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return vals, nil
		}
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
}

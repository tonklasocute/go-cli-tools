// Package jsonformat implements the jsonformat tool: a streaming JSON
// pretty-printer, minifier and validator with syntax highlighting, path
// queries and precise, friendly syntax error reports.
//
// The package uses its own strict RFC 8259 scanner rather than
// encoding/json's Decoder.Token: the scanner streams tokens from a bounded
// buffer, tracks exact line/column positions (columns count runes) and keeps
// recent input around so an error can be shown with its source context.
package jsonformat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Kind identifies the type of a JSON token or value.
type Kind uint8

// Token and value kinds. BeginObject and BeginArray double as the kinds of
// object and array values in a Value tree.
const (
	KindInvalid Kind = iota
	BeginObject
	EndObject
	BeginArray
	EndArray
	String
	Number
	True
	False
	Null
)

// Token is a single JSON token produced by Scanner.Next.
type Token struct {
	// Kind is the token type.
	Kind Kind
	// Text holds the decoded contents of a String token or the exact literal
	// text of a Number token. It is only valid until the next call to Next.
	Text []byte
	// Key reports whether a String token is an object key.
	Key bool
}

// Pos is a position in the input. Line and Col are 1-based and Col counts
// runes, not bytes. Col is 0 when the column could not be determined.
type Pos struct {
	Offset int64
	Line   int
	Col    int
}

// String formats p as "line:col" (or "line N" when the column is unknown).
func (p Pos) String() string {
	if p.Col == 0 {
		return fmt.Sprintf("line %d", p.Line)
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

const (
	bufSize   = 256 << 10 // read buffer size
	keepBytes = 64 << 10  // history retained behind the read position for error context
	readAhead = 8 << 10   // bytes read past an error to complete its source line
	// MaxDepth is the maximum nesting depth of objects and arrays accepted by
	// the scanner (the same limit encoding/json uses).
	MaxDepth = 10000
)

type state uint8

const (
	stTop      state = iota // expecting a top-level value or end of input
	stObjStart              // after '{': key or '}'
	stObjKey                // after ',' in an object: key
	stObjColon              // after a key: ':'
	stObjValue              // after ':': value
	stObjNext               // after a member value: ',' or '}'
	stArrStart              // after '[': value or ']'
	stArrValue              // after ',' in an array: value
	stArrNext               // after an element: ',' or ']'
)

type frame struct {
	obj bool
	pos Pos
}

// mark is a cheap snapshot of a position whose column is resolved lazily.
type mark struct {
	off   int64
	line  int
	lcOff int64
	lcCol int
}

// Scanner is a strict, streaming JSON tokenizer. It validates the grammar as
// it goes, accepts any number of concatenated top-level values (NDJSON) and
// reports errors as *SyntaxError with exact positions and source context.
type Scanner struct {
	ctx  context.Context
	r    io.Reader
	buf  []byte
	pos  int
	end  int
	base int64 // absolute offset of buf[0]
	eof  bool
	rerr error // sticky read or context error
	err  error // sticky error returned by Next

	line  int
	lcOff int64 // a known point on the current line ...
	lcCol int   // ... and the number of runes between line start and it

	stack     []frame
	state     state
	values    int
	scratch   []byte
	wsStart   mark // position where the last whitespace run began (end of the previous token)
	lastComma mark
	started   bool
}

// NewScanner returns a scanner reading from r. Reading stops with ctx.Err()
// once ctx is canceled; ctx may be nil.
func NewScanner(ctx context.Context, r io.Reader) *Scanner {
	return &Scanner{ctx: ctx, r: r, buf: make([]byte, bufSize), line: 1}
}

// Values returns the number of complete top-level values scanned so far.
func (s *Scanner) Values() int { return s.values }

// Depth returns the current nesting depth.
func (s *Scanner) Depth() int { return len(s.stack) }

// Next returns the next token. It returns io.EOF after the last top-level
// value, a *SyntaxError for malformed input, or the underlying read error.
// Errors are sticky.
func (s *Scanner) Next() (Token, error) {
	if s.err != nil {
		return Token{}, s.err
	}
	tok, err := s.next()
	if err != nil {
		s.err = err
	}
	return tok, err
}

func (s *Scanner) next() (Token, error) {
	for {
		c, ok := s.skipWS()
		if !ok {
			if s.rerr != nil {
				return Token{}, s.rerr
			}
			return Token{}, s.eofError()
		}
		switch s.state {
		case stTop, stObjValue, stArrStart, stArrValue:
			if c == ']' && s.state == stArrStart {
				return s.closeContainer(false)
			}
			if c == ']' && s.state == stArrValue {
				return Token{}, s.errAtMark(s.lastComma, "trailing comma before ']'", "JSON does not allow trailing commas")
			}
			return s.value(c)
		case stObjStart, stObjKey:
			switch {
			case c == '"':
				if err := s.readString(); err != nil {
					return Token{}, err
				}
				s.state = stObjColon
				return Token{Kind: String, Text: s.scratch, Key: true}, nil
			case c == '}' && s.state == stObjStart:
				return s.closeContainer(true)
			case c == '}':
				return Token{}, s.errAtMark(s.lastComma, "trailing comma before '}'", "JSON does not allow trailing commas")
			}
			return Token{}, s.badKey(c)
		case stObjColon:
			if c != ':' {
				return Token{}, s.unexpected(c, "expected ':' after object key", "")
			}
			s.pos++
			s.state = stObjValue
		case stObjNext, stArrNext:
			obj := s.state == stObjNext
			switch {
			case c == ',':
				s.lastComma = s.markHere()
				s.pos++
				if obj {
					s.state = stObjKey
				} else {
					s.state = stArrValue
				}
			case obj && c == '}', !obj && c == ']':
				return s.closeContainer(obj)
			default:
				return Token{}, s.badSeparator(c, obj)
			}
		}
	}
}

// value scans a value starting with c in any value position.
func (s *Scanner) value(c byte) (Token, error) {
	switch {
	case c == '{' || c == '[':
		if len(s.stack) >= MaxDepth {
			return Token{}, s.errAt(s.posHere(), fmt.Sprintf("nesting too deep (more than %d levels)", MaxDepth), "")
		}
		obj := c == '{'
		s.stack = append(s.stack, frame{obj: obj, pos: s.posHere()})
		s.pos++
		if obj {
			s.state = stObjStart
			return Token{Kind: BeginObject}, nil
		}
		s.state = stArrStart
		return Token{Kind: BeginArray}, nil
	case c == '"':
		if err := s.readString(); err != nil {
			return Token{}, err
		}
		s.valueDone()
		return Token{Kind: String, Text: s.scratch}, nil
	case c == '-' || (c >= '0' && c <= '9'):
		if err := s.readNumber(); err != nil {
			return Token{}, err
		}
		s.valueDone()
		return Token{Kind: Number, Text: s.scratch}, nil
	case isWordByte(c):
		start := s.posHere()
		word := s.readWord()
		if s.rerr != nil {
			return Token{}, s.rerr
		}
		var k Kind
		switch word {
		case "true":
			k = True
		case "false":
			k = False
		case "null":
			k = Null
		default:
			return Token{}, s.errAt(start, literalMessage(word), "")
		}
		s.valueDone()
		return Token{Kind: k}, nil
	}
	return Token{}, s.badValue(c)
}

func (s *Scanner) closeContainer(obj bool) (Token, error) {
	s.pos++
	s.stack = s.stack[:len(s.stack)-1]
	s.valueDone()
	if obj {
		return Token{Kind: EndObject}, nil
	}
	return Token{Kind: EndArray}, nil
}

func (s *Scanner) valueDone() {
	if len(s.stack) == 0 {
		s.state = stTop
		s.values++
		return
	}
	if s.stack[len(s.stack)-1].obj {
		s.state = stObjNext
	} else {
		s.state = stArrNext
	}
}

// ---- error messages -------------------------------------------------------

func containerName(obj bool) string {
	if obj {
		return "object"
	}
	return "array"
}

func (s *Scanner) eofError() error {
	p := s.resolve(s.wsStart)
	if len(s.stack) == 0 {
		if s.values > 0 && s.state == stTop {
			return io.EOF
		}
		return s.errAt(p, "unexpected end of input: expected a JSON value", "")
	}
	fr := s.stack[len(s.stack)-1]
	msg := fmt.Sprintf("unexpected end of input: unclosed %s opened at %s", containerName(fr.obj), fr.pos)
	if n := len(s.stack) - 1; n > 0 {
		msg += fmt.Sprintf(" (%d more enclosing %s still open)", n, plural(n, "container", "containers"))
	}
	return s.errAt(p, msg, "")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// unexpected reports the character at the read position.
func (s *Scanner) unexpected(c byte, expected, hint string) error {
	if c == '/' {
		return s.errAt(s.posHere(), "comments are not allowed in JSON", "")
	}
	return s.errAt(s.posHere(), expected+", found "+s.describeHere(), hint)
}

func (s *Scanner) badValue(c byte) error {
	switch c {
	case '\'':
		return s.errAt(s.posHere(), "strings must be enclosed in double quotes, found '\\''", "")
	case '+':
		return s.errAt(s.posHere(), "invalid number: numbers cannot start with '+'", "")
	case '.':
		return s.errAt(s.posHere(), "invalid number: expected a digit before '.'", "")
	}
	switch s.state {
	case stObjValue:
		return s.unexpected(c, "expected a value after ':'", "")
	case stArrStart:
		return s.unexpected(c, "expected a value or ']'", "")
	case stArrValue:
		return s.unexpected(c, "expected a value after ','", "")
	}
	// top level
	if c == '}' || c == ']' {
		return s.errAt(s.posHere(), fmt.Sprintf("unexpected '%c' with no open object or array", c), "")
	}
	if s.values > 0 && (c == ',' || c == ':') {
		return s.errAt(s.posHere(), fmt.Sprintf("unexpected '%c' after top-level value", c), "")
	}
	return s.unexpected(c, "expected a JSON value", "")
}

func (s *Scanner) badKey(c byte) error {
	switch {
	case c == '\'':
		return s.errAt(s.posHere(), "object keys must be enclosed in double quotes, found '\\''", "")
	case isWordByte(c):
		start := s.posHere()
		word := s.readWord()
		if s.rerr != nil {
			return s.rerr
		}
		return s.errAt(start, fmt.Sprintf("object keys must be double-quoted strings, found %s", quoteWord(word)),
			fmt.Sprintf("write it as \"%s\"", word))
	case c == ']':
		return s.mismatched(c)
	}
	if s.state == stObjStart {
		return s.unexpected(c, "expected a string key or '}'", "")
	}
	return s.unexpected(c, "expected a string key after ','", "")
}

func (s *Scanner) badSeparator(c byte, obj bool) error {
	if (obj && c == ']') || (!obj && c == '}') {
		return s.mismatched(c)
	}
	hint := ""
	if startsValue(c) {
		end := s.resolve(s.wsStart)
		here := s.posHere()
		if end.Line < here.Line {
			hint = fmt.Sprintf("is a ',' missing at the end of line %d?", end.Line)
		} else {
			hint = fmt.Sprintf("is a ',' missing at %s?", end)
		}
	}
	if obj {
		return s.unexpected(c, "expected ',' or '}' after object value", hint)
	}
	return s.unexpected(c, "expected ',' or ']' after array element", hint)
}

func (s *Scanner) mismatched(c byte) error {
	fr := s.stack[len(s.stack)-1]
	want := ']'
	if fr.obj {
		want = '}'
	}
	return s.errAt(s.posHere(), fmt.Sprintf("mismatched '%c': %s opened at %s must be closed with '%c'",
		c, containerName(fr.obj), fr.pos, want), "")
}

func startsValue(c byte) bool {
	return c == '"' || c == '{' || c == '[' || c == '-' || (c >= '0' && c <= '9') || isWordByte(c)
}

func isWordByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '$'
}

func quoteWord(w string) string { return "'" + w + "'" }

func literalMessage(word string) string {
	lw := strings.ToLower(word)
	for _, lit := range []string{"true", "false", "null"} {
		if lw == lit {
			return fmt.Sprintf("invalid literal %s: JSON literals are lowercase (%s)", quoteWord(word), lit)
		}
		if strings.HasPrefix(lit, word) {
			return fmt.Sprintf("invalid literal %s (did you mean '%s'?)", quoteWord(word), lit)
		}
	}
	switch word {
	case "NaN", "Infinity":
		return fmt.Sprintf("%s is not valid JSON (numbers must be finite)", quoteWord(word))
	case "undefined", "None", "nil":
		return fmt.Sprintf("%s is not valid JSON (use null)", quoteWord(word))
	}
	return fmt.Sprintf("unexpected %s, expected a JSON value (strings must be double-quoted)", quoteWord(word))
}

// describeHere renders the (possibly multi-byte) character at the read
// position for an error message.
func (s *Scanner) describeHere() string {
	s.ensure(utf8.UTFMax)
	r, size := utf8.DecodeRune(s.buf[s.pos:s.end])
	switch {
	case r == utf8.RuneError && size <= 1:
		return fmt.Sprintf("invalid UTF-8 byte 0x%02X", s.buf[s.pos])
	case r < 0x20 || r == 0x7f:
		return fmt.Sprintf("control character U+%04X", r)
	case r == 0xFEFF:
		return "byte order mark U+FEFF"
	}
	return "'" + string(r) + "'"
}

// ---- lexing ---------------------------------------------------------------

// readWord consumes a run of identifier-like bytes (used for literals).
func (s *Scanner) readWord() string {
	var b []byte
	for {
		for s.pos < s.end {
			c := s.buf[s.pos]
			if !isWordByte(c) && (c < '0' || c > '9') {
				return string(b)
			}
			if len(b) < 64 {
				b = append(b, c)
			}
			s.pos++
		}
		if !s.fill() {
			return string(b)
		}
	}
}

// peek returns the byte at the read position, filling the buffer if needed.
func (s *Scanner) peek() (byte, bool) {
	if s.pos >= s.end && !s.fill() {
		return 0, false
	}
	return s.buf[s.pos], true
}

func (s *Scanner) readNumber() error {
	s.scratch = s.scratch[:0]
	start := s.posHere()
	c, _ := s.peek()
	if c == '-' {
		s.scratch = append(s.scratch, c)
		s.pos++
		c2, ok := s.peek()
		if !ok || c2 < '0' || c2 > '9' {
			return s.numErr(ok, "invalid number: expected a digit after '-'")
		}
		c = c2
	}
	if c == '0' {
		s.scratch = append(s.scratch, c)
		s.pos++
		if c2, ok := s.peek(); ok && c2 >= '0' && c2 <= '9' {
			return s.errAt(start, "invalid number: leading zeros are not allowed", "")
		}
	} else {
		s.digits()
	}
	if c, ok := s.peek(); ok && c == '.' {
		s.scratch = append(s.scratch, c)
		s.pos++
		if c2, ok := s.peek(); !ok || c2 < '0' || c2 > '9' {
			return s.numErr(ok, "invalid number: expected a digit after '.'")
		}
		s.digits()
	}
	if c, ok := s.peek(); ok && (c == 'e' || c == 'E') {
		s.scratch = append(s.scratch, c)
		s.pos++
		if c2, ok := s.peek(); ok && (c2 == '+' || c2 == '-') {
			s.scratch = append(s.scratch, c2)
			s.pos++
		}
		if c2, ok := s.peek(); !ok || c2 < '0' || c2 > '9' {
			return s.numErr(ok, "invalid number: expected a digit in exponent")
		}
		s.digits()
	}
	// A number immediately followed by letters or a dot (0x1F, 1.2.3, 10px)
	// is reported as one bad number rather than a confusing separator error.
	if c, ok := s.peek(); ok && (isWordByte(c) || c == '.') {
		text := string(s.scratch)
		for {
			c, ok := s.peek()
			if !ok || len(text) >= 64 || (!isWordByte(c) && c != '.' && (c < '0' || c > '9')) {
				break
			}
			text += string(c)
			s.pos++
		}
		return s.errAt(start, fmt.Sprintf("invalid number %s", quoteWord(text)), "")
	}
	return nil
}

func (s *Scanner) numErr(ok bool, msg string) error {
	if !ok && s.rerr != nil {
		return s.rerr
	}
	return s.errAt(s.posHere(), msg, "")
}

func (s *Scanner) digits() {
	for {
		i := s.pos
		for i < s.end && s.buf[i] >= '0' && s.buf[i] <= '9' {
			i++
		}
		s.scratch = append(s.scratch, s.buf[s.pos:i]...)
		s.pos = i
		if i < s.end || !s.fill() {
			return
		}
	}
}

// readString decodes the string starting at the opening quote under the read
// position into s.scratch.
func (s *Scanner) readString() error {
	start := s.markHere()
	s.scratch = s.scratch[:0]
	s.pos++
	for {
		if s.pos >= s.end && !s.fill() {
			if s.rerr != nil {
				return s.rerr
			}
			return s.errAt(s.posHere(), fmt.Sprintf("unexpected end of input: unterminated string starting at %s", s.resolve(start)), "")
		}
		i := s.pos
		for i < s.end {
			c := s.buf[i]
			if c < 0x20 || c == '"' || c == '\\' {
				break
			}
			i++
		}
		s.scratch = append(s.scratch, s.buf[s.pos:i]...)
		s.pos = i
		if i == s.end {
			continue
		}
		switch c := s.buf[i]; c {
		case '"':
			s.pos++
			return nil
		case '\\':
			if err := s.readEscape(start); err != nil {
				return err
			}
		case '\n', '\r':
			return s.errAt(s.posHere(), "unterminated string: missing closing '\"' before end of line",
				fmt.Sprintf("the string starts at %s", s.resolve(start)))
		default:
			esc := fmt.Sprintf("\\u%04x", c)
			if c == '\t' {
				esc = "\\t"
			}
			return s.errAt(s.posHere(), fmt.Sprintf("invalid control character U+%04X in string (escape it as %s)", c, esc), "")
		}
	}
}

func (s *Scanner) readEscape(start mark) error {
	if !s.ensure(2) {
		if s.rerr != nil {
			return s.rerr
		}
		s.pos = s.end
		return s.errAt(s.posHere(), fmt.Sprintf("unexpected end of input: unterminated string starting at %s", s.resolve(start)), "")
	}
	switch e := s.buf[s.pos+1]; e {
	case '"', '\\', '/':
		s.scratch = append(s.scratch, e)
	case 'b':
		s.scratch = append(s.scratch, '\b')
	case 'f':
		s.scratch = append(s.scratch, '\f')
	case 'n':
		s.scratch = append(s.scratch, '\n')
	case 'r':
		s.scratch = append(s.scratch, '\r')
	case 't':
		s.scratch = append(s.scratch, '\t')
	case 'u':
		r, ok := s.hex4(2)
		if !ok && s.rerr != nil {
			return s.rerr
		}
		if !ok {
			return s.errAt(s.posHere(), "invalid \\u escape in string: expected 4 hex digits", "")
		}
		s.pos += 6
		if r >= 0xD800 && r < 0xDC00 {
			// high surrogate: combine with a following low surrogate if present
			if s.ensure(6) && s.buf[s.pos] == '\\' && s.buf[s.pos+1] == 'u' {
				if r2, ok := s.hex4(2); ok && r2 >= 0xDC00 && r2 < 0xE000 {
					s.pos += 6
					s.scratch = utf8.AppendRune(s.scratch, (r-0xD800)<<10+(r2-0xDC00)+0x10000)
					return nil
				}
			}
			r = utf8.RuneError
		} else if r >= 0xDC00 && r < 0xE000 {
			r = utf8.RuneError
		}
		s.scratch = utf8.AppendRune(s.scratch, r)
		return nil
	default:
		s.ensure(2 + utf8.UTFMax)
		r, _ := utf8.DecodeRune(s.buf[s.pos+1 : s.end])
		what := "'\\" + string(r) + "'"
		if r < 0x20 || r == utf8.RuneError {
			what = fmt.Sprintf("'\\' followed by U+%04X", r)
		}
		return s.errAt(s.posHere(), fmt.Sprintf("invalid escape sequence %s in string", what),
			"valid escapes are \\\" \\\\ \\/ \\b \\f \\n \\r \\t \\uXXXX")
	}
	s.pos += 2
	return nil
}

// hex4 decodes four hex digits k bytes after the read position.
func (s *Scanner) hex4(k int) (rune, bool) {
	if !s.ensure(k + 4) {
		return 0, false
	}
	var r rune
	for _, c := range s.buf[s.pos+k : s.pos+k+4] {
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		default:
			return 0, false
		}
		r = r<<4 | rune(v)
	}
	return r, true
}

// ---- buffer and position management --------------------------------------

func (s *Scanner) skipWS() (byte, bool) {
	if !s.started {
		s.started = true
		if s.ensure(3) && s.buf[0] == 0xEF && s.buf[1] == 0xBB && s.buf[2] == 0xBF {
			s.pos = 3 // skip a UTF-8 byte order mark
			s.lcOff = 3
		}
	}
	s.wsStart = s.markHere()
	for {
		for s.pos < s.end {
			switch s.buf[s.pos] {
			case ' ', '\t', '\r':
				s.pos++
			case '\n':
				s.pos++
				s.line++
				s.lcOff = s.base + int64(s.pos)
				s.lcCol = 0
			default:
				return s.buf[s.pos], true
			}
		}
		if !s.fill() {
			return 0, false
		}
	}
}

// ensure makes at least n bytes available from the read position. The
// buffer may be shifted, so callers must not hold buffer indexes across it.
func (s *Scanner) ensure(n int) bool {
	for s.end-s.pos < n {
		if !s.fill() {
			return false
		}
	}
	return true
}

// fill reads more input, keeping up to keepBytes of history behind the read
// position. It returns false at end of input or on error (see s.rerr).
func (s *Scanner) fill() bool {
	if s.eof || s.rerr != nil {
		return false
	}
	if s.ctx != nil {
		if err := s.ctx.Err(); err != nil {
			s.rerr = err
			return false
		}
	}
	if s.end == len(s.buf) {
		if drop := s.pos - keepBytes; drop > 0 {
			s.shift(drop)
		} else {
			nb := make([]byte, 2*len(s.buf))
			copy(nb, s.buf[:s.end])
			s.buf = nb
		}
	}
	for tries := 0; tries < 100; tries++ {
		n, err := s.r.Read(s.buf[s.end:])
		s.end += n
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.eof = true
			} else {
				s.rerr = err
			}
			return n > 0
		}
		if n > 0 {
			return true
		}
	}
	s.rerr = io.ErrNoProgress
	return false
}

func (s *Scanner) shift(drop int) {
	newBase := s.base + int64(drop)
	if s.lcOff < newBase {
		s.lcCol += countRunes(s.buf[s.lcOff-s.base : drop])
		s.lcOff = newBase
	}
	copy(s.buf, s.buf[drop:s.end])
	s.end -= drop
	s.pos -= drop
	s.base = newBase
}

// countRunes counts UTF-8 rune starts; invalid bytes count as one rune each.
// It is safe on slices that split a multi-byte sequence.
func countRunes(b []byte) int {
	n := 0
	for _, c := range b {
		if c&0xC0 != 0x80 {
			n++
		}
	}
	return n
}

func (s *Scanner) markHere() mark {
	return mark{off: s.base + int64(s.pos), line: s.line, lcOff: s.lcOff, lcCol: s.lcCol}
}

// posHere returns the position of the read position on the current line.
func (s *Scanner) posHere() Pos {
	off := s.base + int64(s.pos)
	from := int(s.lcOff - s.base)
	s.lcCol += countRunes(s.buf[from:s.pos])
	s.lcOff = off
	return Pos{Offset: off, Line: s.line, Col: s.lcCol + 1}
}

// resolve computes the full position of a mark if the bytes needed to count
// its column are still buffered.
func (s *Scanner) resolve(m mark) Pos {
	p := Pos{Offset: m.off, Line: m.line}
	end := s.base + int64(s.end)
	switch {
	case m.off > end:
	case m.line == s.line && s.lcOff <= m.off:
		p.Col = s.lcCol + countRunes(s.buf[s.lcOff-s.base:m.off-s.base]) + 1
	case m.line == s.line && m.off >= s.base:
		p.Col = s.lcCol - countRunes(s.buf[m.off-s.base:s.lcOff-s.base]) + 1
	case m.lcOff >= s.base && m.lcOff <= m.off:
		p.Col = m.lcCol + countRunes(s.buf[m.lcOff-s.base:m.off-s.base]) + 1
	}
	return p
}

func (s *Scanner) errAtMark(m mark, msg, hint string) error {
	return s.errAt(s.resolve(m), msg, hint)
}

// errAt builds a SyntaxError at p, capturing the surrounding source lines.
func (s *Scanner) errAt(p Pos, msg, hint string) error {
	e := &SyntaxError{Pos: p, Msg: msg, Hint: hint}
	if p.Col > 0 && p.Offset >= s.base && p.Offset <= s.base+int64(s.end) {
		s.captureContext(e)
	}
	return e
}

// captureContext reads ahead to the end of the error line and copies up to
// two preceding lines plus the error line out of the buffer.
func (s *Scanner) captureContext(e *SyntaxError) {
	errAbs := e.Pos.Offset
	s.pos = int(errAbs - s.base)
	searched := errAbs
	for {
		found := false
		for i := int(searched - s.base); i < s.end; i++ {
			if s.buf[i] == '\n' {
				found = true
				break
			}
		}
		searched = s.base + int64(s.end)
		if found || searched-errAbs >= readAhead || !s.fill() {
			break
		}
	}
	errIdx := int(errAbs - s.base)
	lineEnd := errIdx
	for lineEnd < s.end && s.buf[lineEnd] != '\n' {
		lineEnd++
	}
	lineStart := s.lineStart(errIdx)
	errLine := srcLine{
		num:      e.Pos.Line,
		text:     displayRunes(s.buf[lineStart:lineEnd]),
		firstCol: e.Pos.Col - countRunes(s.buf[lineStart:errIdx]),
	}
	var lines []srcLine
	end := lineStart - 1 // index of the '\n' ending the previous line
	for n := 1; n <= 2 && end >= 0 && e.Pos.Line-n >= 1; n++ {
		start := s.lineStart(end)
		if start == 0 && s.base > 0 {
			break // the start of this line is no longer buffered
		}
		lines = append([]srcLine{{num: e.Pos.Line - n, text: displayRunes(s.buf[start:end]), firstCol: 1}}, lines...)
		end = start - 1
	}
	lines = append(lines, errLine)
	e.lines = lines
}

// lineStart returns the buffer index where the line containing index i
// starts (or the oldest buffered byte), skipping a leading byte order mark.
func (s *Scanner) lineStart(i int) int {
	for i > 0 && s.buf[i-1] != '\n' {
		i--
	}
	if i == 0 && s.base == 0 && s.end >= 3 && s.buf[0] == 0xEF && s.buf[1] == 0xBB && s.buf[2] == 0xBF {
		i = 3
	}
	return i
}

// displayRunes decodes a source line for display: a trailing CR is dropped and
// control characters (other than tab) and invalid bytes become U+FFFD. It
// yields exactly one rune per rune start counted by countRunes, so rune
// indexes line up with reported columns.
func displayRunes(b []byte) []rune {
	if n := len(b); n > 0 && b[n-1] == '\r' {
		b = b[:n-1]
	}
	out := make([]rune, 0, len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		switch {
		case r == utf8.RuneError && size <= 1 && b[0]&0xC0 == 0x80:
			b = b[1:] // stray continuation byte: not counted as a column
			continue
		case (r < 0x20 && r != '\t') || r == 0x7f:
			r = utf8.RuneError
		}
		out = append(out, r)
		b = b[size:]
	}
	return out
}

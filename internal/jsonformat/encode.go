package jsonformat

import (
	"strings"
	"unicode/utf8"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

const hexDigits = "0123456789abcdef"

// appendQuoted appends s to dst as a JSON string literal. Unlike
// encoding/json it does not escape <, > and &. Control characters, quotes
// and backslashes are escaped, U+2028/U+2029 are escaped for JavaScript
// safety and invalid UTF-8 is replaced with �.
func appendQuoted[T ~string | ~[]byte](dst []byte, s T) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' && c < utf8.RuneSelf {
			i++
			continue
		}
		if c < utf8.RuneSelf {
			dst = append(dst, s[start:i]...)
			switch c {
			case '"', '\\':
				dst = append(dst, '\\', c)
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(string(s[i:min(i+utf8.UTFMax, len(s))]))
		if (r == utf8.RuneError && size == 1) || r == ' ' || r == ' ' {
			dst = append(dst, s[start:i]...)
			switch r {
			case ' ':
				dst = append(dst, ` `...)
			case ' ':
				dst = append(dst, ` `...)
			default:
				dst = append(dst, `�`...)
			}
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// paint holds the ANSI prefix/suffix for one highlight role.
type paint struct{ pre, suf string }

// palette precomputes ANSI sequences for the JSON roles so highlighting
// costs two string writes per token instead of a lipgloss render.
type palette struct {
	on                                  bool
	key, str, num, boolean, null, punct paint
}

func newPalette(th *ui.Theme) palette {
	if !th.Color() {
		return palette{}
	}
	mk := func(role ui.Role) paint {
		// "x" never occurs inside an SGR sequence, so it splits the
		// rendered sentinel into the role's prefix and suffix.
		out := th.Paint(role, "x")
		i := strings.LastIndexByte(out, 'x')
		if i < 0 {
			return paint{}
		}
		return paint{pre: out[:i], suf: out[i+1:]}
	}
	return palette{
		on:      true,
		key:     mk(ui.RoleKey),
		str:     mk(ui.RoleString),
		num:     mk(ui.RoleNumber),
		boolean: mk(ui.RoleBool),
		null:    mk(ui.RoleNull),
		punct:   mk(ui.RolePunct),
	}
}

// emitter writes JSON tokens with layout (indentation or compact) and
// optional syntax highlighting. It is driven either by the streaming token
// loop or by a Value tree walk.
type emitter struct {
	w        *outBuf
	indent   string // indentation unit; "" means compact output
	pal      palette
	depth    int
	first    bool // no element written yet in the current container
	afterKey bool // a key was written and its value is pending
	scratch  []byte
}

func newEmitter(w *outBuf, indent string, th *ui.Theme) *emitter {
	return &emitter{w: w, indent: indent, pal: newPalette(th)}
}

func (e *emitter) colored(p paint, b []byte) {
	if e.pal.on {
		e.w.str(p.pre)
		e.w.bytes(b)
		e.w.str(p.suf)
		return
	}
	e.w.bytes(b)
}

func (e *emitter) punct(c byte) {
	if e.pal.on {
		e.w.str(e.pal.punct.pre)
		e.w.byte1(c)
		e.w.str(e.pal.punct.suf)
		return
	}
	e.w.byte1(c)
}

func (e *emitter) newline() {
	if e.indent == "" {
		return
	}
	e.w.byte1('\n')
	for i := 0; i < e.depth; i++ {
		e.w.str(e.indent)
	}
}

// before positions the output for a new value or key.
func (e *emitter) before() {
	if e.afterKey {
		e.afterKey = false
		return
	}
	if e.depth > 0 {
		if !e.first {
			e.punct(',')
		}
		e.first = false
		e.newline()
	}
}

// after terminates a top-level value with a newline.
func (e *emitter) after() {
	if e.depth == 0 {
		e.w.byte1('\n')
	}
}

func (e *emitter) open(c byte) {
	e.before()
	e.punct(c)
	e.depth++
	e.first = true
}

func (e *emitter) close(c byte) {
	e.depth--
	if !e.first {
		e.newline()
	}
	e.punct(c)
	e.first = false
	e.after()
}

func (e *emitter) key(k []byte) {
	e.before()
	e.scratch = appendQuoted(e.scratch[:0], k)
	e.writeKey()
}

// writeKey writes the quoted key in e.scratch followed by the separator.
func (e *emitter) writeKey() {
	e.colored(e.pal.key, e.scratch)
	e.punct(':')
	if e.indent != "" {
		e.w.byte1(' ')
	}
	e.afterKey = true
}

func (e *emitter) str(s []byte) {
	e.before()
	e.scratch = appendQuoted(e.scratch[:0], s)
	e.colored(e.pal.str, e.scratch)
	e.after()
}

func (e *emitter) scalar(k Kind, text []byte) {
	e.before()
	switch k {
	case Number:
		e.colored(e.pal.num, text)
	case True:
		e.colored(e.pal.boolean, litTrue)
	case False:
		e.colored(e.pal.boolean, litFalse)
	default:
		e.colored(e.pal.null, litNull)
	}
	e.after()
}

var (
	litTrue  = []byte("true")
	litFalse = []byte("false")
	litNull  = []byte("null")
)

// token writes one scanner token.
func (e *emitter) token(t Token) {
	switch t.Kind {
	case BeginObject:
		e.open('{')
	case EndObject:
		e.close('}')
	case BeginArray:
		e.open('[')
	case EndArray:
		e.close(']')
	case String:
		if t.Key {
			e.key(t.Text)
		} else {
			e.str(t.Text)
		}
	default:
		e.scalar(t.Kind, t.Text)
	}
}

// abort ends a partially written value with a newline and resets the layout
// state, so an error report never starts mid-line.
func (e *emitter) abort() {
	if e.depth > 0 || e.afterKey {
		e.w.byte1('\n')
	}
	e.depth, e.first, e.afterKey = 0, false, false
}

package envcheck

import (
	"fmt"
	"strings"
)

// Entry is one KEY=VALUE assignment found in a dotenv file.
type Entry struct {
	// Key is the variable name.
	Key string
	// Value is the decoded value (quotes removed, escapes applied).
	Value string
	// Line is the 1-based line on which the assignment starts.
	Line int `json:"line"`
	// EndLine is the last line of the assignment; it differs from Line only
	// for multiline quoted values.
	EndLine int
	// Comment is the inline comment after the value, without the leading
	// '#' and surrounding whitespace.
	Comment string
	// Exported reports whether the line used the "export " prefix.
	Exported bool
}

// Comment is a full-line comment in a dotenv file.
type Comment struct {
	// Line is the 1-based line number.
	Line int `json:"line"`
	// Text is everything after the leading '#', with trailing whitespace
	// removed.
	Text string
}

// LineError describes a line that could not be parsed.
type LineError struct {
	// Line is the 1-based line number where the problem starts.
	Line int `json:"line"`
	// Msg explains the problem. It never contains the line's value.
	Msg string `json:"message"`
}

// Error implements the error interface.
func (e LineError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// File is a parsed dotenv file. Parsing never fails as a whole: lines that
// cannot be understood are collected in Errors and parsing continues.
type File struct {
	// Entries lists the assignments in file order, duplicates included.
	Entries []Entry
	// Comments lists full-line comments in file order.
	Comments []Comment
	// Errors lists malformed lines in file order.
	Errors []LineError
}

// Lookup returns the effective entry for key (the last one wins) and the
// line numbers of every assignment of key. ok is false when key is absent.
func (f *File) Lookup(key string) (e Entry, lines []int, ok bool) {
	for _, en := range f.Entries {
		if en.Key == key {
			e, ok = en, true
			lines = append(lines, en.Line)
		}
	}
	return e, lines, ok
}

// Parse parses dotenv content.
//
// Supported syntax: full-line '#' comments, inline comments after unquoted
// values (a '#' preceded by whitespace), an optional "export " prefix,
// whitespace around '=', empty values, single-quoted (literal) values,
// double-quoted values with the escapes \n \r \t \" \\ \$, backtick-quoted
// (literal) values, quoted values spanning several lines, CRLF line endings
// and a leading UTF-8 byte order mark. Variable expansion (${VAR}) is not
// performed: values are taken literally.
func Parse(data []byte) *File {
	s := strings.TrimPrefix(string(data), "\xef\xbb\xbf") // UTF-8 BOM
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	f := &File{}
	for i := 0; i < len(lines); i++ {
		lineNo := i + 1
		t := strings.TrimLeft(strings.TrimRight(lines[i], "\r"), " \t")
		if t == "" {
			continue
		}
		if t[0] == '#' {
			f.Comments = append(f.Comments, Comment{Line: lineNo, Text: strings.TrimRight(t[1:], " \t")})
			continue
		}
		e, consumed, msg := parseAssignment(t, lines[i+1:])
		if msg != "" {
			f.Errors = append(f.Errors, LineError{Line: lineNo, Msg: msg})
			continue
		}
		e.Line, e.EndLine = lineNo, lineNo+consumed
		f.Entries = append(f.Entries, e)
		i += consumed
	}
	return f
}

// validKey reports whether k matches [A-Za-z_][A-Za-z0-9_.]*.
func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '.'):
		default:
			return false
		}
	}
	return true
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

// parseAssignment parses a non-blank, non-comment line t (leading whitespace
// already removed). rest holds the following lines, used for multiline
// quoted values. It returns how many extra lines were consumed, or a
// non-empty problem description.
func parseAssignment(t string, rest []string) (e Entry, consumed int, problem string) {
	if strings.HasPrefix(t, "export") && len(t) > 6 && isBlank(t[6]) {
		t = strings.TrimLeft(t[6:], " \t")
		e.Exported = true
	}
	j := 0
	for j < len(t) && t[j] != '=' && !isBlank(t[j]) {
		j++
	}
	key := t[:j]
	r := strings.TrimLeft(t[j:], " \t")
	if !strings.HasPrefix(r, "=") {
		if validKey(key) {
			return e, 0, fmt.Sprintf("expected '=' after key %q", key)
		}
		return e, 0, "expected KEY=VALUE"
	}
	if key == "" {
		return e, 0, "missing key before '='"
	}
	if !validKey(key) {
		return e, 0, fmt.Sprintf("invalid key %q (must match [A-Za-z_][A-Za-z0-9_.]*)", shorten(key, 40))
	}
	e.Key = key
	r = r[1:]
	trimmed := strings.TrimLeft(r, " \t")
	hadSpace := len(trimmed) < len(r)
	r = trimmed

	switch {
	case r == "":
	case r[0] == '#' && hadSpace:
		e.Comment = strings.TrimSpace(r[1:])
	case r[0] == '"' || r[0] == '\'' || r[0] == '`':
		q := r[0]
		val, after, n, ok := scanQuoted(r[1:], q, rest)
		if !ok {
			return e, 0, fmt.Sprintf("unterminated %s-quoted value for %s", quoteName(q), key)
		}
		after = strings.TrimLeft(after, " \t")
		if after != "" {
			if after[0] != '#' {
				return e, 0, fmt.Sprintf("unexpected text after closing quote of %s", key)
			}
			e.Comment = strings.TrimSpace(after[1:])
		}
		e.Value, consumed = val, n
	default:
		e.Value, e.Comment = splitInlineComment(r)
	}
	return e, consumed, ""
}

func quoteName(q byte) string {
	switch q {
	case '"':
		return "double"
	case '\'':
		return "single"
	}
	return "backtick"
}

// scanQuoted reads a quoted value starting right after the opening quote q.
// When the closing quote is not on the current line it continues on the
// following lines in rest. It returns the decoded value, the text after the
// closing quote and the number of extra lines consumed.
func scanQuoted(s string, q byte, rest []string) (val, after string, consumed int, ok bool) {
	var b strings.Builder
	cur := s
	for {
		for i := 0; i < len(cur); i++ {
			c := cur[i]
			if q == '"' && c == '\\' && i+1 < len(cur) {
				i++
				switch cur[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				case '"', '\\', '$':
					b.WriteByte(cur[i])
				default:
					b.WriteByte('\\')
					b.WriteByte(cur[i])
				}
				continue
			}
			if c == q {
				return b.String(), cur[i+1:], consumed, true
			}
			b.WriteByte(c)
		}
		if consumed >= len(rest) {
			return "", "", 0, false
		}
		b.WriteByte('\n')
		cur = strings.TrimRight(rest[consumed], "\r")
		consumed++
	}
}

// splitInlineComment splits an unquoted value from a trailing comment that
// starts with a '#' preceded by whitespace.
func splitInlineComment(r string) (value, comment string) {
	for i := 1; i < len(r); i++ {
		if r[i] == '#' && isBlank(r[i-1]) {
			return strings.TrimRight(r[:i], " \t"), strings.TrimSpace(r[i+1:])
		}
	}
	return strings.TrimRight(r, " \t"), ""
}

// shorten truncates s to at most n runes, appending "…" when cut.
func shorten(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}

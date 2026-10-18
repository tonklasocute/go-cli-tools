package jsonformat

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Segment is one step of a query path: an object key or an array index.
type Segment struct {
	// Key is the object key (when IsIndex is false).
	Key string
	// Index is the array index; negative values count from the end.
	Index int
	// IsIndex reports whether the segment is an array index.
	IsIndex bool
}

// Path is a parsed query such as a.b[0].c or .["key.with.dots"][-1].
type Path []Segment

// ParsePath parses a query path. Supported syntax: dotted keys (a.b.c), an
// optional leading '.', array indexes ([0], [-1] from the end) and quoted
// keys (["key.with.dots"], using JSON string escapes). "" and "." select the
// whole value.
func ParsePath(s string) (Path, error) {
	src := s
	s = strings.TrimPrefix(s, ".")
	p := Path{}
	needSep := false // a key or ']' was just parsed
	for i := 0; i < len(s); {
		switch s[i] {
		case '[':
			seg, n, err := parseBracket(s[i:])
			if err != nil {
				return nil, fmt.Errorf("invalid query %q: %w at offset %d", src, err, i+len(src)-len(s))
			}
			p = append(p, seg)
			i += n
			needSep = true
		case '.':
			if !needSep {
				return nil, fmt.Errorf("invalid query %q: empty key at offset %d", src, i+len(src)-len(s))
			}
			i++
			if i == len(s) {
				return nil, fmt.Errorf("invalid query %q: trailing '.'", src)
			}
			needSep = false
		default:
			if needSep {
				return nil, fmt.Errorf("invalid query %q: expected '.' or '[' at offset %d", src, i+len(src)-len(s))
			}
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '[' {
				if s[j] == ']' || s[j] == '"' {
					return nil, fmt.Errorf("invalid query %q: unexpected %q at offset %d (quote unusual keys as [\"key\"])",
						src, s[j], j+len(src)-len(s))
				}
				j++
			}
			p = append(p, Segment{Key: s[i:j]})
			i = j
			needSep = true
		}
	}
	return p, nil
}

// parseBracket parses "[N]" or `["key"]` at the start of s and returns the
// segment and the number of bytes consumed.
func parseBracket(s string) (Segment, int, error) {
	if len(s) > 1 && s[1] == '"' {
		// find the closing quote, honoring backslash escapes
		j := 2
		for j < len(s) && s[j] != '"' {
			if s[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(s) {
			return Segment{}, 0, fmt.Errorf("unterminated quoted key")
		}
		var key string
		if err := json.Unmarshal([]byte(s[1:j+1]), &key); err != nil {
			return Segment{}, 0, fmt.Errorf("bad quoted key")
		}
		if j+1 >= len(s) || s[j+1] != ']' {
			return Segment{}, 0, fmt.Errorf("expected ']' after quoted key")
		}
		return Segment{Key: key}, j + 2, nil
	}
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return Segment{}, 0, fmt.Errorf("missing ']'")
	}
	n, err := strconv.Atoi(strings.TrimSpace(s[1:end]))
	if err != nil {
		return Segment{}, 0, fmt.Errorf("index %q is not an integer", s[1:end])
	}
	return Segment{Index: n, IsIndex: true}, end + 1, nil
}

// String renders p in canonical query syntax ("." for the empty path).
func (p Path) String() string {
	if len(p) == 0 {
		return "."
	}
	var b strings.Builder
	for i, seg := range p {
		switch {
		case seg.IsIndex:
			fmt.Fprintf(&b, "[%d]", seg.Index)
		case plainKey(seg.Key):
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(seg.Key)
		default:
			b.WriteByte('[')
			b.Write(appendQuoted(nil, seg.Key))
			b.WriteByte(']')
		}
	}
	return b.String()
}

func plainKey(k string) bool {
	return k != "" && !strings.ContainsAny(k, ".[]\" \t\n")
}

// QueryError reports a path that does not exist in the input.
type QueryError struct {
	// Path is the query prefix up to and including the failing segment.
	Path Path
	// Msg explains why the segment could not be resolved.
	Msg string
}

// Error formats the error as "path a.b[3]: index out of range (len 2)".
func (e *QueryError) Error() string { return "path " + e.Path.String() + ": " + e.Msg }

// Lookup resolves p against v.
func Lookup(v *Value, p Path) (*Value, error) {
	cur := v
	for i, seg := range p {
		fail := func(format string, args ...any) error {
			return &QueryError{Path: p[:i+1], Msg: fmt.Sprintf(format, args...)}
		}
		switch {
		case seg.IsIndex:
			if cur.Kind != BeginArray {
				return nil, fail("cannot index %s with [%d]", kindName(cur.Kind), seg.Index)
			}
			idx := seg.Index
			if idx < 0 {
				idx += len(cur.Elems)
			}
			if idx < 0 || idx >= len(cur.Elems) {
				return nil, fail("index out of range (len %d)", len(cur.Elems))
			}
			cur = cur.Elems[idx]
		default:
			if cur.Kind != BeginObject {
				return nil, fail("cannot look up key %q in %s", seg.Key, kindName(cur.Kind))
			}
			var found *Value
			// the last duplicate wins, matching encoding/json and jq
			for _, m := range cur.Members {
				if m.Key == seg.Key {
					found = m.Value
				}
			}
			if found == nil {
				return nil, fail("key not found%s", availableKeys(cur))
			}
			cur = found
		}
	}
	return cur, nil
}

func availableKeys(v *Value) string {
	if len(v.Members) == 0 {
		return " (object is empty)"
	}
	const maxKeys = 8
	keys := make([]string, 0, maxKeys)
	for i, m := range v.Members {
		if i == maxKeys {
			keys = append(keys, "…")
			break
		}
		keys = append(keys, Path{{Key: m.Key}}.String())
	}
	return " (available: " + strings.Join(keys, ", ") + ")"
}

func kindName(k Kind) string {
	switch k {
	case BeginObject:
		return "an object"
	case BeginArray:
		return "an array"
	case String:
		return "a string"
	case Number:
		return "a number"
	case True, False:
		return "a boolean"
	default:
		return "null"
	}
}

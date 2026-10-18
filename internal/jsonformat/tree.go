package jsonformat

import (
	"errors"
	"io"
	"sort"
)

// Value is an ordered JSON value tree. Object members keep their input
// order (until SortKeys) and numbers keep their exact literal text.
type Value struct {
	// Kind is BeginObject for objects, BeginArray for arrays, or the scalar
	// kind (String, Number, True, False, Null).
	Kind Kind
	// Text is the decoded string or the literal number text.
	Text string
	// Members holds object members in order.
	Members []Member
	// Elems holds array elements.
	Elems []*Value
}

// Member is one key/value pair of an object.
type Member struct {
	Key   string
	Value *Value
}

// ReadValue reads the next complete top-level value from sc. It returns
// io.EOF when the input is exhausted.
func ReadValue(sc *Scanner) (*Value, error) {
	tok, err := sc.Next()
	if err != nil {
		return nil, err
	}
	return buildValue(sc, tok)
}

func buildValue(sc *Scanner, tok Token) (*Value, error) {
	switch tok.Kind {
	case BeginObject:
		v := &Value{Kind: BeginObject}
		for {
			k, err := next(sc)
			if err != nil {
				return nil, err
			}
			if k.Kind == EndObject {
				return v, nil
			}
			key := string(k.Text)
			t, err := next(sc)
			if err != nil {
				return nil, err
			}
			child, err := buildValue(sc, t)
			if err != nil {
				return nil, err
			}
			v.Members = append(v.Members, Member{Key: key, Value: child})
		}
	case BeginArray:
		v := &Value{Kind: BeginArray}
		for {
			t, err := next(sc)
			if err != nil {
				return nil, err
			}
			if t.Kind == EndArray {
				return v, nil
			}
			child, err := buildValue(sc, t)
			if err != nil {
				return nil, err
			}
			v.Elems = append(v.Elems, child)
		}
	case String, Number:
		return &Value{Kind: tok.Kind, Text: string(tok.Text)}, nil
	default:
		return &Value{Kind: tok.Kind}, nil
	}
}

// next is sc.Next where end of input inside a value is impossible (the
// scanner reports a SyntaxError instead), so io.EOF is converted defensively.
func next(sc *Scanner) (Token, error) {
	t, err := sc.Next()
	if errors.Is(err, io.EOF) {
		return t, io.ErrUnexpectedEOF
	}
	return t, err
}

// SortKeys sorts object members by key (byte-wise, stable for duplicate
// keys) recursively.
func (v *Value) SortKeys() {
	switch v.Kind {
	case BeginObject:
		sort.SliceStable(v.Members, func(i, j int) bool { return v.Members[i].Key < v.Members[j].Key })
		for _, m := range v.Members {
			m.Value.SortKeys()
		}
	case BeginArray:
		for _, e := range v.Elems {
			e.SortKeys()
		}
	}
}

// emit writes the tree through the emitter.
func (e *emitter) value(v *Value) {
	switch v.Kind {
	case BeginObject:
		e.open('{')
		for _, m := range v.Members {
			e.before()
			e.scratch = appendQuoted(e.scratch[:0], m.Key)
			e.writeKey()
			e.value(m.Value)
		}
		e.close('}')
	case BeginArray:
		e.open('[')
		for _, el := range v.Elems {
			e.value(el)
		}
		e.close(']')
	case String:
		e.before()
		e.scratch = appendQuoted(e.scratch[:0], v.Text)
		e.colored(e.pal.str, e.scratch)
		e.after()
	default:
		e.scalar(v.Kind, []byte(v.Text))
	}
}

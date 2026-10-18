package envcheck

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Type is the kind of value a schema field expects.
type Type string

// Value types understood by the schema.
const (
	TypeString Type = "string"
	TypeInt    Type = "int"
	TypeFloat  Type = "float"
	TypeBool   Type = "bool"
	TypeURL    Type = "url"
	TypeEmail  Type = "email"
)

// Field is one key declared in the schema.
type Field struct {
	// Key is the variable name.
	Key string
	// Line is the schema line declaring the key.
	Line int
	// Default is the example value written in the schema (informational).
	Default string
	// Required means the key must be present with a non-empty value.
	Required bool
	// Secret means the value must never be printed.
	Secret bool
	// Type is the expected value type (TypeString when not annotated).
	Type Type
	// Enum lists the allowed values, if restricted.
	Enum []string
	// Pattern is the regular expression the value must match, if any.
	Pattern *regexp.Regexp
	// Min and Max bound the numeric value for int/float fields and the
	// length in characters for every other type. Nil means unbounded.
	Min, Max *float64
}

// Numeric reports whether min/max apply to the value rather than its length.
func (f *Field) Numeric() bool { return f.Type == TypeInt || f.Type == TypeFloat }

// Schema is a parsed .env.example schema.
type Schema struct {
	// Fields lists the declared keys in schema order.
	Fields []*Field
	index  map[string]*Field
}

// Field returns the field declared for key, or nil.
func (s *Schema) Field(key string) *Field { return s.index[key] }

// SchemaError reports every problem found while parsing a schema.
type SchemaError struct {
	// File is the schema's display name.
	File string
	// Problems lists the offending lines.
	Problems []LineError
}

// Error implements the error interface; each problem is rendered as
// "file:line: message".
func (e *SchemaError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = fmt.Sprintf("%s:%d: %s", e.File, p.Line, p.Msg)
	}
	if len(parts) == 1 {
		return "invalid schema: " + parts[0]
	}
	return "invalid schema:\n  " + strings.Join(parts, "\n  ")
}

// KnownRules lists the rule names accepted in schema annotations.
var KnownRules = []string{"required", "secret", "int", "float", "bool", "url", "email", "enum", "regex", "min", "max"}

// ParseSchema parses an annotated .env.example. name is used in error
// messages. Full-line comments and blank lines are ignored; the inline
// comment of each assignment is its comma-separated rule list. A key
// without a comment is an optional string.
//
// The regex=<pattern> rule consumes the rest of the annotation, so a
// pattern may contain commas; put it last.
func ParseSchema(data []byte, name string) (*Schema, error) {
	env := Parse(data)
	s := &Schema{index: map[string]*Field{}}
	var problems []LineError
	problems = append(problems, env.Errors...)
	for _, e := range env.Entries {
		if prev := s.index[e.Key]; prev != nil {
			problems = append(problems, LineError{Line: e.Line, Msg: fmt.Sprintf("duplicate key %s (first declared on line %d)", e.Key, prev.Line)})
			continue
		}
		f := &Field{Key: e.Key, Line: e.Line, Default: e.Value, Type: TypeString}
		if err := parseRules(f, e.Comment); err != nil {
			problems = append(problems, LineError{Line: e.Line, Msg: fmt.Sprintf("%s: %v", e.Key, err)})
			continue
		}
		s.Fields = append(s.Fields, f)
		s.index[f.Key] = f
	}
	if len(problems) > 0 {
		sort.SliceStable(problems, func(i, j int) bool { return problems[i].Line < problems[j].Line })
		return nil, &SchemaError{File: name, Problems: problems}
	}
	return s, nil
}

// nextRule splits the first rule token off an annotation. A token named
// regex consumes the rest of the annotation.
func nextRule(ann string) (tok, rest string) {
	t := strings.TrimLeft(ann, " \t")
	if i := strings.IndexAny(t, ",="); i >= 0 && t[i] == '=' && strings.EqualFold(strings.TrimSpace(t[:i]), "regex") {
		return t, ""
	}
	tok, rest, _ = strings.Cut(ann, ",")
	return tok, rest
}

// parseRules applies the rules in annotation ann to f.
func parseRules(f *Field, ann string) error {
	seen := map[string]bool{}
	typed := false
	for rest := ann; strings.TrimSpace(rest) != ""; {
		var tok string
		tok, rest = nextRule(rest)
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		name, arg, hasArg := strings.Cut(tok, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		arg = strings.TrimSpace(arg)
		if seen[name] {
			return fmt.Errorf("rule %q given more than once", name)
		}
		seen[name] = true
		switch name {
		case "required", "secret", "int", "float", "bool", "url", "email":
			if hasArg {
				return fmt.Errorf("rule %q takes no value", name)
			}
			switch name {
			case "required":
				f.Required = true
			case "secret":
				f.Secret = true
			default:
				if typed {
					return fmt.Errorf("conflicting types %q and %q", f.Type, name)
				}
				typed = true
				f.Type = Type(name)
			}
		case "enum", "regex", "min", "max":
			if arg == "" {
				return fmt.Errorf("rule %q needs a value (%s=...)", name, name)
			}
			if err := applyArgRule(f, name, arg); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown rule %q (known rules: %s; put descriptions on their own comment line)",
				shorten(tok, 40), strings.Join(KnownRules, ", "))
		}
	}
	return checkBounds(f)
}

func applyArgRule(f *Field, name, arg string) error {
	switch name {
	case "enum":
		for _, v := range strings.Split(arg, "|") {
			v = strings.TrimSpace(v)
			if v == "" {
				return fmt.Errorf("enum %q contains an empty choice", arg)
			}
			f.Enum = append(f.Enum, v)
		}
	case "regex":
		re, err := regexp.Compile(arg)
		if err != nil {
			return fmt.Errorf("invalid regex %q: %w", arg, err)
		}
		f.Pattern = re
	default: // min, max
		v, err := strconv.ParseFloat(arg, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%s=%s is not a number", name, arg)
		}
		if name == "min" {
			f.Min = &v
		} else {
			f.Max = &v
		}
	}
	return nil
}

// checkBounds validates min/max once the field's type is known.
func checkBounds(f *Field) error {
	if !f.Numeric() {
		for _, b := range []struct {
			name string
			v    *float64
		}{{"min", f.Min}, {"max", f.Max}} {
			if b.v != nil && (*b.v < 0 || *b.v != math.Trunc(*b.v)) {
				return fmt.Errorf("%s=%s must be a non-negative whole number (it bounds the length of a %s value)",
					b.name, formatNum(*b.v), f.Type)
			}
		}
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return fmt.Errorf("min=%s is greater than max=%s", formatNum(*f.Min), formatNum(*f.Max))
	}
	return nil
}

func formatNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

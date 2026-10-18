package envcheck

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// GeneratedKey describes one key written by GenerateSchema.
type GeneratedKey struct {
	// Key is the variable name.
	Key string `json:"key"`
	// Rules is the generated annotation, e.g. "required,int".
	Rules string `json:"rules"`
	// Line is the source line of the (first) assignment.
	Line int `json:"line"`
}

var floatRe = regexp.MustCompile(`^[+-]?(\d+\.\d*|\.\d+)([eE][+-]?\d+)?$`)

// GuessRules returns the annotation GenerateSchema writes for key=value:
// every key is required; keys whose name looks like a credential become
// "required,secret"; otherwise a type is guessed from the value (bool,
// int, float, url or email; nothing for plain strings).
func GuessRules(key, value string) string {
	if LooksSecret(key) {
		return "required,secret"
	}
	if t := guessType(value); t != "" {
		return "required," + string(t)
	}
	return "required"
}

func guessType(v string) Type {
	switch {
	case v == "":
		return ""
	case isWordBool(v):
		return TypeBool
	case isInt(v):
		return TypeInt
	case floatRe.MatchString(v):
		return TypeFloat
	case strings.Contains(v, "://") && IsURL(v):
		return TypeURL
	case IsEmail(v):
		return TypeEmail
	}
	return ""
}

// isWordBool accepts the spelled-out booleans only; 0/1 are guessed as int.
func isWordBool(v string) bool { return IsBool(v) && v != "0" && v != "1" }

func isInt(v string) bool {
	_, err := strconv.ParseInt(v, 10, 64)
	return err == nil
}

// GenerateSchema renders a schema for env. Values are stripped (they may be
// secrets), full-line comments and blank-line grouping are preserved,
// inline comments are dropped and duplicate keys are written once, at their
// first position. source names the input in the header comment.
func GenerateSchema(env *File, source string) (string, []GeneratedKey) {
	type item struct {
		line, end int
		text      string
		key       *GeneratedKey
	}
	var items []item
	var keys []GeneratedKey
	seen := map[string]bool{}
	for _, c := range env.Comments {
		items = append(items, item{line: c.Line, end: c.Line, text: "#" + c.Text})
	}
	width := 0
	for _, e := range env.Entries {
		if seen[e.Key] {
			continue
		}
		seen[e.Key] = true
		last, _, _ := env.Lookup(e.Key)
		keys = append(keys, GeneratedKey{Key: e.Key, Rules: GuessRules(e.Key, last.Value), Line: e.Line})
		width = max(width, len(e.Key)+1)
	}
	ki := 0
	for _, e := range env.Entries {
		if ki < len(keys) && keys[ki].Line == e.Line && keys[ki].Key == e.Key {
			items = append(items, item{line: e.Line, end: e.EndLine, key: &keys[ki]})
			ki++
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].line < items[j].line })

	var b strings.Builder
	b.WriteString("# Schema for envcheck, generated from " + source + " by `envcheck init`.\n")
	b.WriteString("# Values were stripped on purpose. Review the guessed rules before committing.\n")
	b.WriteString("# Rules: required, secret, int, float, bool, url, email, enum=a|b, min=N, max=N, regex=<pattern> (last).\n")
	prevEnd := -1
	for _, it := range items {
		if prevEnd < 0 || it.line > prevEnd+1 {
			b.WriteString("\n")
		}
		prevEnd = it.end
		if it.key == nil {
			b.WriteString(it.text + "\n")
			continue
		}
		k := it.key.Key + "="
		b.WriteString(k + strings.Repeat(" ", width-len(k)+2) + "# " + it.key.Rules + "\n")
	}
	return b.String(), keys
}

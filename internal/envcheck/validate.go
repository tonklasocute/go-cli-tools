package envcheck

import (
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Mask replaces secret values in every kind of output. It has a fixed
// length so the real value's length is not revealed.
const Mask = "••••••"

// Status is the outcome of one check.
type Status string

// Possible result statuses.
const (
	StatusOK    Status = "ok"
	StatusWarn  Status = "warn"
	StatusError Status = "error"
	StatusSkip  Status = "skip"
)

// Result is one row of a validation report.
type Result struct {
	// Key is the variable name; empty for malformed lines.
	Key string `json:"key"`
	// Status is ok, warn, error or skip.
	Status Status `json:"status"`
	// Message explains the status. Secret values are masked.
	Message string `json:"message"`
	// Line is the .env line the result refers to, or 0 when the key is
	// not present in the file.
	Line int `json:"line"`
	// Value is the (masked when secret) value, only set when requested
	// with Options.ShowValues and the key is present.
	Value *string `json:"value,omitempty"`

	lineInMsg bool
}

// Summary counts results by status.
type Summary struct {
	OK       int `json:"ok"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
	Skipped  int `json:"skipped"`
}

// Summarize counts results by status.
func Summarize(results []Result) Summary {
	var s Summary
	for _, r := range results {
		switch r.Status {
		case StatusOK:
			s.OK++
		case StatusWarn:
			s.Warnings++
		case StatusError:
			s.Errors++
		case StatusSkip:
			s.Skipped++
		}
	}
	return s
}

// Options tunes Validate.
type Options struct {
	// ShowValues fills Result.Value (secrets stay masked).
	ShowValues bool
}

// secretName matches key names that look like they hold credentials.
var secretName = regexp.MustCompile(`(?i)(secret|password|passwd|token|api_?key|private|credential)`)

// LooksSecret reports whether a key name suggests it holds a credential.
func LooksSecret(key string) bool { return secretName.MatchString(key) }

// Validate checks env against schema. Results come in schema order, then
// keys missing from the schema in file order, then malformed lines.
func Validate(schema *Schema, env *File, opts Options) []Result {
	var results []Result
	for _, f := range schema.Fields {
		e, lines, present := env.Lookup(f.Key)
		r := checkField(f, e, present)
		if present && opts.ShowValues {
			r.Value = displayValue(e.Value, f.Secret)
		}
		results = append(results, r)
		if len(lines) > 1 {
			results = append(results, duplicateResult(f.Key, lines))
		}
	}
	seen := map[string]bool{}
	for _, e := range env.Entries {
		if schema.Field(e.Key) != nil || seen[e.Key] {
			continue
		}
		seen[e.Key] = true
		last, lines, _ := env.Lookup(e.Key)
		r := Result{Key: e.Key, Status: StatusWarn, Message: "not declared in the schema", Line: last.Line}
		if opts.ShowValues {
			r.Value = displayValue(last.Value, LooksSecret(e.Key))
		}
		results = append(results, r)
		if len(lines) > 1 {
			results = append(results, duplicateResult(e.Key, lines))
		}
	}
	for _, pe := range env.Errors {
		results = append(results, Result{Status: StatusError, Message: "malformed line: " + pe.Msg, Line: pe.Line, lineInMsg: true})
	}
	return results
}

func displayValue(v string, secret bool) *string {
	if secret {
		m := Mask
		return &m
	}
	return &v
}

func duplicateResult(key string, lines []int) Result {
	ls := make([]string, len(lines))
	for i, l := range lines {
		ls[i] = strconv.Itoa(l)
	}
	return Result{
		Key:       key,
		Status:    StatusWarn,
		Message:   fmt.Sprintf("duplicate key on lines %s; the last one (line %d) wins", strings.Join(ls, ", "), lines[len(lines)-1]),
		Line:      lines[len(lines)-1],
		lineInMsg: true,
	}
}

// checkField validates the effective entry e for field f.
func checkField(f *Field, e Entry, present bool) Result {
	r := Result{Key: f.Key, Line: e.Line}
	switch {
	case !present && f.Required:
		r.Status, r.Message = StatusError, "required but not set"
		return r
	case !present:
		r.Status, r.Message = StatusSkip, "optional, not set"
		if f.Default != "" {
			r.Message += " (example: " + quoteValue(f.Default, f.Secret) + ")"
		}
		return r
	case e.Value == "" && f.Required:
		r.Status, r.Message = StatusError, "required but empty"
		return r
	case e.Value == "":
		r.Status, r.Message = StatusSkip, "optional, empty"
		return r
	}
	if problems := checkValue(f, e.Value); len(problems) > 0 {
		r.Status, r.Message = StatusError, strings.Join(problems, "; ")
		return r
	}
	r.Status, r.Message = StatusOK, Describe(f)
	return r
}

// quoteValue renders v for messages: quoted and shortened, or masked.
func quoteValue(v string, secret bool) string {
	if secret {
		return Mask
	}
	return strconv.Quote(shorten(v, 48))
}

// checkValue returns every rule that v violates.
func checkValue(f *Field, v string) []string {
	var problems []string
	shown := quoteValue(v, f.Secret)
	num, typeOK := 0.0, true
	switch f.Type {
	case TypeInt:
		n, err := strconv.ParseInt(v, 10, 64)
		if typeOK = err == nil; typeOK {
			num = float64(n)
		} else {
			problems = append(problems, shown+" is not an integer")
		}
	case TypeFloat:
		n, err := strconv.ParseFloat(v, 64)
		if typeOK = err == nil && !math.IsNaN(n) && !math.IsInf(n, 0); typeOK {
			num = n
		} else {
			problems = append(problems, shown+" is not a number")
		}
	case TypeBool:
		if !IsBool(v) {
			problems = append(problems, shown+" is not a boolean (use true/false, 1/0, yes/no, on/off)")
		}
	case TypeURL:
		if !IsURL(v) {
			problems = append(problems, shown+" is not an absolute URL (needs scheme and host)")
		}
	case TypeEmail:
		if !IsEmail(v) {
			problems = append(problems, shown+" is not a valid email address")
		}
	}
	if len(f.Enum) > 0 && !contains(f.Enum, v) {
		problems = append(problems, shown+" is not one of "+strings.Join(f.Enum, "|"))
	}
	if f.Pattern != nil && !f.Pattern.MatchString(v) {
		problems = append(problems, shown+" does not match "+f.Pattern.String())
	}
	if !typeOK {
		return problems
	}
	if f.Numeric() {
		if f.Min != nil && num < *f.Min {
			problems = append(problems, fmt.Sprintf("%s is below the minimum %s", shown, formatNum(*f.Min)))
		}
		if f.Max != nil && num > *f.Max {
			problems = append(problems, fmt.Sprintf("%s is above the maximum %s", shown, formatNum(*f.Max)))
		}
		return problems
	}
	n := float64(utf8.RuneCountInString(v))
	if f.Min != nil && n < *f.Min {
		problems = append(problems, lengthProblem(f, shown, n, "shorter", *f.Min))
	}
	if f.Max != nil && n > *f.Max {
		problems = append(problems, lengthProblem(f, shown, n, "longer", *f.Max))
	}
	return problems
}

func lengthProblem(f *Field, shown string, n float64, cmp string, bound float64) string {
	if f.Secret {
		// Never reveal a secret's length.
		return fmt.Sprintf("value is %s than %s characters", cmp, formatNum(bound))
	}
	return fmt.Sprintf("%s is %s characters, %s than %s", shown, formatNum(n), cmp, formatNum(bound))
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// IsBool reports whether v is a boolean: true/false, 1/0, yes/no or
// on/off, case-insensitive.
func IsBool(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false", "1", "0", "yes", "no", "on", "off":
		return true
	}
	return false
}

// IsURL reports whether v is an absolute URL with a scheme and a host.
func IsURL(v string) bool {
	u, err := url.Parse(v)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// IsEmail reports whether v is a bare email address (no display name).
func IsEmail(v string) bool {
	a, err := mail.ParseAddress(v)
	return err == nil && a.Name == "" && a.Address == v
}

// Describe summarizes the rules a field enforces, e.g. "int 1–65535".
func Describe(f *Field) string {
	var parts []string
	head := ""
	if f.Type != TypeString {
		head = string(f.Type)
	}
	if b := describeBounds(f); b != "" {
		head = strings.TrimSpace(head + " " + b)
	}
	if head != "" {
		parts = append(parts, head)
	}
	if len(f.Enum) > 0 {
		parts = append(parts, "one of "+strings.Join(f.Enum, "|"))
	}
	if f.Pattern != nil {
		parts = append(parts, "matches "+f.Pattern.String())
	}
	if f.Secret {
		parts = append(parts, "secret")
	}
	if len(parts) == 0 {
		return "string"
	}
	return strings.Join(parts, ", ")
}

func describeBounds(f *Field) string {
	unit := ""
	if !f.Numeric() {
		unit = "length "
	}
	switch {
	case f.Min != nil && f.Max != nil:
		return unit + formatNum(*f.Min) + "–" + formatNum(*f.Max)
	case f.Min != nil:
		return unit + "≥ " + formatNum(*f.Min)
	case f.Max != nil:
		return unit + "≤ " + formatNum(*f.Max)
	}
	return ""
}

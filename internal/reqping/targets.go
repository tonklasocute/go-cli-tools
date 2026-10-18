// Package reqping implements the reqping HTTP health checker: target list
// parsing, concurrent checks with latency statistics and TLS expiry
// reporting, and the text / JSON / watch-mode renderers.
package reqping

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// expectItem is one accepted status: an exact code (200) or a class (2xx).
type expectItem struct {
	code  int  // exact code, or the class digit when class is true
	class bool // true for "Nxx"
}

// Expect is the set of HTTP status codes considered healthy for a target.
// The zero value matches nothing and means "not specified".
type Expect struct {
	items []expectItem
}

// ParseExpect parses an expectation such as "200", "2xx" or "200,204,3xx".
func ParseExpect(s string) (Expect, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Expect{}, errors.New("empty status expectation")
	}
	var e Expect
	for _, tok := range strings.Split(s, ",") {
		tok = strings.ToLower(strings.TrimSpace(tok))
		switch {
		case len(tok) == 3 && strings.HasSuffix(tok, "xx") && tok[0] >= '1' && tok[0] <= '5':
			e.items = append(e.items, expectItem{code: int(tok[0] - '0'), class: true})
		default:
			n, err := strconv.Atoi(tok)
			if err != nil || len(tok) != 3 || n < 100 || n > 599 {
				return Expect{}, fmt.Errorf("invalid status %q (want a code like 200, a class like 2xx, or a comma list)", tok)
			}
			e.items = append(e.items, expectItem{code: n})
		}
	}
	return e, nil
}

// mustExpect is like ParseExpect for known-good literals; it falls
// back to the zero Expect instead of panicking on bad input.
func mustExpect(s string) Expect {
	e, err := ParseExpect(s)
	if err != nil {
		return Expect{}
	}
	return e
}

// DefaultExpect returns the expectation used when none is given: any 2xx
// when redirects are followed, any 2xx or 3xx when they are not.
func DefaultExpect(follow bool) Expect {
	if follow {
		return mustExpect("2xx")
	}
	return mustExpect("2xx,3xx")
}

// IsZero reports whether no expectation was specified.
func (e Expect) IsZero() bool { return len(e.items) == 0 }

// Match reports whether code satisfies the expectation.
func (e Expect) Match(code int) bool {
	for _, it := range e.items {
		if it.class && code/100 == it.code || !it.class && code == it.code {
			return true
		}
	}
	return false
}

// String renders the expectation in canonical form, e.g. "2xx" or "200,204".
func (e Expect) String() string {
	parts := make([]string, len(e.items))
	for i, it := range e.items {
		if it.class {
			parts[i] = strconv.Itoa(it.code) + "xx"
		} else {
			parts[i] = strconv.Itoa(it.code)
		}
	}
	return strings.Join(parts, ",")
}

// Target is one endpoint to check.
type Target struct {
	// Name is the display name (defaults to the URL host).
	Name string
	// URL is the absolute http(s) URL to request.
	URL string
	// Expect lists the healthy status codes.
	Expect Expect
}

// ParseURL validates that raw is an absolute http or https URL with a host.
// URLs without a scheme are rejected rather than guessed.
func ParseURL(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		return nil, fmt.Errorf("invalid URL %q: missing scheme (use http:// or https://)", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid URL %q: unsupported scheme %q (use http or https)", raw, u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("invalid URL %q: missing host", raw)
	}
	return u, nil
}

// NewTarget builds a target from a URL, naming it after the URL host. A zero
// expect is kept as is so callers can apply their own default.
func NewTarget(name, rawURL string, expect Expect) (Target, error) {
	u, err := ParseURL(rawURL)
	if err != nil {
		return Target{}, err
	}
	if name == "" {
		name = u.Host
	}
	return Target{Name: name, URL: u.String(), Expect: expect}, nil
}

// ParseTargets reads a targets list: one target per line in the form
// "[name] url [expected_status]", whitespace separated. Blank lines and
// "#" comments (whole-line or trailing) are ignored. Targets without an
// expected status get def. Errors are prefixed with "source:line:".
func ParseTargets(r io.Reader, source string, def Expect) ([]Target, error) {
	var out []Target
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		fields := stripComment(strings.Fields(sc.Text()))
		if len(fields) == 0 {
			continue
		}
		t, err := parseLine(fields, def)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", source, line, err)
		}
		out = append(out, t)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return out, nil
}

func stripComment(fields []string) []string {
	for i, f := range fields {
		if strings.HasPrefix(f, "#") {
			return fields[:i]
		}
	}
	return fields
}

func parseLine(fields []string, def Expect) (Target, error) {
	var name, rawURL, status string
	switch len(fields) {
	case 1:
		rawURL = fields[0]
	case 2:
		if strings.Contains(fields[0], "://") {
			rawURL, status = fields[0], fields[1]
		} else {
			name, rawURL = fields[0], fields[1]
		}
	case 3:
		name, rawURL, status = fields[0], fields[1], fields[2]
	default:
		return Target{}, fmt.Errorf("too many fields (%d); want \"[name] url [expected_status]\"", len(fields))
	}
	if name != "" && !strings.Contains(rawURL, "://") {
		if _, err := ParseExpect(rawURL); err == nil {
			// "example.com 200": the first field was meant as the URL.
			rawURL = name
		}
	}
	exp := def
	if status != "" {
		e, err := ParseExpect(status)
		if err != nil {
			return Target{}, err
		}
		exp = e
	}
	return NewTarget(name, rawURL, exp)
}

package dirsize

import (
	"fmt"
	"path/filepath"
	"strings"
)

// matcher decides whether a base name is excluded.
type matcher struct {
	exact map[string]bool
	globs []string
}

// newMatcher builds a matcher from names and filepath.Match patterns.
// Empty items are ignored; malformed patterns are an error wrapping
// filepath.ErrBadPattern.
func newMatcher(patterns []string) (*matcher, error) {
	m := &matcher{exact: map[string]bool{}}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.ContainsAny(p, "*?[") {
			m.exact[p] = true
			continue
		}
		if _, err := filepath.Match(p, ""); err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
		m.globs = append(m.globs, p)
	}
	return m, nil
}

func (m *matcher) excluded(name string) bool {
	if m.exact[name] {
		return true
	}
	for _, g := range m.globs {
		if ok, _ := filepath.Match(g, name); ok {
			return true
		}
	}
	return false
}

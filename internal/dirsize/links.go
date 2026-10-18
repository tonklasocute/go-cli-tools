package dirsize

import "sync"

// linkSet remembers multiply-linked files that were already counted, so
// hard links contribute their size once.
type linkSet struct {
	mu   sync.Mutex
	seen map[fileID]struct{}
}

// first reports whether id is seen for the first time, recording it.
func (l *linkSet) first(id fileID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.seen[id]; ok {
		return false
	}
	l.seen[id] = struct{}{}
	return true
}

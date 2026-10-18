package dirsize

import (
	"cmp"
	"container/heap"
	"strings"
)

// fileItem is a candidate for the largest-files listing.
type fileItem struct {
	path  string // relative to the root, OS separators
	bytes int64
}

// compareItems orders by size descending, then path ascending.
func compareItems(a, b fileItem) int {
	if c := cmp.Compare(b.bytes, a.bytes); c != 0 {
		return c
	}
	return strings.Compare(a.path, b.path)
}

// topFiles keeps the limit largest files seen (all of them when limit is
// 0). With a limit it is a min-heap whose root is the current worst item.
type topFiles struct {
	limit int
	items []fileItem
}

// wants reports cheaply whether a file of the given size could enter the
// set, so callers can skip building its path.
func (t *topFiles) wants(size int64) bool {
	return t.limit <= 0 || len(t.items) < t.limit || size >= t.items[0].bytes
}

func (t *topFiles) add(it fileItem) {
	switch {
	case t.limit <= 0:
		t.items = append(t.items, it)
	case len(t.items) < t.limit:
		heap.Push(t, it)
	case compareItems(it, t.items[0]) < 0: // better than the current worst
		t.items[0] = it
		heap.Fix(t, 0)
	}
}

// Len implements heap.Interface.
func (t *topFiles) Len() int { return len(t.items) }

// Less implements heap.Interface: the "smallest" item is the worst one.
func (t *topFiles) Less(i, j int) bool { return compareItems(t.items[i], t.items[j]) > 0 }

// Swap implements heap.Interface.
func (t *topFiles) Swap(i, j int) { t.items[i], t.items[j] = t.items[j], t.items[i] }

// Push implements heap.Interface.
func (t *topFiles) Push(x any) {
	if it, ok := x.(fileItem); ok {
		t.items = append(t.items, it)
	}
}

// Pop implements heap.Interface.
func (t *topFiles) Pop() any {
	n := len(t.items) - 1
	it := t.items[n]
	t.items = t.items[:n]
	return it
}

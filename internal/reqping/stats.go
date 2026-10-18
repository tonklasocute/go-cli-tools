package reqping

import (
	"slices"
	"time"
)

// Latency summarises a set of latency samples.
type Latency struct {
	// N is the number of samples.
	N   int
	Min time.Duration
	Avg time.Duration
	// P95 is the 95th percentile using the nearest-rank method.
	P95 time.Duration
	Max time.Duration
}

// Summarize computes min / avg / p95 / max over samples. The 95th percentile
// uses the nearest-rank method on the sorted samples: the value at rank
// ceil(0.95 * n). An empty input yields the zero Latency.
func Summarize(samples []time.Duration) Latency {
	n := len(samples)
	if n == 0 {
		return Latency{}
	}
	s := slices.Clone(samples)
	slices.Sort(s)
	var sum time.Duration
	for _, d := range s {
		sum += d
	}
	return Latency{
		N:   n,
		Min: s[0],
		Avg: sum / time.Duration(n),
		P95: s[nearestRank(95, n)-1],
		Max: s[n-1],
	}
}

// nearestRank returns the 1-based rank ceil(p/100 * n), clamped to [1, n].
func nearestRank(p, n int) int {
	r := (p*n + 99) / 100
	return max(1, min(r, n))
}

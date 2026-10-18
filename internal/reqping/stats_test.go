package reqping

import (
	"testing"
	"time"
)

func msd(vs ...int) []time.Duration {
	out := make([]time.Duration, len(vs))
	for i, v := range vs {
		out[i] = time.Duration(v) * time.Millisecond
	}
	return out
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name               string
		in                 []time.Duration
		min, avg, p95, max time.Duration
	}{
		{name: "empty"},
		{name: "single", in: msd(42), min: 42 * time.Millisecond, avg: 42 * time.Millisecond, p95: 42 * time.Millisecond, max: 42 * time.Millisecond},
		{name: "unsorted five", in: msd(50, 10, 40, 20, 30), min: 10 * time.Millisecond, avg: 30 * time.Millisecond, p95: 50 * time.Millisecond, max: 50 * time.Millisecond},
		{
			// 20 samples 1..20: rank ceil(0.95*20)=19 → 19ms.
			name: "twenty",
			in:   msd(20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1),
			min:  time.Millisecond, avg: 10500 * time.Microsecond, p95: 19 * time.Millisecond, max: 20 * time.Millisecond,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := append([]time.Duration(nil), tt.in...)
			got := Summarize(tt.in)
			if got.N != len(tt.in) || got.Min != tt.min || got.Avg != tt.avg || got.P95 != tt.p95 || got.Max != tt.max {
				t.Fatalf("Summarize = %+v, want min=%v avg=%v p95=%v max=%v", got, tt.min, tt.avg, tt.p95, tt.max)
			}
			for i := range orig {
				if orig[i] != tt.in[i] {
					t.Fatal("Summarize modified its input")
				}
			}
		})
	}
}

func TestNearestRank(t *testing.T) {
	tests := []struct{ p, n, want int }{
		{95, 1, 1}, {95, 2, 2}, {95, 10, 10}, {95, 20, 19}, {95, 100, 95}, {50, 4, 2}, {0, 5, 1},
	}
	for _, tt := range tests {
		if got := nearestRank(tt.p, tt.n); got != tt.want {
			t.Errorf("nearestRank(%d, %d) = %d, want %d", tt.p, tt.n, got, tt.want)
		}
	}
}

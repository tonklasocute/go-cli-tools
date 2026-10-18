package portscout

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkScan measures worker-pool scaling.
//
// The "fake" sub-benchmarks scan 200 ports through a simulated network where
// every dial takes 1ms (5 ports open, the rest refused), so the numbers show
// pure pool behaviour independent of the OS: ideal time per scan is
// ceil(200/workers) ms. The "real" sub-benchmarks dial actual listeners on
// 127.0.0.1 only (closed-port behaviour differs too much between OSes).
func BenchmarkScan(b *testing.B) {
	const nPorts = 200
	ports := make([]int, nPorts)
	for i := range ports {
		ports[i] = 20000 + i
	}
	states := map[int]State{}
	for i := 0; i < 5; i++ {
		states[ports[i*40]] = StateOpen
	}
	fn := &fakeNet{states: states, delay: time.Millisecond}

	for _, workers := range []int{1, 10, 100, 500} {
		b.Run(fmt.Sprintf("fake/workers=%d", workers), func(b *testing.B) {
			sc := &Scanner{Dial: fn.dial, Workers: workers, Timeout: time.Second}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				res, err := sc.Scan(context.Background(), "127.0.0.1", ports)
				if err != nil || len(res) != nPorts {
					b.Fatalf("%d results, %v", len(res), err)
				}
			}
		})
	}

	live := listenLocal(b, 20)
	for _, workers := range []int{1, 20} {
		b.Run(fmt.Sprintf("real-listeners/ports=20/workers=%d", workers), func(b *testing.B) {
			sc := &Scanner{Workers: workers, Timeout: 5 * time.Second}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				res, err := sc.Scan(context.Background(), "127.0.0.1", live)
				if err != nil || len(res) != len(live) {
					b.Fatalf("%d results, %v", len(res), err)
				}
			}
		})
	}
}

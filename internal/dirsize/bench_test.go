package dirsize

import (
	"context"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Benchmark tree: 10 × 10 × 10 = 1,000 leaf directories (1,110 dirs in
// all) holding 20 files each = 20,000 files with sizes 0–64 KiB (skewed
// small). Files are created with Truncate, so building it is cheap.
const (
	benchFanout       = 10
	benchFilesPerLeaf = 20
	benchFiles        = benchFanout * benchFanout * benchFanout * benchFilesPerLeaf
)

func buildBenchTree(b *testing.B) string {
	b.Helper()
	root := b.TempDir()
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < benchFanout; i++ {
		for j := 0; j < benchFanout; j++ {
			for k := 0; k < benchFanout; k++ {
				dir := filepath.Join(root, fmt.Sprintf("d%d", i), fmt.Sprintf("d%d", j), fmt.Sprintf("d%d", k))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					b.Fatal(err)
				}
				for f := 0; f < benchFilesPerLeaf; f++ {
					size := rng.Int63n(1<<16) >> rng.Intn(8) // mostly small, a few large
					fh, err := os.Create(filepath.Join(dir, fmt.Sprintf("f%02d.dat", f)))
					if err != nil {
						b.Fatal(err)
					}
					if err := fh.Truncate(size); err != nil {
						b.Fatal(err)
					}
					if err := fh.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
		}
	}
	return root
}

func reportFiles(b *testing.B) {
	if s := b.Elapsed().Seconds(); s > 0 {
		b.ReportMetric(float64(benchFiles)*float64(b.N)/s, "files/s")
	}
}

// BenchmarkScan compares a sequential filepath.WalkDir baseline with the
// concurrent scanner at several pool sizes over the same 20k-file tree.
func BenchmarkScan(b *testing.B) {
	root := buildBenchTree(b)

	b.Run("WalkDir", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var total, files int64
			err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				total += info.Size()
				files++
				return nil
			})
			if err != nil || files != benchFiles {
				b.Fatalf("walk: %v (%d files)", err, files)
			}
		}
		reportFiles(b)
	})

	for _, workers := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				res, err := Scan(context.Background(), root, Options{Depth: 1, Workers: workers})
				if err != nil || res.Files != benchFiles {
					b.Fatalf("scan: %v", err)
				}
			}
			reportFiles(b)
		})
	}
}

package dirsize

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// makeTree creates files (key → size in bytes) and directories (keys ending
// in "/") below root. Keys are slash-separated.
func makeTree(t testing.TB, root string, spec map[string]int64) {
	t.Helper()
	for k, size := range spec {
		p := filepath.Join(root, filepath.FromSlash(k))
		if strings.HasSuffix(k, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// sampleTree totals 9850 bytes in 9 files and 9 directories.
var sampleTree = map[string]int64{
	"a/x.bin":                   1000,
	"a/y.bin":                   500,
	"a/sub/z.bin":               2000,
	"b/w.bin":                   300,
	"b/deep/er/f.bin":           50,
	"c/":                        0,
	"top.txt":                   100,
	"node_modules/pkg/index.js": 5000,
	"logs/app.log":              700,
	"logs/old.log":              200,
}

func newSample(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	makeTree(t, root, sampleTree)
	return root
}

func summary(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		s := fmt.Sprintf("%s=%d", e.Path, e.Bytes)
		if e.IsDir {
			s = fmt.Sprintf("%s/=%d f%d d%d", e.Path, e.Bytes, e.Files, e.Dirs)
		}
		out[i] = s
	}
	return out
}

func TestScanAndReport(t *testing.T) {
	root := newSample(t)
	tests := []struct {
		name        string
		opts        Options
		rep         ReportOptions
		want        []string
		total       int64
		files, dirs int64
		hidden      int64
		hiddenBytes int64
	}{
		{
			name:  "depth 1",
			opts:  Options{Depth: 1},
			rep:   ReportOptions{Depth: 1},
			total: 9850, files: 9, dirs: 9,
			want: []string{
				"node_modules/=5000 f1 d1", "a/=3500 f3 d1", "logs/=900 f2 d0",
				"b/=350 f2 d2", "top.txt=100", "c/=0 f0 d0",
			},
		},
		{
			name:  "depth 2 includes shallower files and empty dirs",
			opts:  Options{Depth: 2},
			rep:   ReportOptions{Depth: 2},
			total: 9850, files: 9, dirs: 9,
			want: []string{
				"node_modules/pkg/=5000 f1 d0", "a/sub/=2000 f1 d0", "a/x.bin=1000",
				"logs/app.log=700", "a/y.bin=500", "b/w.bin=300", "logs/old.log=200",
				"top.txt=100", "b/deep/=50 f1 d1", "c/=0 f0 d0",
			},
		},
		{
			name:  "depth 0 totals only",
			opts:  Options{Depth: 0},
			rep:   ReportOptions{Depth: 0},
			total: 9850, files: 9, dirs: 9,
			want: []string{},
		},
		{
			name:  "top 2",
			opts:  Options{Depth: 1},
			rep:   ReportOptions{Depth: 1, Top: 2},
			total: 9850, files: 9, dirs: 9,
			want:   []string{"node_modules/=5000 f1 d1", "a/=3500 f3 d1"},
			hidden: 4, hiddenBytes: 1350,
		},
		{
			name:  "min size",
			opts:  Options{Depth: 2},
			rep:   ReportOptions{Depth: 2, MinSize: 600},
			total: 9850, files: 9, dirs: 9,
			want:   []string{"node_modules/pkg/=5000 f1 d0", "a/sub/=2000 f1 d0", "a/x.bin=1000", "logs/app.log=700"},
			hidden: 6, hiddenBytes: 1150,
		},
		{
			name:  "exclude names and globs",
			opts:  Options{Depth: 1, Exclude: []string{"node_modules", " *.log ", ""}},
			rep:   ReportOptions{Depth: 1},
			total: 3950, files: 6, dirs: 7,
			want: []string{"a/=3500 f3 d1", "b/=350 f2 d2", "top.txt=100", "c/=0 f0 d0", "logs/=0 f0 d0"},
		},
		{
			name:  "exclude nested dir",
			opts:  Options{Depth: 1, Exclude: []string{"sub"}},
			rep:   ReportOptions{Depth: 1, Top: 2},
			total: 7850, files: 8, dirs: 8,
			want:   []string{"node_modules/=5000 f1 d1", "a/=1500 f2 d0"},
			hidden: 4, hiddenBytes: 1350,
		},
		{
			name:  "files top 3",
			opts:  Options{Files: true, FileLimit: 3},
			rep:   ReportOptions{Files: true, Top: 3},
			total: 9850, files: 9, dirs: 9,
			want:   []string{"node_modules/pkg/index.js=5000", "a/sub/z.bin=2000", "a/x.bin=1000"},
			hidden: 6, hiddenBytes: 1850,
		},
		{
			name:  "files all with min size and exclude",
			opts:  Options{Files: true, MinFileSize: 500, Exclude: []string{"a"}},
			rep:   ReportOptions{Files: true, MinSize: 500},
			total: 6350, files: 6, dirs: 7,
			want:   []string{"node_modules/pkg/index.js=5000", "logs/app.log=700"},
			hidden: 4, hiddenBytes: 650,
		},
	}
	for _, tt := range tests {
		for _, workers := range []int{1, 8} {
			t.Run(fmt.Sprintf("%s/workers=%d", tt.name, workers), func(t *testing.T) {
				opts := tt.opts
				opts.Workers = workers
				res, err := Scan(context.Background(), root, opts)
				if err != nil {
					t.Fatal(err)
				}
				if res.TotalBytes != tt.total || res.Files != tt.files || res.Dirs != tt.dirs {
					t.Errorf("totals = %d bytes %d files %d dirs, want %d/%d/%d",
						res.TotalBytes, res.Files, res.Dirs, tt.total, tt.files, tt.dirs)
				}
				rep := BuildReport(res, tt.rep)
				if got := summary(rep.Entries); !slices.Equal(got, tt.want) {
					t.Errorf("entries\n got %q\nwant %q", got, tt.want)
				}
				if rep.hidden != tt.hidden || rep.hiddenBytes != tt.hiddenBytes {
					t.Errorf("hidden = %d (%d B), want %d (%d B)", rep.hidden, rep.hiddenBytes, tt.hidden, tt.hiddenBytes)
				}
				if res.Skipped != 0 {
					t.Errorf("skipped = %d, errors %v", res.Skipped, res.Errors)
				}
			})
		}
	}
}

func TestScanPercentAndPartition(t *testing.T) {
	root := newSample(t)
	res, err := Scan(context.Background(), root, Options{Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, e := range res.Entries {
		sum += e.Bytes
	}
	if sum != res.TotalBytes {
		t.Errorf("entries sum to %d, total %d", sum, res.TotalBytes)
	}
	rep := BuildReport(res, ReportOptions{Depth: 3})
	if rep.Entries[0].Path != "node_modules/pkg/index.js" || rep.Entries[0].Percent != 50.76 {
		t.Errorf("first entry = %+v", rep.Entries[0])
	}
}

func TestScanRootIsFile(t *testing.T) {
	root := newSample(t)
	file := filepath.Join(root, "a", "x.bin")
	for _, opts := range []Options{{Depth: 1}, {Files: true}, {Depth: 0}} {
		res, err := Scan(context.Background(), file, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.IsDir || res.TotalBytes != 1000 || res.Files != 1 {
			t.Errorf("%+v: res = %+v", opts, res)
		}
		wantEntries := 1
		if opts.Depth == 0 && !opts.Files {
			wantEntries = 0
		}
		if len(res.Entries) != wantEntries {
			t.Errorf("%+v: entries = %v", opts, res.Entries)
		}
	}
}

func TestScanErrors(t *testing.T) {
	_, err := Scan(context.Background(), filepath.Join(t.TempDir(), "missing"), Options{})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing root: err = %v", err)
	}
	_, err = Scan(context.Background(), t.TempDir(), Options{Exclude: []string{"[unclosed"}})
	if !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("bad pattern: err = %v", err)
	}
	if ValidateExcludes([]string{"ok", "*.go"}) != nil {
		t.Error("valid patterns rejected")
	}
}

func TestScanCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Scan(ctx, newSample(t), Options{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestScanCanceledMidWalk(t *testing.T) {
	root := newSample(t)
	for _, workers := range []int{1, 4} {
		ctx, cancel := context.WithCancel(context.Background())
		var dirs atomic.Int64
		var once sync.Once
		_, err := Scan(ctx, root, Options{Workers: workers, onDir: func(string) {
			if dirs.Add(1) == 2 {
				once.Do(cancel)
			}
		}})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Errorf("workers=%d: err = %v, want context.Canceled", workers, err)
		}
		if n := dirs.Load(); n >= 10 {
			t.Errorf("workers=%d: walk continued after cancel (%d dirs read)", workers, n)
		}
	}
}

func TestScanProgressCounter(t *testing.T) {
	var scanned atomic.Int64
	res, err := Scan(context.Background(), newSample(t), Options{Scanned: &scanned})
	if err != nil {
		t.Fatal(err)
	}
	if got := scanned.Load(); got != res.Files+res.Dirs {
		t.Errorf("scanned = %d, want %d", got, res.Files+res.Dirs)
	}
}

func TestSymlinksNotFollowed(t *testing.T) {
	outside := t.TempDir()
	makeTree(t, outside, map[string]int64{"big/blob.bin": 1 << 20})
	root := t.TempDir()
	makeTree(t, root, map[string]int64{"real/a.bin": 10})
	if err := os.Symlink(filepath.Join(outside, "big"), filepath.Join(root, "dirlink")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "big", "blob.bin"), filepath.Join(root, "real", "filelink")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	for _, opts := range []Options{{Depth: 1}, {Files: true}} {
		res, err := Scan(context.Background(), root, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.TotalBytes >= 1<<20 {
			t.Errorf("symlink target was counted: total %d", res.TotalBytes)
		}
		if res.Files != 3 || res.Dirs != 1 {
			t.Errorf("files=%d dirs=%d, want 3 files (incl. 2 links) and 1 dir", res.Files, res.Dirs)
		}
		for _, e := range res.Entries {
			if e.Path == "dirlink" && e.IsDir {
				t.Errorf("dirlink reported as directory: %+v", e)
			}
		}
	}
}

func TestMatcher(t *testing.T) {
	m, err := newMatcher([]string{"node_modules", "*.tmp", "build?"})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]bool{
		"node_modules": true, "a.tmp": true, "build1": true,
		"build": false, "node_modules2": false, "x.go": false,
	}
	for name, want := range tests {
		if got := m.excluded(name); got != want {
			t.Errorf("excluded(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestTopFiles(t *testing.T) {
	top := &topFiles{limit: 3}
	in := []fileItem{{"e", 5}, {"a", 1}, {"d", 4}, {"b", 4}, {"c", 9}, {"f", 4}, {"g", 0}}
	for _, it := range in {
		if top.wants(it.bytes) {
			top.add(it)
		}
	}
	got := slices.Clone(top.items)
	slices.SortFunc(got, compareItems)
	want := []fileItem{{"c", 9}, {"e", 5}, {"b", 4}}
	if !slices.Equal(got, want) {
		t.Errorf("top = %v, want %v", got, want)
	}
	if top.Pop().(fileItem).bytes > 9 || top.Len() != 2 {
		t.Error("Pop misbehaves")
	}
	top.Push("not an item")
	if top.Len() != 2 {
		t.Error("Push accepted a foreign value")
	}
}

func TestReason(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{&fs.PathError{Op: "open", Path: "x", Err: fs.ErrPermission}, "permission denied"},
		{&fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}, "no such file or directory"},
		{&fs.PathError{Op: "readdirent", Path: "x", Err: errors.New("i/o error")}, "i/o error"},
		{errors.New("boom"), "boom"},
	}
	for _, tt := range tests {
		if got := reason(tt.err); got != tt.want {
			t.Errorf("reason(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestWorkerRecordCapsErrors(t *testing.T) {
	s := &scanner{opts: Options{MaxErrors: 2}, prefixLen: len("root/")}
	w := &worker{s: s, top: &topFiles{}}
	for i := 0; i < 5; i++ {
		w.record(fmt.Sprintf("root/d%d", i), fs.ErrPermission)
	}
	w.record("x", errors.New("short path"))
	res := s.merge([]*worker{w})
	if res.Skipped != 6 || len(res.Errors) != 2 || res.Errors[0].Path != "d0" {
		t.Errorf("res = %+v", res)
	}
}

func TestJoinPath(t *testing.T) {
	sep := string(os.PathSeparator)
	if got := joinPath("a", "b"); got != "a"+sep+"b" {
		t.Errorf("joinPath = %q", got)
	}
	if got := joinPath("a"+sep, "b"); got != "a"+sep+"b" {
		t.Errorf("joinPath with trailing sep = %q", got)
	}
}

// Package dirsize implements a concurrent directory size analyzer.
//
// A bounded pool of workers walks the tree (never following symbolic links),
// sums apparent file sizes and aggregates them at a chosen depth, or collects
// the largest individual files. Per-entry errors such as permission denied
// are counted and reported instead of aborting the scan.
package dirsize

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultMaxErrors is how many per-entry errors a scan keeps when
// Options.MaxErrors is zero. Errors beyond it are only counted.
const DefaultMaxErrors = 1000

// readBatch is how many directory entries are read per ReadDir call, which
// bounds memory for huge directories and lets cancellation be noticed.
const readBatch = 512

// DefaultWorkers returns the default worker-pool size: four workers per CPU
// (directory walking is I/O- and syscall-bound), clamped to [4, 64].
func DefaultWorkers() int { return min(max(runtime.NumCPU()*4, 4), 64) }

// Options configures a Scan.
type Options struct {
	// Depth is the level, relative to the root, at which sizes are
	// aggregated into entries. 0 produces totals only.
	Depth int
	// Workers is the number of concurrent directory readers; values < 1
	// mean DefaultWorkers().
	Workers int
	// Exclude lists base names or filepath.Match patterns; matching files
	// and directories are skipped and excluded subtrees are not walked.
	Exclude []string
	// Files switches from directory aggregation to collecting the largest
	// individual files.
	Files bool
	// FileLimit keeps only the N largest files when Files is set; 0 keeps all.
	FileLimit int
	// MinFileSize drops files smaller than this from the Files listing
	// (they are still counted in the totals).
	MinFileSize int64
	// MaxErrors caps the number of per-entry errors kept in Result.Errors;
	// 0 means DefaultMaxErrors.
	MaxErrors int
	// Scanned, when non-nil, is incremented with the number of entries seen
	// so far; it is meant for live progress display.
	Scanned *atomic.Int64

	// onDir is a test hook called before each directory is read.
	onDir func(path string)
}

// Entry is one row of the result: a file or a directory with its recursive
// totals.
type Entry struct {
	// Path is relative to the root, slash-separated.
	Path string `json:"path"`
	// Name is the base name.
	Name string `json:"name"`
	// IsDir reports whether the entry is a directory.
	IsDir bool `json:"is_dir"`
	// Bytes is the recursive apparent size.
	Bytes int64 `json:"bytes"`
	// Files is the recursive file count (1 for a file).
	Files int64 `json:"files"`
	// Dirs is the number of directories beneath the entry.
	Dirs int64 `json:"dirs"`
	// Percent is Bytes as a share of the scanned total (0-100).
	Percent float64 `json:"percent"`
}

// EntryError records an entry that could not be read.
type EntryError struct {
	// Path is relative to the root, slash-separated.
	Path string `json:"path"`
	// Error is the reason; permission errors are normalized to
	// "permission denied" on every OS.
	Error string `json:"error"`
}

// Result is the raw outcome of a Scan.
type Result struct {
	// Root is the absolute path that was scanned.
	Root string
	// IsDir reports whether the root is a directory.
	IsDir bool
	// TotalBytes, Files and Dirs are totals beneath the root (the root
	// directory itself is not counted in Dirs).
	TotalBytes int64
	Files      int64
	Dirs       int64
	// Entries holds, in no particular order, either the entries at the
	// requested depth (plus shallower files and empty directories, so that
	// they partition the total) or, in Files mode, the largest files.
	Entries []Entry
	// Skipped counts entries that could not be read.
	Skipped int
	// Errors lists up to MaxErrors of the skipped entries, sorted by path.
	Errors []EntryError
	// Elapsed is the wall-clock scan duration.
	Elapsed time.Duration
}

// ValidateExcludes checks that every exclude pattern is well-formed.
func ValidateExcludes(patterns []string) error {
	_, err := newMatcher(patterns)
	return err
}

// Scan walks root and aggregates sizes according to opts. A missing or
// unreadable root is an error; unreadable entries below it are counted in
// Result.Skipped. Scan returns ctx.Err() promptly when ctx is canceled.
func Scan(ctx context.Context, root string, opts Options) (*Result, error) {
	start := time.Now()
	if opts.Workers < 1 {
		opts.Workers = DefaultWorkers()
	}
	if opts.Depth < 0 {
		opts.Depth = 0
	}
	if opts.MaxErrors <= 0 {
		opts.MaxErrors = DefaultMaxErrors
	}
	match, err := newMatcher(opts.Exclude)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs) // an explicit root symlink is followed
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return scanFile(abs, info, opts, start), nil
	}

	prefix := abs
	if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
		prefix += string(os.PathSeparator)
	}
	s := &scanner{
		ctx:       ctx,
		opts:      opts,
		match:     match,
		prefixLen: len(prefix),
		q:         newQueue(),
		links:     &linkSet{seen: map[fileID]struct{}{}},
	}
	s.q.push(job{path: abs})
	stop := context.AfterFunc(ctx, s.q.close)
	defer stop()

	workers := make([]*worker, opts.Workers)
	var wg sync.WaitGroup
	for i := range workers {
		w := &worker{s: s, top: &topFiles{limit: opts.FileLimit}}
		workers[i] = w
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop()
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.fatal != nil {
		return nil, s.fatal
	}
	res := s.merge(workers)
	res.Root = abs
	res.IsDir = true
	res.Elapsed = time.Since(start)
	return res, nil
}

// scanFile handles a root that is not a directory.
func scanFile(abs string, info fs.FileInfo, opts Options, start time.Time) *Result {
	res := &Result{Root: abs, TotalBytes: info.Size(), Files: 1, Errors: []EntryError{}}
	e := Entry{Path: info.Name(), Name: info.Name(), Bytes: info.Size(), Files: 1}
	if (opts.Files && info.Size() >= opts.MinFileSize) || (!opts.Files && opts.Depth > 0) {
		res.Entries = []Entry{e}
	}
	if opts.Scanned != nil {
		opts.Scanned.Add(1)
	}
	res.Elapsed = time.Since(start)
	return res
}

// job is one directory waiting to be read.
type job struct {
	path  string
	depth int
	// acc is the listed ancestor-or-self at the aggregation depth that
	// receives this directory's sizes (nil above the aggregation depth).
	acc *node
	// self is this directory's own node when it is at or above the
	// aggregation depth.
	self *node
}

// node is a candidate output entry at or above the aggregation depth.
type node struct {
	rel         string
	name        string
	depth       int
	isDir       bool
	hasChildren bool // written only by the job reading this directory
	bytes       atomic.Int64
	files       atomic.Int64
	dirs        atomic.Int64
}

type scanner struct {
	ctx       context.Context
	opts      Options
	match     *matcher
	prefixLen int
	q         *queue
	links     *linkSet
	// fatal is set only by the worker reading the root directory and read
	// after all workers have exited.
	fatal error
}

type worker struct {
	s       *scanner
	bytes   int64
	files   int64
	dirs    int64
	nodes   []*node
	top     *topFiles
	errs    []EntryError
	skipped int
	subdirs []job
}

func (w *worker) loop() {
	for {
		j, ok := w.s.q.pop()
		if !ok {
			return
		}
		if w.s.ctx.Err() == nil {
			w.scanDir(j)
		}
		w.s.q.done()
	}
}

func joinPath(dir, name string) string {
	if strings.HasSuffix(dir, string(os.PathSeparator)) {
		return dir + name
	}
	return dir + string(os.PathSeparator) + name
}

func (w *worker) scanDir(j job) {
	s := w.s
	if s.opts.onDir != nil {
		s.opts.onDir(j.path)
	}
	f, err := os.Open(j.path)
	if err != nil {
		w.fail(j, err)
		return
	}
	defer f.Close()

	var bytes, files, dirs int64
	w.subdirs = w.subdirs[:0]
	childDepth := j.depth + 1
	listed := !s.opts.Files && childDepth <= s.opts.Depth
	for {
		ents, rerr := f.ReadDir(readBatch)
		for _, e := range ents {
			name := e.Name()
			if s.match.excluded(name) {
				continue
			}
			if j.self != nil {
				j.self.hasChildren = true
			}
			t := e.Type()
			// Symlinks (and, on Windows, junctions/mount points) never carry
			// ModeDir in a DirEntry, so they are not traversed.
			if t.IsDir() {
				dirs++
				p := joinPath(j.path, name)
				child := job{path: p, depth: childDepth, acc: j.acc}
				if listed {
					n := &node{rel: p[s.prefixLen:], name: name, depth: childDepth, isDir: true}
					child.self = n
					if childDepth == s.opts.Depth {
						child.acc = n
					}
					w.nodes = append(w.nodes, n)
				}
				w.subdirs = append(w.subdirs, child)
				continue
			}
			// Regular files, symlinks (their own lstat size, never followed),
			// devices, sockets, ... all count as files.
			info, ierr := e.Info()
			if ierr != nil {
				w.record(joinPath(j.path, name), ierr)
				continue
			}
			size := info.Size()
			if t.IsRegular() {
				if id, ok := linkID(info); ok && !s.links.first(id) {
					continue // another hard link to this file was already counted
				}
			}
			files++
			bytes += size
			if listed {
				p := joinPath(j.path, name)
				n := &node{rel: p[s.prefixLen:], name: name, depth: childDepth}
				n.bytes.Store(size)
				n.files.Store(1)
				w.nodes = append(w.nodes, n)
			}
			if s.opts.Files && size >= s.opts.MinFileSize && w.top.wants(size) {
				p := joinPath(j.path, name)
				w.top.add(fileItem{path: p[s.prefixLen:], bytes: size})
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				w.fail(j, rerr)
			}
			break
		}
		if s.ctx.Err() != nil {
			break
		}
	}

	if j.acc != nil {
		j.acc.bytes.Add(bytes)
		j.acc.files.Add(files)
		j.acc.dirs.Add(dirs)
	}
	w.bytes += bytes
	w.files += files
	w.dirs += dirs
	if s.opts.Scanned != nil {
		s.opts.Scanned.Add(files + dirs)
	}
	if len(w.subdirs) > 0 {
		s.q.push(w.subdirs...)
	}
}

// fail records an error reading directory j; at the root it is fatal.
func (w *worker) fail(j job, err error) {
	if j.depth == 0 {
		w.s.fatal = err
		return
	}
	w.record(j.path, err)
}

func (w *worker) record(path string, err error) {
	w.skipped++
	if len(w.errs) >= w.s.opts.MaxErrors {
		return
	}
	rel := path
	if len(path) >= w.s.prefixLen {
		rel = path[w.s.prefixLen:]
	}
	w.errs = append(w.errs, EntryError{Path: filepath.ToSlash(rel), Error: reason(err)})
}

// reason turns err into a short, OS-independent message.
func reason(err error) string {
	if errors.Is(err, fs.ErrPermission) {
		return "permission denied"
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "no such file or directory"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func (s *scanner) merge(workers []*worker) *Result {
	res := &Result{Errors: []EntryError{}}
	var items []fileItem
	for _, w := range workers {
		res.TotalBytes += w.bytes
		res.Files += w.files
		res.Dirs += w.dirs
		res.Skipped += w.skipped
		res.Errors = append(res.Errors, w.errs...)
		for _, n := range w.nodes {
			if n.depth == s.opts.Depth || !n.isDir || !n.hasChildren {
				res.Entries = append(res.Entries, Entry{
					Path:  filepath.ToSlash(n.rel),
					Name:  n.name,
					IsDir: n.isDir,
					Bytes: n.bytes.Load(),
					Files: n.files.Load(),
					Dirs:  n.dirs.Load(),
				})
			}
		}
		items = append(items, w.top.items...)
	}
	slices.SortFunc(res.Errors, func(a, b EntryError) int { return strings.Compare(a.Path, b.Path) })
	if len(res.Errors) > s.opts.MaxErrors {
		res.Errors = res.Errors[:s.opts.MaxErrors]
	}
	if s.opts.Files {
		slices.SortFunc(items, compareItems)
		if s.opts.FileLimit > 0 && len(items) > s.opts.FileLimit {
			items = items[:s.opts.FileLimit]
		}
		res.Entries = make([]Entry, len(items))
		for i, it := range items {
			res.Entries[i] = Entry{
				Path:  filepath.ToSlash(it.path),
				Name:  filepath.Base(it.path),
				Bytes: it.bytes,
				Files: 1,
			}
		}
	}
	return res
}

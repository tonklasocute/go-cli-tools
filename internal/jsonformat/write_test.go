package jsonformat

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func formatter(opt Options) func(io.Reader, io.Writer) error {
	return func(src io.Reader, dst io.Writer) error { return Format(context.Background(), src, dst, opt) }
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.json")
	if err := os.WriteFile(path, []byte(`{"b":1,"a":[1,2]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)

	changed, err := WriteFileAtomic(path, formatter(Options{Indent: "  ", SortKeys: true}))
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(path)
	if want := "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": 1\n}\n"; string(got) != want {
		t.Fatalf("content:\n%s", got)
	}
	after, _ := os.Stat(path)
	if runtime.GOOS != "windows" && after.Mode().Perm() != before.Mode().Perm() {
		t.Errorf("mode %v, want %v", after.Mode().Perm(), before.Mode().Perm())
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Errorf("leftover files: %v", names)
	}

	// already formatted: reported unchanged and left alone
	changed, err = WriteFileAtomic(path, formatter(Options{Indent: "  ", SortKeys: true}))
	if err != nil || changed {
		t.Fatalf("second run changed=%v err=%v", changed, err)
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Errorf("leftover files: %v", names)
	}
}

func TestWriteFileAtomicFailureLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	orig := "{\n  \"a\": 1\n  \"b\": 2\n}\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := WriteFileAtomic(path, formatter(Options{Indent: "  "}))
	asSyntax(t, err)
	got, _ := os.ReadFile(path)
	if string(got) != orig {
		t.Fatalf("original modified:\n%s", got)
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Errorf("temp file left behind: %v", names)
	}

	// a producer that writes something and then fails
	boom := errors.New("boom")
	_, err = WriteFileAtomic(path, func(_ io.Reader, w io.Writer) error {
		_, _ = io.WriteString(w, "garbage")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Fatal("original modified by failed producer")
	}
}

func TestWriteFileAtomicErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteFileAtomic(filepath.Join(dir, "missing.json"), formatter(Options{})); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	if _, err := WriteFileAtomic(dir, formatter(Options{})); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("dir: %v", err)
	}
}

func TestWriteFileAtomicFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "link.json")
	if err := os.WriteFile(target, []byte(`[1,2]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := WriteFileAtomic(link, formatter(Options{})); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced by a regular file: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "[1,2]\n" {
		t.Fatalf("target content %q", got)
	}
}

func TestSameContents(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	big := strings.Repeat("x", 100000)
	a, b, c, d := write("a", big), write("b", big), write("c", big[:99999]+"y"), write("d", "short")
	for _, tt := range []struct {
		x, y string
		want bool
	}{{a, b, true}, {a, c, false}, {a, d, false}} {
		got, err := sameContents(tt.x, tt.y)
		if err != nil || got != tt.want {
			t.Errorf("%s vs %s: %v %v", tt.x, tt.y, got, err)
		}
	}
	if _, err := sameContents(a, filepath.Join(dir, "nope")); err == nil {
		t.Error("want error for missing file")
	}
}

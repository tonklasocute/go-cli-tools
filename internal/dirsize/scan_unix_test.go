//go:build unix

package dirsize

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHardLinksCountedOnce(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]int64{"a/orig.bin": 4096, "b/other.bin": 10})
	if err := os.Link(filepath.Join(root, "a", "orig.bin"), filepath.Join(root, "b", "link.bin")); err != nil {
		t.Skipf("hard links unsupported: %v", err)
	}
	for _, workers := range []int{1, 8} {
		res, err := Scan(context.Background(), root, Options{Depth: 1, Workers: workers})
		if err != nil {
			t.Fatal(err)
		}
		if res.TotalBytes != 4106 || res.Files != 2 {
			t.Errorf("workers=%d: total=%d files=%d, want 4106 bytes in 2 files", workers, res.TotalBytes, res.Files)
		}
	}
}

func TestPermissionDeniedSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks are bypassed")
	}
	root := t.TempDir()
	makeTree(t, root, map[string]int64{"ok/a.bin": 100, "locked/secret/b.bin": 5000, "locked2/c.bin": 1})
	for _, d := range []string{"locked", "locked2"} {
		p := filepath.Join(root, d)
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
	}
	res, err := Scan(context.Background(), root, Options{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 2 || len(res.Errors) != 2 {
		t.Fatalf("skipped=%d errors=%v, want 2", res.Skipped, res.Errors)
	}
	if res.Errors[0].Path != "locked" || res.Errors[0].Error != "permission denied" {
		t.Errorf("errors[0] = %+v", res.Errors[0])
	}
	if res.TotalBytes != 100 || res.Dirs != 3 {
		t.Errorf("total=%d dirs=%d, want 100 bytes, 3 dirs", res.TotalBytes, res.Dirs)
	}

	// An unreadable root is fatal.
	_, err = Scan(context.Background(), filepath.Join(root, "locked"), Options{})
	if err == nil {
		t.Error("unreadable root: want error")
	}
}

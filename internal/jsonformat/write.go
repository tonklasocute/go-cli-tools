package jsonformat

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces the contents of path with what produce writes.
// produce receives the current contents and the destination. The new data
// goes to a temporary file in the same directory which is synced, given the
// original file's permissions and renamed over the original, so readers see
// either the old or the new file and a failure leaves the original
// untouched. Symlinks are followed: the link target is replaced.
//
// It reports whether the contents changed; an unchanged file is left alone
// (its modification time is preserved).
func WriteFileAtomic(path string, produce func(src io.Reader, dst io.Writer) error) (changed bool, err error) {
	// Opening first gives the familiar "open <path>: ..." error for missing
	// or unreadable files.
	probe, err := os.Open(path)
	if err != nil {
		return false, err
	}
	_ = probe.Close()
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s: not a regular file", path)
	}
	src, err := os.Open(target)
	if err != nil {
		return false, err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	bw := bufio.NewWriterSize(tmp, 64<<10)
	if err := produce(src, bw); err != nil {
		return false, err
	}
	if err := bw.Flush(); err != nil {
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	same, err := sameContents(target, tmpName)
	if err != nil {
		return false, err
	}
	if same {
		return false, nil
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return false, err
	}
	if err := src.Close(); err != nil { // Windows cannot replace an open file
		return false, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return false, err
	}
	keep = true
	return true, nil
}

// sameContents compares two files byte by byte without loading them whole.
func sameContents(a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()
	ia, err := fa.Stat()
	if err != nil {
		return false, err
	}
	ib, err := fb.Stat()
	if err != nil {
		return false, err
	}
	if ia.Size() != ib.Size() {
		return false, nil
	}
	ra, rb := bufio.NewReader(fa), bufio.NewReader(fb)
	bufA, bufB := make([]byte, 32<<10), make([]byte, 32<<10)
	for {
		na, errA := io.ReadFull(ra, bufA)
		nb, errB := io.ReadFull(rb, bufB)
		if !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		doneA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		doneB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)
		if errA != nil && !doneA {
			return false, errA
		}
		if errB != nil && !doneB {
			return false, errB
		}
		if doneA || doneB {
			return doneA && doneB, nil
		}
	}
}

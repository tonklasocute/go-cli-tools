//go:build unix

package dirsize

import (
	"io/fs"
	"syscall"
)

// fileID identifies a file independently of its name (device + inode).
type fileID struct {
	dev uint64
	ino uint64
}

// linkID returns the (device, inode) identity of a file that has more than
// one hard link. Singly-linked files report ok=false and need no dedupe.
func linkID(info fs.FileInfo) (fileID, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink <= 1 {
		return fileID{}, false
	}
	return fileID{dev: toU64(st.Dev), ino: toU64(st.Ino)}, true
}

// toU64 converts the platform-specific Stat_t field types (int32 on darwin,
// uint64 on linux, ...) without tripping per-platform conversion lints.
// The values are only used as an opaque map key, so sign reinterpretation
// is harmless.
func toU64[T ~int32 | ~uint32 | ~int64 | ~uint64](v T) uint64 { return uint64(v) }

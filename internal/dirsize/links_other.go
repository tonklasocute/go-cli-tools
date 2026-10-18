//go:build !unix

package dirsize

import "io/fs"

// fileID is empty: there is no portable file identity on this platform.
type fileID struct{}

// linkID always reports ok=false on platforms without a portable inode
// number (e.g. Windows): every hard link is counted separately there.
func linkID(fs.FileInfo) (fileID, bool) { return fileID{}, false }

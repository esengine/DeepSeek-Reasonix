//go:build !darwin && !linux

package observation

import "io/fs"

// stampOf has no change time here, so a write that restores the previous size
// and modification time goes unseen.
func stampOf(info fs.FileInfo) stamp {
	return stamp{Size: info.Size(), Mtime: info.ModTime().UnixNano()}
}

//go:build !windows

package storage

import "golang.org/x/sys/unix"

// readVolume asks the filesystem holding dir for its free and total bytes.
// Bavail, not Bfree: the reserved blocks a filesystem keeps for root are not
// space a move can use.
func readVolume(dir string) Volume {
	vol := Volume{Path: dir}
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return vol
	}
	vol.Free = int64(st.Bavail) * int64(st.Bsize)
	vol.Total = int64(st.Blocks) * int64(st.Bsize)
	return vol
}

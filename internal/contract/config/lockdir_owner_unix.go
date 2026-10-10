//go:build !windows

package config

import (
	"os"
	"syscall"
)

// lockDirCurrentUID is the uid the lock directory must belong to; tests replace it.
var lockDirCurrentUID = os.Getuid

// lockDirForeignOwner reports the owner uid of info and whether it differs from
// the current user.
func lockDirForeignOwner(info os.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	owner := int(st.Uid)
	return owner, owner != lockDirCurrentUID()
}

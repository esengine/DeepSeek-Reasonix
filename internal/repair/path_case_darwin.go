//go:build darwin

package repair

import (
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/text/unicode/norm"
)

const darwinPathconfCaseSensitive = 11

func platformRepairPathCaseInsensitive(path string) bool {
	parent := existingRepairPathParent(path)
	if parent == "" {
		return false
	}
	caseSensitive, err := syscall.Pathconf(parent, darwinPathconfCaseSensitive)
	return err == nil && caseSensitive == 0
}

func platformRepairPathUnicodeNormalized(path string) string {
	parent := existingRepairPathParent(path)
	if parent == "" {
		return path
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(parent, &stat); err != nil {
		return path
	}
	fsType := strings.TrimRight(string(stat.Fstypename[:]), "\x00")
	if fsType != "apfs" && fsType != "hfs" {
		return path
	}
	return norm.NFD.String(path)
}

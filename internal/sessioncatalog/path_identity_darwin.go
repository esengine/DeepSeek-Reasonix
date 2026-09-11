//go:build darwin

package sessioncatalog

import (
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/text/unicode/norm"
)

const darwinCatalogPathconfCaseSensitive = 11

func platformCatalogPathIdentity(path string) string {
	path = platformCatalogPathUnicodeNormalized(path)
	if platformCatalogPathCaseInsensitive(path) {
		path = strings.ToLower(path)
	}
	return path
}

func platformCatalogPathCaseInsensitive(path string) bool {
	parent := existingCatalogPathParent(path)
	if parent == "" {
		return false
	}
	caseSensitive, err := syscall.Pathconf(parent, darwinCatalogPathconfCaseSensitive)
	return err == nil && caseSensitive == 0
}

func platformCatalogPathUnicodeNormalized(path string) string {
	parent := existingCatalogPathParent(path)
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

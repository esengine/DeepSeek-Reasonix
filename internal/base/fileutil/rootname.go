package fileutil

import (
	"path/filepath"
	"strings"
)

// RootName is what to call a workspace root in a list a person reads.
// filepath.Base answers a separator for a volume root — "D:\" and "/" both come
// back as "\" on Windows — so a workspace opened at a drive root was listed,
// tabbed and titled as a lone backslash, which names nothing.
func RootName(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if base := filepath.Base(root); base != string(filepath.Separator) && base != "/" && base != "." {
		return base
	}
	if vol := filepath.VolumeName(root); vol != "" {
		return vol + string(filepath.Separator)
	}
	return string(filepath.Separator)
}

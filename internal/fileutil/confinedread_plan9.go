//go:build plan9

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func OpenFileBeneath(root, rel string) (*os.File, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, fmt.Errorf("workspace root is empty")
	}
	rel, err := confinedRelativePath(rel)
	if err != nil {
		return nil, err
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, target)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("file resolves outside workspace root")
	}
	return os.Open(target)
}

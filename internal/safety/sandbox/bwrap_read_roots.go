//go:build !darwin && !windows

package sandbox

import (
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
)

func bwrapReadRootArgs(spec Spec) []string {
	var out []string
	seen := map[string]bool{}
	for _, root := range spec.ReadRoots {
		if !filepath.IsAbs(root) {
			continue
		}
		path, err := filepath.EvalSymlinks(root)
		if err != nil || path == "/tmp" || !fileutil.Under(path, "/tmp") || seen[path] {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[path] = true
		out = append(out, "--ro-bind", path, path)
	}
	return out
}

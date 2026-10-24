package boot

import (
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/writeclaim"
)

// isolatedReadPolicy keeps source denies and adds their candidate aliases.
// Rebinding only cwd or relative rules would let an absolute source rule miss
// its copied file, or let a relative rule stop protecting the source file.
func isolatedReadPolicy(cfg *config.Config, source, target string) ([]string, error) {
	return rebindReadPolicy(RuntimeForbidReadRoots(cfg, source), source, target)
}

func rebindReadPolicy(roots []string, source, target string) ([]string, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	sourceReal, err := filepath.EvalSymlinks(sourceAbs)
	if err != nil {
		return nil, err
	}
	result := append([]string(nil), roots...)
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if os.IsNotExist(err) {
			resolved, err = fileutil.CanonicalWritePath(absolute)
		}
		if err != nil {
			return nil, err
		}
		result = appendUniquePaths(result, resolved)
		for _, alias := range [][2]string{{sourceAbs, absolute}, {sourceReal, resolved}} {
			if writeclaim.PathWithin(alias[1], alias[0]) {
				result = appendUniquePaths(result, target)
				continue
			}
			if !writeclaim.PathWithin(alias[0], alias[1]) {
				continue
			}
			relative, err := filepath.Rel(alias[0], alias[1])
			if err != nil {
				return nil, err
			}
			result = appendUniquePaths(result, filepath.Join(target, relative))
		}
	}
	return result, nil
}

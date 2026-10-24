package isolation

import (
	"path/filepath"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

// MapSourcePaths projects concrete host paths between code copies. It grants
// no access; callers must authorize the returned paths in the target source.
func MapSourcePaths(candidate, source string, paths []string) ([]string, error) {
	root, err := fileutil.CanonicalWritePath(candidate)
	if err != nil {
		return nil, err
	}
	target, err := fileutil.CanonicalWritePath(source)
	if err != nil {
		return nil, err
	}
	mapped := make([]string, 0, len(paths))
	for _, path := range paths {
		canonical, err := fileutil.CanonicalWritePath(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, canonical)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "integration path is outside its direct source workspace"}
		}
		projected, err := fileutil.CanonicalWritePath(filepath.Join(target, rel))
		if err != nil {
			return nil, err
		}
		within, err := filepath.Rel(target, projected)
		if err != nil || !filepath.IsLocal(within) {
			return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "integration path escapes its ancestor source workspace"}
		}
		mapped = append(mapped, projected)
	}
	return mapped, nil
}

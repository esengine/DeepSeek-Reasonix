package pluginpkg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
)

// WalkProfileSources visits bounded resident sources in declared profile roots.
// A visitor accepting a directory profile stops descent into that directory.
// Paths are package-relative; the opened root owns every filesystem operation.
func (p Package) WalkProfileSources(visit func(path string, body []byte) bool) {
	root, err := os.OpenRoot(p.Root)
	if err != nil {
		return
	}
	defer root.Close()
	for _, group := range []struct {
		paths []string
		flat  bool
	}{{p.Manifest.Skills, true}, {p.Manifest.Agents, false}} {
		paths := slices.Clone(group.paths)
		slices.Sort(paths)
		for _, raw := range paths {
			rel, err := cleanPortableRelativePath(raw)
			if err != nil || strings.TrimSpace(raw) == "" || !filepath.IsLocal(rel) {
				continue
			}
			rel = filepath.FromSlash(rel)
			info, file := agentPathInfo(root, rel)
			if file == nil {
				continue
			}
			file.Close()
			if !info.IsDir() {
				stem := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
				if group.flat && strings.EqualFold(filepath.Ext(rel), ".md") && config.IsValidSkillName(stem) {
					if body := agentSourceBody(root, rel); body != nil {
						visit(rel, body)
					}
				}
				continue
			}
			var seen []os.FileInfo
			scanProfileSources(root, rel, 1, &seen, func(path, _ string, body []byte, _ int) bool {
				return visit(path, body)
			})
		}
	}
}

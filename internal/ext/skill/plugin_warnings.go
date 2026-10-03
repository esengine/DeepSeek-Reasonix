package skill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/frontmatter"
	"reasonix/internal/ext/pluginpkg"
)

// PluginWarnings reports declarations that prevent an installed profile from
// loading, using the same delivery rules as the skill store.
func PluginWarnings(pkg pluginpkg.Package) []string {
	var paths []string
	inv := pkg.Inventory()
	for _, ref := range inv.Skills {
		paths = append(paths, ref.Path)
	}
	for _, ref := range inv.Agents {
		paths = append(paths, ref.Path)
	}
	st := &Store{maxDepth: normalizeMaxDepth(5), stderr: io.Discard, suppressWarnings: true}
	for _, root := range append(pkg.SkillRoots(), pkg.AgentRoots()...) {
		st.pluginProfilePaths(root, 1, map[string]bool{}, &paths)
	}
	seen := map[string]bool{}
	var warnings []string
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		rel, err := filepath.Rel(pkg.Root, path)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		raw, err := fileencoding.ReadFileUTF8(path)
		if err == nil {
			doc, _ := frontmatter.Parse(string(raw))
			_, err = deliveryFromDocument(doc)
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", filepath.ToSlash(rel), err))
		}
	}
	return warnings
}

func (s *Store) pluginProfilePaths(dir string, depth int, seen map[string]bool, out *[]string) {
	key := filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		key = filepath.Clean(resolved)
	}
	if seen[key] {
		return
	}
	seen[key] = true
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if path, _, _, ok := profileEntry(dir, entry); ok {
			*out = append(*out, path)
		}
		if _, ok := s.readEntry(dir, ScopeCustom, false, entry); ok {
			continue
		}
		if depth < s.maxDepth && s.canScanChildDir(dir, entry) {
			s.pluginProfilePaths(filepath.Join(dir, entry.Name()), depth+1, seen, out)
		}
	}
}

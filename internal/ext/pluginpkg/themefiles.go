package pluginpkg

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
)

// ThemeRef is one theme file (*.reasonix-theme) a plugin contributes,
// resolved from the manifest's themes list (plain paths and globs).
type ThemeRef struct {
	Name string
	Path string
}

// ThemeFiles resolves the manifest's themes list (plain paths and
// per-segment globs) to concrete theme files. Parse-time validation has
// already rejected escapes and non-regular files, so unreadable entries
// here simply drop out (they were reported as parse warnings).
func (p Package) ThemeFiles() []ThemeRef {
	seen := map[string]bool{}
	var out []ThemeRef
	for _, pattern := range p.Manifest.Themes {
		var matches []string
		if hasGlobMeta(pattern) {
			matches, _ = globThemePattern(p.Root, pattern)
		} else {
			abs := filepath.Join(p.Root, filepath.FromSlash(pattern))
			if info, err := os.Stat(abs); err == nil && info.Mode().IsRegular() {
				matches = []string{abs}
			}
		}
		for _, match := range matches {
			match = filepath.Clean(match)
			if seen[match] {
				continue
			}
			seen[match] = true
			out = append(out, ThemeRef{
				Name: themeFileName(match),
				Path: match,
			})
		}
	}
	slices.SortStableFunc(out, func(a, b ThemeRef) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Path, b.Path))
	})
	return out
}

package pluginpkg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
)

func scanProfileSources(root *os.Root, dir string, depth int, seen *[]os.FileInfo, visit func(path, stem string, body []byte, depth int) bool) {
	info, source := agentPathInfo(root, dir)
	if source == nil {
		return
	}
	defer source.Close()
	if !info.IsDir() {
		return
	}
	for _, previous := range *seen {
		if os.SameFile(previous, info) {
			return
		}
	}
	*seen = append(*seen, info)
	entries, err := source.ReadDir(-1)
	if err != nil {
		return
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		entryInfo, handle := agentPathInfo(root, path)
		if handle == nil {
			continue
		}
		handle.Close()
		var profile, stem string
		if entryInfo.IsDir() {
			profile, stem = filepath.Join(path, "SKILL.md"), entry.Name()
		} else if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			profile, stem = path, strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		if profile != "" && config.IsValidSkillName(stem) {
			if body := agentSourceBody(root, profile); body != nil && visit(profile, stem, body, depth) {
				continue
			}
		}
		if entryInfo.IsDir() && depth < (&config.Config{}).SkillMaxDepth() && !shouldSkipSkillScanDir(entry.Name()) {
			scanProfileSources(root, path, depth+1, seen, visit)
		}
	}
}

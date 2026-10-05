package installsource

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
)

// resolveSkillPath finds a previously installed flat or directory skill.
func (t *Tool) resolveSkillPath(name, scope string) (string, bool) {
	key := config.SkillNameKey(name)
	if key == "" {
		return "", false
	}
	var root string
	if scope == "global" {
		if t.reasonixHome == "" {
			return "", false
		}
		root = filepath.Join(t.reasonixHome, skill.SkillsDirname)
	} else {
		root = filepath.Join(t.root, ".reasonix", skill.SkillsDirname)
	}
	flat := filepath.Join(root, name+".md")
	if _, err := lstat(flat); err == nil {
		return flat, true
	}
	dir := filepath.Join(root, name)
	if _, err := lstat(filepath.Join(dir, skill.SkillFile)); err == nil {
		return dir, true
	}
	// macOS can keep a decomposed spelling on disk for an NFC skill name.
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		entryName := entry.Name()
		stem := strings.TrimSuffix(entryName, filepath.Ext(entryName))
		if strings.EqualFold(filepath.Ext(entryName), ".md") && config.SkillNameKey(stem) == key {
			return filepath.Join(root, entryName), true
		}
		if config.SkillNameKey(entryName) == key {
			candidate := filepath.Join(root, entryName)
			if _, err := lstat(filepath.Join(candidate, skill.SkillFile)); err == nil {
				return candidate, true
			}
		}
	}
	return "", false
}

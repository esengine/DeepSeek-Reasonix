package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ruleCSSImportant = "css-important"
	ruleCSSSize      = "css-size"
	ruleStrayFile    = "stray-file"
)

const stylesDir = "desktop/frontend-next/src/styles"

// The two layered sheets grow by accretion and are exempt from the 800-line
// ceiling; their budget only stops the growth.
var cssSizeBudgeted = map[string]bool{
	stylesDir + "/app.css":    true,
	stylesDir + "/studio.css": true,
}

var (
	cssComment   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssImportant = regexp.MustCompile(`!\s*important`)
)

func countImportant(css string) int {
	return len(cssImportant.FindAllString(cssComment.ReplaceAllString(css, ""), -1))
}

// checkCSS weighs each stylesheet by its `!important` declarations, and the two
// layered sheets by their length, so the baseline records today's count and
// anything above it fails.
func checkCSS(root string) []Finding {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(stylesDir)))
	if err != nil {
		return nil
	}
	var out []Finding
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		rel := stylesDir + "/" + e.Name()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		if n := countImportant(string(data)); n > 0 {
			out = append(out, Finding{rel, 1, ruleCSSImportant,
				"stylesheet carries !important declarations; win the cascade by source order or specificity instead", n})
		}
		if cssSizeBudgeted[rel] {
			if n := len(splitLines(data)); n > 0 {
				out = append(out, Finding{rel, 1, ruleCSSSize, "stylesheet length is held at its recorded budget", n})
			}
		}
	}
	return out
}

// checkStrayFiles rejects editor and patch leftovers: `sed -i` on macOS writes
// `<name>-e` beside the file it edited.
func checkStrayFiles(root string) []Finding {
	var out []Finding
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, "-e") || strings.HasSuffix(name, ".orig") || strings.HasSuffix(name, ".rej") {
			rel, _ := filepath.Rel(root, path)
			out = append(out, Finding{filepath.ToSlash(rel), 1, ruleStrayFile,
				"leftover backup or patch file; delete it", 1})
		}
		return nil
	})
	return out
}

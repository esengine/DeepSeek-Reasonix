// Package builtincontent holds shipped skill markdown that is embedded into the
// Reasonix binary. Bodies stay out of the system prompt until the skill is
// invoked; only the name+description index line is cache-stable.
package builtincontent

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"reasonix/internal/frontmatter"
)

//go:embed reasonix-guide/SKILL.md
var files embed.FS

// SkillMarkdown is one embedded skill file after frontmatter split.
type SkillMarkdown struct {
	Name        string
	Description string
	RunAs       string // "inline" | "subagent" (and cross-tool aliases handled by caller)
	Context     string
	Agent       string
	Body        string
	Path        string // stable embed path for diagnostics
	Frontmatter map[string]string
}

// LoadReasonixGuide returns the shipped reasonix-guide skill markdown.
func LoadReasonixGuide() (SkillMarkdown, error) {
	return loadSkill("reasonix-guide/SKILL.md")
}

// All loads every embedded SKILL.md under this package (currently one).
func All() ([]SkillMarkdown, error) {
	var out []SkillMarkdown
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(path.Base(p), "SKILL.md") {
			return nil
		}
		sk, err := loadSkill(p)
		if err != nil {
			return err
		}
		out = append(out, sk)
		return nil
	})
	return out, err
}

func loadSkill(embedPath string) (SkillMarkdown, error) {
	raw, err := files.ReadFile(embedPath)
	if err != nil {
		return SkillMarkdown{}, err
	}
	return ParseSkillMarkdown(embedPath, string(raw))
}

// ParseSkillMarkdown splits embedded (or test) skill markdown using the same
// frontmatter rules as on-disk skills. It does not expand references/scripts
// (embedded skills ship a single file).
func ParseSkillMarkdown(sourcePath, content string) (SkillMarkdown, error) {
	content = strings.TrimPrefix(strings.ReplaceAll(content, "\r\n", "\n"), "\uFEFF")
	fm, body := frontmatter.Split(content)
	name := strings.TrimSpace(fm["name"])
	if name == "" {
		// Fall back to directory name: reasonix-guide/SKILL.md → reasonix-guide
		dir := path.Dir(sourcePath)
		if dir != "" && dir != "." {
			name = path.Base(dir)
		}
	}
	if name == "" {
		return SkillMarkdown{}, fmt.Errorf("embedded skill %s: missing name", sourcePath)
	}
	return SkillMarkdown{
		Name:        name,
		Description: strings.TrimSpace(fm["description"]),
		RunAs:       strings.TrimSpace(fm["runas"]),
		Context:     strings.TrimSpace(fm["context"]),
		Agent:       strings.TrimSpace(fm["agent"]),
		Body:        strings.TrimSpace(body),
		Path:        sourcePath,
		Frontmatter: fm,
	}, nil
}

// Package persona provides a system for loading and selecting user-defined
// agent personas — markdown files with frontmatter that describe a role,
// optionally pin a model, restrict tool access, and supply a body appended
// to the system prompt.
package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/frontmatter"
)

// Persona represents one selectable agent persona loaded from a .md file.
type Persona struct {
	Name        string
	Description string
	Model       string   // empty = keep current
	Tools       []string // empty = all tools
	Body        string   // Markdown body appended to system prompt
	Path        string   // source file path
}

// conventionDirs mirrors config.ConventionDirs (kept local to avoid an import
// cycle; config imports nothing from here, but this package stays dependency-light).
var conventionDirs = []string{".reasonix", ".agents", ".agent", ".claude"}

// Dirs returns the persona search directories in load order (later wins),
// mirroring command/skill discovery: home convention dirs, then project ones.
func Dirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		for i := len(conventionDirs) - 1; i >= 0; i-- {
			dirs = append(dirs, filepath.Join(home, conventionDirs[i], "personas"))
		}
	}
	for i := len(conventionDirs) - 1; i >= 0; i-- {
		dirs = append(dirs, filepath.Join(".", conventionDirs[i], "personas"))
	}
	return dirs
}

// List scans every dir for .md files, parses them with parseFile, deduplicates
// by lowercase name (later dir wins), and returns the result sorted by name.
func List(dirs []string) []Persona {
	byName := map[string]Persona{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p, ok := parseFile(filepath.Join(dir, e.Name()))
			if !ok {
				continue
			}
			byName[strings.ToLower(p.Name)] = p
		}
	}
	out := make([]Persona, 0, len(byName))
	for _, p := range byName {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Resolve finds the persona named name (case-insensitive) among the given dirs.
// Empty name returns false.
func Resolve(name string, dirs []string) (Persona, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return Persona{}, false
	}
	for _, p := range List(dirs) {
		if strings.ToLower(p.Name) == n {
			return p, true
		}
	}
	return Persona{}, false
}

// Apply appends p.Body to base as a structured Persona section. An empty body
// returns base unchanged.
func Apply(base string, p Persona) string {
	if p.Body == "" {
		return base
	}
	return base + "\n\n# Persona: " + p.Name + "\n\n" + p.Body
}

// parseFile reads one .md file, extracts frontmatter, and returns a Persona.
// A valid "name" key in frontmatter is required.
func parseFile(path string) (Persona, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Persona{}, false
	}
	meta, body := frontmatter.Split(string(b))
	name := meta["name"]
	if name == "" {
		return Persona{}, false
	}
	toolsStr := meta["tools"]
	var tools []string
	if toolsStr != "" {
		for _, t := range strings.Split(toolsStr, ",") {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				tools = append(tools, trimmed)
			}
		}
	}
	return Persona{
		Name:        name,
		Description: meta["description"],
		Model:       meta["model"],
		Tools:       tools,
		Body:        strings.TrimSpace(body),
		Path:        path,
	}, true
}

// DescribeList renders the available personas as a short listing.
func DescribeList(personas []Persona, active string) string {
	var b strings.Builder
	for _, p := range personas {
		suffix := ""
		if strings.EqualFold(p.Name, active) {
			suffix = " (current)"
		}
		scope := "custom"
		fmt.Fprintf(&b, "  %s (%s)%s — %s\n", p.Name, scope, suffix, p.Description)
	}
	b.WriteString("/persona none — 清除角色，回到无人设状态")
	return strings.TrimRight(b.String(), "\n")
}

// Package outputstyle adds a selectable "output style" — a block of persona /
// tone instructions appended to (or replacing) the system prompt — so the user
// can shift how the agent communicates without rewriting the system prompt. It
// mirrors the skill/command loaders: built-in styles plus markdown files with
// frontmatter discovered under the project and home convention dirs.
package outputstyle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/frontmatter"
)

// OutputStyle is one selectable persona. Body is appended to the system prompt
// (KeepCoding true) or used as the whole prompt (false). Name is the selector
// (case-insensitive); Builtin marks the baked-in ones for listing.
type OutputStyle struct {
	Name        string
	Description string
	Body        string
	KeepCoding  bool // true: append to the coding system prompt; false: replace it
	Builtin     bool
	Path        string // file it loaded from ("" for built-ins)
}

// builtins are the always-available styles. Default ("" / "default") is absent
// on purpose — no style means the unmodified system prompt.
var builtins = []OutputStyle{
	{
		Name:        "explanatory",
		Description: "Explain non-obvious implementation choices as you go",
		KeepCoding:  true,
		Builtin:     true,
		Body: "Communication style — Explanatory: as you work, surface the reasoning behind " +
			"non-obvious choices. After a substantive change, add a short \"## Insight\" note " +
			"covering the key trade-off or why an alternative was rejected. Teach the why, not just the what; keep it brief.",
	},
	{
		Name:        "learning",
		Description: "Collaborate and leave TODO(human) stubs for the user to complete",
		KeepCoding:  true,
		Builtin:     true,
		Body: "Communication style — Learning: work collaboratively rather than doing everything. " +
			"When a meaningful implementation decision comes up, pause and ask the user to make the call. " +
			"For the most instructive pieces, write the surrounding code but leave a small, clearly-marked " +
			"`TODO(human)` stub with a one-line description for the user to implement themselves.",
	},
	{
		Name:        "concise",
		Description: "Terse replies: minimal prose, code and bullets only",
		KeepCoding:  true,
		Builtin:     true,
		Body: "Communication style — Concise: keep replies terse. No preamble or postamble, no restating " +
			"the request. Prefer code and short bullet points over paragraphs; answer in the fewest words that are still clear.",
	},
}

// Dirs returns the output-style search directories in load order (later wins),
// mirroring command/skill discovery: home convention dirs, then project ones.
func Dirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		for i := len(conventionDirs) - 1; i >= 0; i-- {
			dirs = append(dirs, filepath.Join(home, conventionDirs[i], "output-styles"))
		}
	}
	for i := len(conventionDirs) - 1; i >= 0; i-- {
		dirs = append(dirs, filepath.Join(".", conventionDirs[i], "output-styles"))
	}
	return dirs
}

// conventionDirs mirrors config.ConventionDirs (kept local to avoid an import
// cycle; config imports nothing from here, but this package stays dependency-light).
var conventionDirs = []string{".reasonix", ".agents", ".agent", ".claude"}

// List returns every available style — built-ins plus the markdown files under
// dirs — deduped by lowercased name, with custom files overriding built-ins.
// Sorted by name. Malformed files are skipped.
func List(dirs []string) []OutputStyle {
	byName := map[string]OutputStyle{}
	for _, b := range builtins {
		byName[strings.ToLower(b.Name)] = b
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			st, ok := parseFile(filepath.Join(dir, e.Name()))
			if !ok {
				continue
			}
			byName[strings.ToLower(st.Name)] = st
		}
	}
	out := make([]OutputStyle, 0, len(byName))
	for _, st := range byName {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Resolve finds the style named name (case-insensitive) among dirs + built-ins.
// An empty or "default" name returns ok=false (no style — leave the prompt as-is).
func Resolve(name string, dirs []string) (OutputStyle, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || n == "default" {
		return OutputStyle{}, false
	}
	for _, st := range List(dirs) {
		if strings.ToLower(st.Name) == n {
			return st, true
		}
	}
	return OutputStyle{}, false
}

// Apply folds a style into a base system prompt: appended when KeepCoding is set,
// otherwise the style replaces the prompt (a pure persona). A style with an empty
// body leaves the base untouched.
func Apply(base string, st OutputStyle) string {
	if strings.TrimSpace(st.Body) == "" {
		return base
	}
	if !st.KeepCoding {
		return st.Body
	}
	if strings.TrimSpace(base) == "" {
		return st.Body
	}
	return base + "\n\n" + st.Body
}

// parseFile loads one <name>.md output-style file. The name is the filename
// stem; frontmatter supplies description and keep-coding-instructions; the body
// is the prompt text.
func parseFile(path string) (OutputStyle, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return OutputStyle{}, false
	}
	meta, body := frontmatter.Split(string(b))
	name := meta["name"]
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return OutputStyle{}, false
	}
	keep := true // default: augment the coding prompt rather than replace it
	if v, ok := meta["keep-coding-instructions"]; ok {
		keep = !isFalse(v)
	}
	return OutputStyle{
		Name:        name,
		Description: meta["description"],
		Body:        body,
		KeepCoding:  keep,
		Path:        path,
	}, true
}

func isFalse(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "false", "no", "0", "off":
		return true
	}
	return false
}

// DescribeList renders the available styles as a short listing for /output-style.
func DescribeList(styles []OutputStyle, active string) string {
	var b strings.Builder
	for _, st := range styles {
		marker := "  "
		if strings.EqualFold(st.Name, active) {
			marker = "* "
		}
		scope := "builtin"
		if !st.Builtin {
			scope = "custom"
		}
		fmt.Fprintf(&b, "%s%s (%s) — %s\n", marker, st.Name, scope, st.Description)
	}
	return strings.TrimRight(b.String(), "\n")
}

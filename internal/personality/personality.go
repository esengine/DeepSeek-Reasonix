// Package personality loads IDENTITY.md / SOUL.md / USER.md files from
// .reasonix/personality/ (project-level) and ~/.config/reasonix/personality/
// (user-level) and folds them into the system prompt — giving the agent a
// durable identity, core behavioural traits, and knowledge about the user.
//
// File lookup order (later wins):
//  1. Project-level: <root>/.reasonix/personality/<file>
//  2. Home/user-level: ~/.config/reasonix/personality/<file>
//
// Merge strategy: each file is independent; if both project and home versions
// exist, the project version takes precedence. When multiple identity/soul/user
// fragments should accumulate, edit the project-level file to include them.
package personality

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName constants.
const (
	FileNameIdentity = "IDENTITY.md"
	FileNameSoul     = "SOUL.md"
	FileNameUser     = "USER.md"
)

// Personality holds the loaded personality fragments.
type Personality struct {
	Identity string // content of IDENTITY.md
	Soul     string // content of SOUL.md
	User     string // content of USER.md
}

// Empty reports whether all fragments are empty.
func (p Personality) Empty() bool {
	return strings.TrimSpace(p.Identity) == "" &&
		strings.TrimSpace(p.Soul) == "" &&
		strings.TrimSpace(p.User) == ""
}

// Load reads the three personality files from the given directories.
// Directories are tried in order — later dirs override earlier ones when
// the same file exists in both (so pass project dir first, then home dir).
// Non-existent files or directories are silently skipped.
func Load(dirs ...string) Personality {
	var p Personality
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if content, ok := readFile(filepath.Join(dir, FileNameIdentity)); ok {
			p.Identity = content
		}
		if content, ok := readFile(filepath.Join(dir, FileNameSoul)); ok {
			p.Soul = content
		}
		if content, ok := readFile(filepath.Join(dir, FileNameUser)); ok {
			p.User = content
		}
	}
	return p
}

// Compose appends personality fragments to the base system prompt.
// Each non-empty fragment is wrapped with a header and folded in.
// The fragments are injected in order: Identity → Soul → User.
func Compose(base string, p Personality) string {
	if p.Empty() {
		return base
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(base))
	b.WriteString("\n\n")

	var parts []string
	if v := strings.TrimSpace(p.Identity); v != "" {
		parts = append(parts, "=== IDENTITY ===\n\n"+v)
	}
	if v := strings.TrimSpace(p.Soul); v != "" {
		parts = append(parts, "=== SOUL ===\n\n"+v)
	}
	if v := strings.TrimSpace(p.User); v != "" {
		parts = append(parts, "=== USER ===\n\n"+v)
	}
	b.WriteString(strings.Join(parts, "\n\n"))

	return b.String()
}

// Dir returns the personality subdirectory under the given convention dir.
func Dir(conventionDir string) string {
	return filepath.Join(conventionDir, "personality")
}

// ProjectDirs returns the personality search directories for a project root:
//  1. <root>/.reasonix/personality
//  2. ~/.config/reasonix/personality   (or the OS equivalent)
func ProjectDirs(root string) []string {
	var dirs []string
	if root != "" {
		for _, cd := range conventionDirs {
			dirs = append(dirs, filepath.Join(root, cd, "personality"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for i := len(homeConventionDirs) - 1; i >= 0; i-- {
			dirs = append(dirs, filepath.Join(home, homeConventionDirs[i], "personality"))
		}
	}
	return dirs
}

// List returns the names of personality files available across all dirs.
// Deduplicated: the first occurrence wins (project over home).
func List(dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name == FileNameIdentity || name == FileNameSoul || name == FileNameUser {
				if !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		}
	}
	return out
}

// ReadFile reads a single personality file, searching across dirs in order.
// Returns the content and true if found.
func ReadFile(name string, dirs []string) (string, bool) {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		content, ok := readFile(filepath.Join(dir, name))
		if ok {
			return content, true
		}
	}
	return "", false
}

// WriteFile writes a personality file to the first writable project dir.
// Returns the path written to.
func WriteFile(name, content string, root string) (string, error) {
	if name != FileNameIdentity && name != FileNameSoul && name != FileNameUser {
		return "", fmt.Errorf("invalid personality file name: %q", name)
	}
	// Write to the project's .reasonix/personality/ dir
	dir := filepath.Join(root, ".reasonix", "personality")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("personality: create dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		return "", fmt.Errorf("personality: write %s: %w", name, err)
	}
	return path, nil
}

// DeleteFile removes a personality file from the project's personality dir.
func DeleteFile(name string, root string) error {
	if name != FileNameIdentity && name != FileNameSoul && name != FileNameUser {
		return fmt.Errorf("invalid personality file name: %q", name)
	}
	path := filepath.Join(root, ".reasonix", "personality", name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("personality: delete %s: %w", name, err)
	}
	return nil
}

// --- helpers ---

func readFile(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	content := strings.TrimSpace(string(b))
	if content == "" {
		return "", false
	}
	return content, true
}

// conventionDirs mirror config.ConventionDirs — duplicated to avoid import cycle.
var conventionDirs = []string{".reasonix", ".agents", ".agent", ".claude"}

// homeConventionDirs mirror the home-directory equivalents (reverse priority).
var homeConventionDirs = []string{".claude", ".agent", ".agents", ".reasonix"}

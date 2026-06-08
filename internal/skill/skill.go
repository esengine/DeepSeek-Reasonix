// Package skill loads invokable playbooks ("skills") from Markdown files. A skill
// is a named, described prompt body the model can invoke via the run_skill tool
// (or the user via "/<name>"): an "inline" skill folds its body into the turn as
// a tool result, a "subagent" skill runs in an isolated child loop and returns
// only its final answer. Project scope wins over global; only names+descriptions
// enter the cache-stable system-prompt index (see index.go) — bodies load on
// demand. Discovery scans several conventions (.reasonix / .agents / .agent /
// .claude under the project root and the home dir — see config.ConventionDirs) so
// skills authored for other agent tools migrate in unchanged, and follows
// symlinks, so a linked skill directory or flat <name>.md is picked up like a real one.
package skill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/frontmatter"
)

// Scope records where a skill was loaded from. Higher-priority scopes win on a
// name collision: project > custom > global > builtin.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeCustom  Scope = "custom"
	ScopeGlobal  Scope = "global"
	ScopeBuiltin Scope = "builtin"
)

// RunAs selects how an invoked skill executes. Inline folds the body into the
// parent turn; subagent spawns an isolated child loop and returns only the final
// answer (its tool calls and reasoning never enter the parent context).
type RunAs string

const (
	RunInline   RunAs = "inline"
	RunSubagent RunAs = "subagent"
)

const (
	// SkillsDirname is the directory under each root that holds skills.
	SkillsDirname = "skills"
	// SkillFile is the canonical filename inside a directory-layout skill.
	SkillFile = "SKILL.md"
)

// Skill is a loaded playbook.
type Skill struct {
	Name        string // canonical identifier; matches the directory / filename stem
	Description string // one-liner shown in the pinned index
	Body        string // full markdown body (post-frontmatter), loaded eagerly
	Scope       Scope  // where it came from
	Path        string // absolute path to the SKILL.md / <name>.md, or "(builtin)"
	// AllowedTools, when non-empty, scopes a subagent skill's tool registry to
	// these literal tool names (from the `allowed-tools` frontmatter).
	AllowedTools []string
	RunAs        RunAs  // inline | subagent
	Model        string // optional model override for runAs=subagent (frontmatter `model:`)
	Effort       string // optional effort for runAs=subagent (frontmatter `effort:`)
}

// IsValidName reports whether name is a usable skill identifier.
func IsValidName(name string) bool { return config.IsValidSkillName(name) }

// Options configure a Store. ProjectRoot "" reads only the global + custom
// scopes. HomeDir "" resolves to the OS home dir (tests point it at a tmpdir).
type Options struct {
	HomeDir         string
	ProjectRoot     string
	CustomPaths     []string
	ExcludedPaths   []string
	DisabledNames   []string
	MaxDepth        int
	DisableBuiltins bool // suppress shipped built-ins (test-only knob)
	// Stderr is the writer for diagnostic warnings. When nil, defaults to
	// os.Stderr. Set to io.Discard to suppress output (e.g. during model
	// switch inside a bubbletea session).
	Stderr io.Writer
}

// Store resolves skills across the configured roots.
type Store struct {
	homeDir         string
	projectRoot     string
	customPaths     []string
	excludedPaths   map[string]bool
	disabled        map[string]bool
	maxDepth        int
	disableBuiltins bool
	stderr          io.Writer
}

// New builds a Store. Relative custom paths and a relative project root are made
// absolute; "~" in a custom path expands to the home dir.
func New(opts Options) *Store {
	home := opts.HomeDir
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	root := opts.ProjectRoot
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}
	base := root
	if base == "" {
		if wd, err := os.Getwd(); err == nil {
			base = wd
		}
	}
	custom := dedupePaths(resolveCustomPaths(opts.CustomPaths, base, home))
	excluded := map[string]bool{}
	for _, p := range dedupePaths(resolveCustomPaths(opts.ExcludedPaths, base, home)) {
		excluded[config.CanonicalSkillPath(p)] = true
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	return &Store{
		homeDir:         home,
		projectRoot:     root,
		customPaths:     custom,
		excludedPaths:   excluded,
		disabled:        disabledNameSet(opts.DisabledNames),
		maxDepth:        normalizeMaxDepth(opts.MaxDepth),
		disableBuiltins: opts.DisableBuiltins,
		stderr:          stderr,
	}
}

// HasProjectScope reports whether the store was configured with a project root.
func (s *Store) HasProjectScope() bool { return s.projectRoot != "" }

// PathStatus describes a root directory's readability, surfaced by `/skill paths`.
type PathStatus string

const (
	StatusOK           PathStatus = "ok"
	StatusMissing      PathStatus = "missing"
	StatusNotDirectory PathStatus = "not-directory"
	StatusUnreadable   PathStatus = "unreadable"
)

// Root is one discovery directory with its scope, priority, and status.
type Root struct {
	Dir      string
	Scope    Scope
	Priority int
	Status   PathStatus
}

// roots returns the discovery directories, highest priority first: the
// convention dirs (config.ConventionDirs: .reasonix / .agents / .agent / .claude)
// under the project root → custom paths → the same convention dirs under the home
// dir. A later root never overrides an earlier one.
func (s *Store) roots() []Root {
	type de struct {
		dir   string
		scope Scope
	}
	var dirs []de
	if s.projectRoot != "" {
		for _, c := range config.ConventionDirs {
			dirs = append(dirs, de{filepath.Join(s.projectRoot, c, SkillsDirname), ScopeProject})
		}
	}
	for _, d := range s.customPaths {
		dirs = append(dirs, de{d, ScopeCustom})
	}
	for _, c := range config.ConventionDirs {
		dirs = append(dirs, de{filepath.Join(s.homeDir, c, SkillsDirname), ScopeGlobal})
	}
	out := make([]Root, 0, len(dirs))
	for _, d := range dirs {
		if s.excludedPaths[config.CanonicalSkillPath(d.dir)] {
			continue
		}
		out = append(out, Root{Dir: d.dir, Scope: d.scope, Priority: len(out), Status: pathStatus(d.dir)})
	}
	return out
}

// Roots exposes the discovery directories with their status for `/skill paths`.
func (s *Store) Roots() []Root { return s.roots() }

func disabledNameSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		if key := config.SkillNameKey(name); key != "" {
			out[key] = true
		}
	}
	return out
}

func (s *Store) disabledName(name string) bool {
	return s.disabled[config.SkillNameKey(name)]
}

func normalizeMaxDepth(depth int) int {
	const (
		defaultDepth = 3
		maxDepth     = 5
	)
	if depth == 0 {
		return defaultDepth
	}
	if depth < 1 {
		return 1
	}
	if depth > maxDepth {
		return maxDepth
	}
	return depth
}

// pathStatus classifies a root directory without failing on the common case of
// "not created yet".
func pathStatus(dir string) PathStatus {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return StatusMissing
		}
		return StatusUnreadable
	}
	if !info.IsDir() {
		return StatusNotDirectory
	}
	if f, err := os.Open(dir); err != nil {
		return StatusUnreadable
	} else {
		_ = f.Close()
	}
	return StatusOK
}

// List returns every discoverable skill, deduped by name (first/highest-priority
// root wins) with built-ins folded in last, sorted by name so the prefix index
// stays stable and cacheable.
func (s *Store) List() []Skill {
	byName := map[string]Skill{}
	for _, r := range s.roots() {
		if r.Status != StatusOK {
			continue
		}
		for _, sk := range s.discoverRoot(r) {
			if s.disabledName(sk.Name) {
				continue
			}
			if _, dup := byName[sk.Name]; !dup {
				byName[sk.Name] = sk
			}
		}
	}
	if !s.disableBuiltins {
		for _, sk := range builtinSkills() {
			if s.disabledName(sk.Name) {
				continue
			}
			if _, dup := byName[sk.Name]; !dup {
				byName[sk.Name] = sk
			}
		}
	}
	out := make([]Skill, 0, len(byName))
	for _, sk := range byName {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Read resolves one skill by name, scanning the roots in priority order then the
// built-ins. ok is false when no such skill exists or the file is unreadable.
func (s *Store) Read(name string) (Skill, bool) {
	if !IsValidName(name) {
		return Skill{}, false
	}
	if s.disabledName(name) {
		return Skill{}, false
	}
	for _, sk := range s.List() {
		if sk.Name == name {
			return sk, true
		}
	}
	return Skill{}, false
}

func (s *Store) discoverRoot(r Root) []Skill {
	var out []Skill
	s.scanDir(r.Dir, r.Scope, 1, map[string]bool{}, &out)
	return out
}

func (s *Store) scanDir(dir string, scope Scope, depth int, seen map[string]bool, out *[]Skill) {
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
	for _, e := range entries {
		sk, ok := s.readEntry(dir, scope, e)
		if ok {
			if depth == 1 || strings.TrimSpace(sk.Description) != "" {
				*out = append(*out, sk)
			}
			continue
		}
		if depth >= s.maxDepth || !s.canScanChildDir(dir, e) {
			continue
		}
		s.scanDir(filepath.Join(dir, e.Name()), scope, depth+1, seen, out)
	}
}

func (s *Store) canScanChildDir(dir string, e os.DirEntry) bool {
	name := e.Name()
	if shouldSkipScanDir(name) {
		return false
	}
	full := filepath.Join(dir, name)
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(full)
	return err == nil && info.IsDir()
}

func shouldSkipScanDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "assets", "node_modules", "references", "scripts":
		return true
	default:
		return false
	}
}

// readEntry turns one directory entry into a skill. It resolves symlinks via
// os.Stat (os.ReadDir reports a symlink's own type, not its target's), so a
// linked skill directory or a linked flat <name>.md is discovered like a real
// one; a broken link fails Stat and is skipped.
func (s *Store) readEntry(dir string, scope Scope, e os.DirEntry) (Skill, bool) {
	name := e.Name()
	full := filepath.Join(dir, name)

	isDir := e.IsDir()
	isFile := e.Type().IsRegular()
	if e.Type()&os.ModeSymlink != 0 {
		info, err := os.Stat(full) // follows the link
		if err != nil {
			return Skill{}, false // broken link
		}
		isDir = info.IsDir()
		isFile = info.Mode().IsRegular()
	}

	if isDir {
		if !IsValidName(name) {
			return Skill{}, false
		}
		file := filepath.Join(full, SkillFile)
		if _, err := os.Stat(file); err != nil {
			return Skill{}, false // a directory without a SKILL.md is not a skill
		}
		return s.parse(file, name, scope)
	}
	if isFile && strings.EqualFold(filepath.Ext(name), ".md") {
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if !IsValidName(stem) {
			return Skill{}, false
		}
		return s.parse(full, stem, scope)
	}
	return Skill{}, false
}

// parse reads and decodes one skill file. The frontmatter `name:` overrides the
// filename stem when valid; a missing `description:` is a warning, not a failure
// (the skill loads but won't appear in the model's index).
func (s *Store) parse(path, stem string, scope Scope) (Skill, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false
	}
	content := strings.TrimPrefix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\uFEFF")
	fm, body := splitFrontmatter(content)

	name := stem
	if v := fm["name"]; v != "" && IsValidName(v) {
		name = v
	}
	desc := strings.TrimSpace(fm["description"])
	if desc == "" {
		fmt.Fprintf(s.stderr, "warning: skill %q at %s has no description: — it will load but won't appear in the skills index\n", name, path)
	}
	return Skill{
		Name:         name,
		Description:  desc,
		Body:         loadBodyWithReferences(path, strings.TrimSpace(body)),
		Scope:        scope,
		Path:         path,
		AllowedTools: parseAllowedTools(fm["allowed-tools"]),
		RunAs:        parseRunAs(fm["runas"], fm["context"], fm["agent"]),
		Model:        strings.TrimSpace(fm["model"]),
		Effort:       strings.TrimSpace(fm["effort"]),
	}, true
}

// Create scaffolds a new skill stub at the chosen scope. Refuses to overwrite.
func (s *Store) Create(name string, scope Scope) (string, error) {
	return s.CreateWithContent(name, scope, stubBody(name))
}

// CreateWithContent writes caller-supplied file contents as a canonical
// <name>/SKILL.md skill, refusing to clobber an existing directory-layout or
// legacy flat skill of the same name. Returns the written path.
func (s *Store) CreateWithContent(name string, scope Scope, content string) (string, error) {
	if !IsValidName(name) {
		return "", fmt.Errorf("invalid skill name %q — use letters, digits, '_', '-', '.'", name)
	}
	var root string
	switch scope {
	case ScopeProject:
		if s.projectRoot == "" {
			return "", fmt.Errorf("project scope requires a workspace — run from a project directory, or use global scope")
		}
		root = filepath.Join(s.projectRoot, ".reasonix", SkillsDirname)
	default:
		root = filepath.Join(s.homeDir, ".reasonix", SkillsDirname)
	}
	flat := filepath.Join(root, name+".md")
	folder := filepath.Join(root, name, SkillFile)
	if _, err := os.Stat(flat); err == nil {
		return "", fmt.Errorf("skill %q already exists at %s", name, flat)
	}
	if _, err := os.Stat(folder); err == nil {
		return "", fmt.Errorf("skill %q already exists at %s", name, folder)
	}
	if err := os.MkdirAll(filepath.Dir(folder), 0o755); err != nil {
		return "", err
	}
	// O_EXCL so a concurrent create (or an existing file) is reported, not clobbered.
	f, err := os.OpenFile(folder, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("skill %q already exists at %s", name, folder)
		}
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return "", err
	}
	return folder, nil
}

// loadBodyWithReferences appends a directory-layout skill's sibling
// references/*.md files to its body (Anthropic Skills compatibility), so depth
// material is available without on-demand resolution. Flat skills have no
// references dir and are returned unchanged.
func loadBodyWithReferences(skillPath, body string) string {
	if filepath.Base(skillPath) != SkillFile {
		return body
	}
	refsDir := filepath.Join(filepath.Dir(skillPath), "references")
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		return body
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return body
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(body)
	for _, n := range names {
		content, err := os.ReadFile(filepath.Join(refsDir, n))
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(content))
		if trimmed == "" {
			continue
		}
		slug := strings.TrimSuffix(n, filepath.Ext(n))
		b.WriteString("\n\n## Reference: " + slug + "\n\n" + trimmed)
	}
	return b.String()
}

// parseAllowedTools splits a comma-separated `allowed-tools` value into trimmed,
// non-empty tool names; nil when absent.
func parseAllowedTools(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// parseRunAs maps frontmatter to a run mode. An unknown value defaults to the
// safe (non-spawning) inline mode; a `context: fork` or a non-empty `agent:`
// field (cross-tool conventions) signals subagent isolation.
func parseRunAs(runAs, context, agent string) RunAs {
	if strings.TrimSpace(runAs) == "subagent" {
		return RunSubagent
	}
	if strings.EqualFold(strings.TrimSpace(context), "fork") {
		return RunSubagent
	}
	if strings.TrimSpace(agent) != "" {
		return RunSubagent
	}
	return RunInline
}

// stubBody is the scaffold written by `/skill new` — minimal frontmatter plus
// guidance the author fills in.
func stubBody(name string) string {
	return "---\nname: " + name + "\ndescription: One-liner — what does this skill do?\n---\n\n# " + name + `

Replace this body with the playbook the model should follow when this skill is invoked.

Tips:
- Reference tools by name (bash, edit_file, grep, read_file, ...)
- Add ` + "`runAs: subagent`" + ` to frontmatter to spawn an isolated subagent loop
- Add ` + "`allowed-tools: read_file, grep`" + ` to scope a subagent's tools
`
}

// resolveCustomPaths expands "~" and makes each custom path absolute relative to
// baseDir.
func resolveCustomPaths(paths []string, baseDir, homeDir string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		switch {
		case trimmed == "~":
			trimmed = homeDir
		case strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, `~\`):
			trimmed = filepath.Join(homeDir, trimmed[2:])
		}
		if !filepath.IsAbs(trimmed) {
			trimmed = filepath.Join(baseDir, trimmed)
		}
		out = append(out, filepath.Clean(trimmed))
	}
	return out
}

// dedupePaths drops duplicate custom roots, preserving order.
func dedupePaths(paths []string) []string {
	seen := map[string]bool{}
	out := paths[:0]
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// splitFrontmatter is a thin wrapper kept for internal use; the real parser
// lives in internal/frontmatter.
func splitFrontmatter(s string) (map[string]string, string) {
	return frontmatter.Split(s)
}

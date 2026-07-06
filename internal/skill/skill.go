// Package skill loads invokable playbooks ("skills") from Markdown files. A skill
// is a named, described prompt body the model can invoke via the run_skill tool
// (or the user via "/<name>"): an "inline" skill folds its body into the turn as
// a tool result, a "subagent" skill runs in an isolated child loop and returns
// only its final answer. Project scope wins over global; only names+descriptions
// enter the cache-stable system-prompt index (see index.go) — bodies load on
// demand. Discovery scans several conventions (.reasonix / .agents / .agent /
// .claude under the project root and the home dir — see config.ConventionDirs) so
// skills authored for other agent tools migrate in unchanged. Directory skills
// use <name>/SKILL.md; flat <name>.md files from Claude roots are loaded only
// when they carry skill frontmatter. Discovery follows symlinks, so linked
// skills are picked up like real ones.
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
	// Routing metadata is intentionally kept out of the cache-stable Skills
	// index; it feeds per-turn capability hints only.
	Triggers         []string
	NegativeTriggers []string
	AutoUse          string // off | suggest | prefer | require
	NeedsFreshData   bool
	Cost             string // low | medium | high (advisory)
}

// IsValidName reports whether name is a usable skill identifier.
func IsValidName(name string) bool { return config.IsValidSkillName(name) }

// Options configure a Store. ProjectRoot "" reads only the global + custom
// scopes. HomeDir "" resolves to the OS home dir (tests point it at a tmpdir).
// ReasonixHomeDir overrides the canonical Reasonix home; empty uses
// config.ReasonixHomeDir(), or HomeDir/.reasonix when HomeDir is explicitly set.
type Options struct {
	HomeDir         string
	ReasonixHomeDir string
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
	reasonixHomeDir string
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
	reasonixHome := opts.ReasonixHomeDir
	if reasonixHome == "" {
		if opts.HomeDir != "" {
			reasonixHome = filepath.Join(home, ".reasonix")
		} else {
			reasonixHome = config.ReasonixHomeDir()
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
		reasonixHomeDir: reasonixHome,
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

type discoveryRoot struct {
	Root
	requireFlatMarker bool
}

// roots returns the discovery directories, highest priority first: the
// convention dirs (config.ConventionDirs: .reasonix / .agents / .agent / .claude)
// under the project root → custom paths → the Reasonix home skills dir → other
// home-dir convention dirs. A later root never overrides an earlier one.
func (s *Store) roots() []discoveryRoot {
	type de struct {
		dir               string
		scope             Scope
		requireFlatMarker bool
	}
	var dirs []de
	if s.projectRoot != "" {
		for _, c := range config.ConventionDirs {
			dirs = append(dirs, de{filepath.Join(s.projectRoot, c, SkillsDirname), ScopeProject, c == ".claude"})
		}
	}
	for _, d := range s.customPaths {
		dirs = append(dirs, de{d, ScopeCustom, false})
	}
	if s.reasonixHomeDir != "" {
		dirs = append(dirs, de{filepath.Join(s.reasonixHomeDir, SkillsDirname), ScopeGlobal, false})
	}
	if config.IsolatedHomeDir() == "" {
		for _, c := range config.ConventionDirs {
			dir := filepath.Join(s.homeDir, c, SkillsDirname)
			if s.reasonixHomeDir != "" && config.CanonicalSkillPath(filepath.Dir(dir)) == config.CanonicalSkillPath(s.reasonixHomeDir) {
				continue
			}
			dirs = append(dirs, de{dir, ScopeGlobal, c == ".claude"})
		}
	}
	out := make([]discoveryRoot, 0, len(dirs))
	for _, d := range dirs {
		if s.excludedPaths[config.CanonicalSkillPath(d.dir)] {
			continue
		}
		out = append(out, discoveryRoot{
			Root:              Root{Dir: d.dir, Scope: d.scope, Priority: len(out), Status: pathStatus(d.dir)},
			requireFlatMarker: d.requireFlatMarker,
		})
	}
	return out
}

// Roots exposes the discovery directories with their status for `/skill paths`.
func (s *Store) Roots() []Root {
	roots := s.roots()
	out := make([]Root, 0, len(roots))
	for _, r := range roots {
		out = append(out, r.Root)
	}
	return out
}

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

func (s *Store) discoverRoot(r discoveryRoot) []Skill {
	var out []Skill
	s.scanDir(r.Dir, r.Scope, r.requireFlatMarker, 1, map[string]bool{}, &out)
	return out
}

func (s *Store) scanDir(dir string, scope Scope, requireFlatMarker bool, depth int, seen map[string]bool, out *[]Skill) {
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
		sk, ok := s.readEntry(dir, scope, requireFlatMarker, e)
		if ok {
			if depth == 1 || strings.TrimSpace(sk.Description) != "" {
				*out = append(*out, sk)
			}
			continue
		}
		if depth >= s.maxDepth || !s.canScanChildDir(dir, e) {
			continue
		}
		s.scanDir(filepath.Join(dir, e.Name()), scope, requireFlatMarker, depth+1, seen, out)
	}
}

func (s *Store) canScanChildDir(dir string, e os.DirEntry) bool {
	name := e.Name()
	if shouldSkipScanDir(name) {
		return false
	}
	if e.IsDir() {
		return true
	}
	if !shouldStatEntryTarget(e.Type()) {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.IsDir()
}

func shouldStatEntryTarget(mode os.FileMode) bool {
	return mode&os.ModeSymlink != 0 || mode&os.ModeIrregular != 0
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

// readEntry turns one directory entry into a skill. It resolves symlink and
// Windows reparse-style entries via os.Stat (os.ReadDir can report the link's
// own type, not its target's), so a linked skill directory or flat <name>.md is
// discovered like a real one; a broken link fails Stat and is skipped.
func (s *Store) readEntry(dir string, scope Scope, requireFlatMarker bool, e os.DirEntry) (Skill, bool) {
	name := e.Name()
	full := filepath.Join(dir, name)

	isDir := e.IsDir()
	isFile := e.Type().IsRegular()
	if !isDir && !isFile && shouldStatEntryTarget(e.Type()) {
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
		return s.parseFlat(full, stem, scope, requireFlatMarker)
	}
	return Skill{}, false
}

// parse reads and decodes one skill file. The frontmatter `name:` overrides the
// filename stem when valid; a missing `description:` is a warning, not a failure
// (the skill loads but won't appear in the model's index).
func (s *Store) parse(path, stem string, scope Scope) (Skill, bool) {
	return s.parseSkill(path, stem, scope, false)
}

// parseFlat reads a flat <name>.md skill candidate. Claude skill roots can also
// contain ordinary documentation, so those flat files need explicit skill
// frontmatter before they are treated as skills.
func (s *Store) parseFlat(path, stem string, scope Scope, requireSkillMarker bool) (Skill, bool) {
	return s.parseSkill(path, stem, scope, requireSkillMarker)
}

func (s *Store) parseSkill(path, stem string, scope Scope, requireSkillMarker bool) (Skill, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false
	}
	content := strings.TrimPrefix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\uFEFF")
	fm, body := splitFrontmatter(content)
	if requireSkillMarker && !hasSkillMarker(content, fm) {
		return Skill{}, false
	}

	name := stem
	if v := fm[skillFrontmatterName]; v != "" && IsValidName(v) {
		name = v
	}
	desc := strings.TrimSpace(fm[skillFrontmatterDescription])
	if desc == "" {
		fmt.Fprintf(s.stderr, "warning: skill %q at %s has no description: — it will load but won't appear in the skills index\n", name, path)
	}
	return Skill{
		Name:         name,
		Description:  desc,
		Body:         loadBodyWithScripts(path, loadBodyWithReferences(path, strings.TrimSpace(body))),
		Scope:        scope,
		Path:         path,
		AllowedTools: parseAllowedTools(fm[skillFrontmatterAllowedTools]),
		RunAs:        parseRunAs(fm[skillFrontmatterRunAs], fm[skillFrontmatterContext], fm[skillFrontmatterAgent]),
		Model:        strings.TrimSpace(fm[skillFrontmatterModel]),
		Effort:       strings.TrimSpace(fm[skillFrontmatterEffort]),
		Triggers:     parseCSVFrontmatter(fm[skillFrontmatterTriggers]),
		NegativeTriggers: parseCSVFrontmatter(
			fm[skillFrontmatterNegativeTriggers],
		),
		AutoUse:        parseAutoUse(fm[skillFrontmatterAutoUse]),
		NeedsFreshData: parseBoolFrontmatter(fm[skillFrontmatterNeedsFreshData]),
		Cost:           parseCost(fm[skillFrontmatterCost]),
	}, true
}

const (
	skillFrontmatterDescription      = "description"
	skillFrontmatterName             = "name"
	skillFrontmatterRunAs            = "runas"
	skillFrontmatterContext          = "context"
	skillFrontmatterAgent            = "agent"
	skillFrontmatterAllowedTools     = "allowed-tools"
	skillFrontmatterModel            = "model"
	skillFrontmatterEffort           = "effort"
	skillFrontmatterTriggers         = "triggers"
	skillFrontmatterNegativeTriggers = "negative-triggers"
	skillFrontmatterAutoUse          = "auto-use"
	skillFrontmatterNeedsFreshData   = "needs-fresh-data"
	skillFrontmatterCost             = "cost"
)

var skillMarkerFrontmatterKeys = []string{
	skillFrontmatterDescription,
	skillFrontmatterName,
	skillFrontmatterRunAs,
	skillFrontmatterContext,
	skillFrontmatterAgent,
	skillFrontmatterAllowedTools,
	skillFrontmatterModel,
	skillFrontmatterEffort,
	skillFrontmatterTriggers,
	skillFrontmatterNegativeTriggers,
	skillFrontmatterAutoUse,
	skillFrontmatterNeedsFreshData,
	skillFrontmatterCost,
}

func hasSkillMarker(content string, fm map[string]string) bool {
	for _, key := range skillMarkerFrontmatterKeys {
		if strings.TrimSpace(fm[key]) != "" {
			return true
		}
	}
	return frontmatterHasSkillMarkerKey(content)
}

func frontmatterHasSkillMarkerKey(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return false
	}
	for _, line := range lines[1:end] {
		key, _, ok := strings.Cut(line, ":")
		if ok && isSkillMarkerFrontmatterKey(strings.ToLower(strings.TrimSpace(key))) {
			return true
		}
	}
	return false
}

func isSkillMarkerFrontmatterKey(key string) bool {
	for _, marker := range skillMarkerFrontmatterKeys {
		if key == marker {
			return true
		}
	}
	return false
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
		root = s.globalSkillsRoot()
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

func (s *Store) globalSkillsRoot() string {
	if s.reasonixHomeDir != "" {
		return filepath.Join(s.reasonixHomeDir, SkillsDirname)
	}
	return filepath.Join(s.homeDir, ".reasonix", SkillsDirname)
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

// loadBodyWithScripts appends a directory-layout skill's sibling scripts/
// directory listing to the body, so the model knows what scripts are
// available and can run them via bash (inheriting sandbox, gate, hooks).
func loadBodyWithScripts(skillPath, body string) string {
	if filepath.Base(skillPath) != SkillFile {
		return body
	}
	scriptsDir := filepath.Join(filepath.Dir(skillPath), "scripts")
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return body
	}
	var names []string
	for _, e := range entries {
		// Filter hidden files — bash should not see config dotfiles in scripts/.
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !isScriptExt(filepath.Ext(e.Name())) {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return body
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n## Scripts\n\nRun a listed script with bash using the exact path shown below; quote the path if it contains spaces.\n\n")
	for _, n := range names {
		b.WriteString("- `" + filepath.Join(scriptsDir, n) + "`\n")
	}
	return b.String()
}

func isScriptExt(ext string) bool {
	switch strings.ToLower(ext) {
	case "", ".sh", ".py", ".js", ".ts", ".rb", ".pl", ".php", ".ps1":
		return true
	default:
		return false
	}
}

// parseAllowedTools splits a comma-separated `allowed-tools` value into trimmed,
// non-empty tool names; nil when absent.
func parseAllowedTools(raw string) []string {
	return parseCSVFrontmatter(raw)
}

// parseCSVFrontmatter splits simple comma-separated frontmatter values. Full
// YAML lists are intentionally out of scope for the existing frontmatter parser.
func parseCSVFrontmatter(raw string) []string {
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

func parseAutoUse(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "off", "suggest", "prefer", "require":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func parseBoolFrontmatter(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes", "1", "on":
		return true
	default:
		return false
	}
}

func parseCost(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
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

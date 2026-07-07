// Package pluginpkg handles installed Reasonix plugin packages.
//
// Plugin packages are higher-level bundles that can contribute skills, hooks,
// and MCP servers. They are intentionally parsed into package-local structs so
// config/hook/desktop callers can adapt them without creating import cycles.
package pluginpkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/fileutil"
	"reasonix/internal/frontmatter"
)

const (
	NativeManifest = "reasonix-plugin.json"
	CodexManifest  = ".codex-plugin/plugin.json"
	ClaudeManifest = ".claude-plugin/plugin.json"
	StateFilename  = "plugin-packages.json"

	claudeSettingsPath = ".claude/settings.json"
	claudeInstructions = "CLAUDE.md"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// Package is one parsed plugin package rooted on disk.
type Package struct {
	Root         string
	ManifestKind string
	Manifest     Manifest
}

type Inventory struct {
	Skills     []SkillRef
	Hooks      []HookRef
	MCPServers []MCPServerRef
}

type SkillRef struct {
	Name        string
	Description string
	Path        string
	Invocation  string
	RunAs       string
}

type HookRef struct {
	Event       string
	Match       string
	Command     string
	ContextFile string
	Description string
}

type MCPServerRef struct {
	Name      string
	Transport string
	Command   string
	URL       string
}

// Manifest is the normalized manifest shape used by Reasonix.
type Manifest struct {
	Name        string
	Version     string
	Description string
	Homepage    string
	Repository  string
	Skills      []string
	Hooks       map[string][]Hook
	MCPServers  map[string]MCPServer
}

type Hook struct {
	Match        string            `json:"match,omitempty"`
	Command      string            `json:"command,omitempty"`
	ContextFile  string            `json:"contextFile,omitempty"`
	ShellCommand bool              `json:"shellCommand,omitempty"`
	Description  string            `json:"description,omitempty"`
	Timeout      int               `json:"timeout,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
}

type MCPServer struct {
	Type      string            `json:"type,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	AutoStart *bool             `json:"auto_start,omitempty"`
	Tier      string            `json:"tier,omitempty"`
}

// State is persisted at <Reasonix home>/plugin-packages.json.
type State struct {
	Version int               `json:"version"`
	Plugins []InstalledPlugin `json:"plugins"`
}

type InstalledPlugin struct {
	Name         string `json:"name"`
	Source       string `json:"source,omitempty"`
	Root         string `json:"root"`
	Version      string `json:"version,omitempty"`
	Description  string `json:"description,omitempty"`
	ManifestKind string `json:"manifestKind,omitempty"`
	Enabled      bool   `json:"enabled"`
}

type InstalledPackage struct {
	Installed InstalledPlugin
	Package   Package
	Warnings  []string
}

func IsValidName(name string) bool { return validName.MatchString(strings.TrimSpace(name)) }

func StatePath(reasonixHome string) string {
	return filepath.Join(reasonixHome, StateFilename)
}

func PluginsDir(reasonixHome string) string {
	return filepath.Join(reasonixHome, "plugins")
}

func InstallRoot(reasonixHome, name string) string {
	return filepath.Join(PluginsDir(reasonixHome), name)
}

func LoadState(reasonixHome string) (State, error) {
	var st State
	b, err := os.ReadFile(StatePath(reasonixHome))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{Version: 1}, nil
		}
		return State{}, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, err
	}
	if st.Version == 0 {
		st.Version = 1
	}
	sort.SliceStable(st.Plugins, func(i, j int) bool { return st.Plugins[i].Name < st.Plugins[j].Name })
	return st, nil
}

func SaveState(reasonixHome string, st State) error {
	if st.Version == 0 {
		st.Version = 1
	}
	sort.SliceStable(st.Plugins, func(i, j int) bool { return st.Plugins[i].Name < st.Plugins[j].Name })
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fileutil.AtomicWriteFile(StatePath(reasonixHome), b, 0o644)
}

// stateMu serialises the read-modify-write of the state file within this
// process. SaveState writes atomically (tmpfile + rename), so concurrent
// callers never see a half-written file; this lock additionally prevents two
// in-process load-modify-save cycles from clobbering each other's edit. It is
// not a cross-process lock — concurrent Reasonix processes can still race.
var stateMu sync.Mutex

func Upsert(reasonixHome string, p InstalledPlugin) error {
	if !IsValidName(p.Name) {
		return fmt.Errorf("invalid plugin name %q", p.Name)
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	st, err := LoadState(reasonixHome)
	if err != nil {
		return err
	}
	for i := range st.Plugins {
		if st.Plugins[i].Name == p.Name {
			st.Plugins[i] = p
			return SaveState(reasonixHome, st)
		}
	}
	st.Plugins = append(st.Plugins, p)
	return SaveState(reasonixHome, st)
}

func Remove(reasonixHome, name string) (InstalledPlugin, bool, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	st, err := LoadState(reasonixHome)
	if err != nil {
		return InstalledPlugin{}, false, err
	}
	for i, p := range st.Plugins {
		if p.Name != name {
			continue
		}
		st.Plugins = append(st.Plugins[:i], st.Plugins[i+1:]...)
		return p, true, SaveState(reasonixHome, st)
	}
	return InstalledPlugin{}, false, nil
}

func SetEnabled(reasonixHome, name string, enabled bool) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	st, err := LoadState(reasonixHome)
	if err != nil {
		return err
	}
	for i := range st.Plugins {
		if st.Plugins[i].Name == name {
			st.Plugins[i].Enabled = enabled
			return SaveState(reasonixHome, st)
		}
	}
	return fmt.Errorf("plugin %q is not installed", name)
}

func LoadInstalled(reasonixHome string) ([]InstalledPackage, []string) {
	st, err := LoadState(reasonixHome)
	if err != nil {
		return nil, []string{err.Error()}
	}
	var out []InstalledPackage
	var warnings []string
	for _, installed := range st.Plugins {
		if !installed.Enabled {
			continue
		}
		root := ResolveRoot(reasonixHome, installed.Root)
		pkg, pkgWarnings, err := ParseDir(root)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", installed.Name, err))
			continue
		}
		out = append(out, InstalledPackage{Installed: installed, Package: pkg, Warnings: pkgWarnings})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Installed.Name < out[j].Installed.Name })
	return out, warnings
}

func ResolveRoot(reasonixHome, root string) string {
	if filepath.IsAbs(root) {
		return filepath.Clean(root)
	}
	return filepath.Join(reasonixHome, filepath.Clean(root))
}

func RelativeRoot(reasonixHome, root string) string {
	if rel, err := filepath.Rel(reasonixHome, root); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return filepath.ToSlash(rel)
	}
	return filepath.Clean(root)
}

func ParseDir(root string) (Package, []string, error) {
	root = filepath.Clean(root)
	if pkg, warnings, err := parseNative(filepath.Join(root, NativeManifest), root); err == nil {
		return pkg, warnings, nil
	}
	if pkg, warnings, err := parseCodex(filepath.Join(root, CodexManifest), root); err == nil {
		return pkg, warnings, nil
	}
	if pkg, warnings, err := parseClaudePlugin(filepath.Join(root, ClaudeManifest), root); err == nil {
		return pkg, warnings, nil
	}
	return Package{}, nil, fmt.Errorf("no %s, %s, or %s found", NativeManifest, CodexManifest, ClaudeManifest)
}

func parseNative(path, root string) (Package, []string, error) {
	var raw struct {
		Name        string               `json:"name"`
		Version     string               `json:"version"`
		Description string               `json:"description"`
		Homepage    string               `json:"homepage"`
		Repository  string               `json:"repository"`
		Skills      json.RawMessage      `json:"skills"`
		Hooks       map[string][]Hook    `json:"hooks"`
		MCPServers  map[string]MCPServer `json:"mcpServers"`
	}
	if err := readJSONFile(path, &raw); err != nil {
		return Package{}, nil, err
	}
	skills, err := parseSkillPaths(raw.Skills)
	if err != nil {
		return Package{}, nil, err
	}
	manifest := Manifest{
		Name:        strings.TrimSpace(raw.Name),
		Version:     strings.TrimSpace(raw.Version),
		Description: strings.TrimSpace(raw.Description),
		Homepage:    strings.TrimSpace(raw.Homepage),
		Repository:  strings.TrimSpace(raw.Repository),
		Skills:      skills,
		Hooks:       normalizeHooks(raw.Hooks),
		MCPServers:  raw.MCPServers,
	}
	if err := validateManifest(root, &manifest); err != nil {
		return Package{}, nil, err
	}
	warnings := applyClaudeCompatibility(root, &manifest)
	if err := validateManifest(root, &manifest); err != nil {
		return Package{}, warnings, err
	}
	return Package{Root: root, ManifestKind: "reasonix", Manifest: manifest}, warnings, nil
}

func parseCodex(path, root string) (Package, []string, error) {
	return parseCodexLike(path, root, "codex", true)
}

func parseClaudePlugin(path, root string) (Package, []string, error) {
	return parseCodexLike(path, root, "claude", false)
}

func parseCodexLike(path, root, kind string, includeCodexSessionStartHook bool) (Package, []string, error) {
	var raw struct {
		Name        string          `json:"name"`
		Version     string          `json:"version"`
		Description string          `json:"description"`
		Homepage    string          `json:"homepage"`
		Repository  string          `json:"repository"`
		Skills      json.RawMessage `json:"skills"`
	}
	if err := readJSONFile(path, &raw); err != nil {
		return Package{}, nil, err
	}
	skills, err := parseSkillPaths(raw.Skills)
	if err != nil {
		return Package{}, nil, err
	}
	manifest := Manifest{
		Name:        strings.TrimSpace(raw.Name),
		Version:     strings.TrimSpace(raw.Version),
		Description: strings.TrimSpace(raw.Description),
		Homepage:    strings.TrimSpace(raw.Homepage),
		Repository:  strings.TrimSpace(raw.Repository),
		Skills:      skills,
	}
	hookPath := filepath.Join(root, "hooks", "session-start-codex")
	if includeCodexSessionStartHook {
		if info, err := os.Stat(hookPath); err == nil && info.Mode().IsRegular() {
			manifest.Hooks = map[string][]Hook{
				"SessionStart": {{
					Command:     hookPath,
					Cwd:         root,
					Description: "Codex-compatible session start hook from " + manifest.Name,
				}},
			}
		}
	}
	var warnings []string
	if kind == "claude" {
		warnings = append(warnings, applyClaudeConventionDirs(root, &manifest)...)
	}
	warnings = append(warnings, applyClaudeCompatibility(root, &manifest)...)
	if err := validateManifest(root, &manifest); err != nil {
		return Package{}, warnings, err
	}
	return Package{Root: root, ManifestKind: kind, Manifest: manifest}, warnings, nil
}

// claudeConventionSkillDirs are the directories a Claude plugin loads skills
// from BY CONVENTION — the official plugin layout auto-discovers skills/ (and
// packs in the wild use .claude/skills/) without declaring them in
// plugin.json, whose manifest usually carries metadata only.
var claudeConventionSkillDirs = []string{"skills", ".claude/skills"}

// claudeUnmappedCapabilities are the conventional Claude plugin surfaces
// Reasonix does not map yet. Their presence is worth a warning: silently
// installing a package while dropping half its capabilities reads as "install
// succeeded" when it mostly didn't.
var claudeUnmappedCapabilities = []string{"commands", "agents", "hooks/hooks.json", ".mcp.json"}

// applyClaudeConventionDirs fills manifest.Skills from the conventional skill
// directories when the manifest declares none (the standard Claude plugin
// shape), and reports the conventional capabilities Reasonix cannot map.
func applyClaudeConventionDirs(root string, manifest *Manifest) []string {
	var warnings []string
	if len(manifest.Skills) == 0 {
		for _, rel := range claudeConventionSkillDirs {
			dir := filepath.Join(root, filepath.FromSlash(rel))
			if dirContainsSkill(dir) {
				manifest.Skills = append(manifest.Skills, rel)
			}
		}
	}
	for _, rel := range claudeUnmappedCapabilities {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			warnings = append(warnings, fmt.Sprintf("claude plugin declares %s, which Reasonix does not map yet; that capability will not be installed", rel))
		}
	}
	return warnings
}

// dirContainsSkill reports whether dir holds at least one skill definition
// (<dir>/<name>/SKILL.md), so an empty conventional directory is not adopted
// as a skill root.
func dirContainsSkill(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func ManifestPath(kind string) string {
	switch kind {
	case "reasonix":
		return NativeManifest
	case "codex":
		return CodexManifest
	case "claude":
		return ClaudeManifest
	default:
		return NativeManifest
	}
}

func ManifestPaths() []string {
	return []string{NativeManifest, CodexManifest, ClaudeManifest}
}

func applyClaudeCompatibility(root string, manifest *Manifest) []string {
	appendRootClaudeInstructions(root, manifest)
	return appendClaudeSettingsHooks(root, manifest)
}

func appendRootClaudeInstructions(root string, manifest *Manifest) {
	path := filepath.Join(root, claudeInstructions)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if manifest.Hooks == nil {
		manifest.Hooks = map[string][]Hook{}
	}
	manifest.Hooks["SessionStart"] = append(manifest.Hooks["SessionStart"], Hook{
		ContextFile: claudeInstructions,
		Cwd:         ".",
		Description: "Plugin CLAUDE.md startup context from " + manifest.Name,
	})
}

func appendClaudeSettingsHooks(root string, manifest *Manifest) []string {
	path := filepath.Join(root, claudeSettingsPath)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Match   string `json:"match"`
			Hooks   []struct {
				Type        string            `json:"type"`
				Command     string            `json:"command"`
				Description string            `json:"description"`
				Timeout     int               `json:"timeout"`
				Env         map[string]string `json:"env"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return []string{fmt.Sprintf("%s: %v", claudeSettingsPath, err)}
	}
	if len(raw.Hooks) == 0 {
		return nil
	}
	if manifest.Hooks == nil {
		manifest.Hooks = map[string][]Hook{}
	}
	var warnings []string
	for event, blocks := range raw.Hooks {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		for _, block := range blocks {
			match := strings.TrimSpace(block.Matcher)
			if match == "" {
				match = strings.TrimSpace(block.Match)
			}
			for _, item := range block.Hooks {
				typ := strings.TrimSpace(item.Type)
				command := strings.TrimSpace(item.Command)
				if typ != "" && typ != "command" {
					warnings = append(warnings, fmt.Sprintf("%s: skipped unsupported hook type %q for %s", claudeSettingsPath, typ, event))
					continue
				}
				if command == "" {
					continue
				}
				manifest.Hooks[event] = append(manifest.Hooks[event], Hook{
					Match:        match,
					Command:      command,
					ShellCommand: true,
					Description:  firstNonEmpty(strings.TrimSpace(item.Description), "Claude-compatible hook from "+claudeSettingsPath),
					Timeout:      claudeTimeoutMillis(item.Timeout),
					Cwd:          ".",
					Env:          cloneHookEnv(item.Env),
				})
			}
		}
	}
	return warnings
}

func claudeTimeoutMillis(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return seconds * 1000
}

func cloneHookEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range in {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func parseSkillPaths(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return cleanPathList([]string{one})
	}
	var manyStrings []string
	if err := json.Unmarshal(raw, &manyStrings); err == nil {
		return cleanPathList(manyStrings)
	}
	var manyObjects []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &manyObjects); err == nil {
		paths := make([]string, 0, len(manyObjects))
		for _, item := range manyObjects {
			paths = append(paths, item.Path)
		}
		return cleanPathList(paths)
	}
	return nil, fmt.Errorf("skills must be a path string, string array, or object array")
}

func cleanPathList(paths []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "." || p == "" {
			p = "."
		}
		if filepath.IsAbs(p) || strings.HasPrefix(p, ".."+string(filepath.Separator)) || p == ".." {
			return nil, fmt.Errorf("plugin path %q must be relative and stay inside the plugin root", p)
		}
		slash := filepath.ToSlash(p)
		if !seen[slash] {
			seen[slash] = true
			out = append(out, slash)
		}
	}
	sort.Strings(out)
	return out, nil
}

func normalizeHooks(in map[string][]Hook) map[string][]Hook {
	if len(in) == 0 {
		return nil
	}
	out := map[string][]Hook{}
	for event, hooks := range in {
		event = strings.TrimSpace(event)
		for _, h := range hooks {
			h.Command = strings.TrimSpace(h.Command)
			h.ContextFile = strings.TrimSpace(h.ContextFile)
			h.Cwd = strings.TrimSpace(h.Cwd)
			if h.Command == "" && h.ContextFile == "" {
				continue
			}
			out[event] = append(out[event], h)
		}
	}
	return out
}

func validateManifest(root string, m *Manifest) error {
	if !IsValidName(m.Name) {
		return fmt.Errorf("invalid plugin name %q", m.Name)
	}
	for _, p := range m.Skills {
		if err := validateRelativePath(p); err != nil {
			return err
		}
	}
	for event, hooks := range m.Hooks {
		if strings.TrimSpace(event) == "" {
			return fmt.Errorf("hook event is required")
		}
		for _, h := range hooks {
			if h.Command == "" && h.ContextFile == "" {
				return fmt.Errorf("hook command or contextFile is required")
			}
			if h.Command != "" && !h.ShellCommand && !filepath.IsAbs(h.Command) {
				if err := validateRelativePath(h.Command); err != nil {
					return err
				}
			}
			if h.ContextFile != "" {
				if err := validateRelativePath(h.ContextFile); err != nil {
					return err
				}
			}
			if h.Cwd != "" && !filepath.IsAbs(h.Cwd) {
				if err := validateRelativePath(h.Cwd); err != nil {
					return err
				}
			}
		}
	}
	for name := range m.MCPServers {
		if !IsValidName(name) {
			return fmt.Errorf("invalid MCP server name %q", name)
		}
	}
	if _, err := os.Stat(root); err != nil {
		return err
	}
	return nil
}

func validateRelativePath(p string) error {
	p = filepath.Clean(strings.TrimSpace(p))
	if p == "" {
		return fmt.Errorf("plugin path is required")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, ".."+string(filepath.Separator)) || p == ".." {
		return fmt.Errorf("plugin path %q must be relative and stay inside the plugin root", p)
	}
	return nil
}

func (p Package) SkillRoots() []string {
	var out []string
	for _, rel := range p.Manifest.Skills {
		out = append(out, filepath.Join(p.Root, filepath.FromSlash(rel)))
	}
	sort.Strings(out)
	return out
}

func (p Package) CapabilityCounts() (skills, hooks, mcp int) {
	skills = len(p.skillRefs())
	for _, hs := range p.Manifest.Hooks {
		hooks += len(hs)
	}
	mcp = len(p.Manifest.MCPServers)
	return
}

func (p Package) Inventory() Inventory {
	return Inventory{
		Skills:     p.skillRefs(),
		Hooks:      p.hookRefs(),
		MCPServers: p.mcpServerRefs(),
	}
}

func (p Package) skillRefs() []SkillRef {
	var out []SkillRef
	seen := map[string]bool{}
	for _, rel := range p.Manifest.Skills {
		root := filepath.Join(p.Root, filepath.FromSlash(rel))
		p.scanSkillPath(root, 1, map[string]bool{}, &out)
	}
	filtered := out[:0]
	for _, sk := range out {
		key := sk.Path
		if key == "" {
			key = sk.Name
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		filtered = append(filtered, sk)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Name != filtered[j].Name {
			return filtered[i].Name < filtered[j].Name
		}
		return filtered[i].Path < filtered[j].Path
	})
	return filtered
}

func (p Package) scanSkillPath(path string, depth int, seen map[string]bool, out *[]SkillRef) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if !info.IsDir() {
		if info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(path), ".md") {
			if sk, ok := parseSkillRef(path, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))); ok {
				*out = append(*out, sk)
			}
		}
		return
	}

	key := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		key = filepath.Clean(resolved)
	}
	if seen[key] {
		return
	}
	seen[key] = true

	if sk, ok := parseSkillRef(filepath.Join(path, "SKILL.md"), filepath.Base(path)); ok {
		*out = append(*out, sk)
		return
	}
	if depth >= 5 {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if shouldSkipSkillScanDir(name) {
			continue
		}
		full := filepath.Join(path, name)
		if entry.IsDir() {
			p.scanSkillPath(full, depth+1, seen, out)
			continue
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(name), ".md") {
			if sk, ok := parseSkillRef(full, strings.TrimSuffix(name, filepath.Ext(name))); ok {
				*out = append(*out, sk)
			}
		}
	}
}

func shouldSkipSkillScanDir(name string) bool {
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

func parseSkillRef(path, stem string) (SkillRef, bool) {
	if !IsValidName(stem) {
		return SkillRef{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return SkillRef{}, false
	}
	content := strings.TrimPrefix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\uFEFF")
	fm, _ := frontmatter.Split(content)
	name := stem
	if v := strings.TrimSpace(fm["name"]); IsValidName(v) {
		name = v
	}
	return SkillRef{
		Name:        name,
		Description: strings.TrimSpace(fm["description"]),
		Path:        filepath.Clean(path),
		Invocation:  "/" + name,
		RunAs:       pluginSkillRunMode(fm),
	}, true
}

func pluginSkillRunMode(fm map[string]string) string {
	if strings.TrimSpace(fm["runas"]) == "subagent" {
		return "subagent"
	}
	if strings.EqualFold(strings.TrimSpace(fm["context"]), "fork") {
		return "subagent"
	}
	if strings.TrimSpace(fm["agent"]) != "" {
		return "subagent"
	}
	return "inline"
}

func (p Package) hookRefs() []HookRef {
	events := make([]string, 0, len(p.Manifest.Hooks))
	for event := range p.Manifest.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	var out []HookRef
	for _, event := range events {
		for _, hook := range p.Manifest.Hooks[event] {
			out = append(out, HookRef{
				Event:       event,
				Match:       hook.Match,
				Command:     hook.Command,
				ContextFile: hook.ContextFile,
				Description: hook.Description,
			})
		}
	}
	return out
}

func (p Package) mcpServerRefs() []MCPServerRef {
	names := make([]string, 0, len(p.Manifest.MCPServers))
	for name := range p.Manifest.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]MCPServerRef, 0, len(names))
	for _, name := range names {
		server := p.Manifest.MCPServers[name]
		out = append(out, MCPServerRef{
			Name:      name,
			Transport: pluginMCPTransport(server),
			Command:   strings.TrimSpace(server.Command),
			URL:       strings.TrimSpace(server.URL),
		})
	}
	return out
}

func pluginMCPTransport(server MCPServer) string {
	if typ := strings.TrimSpace(server.Type); typ != "" {
		return typ
	}
	if strings.TrimSpace(server.URL) != "" {
		return "http"
	}
	return "stdio"
}

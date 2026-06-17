package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/fileutil"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/netclient"
	"reasonix/internal/permission"
)

// edit.go is the programmatic mutation surface a settings UI drives: change the
// default model, add/remove a provider, set the planner, edit permission rules,
// add/remove an MCP server — each validated, then persisted with SaveTo. It is
// separate from the `reasonix setup` wizard (cli) so a GUI can apply one setting at a
// time without replaying the whole interactive flow. Every mutator works on the
// in-memory *Config; nothing writes to disk until SaveTo/Save is called, so a UI
// can stage several changes and commit once. Mutations round-trip through
// RenderTOML → Load (the wizard relies on the same guarantee).

// permission rule list names accepted by the rule mutators.
const (
	listAllow = "allow"
	listAsk   = "ask"
	listDeny  = "deny"
)

// SetDefaultModel points default_model at an existing model. It accepts both
// forms used by the runtime resolver:
//   - "provider"          — the provider's own default model;
//   - "provider/model"    — that specific model under that provider.
//
// Either is rejected when the target does not exist, so a UI can't strand
// the config on a model that doesn't exist.
func (c *Config) SetDefaultModel(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("set default: empty name")
	}
	if _, ok := c.ResolveModel(name); !ok {
		return fmt.Errorf("set default: no such model %q (configured: %s)", name, c.providerNames())
	}
	c.DefaultModel = name
	return nil
}

// SetPlannerModel sets (or, with "", clears) agent.planner_model for two-model
// collaboration. A non-empty name must be a configured provider.
func (c *Config) SetPlannerModel(name string) error {
	if name == "" {
		c.Agent.PlannerModel = ""
		return nil
	}
	if _, ok := c.Provider(name); !ok {
		return fmt.Errorf("set planner: no provider %q (configured: %s)", name, c.providerNames())
	}
	c.Agent.PlannerModel = name
	return nil
}

// SetAutoPlan sets the interactive auto-plan gate. "off" keeps plan mode manual;
// "on" opts into automatic read-only planning for complex-looking turns.
// "ask" is accepted as a legacy synonym for "on" but is never written back.
func (c *Config) SetAutoPlan(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off":
		c.Agent.AutoPlan = "off"
	case "on", "ask":
		c.Agent.AutoPlan = "on"
	default:
		return fmt.Errorf("auto_plan %q: must be off|on", mode)
	}
	return nil
}

// SetUIShortcutLayout selects the CLI keyboard shortcut layout. "classic" keeps
// historical behavior; "desktop" enables the two-axis desktop-style shortcuts.
func (c *Config) SetUIShortcutLayout(layout string) error {
	switch strings.ToLower(strings.TrimSpace(layout)) {
	case "", "classic", "default", "legacy", "off":
		c.UI.ShortcutLayout = "classic"
	case "desktop", "dual", "dual-axis", "dual_axis":
		c.UI.ShortcutLayout = "desktop"
	default:
		return fmt.Errorf("shortcut_layout %q: must be classic|desktop", layout)
	}
	return nil
}

// UpsertProvider adds e, or replaces an existing provider with the same name
// (preserving its position). Required fields (name, kind, base_url, model/models)
// are validated; whether the kind is actually registered and the key resolves is
// checked later by provider.New / Validate, which give actionable errors.
func (c *Config) UpsertProvider(e ProviderEntry) error {
	normalizeProviderEffortFields(&e)
	if err := validateProvider(e); err != nil {
		return err
	}
	for i := range c.Providers {
		if c.Providers[i].Name == e.Name {
			c.Providers[i] = e
			return nil
		}
	}
	c.Providers = append(c.Providers, e)
	return nil
}

// SetProviderEffort updates a provider's provider-specific thinking effort knob.
func (c *Config) SetProviderEffort(name, effort string) error {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			c.Providers[i].Effort = normalizeStoredEffort(effort)
			return nil
		}
	}
	return fmt.Errorf("set provider effort: no provider %q", name)
}

// SetLanguage pins the CLI UI/model language; empty/auto clears the override so runtime detection falls back to REASONIX_LANG / locale.
func (c *Config) SetLanguage(lang string) error {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto":
		c.Language = ""
	case "en":
		c.Language = "en"
	case "zh":
		c.Language = "zh"
	default:
		return fmt.Errorf("language %q: must be auto|en|zh", lang)
	}
	c.ApplyDeepSeekOfficialDefaultPricing()
	return nil
}

// SetReasoningLanguage pins the preferred language for visible reasoning text.
// Empty/auto follows the conversation language.
func (c *Config) SetReasoningLanguage(lang string) error {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto", "follow", "conversation", "detect", "default", "model", "model-default", "model_default", "provider":
		c.Agent.ReasoningLanguage = ""
	case "zh", "cn", "chinese", "中文":
		c.Agent.ReasoningLanguage = "zh"
	case "en", "english":
		c.Agent.ReasoningLanguage = "en"
	default:
		return fmt.Errorf("reasoning language %q: must be auto|zh|en", lang)
	}
	return nil
}

// SetDesktopLanguage pins the desktop UI language. It intentionally does not
// modify Config.Language, which is used by the CLI/model-facing runtime.
func (c *Config) SetDesktopLanguage(lang string) error {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto":
		c.Desktop.Language = ""
	case "en":
		c.Desktop.Language = "en"
	case "zh":
		c.Desktop.Language = "zh"
	default:
		return fmt.Errorf("desktop language %q: must be auto|en|zh", lang)
	}
	c.ApplyDeepSeekOfficialDefaultPricing()
	return nil
}

// SetDesktopAppearance sets desktop-only theme preferences. It must not affect
// CLI theme settings or provider-visible request data.
func (c *Config) SetDesktopAppearance(theme, style string) error {
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "auto":
		c.Desktop.Theme = "auto"
	case "light":
		c.Desktop.Theme = "light"
	case "", "dark":
		c.Desktop.Theme = "dark"
	default:
		return fmt.Errorf("desktop theme %q: must be auto|dark|light", theme)
	}
	if strings.TrimSpace(style) == "" {
		c.Desktop.ThemeStyle = ""
		return nil
	}
	normalized := normalizeThemeStyle(style)
	if normalized == "" {
		return fmt.Errorf("desktop theme style %q: must be graphite|aurora|slate|carbon|nocturne|amber", style)
	}
	c.Desktop.ThemeStyle = normalized
	return nil
}

// SetDesktopLayoutStyle sets the desktop layout style. UI-only; it must not
// affect CLI output or provider-visible request data.
func (c *Config) SetDesktopLayoutStyle(style string) error {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "", "classic":
		c.Desktop.LayoutStyle = "classic"
	case "workbench", "workspace":
		c.Desktop.LayoutStyle = "workbench"
	default:
		return fmt.Errorf("desktop layout style %q: must be classic|workbench", style)
	}
	return nil
}

// SetDesktopCloseBehavior sets the desktop close-window preference. It is
// intentionally UI-only and must not affect model prompts or provider-visible
// request data.
func (c *Config) SetDesktopCloseBehavior(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "quit", "exit":
		c.Desktop.CloseBehavior = "quit"
	case "", "background", "hide":
		c.Desktop.CloseBehavior = "background"
	default:
		return fmt.Errorf("close behavior %q: must be quit|background", mode)
	}
	return nil
}

// SetDesktopDisplayMode sets the transcript display mode. UI-only.
func (c *Config) SetDesktopDisplayMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "compact", "minimal":
		c.Desktop.DisplayMode = "compact"
	case "", "standard":
		c.Desktop.DisplayMode = "standard"
	default:
		return fmt.Errorf("display mode %q: must be standard|compact", mode)
	}
	return nil
}

// SetDesktopStatusBarStyle sets the desktop status bar metric label style.
// UI-only; it must not affect CLI output or provider-visible request data.
func (c *Config) SetDesktopStatusBarStyle(style string) error {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "icon", "icons":
		c.Desktop.StatusBarStyle = "icon"
	case "", "text", "label", "labels":
		c.Desktop.StatusBarStyle = "text"
	default:
		return fmt.Errorf("status bar style %q: must be icon|text", style)
	}
	return nil
}

// SetDesktopStatusBarItems sets the ordered visible desktop status bar items.
// UI-only; it must not affect CLI output or provider-visible request data.
func (c *Config) SetDesktopStatusBarItems(items []string) error {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, raw := range items {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		if !knownDesktopStatusBarItems[id] {
			return fmt.Errorf("status bar item %q: unknown item", id)
		}
		out = append(out, id)
		seen[id] = true
	}
	if len(out) == 0 {
		out = DefaultDesktopStatusBarItems()
	}
	c.Desktop.StatusBarItems = out
	return nil
}

// SetDesktopCheckUpdates sets whether the desktop app checks for updates on
// startup. Manual checks remain available in Settings regardless of this value.
func (c *Config) SetDesktopCheckUpdates(enabled bool) error {
	c.Desktop.CheckUpdates = &enabled
	return nil
}

// SetColdResumePrune toggles auto-elision of stale tool results on cold resume.
func (c *Config) SetColdResumePrune(enabled bool) error {
	c.Agent.ColdResumePrune = &enabled
	return nil
}

// SetDesktopTelemetry sets whether the desktop sends the anonymous launch ping.
func (c *Config) SetDesktopTelemetry(enabled bool) error {
	c.Desktop.Telemetry = &enabled
	return nil
}

// SetDesktopMetrics sets whether the desktop sends aggregate desktop metrics.
func (c *Config) SetDesktopMetrics(enabled bool) error {
	c.Desktop.Metrics = &enabled
	return nil
}

// SetUICloseBehavior is kept for callers compiled against the old edit API.
func (c *Config) SetUICloseBehavior(mode string) error {
	return c.SetDesktopCloseBehavior(mode)
}

// SetExpandThinking sets whether the desktop reasoning/thinking section is
// expanded by default. It is desktop-only and must not affect CLI output or
// provider-visible request data.
func (c *Config) SetExpandThinking(on bool) error {
	c.Desktop.ExpandThinking = on
	return nil
}

// SetShowReasoning sets the CLI's default verbose-reasoning preference. When
// true, thinking text is shown in the chat TUI on startup; when false (the
// default), it stays collapsed until the user toggles it with Ctrl+O or
// /verbose.
func (c *Config) SetShowReasoning(on bool) error {
	c.UI.ShowReasoning = on
	return nil
}

// SetProviderThinking updates a provider's provider-specific thinking mode knob.
func (c *Config) SetProviderThinking(name, thinking string) error {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			c.Providers[i].Thinking = strings.ToLower(strings.TrimSpace(thinking))
			return nil
		}
	}
	return fmt.Errorf("set provider thinking: no provider %q", name)
}

// SetNetwork updates ordinary outbound network proxy settings. Invalid custom
// proxy settings are rejected here so the desktop panel cannot save a config that
// would break provider startup.
func (c *Config) SetNetwork(n NetworkConfig) error {
	n.ProxyMode = netclient.NormalizeMode(n.ProxyMode)
	n.ProxyURL = strings.TrimSpace(n.ProxyURL)
	n.NoProxy = strings.TrimSpace(n.NoProxy)
	n.Proxy.Type = strings.ToLower(strings.TrimSpace(n.Proxy.Type))
	n.Proxy.Server = strings.TrimSpace(n.Proxy.Server)
	n.Proxy.Username = strings.TrimSpace(n.Proxy.Username)
	c.Network = n
	return netclient.Validate(c.NetworkProxySpec())
}

// ModelRefsProvider reports whether ref targets the named provider. It matches
// both bare provider names ("deepseek") and "provider/model" refs.
func ModelRefsProvider(ref, name string) bool {
	ref = strings.TrimSpace(ref)
	name = strings.TrimSpace(name)
	if ref == "" || name == "" {
		return false
	}
	if ref == name {
		return true
	}
	prov, _, ok := strings.Cut(ref, "/")
	return ok && prov == name
}

func (c *Config) modelRefTargetsProvider(ref, name string) bool {
	if ModelRefsProvider(ref, name) {
		return true
	}
	if e, ok := c.ResolveModel(ref); ok {
		return e.Name == name
	}
	return false
}

// RemoveProvider deletes the named provider. References to the removed provider
// are migrated to the first remaining configured provider when possible. The
// default model is required, so removal is refused when no fallback exists;
// optional planner/subagent refs are cleared instead of being left dangling.
func (c *Config) RemoveProvider(name string) error {
	name = strings.TrimSpace(name)
	idx := -1
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("remove provider: no provider %q", name)
	}

	defaultRefsProvider := c.modelRefTargetsProvider(c.DefaultModel, name)
	plannerRefsProvider := c.modelRefTargetsProvider(c.Agent.PlannerModel, name)
	subagentRefsProvider := c.modelRefTargetsProvider(c.Agent.SubagentModel, name)
	subagentModelRefsProvider := map[string]bool{}
	for skill, ref := range c.Agent.SubagentModels {
		if c.modelRefTargetsProvider(ref, name) {
			subagentModelRefsProvider[skill] = true
		}
	}

	fallback := ""
	if defaultRefsProvider || plannerRefsProvider || subagentRefsProvider || len(subagentModelRefsProvider) > 0 {
		fallback = c.providerRemovalFallback(name)
	}
	if defaultRefsProvider && fallback == "" {
		return fmt.Errorf("remove provider: %q is referenced by default_model and no other configured provider exists", name)
	}

	c.Providers = append(c.Providers[:idx], c.Providers[idx+1:]...)

	if defaultRefsProvider {
		c.DefaultModel = fallback
	}
	if plannerRefsProvider {
		c.Agent.PlannerModel = fallback
	}
	if subagentRefsProvider {
		c.Agent.SubagentModel = fallback
	}
	for skill := range subagentModelRefsProvider {
		if fallback != "" {
			c.Agent.SubagentModels[skill] = fallback
		} else {
			delete(c.Agent.SubagentModels, skill)
		}
	}
	return nil
}

func (c *Config) providerRemovalFallback(name string) string {
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == name || !p.Configured() || len(p.ModelList()) == 0 {
			continue
		}
		return p.Name
	}
	return ""
}

// validateProvider checks the fields a provider can't function without.
func validateProvider(e ProviderEntry) error {
	switch {
	case strings.TrimSpace(e.Name) == "":
		return fmt.Errorf("provider: name is required")
	case strings.TrimSpace(e.Kind) == "":
		return fmt.Errorf("provider %q: kind is required", e.Name)
	case strings.TrimSpace(e.BaseURL) == "":
		return fmt.Errorf("provider %q: base_url is required", e.Name)
	case !providerHasAnyModel(e):
		return fmt.Errorf("provider %q: model is required", e.Name)
	}
	return nil
}

func providerHasAnyModel(e ProviderEntry) bool {
	if strings.TrimSpace(e.Model) != "" {
		return true
	}
	for _, m := range e.Models {
		if strings.TrimSpace(m) != "" {
			return true
		}
	}
	return false
}

// SetPermissionMode sets the writer-fallback mode. Accepts "ask", "allow", or
// "deny" (case-insensitive); anything else errors rather than silently
// defaulting, so a UI surfaces a typo instead of installing a surprising mode.
func (c *Config) SetPermissionMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ask", "allow", "deny":
		c.Permissions.Mode = strings.ToLower(strings.TrimSpace(mode))
		return nil
	default:
		return fmt.Errorf("permission mode %q: must be ask|allow|deny", mode)
	}
}

// AddPermissionRule appends a rule ("ToolName" or "ToolName(glob)") to the
// allow / ask / deny list. The rule is validated with the same parser the gate
// uses, and a duplicate is a no-op so a UI can call it idempotently.
func (c *Config) AddPermissionRule(list, rule string) error {
	target, err := c.ruleList(list)
	if err != nil {
		return err
	}
	rule = strings.TrimSpace(rule)
	if _, ok := permission.ParseRule(rule); !ok {
		return fmt.Errorf("invalid permission rule %q (want \"ToolName\" or \"ToolName(glob)\")", rule)
	}
	for _, existing := range *target {
		if existing == rule {
			return nil // already present
		}
	}
	*target = append(*target, rule)
	return nil
}

// RemovePermissionRule drops the first exact match of rule from the named list,
// reporting whether anything was removed.
func (c *Config) RemovePermissionRule(list, rule string) (bool, error) {
	target, err := c.ruleList(list)
	if err != nil {
		return false, err
	}
	rule = strings.TrimSpace(rule)
	for i, existing := range *target {
		if existing == rule {
			*target = append((*target)[:i], (*target)[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// ruleList returns a pointer to the named rule slice so mutators can append to
// it in place. An unknown list name errors.
func (c *Config) ruleList(list string) (*[]string, error) {
	switch strings.ToLower(strings.TrimSpace(list)) {
	case listAllow:
		return &c.Permissions.Allow, nil
	case listAsk:
		return &c.Permissions.Ask, nil
	case listDeny:
		return &c.Permissions.Deny, nil
	default:
		return nil, fmt.Errorf("unknown permission list %q (want allow|ask|deny)", list)
	}
}

// AddSkillPath appends a custom skill root, deduping by its expanded absolute
// path while preserving the caller's original spelling in the config file.
func (c *Config) AddSkillPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("skill path: empty path")
	}
	want := CanonicalSkillPath(path)
	c.removeExcludedSkillPath(want)
	for _, existing := range c.Skills.Paths {
		if CanonicalSkillPath(existing) == want {
			return nil
		}
	}
	c.Skills.Paths = append(c.Skills.Paths, path)
	return nil
}

// RemoveSkillPath removes the first custom skill root matching path after
// expansion and path cleaning. It reports whether anything changed.
func (c *Config) RemoveSkillPath(path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, fmt.Errorf("skill path: empty path")
	}
	want := CanonicalSkillPath(path)
	for i, existing := range c.Skills.Paths {
		if CanonicalSkillPath(existing) == want {
			c.Skills.Paths = append(c.Skills.Paths[:i], c.Skills.Paths[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// RestoreSkillPath removes a pseudo-deleted skill source from excluded_paths.
func (c *Config) RestoreSkillPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("skill path: empty path")
	}
	want := CanonicalSkillPath(path)
	if want == "" {
		return fmt.Errorf("skill path: empty path")
	}
	c.removeExcludedSkillPath(want)
	return nil
}

// ExcludeSkillPath hides any skill discovery root matching path. This is used by
// UI "remove source" actions for convention roots that are not stored in paths.
func (c *Config) ExcludeSkillPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("skill path: empty path")
	}
	want := CanonicalSkillPath(path)
	if want == "" {
		return fmt.Errorf("skill path: empty path")
	}
	for _, existing := range c.Skills.ExcludedPaths {
		if CanonicalSkillPath(existing) == want {
			return nil
		}
	}
	c.Skills.ExcludedPaths = append(c.Skills.ExcludedPaths, path)
	return nil
}

func (c *Config) removeExcludedSkillPath(want string) {
	next := c.Skills.ExcludedPaths[:0]
	for _, existing := range c.Skills.ExcludedPaths {
		if CanonicalSkillPath(existing) != want {
			next = append(next, existing)
		}
	}
	c.Skills.ExcludedPaths = next
}

// SetSkillEnabled persists a per-skill enable/disable preference. Skills are
// enabled by default; disabling records the name, enabling removes it.
func (c *Config) SetSkillEnabled(name string, enabled bool) error {
	name = strings.TrimSpace(name)
	key := SkillNameKey(name)
	if key == "" {
		return fmt.Errorf("skill name %q: use letters, digits, '_', '-', '.', 1-64 chars, starting alphanumeric", name)
	}
	next := c.DisabledSkillNames()
	idx := -1
	for i, existing := range next {
		if SkillNameKey(existing) == key {
			idx = i
			break
		}
	}
	if enabled {
		if idx >= 0 {
			next = append(next[:idx], next[idx+1:]...)
		}
		c.Skills.DisabledSkills = next
		return nil
	}
	if idx < 0 {
		next = append(next, name)
	}
	c.Skills.DisabledSkills = next
	return nil
}

// CanonicalSkillPath expands env vars, ~ and relative segments to an absolute
// cleaned path for comparing skill roots. On Windows it folds case so paths that
// differ only in casing dedupe. Use only for comparison, never as stored config.
func CanonicalSkillPath(path string) string {
	path = ExpandVars(strings.TrimSpace(path))
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	} else if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			path = home
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// UpsertPlugin adds e, or replaces an MCP server with the same name (preserving
// position). The transport-specific required fields are validated: stdio needs
// a command, http/sse need a url.
func (c *Config) UpsertPlugin(e PluginEntry) error {
	e, _ = NormalizePluginCommandLine(e)
	if err := validatePlugin(e); err != nil {
		return err
	}
	for i := range c.Plugins {
		if c.Plugins[i].Name == e.Name {
			c.Plugins[i] = e
			return nil
		}
	}
	c.Plugins = append(c.Plugins, e)
	return nil
}

// RemovePlugin deletes the named MCP server, reporting whether it was present.
func (c *Config) RemovePlugin(name string) bool {
	for i := range c.Plugins {
		if c.Plugins[i].Name == name {
			c.Plugins = append(c.Plugins[:i], c.Plugins[i+1:]...)
			return true
		}
	}
	return false
}

// ClearPluginAuthentication removes locally stored auth-like material for one
// MCP server while keeping the server entry itself. It intentionally leaves
// non-auth config (command, URL host/path, ordinary env/header keys, tier) alone.
func (c *Config) ClearPluginAuthentication(name string) (PluginEntry, bool, error) {
	for i := range c.Plugins {
		if c.Plugins[i].Name != name {
			continue
		}
		headers, env, url, changed := mcpdiag.ClearAuthConfig(c.Plugins[i].Headers, c.Plugins[i].Env, c.Plugins[i].URL)
		c.Plugins[i].Headers = headers
		c.Plugins[i].Env = env
		c.Plugins[i].URL = url
		return c.Plugins[i], changed, nil
	}
	return PluginEntry{}, false, fmt.Errorf("clear plugin authentication: no plugin %q", name)
}

// ClearPluginAuthenticationInSource clears auth material in the file that actually
// owns the MCP server. Load() merges user/project TOML and project .mcp.json into
// one Config, so callers must not mutate that merged view and Save() it back: a
// .mcp.json-only server would otherwise be serialized into reasonix.toml or the
// user config. Source priority mirrors Load(): project TOML, user TOML, then the
// project .mcp.json entry if TOML did not define that server.
func ClearPluginAuthenticationInSource(name string) (PluginEntry, bool, string, error) {
	if path := pluginTOMLSourcePath(name); path != "" {
		cfg := LoadForEdit(path)
		updated, changed, err := cfg.ClearPluginAuthentication(name)
		if err != nil {
			return PluginEntry{}, false, path, err
		}
		if changed {
			if err := cfg.SaveTo(path); err != nil {
				return PluginEntry{}, false, path, err
			}
		}
		return updated, changed, path, nil
	}
	updated, changed, err := clearMCPJSONAuthentication(mcpJSONFile, name)
	if err != nil {
		return PluginEntry{}, false, "", err
	}
	return updated, changed, mcpJSONFile, nil
}

func pluginTOMLSourcePath(name string) string {
	paths := append([]string{"reasonix.toml"}, userConfigCandidatePaths()...)
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		cfg := LoadForEdit(path)
		for _, p := range cfg.Plugins {
			if p.Name == name {
				return path
			}
		}
	}
	return ""
}

// validatePlugin checks a plugin entry by transport. An empty Type means stdio.
func validatePlugin(e PluginEntry) error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("plugin: name is required")
	}
	switch strings.ToLower(strings.TrimSpace(e.Type)) {
	case "", "stdio":
		if strings.TrimSpace(e.Command) == "" {
			return fmt.Errorf("plugin %q: command is required for a stdio server", e.Name)
		}
	case "http", "sse", "streamable-http":
		if strings.TrimSpace(e.URL) == "" {
			return fmt.Errorf("plugin %q: url is required for a %s server", e.Name, e.Type)
		}
	default:
		return fmt.Errorf("plugin %q: unknown type %q (want stdio|http|sse)", e.Name, e.Type)
	}
	return nil
}

// SaveTo writes the configuration to path as annotated TOML, atomically: it
// writes a sibling temp file then renames, so a crash mid-write can't leave a
// half-written reasonix.toml that fails to parse on next load. Parent directories
// are created as needed.
func (c *Config) SaveTo(path string) error {
	return c.SaveToScope(path, renderScopeForPath(path))
}

func (c *Config) SaveToScope(path string, scope RenderScope) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("save: empty config path")
	}
	return writeConfigFile(path, RenderTOMLForScope(c, scope))
}

// SaveMinimalProjectAutoPlan writes a new project config that only overrides
// [agent].auto_plan. It is intentionally minimal so toggling a project-local
// auto-plan preference in an otherwise unconfigured workspace does not pin
// default_model or providers from built-in defaults.
func SaveMinimalProjectAutoPlan(path, mode string) (string, error) {
	cfg := Default()
	if err := cfg.SetAutoPlan(mode); err != nil {
		return "", err
	}
	body := fmt.Sprintf(`# Reasonix project configuration.
# Project-local overrides are merged over the user config.

[agent]
auto_plan = %q
`, cfg.Agent.AutoPlan)
	return cfg.Agent.AutoPlan, writeConfigFile(path, body)
}

// SaveMinimalProjectReasoningLanguage writes a new project config that only
// overrides [agent].reasoning_language.
func SaveMinimalProjectReasoningLanguage(path, lang string) (string, error) {
	cfg := Default()
	if err := cfg.SetReasoningLanguage(lang); err != nil {
		return "", err
	}
	body := fmt.Sprintf(`# Reasonix project configuration.
# Project-local overrides are merged over the user config.

[agent]
reasoning_language = %q
`, cfg.ReasoningLanguage())
	return cfg.ReasoningLanguage(), writeConfigFile(path, body)
}

func writeConfigFile(path, body string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("save: empty config path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("save: create dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".reasonix.*.toml.tmp")
	if err != nil {
		return fmt.Errorf("save: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("save: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("save: close temp: %w", err)
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

func renderScopeForPath(path string) RenderScope {
	if isUserConfigPath(path) {
		return RenderScopeUser
	}
	return RenderScopeProject
}

func isUserConfigPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, uc := range userConfigCandidatePaths() {
		uc = strings.TrimSpace(uc)
		if uc == "" {
			continue
		}
		pathAbs, pathErr := filepath.Abs(path)
		ucAbs, ucErr := filepath.Abs(uc)
		if pathErr == nil && ucErr == nil {
			if filepath.Clean(pathAbs) == filepath.Clean(ucAbs) {
				return true
			}
			continue
		}
		if filepath.Clean(path) == filepath.Clean(uc) {
			return true
		}
	}
	return false
}

// Save writes the configuration back to the file it was loaded from
// (SourcePath), or to ./reasonix.toml when none exists yet — the conventional
// project-local target a fresh GUI session would create.
func (c *Config) Save() error {
	path := SourcePath()
	if path == "" {
		path = "reasonix.toml"
	}
	return c.SaveTo(path)
}

// SaveForRoot saves the config to root's reasonix.toml, falling back to the
// user's global config when root has no existing reasonix.toml.
func (c *Config) SaveForRoot(root string) error {
	root = resolveRoot(root)
	projectTOML := "reasonix.toml"
	if root != "." {
		projectTOML = filepath.Join(root, "reasonix.toml")
	}
	if _, err := os.Stat(projectTOML); err == nil {
		return c.SaveTo(projectTOML)
	}
	if uc := userConfigPath(); uc != "" {
		if err := os.MkdirAll(filepath.Dir(uc), 0o755); err != nil {
			return err
		}
		return c.SaveTo(uc)
	}
	return c.SaveTo(projectTOML)
}

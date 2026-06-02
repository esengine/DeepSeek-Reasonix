package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/fileutil"
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

// SetDefaultModel points default_model at an existing provider. It errors if no
// provider by that name is configured, so a UI can't strand the config on a
// model that doesn't exist.
func (c *Config) SetDefaultModel(name string) error {
	if _, ok := c.Provider(name); !ok {
		return fmt.Errorf("set default: no provider %q (configured: %s)", name, c.providerNames())
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

// UpsertProvider adds e, or replaces an existing provider with the same name
// (preserving its position). Required fields (name, kind, base_url, model) are
// validated; whether the kind is actually registered and the key resolves is
// checked later by provider.New / Validate, which give actionable errors.
func (c *Config) UpsertProvider(e ProviderEntry) error {
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
			c.Providers[i].Effort = strings.ToLower(strings.TrimSpace(effort))
			return nil
		}
	}
	return fmt.Errorf("set provider effort: no provider %q", name)
}

// RemoveProvider deletes the named provider. It refuses to remove the current
// default_model (reassign it first, so the config never points at a missing
// model); if the removed provider was the planner, planner_model is cleared as
// a side effect since it is optional. Errors when the name isn't configured.
func (c *Config) RemoveProvider(name string) error {
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
	if c.DefaultModel == name {
		return fmt.Errorf("remove provider: %q is the default model — set a different default_model first", name)
	}
	c.Providers = append(c.Providers[:idx], c.Providers[idx+1:]...)
	if c.Agent.PlannerModel == name {
		c.Agent.PlannerModel = ""
	}
	return nil
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
	case strings.TrimSpace(e.Model) == "":
		return fmt.Errorf("provider %q: model is required", e.Name)
	}
	return nil
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
	if _, err := tmp.WriteString(RenderTOML(c)); err != nil {
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

// Package inspect projects a running agent's capabilities into plain,
// JSON-serializable structs a GUI can render directly: which providers are
// configured (and whether their API key is present), which tools are available
// (built-in vs. MCP, read-only, previewable), which MCP servers are connected
// and what prompts/resources they expose, and which slash commands are loaded.
//
// It is a read-only projection layer — every function takes the already-built
// runtime objects (config, tool registry, plugin host, command list) and
// returns a view. Nothing here mutates state or performs I/O beyond reading the
// environment for key readiness. The CLI's `/mcp` listing and a desktop
// settings panel are two front-ends over the same projection.
package inspect

import (
	"encoding/json"
	"strings"

	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

// Snapshot bundles every capability surface so a front-end can populate its
// settings / sidebar in one call. Any input may be nil/empty; the corresponding
// slice is then nil.
type Snapshot struct {
	DefaultModel string         `json:"default_model"`
	Providers    []ProviderInfo `json:"providers"`
	Tools        []ToolInfo     `json:"tools"`
	Servers      []ServerInfo   `json:"servers"`
	Prompts      []PromptInfo   `json:"prompts"`
	Resources    []ResourceInfo `json:"resources"`
	Commands     []CommandInfo  `json:"commands"`
}

// Capabilities builds a full Snapshot. host and reg may be nil (no plugins / no
// registry yet); cmds may be empty.
func Capabilities(cfg *config.Config, reg *tool.Registry, host *plugin.Host, cmds []command.Command) Snapshot {
	s := Snapshot{
		Providers: Providers(cfg),
		Tools:     Tools(reg),
		Servers:   Servers(host),
		Prompts:   Prompts(host),
		Resources: Resources(host),
		Commands:  Commands(cmds),
	}
	if cfg != nil {
		s.DefaultModel = cfg.DefaultModel
	}
	return s
}

// ProviderInfo is a GUI-ready view of one configured model provider. KeyReady
// reflects whether api_key_env is currently set in the environment, so a
// settings screen can show a green/amber dot without exposing the secret.
type ProviderInfo struct {
	Name          string       `json:"name"`
	Kind          string       `json:"kind"`
	Model         string       `json:"model"`
	BaseURL       string       `json:"base_url"`
	APIKeyEnv     string       `json:"api_key_env"`
	KeyReady      bool         `json:"key_ready"`
	ContextWindow int          `json:"context_window"`
	IsDefault     bool         `json:"is_default"`
	Pricing       *PricingInfo `json:"pricing,omitempty"`
}

// PricingInfo mirrors provider.Pricing as a flat, currency-tagged view.
type PricingInfo struct {
	CacheHit float64 `json:"cache_hit"`
	Input    float64 `json:"input"`
	Output   float64 `json:"output"`
	Currency string  `json:"currency"`
}

// Providers projects cfg.Providers, marking the default model and resolving key
// readiness from the environment. For grouped providers (Models list), it emits
// one ProviderInfo per model so the GUI can show each model separately.
func Providers(cfg *config.Config) []ProviderInfo {
	if cfg == nil {
		return nil
	}
	out := make([]ProviderInfo, 0, len(cfg.Providers))
	for i := range cfg.Providers {
		e := &cfg.Providers[i]
		// For grouped providers, emit one entry per model.
		models := e.ModelList()
		if len(models) == 0 {
			models = []string{""} // emit one entry even if no model
		}
		for _, model := range models {
			isDefault := false
			switch {
			case e.Name == cfg.DefaultModel:
				// cfg.DefaultModel names a grouped provider → all its models are default.
				isDefault = true
			case cfg.DefaultModel == e.Name+"/"+model:
				// cfg.DefaultModel is a specific provider/model ref.
				isDefault = true
			}
			info := ProviderInfo{
				Name:          e.Name,
				Kind:          e.Kind,
				Model:         model,
				BaseURL:       e.BaseURL,
				APIKeyEnv:     e.APIKeyEnv,
				KeyReady:      e.APIKey() != "",
				ContextWindow: e.ContextWindow,
				IsDefault:     isDefault,
			}
			if p := e.PriceFor(model); p != nil {
				info.Pricing = &PricingInfo{
					CacheHit: p.CacheHit,
					Input:    p.Input,
					Output:   p.Output,
					Currency: p.Symbol(),
				}
			}
			out = append(out, info)
		}
	}
	return out
}

// ToolInfo is a GUI-ready view of one available tool. Source is "builtin" or
// "mcp:<server>" (derived from the mcp__<server>__<tool> naming). Previewable
// reports whether the tool implements tool.Previewer (the file-writers), so a
// UI knows it can show a diff before approval. Schema is the raw JSON Schema
// for the tool's parameters.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ReadOnly    bool            `json:"read_only"`
	Previewable bool            `json:"previewable"`
	Source      string          `json:"source"`
	Schema      json.RawMessage `json:"schema,omitempty"`
}

// Tools projects the tool registry into ToolInfo entries. nil reg → nil slice.
func Tools(reg *tool.Registry) []ToolInfo {
	if reg == nil {
		return nil
	}
	names := reg.Names()
	out := make([]ToolInfo, 0, len(names))
	for _, n := range names {
		t, ok := reg.Get(n)
		if !ok {
			continue
		}
		out = append(out, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			ReadOnly:    t.ReadOnly(),
			Source:      "builtin",
			Schema:      t.Schema(),
		})
	}
	return out
}

// ServerInfo is a GUI-ready view of one connected MCP server.
type ServerInfo struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
}

// Servers projects the plugin host's connected server names. nil host → nil slice.
func Servers(host *plugin.Host) []ServerInfo {
	if host == nil {
		return nil
	}
	names := host.ServerNames()
	out := make([]ServerInfo, 0, len(names))
	for _, n := range names {
		out = append(out, ServerInfo{Name: n})
	}
	return out
}

// PromptInfo is a GUI-ready view of one MCP prompt.
type PromptInfo struct {
	Name string `json:"name"`
	Desc string `json:"description"`
}

// Prompts projects the plugin host's prompts. nil host → nil slice.
func Prompts(host *plugin.Host) []PromptInfo {
	if host == nil {
		return nil
	}
	prompts := host.Prompts()
	out := make([]PromptInfo, 0, len(prompts))
	for _, p := range prompts {
		out = append(out, PromptInfo{Name: p.Name, Desc: p.Description})
	}
	return out
}

// ResourceInfo is a GUI-ready view of one MCP resource.
type ResourceInfo struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// Resources projects the plugin host's resources. nil host → nil slice.
func Resources(host *plugin.Host) []ResourceInfo {
	if host == nil {
		return nil
	}
	resources := host.Resources()
	out := make([]ResourceInfo, 0, len(resources))
	for _, r := range resources {
		out = append(out, ResourceInfo{URI: r.URI, Name: r.Name})
	}
	return out
}

// CommandInfo is a GUI-ready view of one slash command.
type CommandInfo struct {
	Name string `json:"name"`
	Desc string `json:"description"`
}

// Commands projects the command list. nil/empty cmds → nil slice.
func Commands(cmds []command.Command) []CommandInfo {
	if len(cmds) == 0 {
		return nil
	}
	out := make([]CommandInfo, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, CommandInfo{Name: c.Name, Desc: c.Description})
	}
	return out
}

// ModelRefs returns the configured provider/model refs for slash completion.
func ModelRefs(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var out []string
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		for _, model := range p.ModelList() {
			out = append(out, p.Name+"/"+model)
		}
	}
	return out
}

// ProviderNames returns the configured provider names for slash completion.
func ProviderNames(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	out := make([]string, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		out = append(out, p.Name)
	}
	return out
}

// ModelForRef resolves a "provider/model" ref to the model name, or "" if not found.
func ModelForRef(cfg *config.Config, ref string) string {
	if cfg == nil {
		return ""
	}
	prov, model, ok := strings.Cut(ref, "/")
	if !ok {
		return ""
	}
	if e, found := cfg.Provider(prov); found && e.HasModel(model) {
		return model
	}
	return ""
}

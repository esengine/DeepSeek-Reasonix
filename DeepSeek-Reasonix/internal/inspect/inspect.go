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
// readiness from the environment. Returns nil for a nil config.
func Providers(cfg *config.Config) []ProviderInfo {
	if cfg == nil {
		return nil
	}
	out := make([]ProviderInfo, 0, len(cfg.Providers))
	for i := range cfg.Providers {
		e := &cfg.Providers[i]
		info := ProviderInfo{
			Name:          e.Name,
			Kind:          e.Kind,
			Model:         e.Model,
			BaseURL:       e.BaseURL,
			APIKeyEnv:     e.APIKeyEnv,
			KeyReady:      e.APIKey() != "",
			ContextWindow: e.ContextWindow,
			IsDefault:     e.Name == cfg.DefaultModel,
		}
		if p := e.Price; p != nil {
			info.Pricing = &PricingInfo{
				CacheHit: p.CacheHit,
				Input:    p.Input,
				Output:   p.Output,
				Currency: p.Symbol(),
			}
		}
		out = append(out, info)
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

// Tools projects a runtime registry in its display order. Returns nil for a nil
// registry.
func Tools(reg *tool.Registry) []ToolInfo {
	if reg == nil {
		return nil
	}
	names := reg.Names()
	out := make([]ToolInfo, 0, len(names))
	for _, name := range names {
		t, ok := reg.Get(name)
		if !ok {
			continue
		}
		_, previewable := t.(tool.Previewer)
		out = append(out, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			ReadOnly:    t.ReadOnly(),
			Previewable: previewable,
			Source:      toolSource(t.Name()),
			Schema:      t.Schema(),
		})
	}
	return out
}

// toolSource classifies a tool by its name: an mcp__<server>__<tool> name maps
// to "mcp:<server>", anything else is a compiled-in "builtin".
func toolSource(name string) string {
	if server, _, ok := tool.SplitMCPName(name); ok {
		return "mcp:" + server
	}
	if strings.HasPrefix(name, tool.MCPNamePrefix) {
		return "mcp" // carries the namespace but malformed (missing a part)
	}
	return "builtin"
}

// ServerInfo is one connected MCP server and its exposed-surface counts.
type ServerInfo struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Tools     int    `json:"tools"`
	Prompts   int    `json:"prompts"`
	Resources int    `json:"resources"`
}

// Servers projects the connected MCP servers. Returns nil when host is nil.
func Servers(host *plugin.Host) []ServerInfo {
	if host == nil {
		return nil
	}
	statuses := host.Servers()
	out := make([]ServerInfo, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, ServerInfo{
			Name:      s.Name,
			Transport: s.Transport,
			Tools:     s.Tools,
			Prompts:   s.Prompts,
			Resources: s.Resources,
		})
	}
	return out
}

// PromptInfo is one MCP prompt, surfaced as a slash command. Name is the full
// mcp__<server>__<prompt> command body (without a leading slash).
type PromptInfo struct {
	Name        string          `json:"name"`
	Server      string          `json:"server"`
	Description string          `json:"description"`
	Args        []PromptArgInfo `json:"args,omitempty"`
}

// PromptArgInfo is one declared argument of an MCP prompt.
type PromptArgInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// Prompts projects the MCP prompts exposed across all servers. Returns nil when
// host is nil.
func Prompts(host *plugin.Host) []PromptInfo {
	if host == nil {
		return nil
	}
	ps := host.Prompts()
	out := make([]PromptInfo, 0, len(ps))
	for _, p := range ps {
		info := PromptInfo{Name: p.Name, Server: p.Server, Description: p.Description}
		for _, a := range p.Args {
			info.Args = append(info.Args, PromptArgInfo{
				Name:        a.Name,
				Description: a.Description,
				Required:    a.Required,
			})
		}
		out = append(out, info)
	}
	return out
}

// ResourceInfo is one MCP resource, referenceable in a message as
// @<server>:<uri>.
type ResourceInfo struct {
	Server      string `json:"server"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mime_type"`
}

// Resources projects the MCP resources exposed across all servers. Returns nil
// when host is nil.
func Resources(host *plugin.Host) []ResourceInfo {
	if host == nil {
		return nil
	}
	rs := host.Resources()
	out := make([]ResourceInfo, 0, len(rs))
	for _, r := range rs {
		out = append(out, ResourceInfo{
			Server:      r.Server,
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MimeType:    r.MimeType,
		})
	}
	return out
}

// CommandInfo is one custom slash command loaded from .reasonix/commands. Name
// has no leading slash (e.g. "review" or "git:commit").
type CommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ArgHint     string `json:"arg_hint"`
	Source      string `json:"source"`
}

// Commands projects loaded custom slash commands. Returns nil for an empty list.
func Commands(cmds []command.Command) []CommandInfo {
	if len(cmds) == 0 {
		return nil
	}
	out := make([]CommandInfo, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, CommandInfo{
			Name:        c.Name,
			Description: c.Description,
			ArgHint:     c.ArgHint,
			Source:      c.Source,
		})
	}
	return out
}

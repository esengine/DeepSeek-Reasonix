package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"reasonix/internal/fileutil"
	"reasonix/internal/mcpdiag"
)

// mcpJSONFile is the project-root file Claude Code calls .mcp.json. Reasonix reads
// it so an MCP server already configured for Claude works here unchanged — the
// server specs map field-for-field onto PluginEntry.
const mcpJSONFile = ".mcp.json"

// mcpServerSpec mirrors one entry of Claude Code's "mcpServers" map. The field
// names and semantics match PluginEntry (and Claude): command/args/env describe
// a local stdio server; type/url/headers describe a remote one.
type mcpServerSpec struct {
	Type      string            `json:"type"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	AutoStart *bool             `json:"auto_start"`
}

// loadMCPJSON reads path (Claude Code's .mcp.json) and returns its servers as
// PluginEntry values, sorted by name for a stable connection order. An absent
// file is not an error (returns nil, nil). A present-but-malformed file is an
// error so a typo surfaces loudly instead of silently dropping every server.
func loadMCPJSON(path string) ([]PluginEntry, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mcp config %s: %w", path, err)
	}
	var doc struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("mcp config %s: %w", path, err)
	}
	names := make([]string, 0, len(doc.MCPServers))
	for name := range doc.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]PluginEntry, 0, len(names))
	for _, name := range names {
		s := doc.MCPServers[name]
		entries = append(entries, pluginEntryFromMCPSpec(name, s))
	}
	return entries, nil
}

func pluginEntryFromMCPSpec(name string, s mcpServerSpec) PluginEntry {
	return PluginEntry{
		Name:      name,
		Type:      s.Type,
		Command:   s.Command,
		Args:      s.Args,
		Env:       s.Env,
		URL:       s.URL,
		Headers:   s.Headers,
		AutoStart: s.AutoStart,
	}
}

// mergeMCPJSON appends servers from .mcp.json that the TOML config did not
// already declare. reasonix.toml's [[plugins]] win on a name collision: it is the
// Reasonix-specific, more explicit of the two, so it overrides the shared,
// checked-in .mcp.json rather than the other way round.
func (c *Config) mergeMCPJSON(entries []PluginEntry) {
	have := make(map[string]bool, len(c.Plugins))
	for _, p := range c.Plugins {
		have[p.Name] = true
	}
	for _, e := range entries {
		if have[e.Name] {
			continue
		}
		have[e.Name] = true
		c.Plugins = append(c.Plugins, e)
	}
}

func clearMCPJSONAuthentication(path, name string) (PluginEntry, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return PluginEntry{}, false, fmt.Errorf("clear plugin authentication: no plugin %q", name)
	}
	if err != nil {
		return PluginEntry{}, false, fmt.Errorf("mcp config %s: %w", path, err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(b, &root); err != nil {
		return PluginEntry{}, false, fmt.Errorf("mcp config %s: %w", path, err)
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(root["mcpServers"], &servers); err != nil || servers == nil {
		return PluginEntry{}, false, fmt.Errorf("clear plugin authentication: no plugin %q", name)
	}
	raw, ok := servers[name]
	if !ok {
		return PluginEntry{}, false, fmt.Errorf("clear plugin authentication: no plugin %q", name)
	}
	var spec mcpServerSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return PluginEntry{}, false, fmt.Errorf("mcp config %s: server %q: %w", path, name, err)
	}
	cleanHeaders, cleanEnv, cleanURL, changed := mcpdiag.ClearAuthConfig(spec.Headers, spec.Env, spec.URL)
	if !changed {
		return pluginEntryFromMCPSpec(name, spec), false, nil
	}
	spec.Headers = cleanHeaders
	spec.Env = cleanEnv
	spec.URL = cleanURL

	var server map[string]json.RawMessage
	if err := json.Unmarshal(raw, &server); err != nil || server == nil {
		return PluginEntry{}, false, fmt.Errorf("mcp config %s: server %q is not an object", path, name)
	}
	setMCPJSONStringMap(server, "headers", cleanHeaders)
	setMCPJSONStringMap(server, "env", cleanEnv)
	setMCPJSONString(server, "url", cleanURL)
	updatedRaw, err := json.Marshal(server)
	if err != nil {
		return PluginEntry{}, false, fmt.Errorf("mcp config %s: server %q: %w", path, name, err)
	}
	servers[name] = updatedRaw
	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return PluginEntry{}, false, fmt.Errorf("mcp config %s: %w", path, err)
	}
	root["mcpServers"] = serversRaw
	if err := writeMCPJSON(path, root); err != nil {
		return PluginEntry{}, false, err
	}
	return pluginEntryFromMCPSpec(name, spec), true, nil
}

func setMCPJSONStringMap(server map[string]json.RawMessage, key string, values map[string]string) {
	if len(values) == 0 {
		delete(server, key)
		return
	}
	raw, err := json.Marshal(values)
	if err != nil {
		delete(server, key)
		return
	}
	server[key] = raw
}

func setMCPJSONString(server map[string]json.RawMessage, key, value string) {
	if value == "" {
		delete(server, key)
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		delete(server, key)
		return
	}
	server[key] = raw
}

func writeMCPJSON(path string, root map[string]json.RawMessage) error {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("mcp config %s: %w", path, err)
	}
	out = append(out, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mcp config %s: create dir: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".mcp.*.json.tmp")
	if err != nil {
		return fmt.Errorf("mcp config %s: create temp: %w", path, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("mcp config %s: write: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("mcp config %s: close temp: %w", path, err)
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

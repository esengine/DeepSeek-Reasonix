package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// mcpJSONFile is the project-root file Claude Code calls .mcp.json. Reasonix reads
// it so an MCP server already configured for Claude works here unchanged — the
// server specs map field-for-field onto PluginEntry.
const mcpJSONFile = ".mcp.json"

// mcpServerSpec mirrors one entry of Claude Code's "mcpServers" map. The field
// names and semantics match PluginEntry: command/args/env describe a local
// stdio server; type/url/headers describe a remote one. Reasonix also accepts
// startup and call timeout fields as Reasonix policy extensions.
type mcpServerSpec struct {
	Type                  string            `json:"type"`
	Command               string            `json:"command"`
	Args                  []string          `json:"args"`
	Env                   map[string]string `json:"env"`
	URL                   string            `json:"url"`
	Headers               map[string]string `json:"headers"`
	StartupTimeoutSeconds int               `json:"startup_timeout_seconds"`
	CallTimeoutSeconds    int               `json:"call_timeout_seconds"`
	ToolTimeoutSeconds    map[string]int    `json:"tool_timeout_seconds"`
	DisabledTools         []string          `json:"disabled_tools"`
	AutoStart             *bool             `json:"auto_start"`
	AlwaysLoad            bool              `json:"alwaysLoad"`
}

// loadMCPJSON reads path (Claude Code's .mcp.json) and returns its servers as
// PluginEntry values, sorted by name for a stable connection order. An absent
// file is not an error (returns nil, nil). A present-but-malformed file is an
// error so a typo surfaces loudly instead of silently dropping every server.
func loadMCPJSON(path string) ([]PluginEntry, error) {
	resolved, err := resolveConfigAccessPath(path, false)
	if err != nil {
		return nil, fmt.Errorf("mcp config %s: %w", path, err)
	}
	b, err := fileencoding.ReadFileUTF8(resolved)
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
	return specsToEntries(doc.MCPServers, MCPSourceProjectMCPJSON), nil
}

// ParseMCPServersJSON reads a Claude-compatible mcpServers document — what every
// MCP server's install docs hand the user — and returns its entries. A bare
// server map (no "mcpServers" wrapper) is accepted too, because that is what a
// user copying one server out of a larger document ends up with.
// Source is left unset: the caller owns where the entry will live.
func ParseMCPServersJSON(body []byte) ([]PluginEntry, error) {
	var doc struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	specs := doc.MCPServers
	if len(specs) == 0 {
		bare := map[string]mcpServerSpec{}
		if err := json.Unmarshal(body, &bare); err != nil {
			return nil, err
		}
		specs = bare
	}
	// Either shape needs a command or URL to describe an installable server.
	for name, spec := range specs {
		if spec.Command == "" && spec.URL == "" {
			delete(specs, name)
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("no MCP server found in this JSON")
	}
	entries := specsToEntries(specs, "")
	for i := range entries {
		entries[i].Source = ""
	}
	return entries, nil
}

// LoadMCPJSONPlugin returns one server entry from a Claude-compatible .mcp.json.
func LoadMCPJSONPlugin(path, name string) (PluginEntry, bool, error) {
	entries, err := loadMCPJSON(path)
	if err != nil {
		return PluginEntry{}, false, err
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true, nil
		}
	}
	return PluginEntry{}, false, nil
}

// specsToEntries converts an mcpServers map to PluginEntry values, sorted by name
// for a stable connection order. Names in skip are dropped (used for v0.x's
// mcpDisabled list).
func specsToEntries(specs map[string]mcpServerSpec, source MCPConfigSource, skip ...map[string]bool) []PluginEntry {
	var skipped map[string]bool
	if len(skip) > 0 {
		skipped = skip[0]
	}
	names := make([]string, 0, len(specs))
	for name := range specs {
		if !skipped[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	entries := make([]PluginEntry, 0, len(names))
	for _, name := range names {
		entry := pluginEntryFromMCPSpec(name, specs[name])
		entry.Source = source
		entries = append(entries, entry)
	}
	return entries
}

// legacyConfigPath is the v0.x (TypeScript line) config file, ~/.reasonix/config.json.
func legacyConfigPath() string { return processRoots().legacyConfigPath() }

func (r Roots) legacyConfigPath() string {
	if r.pinnedHomeDir() != "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".reasonix", "config.json")
}

// loadLegacyMCP reads the v0.x ~/.reasonix/config.json and returns its enabled
// MCP servers as PluginEntry values — both the canonical mcpServers map and the
// older `mcp` string list (mcpServers wins on a name collision, matching v0.x;
// servers listed in mcpDisabled are skipped) — so upgrading from v0.x keeps MCP
// servers working without rewriting them as [[plugins]]. Absent or malformed →
// nil: a stale legacy file must never block startup, and it is the
// lowest-priority source anyway (the v2 config and .mcp.json win on a name
// collision — see Load).
func loadLegacyMCP(path string) []PluginEntry {
	if path == "" {
		return nil
	}
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return nil
	}
	var doc struct {
		MCP         []string                     `json:"mcp"`
		MCPServers  map[string]mcpServerSpec     `json:"mcpServers"`
		MCPEnv      map[string]map[string]string `json:"mcpEnv"`
		MCPDisabled []string                     `json:"mcpDisabled"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil
	}
	disabled := make(map[string]bool, len(doc.MCPDisabled))
	for _, n := range doc.MCPDisabled {
		disabled[n] = true
	}
	entries := specsToEntries(doc.MCPServers, MCPSourceLegacyUser, disabled)
	have := make(map[string]bool, len(entries))
	for _, e := range entries {
		have[e.Name] = true
	}
	for i, raw := range doc.MCP {
		pe, ok := parseLegacyMCPSpec(raw)
		if !ok || disabled[pe.Name] {
			continue
		}
		if pe.Name == "" {
			pe.Name = anonymousMCPName(i)
		} else if pe.Command != "" {
			pe.Env = doc.MCPEnv[pe.Name]
		}
		if have[pe.Name] {
			continue
		}
		have[pe.Name] = true
		pe, _ = NormalizePluginCommandLine(pe)
		entries = append(entries, pe)
	}
	for i := range entries {
		entries[i].Source = MCPSourceLegacyUser
	}
	return entries
}

var legacyMCPSpecName = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)=(.*)$`)

// parseLegacyMCPSpec parses one v0.x `--mcp`-format string: "name=cmd args...",
// "name=https://url" (SSE), or "name=streamable+https://url" (streamable HTTP);
// the name= prefix is optional.
func parseLegacyMCPSpec(raw string) (PluginEntry, bool) {
	body := strings.TrimSpace(raw)
	var name string
	if m := legacyMCPSpecName.FindStringSubmatch(body); m != nil {
		name, body = m[1], strings.TrimSpace(m[2])
	}
	if body == "" {
		return PluginEntry{}, false
	}
	lower := strings.ToLower(body)
	if strings.HasPrefix(lower, "streamable+http://") || strings.HasPrefix(lower, "streamable+https://") {
		return PluginEntry{Name: name, Type: "http", URL: body[len("streamable+"):]}, true
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return PluginEntry{Name: name, Type: "sse", URL: body}, true
	}
	parts, ok := splitPluginCommandLine(body)
	if !ok || len(parts) == 0 {
		return PluginEntry{}, false
	}
	if shouldSplitPluginCommand(body, parts[0]) {
		return PluginEntry{Name: name, Command: parts[0], Args: parts[1:]}, true
	}
	return PluginEntry{Name: name, Command: body}, true
}

// anonymousMCPName names a v0.x spec that carried no name= prefix (its tools
// registered unprefixed in v0.x; v1+ plugins require a name).
func anonymousMCPName(i int) string {
	return fmt.Sprintf("mcp-%d", i+1)
}

func pluginEntryFromMCPSpec(name string, s mcpServerSpec) PluginEntry {
	e := PluginEntry{
		Name:                  name,
		Type:                  s.Type,
		Command:               s.Command,
		Args:                  s.Args,
		Env:                   s.Env,
		URL:                   s.URL,
		Headers:               s.Headers,
		StartupTimeoutSeconds: s.StartupTimeoutSeconds,
		CallTimeoutSeconds:    s.CallTimeoutSeconds,
		ToolTimeoutSeconds:    s.ToolTimeoutSeconds,
		DisabledTools:         s.DisabledTools,
		AutoStart:             s.AutoStart,
	}
	if s.AlwaysLoad {
		e.Load = MCPLoadAlways
	}
	e, _ = NormalizePluginCommandLine(e)
	return e
}

// mergeMCPJSON appends servers from .mcp.json that the TOML config did not
// already declare. reasonix.toml's [[plugins]] win on a name collision: it is the
// Reasonix-specific, more explicit of the two, so it overrides the shared,
// checked-in .mcp.json rather than the other way round.
func (c *Config) mergeMCPJSON(entries []PluginEntry) {
	index := make(map[string]int, len(c.Plugins))
	for i, p := range c.Plugins {
		index[p.Name] = i
	}
	for _, e := range entries {
		if i, exists := index[e.Name]; exists {
			// Project configuration wins; within one project, reasonix.toml
			// remains more specific than .mcp.json. Keep the union of
			// disabled_tools so a later source cannot re-enable a restriction.
			e.DisabledTools = mergeDisabledToolPolicies(c.Plugins[i].DisabledTools, e.DisabledTools)
			if e.Source == MCPSourceProjectMCPJSON && !c.Plugins[i].Source.ProjectScoped() {
				c.Plugins[i] = e
			} else {
				c.Plugins[i].DisabledTools = e.DisabledTools
			}
			continue
		}
		index[e.Name] = len(c.Plugins)
		c.Plugins = append(c.Plugins, e)
	}
}

// UpsertMCPJSONPlugin writes one MCP server to a Claude-compatible .mcp.json
// file, preserving unrelated top-level fields and unknown per-server fields.
func UpsertMCPJSONPlugin(path string, entry PluginEntry) (bool, error) {
	entry, _ = NormalizePluginCommandLine(entry)
	if err := validatePlugin(entry); err != nil {
		return false, err
	}
	root, servers, err := readMCPJSONRaw(path)
	if err != nil {
		return false, err
	}
	raw, existed := servers[entry.Name]
	server := map[string]json.RawMessage{}
	if existed && len(raw) > 0 {
		if err := json.Unmarshal(raw, &server); err != nil || server == nil {
			return false, fmt.Errorf("mcp config %s: server %q is not an object", path, entry.Name)
		}
	}
	if err := applyPluginEntryToMCPJSONServer(server, entry); err != nil {
		return false, fmt.Errorf("mcp config %s: server %q: %w", path, entry.Name, err)
	}
	updatedRaw, err := json.Marshal(server)
	if err != nil {
		return false, fmt.Errorf("mcp config %s: server %q: %w", path, entry.Name, err)
	}
	servers[entry.Name] = updatedRaw
	if err := writeMCPJSONServers(path, root, servers); err != nil {
		return false, err
	}
	return !existed, nil
}

// RemoveMCPJSONPlugin removes one MCP server from a Claude-compatible .mcp.json
// file. Missing files or missing entries are reported as unchanged.
func RemoveMCPJSONPlugin(path, name string) (bool, error) {
	root, servers, err := readMCPJSONRaw(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, ok := servers[name]; !ok {
		return false, nil
	}
	delete(servers, name)
	if err := writeMCPJSONServers(path, root, servers); err != nil {
		return false, err
	}
	return true, nil
}

func readMCPJSONRaw(path string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	root := map[string]json.RawMessage{}
	servers := map[string]json.RawMessage{}
	resolved, err := resolveConfigAccessPath(path, false)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp config %s: %w", path, err)
	}
	b, err := fileencoding.ReadFileUTF8(resolved)
	if os.IsNotExist(err) {
		return root, servers, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("mcp config %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, nil, fmt.Errorf("mcp config %s: %w", path, err)
	}
	raw, ok := root["mcpServers"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return root, servers, nil
	}
	if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
		return nil, nil, fmt.Errorf("mcp config %s: mcpServers must be an object", path)
	}
	return root, servers, nil
}

func applyPluginEntryToMCPJSONServer(server map[string]json.RawMessage, entry PluginEntry) error {
	transport := strings.ToLower(strings.TrimSpace(entry.Type))
	if transport == "" {
		transport = "stdio"
	}
	if transport == "stdio" {
		delete(server, "type")
		setMCPJSONString(server, "command", strings.TrimSpace(entry.Command))
		setMCPJSONStringArray(server, "args", entry.Args)
		setMCPJSONStringMap(server, "env", entry.Env)
		delete(server, "url")
		delete(server, "headers")
	} else {
		setMCPJSONString(server, "type", transport)
		setMCPJSONString(server, "url", strings.TrimSpace(entry.URL))
		setMCPJSONStringMap(server, "headers", entry.Headers)
		setMCPJSONStringMap(server, "env", entry.Env)
		delete(server, "command")
		delete(server, "args")
	}
	setMCPJSONInt(server, "startup_timeout_seconds", entry.StartupTimeoutSeconds)
	setMCPJSONInt(server, "call_timeout_seconds", entry.CallTimeoutSeconds)
	setMCPJSONIntMap(server, "tool_timeout_seconds", entry.ToolTimeoutSeconds)
	setMCPJSONStringArray(server, "disabled_tools", entry.DisabledTools)
	// The removed per-tool reader list is accepted on load for compatibility but
	// never persisted. Explicitly delete it when updating an existing shared
	// .mcp.json entry so the obsolete setting disappears naturally.
	delete(server, "trusted_read_only_tools")
	delete(server, "default_tools_approval_mode")
	delete(server, "approvals_reviewer")
	setMCPJSONBool(server, "auto_start", entry.AutoStart)
	if err := removeMCPJSONApprovalModes(server); err != nil {
		return err
	}
	return nil
}

func removeMCPJSONApprovalModes(server map[string]json.RawMessage) error {
	const key = "tools"
	tools := map[string]json.RawMessage{}
	if raw, ok := server[key]; ok && len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" {
		if err := json.Unmarshal(raw, &tools); err != nil || tools == nil {
			return fmt.Errorf("%s must be an object", key)
		}
	}

	// Remove Reasonix's retired approval_mode while preserving tool fields owned
	// by other MCP clients.
	for name, raw := range tools {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			continue
		}
		delete(fields, "approval_mode")
		if len(fields) == 0 {
			delete(tools, name)
			continue
		}
		updated, err := json.Marshal(fields)
		if err != nil {
			return fmt.Errorf("%s[%q]: %w", key, name, err)
		}
		tools[name] = updated
	}

	if len(tools) == 0 {
		delete(server, key)
		return nil
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	server[key] = raw
	return nil
}

func writeMCPJSONServers(path string, root map[string]json.RawMessage, servers map[string]json.RawMessage) error {
	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("mcp config %s: %w", path, err)
	}
	root["mcpServers"] = serversRaw
	return writeMCPJSON(path, root)
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

func setMCPJSONStringArray(server map[string]json.RawMessage, key string, values []string) {
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

func setMCPJSONInt(server map[string]json.RawMessage, key string, value int) {
	if value <= 0 {
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

func setMCPJSONIntMap(server map[string]json.RawMessage, key string, values map[string]int) {
	clean := make(map[string]int, len(values))
	for k, v := range values {
		if strings.TrimSpace(k) == "" || v <= 0 {
			continue
		}
		clean[k] = v
	}
	if len(clean) == 0 {
		delete(server, key)
		return
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		delete(server, key)
		return
	}
	server[key] = raw
}

func setMCPJSONBool(server map[string]json.RawMessage, key string, value *bool) {
	if value == nil {
		delete(server, key)
		return
	}
	raw, err := json.Marshal(*value)
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
	resolved, err := resolveConfigAccessPath(path, false)
	if err != nil {
		return fmt.Errorf("mcp config %s: %w", path, err)
	}
	dir := filepath.Dir(resolved)
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
	if err := fileutil.ReplaceFile(tmpPath, resolved); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

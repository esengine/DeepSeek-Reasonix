package installsource

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/config"
)

// mcpEntryAction assembles the DTO for a single MCP server install. The
// caller decides whether apply=true actually runs cfg.UpsertPlugin +
// SaveTo + connectMCP.
func (t *installSourceTool) mcpEntryAction(req request, e config.PluginEntry, source string) action {
	var normalizedCommand bool
	e, normalizedCommand = config.NormalizePluginCommandLine(e)
	// Tier comes from the call (req.Tier) or the entry; either way we
	// validate. An unrecognised tier is silently downgraded to "lazy" by
	// normalizeTier — we capture the original value to surface a warning.
	desired := firstNonEmpty(req.Tier, e.Tier)
	norm, ok := normalizeTier(desired)
	e.Tier = norm
	a := action{
		Kind:       "mcp",
		Action:     "install_mcp_server",
		Name:       e.Name,
		Source:     source,
		ConfigPath: t.configPath(req.Scope),
		Scope:      req.Scope,
		Transport:  pluginTransport(e),
		URL:        e.URL,
		Command:    e.Command,
		Args:       e.Args,
		Env:        e.Env,
		Headers:    e.Headers,
		entry:      e,
	}
	if !ok && strings.TrimSpace(desired) != "" {
		a.RiskReasons = append(a.RiskReasons, fmt.Sprintf("tier %q is unknown; treating as lazy", desired))
	}
	if normalizedCommand {
		a.RiskReasons = append(a.RiskReasons, "split a pasted MCP command line into command and args")
	}
	a.RiskLevel, a.RiskReasons = mcpActionRisk(e, a.RiskReasons)
	return a
}

// mcpActionRisk classifies a single MCP install. An entry with auth headers,
// an `eager` tier, or a package-name source is medium. An entry with auth
// material and an out-of-tree URL is high.
func mcpActionRisk(e config.PluginEntry, reasons []string) (RiskLevel, []string) {
	level := RiskMedium
	hasAuth := false
	for k, v := range e.Headers {
		if strings.EqualFold(k, "Authorization") || strings.Contains(strings.ToLower(v), "bearer") || strings.Contains(strings.ToLower(v), "token") {
			hasAuth = true
			reasons = append(reasons, "sends auth headers to "+e.URL)
		}
	}
	if e.Tier == "eager" {
		level = RiskHigh
		reasons = append(reasons, "tier=eager blocks startup until the handshake completes")
	}
	if hasAuth && level == RiskMedium {
		level = RiskHigh
	}
	return level, reasons
}

// remoteMCPAction builds a server entry from a URL alone. The default
// transport is http unless the URL's path smells like SSE.
func (t *installSourceTool) remoteMCPAction(req request, sourceURL string) action {
	transport := req.Transport
	if transport == "" || transport == "auto" {
		transport = "http"
		if strings.Contains(strings.ToLower(sourceURL), "sse") {
			transport = "sse"
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = mcpNameFromURL(sourceURL)
	}
	e := config.PluginEntry{
		Name:    name,
		Type:    transport,
		URL:     sourceURL,
		Headers: cleanMap(req.Headers),
		Tier:    req.Tier,
	}
	return t.mcpEntryAction(req, e, sourceURL)
}

// localExecutableMCPAction treats a chmod +x'd local file as a stdio MCP
// server. By default the file itself becomes the command; callers may override
// the command to wrap the source with an interpreter.
func (t *installSourceTool) localExecutableMCPAction(req request, path string) action {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = sanitizeName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = path
	}
	e := config.PluginEntry{
		Name:    name,
		Command: command,
		Args:    append([]string(nil), req.Args...),
		Env:     cleanMap(req.Env),
		Tier:    req.Tier,
	}
	return t.mcpEntryAction(req, e, path)
}

// packageMCPAction treats the source as an npm package name and constructs
// the canonical `npx -y <pkg>` invocation. The caller can override the
// command and args for non-npm package sources.
func (t *installSourceTool) packageMCPAction(req request) action {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = sanitizeName(strings.TrimPrefix(req.Source, "@"))
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
	}
	command := strings.TrimSpace(req.Command)
	args := append([]string(nil), req.Args...)
	if command == "" {
		command = "npx"
		args = []string{"-y", req.Source}
	}
	e := config.PluginEntry{
		Name:    name,
		Command: command,
		Args:    args,
		Env:     cleanMap(req.Env),
		Tier:    req.Tier,
	}
	return t.mcpEntryAction(req, e, req.Source)
}

// readMCPJSON reads a .mcp.json file from disk and parses it. It is a
// convenience wrapper used by planLocal; the parser itself is exported as
// parseMCPJSON for tests.
func readMCPJSON(path string) ([]config.PluginEntry, []string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	entries, warnings, err := parseMCPJSON(b)
	if err != nil {
		return nil, warnings, err
	}
	return entries, warnings, nil
}

// parseMCPJSON extracts mcpServers entries from a .mcp.json-style document.
// Warnings are returned for non-fatal anomalies (unknown tier values) and
// collected separately so a typo does not refuse an otherwise valid file.
func parseMCPJSON(b []byte) ([]config.PluginEntry, []string, error) {
	var raw struct {
		MCPServers map[string]struct {
			Type      string            `json:"type"`
			Command   string            `json:"command"`
			Args      []string          `json:"args"`
			Env       map[string]string `json:"env"`
			URL       string            `json:"url"`
			Headers   map[string]string `json:"headers"`
			AutoStart *bool             `json:"auto_start"`
			Tier      string            `json:"tier"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, nil, newErr(ErrInvalidManifest, "could not parse .mcp.json: %v", err)
	}
	if len(raw.MCPServers) == 0 {
		return nil, nil, newErr(ErrManifestMissing, ".mcp.json has no mcpServers")
	}
	names := make([]string, 0, len(raw.MCPServers))
	for name := range raw.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]config.PluginEntry, 0, len(names))
	var warnings []string
	for _, name := range names {
		s := raw.MCPServers[name]
		// Validate the raw transport before normalizeTransport gets a
		// chance to silently map an unknown value to "auto"/"stdio".
		rawType := strings.ToLower(strings.TrimSpace(s.Type))
		if rawType != "" && rawType != "stdio" && rawType != "http" && rawType != "sse" && rawType != "streamable-http" {
			return nil, warnings, newErr(ErrInvalidManifest, "MCP server %q has unknown transport %q", name, s.Type)
		}
		typ := normalizeTransport(s.Type)
		if typ == "auto" {
			if strings.TrimSpace(s.URL) != "" {
				typ = "http"
			} else {
				typ = "stdio"
			}
		}
		tier, ok := normalizeTier(s.Tier)
		if !ok && strings.TrimSpace(s.Tier) != "" {
			warnings = append(warnings, fmt.Sprintf("%s: tier %q is unknown; treating as lazy", name, s.Tier))
		}
		e := config.PluginEntry{
			Name:      name,
			Type:      typ,
			Command:   strings.TrimSpace(s.Command),
			Args:      append([]string(nil), s.Args...),
			Env:       cleanMap(s.Env),
			URL:       strings.TrimSpace(s.URL),
			Headers:   cleanMap(s.Headers),
			AutoStart: s.AutoStart,
			Tier:      tier,
		}
		// An empty Type is the canonical "stdio" form; keep it that way so
		// the rest of the config layer (UpsertPlugin / ShouldAutoStart)
		// can treat "" and "stdio" identically.
		if e.Type == "stdio" {
			e.Type = ""
		}
		normalized, changed := config.NormalizePluginCommandLine(e)
		e = normalized
		if changed {
			warnings = append(warnings, fmt.Sprintf("%s: split a pasted MCP command line into command and args", name))
		}
		if err := validateMCPEntry(e); err != nil {
			return nil, warnings, err
		}
		out = append(out, e)
	}
	return out, warnings, nil
}

// validateMCPEntry enforces the per-transport required fields. It is the
// last line of defense before a server entry is persisted.
func validateMCPEntry(e config.PluginEntry) error {
	name := strings.TrimSpace(e.Name)
	if name == "" {
		return newErr(ErrInvalidManifest, "MCP server name is required")
	}
	if !config.IsValidSkillName(name) {
		return newErr(ErrInvalidManifest, "MCP server name %q is invalid; use letters, digits, '.', '_', or '-', starting with a letter or digit, up to 64 characters", e.Name)
	}
	// Reject explicit unknown transports. Without this check, normalizeTransport
	// would silently map "carrier-pigeon" -> "auto" -> "stdio" and the entry
	// would be persisted with the wrong effective type.
	rawType := strings.ToLower(strings.TrimSpace(e.Type))
	if rawType != "" && rawType != "stdio" && rawType != "http" && rawType != "sse" {
		return newErr(ErrInvalidManifest, "MCP server %q has unknown transport %q", e.Name, e.Type)
	}
	switch pluginTransport(e) {
	case "stdio":
		if strings.TrimSpace(e.Command) == "" {
			return newErr(ErrInvalidManifest, "MCP server %q is stdio but has no command", e.Name)
		}
	case "http", "sse":
		if strings.TrimSpace(e.URL) == "" {
			return newErr(ErrInvalidManifest, "MCP server %q is remote but has no url", e.Name)
		}
	default:
		return newErr(ErrInvalidManifest, "MCP server %q has unknown transport %q", e.Name, e.Type)
	}
	return nil
}

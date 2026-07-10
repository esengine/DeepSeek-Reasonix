package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/secrets"
)

const ccSwitchDir = ".cc-switch"

type ccSwitchMCPRow struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ServerConfig string `json:"server_config"`
}

type ccSwitchLegacyServer struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Server mcpServerSpec `json:"server"`
	Apps   struct {
		Codex bool `json:"codex"`
	} `json:"apps"`
}

type MCPImportCandidate struct {
	Entry       PluginEntry
	Recommended bool
	Reasons     []string
}

func LoadCCSwitchMCPCandidates() ([]MCPImportCandidate, error) {
	entries, err := LoadCCSwitchMCP()
	if err != nil {
		return nil, err
	}
	candidates := make([]MCPImportCandidate, len(entries))
	for i, e := range entries {
		candidates[i] = classifyMCPImportCandidate(e)
	}
	return candidates, nil
}

// LoadCCSwitchMCP reads MCP servers enabled for Codex from cc-switch and maps
// them to Reasonix plugin entries. Newer cc-switch stores servers in SQLite;
// older installs kept them in config.json(.migrated/.bak), so we support both.
func LoadCCSwitchMCP() ([]PluginEntry, error) {
	if IsolatedHomeDir() != "" {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cc-switch import: resolve home: %w", err)
	}
	return loadCCSwitchMCPFromRoot(filepath.Join(home, ccSwitchDir))
}

func loadCCSwitchMCPFromRoot(root string) ([]PluginEntry, error) {
	dbPath := filepath.Join(root, "cc-switch.db")
	if _, err := os.Stat(dbPath); err == nil {
		entries, err := loadCCSwitchMCPDB(dbPath)
		if err != nil {
			return nil, err
		}
		return entries, nil
	} else if !os.IsNotExist(err) {
		// A present but unreadable/corrupt database should be visible to the user.
		return nil, err
	}

	for _, name := range []string{"config.json", "config.json.migrated", "config.json.bak"} {
		entries, err := loadCCSwitchLegacyConfig(filepath.Join(root, name))
		if err == nil && len(entries) > 0 {
			return entries, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("cc-switch import: no Codex-enabled MCP servers found in %s", root)
}

func ImportCCSwitchMCPEntries(entries []PluginEntry) (total, added, updated int, err error) {
	cfg, err := Load()
	if err != nil {
		return 0, 0, 0, err
	}
	return importMCPEntries(cfg, entries)
}

// ImportCCSwitchMCP upserts cc-switch's Codex-enabled MCP servers into the
// active Reasonix config and saves it.
func ImportCCSwitchMCP() (total, added, updated int, err error) {
	entries, err := LoadCCSwitchMCP()
	if err != nil {
		return 0, 0, 0, err
	}
	cfg, err := Load()
	if err != nil {
		return 0, 0, 0, err
	}
	return importMCPEntries(cfg, entries)
}

func importMCPEntries(cfg *Config, entries []PluginEntry) (total, added, updated int, err error) {
	existing := make(map[string]PluginEntry, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		existing[p.Name] = p
	}
	for _, e := range entries {
		if _, ok := existing[e.Name]; ok {
			updated++
		} else {
			added++
		}
		if err := cfg.UpsertPlugin(e); err != nil {
			return 0, 0, 0, err
		}
		existing[e.Name] = e
	}
	if err := cfg.Save(); err != nil {
		return 0, 0, 0, err
	}
	return len(entries), added, updated, nil
}

func loadCCSwitchMCPDB(path string) ([]PluginEntry, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, fmt.Errorf("cc-switch import: sqlite3 not found to read %s", path)
	}
	query := `SELECT id, name, server_config FROM mcp_servers WHERE enabled_codex = 1 ORDER BY name, id`
	cmd := exec.Command(sqlite, "-readonly", "-json", path, query)
	cmd.Env = secrets.ProcessEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cc-switch import: read %s: %w", path, err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return nil, nil
	}
	var rows []ccSwitchMCPRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("cc-switch import: parse sqlite output: %w", err)
	}
	return ccSwitchRowsToPlugins(rows)
}

func ccSwitchRowsToPlugins(rows []ccSwitchMCPRow) ([]PluginEntry, error) {
	entries := make([]PluginEntry, 0, len(rows))
	for _, row := range rows {
		var s mcpServerSpec
		if err := json.Unmarshal([]byte(row.ServerConfig), &s); err != nil {
			return nil, fmt.Errorf("cc-switch import: server %q config: %w", row.Name, err)
		}
		name := strings.TrimSpace(row.ID)
		if name == "" {
			name = row.Name
		}
		e := pluginFromMCPServerSpec(name, s)
		if err := validatePlugin(e); err != nil {
			return nil, fmt.Errorf("cc-switch import: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func loadCCSwitchLegacyConfig(path string) ([]PluginEntry, error) {
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		MCP struct {
			Servers map[string]ccSwitchLegacyServer `json:"servers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("cc-switch import: parse %s: %w", path, err)
	}
	keys := make([]string, 0, len(doc.MCP.Servers))
	for key := range doc.MCP.Servers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var entries []PluginEntry
	for _, key := range keys {
		srv := doc.MCP.Servers[key]
		if !srv.Apps.Codex {
			continue
		}
		name := srv.Name
		if name == "" {
			name = srv.ID
		}
		if name == "" {
			name = key
		}
		e := pluginFromMCPServerSpec(name, srv.Server)
		if err := validatePlugin(e); err != nil {
			return nil, fmt.Errorf("cc-switch import: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func pluginFromMCPServerSpec(name string, s mcpServerSpec) PluginEntry {
	return PluginEntry{
		Name:    name,
		Type:    s.Type,
		Command: s.Command,
		Args:    s.Args,
		Env:     s.Env,
		URL:     s.URL,
		Headers: s.Headers,
	}
}

func classifyMCPImportCandidate(e PluginEntry) MCPImportCandidate {
	c := MCPImportCandidate{Entry: e, Recommended: true}
	typ := strings.ToLower(strings.TrimSpace(e.Type))
	name := strings.ToLower(e.Name)
	cmd := strings.ToLower(filepath.Base(e.Command))
	if typ == "sse" {
		c.Reasons = append(c.Reasons, "unsupported sse")
		c.Recommended = false
	}
	if strings.Contains(name, "chrome-devtools") {
		c.Reasons = append(c.Reasons, "browser/heavy")
		c.Recommended = false
	}
	if cmd == "npx" || cmd == "uvx" {
		for _, a := range e.Args {
			if strings.Contains(strings.ToLower(a), "@latest") {
				c.Reasons = append(c.Reasons, "@latest")
				c.Recommended = false
				break
			}
		}
	}
	if len(e.Headers) > 0 || len(e.Env) > 0 {
		c.Reasons = append(c.Reasons, "auth/env")
		if !isCommonDocMCP(name) {
			c.Recommended = false
		}
	}
	if strings.Contains(name, "flomo") || strings.Contains(name, "dida") ||
		strings.Contains(name, "ynote") || strings.Contains(name, "youdao") {
		c.Reasons = append(c.Reasons, "personal")
		c.Recommended = false
	}
	if c.Recommended {
		c.Reasons = append([]string{"recommended"}, c.Reasons...)
	}
	if len(c.Reasons) == 0 {
		c.Reasons = append(c.Reasons, "candidate")
	}
	return c
}

func isCommonDocMCP(name string) bool {
	return strings.Contains(name, "context7") || strings.Contains(name, "exa") || strings.Contains(name, "fetch")
}

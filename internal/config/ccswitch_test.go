package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCCSwitchRowsToPlugins(t *testing.T) {
	rows := []ccSwitchMCPRow{
		{ID: "docs-id", Name: "docs", ServerConfig: `{"type":"http","url":"https://mcp.example.test","headers":{"Authorization":"Bearer ${TOKEN}"}}`},
		{Name: "fs", ServerConfig: `{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","."]}`},
	}
	got, err := ccSwitchRowsToPlugins(rows)
	if err != nil {
		t.Fatalf("ccSwitchRowsToPlugins: %v", err)
	}
	if got[0].Name != "docs-id" || got[0].Type != "http" || got[0].URL != "https://mcp.example.test" {
		t.Fatalf("http entry = %+v", got[0])
	}
	if got[0].Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Errorf("header was not preserved: %+v", got[0].Headers)
	}
	if got[1].Name != "fs" || got[1].Command != "npx" ||
		!reflect.DeepEqual(got[1].Args, []string{"-y", "@modelcontextprotocol/server-filesystem", "."}) {
		t.Fatalf("stdio entry = %+v", got[1])
	}
}

func TestCCSwitchRowsPreferIDForDuplicateDisplayNames(t *testing.T) {
	rows := []ccSwitchMCPRow{
		{ID: "search-code", Name: "search", ServerConfig: `{"command":"node","args":["code.js"]}`},
		{ID: "search-docs", Name: "search", ServerConfig: `{"command":"node","args":["docs.js"]}`},
	}
	got, err := ccSwitchRowsToPlugins(rows)
	if err != nil {
		t.Fatalf("ccSwitchRowsToPlugins: %v", err)
	}
	if got[0].Name != "search-code" || got[1].Name != "search-docs" {
		t.Fatalf("names = %q, %q; want stable ids", got[0].Name, got[1].Name)
	}
}

func TestCCSwitchImportClassifiesRiskyServers(t *testing.T) {
	rows := []ccSwitchMCPRow{
		{Name: "@modelcontextprotocol/server-chrome-devtools", ServerConfig: `{"command":"npx","args":["-y","chrome-devtools-mcp@latest"]}`},
		{Name: "legacy", ServerConfig: `{"type":"sse","url":"https://example.test/sse"}`},
	}
	got, err := ccSwitchRowsToPlugins(rows)
	if err != nil {
		t.Fatalf("ccSwitchRowsToPlugins: %v", err)
	}
	for _, e := range got {
		candidate := classifyMCPImportCandidate(e)
		if candidate.Recommended {
			t.Fatalf("%s should not be recommended: %+v", e.Name, candidate)
		}
	}
}

func TestLoadCCSwitchLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json.migrated")
	body := `{
		"mcp": {
			"servers": {
				"off": {
					"name": "off",
					"server": {"command": "node", "args": ["off.js"]},
					"apps": {"codex": false}
				},
				"time": {
					"name": "@modelcontextprotocol/server-time",
					"server": {"type":"stdio", "command": "uvx", "args": ["mcp-server-time"]},
					"apps": {"codex": true}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadCCSwitchLegacyConfig(path)
	if err != nil {
		t.Fatalf("loadCCSwitchLegacyConfig: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1: %+v", len(got), got)
	}
	if got[0].Name != "@modelcontextprotocol/server-time" || got[0].Command != "uvx" {
		t.Fatalf("entry = %+v", got[0])
	}
}

func TestLoadCCSwitchLegacyConfigPrefersReasonixFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
		"mcp": {
			"servers": {
				"legacy": {
					"name": "legacy",
					"server": {"command": "node", "args": ["legacy.js"]},
					"apps": {"codex": true}
				},
				"reasonix-off": {
					"name": "reasonix-off",
					"server": {"command": "node", "args": ["off.js"]},
					"apps": {"codex": true, "reasonix": false}
				},
				"reasonix-on": {
					"name": "reasonix-on",
					"server": {"command": "node", "args": ["on.js"]},
					"apps": {"codex": false, "reasonix": true}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadCCSwitchLegacyConfig(path)
	if err != nil {
		t.Fatalf("loadCCSwitchLegacyConfig: %v", err)
	}
	if len(got) != 2 || got[0].Name != "legacy" || got[1].Name != "reasonix-on" {
		t.Fatalf("entries = %+v, want legacy fallback and explicit Reasonix enablement", got)
	}
}

func TestLoadCCSwitchMCPDBPrefersReasonixColumn(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	dbPath := filepath.Join(t.TempDir(), "cc-switch.db")
	setup := `CREATE TABLE mcp_servers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		server_config TEXT NOT NULL,
		enabled_codex BOOLEAN NOT NULL DEFAULT 0,
		enabled_reasonix BOOLEAN NOT NULL DEFAULT 0
	);
	INSERT INTO mcp_servers VALUES ('codex-only', 'codex-only', '{"command":"node","args":["codex.js"]}', 1, 0);
	INSERT INTO mcp_servers VALUES ('reasonix-only', 'reasonix-only', '{"command":"node","args":["reasonix.js"]}', 0, 1);`
	if out, err := exec.Command("sqlite3", dbPath, setup).CombinedOutput(); err != nil {
		t.Fatalf("create sqlite db: %v\n%s", err, out)
	}
	got, err := loadCCSwitchMCPDB(dbPath)
	if err != nil {
		t.Fatalf("loadCCSwitchMCPDB: %v", err)
	}
	if len(got) != 1 || got[0].Name != "reasonix-only" {
		t.Fatalf("entries = %+v, want only explicit Reasonix enablement", got)
	}
}

func TestLoadCCSwitchMCPDBFallsBackToCodexWithoutReasonixColumn(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	dbPath := filepath.Join(t.TempDir(), "cc-switch.db")
	setup := `CREATE TABLE mcp_servers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		server_config TEXT NOT NULL,
		enabled_codex BOOLEAN NOT NULL DEFAULT 0
	);
	INSERT INTO mcp_servers VALUES ('codex-on', 'codex-on', '{"command":"node","args":["on.js"]}', 1);
	INSERT INTO mcp_servers VALUES ('codex-off', 'codex-off', '{"command":"node","args":["off.js"]}', 0);`
	if out, err := exec.Command("sqlite3", dbPath, setup).CombinedOutput(); err != nil {
		t.Fatalf("create sqlite db: %v\n%s", err, out)
	}
	got, err := loadCCSwitchMCPDB(dbPath)
	if err != nil {
		t.Fatalf("loadCCSwitchMCPDB: %v", err)
	}
	if len(got) != 1 || got[0].Name != "codex-on" {
		t.Fatalf("entries = %+v, want legacy Codex fallback", got)
	}
}

func TestLoadCCSwitchMCPEmptyDBDoesNotReadLegacyBackups(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	root := t.TempDir()
	dbPath := filepath.Join(root, "cc-switch.db")
	if out, err := exec.Command("sqlite3", dbPath, `CREATE TABLE mcp_servers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		server_config TEXT NOT NULL,
		enabled_codex BOOLEAN NOT NULL DEFAULT 0
	);`).CombinedOutput(); err != nil {
		t.Fatalf("create sqlite db: %v\n%s", err, out)
	}
	stale := `{"mcp":{"servers":{"stale":{"name":"stale","server":{"command":"node","args":["stale.js"]},"apps":{"codex":true}}}}}`
	if err := os.WriteFile(filepath.Join(root, "config.json.migrated"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadCCSwitchMCPFromRoot(root)
	if err != nil {
		t.Fatalf("loadCCSwitchMCPFromRoot: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty sqlite db should be authoritative, got legacy entries: %+v", got)
	}
}

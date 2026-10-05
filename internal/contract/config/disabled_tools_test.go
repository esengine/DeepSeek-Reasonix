package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDisabledToolsSurviveEveryTOMLRenderScope(t *testing.T) {
	for _, scope := range []RenderScope{RenderScopeFull, RenderScopeUser, RenderScopeProject, "delta"} {
		t.Run(string(scope), func(t *testing.T) {
			cfg := Default()
			cfg.Plugins = []PluginEntry{{Name: "server", Command: "mcp", DisabledTools: []string{"write_file", `name"with-quote`}}}
			rendered := RenderTOMLForScope(cfg, scope)
			if scope == "delta" {
				rendered = RenderTOMLProjectDelta(cfg)
			}
			var got Config
			if _, err := toml.Decode(rendered, &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Plugins) != 1 || !slices.Equal(got.Plugins[0].DisabledTools, cfg.Plugins[0].DisabledTools) {
				t.Fatalf("roundtrip lost disabled tools: %+v", got.Plugins)
			}
			cfg.Plugins[0].DisabledTools = nil
			rendered = RenderTOMLForScope(cfg, scope)
			if scope == "delta" {
				rendered = RenderTOMLProjectDelta(cfg)
			}
			if strings.Contains(rendered, "disabled_tools") {
				t.Fatal("unset policy should not be rendered")
			}
		})
	}
}

func TestDisabledToolsSurviveMCPJSONEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	entry := PluginEntry{Name: "server", Command: "mcp", DisabledTools: []string{"write_file", "delete_file"}}
	for _, names := range [][]string{entry.DisabledTools, {}} {
		entry.DisabledTools = names
		if _, err := UpsertMCPJSONPlugin(path, entry); err != nil {
			t.Fatal(err)
		}
		got, err := loadMCPJSON(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || !slices.Equal(got[0].DisabledTools, names) {
			t.Fatalf("MCP JSON roundtrip = %+v", got)
		}
	}
}

func TestValidatePluginRejectsEmptyDisabledToolNames(t *testing.T) {
	for _, name := range []string{"", "  "} {
		entry := PluginEntry{Name: "server", Command: "mcp", DisabledTools: []string{name}}
		if err := validatePlugin(entry); err == nil {
			t.Fatalf("disabled_tools entry %q was accepted", name)
		}
	}
	if err := validatePlugin(PluginEntry{Name: "server", Command: "mcp", DisabledTools: []string{"write"}}); err != nil {
		t.Fatalf("valid disabled_tools entry was rejected: %v", err)
	}
}

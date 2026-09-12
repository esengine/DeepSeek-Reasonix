package config

import (
	"strings"
	"testing"
)

func TestNormalizeLoadedPermissionPresetsMigratesLegacyValues(t *testing.T) {
	cfg := Default()
	cfg.Desktop.DefaultToolApprovalMode = "ask"
	cfg.Bot.ToolApprovalMode = "auto"
	cfg.Bot.Connections = []BotConnectionConfig{{ToolApprovalMode: "yolo"}}
	cfg.Bot.Routes = []BotRouteConfig{{ToolApprovalMode: "full-access"}}

	if err := normalizeLoadedConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Desktop.DefaultToolApprovalMode; got != "read-only" {
		t.Fatalf("desktop preset = %q, want read-only", got)
	}
	if got := cfg.Bot.ToolApprovalMode; got != "workspace-write" {
		t.Fatalf("bot preset = %q, want workspace-write", got)
	}
	if got := cfg.Bot.Connections[0].ToolApprovalMode; got != "workspace-write" {
		t.Fatalf("connection preset = %q, want workspace-write", got)
	}
	if got := cfg.Bot.Routes[0].ToolApprovalMode; got != "danger-full-access" {
		t.Fatalf("route preset = %q, want danger-full-access", got)
	}
	if cfg.HasLoadWarnings() {
		t.Fatalf("known migration produced warnings: %v", cfg.LoadWarnings())
	}
}

func TestNormalizeLoadedPermissionPresetUnknownFailsClosedWithWarning(t *testing.T) {
	cfg := Default()
	cfg.Desktop.DefaultToolApprovalMode = "mystery-mode"
	cfg.Bot.Connections = []BotConnectionConfig{{ToolApprovalMode: "future-mode"}}

	if err := normalizeLoadedConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Desktop.DefaultToolApprovalMode; got != "read-only" {
		t.Fatalf("desktop preset = %q, want read-only", got)
	}
	if got := cfg.Bot.Connections[0].ToolApprovalMode; got != "read-only" {
		t.Fatalf("connection preset = %q, want read-only", got)
	}
	warnings := strings.Join(cfg.LoadWarnings(), "\n")
	if !strings.Contains(warnings, "desktop.default_tool_approval_mode") || !strings.Contains(warnings, "bot.connections[0].tool_approval_mode") {
		t.Fatalf("warnings = %q, want both incompatible fields", warnings)
	}
}

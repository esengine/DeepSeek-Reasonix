package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

func TestDesktopBotRuntimePlanStartsSavedConnections(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Enabled = true
	cfg.Bot.Allowlist.Enabled = true
	cfg.Bot.Allowlist.FeishuUsers = []string{"ou-installer"}
	cfg.Bot.Allowlist.WeixinUsers = []string{"wx-user"}
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Enabled: true},
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true},
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Enabled: true},
	}

	plan := desktopBotRuntimePlan(cfg)
	if !plan.Start {
		t.Fatalf("plan = %+v, want start", plan)
	}
	if !plan.Enabled[bot.PlatformFeishu] || !plan.Enabled[bot.PlatformWeixin] {
		t.Fatalf("enabled = %+v, want feishu/lark and weixin platforms", plan.Enabled)
	}
}

func TestDesktopBotRuntimePlanBlocksWithoutAllowlist(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Enabled = true
	cfg.Bot.Allowlist.Enabled = true
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true},
	}

	plan := desktopBotRuntimePlan(cfg)
	if plan.Start || plan.Status != "blocked" {
		t.Fatalf("plan = %+v, want blocked without allowlist", plan)
	}
}

func TestDesktopBotRuntimePlanStopsWhenBotDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Enabled = false
	cfg.Bot.Allowlist.FeishuUsers = []string{"ou-installer"}
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true},
	}

	plan := desktopBotRuntimePlan(cfg)
	if plan.Start || plan.Status != "stopped" {
		t.Fatalf("plan = %+v, want stopped when disabled", plan)
	}
}

func TestDesktopBotRuntimeConfigUsesUserBotSettings(t *testing.T) {
	isolateDesktopUserDirs(t)

	userCfg := config.LoadForEdit(config.UserConfigPath())
	userCfg.Bot.Enabled = true
	userCfg.Bot.Allowlist.Enabled = true
	userCfg.Bot.Allowlist.FeishuUsers = []string{"ou-installer"}
	userCfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true, Status: "connected"},
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	project := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[bot]
enabled = false
`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir project: %v", err)
	}

	got, err := NewApp().loadDesktopBotConfig()
	if err != nil {
		t.Fatalf("load desktop bot config: %v", err)
	}
	plan := desktopBotRuntimePlan(got)
	if !plan.Start || !plan.Enabled[bot.PlatformFeishu] {
		t.Fatalf("desktop runtime plan = %+v, want user-level Lark connection to start", plan)
	}
}

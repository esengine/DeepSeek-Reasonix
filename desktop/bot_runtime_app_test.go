package main

import (
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

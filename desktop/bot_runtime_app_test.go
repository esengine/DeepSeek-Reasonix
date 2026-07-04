package main

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/bot"
	"reasonix/internal/botruntime"
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
	cfg.Bot.Pairing.Enabled = false
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true},
	}

	plan := desktopBotRuntimePlan(cfg)
	if plan.Start || plan.Status != "blocked" {
		t.Fatalf("plan = %+v, want blocked without allowlist", plan)
	}
}

func TestDesktopBotRuntimePlanStartsWithPairing(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Enabled = true
	cfg.Bot.Allowlist.Enabled = true
	cfg.Bot.Pairing.Enabled = true
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true},
	}

	plan := desktopBotRuntimePlan(cfg)
	if !plan.Start {
		t.Fatalf("plan = %+v, want start with pairing enabled", plan)
	}
}

func TestDesktopBotChannelsWithLegacyQQConfig(t *testing.T) {
	channels, connectionChannels := desktopBotChannelsWithLegacyQQ(config.QQBotConfig{
		Model:            "qq-model",
		ToolApprovalMode: "auto",
		WorkspaceRoot:    "/tmp/qq-project",
	}, nil, nil)

	channel, ok := channels[bot.PlatformQQ]
	if !ok {
		t.Fatalf("platform QQ channel missing: %+v", channels)
	}
	if channel.Model != "qq-model" || channel.ToolApprovalMode != "auto" || channel.WorkspaceRoot != "/tmp/qq-project" {
		t.Fatalf("platform channel = %+v, want QQ-specific runtime fields", channel)
	}
	connectionChannel, ok := connectionChannels["qq"]
	if !ok {
		t.Fatalf("connection QQ channel missing: %+v", connectionChannels)
	}
	if connectionChannel.Model != "qq-model" || connectionChannel.ToolApprovalMode != "auto" || connectionChannel.WorkspaceRoot != "/tmp/qq-project" {
		t.Fatalf("connection channel = %+v, want QQ-specific runtime fields", connectionChannel)
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

func TestDesktopBotRuntimeForwardTargetsDeduplicatesMappedChats(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID:       "feishu-lark",
		Provider: "feishu",
		Domain:   "lark",
		Enabled:  true,
		SessionMappings: []config.BotConnectionSessionMapping{
			{RemoteID: "oc-group-1", ChatType: string(bot.ChatGroup), UserID: "ou-user-1"},
			{RemoteID: "oc-group-1", ChatType: string(bot.ChatGroup), UserID: "ou-user-2"},
			{RemoteID: "oc-dm-1", ChatType: string(bot.ChatDM), UserID: "ou-user-1"},
		},
	}}

	targets := newDesktopBotRuntime().ForwardTargets(cfg)
	if len(targets) != 2 {
		t.Fatalf("targets = %+v, want one group target plus one dm target", targets)
	}
	seen := map[string]bool{}
	for _, target := range targets {
		key := target.ConnID + "|" + target.Domain + "|" + target.ChatID + "|" + string(target.ChatType)
		if seen[key] {
			t.Fatalf("duplicate target %q in %+v", key, targets)
		}
		seen[key] = true
	}
	if !seen["feishu-lark|lark|oc-group-1|group"] || !seen["feishu-lark|lark|oc-dm-1|dm"] {
		t.Fatalf("targets = %+v, want group and dm targets", targets)
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

func TestDesktopBotRuntimeConfigLoadsAllSavedCredentialsAfterRestart(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Cleanup(func() {
		_ = os.Unsetenv("FEISHU_BOT_APP_SECRET")
		_ = os.Unsetenv("LARK_BOT_APP_SECRET")
	})

	userCfg := config.Default()
	userCfg.Bot.Enabled = true
	userCfg.Bot.Allowlist.Enabled = true
	userCfg.Bot.Allowlist.FeishuUsers = []string{"ou-feishu-installer", "ou-lark-installer"}
	userCfg.Bot.Allowlist.WeixinUsers = []string{"wx-installer"}
	userCfg.Bot.Feishu.Enabled = true
	userCfg.Bot.Weixin.Enabled = true
	userCfg.Bot.Weixin.AccountID = "weixin-account"
	userCfg.Bot.Weixin.TokenEnv = "WEIXIN_BOT_TOKEN"
	userCfg.Bot.Connections = []config.BotConnectionConfig{
		{
			ID:       "feishu-feishu",
			Provider: "feishu",
			Domain:   "feishu",
			Enabled:  true,
			Status:   "connected",
			Credential: config.BotConnectionCredential{
				AppID:        "cli-feishu",
				AppSecretEnv: "FEISHU_BOT_APP_SECRET",
			},
		},
		{
			ID:       "feishu-lark",
			Provider: "feishu",
			Domain:   "lark",
			Enabled:  true,
			Status:   "connected",
			Credential: config.BotConnectionCredential{
				AppID:        "cli-lark",
				AppSecretEnv: "LARK_BOT_APP_SECRET",
			},
		},
		{
			ID:       "weixin-weixin",
			Provider: "weixin",
			Domain:   "weixin",
			Enabled:  true,
			Status:   "connected",
			Credential: config.BotConnectionCredential{
				AccountID: "weixin-account",
				TokenEnv:  "WEIXIN_BOT_TOKEN",
			},
		},
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(config.UserCredentialsPath()), 0o755); err != nil {
		t.Fatalf("create credentials dir: %v", err)
	}
	if err := os.WriteFile(config.UserCredentialsPath(), []byte("FEISHU_BOT_APP_SECRET=feishu-secret\nLARK_BOT_APP_SECRET=lark-secret\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	weixinAccountPath := filepath.Join(config.MemoryUserDir(), "weixin", "accounts", "weixin-account.json")
	if err := os.MkdirAll(filepath.Dir(weixinAccountPath), 0o700); err != nil {
		t.Fatalf("create weixin account dir: %v", err)
	}
	if err := os.WriteFile(weixinAccountPath, []byte(`{"token":"weixin-token","base_url":"https://ilinkai.weixin.qq.com","user_id":"wx-installer"}`), 0o600); err != nil {
		t.Fatalf("write weixin account: %v", err)
	}
	_ = os.Unsetenv("FEISHU_BOT_APP_SECRET")
	_ = os.Unsetenv("LARK_BOT_APP_SECRET")

	got, err := NewApp().loadDesktopBotConfig()
	if err != nil {
		t.Fatalf("load desktop bot config: %v", err)
	}
	views := botConnectionViews(got.Bot.Connections)
	if len(views) != 3 {
		t.Fatalf("connection views = %+v, want Feishu, Lark, and Weixin", views)
	}
	for _, view := range views {
		if !view.Credential.SecretSet {
			t.Fatalf("connection %s credential = %+v, want saved credential loaded after restart", view.ID, view.Credential)
		}
	}
	plan := desktopBotRuntimePlan(got)
	if !plan.Start || !plan.Enabled[bot.PlatformFeishu] || !plan.Enabled[bot.PlatformWeixin] {
		t.Fatalf("desktop runtime plan = %+v, want saved Feishu/Lark/Weixin connections to start", plan)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bindings := botruntime.AdapterBindings(got, plan.Enabled, nil, logger)
	if len(bindings) != 3 {
		t.Fatalf("adapter bindings = %+v, want one per saved connection", bindings)
	}
}

func TestDesktopBotRuntimeMigratesLegacyProjectBotSettings(t *testing.T) {
	isolateDesktopUserDirs(t)

	userCfg := config.Default()
	if err := userCfg.SetDesktopAppearance("dark", "graphite"); err != nil {
		t.Fatalf("set desktop appearance: %v", err)
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	project := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[bot]
enabled = true

[bot.allowlist]
enabled = true
feishu_users = ["ou-legacy"]

[[bot.connections]]
id = "feishu-lark"
provider = "feishu"
domain = "lark"
label = "Lark"
enabled = true
status = "connected"
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
	if !got.Bot.Enabled || len(got.Bot.Connections) != 1 || got.Bot.Connections[0].ID != "feishu-lark" {
		t.Fatalf("desktop bot config = %+v, want migrated legacy Lark connection", got.Bot)
	}

	persisted := config.LoadForEdit(config.UserConfigPath())
	if !persisted.Bot.Enabled || len(persisted.Bot.Connections) != 1 || persisted.Bot.Connections[0].ID != "feishu-lark" {
		t.Fatalf("persisted bot config = %+v, want migrated legacy Lark connection", persisted.Bot)
	}
	if persisted.DesktopTheme() != "dark" {
		t.Fatalf("desktop theme = %q, want preserved user preference", persisted.DesktopTheme())
	}
}

func TestDesktopBotRuntimePersistsLegacyProjectBotWhenUserConfigMissing(t *testing.T) {
	isolateDesktopUserDirs(t)

	project := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[desktop]
theme = "dark"

[bot]
enabled = true

[bot.allowlist]
enabled = true
feishu_users = ["ou-legacy"]

[[bot.connections]]
id = "feishu-lark"
provider = "feishu"
domain = "lark"
label = "Lark"
enabled = true
status = "connected"
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
	if !got.Bot.Enabled || len(got.Bot.Connections) != 1 || got.Bot.Connections[0].ID != "feishu-lark" {
		t.Fatalf("desktop bot config = %+v, want migrated legacy Lark connection", got.Bot)
	}

	persisted := config.LoadForEdit(config.UserConfigPath())
	if !persisted.Bot.Enabled || len(persisted.Bot.Connections) != 1 || persisted.Bot.Connections[0].ID != "feishu-lark" {
		t.Fatalf("persisted bot config = %+v, want migrated legacy Lark connection", persisted.Bot)
	}
	if persisted.DesktopTheme() == "dark" {
		t.Fatal("legacy project desktop theme should not be persisted during bot-only migration")
	}
}

func TestDesktopSettingsBotMigrationPersistsOnlyBotBeforeFirstEdit(t *testing.T) {
	isolateDesktopUserDirs(t)

	project := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[desktop]
theme = "dark"
close_behavior = "quit"

[bot]
enabled = true

[bot.allowlist]
enabled = true
feishu_users = ["ou-legacy"]

[[bot.connections]]
id = "feishu-lark"
provider = "feishu"
domain = "lark"
label = "Lark"
enabled = true
status = "connected"
`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir project: %v", err)
	}

	settings := NewApp().Settings()
	if !settings.Bot.Enabled || len(settings.Bot.Connections) != 1 || settings.Bot.Connections[0].ID != "feishu-lark" {
		t.Fatalf("settings bot = %+v, want migrated legacy Lark connection", settings.Bot)
	}
	if settings.DesktopTheme != "dark" || settings.CloseBehavior != "quit" {
		t.Fatalf("settings desktop prefs = theme:%q close:%q, want legacy seed visible before first edit", settings.DesktopTheme, settings.CloseBehavior)
	}

	persisted := config.LoadForEdit(config.UserConfigPath())
	if persisted.DesktopTheme() == "dark" || persisted.DesktopCloseBehavior() == "quit" {
		t.Fatalf("persisted desktop prefs = theme:%q close:%q, want bot-only migration", persisted.DesktopTheme(), persisted.DesktopCloseBehavior())
	}
}

func TestDesktopBotRuntimeMigrationDoesNotOverwriteUserBotSettings(t *testing.T) {
	isolateDesktopUserDirs(t)

	userCfg := config.Default()
	userCfg.Bot.Enabled = true
	userCfg.Bot.Allowlist.Enabled = true
	userCfg.Bot.Allowlist.WeixinUsers = []string{"wx-user"}
	userCfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Enabled: true, Status: "connected"},
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	project := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[bot]
enabled = true

[bot.allowlist]
enabled = true
feishu_users = ["ou-legacy"]

[[bot.connections]]
id = "feishu-lark"
provider = "feishu"
domain = "lark"
enabled = true
status = "connected"
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
	if len(got.Bot.Connections) != 1 || got.Bot.Connections[0].ID != "weixin-weixin" {
		t.Fatalf("desktop bot config = %+v, want existing user WeChat connection", got.Bot)
	}
}

func TestSummarizeBotRuntimeErrorsCapsOutput(t *testing.T) {
	got := summarizeBotRuntimeErrors([]error{
		errors.New("first"),
		nil,
		errors.New("second"),
		errors.New("third"),
		errors.New("fourth"),
	})

	for _, want := range []string{"first", "second", "third", "1 more"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "fourth") {
		t.Fatalf("summary = %q, should cap extra errors", got)
	}
}

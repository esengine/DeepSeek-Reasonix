package main

import (
	"testing"

	"reasonix/internal/config"
)

// TestSetBotSettingsDingtalkRoundTrip 验证钉钉配置经 SetBotSettings 后能正确落盘并被读回。
// 这是排查「钉钉配置界面几个字段写进去无法保存」的回归测试。
func TestSetBotSettingsDingtalkRoundTrip(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Bot.Dingtalk = config.DingtalkBotConfig{
		Enabled:        true,
		ClientID:       "appkey-initial",
		SecretEnv:      "DINGTALK_CLIENT_SECRET",
		BotName:        "InitialBot",
		RequireMention: true,
		SessionMappings: []config.BotConnectionSessionMapping{{
			RemoteID:      "cid-preserved",
			Scope:         "global",
			SessionID:     "path:/sessions/preserved.jsonl",
			SessionSource: "auto",
		}},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save initial: %v", err)
	}

	app := NewApp()
	view := botSettingsView(cfg.Bot)
	view.Dingtalk.ClientID = "appkey-edited"
	view.Dingtalk.BotName = "EditedBot"
	view.Dingtalk.RequireMention = false
	view.Dingtalk.ClientSecretEnv = "DINGTALK_CLIENT_SECRET"
	if err := app.SetBotSettings(view); err != nil {
		t.Fatalf("SetBotSettings: %v", err)
	}

	got := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	if got.Bot.Dingtalk.ClientID != "appkey-edited" {
		t.Fatalf("clientId = %q, want appkey-edited", got.Bot.Dingtalk.ClientID)
	}
	if got.Bot.Dingtalk.BotName != "EditedBot" {
		t.Fatalf("botName = %q, want EditedBot", got.Bot.Dingtalk.BotName)
	}
	if got.Bot.Dingtalk.RequireMention {
		t.Fatalf("requireMention = true, want false")
	}
	if got.Bot.Dingtalk.SecretEnv != "DINGTALK_CLIENT_SECRET" {
		t.Fatalf("secretEnv = %q, want DINGTALK_CLIENT_SECRET", got.Bot.Dingtalk.SecretEnv)
	}
	if mappings := got.Bot.Dingtalk.SessionMappings; len(mappings) != 1 || mappings[0].RemoteID != "cid-preserved" || mappings[0].SessionID != "path:/sessions/preserved.jsonl" {
		t.Fatalf("session mappings = %+v, want hidden legacy mapping preserved", mappings)
	}
}

// TestDesktopBotConfigConfiguredDetectsDingtalk 防止回归：用户只配置钉钉（其他平台
// 均未配置）时，desktopBotConfigConfigured 必须返回 true，否则 legacy 合并路径会
// 用 legacy config 整体覆盖 userCfg.Bot，导致钉钉 ClientID 等配置被丢弃
// （表现为「输入 ClientID 后消失」）。
func TestDesktopBotConfigConfiguredDetectsDingtalk(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Dingtalk.ClientID = "dingappkey123"
	if !desktopBotConfigConfigured(cfg.Bot) {
		t.Fatalf("dingtalk-only bot config must count as configured")
	}
	// 仅 SecretEnv 配了也应算已配置（环境变量形式的密钥）。
	cfg = config.Default()
	cfg.Bot.Dingtalk.SecretEnv = "DINGTALK_CLIENT_SECRET"
	if !desktopBotConfigConfigured(cfg.Bot) {
		t.Fatalf("dingtalk secret-env-only config must count as configured")
	}
}

// TestDingtalkRuntimeConnectionID 验证测试发送时解析到真实运行的 connection id
// （自定义 id 如 dingtalk-test 必须命中），而不是硬编码退回 "dingtalk"。
func TestDingtalkRuntimeConnectionID(t *testing.T) {
	// 自定义 id：优先命中启用的钉钉连接。
	got := dingtalkRuntimeConnectionID([]config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Enabled: true},
		{ID: "dingtalk-test", Provider: "dingtalk", Enabled: true},
	})
	if got != "dingtalk-test" {
		t.Fatalf("runtime connection id = %q, want dingtalk-test", got)
	}
	// 未启用：跳过。
	got = dingtalkRuntimeConnectionID([]config.BotConnectionConfig{
		{ID: "dingtalk-disabled", Provider: "dingtalk", Enabled: false},
	})
	if got != "dingtalk" {
		t.Fatalf("disabled connection must be skipped, got %q", got)
	}
	// 无 id 的钉钉连接：退回 provider 名。
	got = dingtalkRuntimeConnectionID([]config.BotConnectionConfig{
		{Provider: "dingtalk", Enabled: true},
	})
	if got != "dingtalk" {
		t.Fatalf("id-less connection should fall back to %q, got %q", "dingtalk", got)
	}
	// 无任何钉钉连接：退回 "dingtalk"（legacy [bot.dingtalk] 路径）。
	got = dingtalkRuntimeConnectionID(nil)
	if got != "dingtalk" {
		t.Fatalf("no connection should fall back to %q, got %q", "dingtalk", got)
	}
}

// TestDingtalkRuntimeConnectionResolvesCredentials 验证测试发送凭据从所选
// connection 解析（而不是 legacy [bot.dingtalk] 块）——否则 connection 驱动的
// 运行时在 legacy 块为空时误报 dingtalk_secret_missing。
func TestDingtalkRuntimeConnectionResolvesCredentials(t *testing.T) {
	conns := []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Enabled: true},
		{ID: "dingtalk-main", Provider: "dingtalk", Enabled: true,
			Credential: config.BotConnectionCredential{AppID: "app-from-conn", AppSecretEnv: "DINGTALK_CONN_SECRET"}},
	}
	conn, id, ok := dingtalkRuntimeConnection(conns)
	if !ok {
		t.Fatal("enabled dingtalk connection not found")
	}
	if id != "dingtalk-main" {
		t.Fatalf("runtime id = %q, want dingtalk-main", id)
	}
	if conn.Credential.AppID != "app-from-conn" || conn.Credential.AppSecretEnv != "DINGTALK_CONN_SECRET" {
		t.Fatalf("resolved connection credentials = %q/%q, want app-from-conn/DINGTALK_CONN_SECRET",
			conn.Credential.AppID, conn.Credential.AppSecretEnv)
	}
	// 未启用的钉钉连接不应命中。
	if _, _, ok := dingtalkRuntimeConnection([]config.BotConnectionConfig{
		{ID: "dingtalk-off", Provider: "dingtalk", Enabled: false},
	}); ok {
		t.Fatal("disabled dingtalk connection must not be selected")
	}
}

// TestSetBotSettingsDingtalkRuntimeOptionsRoundTrip 验证 legacy [bot.dingtalk]
// 的模型/工具审批模式/工作目录经 SetBotSettings 落盘并被读回。这是排查
// 「钉钉面板模型、权限切换无法保存」的回归测试：config 结构体与 settings
// view 都支持这些字段，但 render.go 漏写导致 SaveTo 丢弃（已被修复）。
func TestSetBotSettingsDingtalkRuntimeOptionsRoundTrip(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Bot.Dingtalk = config.DingtalkBotConfig{
		Enabled:          true,
		ClientID:         "ding-appkey",
		ClientSecret:     "secret",
		SecretEnv:        "DINGTALK_CLIENT_SECRET",
		BotName:          "安博特",
		RequireMention:   true,
		Model:            "deepseek-flash/deepseek-v4-flash",
		ToolApprovalMode: "yolo",
		WorkspaceRoot:    "/tmp/probe-ws",
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save initial: %v", err)
	}

	app := NewApp()
	view := botSettingsView(cfg.Bot)
	if view.Dingtalk.Model != "deepseek-flash/deepseek-v4-flash" {
		t.Fatalf("view model = %q, want deepseek-flash", view.Dingtalk.Model)
	}
	if view.Dingtalk.ToolApprovalMode != "yolo" {
		t.Fatalf("view approval = %q, want yolo", view.Dingtalk.ToolApprovalMode)
	}
	view.Dingtalk.Model = "deepseek-flash/deepseek-v4-flash"
	view.Dingtalk.ToolApprovalMode = "yolo"
	view.Dingtalk.WorkspaceRoot = "/tmp/probe-ws"
	if err := app.SetBotSettings(view); err != nil {
		t.Fatalf("SetBotSettings: %v", err)
	}

	got := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	if got.Bot.Dingtalk.Model != "deepseek-flash/deepseek-v4-flash" {
		t.Fatalf("model = %q, want deepseek-flash (render must persist)", got.Bot.Dingtalk.Model)
	}
	if got.Bot.Dingtalk.ToolApprovalMode != "yolo" {
		t.Fatalf("toolApprovalMode = %q, want yolo (render must persist)", got.Bot.Dingtalk.ToolApprovalMode)
	}
	if got.Bot.Dingtalk.WorkspaceRoot != "/tmp/probe-ws" {
		t.Fatalf("workspaceRoot = %q, want /tmp/probe-ws (render must persist)", got.Bot.Dingtalk.WorkspaceRoot)
	}
	// 原有字段不能丢。
	if got.Bot.Dingtalk.ClientSecret != "secret" {
		t.Fatalf("clientSecret lost: %q", got.Bot.Dingtalk.ClientSecret)
	}
	if got.Bot.Dingtalk.BotName != "安博特" {
		t.Fatalf("botName lost: %q", got.Bot.Dingtalk.BotName)
	}
}

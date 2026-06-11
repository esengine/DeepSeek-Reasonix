package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

func TestRememberBotRemoteStoresIncomingChatID(t *testing.T) {
	isolateBotUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Label: "飞书", Enabled: true, Status: "connected"},
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg := bot.InboundMessage{
		Platform: bot.PlatformWeixin,
		ChatType: bot.ChatDM,
		ChatID:   "wx-chat-1",
		UserID:   "wx-user-1",
	}
	if err := rememberBotInbound(msg); err != nil {
		t.Fatalf("rememberBotInbound: %v", err)
	}
	if err := rememberBotInbound(msg); err != nil {
		t.Fatalf("rememberBotRemote duplicate: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	if len(got.Bot.Connections) != 2 {
		t.Fatalf("connections = %d, want 2", len(got.Bot.Connections))
	}
	var wx config.BotConnectionConfig
	var fs config.BotConnectionConfig
	for _, conn := range got.Bot.Connections {
		switch conn.ID {
		case "weixin-weixin":
			wx = conn
		case "feishu-feishu":
			fs = conn
		}
	}
	if len(fs.SessionMappings) != 0 {
		t.Fatalf("feishu mappings = %+v, want none", fs.SessionMappings)
	}
	if len(wx.SessionMappings) != 1 {
		t.Fatalf("weixin mappings = %+v, want one", wx.SessionMappings)
	}
	if m := wx.SessionMappings[0]; m.RemoteID != "wx-chat-1" || m.Scope != "global" || m.WorkspaceRoot != "" || m.UpdatedAt == "" {
		t.Fatalf("weixin mapping = %+v, want global wx-chat-1 with timestamp", m)
	}
	if got := got.Bot.Allowlist.WeixinUsers; len(got) != 1 || got[0] != "wx-user-1" {
		t.Fatalf("weixin users = %+v, want wx-user-1", got)
	}
}

func TestRememberBotRemoteKeepsProjectScopedConnection(t *testing.T) {
	isolateBotUserConfig(t)
	workspace := filepath.Join(t.TempDir(), "project")
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID:            "feishu-project",
		Provider:      "feishu",
		Domain:        "feishu",
		Label:         "飞书",
		Enabled:       true,
		Status:        "connected",
		WorkspaceRoot: workspace,
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if err := rememberBotInbound(bot.InboundMessage{
		Platform: bot.PlatformFeishu,
		ChatType: bot.ChatDM,
		ChatID:   "oc-chat-1",
		UserID:   "ou-user-1",
	}); err != nil {
		t.Fatalf("rememberBotInbound: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	if len(got.Bot.Connections) != 1 || len(got.Bot.Connections[0].SessionMappings) != 1 {
		t.Fatalf("connections = %+v, want one project mapping", got.Bot.Connections)
	}
	if m := got.Bot.Connections[0].SessionMappings[0]; m.RemoteID != "oc-chat-1" || m.Scope != "project" || m.WorkspaceRoot != workspace {
		t.Fatalf("mapping = %+v, want project scoped remote", m)
	}
	if got := got.Bot.Allowlist.FeishuUsers; len(got) != 1 || got[0] != "ou-user-1" {
		t.Fatalf("feishu users = %+v, want ou-user-1", got)
	}
}

func TestRememberBotInboundStoresGroupAllowlist(t *testing.T) {
	isolateBotUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Label: "飞书", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg := bot.InboundMessage{
		Platform: bot.PlatformFeishu,
		ChatType: bot.ChatGroup,
		ChatID:   "oc-group-1",
		UserID:   "ou-user-1",
	}
	if err := rememberBotInbound(msg); err != nil {
		t.Fatalf("rememberBotInbound: %v", err)
	}
	if err := rememberBotInbound(msg); err != nil {
		t.Fatalf("rememberBotInbound duplicate: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	if users := got.Bot.Allowlist.FeishuUsers; len(users) != 1 || users[0] != "ou-user-1" {
		t.Fatalf("feishu users = %+v, want one ou-user-1", users)
	}
	if groups := got.Bot.Allowlist.FeishuGroups; len(groups) != 1 || groups[0] != "oc-group-1" {
		t.Fatalf("feishu groups = %+v, want one oc-group-1", groups)
	}
}

func TestBotDoctorReportsSessionMappingCounts(t *testing.T) {
	isolateBotUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Label: "飞书", Enabled: true, Status: "connected"},
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected"},
	}
	cfg.Bot.Connections[0].SessionMappings = []config.BotConnectionSessionMapping{{RemoteID: "oc-chat-1", Scope: "global"}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out := captureStdout(t, func() {
		if rc := botDoctor([]string{"--json"}); rc != 0 {
			t.Fatalf("botDoctor rc = %d, want 0", rc)
		}
	})
	for _, want := range []string{
		`"name":"bot.connections","status":"ok","detail":"enabled=2 total=2"`,
		`"name":"bot.connection.feishu-feishu.session_mappings","status":"ok","detail":"provider=feishu mappings=1"`,
		`"name":"bot.connection.weixin-weixin.session_mappings","status":"missing","detail":"provider=weixin mappings=0"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bot doctor output missing %s:\n%s", want, out)
		}
	}
}

func isolateBotUserConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Chdir(t.TempDir())
}

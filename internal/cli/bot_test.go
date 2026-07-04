package cli

import (
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
	if err := botruntime.RememberInbound(msg); err != nil {
		t.Fatalf("rememberBotInbound: %v", err)
	}
	if err := botruntime.RememberInbound(msg); err != nil {
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

	if err := botruntime.RememberInbound(bot.InboundMessage{
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
	if err := botruntime.RememberInbound(msg); err != nil {
		t.Fatalf("rememberBotInbound: %v", err)
	}
	if err := botruntime.RememberInbound(msg); err != nil {
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

func TestBotDoctorDeepReportsPairingAndRoles(t *testing.T) {
	isolateBotUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Enabled = true
	cfg.Bot.Pairing.Enabled = true
	cfg.Bot.Allowlist.Enabled = true
	cfg.Bot.Allowlist.FeishuUsers = []string{"ou-user"}
	cfg.Bot.Allowlist.FeishuApprovers = []string{"ou-approver"}
	cfg.Bot.Allowlist.FeishuAdmins = []string{"ou-admin"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if _, _, err := bot.CreateOrRefreshPairingRequest(bot.InboundMessage{
		Platform:     bot.PlatformFeishu,
		ConnectionID: "feishu-feishu",
		ChatType:     bot.ChatDM,
		ChatID:       "chat",
		UserID:       "pending-user",
	}, bot.PairingConfig{Enabled: true}); err != nil {
		t.Fatalf("create pairing: %v", err)
	}

	out := captureStdout(t, func() {
		if rc := botDoctor([]string{"--json", "--deep"}); rc != 0 {
			t.Fatalf("botDoctor rc = %d, want 0", rc)
		}
	})
	for _, want := range []string{
		`"name":"bot.pairing.pending","status":"ok","detail":"1 pending"`,
		`"name":"bot.roles","status":"ok","detail":"approvers=1 admins=1"`,
		`"name":"bot.config.user","status":"ok"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bot doctor deep output missing %s:\n%s", want, out)
		}
	}
}

func TestBotPairingApproveAddsAllowlistAndFirstAdmin(t *testing.T) {
	isolateBotUserConfig(t)
	cfg := config.Default()
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	req, _, err := bot.CreateOrRefreshPairingRequest(bot.InboundMessage{
		Platform: bot.PlatformWeixin,
		ChatType: bot.ChatDM,
		ChatID:   "wx-chat",
		UserID:   "wx-user",
	}, bot.PairingConfig{Enabled: true})
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}

	if rc := botPairing([]string{"approve", req.Code}); rc != 0 {
		t.Fatalf("botPairing approve rc = %d, want 0", rc)
	}
	got := config.LoadForEdit(config.UserConfigPath())
	if users := got.Bot.Allowlist.WeixinUsers; len(users) != 1 || users[0] != "wx-user" {
		t.Fatalf("weixin users = %+v, want wx-user", users)
	}
	if admins := got.Bot.Allowlist.WeixinAdmins; len(admins) != 1 || admins[0] != "wx-user" {
		t.Fatalf("weixin admins = %+v, want first paired admin", admins)
	}
	if approvers := got.Bot.Allowlist.WeixinApprovers; len(approvers) != 1 || approvers[0] != "wx-user" {
		t.Fatalf("weixin approvers = %+v, want first paired approver", approvers)
	}
}

func TestBotPairingApproveAddsUserToConnectionAccess(t *testing.T) {
	isolateBotUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID:       "feishu-lark",
		Provider: "feishu",
		Domain:   "lark",
		Label:    "Lark",
		Enabled:  true,
		Status:   "connected",
		Access: config.BotAccessConfig{
			Enabled:        true,
			PairingEnabled: true,
			Users:          []string{"ou-existing"},
		},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	req, _, err := bot.CreateOrRefreshPairingRequest(bot.InboundMessage{
		Platform:     bot.PlatformFeishu,
		ConnectionID: "feishu-lark",
		Domain:       "lark",
		ChatType:     bot.ChatDM,
		ChatID:       "oc-chat",
		UserID:       "ou-new",
	}, bot.PairingConfig{Enabled: true})
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}

	if rc := botPairing([]string{"approve", req.Code}); rc != 0 {
		t.Fatalf("botPairing approve rc = %d, want 0", rc)
	}
	got := config.LoadForEdit(config.UserConfigPath())
	if users := got.Bot.Allowlist.FeishuUsers; len(users) != 0 {
		t.Fatalf("global feishu users = %+v, want unchanged global allowlist", users)
	}
	if len(got.Bot.Connections) != 1 {
		t.Fatalf("connections = %+v, want one connection", got.Bot.Connections)
	}
	access := got.Bot.Connections[0].Access
	if !access.Enabled {
		t.Fatal("connection access disabled after approval, want enabled")
	}
	for _, want := range []string{"ou-existing", "ou-new"} {
		if !hasTestString(access.Users, want) {
			t.Fatalf("connection users = %+v, want %s", access.Users, want)
		}
	}
}

func TestBotDoctorPrefersUserBotSettingsOverProjectBotConfig(t *testing.T) {
	isolateBotUserConfig(t)
	userCfg := config.Default()
	userCfg.Bot.Enabled = true
	userCfg.Bot.Allowlist.Enabled = true
	userCfg.Bot.Allowlist.FeishuUsers = []string{"ou-user"}
	userCfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Label: "Lark", Enabled: true, Status: "connected"},
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[bot]
enabled = false
`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	t.Chdir(project)

	out := captureStdout(t, func() {
		if rc := botDoctor([]string{"--json"}); rc != 0 {
			t.Fatalf("botDoctor rc = %d, want 0", rc)
		}
	})
	for _, want := range []string{
		`"name":"bot.enabled","status":"ok"`,
		`"name":"bot.connections","status":"ok","detail":"enabled=1 total=1"`,
		`"name":"bot.connection.feishu-lark.session_mappings","status":"missing","detail":"provider=feishu mappings=0"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bot doctor output missing %s:\n%s", want, out)
		}
	}
}

func TestBotDoctorUsesProjectBotConfigWhenUserBotIsUnconfigured(t *testing.T) {
	isolateBotUserConfig(t)
	projectCfg := config.Default()
	projectCfg.Bot.Enabled = true
	projectCfg.Bot.Allowlist.AllowAll = true
	projectCfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected"},
	}
	if err := projectCfg.SaveTo("reasonix.toml"); err != nil {
		t.Fatalf("save project config: %v", err)
	}

	out := captureStdout(t, func() {
		if rc := botDoctor([]string{"--json"}); rc != 0 {
			t.Fatalf("botDoctor rc = %d, want 0", rc)
		}
	})
	for _, want := range []string{
		`"name":"bot.enabled","status":"ok"`,
		`"name":"bot.connections","status":"ok","detail":"enabled=1 total=1"`,
		`"name":"bot.allowlist","status":"open"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bot doctor output missing %s:\n%s", want, out)
		}
	}
}

func TestBotDoctorUsesProjectBotConfigWhenUserConfigOnlyHasBotDefaults(t *testing.T) {
	isolateBotUserConfig(t)
	userCfg := config.Default()
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}
	projectCfg := config.Default()
	projectCfg.Bot.Enabled = true
	projectCfg.Bot.Allowlist.AllowAll = true
	projectCfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Label: "Lark", Enabled: true, Status: "connected"},
	}
	if err := projectCfg.SaveTo("reasonix.toml"); err != nil {
		t.Fatalf("save project config: %v", err)
	}

	out := captureStdout(t, func() {
		if rc := botDoctor([]string{"--json"}); rc != 0 {
			t.Fatalf("botDoctor rc = %d, want 0", rc)
		}
	})
	for _, want := range []string{
		`"name":"bot.enabled","status":"ok"`,
		`"name":"bot.connections","status":"ok","detail":"enabled=1 total=1"`,
		`"name":"bot.allowlist","status":"open"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bot doctor output missing %s:\n%s", want, out)
		}
	}
}

func TestBotConnectionChannelConfigsKeepFeishuAndLarkSeparate(t *testing.T) {
	connections := []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Enabled: true, Model: "feishu-model", WorkspaceRoot: "/feishu"},
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true, Model: "lark-model", WorkspaceRoot: "/lark"},
	}
	channels := botruntime.ConnectionChannelConfigs(connections, true, true)
	if channels["feishu-feishu"].Model != "feishu-model" || channels["feishu-feishu"].WorkspaceRoot != "/feishu" {
		t.Fatalf("feishu channel = %+v, want feishu override", channels["feishu-feishu"])
	}
	if channels["feishu-lark"].Model != "lark-model" || channels["feishu-lark"].WorkspaceRoot != "/lark" {
		t.Fatalf("lark channel = %+v, want lark override", channels["feishu-lark"])
	}
}

func TestBotAdapterBindingsCreateSeparateFeishuAndLarkInstances(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Enabled: true, Credential: config.BotConnectionCredential{AppID: "cli-feishu", AppSecretEnv: "FEISHU_BOT_APP_SECRET"}},
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true, Credential: config.BotConnectionCredential{AppID: "cli-lark", AppSecretEnv: "LARK_BOT_APP_SECRET"}},
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Enabled: true, Credential: config.BotConnectionCredential{AccountID: "wx-account", TokenEnv: "WEIXIN_BOT_TOKEN"}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bindings := botruntime.AdapterBindings(cfg, map[bot.Platform]bool{bot.PlatformFeishu: true, bot.PlatformWeixin: true}, nil, logger)

	got := map[string]bot.AdapterBinding{}
	for _, binding := range bindings {
		got[binding.ID] = binding
	}
	for _, id := range []string{"feishu-feishu", "feishu-lark", "weixin-weixin"} {
		if got[id].Adapter == nil {
			t.Fatalf("binding %s missing from %+v", id, bindings)
		}
	}
	if got["feishu-feishu"].Domain != "feishu" || got["feishu-lark"].Domain != "lark" {
		t.Fatalf("domains = feishu:%q lark:%q, want separate domains", got["feishu-feishu"].Domain, got["feishu-lark"].Domain)
	}
}

func TestBotAdapterBindingsIsolateRequestedFeishuDomain(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Enabled: true, Credential: config.BotConnectionCredential{AppID: "cli-feishu", AppSecretEnv: "FEISHU_BOT_APP_SECRET"}},
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true, Credential: config.BotConnectionCredential{AppID: "cli-lark", AppSecretEnv: "LARK_BOT_APP_SECRET"}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	enabled := map[bot.Platform]bool{bot.PlatformFeishu: true}

	larkOnly := botruntime.AdapterBindings(cfg, enabled, botruntime.RequestedFeishuDomains([]string{"lark"}), logger)
	if len(larkOnly) != 1 || larkOnly[0].ID != "feishu-lark" {
		t.Fatalf("--channels lark bindings = %+v, want only feishu-lark", larkOnly)
	}

	feishuOnly := botruntime.AdapterBindings(cfg, enabled, botruntime.RequestedFeishuDomains([]string{"feishu"}), logger)
	if len(feishuOnly) != 1 || feishuOnly[0].ID != "feishu-feishu" {
		t.Fatalf("--channels feishu bindings = %+v, want only feishu-feishu", feishuOnly)
	}
}

func TestRememberBotInboundUsesConnectionID(t *testing.T) {
	isolateBotUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Label: "飞书", Enabled: true, Status: "connected"},
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Label: "Lark", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if err := botruntime.RememberInbound(bot.InboundMessage{
		Platform:     bot.PlatformFeishu,
		ConnectionID: "feishu-lark",
		Domain:       "lark",
		ChatType:     bot.ChatDM,
		ChatID:       "oc-lark-chat",
		UserID:       "ou-lark-user",
	}); err != nil {
		t.Fatalf("rememberBotInbound: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	var feishuConn, larkConn config.BotConnectionConfig
	for _, conn := range got.Bot.Connections {
		switch conn.ID {
		case "feishu-feishu":
			feishuConn = conn
		case "feishu-lark":
			larkConn = conn
		}
	}
	if len(feishuConn.SessionMappings) != 0 {
		t.Fatalf("feishu mappings = %+v, want none", feishuConn.SessionMappings)
	}
	if len(larkConn.SessionMappings) != 1 || larkConn.SessionMappings[0].RemoteID != "oc-lark-chat" {
		t.Fatalf("lark mappings = %+v, want lark chat only", larkConn.SessionMappings)
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

func hasTestString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

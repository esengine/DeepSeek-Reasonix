package botruntime

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

func TestAllowlistUserCountIncludesRoles(t *testing.T) {
	allowlist := config.BotAllowlist{
		FeishuApprovers: []string{"ou-approver"},
		FeishuAdmins:    []string{"ou-admin"},
	}

	if got := AllowlistUserCount(allowlist); got != 2 {
		t.Fatalf("AllowlistUserCount() = %d, want role users included", got)
	}
}

// TestAllowlistUserCountIncludesDingtalk: 钉钉 allowlist 字段必须计入，
// 否则仅配置钉钉白名单时启动门禁误判为无访问控制而拒绝启动。
func TestAllowlistUserCountIncludesDingtalk(t *testing.T) {
	allowlist := config.BotAllowlist{
		DingtalkUsers: []string{"ding-user"}, DingtalkApprovers: []string{"ding-approver"},
		DingtalkAdmins: []string{"ding-admin"},
	}
	if got := AllowlistUserCount(allowlist); got != 3 {
		t.Fatalf("AllowlistUserCount() = %d, want dingtalk users included", got)
	}
}

// TestBotConfigHasAccessControlDingtalk: 仅直配 [bot.dingtalk].access 时
// 必须识别为有访问控制（关闭 pairing、无全局 allowlist 的场景）。
func TestBotConfigHasAccessControlDingtalk(t *testing.T) {
	bc := config.BotConfig{
		Dingtalk: config.DingtalkBotConfig{
			Access: config.BotAccessConfig{Users: []string{"ding-user"}},
		},
	}
	if !BotConfigHasAccessControl(bc) {
		t.Fatal("BotConfigHasAccessControl() = false, want true for direct dingtalk access")
	}
}

// TestMergeLegacyDingtalkChannel: 直配 [bot.dingtalk]（无 [[bot.connections]]）
// 时，模型/权限/工作目录必须合成进 Channels 与 ConnectionChannels，否则 CLI
// bot 模式忽略这些运行选项（与桌面端 legacy 钉钉通道同路径）。
func TestMergeLegacyDingtalkChannel(t *testing.T) {
	channels, connectionChannels := MergeLegacyDingtalkChannel(config.DingtalkBotConfig{
		Model:            "deepseek/deepseek-v4-flash",
		ToolApprovalMode: "yolo",
		WorkspaceRoot:    "/tmp/work",
	}, nil, nil)

	plat := channels[bot.PlatformDingtalk]
	if plat.Model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("channel model = %q, want deepseek/deepseek-v4-flash", plat.Model)
	}
	if plat.ToolApprovalMode != "yolo" {
		t.Fatalf("channel tool_approval_mode = %q, want yolo", plat.ToolApprovalMode)
	}
	if plat.WorkspaceRoot != "/tmp/work" {
		t.Fatalf("channel workspace_root = %q, want /tmp/work", plat.WorkspaceRoot)
	}
	conn := connectionChannels[string(bot.PlatformDingtalk)]
	if conn.Model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("connection channel model = %q, want deepseek/deepseek-v4-flash", conn.Model)
	}
}

func TestMergeLegacyDingtalkChannelEmptyIsNoop(t *testing.T) {
	channels := map[bot.Platform]bot.ChannelConfig{
		bot.PlatformFeishu: {Model: "keep"},
	}
	out, conn := MergeLegacyDingtalkChannel(config.DingtalkBotConfig{}, channels, nil)
	if _, ok := out[bot.PlatformDingtalk]; ok {
		t.Fatalf("empty legacy dingtalk config should not create a dingtalk channel")
	}
	if out[bot.PlatformFeishu].Model != "keep" {
		t.Fatalf("existing channels were mutated")
	}
	if conn != nil {
		t.Fatalf("connection channels should stay nil for empty config")
	}
}

func TestRemoteRemembererKeepsDistinctGroupUsers(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Label: "Lark", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	remember := NewRemoteRememberer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	remember(bot.InboundMessage{
		Platform:     bot.PlatformFeishu,
		ConnectionID: "feishu-lark",
		Domain:       "lark",
		ChatType:     bot.ChatGroup,
		ChatID:       "oc-group-1",
		UserID:       "ou-user-1",
	})
	remember(bot.InboundMessage{
		Platform:     bot.PlatformFeishu,
		ConnectionID: "feishu-lark",
		Domain:       "lark",
		ChatType:     bot.ChatGroup,
		ChatID:       "oc-group-1",
		UserID:       "ou-user-2",
	})

	got := config.LoadForEdit(config.UserConfigPath())
	if users := got.Bot.Allowlist.FeishuUsers; len(users) != 2 || users[0] != "ou-user-1" || users[1] != "ou-user-2" {
		t.Fatalf("feishu users = %+v, want both group users", users)
	}
	if groups := got.Bot.Allowlist.FeishuGroups; len(groups) != 1 || groups[0] != "oc-group-1" {
		t.Fatalf("feishu groups = %+v, want group once", groups)
	}
	if mappings := got.Bot.Connections[0].SessionMappings; len(mappings) != 2 || mappings[0].RemoteID != "oc-group-1" || mappings[0].UserID != "ou-user-1" || mappings[1].RemoteID != "oc-group-1" || mappings[1].UserID != "ou-user-2" {
		t.Fatalf("session mappings = %+v, want distinct group-user mappings", mappings)
	}
}

func TestRememberInboundSessionFillsExistingMappingSessionID(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg := bot.InboundMessage{
		Platform:     bot.PlatformWeixin,
		ConnectionID: "weixin-weixin",
		Domain:       "weixin",
		ChatType:     bot.ChatDM,
		ChatID:       "wx-chat-1",
		UserID:       "wx-user-1",
	}
	if err := RememberInbound(msg); err != nil {
		t.Fatalf("remember inbound: %v", err)
	}
	if err := RememberInboundSession(msg, "path:/sessions/20260614-120000.000000000-deepseek.jsonl"); err != nil {
		t.Fatalf("remember inbound session: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mappings := got.Bot.Connections[0].SessionMappings
	if len(mappings) != 1 {
		t.Fatalf("mappings = %+v, want one mapping", mappings)
	}
	if mappings[0].RemoteID != "wx-chat-1" || mappings[0].SessionID != "path:/sessions/20260614-120000.000000000-deepseek.jsonl" || mappings[0].SessionSource != "auto" {
		t.Fatalf("mapping = %+v, want remote chat with session id", mappings[0])
	}
}

func TestRememberInboundSessionCreatesMappingWithSessionID(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Label: "Lark", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if err := RememberInboundSession(bot.InboundMessage{
		Platform:     bot.PlatformFeishu,
		ConnectionID: "feishu-lark",
		Domain:       "lark",
		ChatType:     bot.ChatDM,
		ChatID:       "oc-chat-1",
		UserID:       "ou-user-1",
	}, "path:/sessions/topic-bot.jsonl"); err != nil {
		t.Fatalf("remember inbound session: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mappings := got.Bot.Connections[0].SessionMappings
	if len(mappings) != 1 || mappings[0].RemoteID != "oc-chat-1" || mappings[0].SessionID != "path:/sessions/topic-bot.jsonl" || mappings[0].SessionSource != "auto" {
		t.Fatalf("mappings = %+v, want mapping with session id", mappings)
	}
}

// TestRememberInboundSessionCreatesMappingWithSessionID: legacy 直配
// [bot.dingtalk]（无 connection 记录）时，会话绑定必须持久化到
// Dingtalk.SessionMappings，重启后仍能恢复（#9116 review 阻塞项⑤）。
func TestRememberInboundSessionLegacyDingtalkMapping(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Dingtalk.Enabled = true
	cfg.Bot.Dingtalk.Model = "deepseek/deepseek-v4-flash"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if err := RememberInboundSession(bot.InboundMessage{
		Platform: bot.PlatformDingtalk,
		ChatType: bot.ChatDM,
		ChatID:   "cid-ding-1",
		UserID:   "user-1",
	}, "path:/sessions/bot-ding-rotated.jsonl"); err != nil {
		t.Fatalf("remember inbound session: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mappings := got.Bot.Dingtalk.SessionMappings
	if len(mappings) != 1 || mappings[0].RemoteID != "cid-ding-1" || mappings[0].SessionID != "path:/sessions/bot-ding-rotated.jsonl" || mappings[0].SessionSource != "auto" {
		t.Fatalf("legacy dingtalk mappings = %+v, want rotated session pinned", mappings)
	}

	// 有 connection 记录时不写 legacy（避免双写）。
	cfg2 := config.Default()
	cfg2.Bot.Dingtalk.Enabled = true
	cfg2.Bot.Connections = []config.BotConnectionConfig{
		{ID: "ding-conn", Provider: "dingtalk", Enabled: true, Status: "connected"},
	}
	if err := cfg2.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := RememberInboundSession(bot.InboundMessage{
		Platform:     bot.PlatformDingtalk,
		ConnectionID: "ding-conn",
		ChatType:     bot.ChatDM,
		ChatID:       "cid-ding-2",
		UserID:       "user-1",
	}, "path:/sessions/conn-session.jsonl"); err != nil {
		t.Fatalf("remember inbound session via connection: %v", err)
	}
	got2 := config.LoadForEdit(config.UserConfigPath())
	if len(got2.Bot.Dingtalk.SessionMappings) != 0 {
		t.Fatalf("legacy dingtalk mappings = %+v, want empty when connection exists", got2.Bot.Dingtalk.SessionMappings)
	}
	if len(got2.Bot.Connections[0].SessionMappings) != 1 {
		t.Fatalf("connection mappings = %+v, want the rotated session", got2.Bot.Connections[0].SessionMappings)
	}

	// 禁用的 connection 不接管入站，也不能阻止 legacy 映射持久化。
	cfg3 := config.Default()
	cfg3.Bot.Dingtalk.Enabled = true
	cfg3.Bot.Connections = []config.BotConnectionConfig{
		{ID: "ding-disabled", Provider: "dingtalk", Enabled: false},
	}
	if err := cfg3.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save disabled connection config: %v", err)
	}
	if err := RememberInboundSession(bot.InboundMessage{
		Platform: bot.PlatformDingtalk,
		ChatType: bot.ChatDM,
		ChatID:   "cid-ding-disabled-conn",
		UserID:   "user-1",
	}, "path:/sessions/legacy-with-disabled-conn.jsonl"); err != nil {
		t.Fatalf("remember inbound session with disabled connection: %v", err)
	}
	got3 := config.LoadForEdit(config.UserConfigPath())
	if mappings := got3.Bot.Dingtalk.SessionMappings; len(mappings) != 1 || mappings[0].RemoteID != "cid-ding-disabled-conn" {
		t.Fatalf("legacy dingtalk mappings = %+v, want mapping when only disabled connection exists", mappings)
	}
	if mappings := got3.Bot.Connections[0].SessionMappings; len(mappings) != 0 {
		t.Fatalf("disabled connection mappings = %+v, want none", mappings)
	}
}

func TestRememberInboundSessionKeepsDistinctGroupUsers(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Label: "Lark", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg1 := bot.InboundMessage{Platform: bot.PlatformFeishu, ConnectionID: "feishu-lark", Domain: "lark", ChatType: bot.ChatGroup, ChatID: "oc-group-1", UserID: "ou-user-1"}
	msg2 := bot.InboundMessage{Platform: bot.PlatformFeishu, ConnectionID: "feishu-lark", Domain: "lark", ChatType: bot.ChatGroup, ChatID: "oc-group-1", UserID: "ou-user-2"}
	if err := RememberInboundSession(msg1, "path:/sessions/user-1.jsonl"); err != nil {
		t.Fatalf("remember user 1: %v", err)
	}
	if err := RememberInboundSession(msg2, "path:/sessions/user-2.jsonl"); err != nil {
		t.Fatalf("remember user 2: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mappings := got.Bot.Connections[0].SessionMappings
	if len(mappings) != 2 {
		t.Fatalf("mappings = %+v, want two group-user mappings", mappings)
	}
	if mappings[0].UserID != "ou-user-1" || mappings[0].SessionID != "path:/sessions/user-1.jsonl" || mappings[1].UserID != "ou-user-2" || mappings[1].SessionID != "path:/sessions/user-2.jsonl" {
		t.Fatalf("mappings = %+v, want user-specific session ids", mappings)
	}
}

func TestRememberInboundSessionSharesThreadMappingAcrossUsers(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Label: "Lark", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg1 := bot.InboundMessage{Platform: bot.PlatformFeishu, ConnectionID: "feishu-lark", Domain: "lark", ChatType: bot.ChatThread, ChatID: "oc-group-1", ThreadID: "thread-1", UserID: "ou-user-1"}
	msg2 := bot.InboundMessage{Platform: bot.PlatformFeishu, ConnectionID: "feishu-lark", Domain: "lark", ChatType: bot.ChatThread, ChatID: "oc-group-1", ThreadID: "thread-1", UserID: "ou-user-2"}
	if err := RememberInboundSession(msg1, "path:/sessions/thread-old.jsonl"); err != nil {
		t.Fatalf("remember user 1: %v", err)
	}
	if err := RememberInboundSession(msg2, "path:/sessions/thread-new.jsonl"); err != nil {
		t.Fatalf("remember user 2: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mappings := got.Bot.Connections[0].SessionMappings
	if len(mappings) != 1 {
		t.Fatalf("mappings = %+v, want one shared thread mapping", mappings)
	}
	if mappings[0].ChatType != string(bot.ChatThread) || mappings[0].ThreadID != "thread-1" || mappings[0].UserID != "" || mappings[0].SessionID != "path:/sessions/thread-new.jsonl" {
		t.Fatalf("mapping = %+v, want shared thread identity with latest auto session", mappings[0])
	}
}

func TestRememberInboundSessionPreservesExplicitMappingTarget(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected",
		SessionMappings: []config.BotConnectionSessionMapping{{RemoteID: "wx-chat-1", SessionID: "topic:manual-topic"}},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg := bot.InboundMessage{Platform: bot.PlatformWeixin, ConnectionID: "weixin-weixin", Domain: "weixin", ChatType: bot.ChatDM, ChatID: "wx-chat-1", UserID: "wx-user-1"}
	if err := RememberInboundSession(msg, "path:/sessions/auto.jsonl"); err != nil {
		t.Fatalf("remember inbound session: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mapping := got.Bot.Connections[0].SessionMappings[0]
	if mapping.SessionID != "topic:manual-topic" || mapping.SessionSource != "" {
		t.Fatalf("mapping = %+v, want explicit topic preserved", mapping)
	}
}

func TestRememberInboundSessionPreservesBareExplicitMappingTarget(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected",
		SessionMappings: []config.BotConnectionSessionMapping{{RemoteID: "wx-chat-1", SessionID: "manual-topic"}},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg := bot.InboundMessage{Platform: bot.PlatformWeixin, ConnectionID: "weixin-weixin", Domain: "weixin", ChatType: bot.ChatDM, ChatID: "wx-chat-1", UserID: "wx-user-1"}
	if err := RememberInboundSession(msg, "path:/sessions/auto.jsonl"); err != nil {
		t.Fatalf("remember inbound session: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mapping := got.Bot.Connections[0].SessionMappings[0]
	if mapping.SessionID != "manual-topic" || mapping.SessionSource != "" {
		t.Fatalf("mapping = %+v, want bare explicit target preserved", mapping)
	}
}

func TestRememberInboundSessionUsesActualWorkspaceWhenConnectionIsGlobal(t *testing.T) {
	isolateUserConfig(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg := bot.InboundMessage{Platform: bot.PlatformWeixin, ConnectionID: "weixin-weixin", Domain: "weixin", ChatType: bot.ChatDM, ChatID: "wx-chat-1", UserID: "wx-user-1"}
	if err := RememberInboundSessionWorkspace(msg, "path:/sessions/auto.jsonl", workspace); err != nil {
		t.Fatalf("remember inbound session: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mapping := got.Bot.Connections[0].SessionMappings[0]
	if mapping.Scope != "project" || mapping.WorkspaceRoot != workspace {
		t.Fatalf("mapping = %+v, want actual workspace scope", mapping)
	}
}

func TestRememberInboundSessionKeepsConfiguredWorkspaceOverActualWorkspace(t *testing.T) {
	isolateUserConfig(t)
	configured := filepath.Join(t.TempDir(), "configured")
	actual := filepath.Join(t.TempDir(), "actual")
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected", WorkspaceRoot: configured,
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg := bot.InboundMessage{Platform: bot.PlatformWeixin, ConnectionID: "weixin-weixin", Domain: "weixin", ChatType: bot.ChatDM, ChatID: "wx-chat-1", UserID: "wx-user-1"}
	if err := RememberInboundSessionWorkspace(msg, "path:/sessions/auto.jsonl", actual); err != nil {
		t.Fatalf("remember inbound session: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mapping := got.Bot.Connections[0].SessionMappings[0]
	if mapping.Scope != "project" || mapping.WorkspaceRoot != configured {
		t.Fatalf("mapping = %+v, want configured workspace scope", mapping)
	}
}

func TestRememberInboundSessionUpdatesAutoMappingTarget(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected",
		SessionMappings: []config.BotConnectionSessionMapping{{RemoteID: "wx-chat-1", SessionID: "path:/sessions/old.jsonl", SessionSource: "auto"}},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	msg := bot.InboundMessage{Platform: bot.PlatformWeixin, ConnectionID: "weixin-weixin", Domain: "weixin", ChatType: bot.ChatDM, ChatID: "wx-chat-1", UserID: "wx-user-1"}
	if err := RememberInboundSession(msg, "path:/sessions/new.jsonl"); err != nil {
		t.Fatalf("remember inbound session: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mapping := got.Bot.Connections[0].SessionMappings[0]
	if mapping.SessionID != "path:/sessions/new.jsonl" || mapping.SessionSource != "auto" {
		t.Fatalf("mapping = %+v, want auto target updated", mapping)
	}
}

func TestForgetAutoSessionMappingsForPathRemovesOnlyAutoPathTargets(t *testing.T) {
	isolateUserConfig(t)
	target := filepath.Join(t.TempDir(), "bot-channel.jsonl")
	other := filepath.Join(t.TempDir(), "other-channel.jsonl")
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected",
		SessionMappings: []config.BotConnectionSessionMapping{
			{RemoteID: "remove-path-prefix", SessionID: "path:" + target, SessionSource: "auto"},
			{RemoteID: "remove-raw-path", SessionID: target, SessionSource: "auto"},
			{RemoteID: "keep-explicit-path", SessionID: "path:" + target},
			{RemoteID: "keep-other-auto", SessionID: "path:" + other, SessionSource: "auto"},
			{RemoteID: "keep-topic-auto", SessionID: "topic:bot-topic", SessionSource: "auto"},
		},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if err := ForgetAutoSessionMappingsForPath(target); err != nil {
		t.Fatalf("forget auto session mappings: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mappings := got.Bot.Connections[0].SessionMappings
	if len(mappings) != 3 {
		t.Fatalf("mappings = %+v, want three preserved mappings", mappings)
	}
	remotes := map[string]bool{}
	for _, mapping := range mappings {
		remotes[mapping.RemoteID] = true
	}
	for _, remote := range []string{"keep-explicit-path", "keep-other-auto", "keep-topic-auto"} {
		if !remotes[remote] {
			t.Fatalf("mapping %q was not preserved: %+v", remote, mappings)
		}
	}
	if got.Bot.Connections[0].UpdatedAt == "" {
		t.Fatalf("connection UpdatedAt was not refreshed")
	}
}

func TestConnectionChannelConfigsPreserveToolApprovalMode(t *testing.T) {
	connections := []config.BotConnectionConfig{
		{ID: "feishu-feishu", Provider: "feishu", Domain: "feishu", Enabled: true, ToolApprovalMode: "auto"},
		{ID: "feishu-lark", Provider: "feishu", Domain: "lark", Enabled: true, ToolApprovalMode: "yolo"},
		{ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Enabled: true, ToolApprovalMode: "ask"},
	}

	byConnection := ConnectionChannelConfigs(connections, true, true)
	if got := byConnection["feishu-feishu"].ToolApprovalMode; got != "auto" {
		t.Fatalf("feishu tool approval mode = %q, want auto", got)
	}
	if got := byConnection["feishu-lark"].ToolApprovalMode; got != "yolo" {
		t.Fatalf("lark tool approval mode = %q, want yolo", got)
	}
	if got := byConnection["weixin-weixin"].ToolApprovalMode; got != "ask" {
		t.Fatalf("weixin tool approval mode = %q, want explicit ask override", got)
	}

	byPlatform := ChannelConfigs(connections, true, true)
	if got := byPlatform[bot.PlatformFeishu].ToolApprovalMode; got != "yolo" {
		t.Fatalf("platform feishu tool approval mode = %q, want last enabled Feishu/Lark override", got)
	}
}

func TestConnectionChannelConfigsCarrySessionMappingsOnlyPerConnection(t *testing.T) {
	connections := []config.BotConnectionConfig{
		{
			ID:            "weixin-weixin",
			Provider:      "weixin",
			Domain:        "weixin",
			Enabled:       true,
			WorkspaceRoot: "/connection",
			SessionMappings: []config.BotConnectionSessionMapping{{
				RemoteID:      "wx-group-1",
				SessionID:     "path:/tmp/reasonix-session.jsonl",
				ChatType:      string(bot.ChatGroup),
				UserID:        "wx-user-1",
				Scope:         "project",
				WorkspaceRoot: "/mapped",
				UpdatedAt:     "2026-07-04T12:00:00Z",
			}},
		},
	}

	byConnection := ConnectionChannelConfigs(connections, true, true)
	mappings := byConnection["weixin-weixin"].SessionMappings
	if len(mappings) != 1 {
		t.Fatalf("connection mappings = %+v, want one mapping", mappings)
	}
	if got := mappings[0]; got.RemoteID != "wx-group-1" || got.SessionID != "path:/tmp/reasonix-session.jsonl" || got.ChatType != string(bot.ChatGroup) || got.UserID != "wx-user-1" || got.WorkspaceRoot != "/mapped" || got.UpdatedAt == "" {
		t.Fatalf("connection mapping = %+v, want copied routing fields", got)
	}

	byPlatform := ChannelConfigs(connections, true, true)
	if got := byPlatform[bot.PlatformWeixin].SessionMappings; len(got) != 0 {
		t.Fatalf("platform mappings = %+v, want none to avoid cross-connection routing", got)
	}

	noWorkspace := ConnectionChannelConfigs(connections, true, false)
	if got := noWorkspace["weixin-weixin"].SessionMappings; len(got) != 0 {
		t.Fatalf("connection mappings with includeWorkspaceRoot=false = %+v, want none", got)
	}
}

func TestRouteConfigsPreserveRemoteOverrides(t *testing.T) {
	routes := RouteConfigs([]config.BotRouteConfig{
		{ConnectionID: "feishu-lark", Platform: "feishu", ChatType: "group", ChatID: "group-1", Model: "route-model", WorkspaceRoot: "/route", ToolApprovalMode: "full-access"},
		{ConnectionID: "empty-route"},
	}, true, true)
	if len(routes) != 1 {
		t.Fatalf("routes = %+v, want one non-empty route", routes)
	}
	got := routes[0]
	if got.ConnectionID != "feishu-lark" || got.Platform != bot.PlatformFeishu || got.ChatType != bot.ChatGroup || got.ChatID != "group-1" {
		t.Fatalf("route match fields = %+v, want trimmed remote match", got)
	}
	if got.Channel.Model != "route-model" || got.Channel.WorkspaceRoot != "/route" || got.Channel.ToolApprovalMode != "yolo" {
		t.Fatalf("route channel = %+v, want normalized overrides", got.Channel)
	}
}

func isolateUserConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Chdir(t.TempDir())
}

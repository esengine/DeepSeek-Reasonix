package botruntime

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/bot/feishu"
	"reasonix/internal/bot/qq"
	"reasonix/internal/bot/weixin"
	"reasonix/internal/config"
)

var rememberPersistMu sync.Mutex

// EnabledPlatforms resolves the requested channel list against the saved config.
// "lark" is a domain alias for the Feishu adapter platform.
func EnabledPlatforms(cfg *config.Config, channels []string) (map[bot.Platform]bool, []string) {
	enabled := make(map[bot.Platform]bool)
	var warnings []string
	if len(channels) > 0 {
		for _, ch := range channels {
			ch = strings.TrimSpace(ch)
			switch bot.Platform(ch) {
			case bot.PlatformQQ:
				enabled[bot.PlatformQQ] = PlatformConfigured(cfg, bot.PlatformQQ)
			case bot.PlatformFeishu:
				enabled[bot.PlatformFeishu] = PlatformConfigured(cfg, bot.PlatformFeishu)
			case bot.PlatformWeixin:
				enabled[bot.PlatformWeixin] = PlatformConfigured(cfg, bot.PlatformWeixin)
			default:
				if strings.EqualFold(ch, "lark") {
					enabled[bot.PlatformFeishu] = PlatformConfigured(cfg, bot.PlatformFeishu)
				} else if ch != "" {
					warnings = append(warnings, ch)
				}
			}
		}
		return enabled, warnings
	}
	enabled[bot.PlatformQQ] = PlatformConfigured(cfg, bot.PlatformQQ)
	enabled[bot.PlatformFeishu] = PlatformConfigured(cfg, bot.PlatformFeishu)
	enabled[bot.PlatformWeixin] = PlatformConfigured(cfg, bot.PlatformWeixin)
	return enabled, warnings
}

// RequestedFeishuDomains returns the Feishu-family domains the caller explicitly
// named ("feishu"/"lark"), or nil when neither was requested (no restriction).
func RequestedFeishuDomains(channels []string) map[string]bool {
	domains := make(map[string]bool)
	for _, ch := range channels {
		switch {
		case strings.EqualFold(strings.TrimSpace(ch), string(bot.PlatformFeishu)):
			domains["feishu"] = true
		case strings.EqualFold(strings.TrimSpace(ch), "lark"):
			domains["lark"] = true
		}
	}
	if len(domains) == 0 {
		return nil
	}
	return domains
}

func feishuDomainKey(domain string) string {
	if strings.EqualFold(strings.TrimSpace(domain), "lark") {
		return "lark"
	}
	return "feishu"
}

func HasEnabledPlatform(enabled map[bot.Platform]bool) bool {
	for _, value := range enabled {
		if value {
			return true
		}
	}
	return false
}

func PlatformConfigured(cfg *config.Config, platform bot.Platform) bool {
	if cfg == nil {
		return false
	}
	switch platform {
	case bot.PlatformQQ:
		if cfg.Bot.QQ.Enabled {
			return true
		}
	case bot.PlatformFeishu:
		if cfg.Bot.Feishu.Enabled {
			return true
		}
	case bot.PlatformWeixin:
		if cfg.Bot.Weixin.Enabled {
			return true
		}
	}
	for _, conn := range cfg.Bot.Connections {
		if conn.Enabled && bot.Platform(strings.TrimSpace(conn.Provider)) == platform {
			return true
		}
	}
	return false
}

func ChannelConfigs(connections []config.BotConnectionConfig, includeModel bool, includeWorkspaceRoot bool) map[bot.Platform]bot.ChannelConfig {
	if len(connections) == 0 {
		return nil
	}
	out := make(map[bot.Platform]bot.ChannelConfig)
	for _, conn := range connections {
		if !conn.Enabled {
			continue
		}
		plat := bot.Platform(strings.TrimSpace(conn.Provider))
		switch plat {
		case bot.PlatformQQ, bot.PlatformFeishu, bot.PlatformWeixin:
		default:
			continue
		}
		channel := out[plat]
		if includeModel {
			channel.Model = strings.TrimSpace(conn.Model)
		}
		if includeWorkspaceRoot {
			channel.WorkspaceRoot = strings.TrimSpace(conn.WorkspaceRoot)
		}
		if value := normalizeToolApprovalMode(conn.ToolApprovalMode); value != "" {
			channel.ToolApprovalMode = value
		}
		if channel.Model != "" || channel.WorkspaceRoot != "" || channel.ToolApprovalMode != "" {
			out[plat] = channel
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ConnectionChannelConfigs(connections []config.BotConnectionConfig, includeModel bool, includeWorkspaceRoot bool) map[string]bot.ChannelConfig {
	if len(connections) == 0 {
		return nil
	}
	out := make(map[string]bot.ChannelConfig)
	for _, conn := range connections {
		if !conn.Enabled {
			continue
		}
		id := ConnectionRuntimeID(conn)
		if id == "" {
			continue
		}
		var channel bot.ChannelConfig
		if includeModel {
			channel.Model = strings.TrimSpace(conn.Model)
		}
		if includeWorkspaceRoot {
			channel.WorkspaceRoot = strings.TrimSpace(conn.WorkspaceRoot)
		}
		if value := normalizeToolApprovalMode(conn.ToolApprovalMode); value != "" {
			channel.ToolApprovalMode = value
		}
		if channel.Model != "" || channel.WorkspaceRoot != "" || channel.ToolApprovalMode != "" {
			out[id] = channel
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeToolApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ask":
		return "ask"
	case "auto":
		return "auto"
	case "yolo", "full", "full-access", "bypass":
		return "yolo"
	default:
		return ""
	}
}

func AdapterBindings(cfg *config.Config, enabled map[bot.Platform]bool, feishuDomains map[string]bool, logger *slog.Logger) []bot.AdapterBinding {
	if cfg == nil {
		return nil
	}
	var bindings []bot.AdapterBinding
	hasConnection := make(map[bot.Platform]bool)
	for _, conn := range cfg.Bot.Connections {
		if !conn.Enabled {
			continue
		}
		platform := bot.Platform(strings.TrimSpace(conn.Provider))
		if !enabled[platform] {
			continue
		}
		id := ConnectionRuntimeID(conn)
		switch platform {
		case bot.PlatformQQ:
			qqCfg := cfg.Bot.QQ
			qqCfg.Enabled = true
			qqCfg.AppID = firstNonEmptyString(strings.TrimSpace(conn.Credential.AppID), qqCfg.AppID)
			qqCfg.AppSecretEnv = firstNonEmptyString(strings.TrimSpace(conn.Credential.AppSecretEnv), qqCfg.AppSecretEnv)
			bindings = append(bindings, bot.AdapterBinding{ID: id, Domain: strings.TrimSpace(conn.Domain), Platform: platform, Adapter: qq.New(qqCfg, logger)})
			hasConnection[platform] = true
		case bot.PlatformFeishu:
			feishuCfg := cfg.Bot.Feishu
			feishuCfg.Enabled = true
			feishuCfg.Domain = firstNonEmptyString(strings.TrimSpace(conn.Domain), feishuCfg.Domain)
			if feishuDomains != nil && !feishuDomains[feishuDomainKey(feishuCfg.Domain)] {
				continue
			}
			feishuCfg.AppID = firstNonEmptyString(strings.TrimSpace(conn.Credential.AppID), feishuCfg.AppID)
			feishuCfg.AppSecretEnv = firstNonEmptyString(strings.TrimSpace(conn.Credential.AppSecretEnv), feishuCfg.AppSecretEnv)
			bindings = append(bindings, bot.AdapterBinding{ID: id, Domain: feishuCfg.Domain, Platform: platform, Adapter: feishu.New(feishuCfg, logger)})
			hasConnection[platform] = true
		case bot.PlatformWeixin:
			weixinCfg := cfg.Bot.Weixin
			weixinCfg.Enabled = true
			weixinCfg.AccountID = firstNonEmptyString(strings.TrimSpace(conn.Credential.AccountID), weixinCfg.AccountID)
			weixinCfg.TokenEnv = firstNonEmptyString(strings.TrimSpace(conn.Credential.TokenEnv), weixinCfg.TokenEnv)
			bindings = append(bindings, bot.AdapterBinding{ID: id, Domain: strings.TrimSpace(conn.Domain), Platform: platform, Adapter: weixin.New(weixinCfg, logger)})
			hasConnection[platform] = true
		}
	}
	if enabled[bot.PlatformQQ] && !hasConnection[bot.PlatformQQ] {
		bindings = append(bindings, bot.AdapterBinding{ID: string(bot.PlatformQQ), Platform: bot.PlatformQQ, Adapter: qq.New(cfg.Bot.QQ, logger)})
	}
	if enabled[bot.PlatformFeishu] && !hasConnection[bot.PlatformFeishu] {
		if feishuDomains == nil || feishuDomains[feishuDomainKey(cfg.Bot.Feishu.Domain)] {
			bindings = append(bindings, bot.AdapterBinding{ID: string(bot.PlatformFeishu), Domain: cfg.Bot.Feishu.Domain, Platform: bot.PlatformFeishu, Adapter: feishu.New(cfg.Bot.Feishu, logger)})
		}
	}
	if enabled[bot.PlatformWeixin] && !hasConnection[bot.PlatformWeixin] {
		bindings = append(bindings, bot.AdapterBinding{ID: string(bot.PlatformWeixin), Domain: "weixin", Platform: bot.PlatformWeixin, Adapter: weixin.New(cfg.Bot.Weixin, logger)})
	}
	return bindings
}

func ConnectionRuntimeID(conn config.BotConnectionConfig) string {
	if id := strings.TrimSpace(conn.ID); id != "" {
		return id
	}
	provider := strings.TrimSpace(conn.Provider)
	domain := strings.TrimSpace(conn.Domain)
	if provider == "" {
		return ""
	}
	if domain == "" {
		return provider
	}
	return provider + "-" + domain
}

func ModelName(cfg *config.Config, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if cfg == nil {
		return ""
	}
	if strings.TrimSpace(cfg.Bot.Model) != "" {
		return strings.TrimSpace(cfg.Bot.Model)
	}
	return strings.TrimSpace(cfg.DefaultModel)
}

func AllowlistUserCount(a config.BotAllowlist) int {
	return len(a.QQUsers) + len(a.FeishuUsers) + len(a.WeixinUsers)
}

func NewRemoteRememberer(logger *slog.Logger) func(bot.InboundMessage) {
	var mu sync.Mutex
	seen := make(map[string]bool)
	return func(msg bot.InboundMessage) {
		remoteID := strings.TrimSpace(msg.ChatID)
		if remoteID == "" {
			return
		}
		key := strings.Join([]string{
			string(msg.Platform),
			strings.TrimSpace(msg.ConnectionID),
			strings.TrimSpace(msg.Domain),
			string(msg.ChatType),
			remoteID,
			strings.TrimSpace(msg.UserID),
		}, "\x00")
		mu.Lock()
		if seen[key] {
			mu.Unlock()
			return
		}
		seen[key] = true
		mu.Unlock()

		if err := RememberInbound(msg); err != nil && logger != nil {
			logger.Warn("remember bot remote failed", "platform", msg.Platform, "err", err)
		}
	}
}

func RememberInbound(msg bot.InboundMessage) error {
	userPath := config.UserConfigPath()
	platform := msg.Platform
	remoteID := strings.TrimSpace(msg.ChatID)
	if userPath == "" || remoteID == "" {
		return nil
	}
	rememberPersistMu.Lock()
	defer rememberPersistMu.Unlock()

	cfg := config.LoadForEdit(userPath)
	now := time.Now().UTC().Format(time.RFC3339)
	changed := false
	for i := range cfg.Bot.Connections {
		conn := &cfg.Bot.Connections[i]
		if strings.TrimSpace(conn.Provider) != string(platform) || !conn.Enabled || !connectionMatchesInbound(*conn, msg) {
			continue
		}
		exists := false
		for _, mapping := range conn.SessionMappings {
			if strings.TrimSpace(mapping.RemoteID) == remoteID {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		scope := "global"
		workspaceRoot := ""
		if strings.TrimSpace(conn.WorkspaceRoot) != "" {
			scope = "project"
			workspaceRoot = strings.TrimSpace(conn.WorkspaceRoot)
		}
		conn.SessionMappings = append(conn.SessionMappings, config.BotConnectionSessionMapping{
			RemoteID:      remoteID,
			SessionID:     "",
			Scope:         scope,
			WorkspaceRoot: workspaceRoot,
			UpdatedAt:     now,
		})
		conn.UpdatedAt = now
		changed = true
	}
	if rememberAllowlist(&cfg.Bot.Allowlist, platform, msg.UserID, remoteID, msg.ChatType) {
		changed = true
	}
	if !changed {
		return nil
	}
	return cfg.SaveTo(userPath)
}

func connectionMatchesInbound(conn config.BotConnectionConfig, msg bot.InboundMessage) bool {
	if msg.ConnectionID != "" {
		return ConnectionRuntimeID(conn) == strings.TrimSpace(msg.ConnectionID)
	}
	if msg.Domain != "" && strings.TrimSpace(conn.Domain) != "" {
		return strings.EqualFold(strings.TrimSpace(conn.Domain), strings.TrimSpace(msg.Domain))
	}
	return true
}

func rememberAllowlist(allowlist *config.BotAllowlist, platform bot.Platform, userID string, chatID string, chatType bot.ChatType) bool {
	if allowlist == nil {
		return false
	}
	changed := false
	userID = strings.TrimSpace(userID)
	if userID != "" {
		switch platform {
		case bot.PlatformQQ:
			allowlist.QQUsers, changed = appendUniqueString(allowlist.QQUsers, userID)
		case bot.PlatformFeishu:
			allowlist.FeishuUsers, changed = appendUniqueString(allowlist.FeishuUsers, userID)
		case bot.PlatformWeixin:
			allowlist.WeixinUsers, changed = appendUniqueString(allowlist.WeixinUsers, userID)
		}
	}
	if !chatUsesGroupAllowlist(chatType) {
		return changed
	}
	groupID := strings.TrimSpace(chatID)
	if groupID == "" {
		return changed
	}
	groupChanged := false
	switch platform {
	case bot.PlatformQQ:
		allowlist.QQGroups, groupChanged = appendUniqueString(allowlist.QQGroups, groupID)
	case bot.PlatformFeishu:
		allowlist.FeishuGroups, groupChanged = appendUniqueString(allowlist.FeishuGroups, groupID)
	case bot.PlatformWeixin:
		allowlist.WeixinGroups, groupChanged = appendUniqueString(allowlist.WeixinGroups, groupID)
	}
	return changed || groupChanged
}

func appendUniqueString(values []string, next string) ([]string, bool) {
	next = strings.TrimSpace(next)
	if next == "" {
		return values, false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == next {
			return values, false
		}
	}
	return append(values, next), true
}

func chatUsesGroupAllowlist(chatType bot.ChatType) bool {
	switch chatType {
	case bot.ChatGroup, bot.ChatGuild, bot.ChatThread:
		return true
	default:
		return false
	}
}

func firstNonEmptyString(vals ...string) string {
	for _, val := range vals {
		if strings.TrimSpace(val) != "" {
			return val
		}
	}
	return ""
}

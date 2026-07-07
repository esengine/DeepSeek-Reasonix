package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/bot/feishu"
	"reasonix/internal/bot/weixin"
	"reasonix/internal/botruntime"
	"reasonix/internal/config"
)

type BotConnectionCredentialView struct {
	AppID        string `json:"appId"`
	AppSecretEnv string `json:"appSecretEnv"`
	AccountID    string `json:"accountId"`
	TokenEnv     string `json:"tokenEnv"`
	SecretSet    bool   `json:"secretSet"`
}

type BotConnectionSessionMappingView struct {
	RemoteID      string `json:"remoteId"`
	SessionID     string `json:"sessionId"`
	SessionSource string `json:"sessionSource"`
	ChatType      string `json:"chatType"`
	UserID        string `json:"userId"`
	ThreadID      string `json:"threadId"`
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot"`
	UpdatedAt     string `json:"updatedAt"`
}

type BotConnectionView struct {
	ID               string                            `json:"id"`
	Provider         string                            `json:"provider"`
	Domain           string                            `json:"domain"`
	Label            string                            `json:"label"`
	Enabled          bool                              `json:"enabled"`
	Status           string                            `json:"status"`
	Model            string                            `json:"model"`
	ToolApprovalMode string                            `json:"toolApprovalMode"`
	WorkspaceRoot    string                            `json:"workspaceRoot"`
	Access           BotAccessView                     `json:"access"`
	Credential       BotConnectionCredentialView       `json:"credential"`
	SessionMappings  []BotConnectionSessionMappingView `json:"sessionMappings"`
	LastError        string                            `json:"lastError"`
	CreatedAt        string                            `json:"createdAt"`
	UpdatedAt        string                            `json:"updatedAt"`
}

type BotInstallStartResult struct {
	OK         bool   `json:"ok"`
	Provider   string `json:"provider"`
	Domain     string `json:"domain"`
	InstallID  string `json:"installId"`
	URL        string `json:"url"`
	DeviceCode string `json:"deviceCode"`
	UserCode   string `json:"userCode"`
	Interval   int    `json:"interval"`
	ExpireIn   int    `json:"expireIn"`
	Message    string `json:"message"`
}

type BotInstallPollResult struct {
	Done       bool              `json:"done"`
	Connection BotConnectionView `json:"connection"`
	Status     string            `json:"status"`
	Message    string            `json:"message"`
	Error      string            `json:"error"`
}

type BotConnectionDiagnostic struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	MessageID    string `json:"messageId"`
	Phase        string `json:"phase"`
	Code         string `json:"code"`
	ReportKind   string `json:"reportKind"`
	ReportDetail string `json:"reportDetail"`
	OccurredAt   string `json:"occurredAt"`
}

type botInstallSession struct {
	Provider   string
	Domain     string
	PollDomain string
	DeviceCode string
	UserCode   string
	StartedAt  time.Time
	ExpireAt   time.Time
	Weixin     *weixin.LoginSession
}

func (a *App) StartBotConnectionInstall(provider, domain string) (BotInstallStartResult, error) {
	provider, domain = normalizeBotInstallTarget(provider, domain)
	if provider == "weixin" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		session, err := weixin.StartLogin(ctx)
		if err != nil {
			return BotInstallStartResult{OK: false, Provider: provider, Domain: domain, Message: err.Error()}, nil
		}
		installID := randomInstallID()
		a.mu.Lock()
		if a.botInstalls == nil {
			a.botInstalls = map[string]*botInstallSession{}
		}
		a.botInstalls[installID] = &botInstallSession{
			Provider:   provider,
			Domain:     domain,
			DeviceCode: session.QRCode,
			StartedAt:  session.StartedAt,
			ExpireAt:   time.Now().Add(2 * time.Minute),
			Weixin:     session,
		}
		a.mu.Unlock()
		return BotInstallStartResult{
			OK: true, Provider: provider, Domain: domain, InstallID: installID, URL: firstNonEmptyBot(session.QRCodeURL, session.QRCode),
			DeviceCode: session.QRCode, Interval: 3, ExpireIn: 120, Message: "请使用微信扫码完成连接。",
		}, nil
	}
	if provider != "feishu" {
		return BotInstallStartResult{OK: false, Provider: provider, Domain: domain, Message: "unsupported bot provider"}, nil
	}
	return a.startFeishuConnectionInstall(domain)
}

func (a *App) PollBotConnectionInstall(installID string) (BotInstallPollResult, error) {
	installID = strings.TrimSpace(installID)
	// Copy the session under a.mu: overlapping polls of the same install can
	// race the locked PollDomain upgrade below with unlocked field reads.
	a.mu.RLock()
	sessionPtr := a.botInstalls[installID]
	var sessionCopy botInstallSession
	if sessionPtr != nil {
		sessionCopy = *sessionPtr
	}
	a.mu.RUnlock()
	if sessionPtr == nil {
		return BotInstallPollResult{Error: "install session not found"}, nil
	}
	session := &sessionCopy
	if time.Now().After(session.ExpireAt) {
		a.deleteBotInstall(installID)
		return BotInstallPollResult{Status: "expired", Error: "install session expired"}, nil
	}
	if session.Provider == "weixin" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, status, err := weixin.PollLogin(ctx, session.Weixin)
		if err != nil {
			return BotInstallPollResult{Status: status, Error: err.Error()}, nil
		}
		if result == nil {
			return BotInstallPollResult{Status: status, Message: weixinInstallStatusMessage(status)}, nil
		}
		a.deleteBotInstall(installID)
		conn, err := a.upsertBotConnection(config.BotConnectionConfig{
			ID:         connectionID("weixin", "weixin"),
			Provider:   "weixin",
			Domain:     "weixin",
			Label:      "微信",
			Enabled:    true,
			Status:     "connected",
			Access:     botInstallAccess(result.UserID),
			Credential: config.BotConnectionCredential{AccountID: result.AccountID, TokenEnv: "WEIXIN_BOT_TOKEN"},
		}, func(c *config.Config) {
			c.Bot.Enabled = true
			c.Bot.Weixin.Enabled = true
			c.Bot.Weixin.AccountID = result.AccountID
			c.Bot.Weixin.APIBase = result.BaseURL
			if c.Bot.Weixin.TokenEnv == "" {
				c.Bot.Weixin.TokenEnv = "WEIXIN_BOT_TOKEN"
			}
			c.Bot.Allowlist.WeixinUsers = appendUniqueBotString(c.Bot.Allowlist.WeixinUsers, result.UserID)
		})
		if err != nil {
			return BotInstallPollResult{Status: "error", Error: err.Error()}, nil
		}
		a.refreshBotRuntimeAsync()
		return BotInstallPollResult{Done: true, Status: "connected", Connection: conn, Message: "微信已连接。"}, nil
	}
	return a.pollFeishuConnectionInstall(installID, session)
}

func (a *App) DiagnoseBotConnection(id string) (BotConnectionDiagnostic, error) {
	cfg, err := a.loadDesktopBotConfig()
	if err != nil {
		return botConnectionDiagnostic(nil, id, "error", "config", "config_load_failed", err.Error(), true), nil
	}
	for _, conn := range cfg.Bot.Connections {
		if conn.ID == id {
			status := "ok"
			message := "连接配置已保存。"
			phase := "config"
			code := "config_ok"
			reportable := false
			if !conn.Enabled {
				status = "disabled"
				message = "连接已保存但未启用。"
				code = "connection_disabled"
			} else if conn.Status != "connected" {
				status = firstNonEmptyBot(conn.Status, "pending")
				message = firstNonEmptyBot(conn.LastError, "连接还未完成。")
				phase = "install"
				code = "connection_not_connected"
				reportable = status == "error" || strings.TrimSpace(conn.LastError) != ""
			} else if conn.Credential.AppSecretEnv != "" && strings.TrimSpace(conn.Credential.AppSecretEnv) != "" && !envIsSet(conn.Credential.AppSecretEnv) {
				status = "warning"
				message = conn.Credential.AppSecretEnv + " 未设置。"
				phase = "credential"
				code = "secret_missing"
				reportable = true
			} else if conn.Credential.TokenEnv != "" && strings.TrimSpace(conn.Credential.TokenEnv) != "" && !botCredentialSecretSet(conn) {
				status = "warning"
				message = conn.Credential.TokenEnv + " 未设置，且未找到已保存的登录凭据。"
				phase = "credential"
				code = "secret_missing"
				reportable = true
			} else if conn.Provider == "weixin" && !botCredentialSecretSet(conn) {
				status = "warning"
				message = "未找到已保存的微信登录凭据。"
				phase = "credential"
				code = "secret_missing"
				reportable = true
			}
			return botConnectionDiagnostic(&conn, conn.ID, status, phase, code, message, reportable), nil
		}
	}
	return botConnectionDiagnostic(nil, id, "missing", "config", "connection_missing", "未找到连接。", true), nil
}

func (a *App) TestBotConnection(id, target string) (BotConnectionDiagnostic, error) {
	cfg, err := a.loadDesktopBotConfig()
	if err != nil {
		return botConnectionDiagnostic(nil, id, "error", "config", "config_load_failed", err.Error(), true), nil
	}
	var conn *config.BotConnectionConfig
	for i := range cfg.Bot.Connections {
		if cfg.Bot.Connections[i].ID == strings.TrimSpace(id) {
			conn = &cfg.Bot.Connections[i]
			break
		}
	}
	if conn == nil {
		return botConnectionDiagnostic(nil, id, "missing", "config", "connection_missing", "未找到连接。", true), nil
	}
	target = firstNonEmptyBot(strings.TrimSpace(target), firstSessionRemoteID(conn.SessionMappings))
	if conn.Provider != "feishu" && conn.Provider != "weixin" {
		return botConnectionDiagnostic(conn, conn.ID, "warning", "send", "test_send_unsupported", "当前渠道暂不支持桌面端主动发送测试消息，可使用诊断检查基础配置。", false), nil
	}
	if target == "" {
		return botConnectionDiagnostic(conn, conn.ID, "warning", "send", "test_target_missing", "请输入测试会话 ID 后再发送测试消息。", false), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var result bot.SendResult
	switch conn.Provider {
	case "feishu":
		feishuCfg := cfg.Bot.Feishu
		feishuCfg.Enabled = true
		feishuCfg.Domain = firstNonEmptyBot(conn.Domain, feishuCfg.Domain)
		feishuCfg.AppID = firstNonEmptyBot(conn.Credential.AppID, feishuCfg.AppID)
		feishuCfg.AppSecretEnv = firstNonEmptyBot(conn.Credential.AppSecretEnv, feishuCfg.AppSecretEnv)
		result, err = feishu.SendText(ctx, feishuCfg, target, "Reasonix bot 测试消息：连接和发送链路可用。")
	case "weixin":
		weixinCfg := cfg.Bot.Weixin
		weixinCfg.Enabled = true
		weixinCfg.AccountID = firstNonEmptyBot(conn.Credential.AccountID, weixinCfg.AccountID)
		weixinCfg.TokenEnv = firstNonEmptyBot(conn.Credential.TokenEnv, weixinCfg.TokenEnv)
		result, err = weixin.SendText(ctx, weixinCfg, target, "Reasonix bot 测试消息：连接和发送链路可用。")
	}
	if err != nil {
		return botConnectionDiagnostic(conn, conn.ID, "error", "send", "test_send_failed", err.Error(), true), nil
	}
	_ = a.rememberBotConnectionRemote(conn.ID, target)
	msg := "测试消息已发送。"
	if result.MessageID != "" {
		msg += " Message ID: " + result.MessageID
	}
	diag := botConnectionDiagnostic(conn, conn.ID, "ok", "send", "test_send_ok", msg, false)
	diag.MessageID = result.MessageID
	return diag, nil
}

func botConnectionDiagnostic(conn *config.BotConnectionConfig, id, status, phase, code, message string, reportable bool) BotConnectionDiagnostic {
	id = strings.TrimSpace(id)
	label := ""
	if conn != nil {
		id = firstNonEmptyBot(strings.TrimSpace(conn.ID), id)
		label = strings.TrimSpace(conn.Label)
	}
	occurredAt := time.Now().UTC().Format(time.RFC3339)
	diag := BotConnectionDiagnostic{
		ID:         id,
		Label:      label,
		Status:     strings.TrimSpace(status),
		Message:    strings.TrimSpace(message),
		Phase:      strings.TrimSpace(phase),
		Code:       strings.TrimSpace(code),
		OccurredAt: occurredAt,
	}
	if reportable {
		diag.ReportKind = "bot"
		diag.ReportDetail = botConnectionReportDetail(conn, id, diag.Status, diag.Phase, diag.Code, diag.Message, occurredAt)
		if diag.ReportDetail == "" {
			diag.ReportKind = ""
		}
	}
	return diag
}

func botConnectionReportDetail(conn *config.BotConnectionConfig, fallbackID, status, phase, code, message, occurredAt string) string {
	provider := "unknown"
	domain := "unknown"
	configuredStatus := ""
	enabled := false
	workspaceScope := "global"
	sessionMappings := 0
	appIDSet := false
	appSecretEnvConfigured := false
	tokenEnvConfigured := false
	secretAvailable := false
	if conn != nil {
		provider = firstNonEmptyBot(strings.TrimSpace(conn.Provider), provider)
		domain = firstNonEmptyBot(strings.TrimSpace(conn.Domain), domain)
		configuredStatus = strings.TrimSpace(conn.Status)
		enabled = conn.Enabled
		if strings.TrimSpace(conn.WorkspaceRoot) != "" {
			workspaceScope = "project"
		}
		sessionMappings = len(conn.SessionMappings)
		appIDSet = strings.TrimSpace(conn.Credential.AppID) != ""
		appSecretEnvConfigured = strings.TrimSpace(conn.Credential.AppSecretEnv) != ""
		tokenEnvConfigured = strings.TrimSpace(conn.Credential.TokenEnv) != ""
		secretAvailable = botCredentialSecretSet(*conn)
	}
	summary := botConnectionReportSummary(code, message)
	lines := []string{
		"Bot connection diagnostic",
		"",
		"connection_id: " + safeBotReportValue(fallbackID),
		"provider: " + safeBotReportValue(provider),
		"domain: " + safeBotReportValue(domain),
		"status: " + safeBotReportValue(status),
		"phase: " + safeBotReportValue(phase),
		"code: " + safeBotReportValue(code),
		fmt.Sprintf("enabled: %t", enabled),
		"configured_status: " + safeBotReportValue(configuredStatus),
		fmt.Sprintf("app_id_set: %t", appIDSet),
		fmt.Sprintf("app_secret_env_configured: %t", appSecretEnvConfigured),
		fmt.Sprintf("token_env_configured: %t", tokenEnvConfigured),
		fmt.Sprintf("secret_available: %t", secretAvailable),
		"workspace_scope: " + workspaceScope,
		fmt.Sprintf("session_mappings: %d", sessionMappings),
		"",
		"summary: " + summary,
	}
	payload := frontendCrashPayload{
		SchemaVersion: 2,
		Kind:          "bot",
		Source:        "bot.runtime",
		Label:         botConnectionReportLabel(provider, domain, phase),
		Message:       strings.Join(lines, "\n"),
		ErrorType:     "BotConnectionDiagnostic",
		ErrorMessage:  summary,
		TopFrame:      "bot." + safeBotReportSegment(phase),
		OccurredAt:    occurredAt,
	}
	detail, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(detail)
}

func botConnectionReportSummary(code, message string) string {
	switch strings.TrimSpace(code) {
	case "config_load_failed":
		return "desktop bot config could not be loaded: " + scrubSensitiveText(message)
	case "connection_missing":
		return "bot connection record was not found"
	case "connection_not_connected":
		return "bot connection is not connected: " + scrubSensitiveText(message)
	case "secret_missing":
		return "required bot credential is not available"
	case "test_send_failed":
		return "bot test message failed: " + scrubSensitiveText(message)
	default:
		if strings.TrimSpace(message) == "" {
			return strings.TrimSpace(code)
		}
		return scrubSensitiveText(message)
	}
}

func botConnectionReportLabel(provider, domain, phase string) string {
	parts := []string{"bot", safeBotReportSegment(provider), safeBotReportSegment(domain), safeBotReportSegment(phase)}
	return strings.Trim(strings.Join(parts, "."), ".")
}

func safeBotReportSegment(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		if b.Len() == 0 || strings.HasSuffix(b.String(), ".") {
			continue
		}
		b.WriteByte('.')
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "unknown"
	}
	return out
}

func safeBotReportValue(s string) string {
	s = safeBotReportSegment(s)
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func (a *App) startFeishuConnectionInstall(domain string) (BotInstallStartResult, error) {
	// The official registration SDK always begins on the Feishu accounts domain.
	// Lark tenants are detected from the first poll response, then polling moves
	// to the Lark accounts domain for the final credential exchange.
	beginDomain := "feishu"
	data, err := postFeishuInstallForm(feishuAccountsBase(beginDomain), map[string]string{
		"action": "begin", "archetype": "PersonalAgent", "auth_method": "client_secret", "request_user_info": "open_id",
	})
	if err != nil {
		return BotInstallStartResult{OK: false, Provider: "feishu", Domain: domain, Message: err.Error()}, nil
	}
	deviceCode := stringValue(data["device_code"])
	verifyURL := stringValue(data["verification_uri_complete"])
	userCode := stringValue(data["user_code"])
	if deviceCode == "" || verifyURL == "" {
		return BotInstallStartResult{OK: false, Provider: "feishu", Domain: domain, Message: "飞书/Lark 授权响应缺少 device_code 或二维码 URL。"}, nil
	}
	qrURL, err := feishuRegistrationQRCodeURL(verifyURL)
	if err != nil {
		return BotInstallStartResult{OK: false, Provider: "feishu", Domain: domain, Message: err.Error()}, nil
	}
	installID := randomInstallID()
	interval := intValue(data["interval"], 5)
	expireIn := intValue(firstAny(data["expire_in"], data["expires_in"]), 300)
	a.mu.Lock()
	if a.botInstalls == nil {
		a.botInstalls = map[string]*botInstallSession{}
	}
	a.botInstalls[installID] = &botInstallSession{
		Provider: "feishu", Domain: domain, PollDomain: beginDomain, DeviceCode: deviceCode, UserCode: userCode,
		StartedAt: time.Now(), ExpireAt: time.Now().Add(time.Duration(expireIn) * time.Second),
	}
	a.mu.Unlock()
	return BotInstallStartResult{OK: true, Provider: "feishu", Domain: domain, InstallID: installID, URL: qrURL, DeviceCode: deviceCode, UserCode: userCode, Interval: interval, ExpireIn: expireIn}, nil
}

func (a *App) pollFeishuConnectionInstall(installID string, session *botInstallSession) (BotInstallPollResult, error) {
	pollDomain := firstNonEmptyBot(session.PollDomain, session.Domain, "feishu")
	data, statusCode, err := postFeishuInstallFormResult(feishuAccountsBase(pollDomain), map[string]string{"action": "poll", "device_code": session.DeviceCode})
	if err != nil {
		return BotInstallPollResult{Status: "error", Error: err.Error()}, nil
	}
	if errText := stringValue(data["error"]); errText != "" {
		if errText == "authorization_pending" || errText == "slow_down" {
			return BotInstallPollResult{Status: "pending", Message: "等待扫码授权。"}, nil
		}
		a.deleteBotInstall(installID)
		return BotInstallPollResult{Status: "error", Error: firstNonEmptyBot(stringValue(data["error_description"]), errText)}, nil
	}
	if statusCode >= 400 {
		a.deleteBotInstall(installID)
		return BotInstallPollResult{Status: "error", Error: fmt.Sprintf("HTTP %d", statusCode)}, nil
	}
	if feishuInstallDomain(session.Domain, data) == "lark" && pollDomain != "lark" {
		a.mu.Lock()
		if current := a.botInstalls[installID]; current != nil {
			current.PollDomain = "lark"
		}
		a.mu.Unlock()
		return BotInstallPollResult{Status: "pending", Message: "已识别为 Lark 授权，继续等待授权完成。"}, nil
	}
	appID := stringValue(data["client_id"])
	appSecret := stringValue(data["client_secret"])
	if appID == "" || appSecret == "" {
		return BotInstallPollResult{Status: "pending", Message: "等待授权完成。"}, nil
	}
	a.deleteBotInstall(installID)
	domain := feishuInstallDomain(firstNonEmptyBot(pollDomain, session.Domain), data)
	userID := feishuInstallUserID(data)
	secretEnv := "FEISHU_BOT_APP_SECRET"
	if domain == "lark" {
		secretEnv = "LARK_BOT_APP_SECRET"
	}
	if err := upsertDotEnv(secretEnv, appSecret); err != nil {
		return BotInstallPollResult{Status: "error", Error: err.Error()}, nil
	}
	label := "飞书"
	if domain == "lark" {
		label = "Lark"
	}
	conn, err := a.upsertBotConnection(config.BotConnectionConfig{
		ID:         connectionID("feishu", domain),
		Provider:   "feishu",
		Domain:     domain,
		Label:      label,
		Enabled:    true,
		Status:     "connected",
		Access:     botInstallAccess(userID),
		Credential: config.BotConnectionCredential{AppID: appID, AppSecretEnv: secretEnv},
	}, func(c *config.Config) {
		c.Bot.Enabled = true
		c.Bot.Feishu.Enabled = true
		c.Bot.Feishu.Domain = domain
		c.Bot.Feishu.AppID = appID
		c.Bot.Feishu.AppSecretEnv = secretEnv
		c.Bot.Feishu.Mode = "websocket"
		c.Bot.Feishu.RequireMention = true
		c.Bot.Allowlist.FeishuUsers = appendUniqueBotString(c.Bot.Allowlist.FeishuUsers, userID)
	})
	if err != nil {
		return BotInstallPollResult{Status: "error", Error: err.Error()}, nil
	}
	a.refreshBotRuntimeAsync()
	return BotInstallPollResult{Done: true, Status: "connected", Connection: conn, Message: label + " 已连接。"}, nil
}

func (a *App) upsertBotConnection(conn config.BotConnectionConfig, updateLegacy func(*config.Config)) (BotConnectionView, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if conn.CreatedAt == "" {
		conn.CreatedAt = now
	}
	conn.UpdatedAt = now
	if conn.Status == "" {
		conn.Status = "connected"
	}
	if normalizeBotConnectionToolApprovalMode(conn.ToolApprovalMode) == "" {
		conn.ToolApprovalMode = "ask"
	}
	if conn.ID == "" {
		conn.ID = connectionID(conn.Provider, conn.Domain)
	}
	err := a.applyConfigOnly(func(c *config.Config) error {
		if updateLegacy != nil {
			updateLegacy(c)
		}
		replaced := false
		for i, existing := range c.Bot.Connections {
			if existing.ID == conn.ID {
				conn.CreatedAt = firstNonEmptyBot(existing.CreatedAt, conn.CreatedAt)
				if !botruntime.BotAccessActive(conn.Access) && botruntime.BotAccessActive(existing.Access) {
					conn.Access = existing.Access
				}
				c.Bot.Connections[i] = conn
				replaced = true
				break
			}
		}
		if !replaced {
			c.Bot.Connections = append(c.Bot.Connections, conn)
		}
		return nil
	})
	return botConnectionView(conn), err
}

func (a *App) rememberBotConnectionRemote(id, remoteID string) error {
	id = strings.TrimSpace(id)
	remoteID = strings.TrimSpace(remoteID)
	if id == "" || remoteID == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return a.applyConfigOnly(func(c *config.Config) error {
		for i := range c.Bot.Connections {
			if c.Bot.Connections[i].ID != id {
				continue
			}
			for j := range c.Bot.Connections[i].SessionMappings {
				if c.Bot.Connections[i].SessionMappings[j].RemoteID == remoteID {
					workspaceRoot := firstNonEmptyBot(c.Bot.Connections[i].SessionMappings[j].WorkspaceRoot, c.Bot.Connections[i].WorkspaceRoot)
					scope := botMappingScope(c.Bot.Connections[i].SessionMappings[j].Scope, workspaceRoot)
					c.Bot.Connections[i].SessionMappings[j].Scope = scope
					c.Bot.Connections[i].SessionMappings[j].WorkspaceRoot = botMappingWorkspaceRoot(scope, workspaceRoot)
					c.Bot.Connections[i].SessionMappings[j].UpdatedAt = now
					c.Bot.Connections[i].UpdatedAt = now
					return nil
				}
			}
			scope := botMappingScope("", c.Bot.Connections[i].WorkspaceRoot)
			c.Bot.Connections[i].SessionMappings = append(c.Bot.Connections[i].SessionMappings, config.BotConnectionSessionMapping{
				RemoteID:      remoteID,
				SessionID:     "",
				Scope:         scope,
				WorkspaceRoot: botMappingWorkspaceRoot(scope, c.Bot.Connections[i].WorkspaceRoot),
				UpdatedAt:     now,
			})
			c.Bot.Connections[i].UpdatedAt = now
			return nil
		}
		return nil
	})
}

func firstSessionRemoteID(mappings []config.BotConnectionSessionMapping) string {
	for _, mapping := range mappings {
		if strings.TrimSpace(mapping.RemoteID) != "" {
			return strings.TrimSpace(mapping.RemoteID)
		}
	}
	return ""
}

func (a *App) deleteBotInstall(installID string) {
	a.mu.Lock()
	delete(a.botInstalls, installID)
	a.mu.Unlock()
}

func normalizeBotInstallTarget(provider, domain string) (string, string) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if provider == "lark" {
		provider = "feishu"
		domain = "lark"
	}
	if provider == "weixin" || provider == "wechat" {
		return "weixin", "weixin"
	}
	if domain != "lark" {
		domain = "feishu"
	}
	return "feishu", domain
}

func feishuAccountsBase(domain string) string {
	if domain == "lark" {
		return "https://accounts.larksuite.com"
	}
	return "https://accounts.feishu.cn"
}

func feishuRegistrationQRCodeURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	query.Set("from", "sdk")
	query.Set("tp", "sdk")
	query.Set("source", "go-sdk")
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func postFeishuInstallForm(base string, body map[string]string) (map[string]any, error) {
	data, status, err := postFeishuInstallFormResult(base, body)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", status, firstNonEmptyBot(stringValue(data["error_description"]), stringValue(data["message"])))
	}
	return data, nil
}

func postFeishuInstallFormResult(base string, body map[string]string) (map[string]any, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reqBody := url.Values{}
	for k, v := range body {
		reqBody.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/oauth/v1/app/registration", strings.NewReader(reqBody.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, resp.StatusCode, err
	}
	return out, resp.StatusCode, nil
}

func botConnectionView(conn config.BotConnectionConfig) BotConnectionView {
	return BotConnectionView{
		ID: conn.ID, Provider: conn.Provider, Domain: conn.Domain, Label: conn.Label, Enabled: conn.Enabled, Status: conn.Status,
		Model: conn.Model, ToolApprovalMode: normalizeBotConnectionToolApprovalMode(conn.ToolApprovalMode), WorkspaceRoot: conn.WorkspaceRoot,
		Access: botAccessViewFromConfig(conn.Access),
		Credential: BotConnectionCredentialView{
			AppID: conn.Credential.AppID, AppSecretEnv: conn.Credential.AppSecretEnv, AccountID: conn.Credential.AccountID, TokenEnv: conn.Credential.TokenEnv,
			SecretSet: botCredentialSecretSet(conn),
		},
		SessionMappings: botSessionMappingViews(conn.SessionMappings, conn.WorkspaceRoot),
		LastError:       conn.LastError, CreatedAt: conn.CreatedAt, UpdatedAt: conn.UpdatedAt,
	}
}

func botCredentialSecretSet(conn config.BotConnectionConfig) bool {
	if conn.Credential.AppSecretEnv != "" {
		return envIsSet(conn.Credential.AppSecretEnv)
	}
	if conn.Credential.TokenEnv != "" && envIsSet(conn.Credential.TokenEnv) {
		return true
	}
	if conn.Provider == "weixin" {
		return weixin.HasSavedAccount(conn.Credential.AccountID)
	}
	return false
}

func feishuInstallDomain(fallback string, data map[string]any) string {
	if userInfo, ok := data["user_info"].(map[string]any); ok {
		if strings.EqualFold(stringValue(userInfo["tenant_brand"]), "lark") {
			return "lark"
		}
		return "feishu"
	}
	if strings.EqualFold(fallback, "lark") {
		return "lark"
	}
	return "feishu"
}

func feishuInstallUserID(data map[string]any) string {
	if userInfo, ok := data["user_info"].(map[string]any); ok {
		return firstNonEmptyBot(
			stringValue(userInfo["open_id"]),
			stringValue(userInfo["union_id"]),
			stringValue(userInfo["user_id"]),
		)
	}
	return ""
}

func botConnectionViews(connections []config.BotConnectionConfig) []BotConnectionView {
	if connections == nil {
		return []BotConnectionView{}
	}
	out := make([]BotConnectionView, 0, len(connections))
	for _, conn := range connections {
		out = append(out, botConnectionView(conn))
	}
	return out
}

func botConnectionConfig(view BotConnectionView) config.BotConnectionConfig {
	return config.BotConnectionConfig{
		ID:               strings.TrimSpace(view.ID),
		Provider:         strings.TrimSpace(view.Provider),
		Domain:           strings.TrimSpace(view.Domain),
		Label:            strings.TrimSpace(view.Label),
		Enabled:          view.Enabled,
		Status:           strings.TrimSpace(view.Status),
		Model:            strings.TrimSpace(view.Model),
		ToolApprovalMode: firstNonEmptyBot(normalizeBotConnectionToolApprovalMode(view.ToolApprovalMode), "ask"),
		WorkspaceRoot:    strings.TrimSpace(view.WorkspaceRoot),
		Access:           botAccessConfigFromView(view.Access),
		Credential: config.BotConnectionCredential{
			AppID:        strings.TrimSpace(view.Credential.AppID),
			AppSecretEnv: strings.TrimSpace(view.Credential.AppSecretEnv),
			AccountID:    strings.TrimSpace(view.Credential.AccountID),
			TokenEnv:     strings.TrimSpace(view.Credential.TokenEnv),
		},
		SessionMappings: botSessionMappingConfigs(view.SessionMappings, view.WorkspaceRoot),
		LastError:       strings.TrimSpace(view.LastError),
		CreatedAt:       strings.TrimSpace(view.CreatedAt),
		UpdatedAt:       strings.TrimSpace(view.UpdatedAt),
	}
}

func normalizeBotConnectionToolApprovalMode(mode string) string {
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

func botConnectionConfigs(views []BotConnectionView) []config.BotConnectionConfig {
	if views == nil {
		return nil
	}
	out := make([]config.BotConnectionConfig, 0, len(views))
	for _, view := range views {
		cfg := botConnectionConfig(view)
		if cfg.ID == "" || cfg.Provider == "" {
			continue
		}
		out = append(out, cfg)
	}
	return out
}

func botMappingScope(scope, workspaceRoot string) string {
	if strings.TrimSpace(scope) == "project" {
		return "project"
	}
	if strings.TrimSpace(workspaceRoot) != "" {
		return "project"
	}
	return "global"
}

func botMappingWorkspaceRoot(scope, workspaceRoot string) string {
	if botMappingScope(scope, workspaceRoot) != "project" {
		return ""
	}
	return strings.TrimSpace(workspaceRoot)
}

func botSessionMappingViews(mappings []config.BotConnectionSessionMapping, connectionWorkspaceRoot string) []BotConnectionSessionMappingView {
	if mappings == nil {
		return []BotConnectionSessionMappingView{}
	}
	out := make([]BotConnectionSessionMappingView, 0, len(mappings))
	for _, m := range mappings {
		workspaceRoot := firstNonEmptyBot(m.WorkspaceRoot, connectionWorkspaceRoot)
		scope := botMappingScope(m.Scope, workspaceRoot)
		out = append(out, BotConnectionSessionMappingView{
			RemoteID:      m.RemoteID,
			SessionID:     m.SessionID,
			SessionSource: m.SessionSource,
			ChatType:      m.ChatType,
			UserID:        m.UserID,
			ThreadID:      m.ThreadID,
			Scope:         scope,
			WorkspaceRoot: botMappingWorkspaceRoot(scope, workspaceRoot),
			UpdatedAt:     m.UpdatedAt,
		})
	}
	return out
}

func botSessionMappingConfigs(mappings []BotConnectionSessionMappingView, connectionWorkspaceRoot string) []config.BotConnectionSessionMapping {
	if mappings == nil {
		return nil
	}
	out := make([]config.BotConnectionSessionMapping, 0, len(mappings))
	for _, m := range mappings {
		workspaceRoot := firstNonEmptyBot(m.WorkspaceRoot, connectionWorkspaceRoot)
		scope := botMappingScope(m.Scope, workspaceRoot)
		out = append(out, config.BotConnectionSessionMapping{
			RemoteID:      strings.TrimSpace(m.RemoteID),
			SessionID:     strings.TrimSpace(m.SessionID),
			SessionSource: strings.TrimSpace(m.SessionSource),
			ChatType:      strings.TrimSpace(m.ChatType),
			UserID:        strings.TrimSpace(m.UserID),
			ThreadID:      strings.TrimSpace(m.ThreadID),
			Scope:         scope,
			WorkspaceRoot: botMappingWorkspaceRoot(scope, workspaceRoot),
			UpdatedAt:     strings.TrimSpace(m.UpdatedAt),
		})
	}
	return out
}

func connectionID(provider, domain string) string {
	return strings.Trim(strings.ToLower(provider+"-"+domain), "-")
}

func botInstallAccess(userID string) config.BotAccessConfig {
	userID = strings.TrimSpace(userID)
	access := config.BotAccessConfig{Enabled: true, PairingEnabled: true}
	if userID != "" {
		access.Users = []string{userID}
	}
	return access
}

func randomInstallID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("install-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func envIsSet(name string) bool {
	return strings.TrimSpace(name) != "" && strings.TrimSpace(os.Getenv(name)) != ""
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmptyBot(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func appendUniqueBotString(values []string, next string) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return values
	}
	for _, value := range values {
		if strings.TrimSpace(value) == next {
			return values
		}
	}
	return append(values, next)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intValue(value any, fallback int) int {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func weixinInstallStatusMessage(status string) string {
	switch status {
	case "scaned":
		return "已扫码，请在微信里确认。"
	case "scaned_but_redirect":
		return "已扫码，正在切换微信授权节点。"
	default:
		return "等待扫码。"
	}
}

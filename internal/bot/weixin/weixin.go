// Package weixin 实现微信 iLink Bot 适配器。
// 参考 Hermes Agent 的 weixin adapter：
// - getupdates 长轮询
// - sendmessage / sendtyping
// - context_token 持久化
// - 二维码登录
// - DM allowlist（默认只对 allowlist 内用户开放 DM；群聊默认关闭）
package weixin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"
	fileencoding "reasonix/internal/fileutil/encoding"
)

const (
	defaultWeixinAPI = "https://ilinkai.weixin.qq.com"
	getUpdatesPath   = "/ilink/bot/getupdates"
	sendMessagePath  = "/ilink/bot/sendmessage"
	sendTypingPath   = "/ilink/bot/sendtyping"
	uploadMediaPath  = "/ilink/bot/getuploadurl"
	getBotQRPath     = "/ilink/bot/get_bot_qrcode"
	getQRStatusPath  = "/ilink/bot/get_qrcode_status"

	ilinkAppID          = "bot"
	ilinkClientVersion  = (2 << 16) | (2 << 8)
	ilinkChannelVersion = "2.2.0"
	weixinItemText      = 1
	weixinMsgTypeBot    = 2
	weixinMsgStateDone  = 2

	weixinHTTPTimeout = 30 * time.Second
)

var weixinHTTPClient = &http.Client{Timeout: weixinHTTPTimeout}

// ilinkUpdate 微信 iLink getupdates 返回的更新消息。
type ilinkUpdate struct {
	UpdateID   int64  `json:"update_id"`
	UpdateType string `json:"update_type"`
	Message    struct {
		MessageID ilinkString `json:"message_id"`
		ChatID    string      `json:"chat_id"`
		ChatType  string      `json:"chat_type"`
		From      struct {
			UserID   string `json:"user_id"`
			UserName string `json:"user_name"`
		} `json:"from"`
		Text      string `json:"text"`
		Timestamp int64  `json:"timestamp"`
	} `json:"message"`
}

type ilinkMessage struct {
	MessageID    ilinkString `json:"message_id"`
	FromUserID   string      `json:"from_user_id"`
	ToUserID     string      `json:"to_user_id"`
	RoomID       string      `json:"room_id"`
	ChatRoomID   string      `json:"chat_room_id"`
	ContextToken string      `json:"context_token"`
	MsgType      int         `json:"msg_type"`
	ItemList     []struct {
		Type     int `json:"type"`
		TextItem struct {
			Text string `json:"text"`
		} `json:"text_item"`
	} `json:"item_list"`
}

type ilinkResponse struct {
	Ret                  int            `json:"ret"`
	Errcode              int            `json:"errcode"`
	Errmsg               string         `json:"errmsg"`
	Updates              []ilinkUpdate  `json:"updates"`
	Msgs                 []ilinkMessage `json:"msgs"`
	HasMore              bool           `json:"has_more"`
	ContextToken         string         `json:"context_token"`
	GetUpdatesBuf        string         `json:"get_updates_buf"`
	LongpollingTimeoutMs int            `json:"longpolling_timeout_ms"`
}

type ilinkString string

func (s *ilinkString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = ilinkString(str)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(data, &num); err == nil {
		*s = ilinkString(num.String())
		return nil
	}
	return fmt.Errorf("ilink string: expected string or number, got %s", string(data))
}

// adapter 微信适配器实现。
type adapter struct {
	cfg    config.WeixinBotConfig
	logger *slog.Logger
	msgCh  chan bot.InboundMessage
	cancel context.CancelFunc

	mu            sync.Mutex
	contextTokens map[string]string
	syncBuf       string
	lastUpdateID  int64
	pollReadyOnce sync.Once
	lastPollLog   time.Time
}

// New 创建微信 Bot 适配器。
func New(cfg config.WeixinBotConfig, logger *slog.Logger) bot.Adapter {
	return &adapter{
		cfg:           cfg,
		logger:        logger.With("platform", "weixin"),
		contextTokens: make(map[string]string),
	}
}

func (a *adapter) Platform() bot.Platform { return bot.PlatformWeixin }
func (a *adapter) Name() string           { return "weixin" }

func (a *adapter) Start(ctx context.Context) error {
	a.msgCh = make(chan bot.InboundMessage, 64)
	ctx, a.cancel = context.WithCancel(ctx)
	a.loadContextTokens()
	if a.token() == "" {
		return a.tokenMissingError()
	}

	a.logger.Info("weixin polling started", "account", logHash(a.accountID()), "api_base", a.apiBase())
	go a.pollLoop(ctx)
	return nil
}

func (a *adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func (a *adapter) Send(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	return a.sendMessage(ctx, msg)
}

func (a *adapter) SendTyping(ctx context.Context, chatID string) error {
	return a.sendTyping(ctx, chatID)
}

func (a *adapter) Messages() <-chan bot.InboundMessage {
	return a.msgCh
}

// SendText sends one plain text message to a saved Weixin iLink conversation.
// It is used by desktop settings as an actual connection test.
func SendText(ctx context.Context, cfg config.WeixinBotConfig, chatID, text string) (bot.SendResult, error) {
	a := &adapter{cfg: cfg, logger: slog.Default().With("platform", "weixin"), contextTokens: make(map[string]string)}
	return a.sendMessage(ctx, bot.OutboundMessage{ChatID: chatID, Text: text})
}

// token 从环境变量获取微信 token。
func (a *adapter) token() string {
	if token := os.Getenv(a.cfg.TokenEnv); token != "" {
		return token
	}
	account, _ := loadSavedAccount(a.accountID())
	if account.Token != "" {
		return account.Token
	}
	if a.cfg.AccountID == "" {
		account, _ = loadAnySavedAccount()
		return account.Token
	}
	return ""
}

func (a *adapter) tokenMissingError() error {
	if strings.TrimSpace(a.cfg.TokenEnv) == "" {
		return fmt.Errorf("weixin token is not configured and no saved weixin account is available")
	}
	return fmt.Errorf("%s not set and no saved weixin account is available", a.cfg.TokenEnv)
}

// apiBase 返回 API base URL。
func (a *adapter) apiBase() string {
	if a.cfg.APIBase != "" {
		return a.cfg.APIBase
	}
	account, _ := loadSavedAccount(a.accountID())
	if account.BaseURL != "" {
		return strings.TrimRight(account.BaseURL, "/")
	}
	return defaultWeixinAPI
}

func (a *adapter) accountID() string {
	if a.cfg.AccountID != "" {
		return a.cfg.AccountID
	}
	return "default"
}

func (a *adapter) contextToken(chatID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.contextTokens[chatID]
}

func (a *adapter) setContextToken(chatID, token string) {
	a.mu.Lock()
	if token == "" {
		delete(a.contextTokens, chatID)
	} else {
		a.contextTokens[chatID] = token
	}
	a.mu.Unlock()
	a.saveContextTokens()
}

func (a *adapter) tokenStorePath() string {
	root := config.MemoryUserDir()
	if root == "" {
		return ""
	}
	return filepath.Join(weixinAccountDir(root), a.accountID()+".context-tokens.json")
}

func (a *adapter) loadContextTokens() {
	path := a.tokenStorePath()
	if path == "" {
		return
	}
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return
	}
	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		a.logger.Warn("failed to load weixin context tokens", "err", err)
		return
	}
	a.mu.Lock()
	a.contextTokens = tokens
	a.mu.Unlock()
}

func (a *adapter) saveContextTokens() {
	path := a.tokenStorePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		a.logger.Warn("failed to create weixin token dir", "err", err)
		return
	}
	a.mu.Lock()
	data, err := json.MarshalIndent(a.contextTokens, "", "  ")
	a.mu.Unlock()
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		a.logger.Warn("failed to save weixin context tokens", "err", err)
	}
}

func ilinkGET(ctx context.Context, baseURL, endpoint string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/"+strings.TrimLeft(endpoint, "/"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("iLink-App-Id", ilinkAppID)
	req.Header.Set("iLink-App-ClientVersion", fmt.Sprintf("%d", ilinkClientVersion))
	resp, err := weixinHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		if len(data) > 200 {
			data = data[:200]
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// pollLoop 长轮询获取更新。
func (a *adapter) pollLoop(ctx context.Context) {
	// 启动时短暂等待让登录完成
	if !bot.SleepCtx(ctx, 2*time.Second) {
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}

		updates, err := a.getUpdates(ctx)
		if err != nil {
			a.logger.Error("getupdates failed", "err", err)
			if !bot.SleepCtx(ctx, 5*time.Second) {
				return
			}
			continue
		}

		for _, upd := range updates {
			a.handleUpdate(upd)
		}

		// 没有更新时短暂等待
		if len(updates) == 0 {
			if !bot.SleepCtx(ctx, 500*time.Millisecond) {
				return
			}
		}
	}
}

// getUpdates 调用微信 iLink getupdates API。
func (a *adapter) getUpdates(ctx context.Context) ([]ilinkUpdate, error) {
	tok := a.token()
	if tok == "" {
		return nil, a.tokenMissingError()
	}

	url := a.apiBase() + getUpdatesPath

	a.mu.Lock()
	payload := map[string]interface{}{
		"get_updates_buf": a.syncBuf,
		"base_info": map[string]string{
			"channel_version": ilinkChannelVersion,
		},
	}
	a.mu.Unlock()

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	setIlinkHeaders(req, tok, body)

	resp, err := weixinHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ilinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Ret != 0 || result.Errcode != 0 {
		return nil, fmt.Errorf("getupdates error ret=%d errcode=%d: %s", result.Ret, result.Errcode, result.Errmsg)
	}
	a.pollReadyOnce.Do(func() {
		a.logger.Info("weixin getupdates ready", "account", logHash(a.accountID()), "api_base", a.apiBase())
	})
	a.logPollHealth(result)

	a.mu.Lock()
	if result.GetUpdatesBuf != "" {
		a.syncBuf = result.GetUpdatesBuf
	}
	if len(result.Updates) > 0 {
		last := result.Updates[len(result.Updates)-1]
		a.lastUpdateID = last.UpdateID
	}
	a.mu.Unlock()

	if len(result.Msgs) > 0 {
		for _, msg := range result.Msgs {
			a.handleIlinkMessage(msg)
		}
	}
	return result.Updates, nil
}

func (a *adapter) logPollHealth(result ilinkResponse) {
	shouldLog := len(result.Updates) > 0 || len(result.Msgs) > 0
	a.mu.Lock()
	if !shouldLog && time.Since(a.lastPollLog) >= 5*time.Minute {
		shouldLog = true
	}
	if shouldLog {
		a.lastPollLog = time.Now()
	}
	a.mu.Unlock()
	if !shouldLog {
		return
	}
	a.logger.Info("weixin getupdates heartbeat",
		"updates", len(result.Updates),
		"msgs", len(result.Msgs),
		"has_more", result.HasMore,
		"timeout_ms", result.LongpollingTimeoutMs)
}

// handleUpdate 处理单条微信更新消息。
func (a *adapter) handleUpdate(upd ilinkUpdate) {
	if upd.UpdateType != "message" {
		a.logger.Info("weixin update ignored", "reason", "non_message", "update_type", upd.UpdateType)
		return
	}

	m := upd.Message
	chatType := bot.ChatDM
	if m.ChatType == "group" {
		chatType = bot.ChatGroup
	}

	ib := bot.InboundMessage{
		Platform:  bot.PlatformWeixin,
		ChatType:  chatType,
		ChatID:    m.ChatID,
		UserID:    m.From.UserID,
		UserName:  m.From.UserName,
		Text:      m.Text,
		MessageID: string(m.MessageID),
	}

	select {
	case a.msgCh <- ib:
		a.logger.Info("weixin inbound queued", "source", "update", "chat_type", chatType, "chat", logHash(ib.ChatID), "user", logHash(ib.UserID), "message", logHash(ib.MessageID), "text_chars", len([]rune(ib.Text)))
	default:
		a.logger.Warn("weixin message channel full")
	}
}

func (a *adapter) handleIlinkMessage(m ilinkMessage) {
	if m.FromUserID == "" || m.FromUserID == a.accountID() {
		a.logger.Info("weixin message ignored", "reason", "self_or_missing_sender", "from", logHash(m.FromUserID), "message", logHash(string(m.MessageID)))
		return
	}
	text := extractIlinkText(m.ItemList)
	if text == "" {
		a.logger.Info("weixin message ignored", "reason", "empty_text", "from", logHash(m.FromUserID), "message", logHash(string(m.MessageID)))
		return
	}
	chatType, chatID := guessIlinkChat(m, a.accountID())
	if chatID == "" {
		a.logger.Info("weixin message ignored", "reason", "missing_chat", "from", logHash(m.FromUserID), "message", logHash(string(m.MessageID)))
		return
	}
	if m.ContextToken != "" {
		a.setContextToken(chatID, m.ContextToken)
	}
	ib := bot.InboundMessage{
		Platform:  bot.PlatformWeixin,
		ChatType:  chatType,
		ChatID:    chatID,
		UserID:    m.FromUserID,
		UserName:  m.FromUserID,
		Text:      text,
		MessageID: string(m.MessageID),
	}
	select {
	case a.msgCh <- ib:
		a.logger.Info("weixin inbound queued", "source", "message", "chat_type", chatType, "chat", logHash(ib.ChatID), "user", logHash(ib.UserID), "message", logHash(ib.MessageID), "text_chars", len([]rune(ib.Text)))
	default:
		a.logger.Warn("weixin message channel full")
	}
}

func logHash(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:12]
}

func extractIlinkText(items []struct {
	Type     int `json:"type"`
	TextItem struct {
		Text string `json:"text"`
	} `json:"text_item"`
}) string {
	var out []string
	for _, item := range items {
		if item.Type == weixinItemText && item.TextItem.Text != "" {
			out = append(out, item.TextItem.Text)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func guessIlinkChat(m ilinkMessage, accountID string) (bot.ChatType, string) {
	roomID := firstNonEmptyString(m.RoomID, m.ChatRoomID)
	if roomID != "" {
		return bot.ChatGroup, roomID
	}
	if m.ToUserID != "" && accountID != "" && m.ToUserID != accountID && m.MsgType == 1 {
		return bot.ChatGroup, m.ToUserID
	}
	return bot.ChatDM, m.FromUserID
}

func setIlinkHeaders(req *http.Request, token string, body []byte) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	req.Header.Set("X-WECHAT-UIN", randomWechatUIN())
	req.Header.Set("iLink-App-Id", ilinkAppID)
	req.Header.Set("iLink-App-ClientVersion", fmt.Sprintf("%d", ilinkClientVersion))
}

func randomWechatUIN() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]))))
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// sendMessage 使用微信 iLink sendmessage API 发送消息。
func (a *adapter) sendMessage(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	tok := a.token()
	if tok == "" {
		return bot.SendResult{}, a.tokenMissingError()
	}

	url := a.apiBase() + sendMessagePath

	payload := map[string]interface{}{
		"base_info": map[string]string{"channel_version": ilinkChannelVersion},
		"msg": map[string]interface{}{
			"from_user_id":  "",
			"to_user_id":    msg.ChatID,
			"client_id":     fmt.Sprintf("reasonix-%d", time.Now().UnixNano()),
			"message_type":  weixinMsgTypeBot,
			"message_state": weixinMsgStateDone,
			"item_list": []map[string]interface{}{
				{"type": weixinItemText, "text_item": map[string]string{"text": msg.Text}},
			},
		},
	}
	if contextToken := a.contextToken(msg.ChatID); contextToken != "" {
		if m, ok := payload["msg"].(map[string]interface{}); ok {
			m["context_token"] = contextToken
		}
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return bot.SendResult{}, err
	}
	setIlinkHeaders(req, tok, body)

	resp, err := weixinHTTPClient.Do(req)
	if err != nil {
		return bot.SendResult{}, err
	}
	defer resp.Body.Close()

	var result struct {
		Ret       int         `json:"ret"`
		Errcode   int         `json:"errcode"`
		Errmsg    string      `json:"errmsg"`
		MessageID ilinkString `json:"message_id"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &result); err != nil {
		return bot.SendResult{}, err
	}
	if result.Ret != 0 || result.Errcode != 0 {
		if a.contextToken(msg.ChatID) != "" {
			a.setContextToken(msg.ChatID, "")
			return a.sendMessage(ctx, msg)
		}
		return bot.SendResult{}, fmt.Errorf("sendmessage error ret=%d errcode=%d: %s", result.Ret, result.Errcode, result.Errmsg)
	}

	return bot.SendResult{MessageID: string(result.MessageID)}, nil
}

// sendTyping 发送"正在输入"状态。
func (a *adapter) sendTyping(ctx context.Context, chatID string) error {
	tok := a.token()
	if tok == "" {
		return a.tokenMissingError()
	}

	url := a.apiBase() + sendTypingPath

	payload := map[string]interface{}{
		"base_info":     map[string]string{"channel_version": ilinkChannelVersion},
		"ilink_user_id": chatID,
		"status":        1,
	}
	if contextToken := a.contextToken(chatID); contextToken != "" {
		payload["context_token"] = contextToken
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	setIlinkHeaders(req, tok, body)

	resp, err := weixinHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Ret     int    `json:"ret"`
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Ret != 0 || result.Errcode != 0 {
		return fmt.Errorf("sendtyping error ret=%d errcode=%d: %s", result.Ret, result.Errcode, result.Errmsg)
	}

	return nil
}

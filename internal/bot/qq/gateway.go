package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/bot"

	"golang.org/x/net/websocket"
)

const (
	qqTokenURL                     = "https://bots.qq.com/app/getAppAccessToken"
	qqBaseURL                      = "https://api.sgroup.qq.com"
	qqSandboxURL                   = "https://sandbox.api.sgroup.qq.com"
	qqGatewayURL                   = "wss://api.sgroup.qq.com/websocket"
	qqMaxChunkBytes                = 1500
	qqMaxPassiveReplyChunks        = 5
	qqMinHeartbeat                 = 5 * time.Second
	qqMaxHeartbeat                 = time.Minute
	qqStartupValidationTimeout     = 10 * time.Second
	qqPassiveReplyTruncationNotice = "\n\n[Truncated: QQ allows at most 5 passive replies for one incoming message.]"

	opDispatch     = 0
	opHeartbeat    = 1
	opIdentify     = 2
	opResume       = 6
	opReconnect    = 7
	opInvalid      = 9
	opHello        = 10
	opHeartbeatAck = 11
)

var qqMarkdownWrapperRe = regexp.MustCompile("(?is)^```(?:markdown|md)\\s*\\r?\\n([\\s\\S]*?)\\r?\\n```$")

var allowedGatewayHosts = []string{
	"api.sgroup.qq.com",
	"sandbox.api.sgroup.qq.com",
	"qq.com",
}

// gatewayPayload QQ WebSocket 消息载荷。
type gatewayPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  int64           `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

type helloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type identifyData struct {
	Token      string     `json:"token"`
	Intents    int        `json:"intents"`
	Shard      [2]int     `json:"shard"`
	Properties properties `json:"properties"`
}

type properties struct {
	OS      string `json:"$os"`
	Browser string `json:"$browser"`
	Device  string `json:"$device"`
}

type dispatchEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	Author    struct {
		ID           string `json:"id"`
		UserOpenID   string `json:"user_openid"`
		MemberOpenID string `json:"member_openid"`
		UnionOpenID  string `json:"union_openid"`
		Username     string `json:"username"`
	} `json:"author"`
	ChannelID   string `json:"channel_id"`
	GuildID     string `json:"guild_id"`
	GroupOpenID string `json:"group_openid"`
}

// wsClient 管理 QQ WebSocket 连接。
type wsClient struct {
	mu          sync.Mutex
	conn        *websocket.Conn
	heartbeatMs int
	sessionID   string
	lastSeq     int64
	token       string
	logger      *slog.Logger
}

func (a *adapter) gatewayLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		token, err := a.getAccessToken(ctx)
		if err != nil {
			a.logger.Error("get access token failed", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if err := a.connectGateway(ctx, token); err != nil {
			a.logger.Error("gateway connection failed", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

func (a *adapter) getAccessToken(ctx context.Context) (string, error) {
	a.tokenMu.Lock()
	if a.token != "" && time.Now().Before(a.tokenExpiry) {
		token := a.token
		a.tokenMu.Unlock()
		return token, nil
	}
	a.tokenMu.Unlock()

	appID := a.appID()
	appSecret := a.appSecret()
	if appID == "" {
		return "", fmt.Errorf("qq app_id is empty")
	}
	if appSecret == "" {
		return "", fmt.Errorf("qq app secret is empty: set %s or QQ_SECRET", a.appSecretEnvName())
	}
	body, err := json.Marshal(map[string]string{
		"appId":        appID,
		"clientSecret": appSecret,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", qqTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("qq token api error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"-"`
		ExpiresRaw  any    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	result.ExpiresIn, err = qqExpiresInSeconds(result.ExpiresRaw)
	if err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("empty access token")
	}
	a.tokenMu.Lock()
	a.token = result.AccessToken
	if result.ExpiresIn > 60 {
		a.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	} else {
		a.tokenExpiry = time.Now().Add(5 * time.Minute)
	}
	a.tokenMu.Unlock()
	a.logger.Info("qq access token acquired", "expires_in_seconds", result.ExpiresIn)
	return result.AccessToken, nil
}

func qqExpiresInSeconds(value any) (int, error) {
	switch v := value.(type) {
	case nil:
		return 0, nil
	case float64:
		return int(v), nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, fmt.Errorf("invalid qq token expires_in %q: %w", v, err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("invalid qq token expires_in type %T", value)
	}
}

func (a *adapter) connectGateway(ctx context.Context, token string) error {
	gatewayURL, err := a.getGatewayURL(ctx, token)
	if err != nil {
		return err
	}
	if parsed, parseErr := url.Parse(gatewayURL); parseErr == nil {
		a.logger.Info("qq gateway endpoint resolved", "host", parsed.Hostname(), "sandbox", a.cfg.Sandbox)
	}
	cfg, err := websocket.NewConfig(gatewayURL, gatewayURL)
	if err != nil {
		return err
	}
	cfg.Header = http.Header{}
	cfg.Header.Set("Authorization", "QQBot "+token)
	cfg.Header.Set("X-Union-Appid", a.appID())

	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}
	defer conn.Close()
	a.logger.Info("qq gateway connected", "sandbox", a.cfg.Sandbox)

	ws := &wsClient{conn: conn, token: token, logger: a.logger}
	a.ws = ws

	var msg gatewayPayload
	decoder := json.NewDecoder(conn)

	// 第一次读取必须是 Hello
	if err := decoder.Decode(&msg); err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	if msg.Op != opHello {
		return fmt.Errorf("expected op=%d hello, got op=%d", opHello, msg.Op)
	}
	var hello helloData
	if err := json.Unmarshal(msg.D, &hello); err != nil {
		return err
	}
	ws.heartbeatMs = int(sanitizeHeartbeatInterval(time.Duration(hello.HeartbeatInterval) * time.Millisecond).Milliseconds())

	// Identify
	identify := identifyData{
		Token:   fmt.Sprintf("QQBot %s", token),
		Intents: 1<<0 | 1<<1 | 1<<9 | 1<<10 | 1<<12 | 1<<25 | 1<<26,
		Shard:   [2]int{0, 1},
		Properties: properties{
			OS:      "linux",
			Browser: "reasonix",
			Device:  "reasonix-bot",
		},
	}
	identifyJSON, _ := json.Marshal(identify)
	if err := ws.send(opIdentify, identifyJSON); err != nil {
		return fmt.Errorf("send identify: %w", err)
	}

	// 读取 Ready
	if err := decoder.Decode(&msg); err != nil {
		return fmt.Errorf("read ready: %w", err)
	}
	if msg.Op == opDispatch && msg.T == "READY" {
		var ready struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(msg.D, &ready); err != nil {
			return fmt.Errorf("decode ready: %w", err)
		}
		ws.sessionID = ready.SessionID
		a.sessionID = ready.SessionID
		a.seq = msg.S
		a.logger.Info("qq gateway ready", "sandbox", a.cfg.Sandbox, "heartbeat_ms", ws.heartbeatMs)
	} else {
		a.logger.Warn("qq gateway expected ready event", "op", msg.Op, "event", msg.T)
	}

	// 启动 heartbeat
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(time.Duration(ws.heartbeatMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				ws.mu.Lock()
				payload := json.RawMessage("null")
				if ws.lastSeq != 0 {
					payload = json.RawMessage(fmt.Sprintf(`%d`, ws.lastSeq))
				}
				if err := ws.send(opHeartbeat, payload); err != nil {
					ws.logger.Error("heartbeat failed", "err", err)
					ws.mu.Unlock()
					return
				}
				ws.mu.Unlock()
			}
		}
	}()

	// 主循环：读取 dispatch 事件
	for {
		if err := decoder.Decode(&msg); err != nil {
			a.logger.Error("decode gateway message", "err", err)
			heartbeatCancel()
			<-heartbeatDone
			return err
		}
		ws.lastSeq = msg.S
		a.seq = msg.S

		switch msg.Op {
		case opDispatch:
			a.handleDispatch(msg)
		case opHeartbeatAck:
		case opReconnect:
			a.logger.Info("gateway requested reconnect")
			heartbeatCancel()
			<-heartbeatDone
			return nil
		case opInvalid:
			a.sessionID = ""
			a.seq = 0
			a.logger.Info("gateway session invalidated")
			heartbeatCancel()
			<-heartbeatDone
			return nil
		}
	}
}

func (a *adapter) appID() string {
	if value := strings.TrimSpace(a.cfg.AppID); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("QQ_APPID"))
}

func (a *adapter) appSecretEnvName() string {
	if value := strings.TrimSpace(a.cfg.AppSecretEnv); value != "" {
		return value
	}
	return "QQ_BOT_APP_SECRET"
}

func (a *adapter) appSecret() string {
	if value := strings.TrimSpace(os.Getenv(a.appSecretEnvName())); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("QQ_SECRET"))
}

func (a *adapter) apiBaseURL() string {
	if a.cfg.Sandbox {
		return qqSandboxURL
	}
	return qqBaseURL
}

func (a *adapter) getGatewayURL(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.apiBaseURL()+"/gateway", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("qq gateway api error %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	return validateGatewayURL(result.URL)
}

func validateGatewayURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty qq gateway url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "wss" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !allowedGatewayHost(u.Hostname()) {
		return "", fmt.Errorf("unexpected qq gateway url: %s", raw)
	}
	return u.String(), nil
}

func allowedGatewayHost(hostname string) bool {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	for _, allowed := range allowedGatewayHosts {
		if hostname == allowed || strings.HasSuffix(hostname, "."+allowed) {
			return true
		}
	}
	return false
}

func sanitizeHeartbeatInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return qqMinHeartbeat
	}
	if interval < qqMinHeartbeat {
		return qqMinHeartbeat
	}
	if interval > qqMaxHeartbeat {
		return qqMaxHeartbeat
	}
	return interval
}

func (ws *wsClient) send(op int, d json.RawMessage) error {
	payload := gatewayPayload{Op: op, D: d}
	data, _ := json.Marshal(payload)
	_, err := ws.conn.Write(data)
	return err
}

func (a *adapter) handleDispatch(msg gatewayPayload) {
	var evt dispatchEvent
	if err := json.Unmarshal(msg.D, &evt); err != nil {
		a.logger.Error("parse dispatch", "err", err)
		return
	}

	ib := bot.InboundMessage{
		Platform:  bot.PlatformQQ,
		UserID:    qqAuthorID(evt),
		UserName:  evt.Author.Username,
		Text:      evt.Content,
		MessageID: evt.ID,
	}

	switch msg.T {
	case "C2C_MESSAGE_CREATE":
		ib.ChatType = bot.ChatDM
		ib.ChatID = ib.UserID
	case "GROUP_AT_MESSAGE_CREATE":
		ib.ChatType = bot.ChatGroup
		ib.ChatID = evt.GroupOpenID
	case "AT_MESSAGE_CREATE":
		ib.ChatType = bot.ChatGuild
		ib.ChatID = evt.ChannelID
	case "DIRECT_MESSAGE_CREATE":
		ib.ChatType = bot.ChatDirect
		ib.ChatID = evt.GuildID
	case "MESSAGE_CREATE":
		ib.ChatType = bot.ChatDM
		ib.ChatID = evt.ChannelID
	default:
		if strings.TrimSpace(msg.T) != "" {
			a.logger.Info("qq dispatch ignored", "event", msg.T)
		}
		return // 忽略其他事件
	}
	a.logger.Info("qq dispatch received", "event", msg.T, "chat_type", ib.ChatType)

	select {
	case a.msgCh <- ib:
	default:
		a.logger.Warn("message channel full, dropping message")
	}
}

func qqAuthorID(evt dispatchEvent) string {
	for _, value := range []string{evt.Author.UserOpenID, evt.Author.MemberOpenID, evt.Author.UnionOpenID, evt.Author.ID} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// sendMessage 使用 QQ REST API 发送消息。
func (a *adapter) sendMessage(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	text := normalizeQQMarkdownReply(msg.Text)
	chunks := splitQQMessage(text, qqMaxChunkBytes)
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	originalChunkCount := len(chunks)
	var truncated bool
	chunks, truncated = capQQPassiveReplyChunks(msg, chunks)
	if truncated {
		a.logger.Warn("qq passive reply truncated", "chat_type", msg.ChatType, "chunks", originalChunkCount, "limit", len(chunks))
	}
	var last bot.SendResult
	for _, chunk := range chunks {
		seq := a.nextMessageSeq(msg.ReplyToMsgID)
		var result bot.SendResult
		var err error
		if msg.Keyboard == nil && a.markdownDeliveryDisabled() {
			result, err = a.sendPlainMessageChunk(ctx, msg, chunk, seq)
		} else {
			result, err = a.sendMessageChunk(ctx, msg, chunk, seq)
		}
		if err != nil && msg.Keyboard == nil {
			a.disableMarkdownDelivery()
			a.logger.Warn("qq markdown delivery failed, retrying plain text", "chat_type", msg.ChatType, "err", err)
			result, err = a.sendPlainMessageChunk(ctx, msg, chunk, a.nextMessageSeq(msg.ReplyToMsgID))
		}
		if err != nil {
			a.logger.Error("qq message send failed", "chat_type", msg.ChatType, "err", err)
			return last, err
		}
		a.logger.Info("qq message sent", "chat_type", msg.ChatType, "message_id_set", strings.TrimSpace(result.MessageID) != "")
		last = result
	}
	return last, nil
}

func (a *adapter) sendPlainMessageChunk(ctx context.Context, msg bot.OutboundMessage, text string, seq int) (bot.SendResult, error) {
	return a.sendMessagePayload(ctx, msg, map[string]any{
		"content":  text,
		"msg_type": 0,
	}, seq)
}

func (a *adapter) sendMessageChunk(ctx context.Context, msg bot.OutboundMessage, text string, seq int) (bot.SendResult, error) {
	if msg.Keyboard != nil {
		payload := map[string]any{
			"content":  text,
			"msg_type": 2,
		}
		rows := make([]map[string]any, 0, len(msg.Keyboard.Rows))
		for _, row := range msg.Keyboard.Rows {
			buttons := make([]map[string]any, 0, len(row.Buttons))
			for _, btn := range row.Buttons {
				buttons = append(buttons, map[string]any{
					"id": strings.TrimSpace(btn.ID),
					"render_data": map[string]any{
						"label": btn.Label,
						"style": btn.Style,
					},
					"action": map[string]any{
						"type": 2,
						"data": btn.CallbackID,
					},
				})
			}
			rows = append(rows, map[string]any{"buttons": buttons})
		}
		payload["keyboard"] = map[string]interface{}{
			"content": rows,
		}
		return a.sendMessagePayload(ctx, msg, payload, seq)
	}
	return a.sendMessagePayload(ctx, msg, map[string]any{
		"markdown": map[string]string{"content": text},
		"msg_type": 2,
	}, seq)
}

func (a *adapter) sendMessagePayload(ctx context.Context, msg bot.OutboundMessage, payload map[string]any, seq int) (bot.SendResult, error) {
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return bot.SendResult{}, err
	}

	if msg.ReplyToMsgID != "" {
		payload["msg_id"] = msg.ReplyToMsgID
	}
	if seq > 0 {
		payload["msg_seq"] = seq
	}

	url := a.qqSendURL(msg)

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return bot.SendResult{}, err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Union-Appid", a.appID())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return bot.SendResult{}, err
	}
	defer resp.Body.Close()

	var result struct {
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return bot.SendResult{}, fmt.Errorf("qq api error %d: %s", resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return bot.SendResult{}, fmt.Errorf("decode send response: %w", err)
	}

	return bot.SendResult{MessageID: result.ID}, nil
}

func (a *adapter) qqSendURL(msg bot.OutboundMessage) string {
	base := a.apiBaseURL()
	switch msg.ChatType {
	case bot.ChatGroup:
		return fmt.Sprintf("%s/v2/groups/%s/messages", base, url.PathEscape(msg.ChatID))
	case bot.ChatGuild, bot.ChatThread:
		return fmt.Sprintf("%s/v2/channels/%s/messages", base, url.PathEscape(msg.ChatID))
	case bot.ChatDirect:
		return fmt.Sprintf("%s/v2/dms/%s/messages", base, url.PathEscape(msg.ChatID))
	default:
		return fmt.Sprintf("%s/v2/users/%s/messages", base, url.PathEscape(msg.ChatID))
	}
}

func qqSendURL(msg bot.OutboundMessage) string {
	return (&adapter{}).qqSendURL(msg)
}

func (a *adapter) nextMessageSeq(replyTo string) int {
	if strings.TrimSpace(replyTo) == "" {
		return 0
	}
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	if a.nextOutboundMsgSeq <= 0 {
		a.nextOutboundMsgSeq = 1
	}
	seq := a.nextOutboundMsgSeq
	a.nextOutboundMsgSeq++
	return seq
}

func (a *adapter) markdownDeliveryDisabled() bool {
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	return a.markdownDisabled
}

func (a *adapter) disableMarkdownDelivery() {
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	a.markdownDisabled = true
}

func normalizeQQMarkdownReply(text string) string {
	match := qqMarkdownWrapperRe.FindStringSubmatch(strings.TrimSpace(text))
	if len(match) != 2 {
		return text
	}
	return match[1]
}

func splitQQMessage(text string, maxBytes int) []string {
	if maxBytes <= 0 {
		maxBytes = qqMaxChunkBytes
	}
	var chunks []string
	remaining := text
	for remaining != "" {
		if len([]byte(remaining)) <= maxBytes {
			chunks = append(chunks, remaining)
			break
		}
		candidate := fitUTF8Slice(remaining, maxBytes)
		splitAt := pickNaturalSplit(candidate)
		chunks = append(chunks, candidate[:splitAt])
		remaining = strings.TrimLeft(remaining[splitAt:], " \t\r\n")
	}
	return chunks
}

func capQQPassiveReplyChunks(msg bot.OutboundMessage, chunks []string) ([]string, bool) {
	if !qqUsesPassiveReplyLimit(msg) || len(chunks) <= qqMaxPassiveReplyChunks {
		return chunks, false
	}
	capped := make([]string, 0, qqMaxPassiveReplyChunks)
	capped = append(capped, chunks[:qqMaxPassiveReplyChunks-1]...)
	capped = append(capped, fitQQChunkWithSuffix(chunks[qqMaxPassiveReplyChunks-1], qqPassiveReplyTruncationNotice, qqMaxChunkBytes))
	return capped, true
}

func qqUsesPassiveReplyLimit(msg bot.OutboundMessage) bool {
	if strings.TrimSpace(msg.ReplyToMsgID) == "" {
		return false
	}
	return msg.ChatType == bot.ChatDM || msg.ChatType == bot.ChatGroup
}

func fitQQChunkWithSuffix(text, suffix string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = qqMaxChunkBytes
	}
	suffixBytes := len([]byte(suffix))
	if suffixBytes >= maxBytes {
		return fitUTF8Slice(suffix, maxBytes)
	}
	prefix := strings.TrimRight(fitUTF8Slice(text, maxBytes-suffixBytes), " \t\r\n")
	if prefix == "" {
		return strings.TrimLeft(fitUTF8Slice(suffix, maxBytes), " \t\r\n")
	}
	return prefix + suffix
}

func fitUTF8Slice(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	end := 0
	used := 0
	for len(text[end:]) > 0 {
		r, size := utf8.DecodeRuneInString(text[end:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if used > 0 && used+size > maxBytes {
			break
		}
		end += size
		used += size
		if used >= maxBytes {
			break
		}
	}
	if end > 0 {
		return text[:end]
	}
	_, size := utf8.DecodeRuneInString(text)
	if size == 0 {
		return ""
	}
	return text[:size]
}

func pickNaturalSplit(candidate string) int {
	if candidate == "" {
		return 0
	}
	minSplit := len(candidate) * 6 / 10
	for _, sep := range []string{"\n\n", "\n", " "} {
		if at := strings.LastIndex(candidate, sep); at >= minSplit {
			return at + len(sep)
		}
	}
	return len(candidate)
}

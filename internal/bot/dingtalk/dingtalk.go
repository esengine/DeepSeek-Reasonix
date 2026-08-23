// Package dingtalk 实现钉钉企业内部应用机器人（Stream 模式）适配器。
// 参考 dsh-dingtalk-channel 的设计：
// - Stream 模式（WebSocket 长连接），无需公网回调地址
// - @mention gating（群聊）
// - 消息去重（msgId）
// - 会话 webhook 回复（markdown/text）
package dingtalk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"

	"github.com/gorilla/websocket"
)

const (
	// getTokenURL 获取应用 access_token（旧版接口，GET query 传参）。
	getTokenURL = "https://oapi.dingtalk.com/gettoken"
	// gatewayURL 换取 WebSocket 连接端点。
	gatewayURL = "https://api.dingtalk.com/v1.0/gateway/connections/open"
	// robotMessagesTopic 机器人消息推送 topic。
	robotMessagesTopic = "/v1.0/im/bot/messages/get"

	// tokenTTL 钉钉 access_token 有效期（秒），提前 60s 刷新。
	tokenTTL = 7200 * time.Second
	// connTimeout 建连/HTTP 超时。
	connTimeout = 15 * time.Second
	// sendTimeout 单条回复超时。
	sendTimeout = 15 * time.Second
	// maxMessageBytes 入站消息上限。
	maxMessageBytes = 1 << 20
	// thinkingEmotion 收到消息时贴的表情（"已收到，处理中"信号，回复完成后撤回）。
	// 与 dsh-dingtalk-channel transport.ts 的 THINKING_EMOTION 一致。
	thinkingEmotion = "🤔思考中"
)

// emotionURL 贴/撤机器人消息表情的基址（reply=贴，recall=撤）。包级变量
// 以便测试覆盖为 httptest 桩地址。
var emotionURL = "https://api.dingtalk.com/v1.0/robot/emotion"

// adapter 钉钉适配器实现。
type adapter struct {
	cfg    config.DingtalkBotConfig
	logger *slog.Logger
	msgCh  chan bot.InboundMessage
	cancel context.CancelFunc

	// httpClient 复用连接，避免每消息重建。
	httpClient *http.Client
	// webhookHosts 与 allowHTTPWebhook 属于 adapter，避免测试修改包级安全策略。
	webhookHosts     []string
	allowHTTPWebhook bool
	// tokenMu 保护 token 缓存。
	tokenMu sync.Mutex
	token   string
	tokenAt time.Time

	// seenMu 保护消息去重。
	seenMu sync.Mutex
	seen   map[string]bool

	// conn 当前 WebSocket 连接。
	connMu sync.Mutex
	conn   *websocket.Conn

	// webhookMu 保护 chatID→webhook 映射（钉钉无全局发送 API，回复必须
	// POST 到会话 webhook；映射从入站消息学习，DSH 的 sessionWebhooks 模式）。
	webhookMu sync.Mutex
	webhooks  map[string]string
	// lastChatID 最近一次学到 webhook 的会话，供桌面端测试发送使用。
	lastChatID string

	// msgChats 记录 messageID→chatID，供 AddPendingReaction 贴/撤表情时
	// 还原会话（gateway 的 reaction 接口只传 messageID）。
	msgChatsMu sync.Mutex
	msgChats   map[string]string
}

// robotMessage 钉钉 Stream 模式机器人消息载荷（/v1.0/im/bot/messages/get）。
type robotMessage struct {
	SenderStaffID    string            `json:"senderStaffId"`
	SenderNick       string            `json:"senderNick"`
	ConversationID   string            `json:"conversationId"`
	ConversationType string            `json:"conversationType"` // "1"=单聊 "2"=群聊
	MsgID            string            `json:"msgId"`
	MsgType          string            `json:"msgtype"`
	Text             *robotTextContent `json:"text"`
	SessionWebhook   string            `json:"sessionWebhook"`
	// IsInAtList 官方回调的结构化 @ 标记：true 表示本消息 @ 了机器人。
	// 正文示例不保留前导 @token，须以该字段为准（见 dingtalk 开放平台文档）。
	IsInAtList bool `json:"isInAtList"`
}

// robotTextContent 钉钉文本消息内容。
type robotTextContent struct {
	Content string `json:"content"`
}

// New 创建钉钉适配器。
func New(cfg config.DingtalkBotConfig, logger *slog.Logger) bot.Adapter {
	return &adapter{
		cfg:          cfg,
		logger:       logger.With("platform", "dingtalk"),
		seen:         make(map[string]bool),
		webhooks:     make(map[string]string),
		msgChats:     make(map[string]string),
		httpClient:   &http.Client{Timeout: connTimeout},
		webhookHosts: append([]string(nil), dingtalkWebhookHosts...),
	}
}

func (a *adapter) Platform() bot.Platform { return bot.PlatformDingtalk }
func (a *adapter) Name() string           { return "dingtalk" }

func (a *adapter) Start(ctx context.Context) error {
	a.msgCh = make(chan bot.InboundMessage, 64)
	ctx, cancel := context.WithCancel(ctx)
	// 同步校验凭据与连接，失败直接报错而非标记 running 后由重连循环静默失败。
	checkCtx, checkCancel := context.WithTimeout(ctx, connTimeout)
	defer checkCancel()
	if id := a.clientID(); strings.TrimSpace(id) == "" {
		cancel()
		return fmt.Errorf("dingtalk client_id is not configured")
	}
	if secret := a.clientSecret(); strings.TrimSpace(secret) == "" {
		cancel()
		return fmt.Errorf("dingtalk client_secret is not configured")
	}
	if _, err := a.accessToken(checkCtx); err != nil {
		cancel()
		return fmt.Errorf("dingtalk credentials rejected: %w", err)
	}
	conn, err := a.dialConnection(checkCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("dingtalk connection failed: %w", err)
	}
	a.cancel = cancel
	a.setConn(conn)
	go a.runWithRetry(ctx, conn)
	return nil
}

func (a *adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	a.closeConn()
	return nil
}

func (a *adapter) Send(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	return a.sendMessage(ctx, msg)
}

func (a *adapter) SendTyping(ctx context.Context, chatID string) error {
	return nil
}

func (a *adapter) Messages() <-chan bot.InboundMessage {
	return a.msgCh
}

func (a *adapter) closeConn() {
	a.connMu.Lock()
	defer a.connMu.Unlock()
	if a.conn != nil {
		_ = a.conn.Close()
		a.conn = nil
	}
}

func (a *adapter) setConn(conn *websocket.Conn) {
	a.connMu.Lock()
	a.conn = conn
	a.connMu.Unlock()
}

func (a *adapter) releaseConn(conn *websocket.Conn) {
	a.connMu.Lock()
	if a.conn == conn {
		a.conn = nil
	}
	a.connMu.Unlock()
	_ = conn.Close()
}

// clientID 返回配置或环境变量中的 AppKey。
func (a *adapter) clientID() string {
	if v := strings.TrimSpace(a.cfg.ClientID); v != "" {
		return v
	}
	return os.Getenv(a.cfg.ClientIDEnv)
}

// clientSecret 返回配置或环境变量中的 AppSecret。
func (a *adapter) clientSecret() string {
	if v := strings.TrimSpace(a.cfg.ClientSecret); v != "" {
		return v
	}
	return os.Getenv(a.cfg.SecretEnv)
}

// accessToken 获取并缓存钉钉应用 access_token。
func (a *adapter) accessToken(ctx context.Context) (string, error) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()
	if a.token != "" && time.Since(a.tokenAt) < tokenTTL-60*time.Second {
		return a.token, nil
	}
	id := a.clientID()
	secret := a.clientSecret()
	if id == "" || secret == "" {
		return "", fmt.Errorf("dingtalk client_id or client_secret is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s?appkey=%s&appsecret=%s", getTokenURL, url.QueryEscape(id), url.QueryEscape(secret)), nil)
	if err != nil {
		return "", err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || body.AccessToken == "" {
		return "", fmt.Errorf("dingtalk gettoken failed: code=%d msg=%s", body.ErrCode, body.ErrMsg)
	}
	a.token = body.AccessToken
	a.tokenAt = time.Now()
	return a.token, nil
}

// gatewayEndpoint 换取 WebSocket 连接端点与 ticket。
type gatewayEndpoint struct {
	Endpoint string `json:"endpoint"`
	Ticket   string `json:"ticket"`
}

// openConnection 向网关换取 WebSocket 地址。
func (a *adapter) openConnection(ctx context.Context) (string, error) {
	payload := map[string]any{
		"clientId":     a.clientID(),
		"clientSecret": a.clientSecret(),
		"ua":           "reasonix",
		"subscriptions": []map[string]string{
			{"type": "CALLBACK", "topic": robotMessagesTopic},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageBytes))
	if err != nil {
		return "", err
	}
	var ep gatewayEndpoint
	if err := json.Unmarshal(body, &ep); err != nil {
		return "", fmt.Errorf("dingtalk open connection: bad response: %s", truncate(string(body), 200))
	}
	if ep.Endpoint == "" || ep.Ticket == "" {
		return "", fmt.Errorf("dingtalk open connection: endpoint/ticket empty: %s", truncate(string(body), 200))
	}
	return ep.Endpoint + "?ticket=" + url.QueryEscape(ep.Ticket), nil
}

// dialConnection 换取一次连接地址并完成 WebSocket 握手。
func (a *adapter) dialConnection(ctx context.Context) (*websocket.Conn, error) {
	wsURL, err := a.openConnection(ctx)
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: connTimeout}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	return conn, err
}

// runWithRetry 先处理 Start 已握手的连接，断线后带退避重连。
func (a *adapter) runWithRetry(ctx context.Context, conn *websocket.Conn) {
	backoff := time.Second
	for {
		if conn == nil {
			var err error
			conn, err = a.dialConnection(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				a.logger.Warn("dingtalk connection failed; reconnecting", "err", err, "backoff", backoff)
				if !waitForRetry(ctx, backoff) {
					return
				}
				if backoff < 30*time.Second {
					backoff *= 2
				}
				continue
			}
			if ctx.Err() != nil {
				_ = conn.Close()
				return
			}
			a.setConn(conn)
			backoff = time.Second
		}
		if err := a.serveConnection(ctx, conn); err != nil && ctx.Err() == nil {
			a.logger.Warn("dingtalk connection closed; reconnecting", "err", err, "backoff", backoff)
		}
		conn = nil
		if ctx.Err() != nil || !waitForRetry(ctx, backoff) {
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// serveConnection 处理一条已握手连接，直到断开或 ctx 取消。
func (a *adapter) serveConnection(ctx context.Context, conn *websocket.Conn) error {
	defer a.releaseConn(conn)
	a.logger.Info("dingtalk stream connected")

	// 读消息循环（处理 SYSTEM/CALLBACK）。
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, data, err := conn.ReadMessage(); err != nil {
				readErr <- err
				return
			} else {
				a.handleDownstream(ctx, conn, data)
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-readErr:
		return err
	}
}

// downstreamFrame 服务端推送帧。
type downstreamFrame struct {
	Type    string `json:"type"` // SYSTEM | CALLBACK | EVENT
	Headers struct {
		Topic     string `json:"topic"`
		MessageID string `json:"messageId"`
	} `json:"headers"`
	Data json.RawMessage `json:"data"`
}

// handleDownstream 处理一帧服务端消息。
func (a *adapter) handleDownstream(ctx context.Context, conn *websocket.Conn, data []byte) {
	var frame downstreamFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		a.logger.Warn("dingtalk bad frame", "err", err)
		return
	}
	switch frame.Type {
	case "SYSTEM":
		a.handleSystem(conn, frame)
	case "CALLBACK":
		a.handleCallback(ctx, conn, frame)
	}
}

// handleSystem 处理 SYSTEM 帧（ping 心跳回执、REGISTERED 等）。
func (a *adapter) handleSystem(conn *websocket.Conn, frame downstreamFrame) {
	switch frame.Headers.Topic {
	case "ping":
		// 服务端 ping 需原样回包。
		reply, _ := json.Marshal(map[string]any{
			"code":    200,
			"headers": frame.Headers,
			"message": "OK",
			"data":    frame.Data,
		})
		_ = conn.WriteMessage(websocket.TextMessage, reply)
	case "disconnect":
		a.logger.Warn("dingtalk server requested disconnect")
		_ = conn.Close()
	}
}

// handleCallback 处理机器人消息回调：归一化、去重、入队。
func (a *adapter) handleCallback(ctx context.Context, conn *websocket.Conn, frame downstreamFrame) {
	// 立即回执，避免服务端 60s 后重试。
	ack, _ := json.Marshal(map[string]any{
		"code": 200,
		"headers": map[string]string{
			"contentType": "application/json",
			"messageId":   frame.Headers.MessageID,
		},
		"message": "OK",
		"data":    `{"response":{"status":"SUCCESS"}}`,
	})
	_ = conn.WriteMessage(websocket.TextMessage, ack)

	if frame.Headers.Topic != robotMessagesTopic {
		return
	}
	raw, ok := decodeRobotMessage(frame.Data)
	if !ok {
		a.logger.Warn("dingtalk bad robot message", "err", "cannot decode payload")
		return
	}
	msg := a.normalizeMessage(raw)
	if msg == nil {
		return
	}
	select {
	case a.msgCh <- *msg:
	case <-ctx.Done():
	}
}

// decodeRobotMessage 解码 Stream 回调的机器人消息载荷。钉钉回调的 data 是
// JSON 编码的字符串：先解码出字符串再解析为消息对象（与 dsh-dingtalk-channel
// transport.ts 的 JSON.parse(res.data) 一致）；个别网关版本直接下发对象，
// 兼容之。
func decodeRobotMessage(data json.RawMessage) (robotMessage, bool) {
	var raw robotMessage
	var rawStr string
	if err := json.Unmarshal(data, &rawStr); err == nil {
		if err := json.Unmarshal([]byte(rawStr), &raw); err != nil {
			return robotMessage{}, false
		}
		return raw, true
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return robotMessage{}, false
	}
	return raw, true
}

// normalizeMessage 归一化钉钉机器人消息；不满足门控时返回 nil。
func (a *adapter) normalizeMessage(raw robotMessage) *bot.InboundMessage {
	messageID := strings.TrimSpace(raw.MsgID)
	chatID := strings.TrimSpace(raw.ConversationID)
	userID := strings.TrimSpace(raw.SenderStaffID)
	userName := strings.TrimSpace(raw.SenderNick)
	if userName == "" {
		userName = userID
	}
	if messageID == "" || chatID == "" {
		return nil
	}
	if a.markSeen(messageID) {
		return nil
	}
	isGroup := strings.TrimSpace(raw.ConversationType) == "2"
	text := ""
	if raw.Text != nil {
		text = strings.TrimSpace(raw.Text.Content)
	}
	// 群聊 @ 判断：以官方回调的结构化 isInAtList 为准（正文示例不保留
	// 前导 @token）；仅当回调未标记被 @ 时，才回退到文本前导 @ 解析。
	if isGroup {
		if a.cfg.RequireMention {
			mentioned := raw.IsInAtList
			if !mentioned {
				mentioned, text = a.splitMention(text)
			} else {
				// 结构化标记已确认被 @，仅剥离可能残留的前导 @token。
				text = a.stripGroupMention(text)
			}
			if !mentioned {
				a.logger.Info("dingtalk message ignored", "reason", "missing_mention", "chat", logHash(chatID), "message", logHash(messageID))
				return nil
			}
		} else {
			text = a.stripGroupMention(text)
		}
	}
	chatType := bot.ChatDM
	if isGroup {
		chatType = bot.ChatGroup
	}
	webhook := strings.TrimSpace(raw.SessionWebhook)
	if webhook != "" && a.validDingtalkWebhook(webhook) {
		// 记录会话 webhook，供后续回复查表使用；同时记录最近会话供测试发送。
		// 仅记录钉钉官方域名的 webhook，恶意/伪造回调无法注入任意回复目标。
		a.webhookMu.Lock()
		a.webhooks[chatID] = webhook
		a.lastChatID = chatID
		a.webhookMu.Unlock()
	} else if webhook != "" {
		// 非白名单 webhook 不记录、不透传，防止伪造回调注入任意回复目标。
		a.logger.Warn("dingtalk ignored non-dingtalk session webhook", "chat", logHash(chatID))
		webhook = ""
	}
	// 记录 messageID→chatID，供 AddPendingReaction 贴/撤表情时还原会话。
	a.msgChatsMu.Lock()
	a.msgChats[messageID] = chatID
	if len(a.msgChats) > 10000 {
		a.msgChats = make(map[string]string)
		a.msgChats[messageID] = chatID
	}
	a.msgChatsMu.Unlock()
	return &bot.InboundMessage{
		Platform:       bot.PlatformDingtalk,
		ChatType:       chatType,
		ChatID:         chatID,
		UserID:         userID,
		UserName:       userName,
		Text:           text,
		MessageID:      messageID,
		SessionWebhook: webhook,
	}
}

// webhookFor 返回某会话最新的回复 webhook；无记录时返回空串。
func (a *adapter) webhookFor(chatID string) string {
	a.webhookMu.Lock()
	defer a.webhookMu.Unlock()
	return a.webhooks[strings.TrimSpace(chatID)]
}

// chatForMessageID 返回消息所属会话；无记录时返回空串。
func (a *adapter) chatForMessageID(messageID string) string {
	a.msgChatsMu.Lock()
	defer a.msgChatsMu.Unlock()
	return a.msgChats[strings.TrimSpace(messageID)]
}

// AddPendingReaction 在收到的消息上贴 🤔思考中 表情（"已收到，处理中"），
// 返回的 cleanup 在回合结束后撤回表情。实现 gateway 的 pendingReactionAdapter
// 接口（DSH dsh-dingtalk-channel 的 addEmotion/recallEmotion）。
func (a *adapter) AddPendingReaction(ctx context.Context, messageID string) (func(), error) {
	messageID = strings.TrimSpace(messageID)
	chatID := a.chatForMessageID(messageID)
	if chatID == "" {
		return nil, fmt.Errorf("dingtalk emotion: unknown chat for message %s", logHash(messageID))
	}
	if err := a.setEmotion(ctx, chatID, messageID, "reply"); err != nil {
		return nil, err
	}
	return func() {
		// 撤回失败仅记录，不阻塞回合收尾。
		recallCtx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		if err := a.setEmotion(recallCtx, chatID, messageID, "recall"); err != nil {
			a.logger.Warn("dingtalk recall emotion failed", "chat", logHash(chatID), "message", logHash(messageID), "err", err)
		}
	}, nil
}

// emotionBody 构造贴/撤表情的请求体。robotCode 即应用 AppKey（client id）：
// 钉钉将机器人并入应用，client id 就是机器人 code（与 dsh transport.ts 一致）。
func emotionBody(robotCode, chatID, messageID, action string) map[string]any {
	body := map[string]any{
		"robotCode":          robotCode,
		"openMsgId":          messageID,
		"openConversationId": chatID,
		"emotionType":        2,
		"emotionName":        thinkingEmotion,
	}
	if action == "reply" {
		body["textEmotion"] = map[string]any{
			"emotionId":    "2659900",
			"emotionName":  thinkingEmotion,
			"text":         thinkingEmotion,
			"backgroundId": "im_bg_1",
		}
	}
	return body
}

// setEmotion 调钉钉表情接口：action 为 reply（贴）或 recall（撤）。
func (a *adapter) setEmotion(ctx context.Context, chatID, messageID, action string) error {
	robotCode := a.clientID()
	if robotCode == "" {
		return fmt.Errorf("dingtalk emotion: client_id is not configured")
	}
	token, err := a.accessToken(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(emotionBody(robotCode, chatID, messageID, action))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		emotionURL+"/"+action, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk emotion %s failed (%d): %s", action, resp.StatusCode, truncate(string(body), 300))
	}
	return nil
}

// stripGroupMention 剥离开头的 @xxx 词元。
func (a *adapter) stripGroupMention(text string) string {
	mentionsBot, rest := a.splitMention(text)
	if mentionsBot {
		return rest
	}
	return text
}

// splitMention 检查文本是否以 @机器人 开头；是则返回 (true, 剥离后的文本)。
func (a *adapter) splitMention(text string) (bool, string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "@") {
		return false, text
	}
	rest := text[1:]
	end := strings.IndexAny(rest, " \t\n")
	name := rest
	if end >= 0 {
		name = rest[:end]
	}
	botName := strings.TrimSpace(a.cfg.BotName)
	if botName == "" {
		// 未配置昵称：任意 @ 开头都视为 @ 机器人（与飞书旧行为一致）。
		if end < 0 {
			return true, ""
		}
		return true, strings.TrimSpace(rest[end:])
	}
	if name != botName {
		return false, text
	}
	if end < 0 {
		return true, ""
	}
	return true, strings.TrimSpace(rest[end:])
}

// markSeen 消息去重；返回 true 表示已见过。
func (a *adapter) markSeen(messageID string) bool {
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	if a.seen[messageID] {
		return true
	}
	if len(a.seen) > 10000 {
		a.seen = make(map[string]bool)
	}
	a.seen[messageID] = true
	return false
}

// sendMessage 发送出站消息（文本或 markdown）。
// 钉钉无全局发送 API：回复必须 POST 到会话 webhook。webhook 来源：
// 1) msg.SessionWebhook（入站消息透传，含持久化恢复场景）
// 2) chatID→webhook 映射表（入站消息学习而来）
// 两者皆无时拒绝并给出可读错误。
// 注意：gateway 的 sendText 会把 ReplyToMsgID 填成入站消息 ID（非 URL），
// 这里完全不把 ReplyToMsgID 当 URL 使用——回复目标只来自钉钉官方回调携带
// 的 sessionWebhook。所有 webhook 必须通过 validDingtalkWebhook 校验
// （仅钉钉域名），且回复 POST 不携带 access token（官方文档：会话 webhook
// 无需额外认证），避免向任意 URL 泄漏 token 或形成 SSRF。
func (a *adapter) sendMessage(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	webhook := strings.TrimSpace(msg.SessionWebhook)
	if webhook != "" && !a.validDingtalkWebhook(webhook) {
		return bot.SendResult{}, fmt.Errorf("dingtalk send rejected: session webhook %q is not a dingtalk endpoint", truncate(webhook, 64))
	}
	if webhook == "" {
		webhook = a.webhookFor(msg.ChatID)
	}
	if webhook == "" {
		return bot.SendResult{}, fmt.Errorf("dingtalk send requires a session webhook: no webhook known for chat %s (bot must receive a message in this chat first)", logHash(msg.ChatID))
	}
	if !a.validDingtalkWebhook(webhook) {
		return bot.SendResult{}, fmt.Errorf("dingtalk send rejected: webhook %q is not a dingtalk endpoint", truncate(webhook, 64))
	}
	// 一律以 markdown 类型发送：Reasonix bot 回复是 markdown 文本，钉钉
	// text 类型按纯文本显示、不渲染语法（与飞书 buildMarkdownCard 一致）。
	title := "Reasonix"
	if msg.Card != nil && strings.TrimSpace(msg.Card.Header) != "" {
		title = msg.Card.Header
	}
	payload := map[string]any{"msgtype": "markdown", "markdown": map[string]string{
		"title": title,
		"text":  msg.Text,
	}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return bot.SendResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(raw))
	if err != nil {
		return bot.SendResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.webhookClient().Do(req)
	if err != nil {
		return bot.SendResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return bot.SendResult{}, fmt.Errorf("dingtalk send failed (%d): %s", resp.StatusCode, truncate(string(body), 300))
	}
	return bot.SendResult{MessageID: ""}, nil
}

// TestSend 向最近一个交互过的会话发送测试消息，验证凭据与发送链路。
// 钉钉无全局发送 API，必须先收到过该会话的消息（学到 webhook）才能回复。
func (a *adapter) TestSend(ctx context.Context, text string) (bot.SendResult, error) {
	a.webhookMu.Lock()
	chatID := a.lastChatID
	a.webhookMu.Unlock()
	if chatID == "" {
		return bot.SendResult{}, fmt.Errorf("dingtalk test send requires a known chat: the bot must have received a message first")
	}
	return a.sendMessage(ctx, bot.OutboundMessage{ChatID: chatID, ChatType: bot.ChatDM, Text: text})
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// logHash 对 chat/消息 ID 做脱敏哈希，用于日志（不泄漏完整 ID）。
func logHash(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:12]
}

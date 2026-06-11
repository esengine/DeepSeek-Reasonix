// Package feishu 实现飞书自建应用 Bot 适配器。
// 参考 Hermes Agent 的 feishu adapter：
// - 长连接 WebSocket（默认）或 Webhook 模式
// - @mention gating
// - open_id / user_id / union_id 映射
// - 消息去重
// - interactive card 审批/问答
package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larknormalize "github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// textContent 飞书消息文本内容结构。
type textContent struct {
	Text string `json:"text"`
}

const feishuPendingReactionEmoji = "OnIt"

// feishuEvent 飞书事件结构。
type feishuEvent struct {
	Schema string          `json:"schema"`
	Header feishuHeader    `json:"header"`
	Event  json.RawMessage `json:"event"`
}

type feishuHeader struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	Token      string `json:"token"`
	CreateTime string `json:"create_time"`
}

type feishuMsgEvent struct {
	MessageID string          `json:"message_id"`
	RootID    string          `json:"root_id"`
	ParentID  string          `json:"parent_id"`
	ChatID    string          `json:"chat_id"`
	ChatType  string          `json:"chat_type"`
	MsgType   string          `json:"msg_type"`
	Content   string          `json:"content"`
	Sender    feishuSender    `json:"sender"`
	Mentions  []feishuMention `json:"mentions"`
}

type feishuSender struct {
	SenderID struct {
		UserID  string `json:"user_id"`
		OpenID  string `json:"open_id"`
		UnionID string `json:"union_id"`
	} `json:"sender_id"`
}

type feishuMention struct {
	Key string `json:"key"`
	ID  struct {
		OpenID string `json:"open_id"`
	} `json:"id"`
}

// adapter 飞书适配器实现。
type adapter struct {
	cfg      config.FeishuBotConfig
	logger   *slog.Logger
	msgCh    chan bot.InboundMessage
	cancel   context.CancelFunc
	client   *lark.Client
	wsClient *larkws.Client

	seenMu sync.Mutex
	seen   map[string]bool // 消息去重
}

// New 创建飞书 Bot 适配器。
func New(cfg config.FeishuBotConfig, logger *slog.Logger) bot.Adapter {
	return &adapter{
		cfg:    cfg,
		logger: logger.With("platform", "feishu"),
		seen:   make(map[string]bool),
	}
}

func (a *adapter) Platform() bot.Platform { return bot.PlatformFeishu }
func (a *adapter) Name() string           { return "feishu" }

func (a *adapter) Start(ctx context.Context) error {
	a.msgCh = make(chan bot.InboundMessage, 64)
	ctx, a.cancel = context.WithCancel(ctx)

	mode := a.cfg.Mode
	if mode == "" {
		mode = "webhook"
	}

	switch mode {
	case "webhook":
		// Webhook mode exposes a public HTTP endpoint; without a verification
		// token verificationTokenValid accepts every caller, so fail closed
		// rather than let anyone drive the agent.
		if strings.TrimSpace(a.cfg.VerificationToken) == "" {
			return fmt.Errorf("feishu: webhook mode needs verification_token set — refusing to expose an unauthenticated event endpoint")
		}
		go a.runWebhook(ctx)
	default:
		go a.runWebSocket(ctx)
	}
	return nil
}

func (a *adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.wsClient != nil {
		a.wsClient.Close()
	}
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

func (a *adapter) appSecret() (string, error) {
	secret := os.Getenv(a.cfg.AppSecretEnv)
	if a.cfg.AppID == "" || secret == "" {
		return "", fmt.Errorf("feishu app_id or %s is not configured", a.cfg.AppSecretEnv)
	}
	return secret, nil
}

// runWebSocket 启动飞书 WebSocket 长连接。
func (a *adapter) runWebSocket(ctx context.Context) {
	secret, err := a.appSecret()
	if err != nil {
		a.logger.Error("feishu websocket config error", "err", err)
		return
	}
	eventHandler := dispatcher.NewEventDispatcher(a.cfg.VerificationToken, "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			a.handleSDKMessage(event)
			return nil
		}).
		OnP2MessageReadV1(func(ctx context.Context, event *larkim.P2MessageReadV1) error {
			return nil
		})
	opts := []larkws.ClientOption{
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelError),
		larkws.WithAutoReconnect(true),
		larkws.WithOnReady(func() { a.logger.Info("feishu sdk websocket connected") }),
		larkws.WithOnReconnecting(func() { a.logger.Warn("feishu sdk websocket reconnecting") }),
		larkws.WithOnReconnected(func() { a.logger.Info("feishu sdk websocket reconnected") }),
		larkws.WithOnError(func(err error) { a.logger.Error("feishu sdk websocket error", "err", err) }),
	}
	if feishuDomain(a.cfg.Domain) == "lark" {
		opts = append(opts, larkws.WithDomain(lark.LarkBaseUrl))
	}
	client := larkws.NewClient(a.cfg.AppID, secret, opts...)
	a.wsClient = client
	errCh := make(chan error, 1)
	go func() { errCh <- client.Start(ctx) }()
	select {
	case <-ctx.Done():
		client.Close()
	case err := <-errCh:
		if err != nil {
			a.logger.Error("feishu sdk websocket stopped", "err", err)
		}
	}
}

func (a *adapter) handleSDKMessage(event *larkim.P2MessageReceiveV1) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	eventID := ""
	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		eventID = event.EventV2Base.Header.EventID
	}
	if eventID != "" {
		if a.markSeen(eventID) {
			return
		}
	}
	msg := event.Event.Message
	if stringPtrValue(msg.MessageType) != "text" {
		return
	}
	var content textContent
	if err := json.Unmarshal([]byte(stringPtrValue(msg.Content)), &content); err != nil {
		return
	}
	chatType := bot.ChatDM
	if stringPtrValue(msg.ChatType) == "group" || stringPtrValue(msg.ChatType) == "topic_group" {
		chatType = bot.ChatGroup
		if a.cfg.RequireMention && len(msg.Mentions) == 0 {
			return
		}
	}
	userID := ""
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		userID = firstNonEmpty(
			stringPtrValue(event.Event.Sender.SenderId.OpenId),
			stringPtrValue(event.Event.Sender.SenderId.UnionId),
			stringPtrValue(event.Event.Sender.SenderId.UserId),
		)
	}
	ib := bot.InboundMessage{
		Platform:  bot.PlatformFeishu,
		ChatType:  chatType,
		ChatID:    stringPtrValue(msg.ChatId),
		UserID:    userID,
		UserName:  userID,
		Text:      content.Text,
		MessageID: stringPtrValue(msg.MessageId),
		ThreadID:  stringPtrValue(msg.ThreadId),
		Raw:       event,
	}
	select {
	case a.msgCh <- ib:
	default:
		a.logger.Warn("feishu message channel full")
	}
}

func (a *adapter) handleWSEvent(ctx context.Context, raw json.RawMessage) {
	var evt feishuEvent
	if err := json.Unmarshal(raw, &evt); err != nil {
		return
	}

	if a.markSeen(evt.Header.EventID) {
		return
	}

	switch evt.Header.EventType {
	case "im.message.receive_v1":
		var msg feishuMsgEvent
		if err := json.Unmarshal(evt.Event, &msg); err != nil {
			return
		}
		a.handleMessage(msg)
	}
}

func (a *adapter) handleCardAction(raw []byte) bool {
	var payload struct {
		Header feishuHeader `json:"header"`
		Event  struct {
			Operator struct {
				OperatorID struct {
					UserID  string `json:"user_id"`
					OpenID  string `json:"open_id"`
					UnionID string `json:"union_id"`
				} `json:"operator_id"`
			} `json:"operator"`
			Context struct {
				OpenMessageID string `json:"open_message_id"`
				OpenChatID    string `json:"open_chat_id"`
			} `json:"context"`
			Action struct {
				Value map[string]string `json:"value"`
			} `json:"action"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	command := payload.Event.Action.Value["command"]
	if command == "" || payload.Event.Context.OpenChatID == "" {
		return false
	}
	chatType := cardActionChatType(payload.Event.Action.Value["chat_type"])
	userID := firstNonEmpty(payload.Event.Operator.OperatorID.UnionID, payload.Event.Operator.OperatorID.OpenID, payload.Event.Operator.OperatorID.UserID)
	ib := bot.InboundMessage{
		Platform:  bot.PlatformFeishu,
		ChatType:  chatType,
		ChatID:    payload.Event.Context.OpenChatID,
		UserID:    userID,
		UserName:  userID,
		Text:      command,
		MessageID: payload.Event.Context.OpenMessageID,
	}
	select {
	case a.msgCh <- ib:
	default:
		a.logger.Warn("feishu card action channel full")
	}
	return true
}

func (a *adapter) markSeen(eventID string) bool {
	if eventID == "" {
		return false
	}
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	if a.seen == nil {
		a.seen = make(map[string]bool)
	}
	if a.seen[eventID] {
		return true
	}
	a.seen[eventID] = true
	if len(a.seen) > 10000 {
		a.seen = make(map[string]bool)
		a.seen[eventID] = true
	}
	return false
}

func cardActionChatType(raw string) bot.ChatType {
	switch bot.ChatType(raw) {
	case bot.ChatDM, bot.ChatGroup, bot.ChatGuild, bot.ChatDirect, bot.ChatThread:
		return bot.ChatType(raw)
	default:
		return bot.ChatGroup
	}
}

func (a *adapter) verificationTokenValid(token string) bool {
	return a.cfg.VerificationToken == "" || token == a.cfg.VerificationToken
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (a *adapter) handleMessage(msg feishuMsgEvent) {
	if msg.MsgType != "text" {
		return
	}

	// 解析文本内容
	var content textContent
	if err := json.Unmarshal([]byte(msg.Content), &content); err != nil {
		return
	}

	// @mention gating：仅在群聊中检查是否 @了 bot
	chatType := bot.ChatDM
	if msg.ChatType == "group" {
		chatType = bot.ChatGroup
		if a.cfg.RequireMention && len(msg.Mentions) == 0 {
			return
		}
	}

	ib := bot.InboundMessage{
		Platform:  bot.PlatformFeishu,
		ChatType:  chatType,
		ChatID:    msg.ChatID,
		UserID:    msg.Sender.SenderID.OpenID,
		UserName:  "",
		Text:      content.Text,
		MessageID: msg.MessageID,
	}

	// 获取用户信息填充用户名
	if msg.Sender.SenderID.OpenID != "" {
		ib.UserName = msg.Sender.SenderID.OpenID
	}

	select {
	case a.msgCh <- ib:
	default:
		a.logger.Warn("feishu message channel full")
	}
}

// SendText sends one markdown-rendered message to a Feishu/Lark chat_id using the SDK.
// It is used by the desktop settings panel as an actual connection test.
func SendText(ctx context.Context, cfg config.FeishuBotConfig, chatID, text string) (bot.SendResult, error) {
	a := &adapter{cfg: cfg, logger: slog.Default().With("platform", "feishu")}
	return a.sendMessage(ctx, bot.OutboundMessage{ChatID: chatID, Text: text})
}

// sendMessage 使用飞书/Lark SDK 回复或主动发送消息。
func (a *adapter) sendMessage(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	if msg.Card != nil {
		return a.sendCard(ctx, msg)
	}
	content, err := feishuMarkdownPostContent(msg.Text)
	if err != nil {
		a.logger.Warn("format feishu markdown failed, falling back to text", "err", err)
		content = feishuTextContent(msg.Text)
		return a.sendSDKContent(ctx, msg, larkim.MsgTypeText, content)
	}
	return a.sendSDKContent(ctx, msg, larkim.MsgTypePost, content)
}

func feishuMarkdownPostContent(text string) (string, error) {
	return larknormalize.SimpleMarkdownToPost("", text, nil)
}

func feishuTextContent(text string) string {
	content, _ := json.Marshal(textContent{Text: text})
	return string(content)
}

func (a *adapter) sdkClient() (*lark.Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	secret, err := a.appSecret()
	if err != nil {
		return nil, err
	}
	opts := []lark.ClientOptionFunc{
		lark.WithLogLevel(larkcore.LogLevelError),
		lark.WithReqTimeout(15 * time.Second),
		lark.WithSource("reasonix"),
	}
	if feishuDomain(a.cfg.Domain) == "lark" {
		opts = append(opts, lark.WithOpenBaseUrl(lark.LarkBaseUrl), lark.WithOAuthBaseUrl(lark.OAuthBaseUrlLark))
	}
	a.client = lark.NewClient(a.cfg.AppID, secret, opts...)
	return a.client, nil
}

func (a *adapter) sendSDKContent(ctx context.Context, msg bot.OutboundMessage, msgType, content string) (bot.SendResult, error) {
	client, err := a.sdkClient()
	if err != nil {
		return bot.SendResult{}, err
	}
	if msg.ReplyToMsgID != "" {
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(msg.ReplyToMsgID).
			Body(larkim.NewReplyMessageReqBodyBuilder().MsgType(msgType).Content(content).Build()).
			Build()
		resp, err := client.Im.Message.Reply(ctx, req)
		if err != nil {
			return bot.SendResult{}, err
		}
		if resp == nil {
			return bot.SendResult{}, fmt.Errorf("feishu reply error: empty response")
		}
		if !resp.Success() {
			return bot.SendResult{}, fmt.Errorf("feishu reply error: %s", feishuCodeError(resp.Code, resp.Msg))
		}
		if resp.Data == nil {
			return bot.SendResult{}, nil
		}
		return bot.SendResult{MessageID: stringPtrValue(resp.Data.MessageId)}, nil
	}

	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		return bot.SendResult{}, fmt.Errorf("feishu chat_id is empty")
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.CreateMessageV1ReceiveIDTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(chatID).MsgType(msgType).Content(content).Build()).
		Build()
	resp, err := client.Im.Message.Create(ctx, req)
	if err != nil {
		return bot.SendResult{}, err
	}
	if resp == nil {
		return bot.SendResult{}, fmt.Errorf("feishu send error: empty response")
	}
	if !resp.Success() {
		return bot.SendResult{}, fmt.Errorf("feishu send error: %s", feishuCodeError(resp.Code, resp.Msg))
	}
	if resp.Data == nil {
		return bot.SendResult{}, nil
	}
	return bot.SendResult{MessageID: stringPtrValue(resp.Data.MessageId)}, nil
}

func (a *adapter) AddPendingReaction(ctx context.Context, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	client, err := a.sdkClient()
	if err != nil {
		return err
	}
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(feishuPendingReactionEmoji).Build()).
			Build()).
		Build()
	resp, err := client.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("feishu reaction error: empty response")
	}
	if !resp.Success() {
		return fmt.Errorf("feishu reaction error: %s", feishuCodeError(resp.Code, resp.Msg))
	}
	return nil
}

// sendCard 发送 interactive card 消息（用于审批/问答）。
func (a *adapter) sendCard(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	card := msg.Card

	elements := make([]map[string]interface{}, 0)
	for _, el := range card.Elements {
		item := map[string]interface{}{"tag": el.Tag}
		if el.Content != "" {
			item["content"] = el.Content
		}
		if actions, ok := el.Extra["actions"]; ok && el.Tag == "action" {
			item["actions"] = actions
		} else {
			for k, v := range el.Extra {
				item[k] = v
			}
		}
		elements = append(elements, item)
	}

	cardPayload := map[string]interface{}{
		"header": map[string]interface{}{
			"title": map[string]string{
				"tag":     "plain_text",
				"content": card.Header,
			},
		},
		"elements": elements,
	}

	cardJSON, _ := json.Marshal(cardPayload)
	return a.sendSDKContent(ctx, msg, larkim.MsgTypeInteractive, string(cardJSON))
}

func feishuDomain(domain string) string {
	if strings.EqualFold(strings.TrimSpace(domain), "lark") {
		return "lark"
	}
	return "feishu"
}

func stringPtrValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return strings.TrimSpace(*ptr)
}

func feishuCodeError(code int, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "unknown error"
	}
	if code == 0 {
		return msg
	}
	return fmt.Sprintf("%s (code %d)", msg, code)
}

// runWebhook 启动飞书 Webhook 模式。
func (a *adapter) runWebhook(ctx context.Context) {
	port := a.cfg.WebhookPort
	if port == 0 {
		port = 8080
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/feishu/event", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
		if err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		var challenge struct {
			Challenge string `json:"challenge"`
			Token     string `json:"token"`
			Type      string `json:"type"`
		}
		_ = json.Unmarshal(body, &challenge)
		if challenge.Type == "url_verification" {
			if !a.verificationTokenValid(challenge.Token) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{"challenge": challenge.Challenge}); err != nil {
				a.logger.Error("feishu challenge response error", "err", err)
			}
			return
		}

		var evt feishuEvent
		if err := json.Unmarshal(body, &evt); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if !a.verificationTokenValid(evt.Header.Token) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if !a.handleCardAction(body) {
			raw, _ := json.Marshal(evt)
			a.handleWSEvent(ctx, raw)
		}
		w.WriteHeader(200)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		if err := server.Shutdown(context.Background()); err != nil && err != http.ErrServerClosed {
			a.logger.Error("feishu webhook shutdown error", "err", err)
		}
	}()

	a.logger.Info("feishu webhook listening", "port", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		a.logger.Error("feishu webhook server error", "err", err)
	}
}

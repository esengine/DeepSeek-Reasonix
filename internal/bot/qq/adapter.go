// Package qq 实现 QQ 官方 Bot API v2 适配器。
// 参考 Hermes Agent 的 qqbot adapter 实现：
// - app token 获取与刷新
// - WebSocket gateway 连接、heartbeat、resume
// - REST API 回复消息
// - C2C / group / guild / direct message 支持
// - inline keyboard 审批
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
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"

	"github.com/gorilla/websocket"
)

// New 创建 QQ Bot 适配器。
func New(cfg config.QQBotConfig, logger *slog.Logger) bot.Adapter {
	return &adapter{
		cfg:    cfg,
		logger: logger.With("platform", "qq"),
	}
}

// VerifyConnection performs the same Token -> Gateway -> Identify -> READY
// path as the runtime and returns only after the provider reports READY.
func VerifyConnection(ctx context.Context, cfg config.QQBotConfig) (GatewayStatusSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a := New(cfg, slog.Default()).(*adapter)
	if err := a.Start(ctx); err != nil {
		return a.GatewayStatus(), err
	}
	defer func() { _ = a.Stop() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status := a.GatewayStatus()
		if status.Ready && status.Phase == GatewayPhaseReady {
			return status, nil
		}
		if status.Phase == GatewayPhaseFatal {
			return status, fmt.Errorf("qq connection verification failed: %s", status.LastError)
		}
		select {
		case <-ctx.Done():
			status = a.GatewayStatus()
			if status.LastError != "" {
				return status, fmt.Errorf("qq connection verification timed out: %s", status.LastError)
			}
			return status, fmt.Errorf("qq connection verification timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// SendText performs a one-shot official QQ REST send for diagnostics and
// operator tests without starting a second Gateway connection.
func SendText(ctx context.Context, cfg config.QQBotConfig, chatID, text string) (bot.SendResult, error) {
	a := &adapter{cfg: cfg, logger: slog.Default().With("platform", "qq")}
	return a.sendMessage(ctx, bot.OutboundMessage{ChatType: bot.ChatDM, ChatID: strings.TrimSpace(chatID), Text: text})
}

type adapter struct {
	cfg           config.QQBotConfig
	logger        *slog.Logger
	msgCh         chan bot.InboundMessage
	interactionCh chan bot.Interaction
	cancel        context.CancelFunc
	loopWG        sync.WaitGroup

	// gateway 状态
	connMu      sync.Mutex
	conn        *websocket.Conn // live gateway connection, closed by Stop to unblock reads
	sessionID   string
	seq         int64
	token       string
	tokenExpiry time.Time
	tokenMu     sync.Mutex

	sendMu             sync.Mutex
	nextOutboundMsgSeq int
	markdownDisabled   bool
	mediaMu            sync.Mutex
	mediaCache         map[string]qqMediaCacheEntry

	statusMu sync.RWMutex
	status   GatewayStatusSnapshot
}

func (a *adapter) Platform() bot.Platform { return bot.PlatformQQ }
func (a *adapter) Name() string           { return "qq" }

func (a *adapter) Capabilities() bot.AdapterCapabilities {
	return bot.AdapterCapabilities{
		Typing: true, NativeStreaming: a.cfg.NativeStreaming, Media: true, Keyboard: true, ProactiveSend: true,
	}
}

func (a *adapter) Start(ctx context.Context) error {
	a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseConfigured})
	a.msgCh = make(chan bot.InboundMessage, 64)
	a.interactionCh = make(chan bot.Interaction, 64)
	// Reject static configuration mistakes synchronously. Network and provider
	// failures are intentionally left to gatewayLoop so they receive retries.
	if strings.TrimSpace(a.appID()) == "" {
		err := fmt.Errorf("qq app_id is empty")
		a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseFatal, LastError: err.Error()})
		return err
	}
	if strings.TrimSpace(a.appSecret()) == "" {
		err := fmt.Errorf("qq app secret is empty: set the %s environment variable", a.appSecretEnvName())
		a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseFatal, LastError: err.Error()})
		return err
	}
	ctx, a.cancel = context.WithCancel(ctx)

	a.loopWG.Go(func() {
		a.gatewayLoop(ctx)
	})
	return nil
}

// Stop 取消 gateway context、关闭当前 WebSocket 连接并等待 gatewayLoop 退出。
// websocket 的阻塞读不响应 context，只有关闭连接才能解除阻塞；不等待就返回
// 会在宿主重建 bot runtime 后留下仍占用 QQ gateway session 的僵尸连接。
func (a *adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	a.closeConn()
	a.loopWG.Wait()
	a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseStopped})
	return nil
}

func (a *adapter) Send(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	return a.sendMessage(ctx, msg)
}

func (a *adapter) SendTyping(ctx context.Context, chatID string) error {
	return a.SendTypingMessage(ctx, bot.OutboundMessage{ChatType: bot.ChatDM, ChatID: strings.TrimSpace(chatID)})
}

func (a *adapter) SendTypingMessage(ctx context.Context, msg bot.OutboundMessage) error {
	if strings.TrimSpace(msg.ChatID) == "" || msg.ChatType != bot.ChatDM {
		return nil
	}
	_, err := a.sendMessagePayload(ctx, msg, map[string]any{
		"msg_type": 6,
		"input_notify": map[string]any{
			"input_type":   1,
			"input_second": 10,
		},
	}, a.nextMessageSeq(""))
	return err
}

// OpenStream implements QQ's C2C native full-text replacement stream. QQ does
// not expose this protocol for group or guild messages; those callers use the
// ordinary final-message path.
func (a *adapter) OpenStream(ctx context.Context, msg bot.OutboundMessage) (bot.OutboundStream, error) {
	if !a.cfg.NativeStreaming || msg.ChatType != bot.ChatDM || strings.TrimSpace(msg.ChatID) == "" || strings.TrimSpace(msg.ReplyToMsgID) == "" {
		return nil, fmt.Errorf("qq native streaming requires a C2C reply context")
	}
	return &qqStream{adapter: a, msg: msg, seq: a.nextMessageSeq(msg.ReplyToMsgID)}, nil
}

type qqStream struct {
	adapter  *adapter
	msg      bot.OutboundMessage
	seq      int
	index    int
	streamID string
	lastText string
	mu       sync.Mutex
}

type qqMediaCacheEntry struct {
	FileInfo  string
	ExpiresAt time.Time
}

func (s *qqStream) Update(ctx context.Context, text string) (bot.SendResult, error) {
	return s.send(ctx, text, 1)
}

func (s *qqStream) Complete(ctx context.Context, text string) (bot.SendResult, error) {
	return s.send(ctx, text, 10)
}

func (s *qqStream) Abort(context.Context) error { return nil }

func (s *qqStream) send(ctx context.Context, text string, state int) (bot.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text = normalizeQQMarkdownReply(text)
	if text == s.lastText && state == 1 {
		return bot.SendResult{}, nil
	}
	payload := map[string]any{
		"input_mode":   "replace",
		"input_state":  state,
		"content_type": "markdown",
		"content_raw":  text,
		"event_id":     s.msg.ReplyToMsgID,
		"msg_id":       s.msg.ReplyToMsgID,
		"msg_seq":      s.seq,
		"index":        s.index,
	}
	if s.streamID != "" {
		payload["stream_msg_id"] = s.streamID
	}
	urlValue := fmt.Sprintf("%s/v2/users/%s/stream_messages", s.adapter.apiBaseURL(), url.PathEscape(s.msg.ChatID))
	result, err := s.adapter.postJSON(ctx, urlValue, payload)
	if err != nil {
		return bot.SendResult{}, err
	}
	var response struct {
		ID        string `json:"id"`
		MessageID string `json:"message_id"`
	}
	_ = json.Unmarshal(result, &response)
	if response.ID == "" {
		response.ID = response.MessageID
	}
	if s.streamID == "" {
		s.streamID = response.ID
	}
	s.lastText = text
	s.index++
	return bot.SendResult{MessageID: response.ID}, nil
}

func (a *adapter) Messages() <-chan bot.InboundMessage {
	return a.msgCh
}

func (a *adapter) Interactions() <-chan bot.Interaction { return a.interactionCh }

func (a *adapter) AckIngress(_ context.Context, eventID string) error {
	return removeQQGatewayRawEvent(a.appID(), a.cfg.Sandbox, eventID)
}

func (a *adapter) RetryIngress(_ context.Context, _ string) error {
	a.closeConn()
	return nil
}

func (a *adapter) AckInteraction(ctx context.Context, interactionID string) error {
	return a.ackInteraction(ctx, interactionID)
}

func (a *adapter) postJSON(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Union-Appid", a.appID())
	resp, err := qqHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("qq api error %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// GatewayStatus exposes provider lifecycle without widening bot.Adapter for
// every existing adapter. The desktop runtime can therefore distinguish
// "configured" from a gateway that actually reached READY/RESUMED.
func (a *adapter) GatewayStatus() GatewayStatusSnapshot {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

func (a *adapter) Lifecycle() bot.AdapterLifecycleSnapshot {
	s := a.GatewayStatus()
	return bot.AdapterLifecycleSnapshot{
		Phase: string(s.Phase), Ready: s.Ready, LastError: s.LastError,
		LastErrorCode: s.LastErrorCode, RetryAt: s.RetryAt,
		LastReadyAt: s.LastReadyAt, LastMessageAt: s.LastMessageAt,
	}
}

func (a *adapter) setStatus(status GatewayStatusSnapshot) {
	a.statusMu.Lock()
	if status.LastReadyAt.IsZero() {
		status.LastReadyAt = a.status.LastReadyAt
	}
	if status.LastMessageAt.IsZero() {
		status.LastMessageAt = a.status.LastMessageAt
	}
	a.status = status
	a.statusMu.Unlock()
}

func (a *adapter) markMessage() {
	a.statusMu.Lock()
	a.status.LastMessageAt = time.Now()
	a.statusMu.Unlock()
}

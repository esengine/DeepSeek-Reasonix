// Package onebot implements the OneBot v11 WebSocket transport. It is kept as
// a protocol adapter (rather than a NapCat adapter) so other OneBot v11
// implementations can be used without changing Reasonix business logic.
package onebot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"

	"golang.org/x/net/websocket"
)

const startupTimeout = 10 * time.Second
const maxOneBotInlineMediaBytes = 25 * 1024 * 1024

type Config struct {
	WebSocketURL   string
	Token          string
	SelfID         string
	RequireMention bool
}

func New(cfg Config, logger *slog.Logger) bot.Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &adapter{cfg: cfg, logger: logger.With("platform", "qq", "protocol", "onebot-v11")}
}

// FromConnection translates the generic desktop connection record while
// keeping the token out of the TOML model.
func FromConnection(conn config.BotConnectionConfig) (Config, error) {
	urlValue := strings.TrimSpace(conn.OneBot.WebSocketURL)
	if urlValue == "" {
		return Config{}, fmt.Errorf("onebot websocket_url is empty")
	}
	tokenEnv := strings.TrimSpace(conn.OneBot.TokenEnv)
	if tokenEnv == "" {
		tokenEnv = strings.TrimSpace(conn.Credential.TokenEnv)
	}
	if tokenEnv == "" {
		tokenEnv = "QQ_ONEBOT_TOKEN"
	}
	return Config{WebSocketURL: urlValue, Token: strings.TrimSpace(os.Getenv(tokenEnv)), SelfID: strings.TrimSpace(conn.OneBot.SelfID), RequireMention: true}, nil
}

type adapter struct {
	cfg    Config
	logger *slog.Logger

	msgCh      chan bot.InboundMessage
	cancel     context.CancelFunc
	loopWG     sync.WaitGroup
	connMu     sync.Mutex
	conn       *websocket.Conn
	writeMu    sync.Mutex
	pendingMu  sync.Mutex
	pending    map[string]chan oneBotResponse
	echoSeq    atomic.Uint64
	identityMu sync.RWMutex
	selfID     string
	statusMu   sync.RWMutex
	status     bot.AdapterLifecycleSnapshot
}

func (a *adapter) Platform() bot.Platform { return bot.PlatformQQ }
func (a *adapter) Name() string           { return "onebot-v11" }

func (a *adapter) Capabilities() bot.AdapterCapabilities {
	return bot.AdapterCapabilities{Media: true, ProactiveSend: true}
}

func (a *adapter) Start(ctx context.Context) error {
	a.setStatus(bot.AdapterLifecycleSnapshot{Phase: "connecting", LastError: ""})
	if err := validateEndpoint(a.cfg.WebSocketURL, a.cfg.Token); err != nil {
		a.setStatus(bot.AdapterLifecycleSnapshot{Phase: "fatal", LastError: err.Error()})
		return err
	}
	a.msgCh = make(chan bot.InboundMessage, 64)
	runCtx, runCancel := context.WithCancel(ctx)
	a.cancel = runCancel
	a.pendingMu.Lock()
	a.pending = make(map[string]chan oneBotResponse)
	a.pendingMu.Unlock()
	a.loopWG.Go(func() { a.runLoop(runCtx) })
	return nil
}

func (a *adapter) runLoop(ctx context.Context) {
	backoff := []time.Duration{1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}
	attempt := 0
	for ctx.Err() == nil {
		a.setStatus(bot.AdapterLifecycleSnapshot{Phase: "connecting", Ready: false, LastError: ""})
		err := a.connectAndServe(ctx)
		if ctx.Err() != nil {
			return
		}
		attempt++
		delay := backoff[len(backoff)-1]
		if attempt <= len(backoff) {
			delay = backoff[attempt-1]
		}
		a.setStatus(bot.AdapterLifecycleSnapshot{Phase: "backoff", Ready: false, LastError: errString(err), RetryAt: time.Now().Add(delay)})
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		}
	}
}

func (a *adapter) connectAndServe(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	conn, err := dial(dialCtx, a.cfg.WebSocketURL, a.cfg.Token)
	if err != nil {
		return fmt.Errorf("onebot websocket dial: %w", err)
	}
	a.connMu.Lock()
	a.conn = conn
	a.connMu.Unlock()
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})
	// RPC responses and inbound events share one websocket stream. Start the
	// reader before the capability probes; otherwise get_version_info would
	// wait forever for a response that no goroutine is consuming.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		a.readLoop(ctx, conn)
	}()
	if _, err := a.rpc(dialCtx, "get_version_info", map[string]any{}); err != nil {
		_ = conn.Close()
		<-readDone
		return err
	}
	loginData, err := a.rpc(dialCtx, "get_login_info", map[string]any{})
	if err != nil {
		_ = conn.Close()
		<-readDone
		return err
	}
	var login struct {
		UserID oneBotID `json:"user_id"`
	}
	if err := json.Unmarshal(loginData, &login); err != nil || strings.TrimSpace(login.UserID.String()) == "" {
		_ = conn.Close()
		<-readDone
		if err != nil {
			return fmt.Errorf("onebot get_login_info returned invalid user_id: %w", err)
		}
		return fmt.Errorf("onebot get_login_info returned empty user_id")
	}
	a.setSelfID(login.UserID.String())
	a.setStatus(bot.AdapterLifecycleSnapshot{Phase: "ready", Ready: true, LastReadyAt: time.Now(), LastError: ""})
	<-readDone
	return fmt.Errorf("onebot websocket disconnected")
}

func errString(err error) string {
	if err == nil {
		return "onebot websocket disconnected"
	}
	return err.Error()
}

func (a *adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	a.connMu.Lock()
	conn := a.conn
	a.conn = nil
	a.connMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	a.loopWG.Wait()
	a.setStatus(bot.AdapterLifecycleSnapshot{Phase: "stopped"})
	return nil
}

func (a *adapter) Messages() <-chan bot.InboundMessage      { return a.msgCh }
func (a *adapter) SendTyping(context.Context, string) error { return nil }

func (a *adapter) Lifecycle() bot.AdapterLifecycleSnapshot {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

func (a *adapter) setStatus(status bot.AdapterLifecycleSnapshot) {
	a.statusMu.Lock()
	if status.LastReadyAt.IsZero() {
		status.LastReadyAt = a.status.LastReadyAt
	}
	a.status = status
	a.statusMu.Unlock()
}

func (a *adapter) Send(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	messageType := "private"
	params := map[string]any{"message_type": messageType, "user_id": msg.ChatID, "message": oneBotSegments(msg)}
	if msg.ChatType == bot.ChatGroup {
		messageType = "group"
		params = map[string]any{"message_type": messageType, "group_id": msg.ChatID, "message": oneBotSegments(msg)}
	}
	data, err := a.rpc(ctx, "send_msg", params)
	if err != nil {
		return bot.SendResult{}, err
	}
	var response struct {
		MessageID oneBotID `json:"message_id"`
	}
	_ = json.Unmarshal(data, &response)
	return bot.SendResult{MessageID: response.MessageID.String()}, nil
}

func (a *adapter) readLoop(ctx context.Context, conn *websocket.Conn) {
	defer a.dropConn(conn)
	decoder := json.NewDecoder(conn)
	for ctx.Err() == nil {
		var envelope oneBotEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			if ctx.Err() == nil {
				a.logger.Warn("onebot websocket closed", "err", err)
				a.setStatus(bot.AdapterLifecycleSnapshot{Phase: "degraded", LastError: err.Error()})
			}
			return
		}
		if envelope.Echo != "" {
			a.pendingMu.Lock()
			waiter := a.pending[envelope.Echo]
			delete(a.pending, envelope.Echo)
			a.pendingMu.Unlock()
			if waiter != nil {
				waiter <- oneBotResponse{Envelope: envelope}
				close(waiter)
			}
			continue
		}
		event := envelope.oneBotEvent
		if event.PostType != "message" || event.MessageType == "" {
			continue
		}
		msg := bot.InboundMessage{
			Platform:  bot.PlatformQQ,
			ChatType:  bot.ChatDM,
			ChatID:    event.UserID.String(),
			UserID:    event.UserID.String(),
			UserName:  event.Sender.Nickname,
			Text:      strings.TrimSpace(event.RawMessage),
			MessageID: event.MessageID.String(),
		}
		appendOneBotMedia(&msg, event.Message)
		if event.MessageType == "group" {
			msg.ChatType = bot.ChatGroup
			msg.ChatID = event.GroupID.String()
			if a.cfg.RequireMention && !oneBotMessageMentions(event.RawMessage, a.currentSelfID()) {
				continue
			}
		}
		select {
		case a.msgCh <- msg:
		case <-ctx.Done():
			return
		}
	}
}

type oneBotEnvelope struct {
	Status   string          `json:"status"`
	RetCode  int             `json:"retcode"`
	Echo     string          `json:"echo"`
	Data     json.RawMessage `json:"data"`
	PostType string          `json:"post_type"`
	oneBotEvent
}

type oneBotResponse struct{ Envelope oneBotEnvelope }

type oneBotID string

func (id *oneBotID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return fmt.Errorf("onebot id receiver is nil")
	}
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 || string(data) == "null" {
		*id = ""
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*id = oneBotID(strings.TrimSpace(value))
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("invalid onebot id: %w", err)
	}
	*id = oneBotID(number.String())
	return nil
}

func (id oneBotID) String() string { return string(id) }

func (a *adapter) setSelfID(value string) {
	a.identityMu.Lock()
	a.selfID = strings.TrimSpace(value)
	a.identityMu.Unlock()
}

func (a *adapter) currentSelfID() string {
	a.identityMu.RLock()
	value := a.selfID
	a.identityMu.RUnlock()
	if value != "" {
		return value
	}
	return strings.TrimSpace(a.cfg.SelfID)
}

func (a *adapter) rpc(ctx context.Context, action string, params map[string]any) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	echo := fmt.Sprintf("reasonix-%d", a.echoSeq.Add(1))
	waiter := make(chan oneBotResponse, 1)
	a.pendingMu.Lock()
	if a.pending == nil {
		a.pending = make(map[string]chan oneBotResponse)
	}
	a.pending[echo] = waiter
	a.pendingMu.Unlock()
	request, _ := json.Marshal(map[string]any{"action": action, "params": params, "echo": echo})
	a.connMu.Lock()
	conn := a.conn
	a.connMu.Unlock()
	if conn == nil {
		a.removePending(echo)
		return nil, fmt.Errorf("onebot websocket is not connected")
	}
	a.writeMu.Lock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	}
	_, err := conn.Write(request)
	a.writeMu.Unlock()
	if err != nil {
		a.removePending(echo)
		return nil, err
	}
	select {
	case response := <-waiter:
		if response.Envelope.Status != "" && response.Envelope.Status != "ok" || response.Envelope.RetCode != 0 {
			return nil, fmt.Errorf("onebot %s failed (%d): %s", action, response.Envelope.RetCode, oneBotErrorMessage(response.Envelope.Message))
		}
		return response.Envelope.Data, nil
	case <-ctx.Done():
		a.removePending(echo)
		return nil, ctx.Err()
	}
}

func (a *adapter) removePending(echo string) {
	a.pendingMu.Lock()
	delete(a.pending, echo)
	a.pendingMu.Unlock()
}

func oneBotSegments(msg bot.OutboundMessage) []map[string]any {
	segments := make([]map[string]any, 0, 1+len(msg.Media))
	if strings.TrimSpace(msg.Text) != "" {
		segments = append(segments, map[string]any{"type": "text", "data": map[string]any{"text": msg.Text}})
	}
	for _, media := range msg.Media {
		kind := strings.ToLower(strings.TrimSpace(media.Kind))
		segmentType := kind
		if kind == "audio" {
			segmentType = "record"
		}
		data := media.Data
		if len(data) == 0 {
			continue
		}
		value := "base64://" + base64.StdEncoding.EncodeToString(data)
		fields := map[string]any{"file": value}
		if media.Name != "" {
			fields["name"] = media.Name
		}
		segments = append(segments, map[string]any{"type": segmentType, "data": fields})
	}
	return segments
}

func oneBotMessageMentions(raw, selfID string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if selfID = strings.TrimSpace(selfID); selfID != "" {
		return strings.Contains(raw, "[CQ:at,qq="+selfID+"]")
	}
	return strings.Contains(raw, "[CQ:at,")
}

func (a *adapter) dropConn(conn *websocket.Conn) {
	a.connMu.Lock()
	if a.conn == conn {
		a.conn = nil
	}
	a.connMu.Unlock()
	a.pendingMu.Lock()
	for echo, waiter := range a.pending {
		waiter <- oneBotResponse{Envelope: oneBotEnvelope{Status: "failed", RetCode: -1, Echo: echo}}
		close(waiter)
		delete(a.pending, echo)
	}
	a.pendingMu.Unlock()
}

type oneBotEvent struct {
	PostType    string          `json:"post_type"`
	MessageType string          `json:"message_type"`
	UserID      oneBotID        `json:"user_id"`
	GroupID     oneBotID        `json:"group_id"`
	MessageID   oneBotID        `json:"message_id"`
	RawMessage  string          `json:"raw_message"`
	Message     json.RawMessage `json:"message"`
	Sender      struct {
		Nickname string `json:"nickname"`
	} `json:"sender"`
}

func oneBotErrorMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return strings.TrimSpace(string(raw))
}

func appendOneBotMedia(msg *bot.InboundMessage, raw json.RawMessage) {
	if msg == nil || len(raw) == 0 || raw[0] != '[' {
		return
	}
	var segments []struct {
		Type string `json:"type"`
		Data struct {
			URL  string `json:"url"`
			File string `json:"file"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &segments) != nil {
		return
	}
	for _, segment := range segments {
		kind := strings.ToLower(strings.TrimSpace(segment.Type))
		switch kind {
		case "image", "file", "record", "video":
		default:
			continue
		}
		if segment.Data.URL != "" {
			msg.MediaURLs = append(msg.MediaURLs, segment.Data.URL)
			continue
		}
		if !strings.HasPrefix(segment.Data.File, "base64://") {
			continue
		}
		encoded := strings.TrimPrefix(segment.Data.File, "base64://")
		// Keep inline media bounded before decoding; the host applies the same
		// limit to URL-backed attachments after download.
		if len(encoded) > ((maxOneBotInlineMediaBytes+2)/3)*4 {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) == 0 {
			continue
		}
		if len(data) > maxOneBotInlineMediaBytes {
			continue
		}
		if kind == "record" {
			kind = "audio"
		}
		msg.Media = append(msg.Media, bot.InboundMedia{Kind: kind, Name: segment.Data.Name, Data: data})
	}
}

func validateEndpoint(raw, token string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "ws" && u.Scheme != "wss" || u.Hostname() == "" {
		return fmt.Errorf("onebot websocket_url must be ws:// or wss://")
	}
	if strings.TrimSpace(token) == "" && !isLoopback(u.Hostname()) {
		return fmt.Errorf("onebot token is required for non-loopback websocket")
	}
	if u.Scheme == "ws" && !isLoopback(u.Hostname()) {
		return fmt.Errorf("public onebot ws:// is disabled; use wss:// or loopback")
	}
	return nil
}

func isLoopback(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func dial(ctx context.Context, raw, token string) (*websocket.Conn, error) {
	cfg, err := websocket.NewConfig(raw, raw)
	if err != nil {
		return nil, err
	}
	cfg.Header = http.Header{}
	if strings.TrimSpace(token) != "" {
		cfg.Header.Set("Authorization", "Bearer "+token)
	}
	return cfg.DialContext(ctx)
}

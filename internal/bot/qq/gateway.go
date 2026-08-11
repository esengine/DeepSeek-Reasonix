package qq

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"
	"reasonix/internal/textutil"

	"github.com/gorilla/websocket"
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
	qqPassiveReplyTruncationNotice = "\n\n[Truncated: QQ allows at most 5 passive replies for one incoming message.]"
	qqHTTPTimeout                  = 30 * time.Second

	opDispatch     = 0
	opHeartbeat    = 1
	opIdentify     = 2
	opResume       = 6
	opReconnect    = 7
	opInvalid      = 9
	opHello        = 10
	opHeartbeatAck = 11
)

const (
	// QQ Bot v2 uses dedicated group/C2C and interaction intents. The legacy
	// GUILD_MESSAGES (1<<9) bit is not authorized for ordinary QQ apps.
	qqIntentGroupAndC2C = 1 << 25
	qqIntentInteraction = 1 << 26
	qqIntentPublicGuild = 1 << 30
	qqIntentDirect      = 1 << 12
)

type GatewayPhase string

const (
	GatewayPhaseConfigured     GatewayPhase = "configured"
	GatewayPhaseAuthenticating GatewayPhase = "authenticating"
	GatewayPhaseConnecting     GatewayPhase = "connecting"
	GatewayPhaseIdentifying    GatewayPhase = "identifying"
	GatewayPhaseReady          GatewayPhase = "ready"
	GatewayPhaseBackoff        GatewayPhase = "backoff"
	GatewayPhaseDegraded       GatewayPhase = "degraded"
	GatewayPhaseFatal          GatewayPhase = "fatal"
	GatewayPhaseStopped        GatewayPhase = "stopped"
)

// GatewayStatusSnapshot is optional adapter lifecycle information. Keeping it
// out of bot.Adapter preserves compatibility with the existing adapters.
type GatewayStatusSnapshot struct {
	Phase         GatewayPhase `json:"phase"`
	Ready         bool         `json:"ready"`
	LastError     string       `json:"last_error,omitempty"`
	LastErrorCode int          `json:"last_error_code,omitempty"`
	RetryAt       time.Time    `json:"retry_at,omitempty"`
	LastReadyAt   time.Time    `json:"last_ready_at,omitempty"`
	LastMessageAt time.Time    `json:"last_message_at,omitempty"`
}

type qqGatewayError struct {
	err        error
	fatal      bool
	resume     bool
	code       int
	retryAfter time.Duration
}

func (e *qqGatewayError) Error() string {
	if e == nil || e.err == nil {
		return "qq gateway error"
	}
	return e.err.Error()
}

func (e *qqGatewayError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func qqDefaultIntents(cfg config.QQBotConfig) int {
	if strings.EqualFold(strings.TrimSpace(cfg.IntentProfile), "guild") {
		return qqIntentGroupAndC2C | qqIntentInteraction | qqIntentPublicGuild | qqIntentDirect
	}
	return qqIntentGroupAndC2C | qqIntentInteraction
}

var qqMarkdownWrapperRe = regexp.MustCompile("(?is)^```(?:markdown|md)\\s*\\r?\\n([\\s\\S]*?)\\r?\\n```$")

var qqHTTPClient = &http.Client{Timeout: qqHTTPTimeout}

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
	Attachments []struct {
		URL         string `json:"url"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
	} `json:"attachments"`
}

// wsClient 管理 QQ WebSocket 连接。
type wsClient struct {
	mu               sync.Mutex
	conn             *websocket.Conn
	heartbeatMs      int
	lastHeartbeatAck time.Time
	sessionID        string
	lastSeq          int64
	token            string
	logger           *slog.Logger
}

func (a *adapter) gatewayLoop(ctx context.Context) {
	delay := time.Second
	quickDrops := 0
	attempt := 0
	forceIdentify := false
	for ctx.Err() == nil {
		started := time.Now()
		a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseAuthenticating})
		token, err := a.getAccessToken(ctx)
		if err == nil {
			err = a.connectGateway(ctx, token, forceIdentify)
		}
		if ctx.Err() != nil {
			return
		}
		var ge *qqGatewayError
		_ = errors.As(err, &ge)
		if ge != nil && ge.resume {
			// Invalid Resume: discard only the persisted gateway session and do
			// one fresh Identify. This avoids both a stale Resume loop and a
			// reconnect storm.
			_ = clearQQGatewayState(a.appID(), a.cfg.Sandbox)
			forceIdentify = true
			delay = time.Second
			quickDrops = 0
			continue
		}
		if ge != nil && ge.fatal || ge == nil && (qqFatalAuthError(err) || qqFatalConfigurationError(err)) {
			message := errorString(err)
			code := 0
			if ge != nil {
				message = ge.Error()
				code = ge.code
			}
			a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseFatal, LastError: message, LastErrorCode: code})
			return
		}
		forceIdentify = false
		if err != nil {
			a.logger.Error("qq gateway connection failed", "err", err)
		} else {
			a.logger.Warn("qq gateway connection closed")
		}
		if time.Since(started) < 15*time.Second {
			quickDrops++
		} else {
			quickDrops = 0
			attempt = 0
			delay = time.Second
		}
		if quickDrops >= 3 {
			delay = 15 * time.Minute
			a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseBackoff, LastError: "three rapid gateway disconnects; circuit paused", RetryAt: time.Now().Add(delay)})
		} else {
			retryAfter := qqRetryAfter(err)
			if ge != nil && ge.retryAfter > retryAfter {
				retryAfter = ge.retryAfter
			}
			if retryAfter > delay {
				delay = retryAfter
			}
			a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseBackoff, LastError: errorString(err), RetryAt: time.Now().Add(delay)})
		}
		if !bot.SleepCtx(ctx, qqJitterDelay(delay)) {
			return
		}
		attempt++
		delay = qqNextRetryDelay(delay, attempt)
	}
}

func qqRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	message := err.Error()
	if strings.Contains(message, "4008") || strings.Contains(message, "100017") {
		return time.Minute
	}
	return 0
}

func qqFatalAuthError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "qq token api error 401") ||
		strings.Contains(message, "qq token api error 403") ||
		strings.Contains(message, "qq gateway api error 401") ||
		strings.Contains(message, "qq gateway api error 403")
}

func qqFatalConfigurationError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "qq app_id is empty") || strings.Contains(message, "qq app secret is empty")
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func qqJitterDelay(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	// Positive-only jitter preserves provider minimums such as the 60 second
	// wait required after gateway rate-limit close codes.
	return time.Duration(float64(delay) * (1 + rand.Float64()*0.2))
}

func qqNextRetryDelay(current time.Duration, attempt int) time.Duration {
	switch attempt {
	case 1:
		return 2 * time.Second
	case 2:
		return 5 * time.Second
	case 3:
		return 10 * time.Second
	case 4:
		return 30 * time.Second
	}
	if current > 0 && current < 5*time.Minute {
		next := current * 2
		if next <= 0 || next > 5*time.Minute {
			return 5 * time.Minute
		}
		return next
	}
	return 5 * time.Minute
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
		return "", fmt.Errorf("qq app secret is empty: set the %s environment variable", a.appSecretEnvName())
	}
	body, err := json.Marshal(map[string]string{
		"appId":        appID,
		"clientSecret": appSecret,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qqTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := qqHTTPClient.Do(req)
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
	expiresIn := int(result.ExpiresIn)
	if expiresIn > 60 {
		a.tokenExpiry = time.Now().Add(time.Duration(expiresIn-60) * time.Second)
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

func (a *adapter) connectGateway(ctx context.Context, token string, forceIdentify bool) error {
	a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseConnecting})
	gatewayURL, err := a.getGatewayURL(ctx, token)
	if err != nil {
		return err
	}
	if parsed, parseErr := url.Parse(gatewayURL); parseErr == nil {
		a.logger.Info("qq gateway endpoint resolved", "host", parsed.Hostname(), "sandbox", a.cfg.Sandbox)
	}
	conn, err := a.dialGateway(ctx, gatewayURL, token)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}
	defer conn.Close()
	defer a.dropConn(conn)
	if !a.trackConn(ctx, conn) {
		// Stop already closed the tracked conn slot; entering a blocking read
		// now would leave a connection Stop can no longer unblock.
		return ctx.Err()
	}
	a.logger.Info("qq gateway connected", "sandbox", a.cfg.Sandbox)

	ws := &wsClient{conn: conn, token: token, logger: a.logger}

	var msg gatewayPayload
	// 第一次读取必须是 Hello
	if err := conn.ReadJSON(&msg); err != nil {
		return classifyQQGatewayReadError("read hello", err)
	}
	if msg.Op != opHello {
		return fmt.Errorf("expected op=%d hello, got op=%d", opHello, msg.Op)
	}
	var hello helloData
	if err := json.Unmarshal(msg.D, &hello); err != nil {
		return err
	}
	ws.heartbeatMs = int(sanitizeHeartbeatInterval(time.Duration(hello.HeartbeatInterval) * time.Millisecond).Milliseconds())
	ws.lastHeartbeatAck = time.Now()

	state, stateErr := loadQQGatewayState(a.appID(), a.cfg.Sandbox)
	if stateErr != nil {
		a.logger.Warn("qq gateway state unavailable; identifying afresh", "err", stateErr)
		state = qqGatewayState{}
	}
	resume := !forceIdentify && strings.TrimSpace(state.SessionID) != ""
	a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseIdentifying})
	if resume {
		resumeJSON, _ := json.Marshal(map[string]any{
			"token":      fmt.Sprintf("QQBot %s", token),
			"session_id": state.SessionID,
			"seq":        state.Seq,
		})
		if err := ws.send(opResume, resumeJSON); err != nil {
			return &qqGatewayError{err: fmt.Errorf("send resume: %w", err), resume: true}
		}
	} else {
		identify := identifyData{
			Token:   fmt.Sprintf("QQBot %s", token),
			Intents: qqDefaultIntents(a.cfg),
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
	}

	// 读取 READY/RESUMED。其它首包表示握手失败；继续读循环会把
	// Invalid Session 当作在线，是 #7424/#7816 的核心问题。
	if err := conn.ReadJSON(&msg); err != nil {
		return classifyQQGatewayReadError("read ready", err)
	}
	if resume && msg.Op == opInvalid {
		return &qqGatewayError{err: fmt.Errorf("qq gateway resume rejected (op=9)"), resume: true}
	}
	if !resume && msg.Op == opInvalid {
		return &qqGatewayError{err: fmt.Errorf("qq gateway identify rejected (op=9)"), fatal: true, code: qqInvalidSessionCode(msg.D)}
	}
	if !resume && msg.Op == opDispatch && msg.T == "READY" {
		var ready struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(msg.D, &ready); err != nil {
			return fmt.Errorf("decode ready: %w", err)
		}
		ws.sessionID = ready.SessionID
		a.sessionID = ready.SessionID
		a.seq = msg.S
		ws.lastSeq = msg.S
		if err := saveQQGatewayState(a.appID(), a.cfg.Sandbox, qqGatewayState{SessionID: ready.SessionID, Seq: msg.S}); err != nil {
			return fmt.Errorf("persist qq gateway ready state: %w", err)
		}
		a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseReady, Ready: true, LastReadyAt: time.Now()})
		a.logger.Info("qq gateway ready", "sandbox", a.cfg.Sandbox, "heartbeat_ms", ws.heartbeatMs)
	} else if resume && msg.Op == opDispatch && msg.T == "RESUMED" {
		ws.sessionID = state.SessionID
		a.sessionID = state.SessionID
		seq := msg.S
		if seq == 0 {
			seq = state.Seq
		}
		a.seq = seq
		ws.lastSeq = seq
		if err := saveQQGatewayState(a.appID(), a.cfg.Sandbox, qqGatewayState{SessionID: state.SessionID, Seq: seq}); err != nil {
			return fmt.Errorf("persist qq gateway resumed state: %w", err)
		}
		a.setStatus(GatewayStatusSnapshot{Phase: GatewayPhaseReady, Ready: true, LastReadyAt: time.Now()})
		a.logger.Info("qq gateway resumed", "sandbox", a.cfg.Sandbox, "heartbeat_ms", ws.heartbeatMs)
	} else {
		return &qqGatewayError{err: fmt.Errorf("qq gateway handshake expected %s, got op=%d event=%s", map[bool]string{true: "RESUMED", false: "READY"}[resume], msg.Op, msg.T), fatal: true}
	}
	// Replay raw dispatches persisted after the server sequence advanced but
	// before the host accepted the turn. The ingress journal suppresses events
	// that completed before the crash.
	if replay, replayErr := loadQQGatewayRawEvents(a.appID(), a.cfg.Sandbox); replayErr != nil {
		a.logger.Warn("qq gateway replay journal unavailable", "err", replayErr)
	} else {
		for _, replayEvent := range replay {
			if !a.handleDispatch(replayEvent) {
				return fmt.Errorf("qq replay channel unavailable")
			}
			if !qqDispatchProducesIngress(replayEvent.T) {
				_ = removeQQGatewayRawEvent(a.appID(), a.cfg.Sandbox, gatewayPayloadEventID(replayEvent))
			}
		}
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
				if !ws.lastHeartbeatAck.IsZero() && time.Since(ws.lastHeartbeatAck) > 2*time.Duration(ws.heartbeatMs)*time.Millisecond {
					ws.logger.Warn("qq gateway heartbeat acknowledgement timed out")
					ws.mu.Unlock()
					a.closeConn()
					return
				}
				payload := json.RawMessage("null")
				if ws.lastSeq != 0 {
					payload = json.RawMessage(fmt.Sprintf(`%d`, ws.lastSeq))
				}
				if err := ws.send(opHeartbeat, payload); err != nil {
					ws.logger.Error("heartbeat failed", "err", err)
					ws.mu.Unlock()
					a.closeConn()
					return
				}
				ws.mu.Unlock()
			}
		}
	}()

	// 主循环：读取 dispatch 事件
	for {
		if err := conn.ReadJSON(&msg); err != nil {
			a.logger.Error("decode gateway message", "err", err)
			heartbeatCancel()
			<-heartbeatDone
			return classifyQQGatewayReadError("read gateway message", err)
		}
		switch msg.Op {
		case opDispatch:
			if err := saveQQGatewayRawEvent(a.appID(), a.cfg.Sandbox, msg); err != nil {
				a.logger.Error("persist qq gateway event failed", "err", err)
				heartbeatCancel()
				<-heartbeatDone
				return fmt.Errorf("persist qq event: %w", err)
			}
			if !a.handleDispatch(msg) {
				heartbeatCancel()
				<-heartbeatDone
				return fmt.Errorf("qq inbound message channel is unavailable")
			}
			if !qqDispatchProducesIngress(msg.T) {
				_ = removeQQGatewayRawEvent(a.appID(), a.cfg.Sandbox, gatewayPayloadEventID(msg))
			}
			ws.mu.Lock()
			ws.lastSeq = msg.S
			ws.mu.Unlock()
			a.seq = msg.S
			if err := saveQQGatewayState(a.appID(), a.cfg.Sandbox, qqGatewayState{SessionID: ws.sessionID, Seq: msg.S}); err != nil {
				heartbeatCancel()
				<-heartbeatDone
				return fmt.Errorf("persist qq gateway sequence: %w", err)
			}
			a.markMessage()
		case opHeartbeatAck:
			ws.mu.Lock()
			ws.lastHeartbeatAck = time.Now()
			ws.mu.Unlock()
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
			if resume {
				return &qqGatewayError{err: fmt.Errorf("qq gateway resumed session invalidated (op=9)"), resume: true}
			}
			return &qqGatewayError{err: fmt.Errorf("qq gateway session invalidated (op=9)"), fatal: true, code: qqInvalidSessionCode(msg.D)}
		}
	}
}

func qqDispatchProducesIngress(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "C2C_MESSAGE_CREATE", "GROUP_AT_MESSAGE_CREATE", "AT_MESSAGE_CREATE", "DIRECT_MESSAGE_CREATE", "MESSAGE_CREATE", "INTERACTION_CREATE":
		return true
	default:
		return false
	}
}

func qqInvalidSessionCode(raw json.RawMessage) int {
	var body struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.Code
}

func classifyQQGatewayReadError(stage string, err error) error {
	if err == nil {
		return nil
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		message := fmt.Sprintf("%s: qq gateway closed (%d): %s", stage, closeErr.Code, strings.TrimSpace(closeErr.Text))
		switch closeErr.Code {
		case 4914, 4915:
			return &qqGatewayError{err: errors.New(message), fatal: true, code: closeErr.Code}
		case 4008:
			return &qqGatewayError{err: errors.New(message), code: closeErr.Code, retryAfter: time.Minute}
		}
		if strings.Contains(closeErr.Text, "100017") {
			return &qqGatewayError{err: errors.New(message), code: 100017, retryAfter: time.Minute}
		}
		return &qqGatewayError{err: errors.New(message), code: closeErr.Code}
	}
	return fmt.Errorf("%s: %w", stage, err)
}

// dialGateway dials the QQ gateway honoring ctx. The conn only becomes
// trackable after the dial returns, so Stop can interrupt a stalled TCP dial
// or WebSocket/TLS handshake only through ctx cancellation —
// websocket.DialConfig would dial with context.Background() and leave Stop's
// loopWG.Wait blocked with nothing to close.
func (a *adapter) dialGateway(ctx context.Context, gatewayURL, token string) (*websocket.Conn, error) {
	header := http.Header{}
	header.Set("Authorization", "QQBot "+token)
	header.Set("X-Union-Appid", a.appID())
	// Gorilla does not observe ctx cancellation after TCP connects. Keep the raw
	// connection cancellable until DialContext returns so Stop can drain a
	// stalled HTTP handshake.
	handshakeDone := make(chan struct{})
	dialer := *websocket.DefaultDialer
	netDialer := &net.Dialer{}
	dialer.NetDialContext = func(_ context.Context, network, address string) (net.Conn, error) {
		conn, err := netDialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.Close()
			case <-handshakeDone:
			}
		}()
		return conn, nil
	}
	conn, response, err := dialer.DialContext(ctx, gatewayURL, header)
	close(handshakeDone)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	return conn, err
}

// trackConn publishes the live gateway connection so Stop can close it and
// unblock the blocking websocket reads, which do not honor ctx. Publication is
// refused once ctx is cancelled, so a conn that finishes dialing concurrently
// with Stop can never be left open but unreachable.
func (a *adapter) trackConn(ctx context.Context, conn *websocket.Conn) bool {
	a.connMu.Lock()
	defer a.connMu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	a.conn = conn
	return true
}

func (a *adapter) dropConn(conn *websocket.Conn) {
	a.connMu.Lock()
	if a.conn == conn {
		a.conn = nil
	}
	a.connMu.Unlock()
}

func (a *adapter) closeConn() {
	a.connMu.Lock()
	conn := a.conn
	a.conn = nil
	a.connMu.Unlock()
	if conn != nil {
		conn.Close()
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
	return strings.TrimSpace(os.Getenv(a.appSecretEnvName()))
}

func (a *adapter) apiBaseURL() string {
	if a.cfg.Sandbox {
		return qqSandboxURL
	}
	return qqBaseURL
}

func (a *adapter) getGatewayURL(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.apiBaseURL()+"/gateway", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := qqHTTPClient.Do(req)
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
	return ws.conn.WriteJSON(payload)
}

func (a *adapter) handleDispatch(msg gatewayPayload) bool {
	if msg.T == "INTERACTION_CREATE" {
		return a.handleInteraction(msg)
	}
	var evt dispatchEvent
	if err := json.Unmarshal(msg.D, &evt); err != nil {
		a.logger.Error("parse dispatch", "err", err)
		return false
	}

	ib := bot.InboundMessage{
		Platform:  bot.PlatformQQ,
		UserID:    qqAuthorID(evt),
		UserName:  evt.Author.Username,
		Text:      evt.Content,
		MessageID: evt.ID,
	}
	for _, attachment := range evt.Attachments {
		if rawURL := strings.TrimSpace(attachment.URL); rawURL != "" {
			ib.MediaURLs = append(ib.MediaURLs, rawURL)
		}
	}

	switch msg.T {
	case "C2C_MESSAGE_CREATE":
		ib.ChatType = bot.ChatDM
		ib.UserID = firstQQID(evt.Author.UserOpenID, evt.Author.ID)
		ib.ChatID = ib.UserID
	case "GROUP_AT_MESSAGE_CREATE":
		ib.ChatType = bot.ChatGroup
		ib.ChatID = evt.GroupOpenID
		ib.UserID = firstQQID(evt.Author.MemberOpenID, evt.Author.UserOpenID, evt.Author.ID)
	case "AT_MESSAGE_CREATE":
		ib.ChatType = bot.ChatGuild
		ib.ChatID = evt.ChannelID
		ib.UserID = firstQQID(evt.Author.ID, evt.Author.UserOpenID, evt.Author.MemberOpenID)
	case "DIRECT_MESSAGE_CREATE":
		ib.ChatType = bot.ChatDirect
		ib.ChatID = evt.GuildID
		ib.UserID = firstQQID(evt.Author.ID, evt.Author.UserOpenID, evt.Author.MemberOpenID)
	case "MESSAGE_CREATE":
		ib.ChatType = bot.ChatDM
		ib.ChatID = evt.ChannelID
	default:
		if strings.TrimSpace(msg.T) != "" {
			a.logger.Info("qq dispatch ignored", "event", msg.T)
		}
		return true // 忽略其他事件，但仍可安全推进 seq
	}
	a.logger.Info("qq dispatch received", "event", msg.T, "chat_type", ib.ChatType)

	select {
	case a.msgCh <- ib:
		return true
	default:
		a.logger.Warn("message channel full, dropping message")
		return false
	}
}

type qqInteractionEvent struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	Data      struct {
		Resolved struct {
			ButtonID  string `json:"button_id"`
			MessageID string `json:"message_id"`
		} `json:"resolved"`
		ButtonID  string `json:"button_id"`
		MessageID string `json:"message_id"`
	} `json:"data"`
	GroupOpenID string `json:"group_openid"`
	ChatType    string `json:"chat_type"`
	User        struct {
		UserOpenID   string `json:"user_openid"`
		MemberOpenID string `json:"member_openid"`
		ID           string `json:"id"`
	} `json:"user"`
}

func (a *adapter) handleInteraction(msg gatewayPayload) bool {
	var evt qqInteractionEvent
	if err := json.Unmarshal(msg.D, &evt); err != nil {
		a.logger.Error("parse qq interaction", "err", err)
		return false
	}
	callback := firstQQID(evt.Data.Resolved.ButtonID, evt.Data.ButtonID)
	if strings.TrimSpace(callback) == "" {
		return true
	}
	userID := firstQQID(evt.User.MemberOpenID, evt.User.UserOpenID, evt.User.ID)
	messageID := firstQQID(evt.MessageID, evt.Data.Resolved.MessageID, evt.Data.MessageID)
	ackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := a.ackInteraction(ackCtx, evt.ID); err != nil {
		a.logger.Warn("qq interaction ack failed", "err", err)
	}
	candidate := bot.Interaction{ID: evt.ID, ChatID: strings.TrimSpace(evt.GroupOpenID), UserID: userID, MessageID: messageID, CallbackID: callback}
	if strings.EqualFold(strings.TrimSpace(evt.ChatType), "group") || strings.TrimSpace(evt.GroupOpenID) != "" {
		candidate.ChatType = bot.ChatGroup
	} else {
		candidate.ChatType = bot.ChatDM
		candidate.ChatID = userID
	}
	cancel()
	if a.interactionCh != nil {
		select {
		case a.interactionCh <- candidate:
		default:
			a.logger.Warn("qq interaction channel full")
		}
	}
	ib := bot.InboundMessage{Platform: bot.PlatformQQ, UserID: userID, UserName: "", Text: callback, MessageID: evt.ID, Raw: candidate}
	if strings.EqualFold(strings.TrimSpace(evt.ChatType), "group") || strings.TrimSpace(evt.GroupOpenID) != "" {
		ib.ChatType = bot.ChatGroup
		ib.ChatID = strings.TrimSpace(evt.GroupOpenID)
	} else {
		ib.ChatType = bot.ChatDM
		ib.ChatID = userID
	}
	select {
	case a.msgCh <- ib:
		return true
	default:
		a.logger.Warn("message channel full, dropping qq interaction")
		return false
	}
}

func (a *adapter) ackInteraction(ctx context.Context, interactionID string) error {
	interactionID = strings.TrimSpace(interactionID)
	if interactionID == "" {
		return fmt.Errorf("qq interaction id is empty")
	}
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"code": 0})
	endpoint := fmt.Sprintf("%s/interactions/%s", a.apiBaseURL(), url.PathEscape(interactionID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Union-Appid", a.appID())
	resp, err := qqHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("qq interaction ack error %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

func qqAuthorID(evt dispatchEvent) string {
	return firstQQID(evt.Author.UserOpenID, evt.Author.MemberOpenID, evt.Author.UnionOpenID, evt.Author.ID)
}

func firstQQID(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// sendMessage 使用 QQ REST API 发送消息。
func (a *adapter) sendMessage(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	if len(msg.Media) > 0 {
		return a.sendMediaMessage(ctx, msg)
	}
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
	var delivered bot.SendResult
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
			return delivered, err
		}
		a.logger.Info("qq message sent", "chat_type", msg.ChatType, "message_id_set", strings.TrimSpace(result.MessageID) != "")
		delivered.Merge(result)
	}
	return delivered, nil
}

func (a *adapter) sendMediaMessage(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	if msg.ChatType != bot.ChatDM && msg.ChatType != bot.ChatGroup {
		return bot.SendResult{}, fmt.Errorf("qq media is supported for C2C and group chats only")
	}
	var delivered bot.SendResult
	for i, media := range msg.Media {
		if len(media.Data) == 0 {
			return delivered, fmt.Errorf("qq media %q has no prepared bytes", media.Name)
		}
		fileInfo, err := a.uploadMedia(ctx, msg, media)
		if err != nil {
			return delivered, err
		}
		payload := map[string]any{"msg_type": 7, "media": map[string]any{"file_info": fileInfo}}
		if i == 0 && strings.TrimSpace(msg.Text) != "" {
			payload["content"] = msg.Text
		}
		result, err := a.sendMessagePayload(ctx, msg, payload, a.nextMessageSeq(msg.ReplyToMsgID))
		if err != nil {
			return delivered, err
		}
		delivered.Merge(result)
	}
	return delivered, nil
}

func (a *adapter) uploadMedia(ctx context.Context, msg bot.OutboundMessage, media bot.OutboundMedia) (string, error) {
	fileType := qqFileType(media.Kind)
	if fileType == 0 {
		return "", fmt.Errorf("unsupported qq media type %q", media.Kind)
	}
	cacheKey := fmt.Sprintf("%s|%s|%s|%d|%x", a.appID(), msg.ChatType, msg.ChatID, fileType, md5.Sum(media.Data))
	a.mediaMu.Lock()
	if cached := a.mediaCache[cacheKey]; cached.FileInfo != "" && time.Now().Before(cached.ExpiresAt) {
		a.mediaMu.Unlock()
		return cached.FileInfo, nil
	}
	a.mediaMu.Unlock()
	if len(media.Data) > 5<<20 {
		fileInfo, ttl, err := a.uploadMediaChunked(ctx, msg, fileType, media)
		if err != nil {
			return "", err
		}
		a.mediaMu.Lock()
		if a.mediaCache == nil {
			a.mediaCache = make(map[string]qqMediaCacheEntry)
		}
		a.mediaCache[cacheKey] = qqMediaCacheEntry{FileInfo: fileInfo, ExpiresAt: time.Now().Add(ttl)}
		a.mediaMu.Unlock()
		return fileInfo, nil
	}
	payload := map[string]any{"file_type": fileType, "srv_send_msg": false, "file_data": base64.StdEncoding.EncodeToString(media.Data)}
	if strings.TrimSpace(media.Name) != "" {
		payload["file_name"] = media.Name
	}
	endpoint := fmt.Sprintf("%s/v2/users/%s/files", a.apiBaseURL(), url.PathEscape(msg.ChatID))
	if msg.ChatType == bot.ChatGroup {
		endpoint = fmt.Sprintf("%s/v2/groups/%s/files", a.apiBaseURL(), url.PathEscape(msg.ChatID))
	}
	data, err := a.postJSON(ctx, endpoint, payload)
	if err != nil {
		return "", fmt.Errorf("upload qq %s: %w", media.Kind, err)
	}
	var response struct {
		FileInfo string `json:"file_info"`
		Data     struct {
			FileInfo string `json:"file_info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", err
	}
	if response.FileInfo == "" {
		response.FileInfo = response.Data.FileInfo
	}
	if response.FileInfo == "" {
		return "", fmt.Errorf("qq upload response has no file_info")
	}
	a.mediaMu.Lock()
	if a.mediaCache == nil {
		a.mediaCache = make(map[string]qqMediaCacheEntry)
	}
	a.mediaCache[cacheKey] = qqMediaCacheEntry{FileInfo: response.FileInfo, ExpiresAt: time.Now().Add(30 * time.Minute)}
	a.mediaMu.Unlock()
	return response.FileInfo, nil
}

func (a *adapter) uploadMediaChunked(ctx context.Context, msg bot.OutboundMessage, fileType int, media bot.OutboundMedia) (string, time.Duration, error) {
	md5sum := md5.Sum(media.Data)
	sha1sum := sha1.Sum(media.Data)
	first := media.Data
	if len(first) > 10002432 {
		first = first[:10002432]
	}
	md510m := md5.Sum(first)
	endpoint := fmt.Sprintf("%s/v2/users/%s/upload_prepare", a.apiBaseURL(), url.PathEscape(msg.ChatID))
	if msg.ChatType == bot.ChatGroup {
		endpoint = fmt.Sprintf("%s/v2/groups/%s/upload_prepare", a.apiBaseURL(), url.PathEscape(msg.ChatID))
	}
	prepare, err := a.postJSON(ctx, endpoint, map[string]any{"file_type": fileType, "file_name": media.Name, "file_size": len(media.Data), "md5": fmt.Sprintf("%x", md5sum[:]), "sha1": fmt.Sprintf("%x", sha1sum[:]), "md5_10m": fmt.Sprintf("%x", md510m[:])})
	if err != nil {
		return "", 0, fmt.Errorf("qq upload prepare: %w", err)
	}
	var info struct {
		UploadID  string `json:"upload_id"`
		BlockSize int    `json:"block_size"`
		Parts     []struct {
			Index        int    `json:"index"`
			PresignedURL string `json:"presigned_url"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(prepare, &info); err != nil {
		return "", 0, err
	}
	if info.UploadID == "" || info.BlockSize <= 0 || len(info.Parts) == 0 {
		return "", 0, fmt.Errorf("qq upload prepare returned no parts")
	}
	for _, part := range info.Parts {
		start := (part.Index - 1) * info.BlockSize
		if start < 0 || start >= len(media.Data) {
			return "", 0, fmt.Errorf("qq upload part index is invalid")
		}
		end := min(start+info.BlockSize, len(media.Data))
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, part.PresignedURL, bytes.NewReader(media.Data[start:end]))
		if err != nil {
			return "", 0, err
		}
		req.Header.Set("Content-Length", strconv.Itoa(end-start))
		resp, err := qqHTTPClient.Do(req)
		if err != nil {
			return "", 0, err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			return "", 0, fmt.Errorf("qq upload part %d failed with HTTP %d", part.Index, resp.StatusCode)
		}
		finishEndpoint := fmt.Sprintf("%s/v2/users/%s/upload_part_finish", a.apiBaseURL(), url.PathEscape(msg.ChatID))
		if msg.ChatType == bot.ChatGroup {
			finishEndpoint = fmt.Sprintf("%s/v2/groups/%s/upload_part_finish", a.apiBaseURL(), url.PathEscape(msg.ChatID))
		}
		if _, err := a.postJSON(ctx, finishEndpoint, map[string]any{"upload_id": info.UploadID, "part_index": part.Index, "block_size": end - start, "md5": fmt.Sprintf("%x", md5.Sum(media.Data[start:end]))}); err != nil {
			return "", 0, err
		}
	}
	completeEndpoint := fmt.Sprintf("%s/v2/users/%s/files", a.apiBaseURL(), url.PathEscape(msg.ChatID))
	if msg.ChatType == bot.ChatGroup {
		completeEndpoint = fmt.Sprintf("%s/v2/groups/%s/files", a.apiBaseURL(), url.PathEscape(msg.ChatID))
	}
	complete, err := a.postJSON(ctx, completeEndpoint, map[string]any{"upload_id": info.UploadID})
	if err != nil {
		return "", 0, err
	}
	var result struct {
		FileInfo string `json:"file_info"`
		TTL      int    `json:"ttl"`
	}
	if err := json.Unmarshal(complete, &result); err != nil {
		return "", 0, err
	}
	if result.FileInfo == "" {
		return "", 0, fmt.Errorf("qq chunked upload response has no file_info")
	}
	ttl := time.Duration(result.TTL) * time.Second
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return result.FileInfo, ttl, nil
}

func qqFileType(kind string) int {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image":
		return 1
	case "video":
		return 2
	case "audio", "voice":
		return 3
	case "file":
		return 4
	default:
		return 0
	}
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
		payload["keyboard"] = map[string]any{
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return bot.SendResult{}, err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Union-Appid", a.appID())

	resp, err := qqHTTPClient.Do(req)
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
	return textutil.FitGraphemeBytes(text, maxBytes)
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

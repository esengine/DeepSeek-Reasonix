// Package nextcloudtalk implements a Nextcloud Talk webhook bot adapter.
package nextcloudtalk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
)

const (
	defaultListenAddr  = "127.0.0.1:38017"
	defaultWebhookPath = "/reasonix/nextcloud-talk"
	maxWebhookBytes    = 2 << 20
	maxResponseBytes   = 1 << 20
)

// Config controls a Nextcloud Talk bot webhook connection.
// SecretEnv points to the shared secret returned by talk:bot:install; the
// secret itself must not be written to Reasonix configuration.
type Config struct {
	ServerURL    string
	ListenAddr   string
	WebhookPath  string
	SecretEnv    string
	ConnectionID string
}

type adapter struct {
	cfg    Config
	logger *slog.Logger
	client *http.Client

	msgCh  chan bot.InboundMessage
	cancel context.CancelFunc

	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
}

// New creates a Nextcloud Talk adapter.
func New(cfg Config, logger *slog.Logger) bot.Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &adapter{
		cfg:    cfg,
		logger: logger.With("platform", "nextcloud-talk"),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *adapter) Platform() bot.Platform { return bot.PlatformNextcloudTalk }
func (a *adapter) Name() string           { return "nextcloud-talk" }

func (a *adapter) Start(ctx context.Context) error {
	if _, err := a.secret(); err != nil {
		return err
	}
	if _, err := normalizedServerURL(a.cfg.ServerURL); err != nil {
		return err
	}

	listenAddr := strings.TrimSpace(a.cfg.ListenAddr)
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	webhookPath := normalizedWebhookPath(a.cfg.WebhookPath)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("nextcloud-talk: listen %s: %w", listenAddr, err)
	}

	a.mu.Lock()
	if a.server != nil {
		a.mu.Unlock()
		_ = listener.Close()
		return fmt.Errorf("nextcloud-talk: adapter already started")
	}
	a.msgCh = make(chan bot.InboundMessage, 64)
	ctx, a.cancel = context.WithCancel(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc(webhookPath, a.handleWebhook)
	a.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	a.listener = listener
	server := a.server
	a.mu.Unlock()

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			a.logger.Error("nextcloud talk webhook server stopped", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	a.logger.Info("nextcloud talk webhook listening", "addr", listener.Addr().String(), "path", webhookPath)
	return nil
}

func (a *adapter) Stop() error {
	a.mu.Lock()
	cancel := a.cancel
	server := a.server
	a.server = nil
	a.listener = nil
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if server == nil {
		return nil
	}
	ctx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (a *adapter) Messages() <-chan bot.InboundMessage { return a.msgCh }

func (a *adapter) SendTyping(context.Context, string) error { return nil }

func (a *adapter) Send(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	secret, err := a.secret()
	if err != nil {
		return bot.SendResult{}, err
	}
	baseURL, err := normalizedServerURL(a.cfg.ServerURL)
	if err != nil {
		return bot.SendResult{}, err
	}
	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		return bot.SendResult{}, fmt.Errorf("nextcloud-talk: chat id is required")
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return bot.SendResult{}, fmt.Errorf("nextcloud-talk: message text is required")
	}

	payload := struct {
		Message     string `json:"message"`
		ReplyTo     int    `json:"replyTo,omitempty"`
		ReferenceID string `json:"referenceId,omitempty"`
	}{Message: msg.Text}
	if replyTo := strings.TrimSpace(msg.ReplyToMsgID); replyTo != "" {
		id, parseErr := strconv.Atoi(replyTo)
		if parseErr != nil || id <= 0 {
			return bot.SendResult{}, fmt.Errorf("nextcloud-talk: invalid reply message id %q", replyTo)
		}
		payload.ReplyTo = id
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return bot.SendResult{}, fmt.Errorf("nextcloud-talk: encode message: %w", err)
	}

	random, err := randomHex(32)
	if err != nil {
		return bot.SendResult{}, fmt.Errorf("nextcloud-talk: generate request nonce: %w", err)
	}
	signature := signatureFor(secret, random, body)
	endpoint := strings.TrimRight(baseURL.String(), "/") + "/ocs/v2.php/apps/spreed/api/v1/bot/" + url.PathEscape(chatID) + "/message"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return bot.SendResult{}, fmt.Errorf("nextcloud-talk: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("OCS-APIRequest", "true")
	req.Header.Set("X-Nextcloud-Talk-Bot-Random", random)
	req.Header.Set("X-Nextcloud-Talk-Bot-Signature", signature)

	resp, err := a.client.Do(req)
	if err != nil {
		return bot.SendResult{}, fmt.Errorf("nextcloud-talk: send message: %w", err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return bot.SendResult{}, fmt.Errorf("nextcloud-talk: read response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return bot.SendResult{}, fmt.Errorf("nextcloud-talk: send failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	messageID := parseMessageID(respBody)
	return bot.SendResult{MessageID: messageID}, nil
}

func (a *adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret, err := a.secret()
	if err != nil {
		http.Error(w, "bot secret is not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBytes+1))
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(body) > maxWebhookBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	random := strings.TrimSpace(r.Header.Get("X-Nextcloud-Talk-Random"))
	signature := strings.TrimSpace(r.Header.Get("X-Nextcloud-Talk-Signature"))
	if random == "" || signature == "" || !validSignature(secret, random, body, signature) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var event talkActivity
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid event payload", http.StatusBadRequest)
		return
	}
	message, ok := activityMessage(event)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	message.ConnectionID = strings.TrimSpace(a.cfg.ConnectionID)
	if message.ConnectionID == "" {
		message.ConnectionID = "nextcloud-talk"
	}

	if a.msgCh == nil {
		http.Error(w, "bot runtime is not started", http.StatusServiceUnavailable)
		return
	}
	select {
	case a.msgCh <- message:
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "bot queue is full", http.StatusServiceUnavailable)
	}
}

type talkActivity struct {
	Type  string `json:"type"`
	Actor struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"actor"`
	Object struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Content   string `json:"content"`
		MediaType string `json:"mediaType"`
	} `json:"object"`
	Target struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"target"`
}

func activityMessage(event talkActivity) (bot.InboundMessage, bool) {
	if !strings.EqualFold(strings.TrimSpace(event.Type), "Create") ||
		!strings.EqualFold(strings.TrimSpace(event.Object.Type), "Note") ||
		!strings.EqualFold(strings.TrimSpace(event.Object.Name), "message") {
		return bot.InboundMessage{}, false
	}
	if strings.EqualFold(strings.TrimSpace(event.Actor.Type), "Application") {
		return bot.InboundMessage{}, false
	}
	chatID := strings.TrimSpace(event.Target.ID)
	messageID := strings.TrimSpace(event.Object.ID)
	if chatID == "" || messageID == "" {
		return bot.InboundMessage{}, false
	}
	var content struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(event.Object.Content), &content); err != nil || strings.TrimSpace(content.Message) == "" {
		return bot.InboundMessage{}, false
	}
	userID := actorIdentifier(event.Actor.ID)
	return bot.InboundMessage{
		Platform:   bot.PlatformNextcloudTalk,
		Domain:     "nextcloud-talk",
		ChatType:   bot.ChatDirect,
		ChatID:     chatID,
		UserID:     userID,
		OperatorID: userID,
		UserName:   strings.TrimSpace(event.Actor.Name),
		Text:       content.Message,
		MessageID:  messageID,
		Raw:        event,
	}, true
}

func actorIdentifier(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '/'); i >= 0 && i+1 < len(raw) {
		return raw[i+1:]
	}
	return raw
}

func (a *adapter) secret() (string, error) {
	env := strings.TrimSpace(a.cfg.SecretEnv)
	if env == "" {
		return "", fmt.Errorf("nextcloud-talk: secret_env is required")
	}
	secret := os.Getenv(env)
	if secret == "" {
		return "", fmt.Errorf("nextcloud-talk: %s is not set", env)
	}
	return secret, nil
}

func normalizedServerURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("nextcloud-talk: server_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("nextcloud-talk: invalid server_url %q", raw)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

func normalizedWebhookPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return defaultWebhookPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func signatureFor(secret, random string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(random))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func validSignature(secret, random string, body []byte, provided string) bool {
	expected, err := hex.DecodeString(signatureFor(secret, random, body))
	if err != nil {
		return false
	}
	actual, err := hex.DecodeString(strings.TrimSpace(provided))
	if err != nil {
		return false
	}
	return hmac.Equal(expected, actual)
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func parseMessageID(body []byte) string {
	var payload struct {
		OCS struct {
			Data struct {
				ID json.RawMessage `json:"id"`
			} `json:"data"`
		} `json:"ocs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.OCS.Data.ID) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(payload.OCS.Data.ID, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n int64
	if json.Unmarshal(payload.OCS.Data.ID, &n) == nil && n > 0 {
		return strconv.FormatInt(n, 10)
	}
	return ""
}

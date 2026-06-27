package napcat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"reasonix/internal/bot"
	"github.com/gorilla/websocket"
)

type adapter struct {
	platform bot.Platform
	logger   *slog.Logger
	msgCh    chan bot.InboundMessage
	wsURL    string
	httpURL  string
	mu       sync.Mutex
	conn     *websocket.Conn
}

type onebotMsg struct {
	PostType string          `json:"post_type"`
	MsgType  string          `json:"message_type"`
	UserID   int64           `json:"user_id"`
	GroupID  int64           `json:"group_id,omitempty"`
	Message  json.RawMessage `json:"message"`
	RawMsg   string          `json:"raw_message"`
	MsgID    int64           `json:"message_id"`
}

func NewAdapter(logger *slog.Logger) bot.Adapter {
	return &adapter{
		platform: bot.PlatformNapCat,
		logger:   logger.With("adapter", "napcat"),
		msgCh:    make(chan bot.InboundMessage, 64),
		wsURL:    "ws://127.0.0.1:3001",
		httpURL:  "http://127.0.0.1:3000",
	}
}

func (a *adapter) Name() string             { return "napcat" }
func (a *adapter) Platform() bot.Platform   { return a.platform }

func (a *adapter) Start(ctx context.Context) error {
	a.logger.Info("connecting to NapCat")
	c, _, err := websocket.DefaultDialer.Dial(a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("napcat ws: %w", err)
	}
	a.mu.Lock()
	a.conn = c
	a.mu.Unlock()
	go a.readLoop(ctx, c)
	return nil
}

func (a *adapter) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		return a.conn.Close()
	}
	return nil
}

func (a *adapter) Messages() <-chan bot.InboundMessage { return a.msgCh }

func (a *adapter) readLoop(ctx context.Context, conn *websocket.Conn) {
	defer conn.Close()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			a.logger.Warn("ws error", "err", err)
			return
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		if raw["post_type"] != "message" {
			continue
		}
		var msg onebotMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		text := extractText(msg.Message)
		if text == "" {
			continue
		}
		inbound := bot.InboundMessage{
			Platform:     a.platform,
			ConnectionID: "napcat-main",
			Domain:       "napcat",
			Text:         text,
			MessageID:    fmt.Sprintf("%d", msg.MsgID),
			UserID:       fmt.Sprintf("%d", msg.UserID),
			ChatType:     bot.ChatDM,
			ChatID:       fmt.Sprintf("%d", msg.UserID),
		}
		if msg.GroupID != 0 {
			inbound.ChatType = bot.ChatGroup
			inbound.ChatID = fmt.Sprintf("%d", msg.GroupID)
		}
		select {
		case a.msgCh <- inbound:
		default:
		}
	}
}

func (a *adapter) Send(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	segments := []map[string]interface{}{}
	for _, p := range msg.MediaURLs {
		segments = append(segments, map[string]interface{}{
			"type": "image",
			"data": map[string]string{"file": p},
		})
	}
	if msg.Text != "" {
		segments = append(segments, map[string]interface{}{
			"type": "text",
			"data": map[string]string{"text": msg.Text},
		})
	}
	payload := map[string]interface{}{"message": segments}
	if msg.ChatType == bot.ChatGroup {
		var gid int64
		fmt.Sscanf(msg.ChatID, "%d", &gid)
		payload["group_id"] = gid
	} else {
		var uid int64
		fmt.Sscanf(msg.ChatID, "%d", &uid)
		payload["user_id"] = uid
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(a.httpURL+"/send_msg", "application/json", bytes.NewReader(body))
	if err != nil {
		return bot.SendResult{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Status string `json:"status"`
		MsgID  int64  `json:"message_id"`
	}
	json.Unmarshal(respBody, &result)
	return bot.SendResult{MessageID: fmt.Sprintf("%d", result.MsgID)}, nil
}

func (a *adapter) SendTyping(ctx context.Context, chatID string) error { return nil }

func extractText(raw json.RawMessage) string {
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		var text string
		for _, seg := range arr {
			if seg["type"] == "text" {
				if data, ok := seg["data"].(map[string]interface{}); ok {
					if t, ok := data["text"].(string); ok {
						text += t
					}
				}
			}
		}
		return text
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

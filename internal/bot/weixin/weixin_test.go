package weixin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

func TestStartReturnsMissingToken(t *testing.T) {
	isolateWeixinUserConfig(t)
	t.Setenv("WEIXIN_TEST_TOKEN", "")
	a := New(config.WeixinBotConfig{
		TokenEnv:  "WEIXIN_TEST_TOKEN",
		AccountID: "missing-account",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := a.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "WEIXIN_TEST_TOKEN") {
		t.Fatalf("Start error = %v, want missing token env", err)
	}
}

func TestSendTextPostsIlinkMessage(t *testing.T) {
	t.Setenv("WEIXIN_TEST_TOKEN", "token-1")
	var gotAuth string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sendMessagePath {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret":        0,
			"errcode":    0,
			"message_id": "wx-msg-1",
		})
	}))
	defer server.Close()

	result, err := SendText(context.Background(), config.WeixinBotConfig{
		TokenEnv: "WEIXIN_TEST_TOKEN",
		APIBase:  server.URL,
	}, "chat-1", "hello weixin")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if result.MessageID != "wx-msg-1" {
		t.Fatalf("message id = %q, want wx-msg-1", result.MessageID)
	}
	if gotAuth != "Bearer token-1" {
		t.Fatalf("Authorization = %q, want Bearer token-1", gotAuth)
	}
	msg, ok := gotPayload["msg"].(map[string]any)
	if !ok {
		t.Fatalf("payload msg = %#v, want object", gotPayload["msg"])
	}
	if msg["to_user_id"] != "chat-1" || msg["message_type"] != float64(weixinMsgTypeBot) || msg["message_state"] != float64(weixinMsgStateDone) {
		t.Fatalf("msg metadata = %#v", msg)
	}
	items, ok := msg["item_list"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("item_list = %#v, want one text item", msg["item_list"])
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["type"] != float64(weixinItemText) {
		t.Fatalf("item = %#v, want text item", items[0])
	}
	textItem, ok := item["text_item"].(map[string]any)
	if !ok || textItem["text"] != "hello weixin" {
		t.Fatalf("text item = %#v, want hello weixin", item["text_item"])
	}
}

func isolateWeixinUserConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
}

func TestLogPollHealthThrottlesEmptyPolls(t *testing.T) {
	a := &adapter{logger: slog.Default().With("platform", "weixin")}
	a.logPollHealth(ilinkResponse{})
	first := a.lastPollLog
	if first.IsZero() {
		t.Fatal("first empty poll should update heartbeat timestamp")
	}
	a.logPollHealth(ilinkResponse{})
	if !a.lastPollLog.Equal(first) {
		t.Fatalf("second empty poll updated heartbeat timestamp: got %v want %v", a.lastPollLog, first)
	}
	stale := time.Now().Add(-6 * time.Minute)
	a.lastPollLog = stale
	a.logPollHealth(ilinkResponse{})
	if !a.lastPollLog.After(stale) {
		t.Fatalf("stale empty poll did not refresh heartbeat timestamp: got %v after %v", a.lastPollLog, stale)
	}
}

func TestLogPollHealthLogsNonEmptyPolls(t *testing.T) {
	a := &adapter{logger: slog.Default().With("platform", "weixin")}
	a.logPollHealth(ilinkResponse{Msgs: []ilinkMessage{{MessageID: "msg-1"}}})
	if a.lastPollLog.IsZero() {
		t.Fatal("non-empty poll should update heartbeat timestamp")
	}
}

func TestGetUpdatesAcceptsNumericIlinkMessageID(t *testing.T) {
	t.Setenv("WEIXIN_TEST_TOKEN", "token-1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != getUpdatesPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret":     0,
			"errcode": 0,
			"msgs": []map[string]any{
				{
					"message_id":   123456789,
					"from_user_id": "wx-user-1",
					"to_user_id":   "bot-account",
					"msg_type":     1,
					"item_list": []map[string]any{
						{"type": weixinItemText, "text_item": map[string]string{"text": "hello"}},
					},
				},
			},
		})
	}))
	defer server.Close()

	a := &adapter{
		cfg: config.WeixinBotConfig{
			TokenEnv:  "WEIXIN_TEST_TOKEN",
			APIBase:   server.URL,
			AccountID: "bot-account",
		},
		logger:        slog.Default().With("platform", "weixin"),
		msgCh:         make(chan bot.InboundMessage, 1),
		contextTokens: make(map[string]string),
	}
	updates, err := a.getUpdates(context.Background())
	if err != nil {
		t.Fatalf("getUpdates: %v", err)
	}
	if len(updates) != 0 {
		t.Fatalf("updates = %d, want 0", len(updates))
	}
	select {
	case msg := <-a.msgCh:
		if msg.MessageID != "123456789" || msg.UserID != "wx-user-1" || msg.Text != "hello" {
			t.Fatalf("message = %+v, want numeric id converted and text preserved", msg)
		}
	case <-context.Background().Done():
		t.Fatal("unreachable")
	default:
		t.Fatal("expected queued inbound message")
	}
}

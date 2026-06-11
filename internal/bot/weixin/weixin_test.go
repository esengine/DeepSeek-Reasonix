package weixin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/config"
)

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

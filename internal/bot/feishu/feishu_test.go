package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/bot"
	"reasonix/internal/config"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestStartReturnsMissingWebSocketSecret(t *testing.T) {
	t.Setenv("FEISHU_TEST_SECRET", "")
	a := New(config.FeishuBotConfig{
		AppID:        "cli-test",
		AppSecretEnv: "FEISHU_TEST_SECRET",
		Mode:         "websocket",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := a.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "FEISHU_TEST_SECRET") {
		t.Fatalf("Start error = %v, want missing secret env", err)
	}
}

func TestVerificationTokenValidRequiresConfiguredToken(t *testing.T) {
	a := &adapter{cfg: config.FeishuBotConfig{VerificationToken: "expected"}}

	if a.verificationTokenValid("") {
		t.Fatal("missing token should be rejected when verification token is configured")
	}
	if a.verificationTokenValid("wrong") {
		t.Fatal("wrong token should be rejected")
	}
	if !a.verificationTokenValid("expected") {
		t.Fatal("matching token should be accepted")
	}

	a.cfg.VerificationToken = ""
	if !a.verificationTokenValid("") {
		t.Fatal("empty configured verification token should preserve unauthenticated mode")
	}
}

func TestMarkSeenConcurrent(t *testing.T) {
	a := &adapter{seen: make(map[string]bool)}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = a.markSeen(fmt.Sprintf("evt-%d", i%5))
		}(i)
	}
	wg.Wait()

	if got := len(a.seen); got != 5 {
		t.Fatalf("seen size = %d, want 5", got)
	}
	if a.markSeen("evt-1") != true {
		t.Fatal("second markSeen call should report duplicate")
	}
	if a.markSeen("") {
		t.Fatal("empty event id should not be treated as duplicate")
	}
}

func TestHandleCardActionUsesChatType(t *testing.T) {
	a := &adapter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	raw := []byte(`{
		"event": {
			"operator": {
				"operator_id": {"open_id": "open-user"}
			},
			"context": {
				"open_message_id": "msg-1",
				"open_chat_id": "chat-1"
			},
			"action": {
				"value": {
					"command": "/approve approval-1",
					"chat_type": "dm"
				}
			}
		}
	}`)

	if !a.handleCardAction(raw) {
		t.Fatal("handleCardAction returned false")
	}

	msg := <-a.msgCh
	if msg.ChatType != bot.ChatDM {
		t.Fatalf("chat type = %q, want %q", msg.ChatType, bot.ChatDM)
	}
	if msg.Text != "/approve approval-1" {
		t.Fatalf("text = %q, want /approve approval-1", msg.Text)
	}
}

func TestHandleCardActionEnqueuesAskAnswerCommand(t *testing.T) {
	a := &adapter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	raw := []byte(`{
		"event": {
			"operator": {
				"operator_id": {"open_id": "open-user"}
			},
			"context": {
				"open_message_id": "msg-ask",
				"open_chat_id": "chat-ask"
			},
			"action": {
				"value": {
					"command": "/answer ask-1 2",
					"chat_type": "dm",
					"user_id": "allowed-user"
				}
			}
		}
	}`)

	if !a.handleCardAction(raw) {
		t.Fatal("handleCardAction returned false")
	}

	msg := <-a.msgCh
	if msg.Text != "/answer ask-1 2" {
		t.Fatalf("text = %q, want /answer ask-1 2", msg.Text)
	}
	if msg.UserID != "allowed-user" {
		t.Fatalf("user id = %q, want allowed-user", msg.UserID)
	}
	if msg.OperatorID != "open-user" {
		t.Fatalf("operator id = %q, want open-user (the actual clicker, not the card requester)", msg.OperatorID)
	}
	if msg.ChatID != "chat-ask" || msg.MessageID != "msg-ask" {
		t.Fatalf("message routing = chat %q msg %q, want chat-ask/msg-ask", msg.ChatID, msg.MessageID)
	}
}

func TestHandleCardActionAcceptsDirectOperatorID(t *testing.T) {
	a := &adapter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	raw := []byte(`{
		"event": {
			"operator": {
				"open_id": "open-user-direct"
			},
			"context": {
				"open_message_id": "msg-1",
				"open_chat_id": "chat-1"
			},
			"action": {
				"value": {
					"command": "/approve approval-1",
					"chat_type": "dm"
				}
			}
		}
	}`)

	if !a.handleCardAction(raw) {
		t.Fatal("handleCardAction returned false")
	}

	msg := <-a.msgCh
	if msg.UserID != "open-user-direct" {
		t.Fatalf("user id = %q, want open-user-direct", msg.UserID)
	}
	if msg.OperatorID != "open-user-direct" {
		t.Fatalf("operator id = %q, want open-user-direct", msg.OperatorID)
	}
}

func TestHandleCardActionDoesNotTrustCardRequesterAsOperator(t *testing.T) {
	a := &adapter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	raw := []byte(`{
		"event": {
			"operator": {
				"operator_id": {"open_id": "clicker"}
			},
			"context": {
				"open_message_id": "msg-1",
				"open_chat_id": "chat-1"
			},
			"action": {
				"value": {
					"command": "/approve approval-1",
					"chat_type": "group",
					"user_id": "requester"
				}
			}
		}
	}`)

	if !a.handleCardAction(raw) {
		t.Fatal("handleCardAction returned false")
	}

	msg := <-a.msgCh
	if msg.UserID != "requester" {
		t.Fatalf("user id = %q, want requester (routing follows the card value)", msg.UserID)
	}
	if msg.OperatorID != "clicker" {
		t.Fatalf("operator id = %q, want clicker (gate follows the real button presser)", msg.OperatorID)
	}
}

func TestWebSocketDispatcherHandlesCardActionTrigger(t *testing.T) {
	a := &adapter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	raw := []byte(`{
		"schema": "2.0",
		"header": {
			"event_id": "evt-card-1",
			"event_type": "card.action.trigger",
			"token": ""
		},
		"event": {
			"operator": {
				"operator_id": {
					"open_id": "open-user",
					"union_id": "union-user"
				}
			},
			"context": {
				"open_message_id": "msg-card-1",
				"open_chat_id": "chat-card-1"
			},
			"action": {
				"value": {
					"command": "/approve approval-2",
					"chat_type": "dm",
					"user_id": "allowed-user"
				}
			}
		}
	}`)

	resp, err := a.newEventDispatcher().Do(context.Background(), raw)
	if err != nil {
		t.Fatalf("dispatcher.Do returned error: %v", err)
	}
	toast, ok := resp.(*callback.CardActionTriggerResponse)
	if !ok {
		t.Fatalf("response = %T, want *callback.CardActionTriggerResponse", resp)
	}
	if toast.Toast == nil || toast.Toast.Type != "success" {
		t.Fatalf("toast = %#v, want success toast", toast.Toast)
	}

	msg := <-a.msgCh
	if msg.Text != "/approve approval-2" {
		t.Fatalf("text = %q, want /approve approval-2", msg.Text)
	}
	if msg.ChatID != "chat-card-1" {
		t.Fatalf("chat id = %q, want chat-card-1", msg.ChatID)
	}
	if msg.UserID != "allowed-user" {
		t.Fatalf("user id = %q, want allowed-user", msg.UserID)
	}

	_, err = a.newEventDispatcher().Do(context.Background(), raw)
	if err != nil {
		t.Fatalf("duplicate dispatcher.Do returned error: %v", err)
	}
	select {
	case duplicate := <-a.msgCh:
		t.Fatalf("duplicate card action enqueued message: %#v", duplicate)
	default:
	}
}

func TestFeishuMarkdownPostContent(t *testing.T) {
	content, err := feishuMarkdownPostContent("hello [docs](https://example.com)")
	if err != nil {
		t.Fatalf("feishuMarkdownPostContent: %v", err)
	}
	var payload struct {
		ZhCn struct {
			Content [][]struct {
				Tag  string `json:"tag"`
				Text string `json:"text"`
				Href string `json:"href"`
			} `json:"content"`
		} `json:"zh_cn"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("post content should be valid json: %v", err)
	}
	if len(payload.ZhCn.Content) != 1 || len(payload.ZhCn.Content[0]) != 2 {
		t.Fatalf("content blocks = %#v, want one paragraph with text and link", payload.ZhCn.Content)
	}
	if payload.ZhCn.Content[0][0].Tag != "text" || payload.ZhCn.Content[0][0].Text != "hello " {
		t.Fatalf("first element = %#v, want text hello", payload.ZhCn.Content[0][0])
	}
	if payload.ZhCn.Content[0][1].Tag != "a" || payload.ZhCn.Content[0][1].Text != "docs" || payload.ZhCn.Content[0][1].Href != "https://example.com" {
		t.Fatalf("second element = %#v, want link", payload.ZhCn.Content[0][1])
	}
}

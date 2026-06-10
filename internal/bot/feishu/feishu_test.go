package feishu

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

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

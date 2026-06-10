package qq

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"reasonix/internal/bot"
)

func TestHandleDispatchDirectMessageUsesDirectChatType(t *testing.T) {
	a := &adapter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	raw, err := json.Marshal(map[string]any{
		"id":       "msg-1",
		"content":  "hello",
		"guild_id": "guild-1",
		"author": map[string]string{
			"id":       "user-1",
			"username": "user",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	a.handleDispatch(gatewayPayload{T: "DIRECT_MESSAGE_CREATE", D: raw})

	msg := <-a.msgCh
	if msg.ChatType != bot.ChatDirect {
		t.Fatalf("chat type = %q, want %q", msg.ChatType, bot.ChatDirect)
	}
	if msg.ChatID != "guild-1" {
		t.Fatalf("chat id = %q, want guild-1", msg.ChatID)
	}
}

func TestQQSendURLDirectMessage(t *testing.T) {
	got := qqSendURL(bot.OutboundMessage{ChatType: bot.ChatDirect, ChatID: "guild-1"})
	want := fmt.Sprintf(qqSendDirectURL, "guild-1")
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

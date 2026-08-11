package bot

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/sessioninbox"
)

func TestCollectAppendDeduplicatesPlatformRedelivery(t *testing.T) {
	dir := t.TempDir()
	ctrl := control.New(control.Options{
		SessionPath: filepath.Join(dir, "s.jsonl"),
		SessionDir:  dir,
		Sink:        event.Discard,
	})
	first := InboundMessage{
		Platform: PlatformFeishu, ChatType: ChatDM, ChatID: "chat-1",
		UserID: "user-1", MessageID: "msg-1", Text: "first request",
	}
	rec, err := enqueueViaInbox(ctrl, first, sessioninbox.IntentFollowup)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.MessageID = "msg-2"
	second.Text = "second request"
	for range 2 {
		got, err := collectAppend(ctrl, second, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if got.ItemID != rec.ItemID {
			t.Fatalf("collect item = %q, want %q", got.ItemID, rec.ItemID)
		}
	}
	_, env, err := ctrl.ReadInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(env.SubmitText, "second request") != 1 {
		t.Fatalf("redelivered collect body = %q", env.SubmitText)
	}
	recovered := botMessageFromEnvelope(env, InboundMessage{})
	if recovered.Platform != second.Platform || recovered.ChatID != second.ChatID || recovered.MessageID != second.MessageID {
		t.Fatalf("durable route metadata was not updated: %+v", recovered)
	}
}

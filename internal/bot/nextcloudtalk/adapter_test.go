package nextcloudtalk

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/bot"
)

func TestWebhookAcceptsSignedMessage(t *testing.T) {
	const envName = "REASONIX_TEST_NEXTCLOUD_TALK_SECRET"
	const secret = "test-shared-secret"
	t.Setenv(envName, secret)

	a := &adapter{
		cfg:    Config{SecretEnv: envName, ConnectionID: "nextcloud-talk-main"},
		logger: slog.Default(),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	body := []byte(`{
		"type":"Create",
		"actor":{"type":"Person","id":"users/ada-lovelace","name":"Ada Lovelace"},
		"object":{"type":"Note","id":"1567","name":"message","content":"{\"message\":\"hello Reasonix\"}","mediaType":"text/markdown"},
		"target":{"type":"Collection","id":"n3xtc10ud","name":"world"}
	}`)
	const random = "0123456789abcdef"
	req := httptest.NewRequest(http.MethodPost, defaultWebhookPath, strings.NewReader(string(body)))
	req.Header.Set("X-Nextcloud-Talk-Random", random)
	req.Header.Set("X-Nextcloud-Talk-Signature", signatureFor(secret, random, body))
	rec := httptest.NewRecorder()

	a.handleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	select {
	case msg := <-a.msgCh:
		if msg.Platform != bot.PlatformNextcloudTalk {
			t.Fatalf("platform = %q", msg.Platform)
		}
		if msg.ConnectionID != "nextcloud-talk-main" {
			t.Fatalf("connection id = %q", msg.ConnectionID)
		}
		if msg.ChatID != "n3xtc10ud" || msg.UserID != "ada-lovelace" {
			t.Fatalf("unexpected routing: chat=%q user=%q", msg.ChatID, msg.UserID)
		}
		if msg.Text != "hello Reasonix" || msg.MessageID != "1567" {
			t.Fatalf("unexpected message: text=%q id=%q", msg.Text, msg.MessageID)
		}
	default:
		t.Fatal("expected inbound message")
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	const envName = "REASONIX_TEST_NEXTCLOUD_TALK_SECRET"
	t.Setenv(envName, "test-shared-secret")
	a := &adapter{cfg: Config{SecretEnv: envName}, logger: slog.Default(), msgCh: make(chan bot.InboundMessage, 1)}
	req := httptest.NewRequest(http.MethodPost, defaultWebhookPath, strings.NewReader(`{"type":"Create"}`))
	req.Header.Set("X-Nextcloud-Talk-Random", "nonce")
	req.Header.Set("X-Nextcloud-Talk-Signature", "00")
	rec := httptest.NewRecorder()

	a.handleWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestSendSignsBodyAndSupportsReply(t *testing.T) {
	const envName = "REASONIX_TEST_NEXTCLOUD_TALK_SECRET"
	const secret = "send-shared-secret"
	t.Setenv(envName, secret)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/ocs/v2.php/apps/spreed/api/v1/bot/room-token/message") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		random := r.Header.Get("X-Nextcloud-Talk-Bot-Random")
		if got, want := r.Header.Get("X-Nextcloud-Talk-Bot-Signature"), signatureFor(secret, random, body); got != want {
			t.Fatalf("signature = %q, want %q", got, want)
		}
		if r.Header.Get("OCS-APIRequest") != "true" {
			t.Fatal("missing OCS-APIRequest header")
		}
		var payload struct {
			Message string `json:"message"`
			ReplyTo int    `json:"replyTo"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Message != "reply from Reasonix" || payload.ReplyTo != 1567 {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ocs":{"data":{"id":2048}}}`))
	}))
	defer server.Close()

	a := New(Config{ServerURL: server.URL, SecretEnv: envName}, slog.Default()).(*adapter)
	result, err := a.Send(context.Background(), bot.OutboundMessage{
		ChatID:       "room-token",
		Text:         "reply from Reasonix",
		ReplyToMsgID: "1567",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.MessageID != "2048" {
		t.Fatalf("message id = %q", result.MessageID)
	}
}

func TestActivityMessageIgnoresApplicationEvents(t *testing.T) {
	var event talkActivity
	if err := json.Unmarshal([]byte(`{
		"type":"Create",
		"actor":{"type":"Application","id":"bots/reasonix","name":"Reasonix"},
		"object":{"type":"Note","id":"42","name":"message","content":"{\"message\":\"echo\"}"},
		"target":{"type":"Collection","id":"room"}
	}`), &event); err != nil {
		t.Fatal(err)
	}
	if _, ok := activityMessage(event); ok {
		t.Fatal("application-authored event should be ignored")
	}
}

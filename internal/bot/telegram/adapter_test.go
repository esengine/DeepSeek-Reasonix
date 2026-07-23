package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/bot"
)

func TestAdapterInboundRules(t *testing.T) {
	a := newAdapter(Config{ConnectionID: "test"})
	a.botUser = user{ID: 9, Username: "ReasonixBot"}
	cases := []struct {
		name string
		u    update
		want bool
		text string
	}{
		{"private", update{Message: &message{MessageID: 1, From: &user{ID: 1}, Chat: chat{ID: 2, Type: "private"}, Text: "hello"}}, true, "hello"},
		{"mention", update{Message: &message{MessageID: 1, From: &user{ID: 1}, Chat: chat{ID: 2, Type: "group"}, Text: "@reasonixbot hello"}}, true, "hello"},
		{"addressed command", update{Message: &message{MessageID: 1, From: &user{ID: 1}, Chat: chat{ID: 2, Type: "group"}, Text: "/status@ReasonixBot"}}, true, "/status"},
		{"other bot command ignored", update{Message: &message{MessageID: 1, From: &user{ID: 1}, Chat: chat{ID: 2, Type: "group"}, Text: "/status@OtherBot"}}, false, ""},
		{"reply", update{Message: &message{MessageID: 1, From: &user{ID: 1}, Chat: chat{ID: 2, Type: "supergroup"}, Text: "hello", ReplyToMessage: &message{From: &user{ID: 9}}}}, true, "hello"},
		{"group ignored", update{Message: &message{From: &user{ID: 1}, Chat: chat{Type: "group"}, Text: "hello"}}, false, ""},
		{"channel ignored", update{Message: &message{From: &user{ID: 1}, Chat: chat{Type: "channel"}, Text: "hello"}}, false, ""},
		{"missing sender", update{Message: &message{Chat: chat{Type: "private"}, Text: "hello"}}, false, ""},
		{"edited ignored", update{EditedMessage: &message{MessageID: 3, From: &user{ID: 1}, Chat: chat{Type: "private"}, Text: "edited"}}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := a.inbound(tc.u)
			if ok != tc.want || got.Text != tc.text {
				t.Fatalf("inbound = (%+v, %v), want text %q, ok %v", got, ok, tc.text, tc.want)
			}
		})
	}
}

func TestSplitTextIsRuneSafeAndPrefersBoundaries(t *testing.T) {
	text := strings.Repeat("段落内容", 999) + "\n\n" + strings.Repeat("🙂", 20)
	chunks := splitText(text)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if !strings.HasSuffix(chunks[0], "\n\n") || strings.HasPrefix(chunks[1], "\n") {
		t.Fatalf("paragraph boundary not preserved")
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > maxMessageRunes || strings.ToValidUTF8(chunk, "") != chunk {
			t.Fatalf("invalid chunk length or UTF-8")
		}
	}
	if strings.Join(chunks, "") != text {
		t.Fatal("split chunks did not preserve text")
	}
}

func TestSendReturnsDeliveredIDsOnPartialFailure(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":502,"description":"failed"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 11}})
	}))
	defer srv.Close()
	a := newAdapter(Config{Token: "secret", APIBaseURL: srv.URL, HTTPClient: srv.Client()})
	result, err := a.Send(context.Background(), bot.OutboundMessage{ChatID: "1", Text: strings.Repeat("x", 4001)})
	if err == nil || len(result.MessageIDs) != 1 || result.MessageIDs[0] != "11" {
		t.Fatalf("result=%+v err=%v, want first delivered ID and error", result, err)
	}
}

func TestSendSplitsRepliesOnlyFirstAndKeepsThread(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		requests = append(requests, request)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": len(requests)}})
	}))
	defer srv.Close()
	a := newAdapter(Config{Token: "secret", APIBaseURL: srv.URL, HTTPClient: srv.Client()})
	result, err := a.Send(context.Background(), bot.OutboundMessage{ChatID: "1", Text: strings.Repeat("界", 4001), ReplyToMsgID: "7", ThreadID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.MessageIDs, []string{"1", "2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if _, ok := requests[0]["reply_to_message_id"]; !ok {
		t.Fatal("first message did not reply")
	}
	if _, ok := requests[1]["reply_to_message_id"]; ok {
		t.Fatal("later message unexpectedly replied")
	}
	for i, request := range requests {
		if request["message_thread_id"] != float64(42) {
			t.Fatalf("request %d thread = %v, want 42", i, request["message_thread_id"])
		}
	}
}

func TestStartValidatesAndStopCancelsPoll(t *testing.T) {
	pollStarted := make(chan struct{})
	var started sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"id": 9, "is_bot": true, "username": "bot"}})
		case strings.HasSuffix(r.URL.Path, "/deleteWebhook"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			started.Do(func() { close(pollStarted) })
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()
	a := newAdapter(Config{Token: "secret", APIBaseURL: srv.URL, HTTPClient: srv.Client()})
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pollStarted:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
}

type failingDoer struct {
	err error
}

func (d failingDoer) Do(*http.Request) (*http.Response, error) {
	return nil, d.err
}

func TestTransportErrorsDoNotContainToken(t *testing.T) {
	token := "very-secret-token"
	_, err := newClient(token, "https://api.telegram.org", failingDoer{err: errors.New("request https://api.telegram.org/bot" + token + "/getMe failed")}).getMe(context.Background())
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked in transport error: %v", err)
	}
}

func TestWaitAfterErrorHonorsRetryAndPermanentErrors(t *testing.T) {
	a := newAdapter(Config{})
	ctx := context.Background()
	started := time.Now()
	if stop := a.waitAfterError(ctx, &apiError{code: 429, retryAfter: 20 * time.Millisecond}); stop || time.Since(started) < 15*time.Millisecond {
		t.Fatalf("429 wait stop=%v elapsed=%s", stop, time.Since(started))
	}
	if !a.waitAfterError(ctx, &apiError{code: 401}) || !a.waitAfterError(ctx, &apiError{code: 409}) {
		t.Fatal("permanent Telegram errors did not stop polling")
	}
}

func TestClientErrorsDoNotContainToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
	}))
	defer srv.Close()
	_, err := newClient("very-secret-token", srv.URL, srv.Client()).getMe(context.Background())
	if err == nil || strings.Contains(err.Error(), "very-secret-token") {
		t.Fatalf("token leaked in error: %v", err)
	}
}

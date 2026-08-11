package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"

	"github.com/gorilla/websocket"
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

func TestHandleDispatchC2CUsesUserOpenID(t *testing.T) {
	a := &adapter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	raw, err := json.Marshal(map[string]any{
		"id":      "msg-1",
		"content": "hello",
		"author": map[string]string{
			"user_openid": "openid-user",
			"username":    "user",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	a.handleDispatch(gatewayPayload{T: "C2C_MESSAGE_CREATE", D: raw})

	msg := <-a.msgCh
	if msg.UserID != "openid-user" {
		t.Fatalf("user id = %q, want openid-user", msg.UserID)
	}
	if msg.ChatID != "openid-user" {
		t.Fatalf("chat id = %q, want openid-user", msg.ChatID)
	}
	if msg.ChatType != bot.ChatDM {
		t.Fatalf("chat type = %q, want %q", msg.ChatType, bot.ChatDM)
	}
}

func TestHandleDispatchGroupUsesMemberOpenIDForActor(t *testing.T) {
	a := &adapter{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), msgCh: make(chan bot.InboundMessage, 1)}
	raw, err := json.Marshal(map[string]any{
		"id": "msg-group", "content": "@bot hello", "group_openid": "group-1",
		"author": map[string]string{"user_openid": "global-user", "member_openid": "group-member", "username": "member"},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.handleDispatch(gatewayPayload{T: "GROUP_AT_MESSAGE_CREATE", D: raw})
	msg := <-a.msgCh
	if msg.ChatID != "group-1" || msg.UserID != "group-member" || msg.ChatType != bot.ChatGroup {
		t.Fatalf("message = %+v, want group conversation/member actor", msg)
	}
}

func TestHandleDispatchCarriesAttachments(t *testing.T) {
	a := &adapter{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), msgCh: make(chan bot.InboundMessage, 1)}
	raw := []byte(`{"id":"msg-media","content":"see this","author":{"user_openid":"user-1"},"attachments":[{"url":"https://cdn.example/image.png","filename":"image.png"}]}`)
	if !a.handleDispatch(gatewayPayload{T: "C2C_MESSAGE_CREATE", D: raw}) {
		t.Fatal("handleDispatch returned false")
	}
	msg := <-a.msgCh
	if len(msg.MediaURLs) != 1 || msg.MediaURLs[0] != "https://cdn.example/image.png" {
		t.Fatalf("media urls = %+v", msg.MediaURLs)
	}
}

func TestHandleInteractionCarriesSourceMessageIDForAuthorization(t *testing.T) {
	a := &adapter{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:         make(chan bot.InboundMessage, 1),
		interactionCh: make(chan bot.Interaction, 1),
	}
	raw := []byte(`{"id":"interaction-1","message_id":"keyboard-message-1","group_openid":"","chat_type":"c2c","user":{"user_openid":"owner-1"},"data":{"resolved":{"button_id":"rx:opaque"}}}`)
	if !a.handleInteraction(gatewayPayload{T: "INTERACTION_CREATE", D: raw}) {
		t.Fatal("handleInteraction returned false")
	}
	interaction := <-a.interactionCh
	if interaction.MessageID != "keyboard-message-1" || interaction.CallbackID != "rx:opaque" {
		t.Fatalf("interaction = %+v, want source message ID and opaque callback", interaction)
	}
	message := <-a.msgCh
	if got, ok := message.Raw.(bot.Interaction); !ok || got.MessageID != "keyboard-message-1" {
		t.Fatalf("inbound interaction raw = %#v, want source message ID", message.Raw)
	}
}

func TestQQDefaultIntentsDoNotRequestLegacyGuildMessages(t *testing.T) {
	got := qqDefaultIntents(config.QQBotConfig{})
	if got&(1<<9) != 0 {
		t.Fatalf("default intents = %b, unexpectedly includes GUILD_MESSAGES (1<<9)", got)
	}
	if got != (qqIntentGroupAndC2C | qqIntentInteraction) {
		t.Fatalf("default intents = %b, want group/c2c + interaction", got)
	}
	if guild := qqDefaultIntents(config.QQBotConfig{IntentProfile: "guild"}); guild&(1<<30) == 0 || guild&(1<<12) == 0 {
		t.Fatalf("guild profile intents = %b, want public guild and direct-message bits", guild)
	}
}

func TestQQRetryScheduleIsBounded(t *testing.T) {
	for _, tt := range []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 5 * time.Second},
		{attempt: 3, want: 10 * time.Second},
		{attempt: 4, want: 30 * time.Second},
	} {
		if got := qqNextRetryDelay(time.Second, tt.attempt); got != tt.want {
			t.Fatalf("attempt %d delay = %s, want %s", tt.attempt, got, tt.want)
		}
	}
	if got := qqNextRetryDelay(5*time.Minute, 99); got != 5*time.Minute {
		t.Fatalf("max retry delay = %s, want 5m", got)
	}
}

func TestQQFatalAuthErrorStopsRetry(t *testing.T) {
	if !qqFatalAuthError(fmt.Errorf("qq token api error 401: invalid client")) {
		t.Fatal("401 token error not classified fatal")
	}
	if qqFatalAuthError(fmt.Errorf("qq token api error 500: temporary")) {
		t.Fatal("temporary token error classified fatal")
	}
}

func TestQQFatalConfigurationErrorStopsRetry(t *testing.T) {
	if !qqFatalConfigurationError(fmt.Errorf("qq app_id is empty")) {
		t.Fatal("missing app id not classified fatal")
	}
	if qqFatalConfigurationError(fmt.Errorf("temporary gateway timeout")) {
		t.Fatal("temporary network error classified fatal")
	}
}

func TestQQGatewayCloseCodesAreClassified(t *testing.T) {
	for _, code := range []int{4914, 4915} {
		err := classifyQQGatewayReadError("read ready", &websocket.CloseError{Code: code, Text: "intent unauthorized"})
		var ge *qqGatewayError
		if !errors.As(err, &ge) || !ge.fatal || ge.code != code {
			t.Fatalf("close %d classified as %#v, want fatal", code, err)
		}
	}
	err := classifyQQGatewayReadError("read gateway message", &websocket.CloseError{Code: 4008, Text: "rate limited"})
	var ge *qqGatewayError
	if !errors.As(err, &ge) || ge.fatal || ge.retryAfter < time.Minute {
		t.Fatalf("close 4008 classified as %#v, want >=1m retry", err)
	}
}

func TestQQSendURLDirectMessage(t *testing.T) {
	got := qqSendURL(bot.OutboundMessage{ChatType: bot.ChatDirect, ChatID: "guild-1"})
	want := fmt.Sprintf("%s/v2/dms/%s/messages", qqBaseURL, "guild-1")
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestQQSendURLUsesSandboxBase(t *testing.T) {
	a := &adapter{cfg: config.QQBotConfig{Sandbox: true}}
	got := a.qqSendURL(bot.OutboundMessage{ChatType: bot.ChatDM, ChatID: "user/open id"})
	want := qqSandboxURL + "/v2/users/user%2Fopen%20id/messages"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestValidateGatewayURL(t *testing.T) {
	for _, raw := range []string{
		"wss://api.sgroup.qq.com/websocket",
		"wss://sandbox.api.sgroup.qq.com/websocket",
		"wss://gateway.qq.com/websocket",
	} {
		if _, err := validateGatewayURL(raw); err != nil {
			t.Fatalf("valid gateway %q rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://api.sgroup.qq.com/websocket",
		"wss://evil.example/websocket",
		"wss://api.sgroup.qq.com/websocket?token=1",
		"wss://user:pass@api.sgroup.qq.com/websocket",
	} {
		if _, err := validateGatewayURL(raw); err == nil {
			t.Fatalf("invalid gateway %q accepted", raw)
		}
	}
}

func TestNormalizeQQMarkdownReply(t *testing.T) {
	got := normalizeQQMarkdownReply("```markdown\n# Title\n\n**bold**\n```")
	if got != "# Title\n\n**bold**" {
		t.Fatalf("normalized markdown = %q", got)
	}
	normal := "Here is code:\n```go\nfmt.Println()\n```"
	if got := normalizeQQMarkdownReply(normal); got != normal {
		t.Fatalf("normal code block changed: %q", got)
	}
}

func TestSplitQQMessageKeepsUTF8Budget(t *testing.T) {
	chunks := splitQQMessage(strings.Repeat("中", 600), 1500)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want more than one", len(chunks))
	}
	for _, chunk := range chunks {
		if len([]byte(chunk)) > 1500 {
			t.Fatalf("chunk byte length = %d, want <= 1500", len([]byte(chunk)))
		}
	}
}

func TestFitUTF8SliceKeepsGraphemeCluster(t *testing.T) {
	cluster := "👨‍👩‍👧‍👦"
	got := fitUTF8Slice(cluster+"!", len([]byte(cluster)))
	if got != cluster {
		t.Fatalf("fitUTF8Slice split grapheme cluster: %q", got)
	}
}

func TestCapQQPassiveReplyChunks(t *testing.T) {
	chunks := splitQQMessage(strings.Repeat("chunk-", 1800), qqMaxChunkBytes)
	if len(chunks) <= qqMaxPassiveReplyChunks {
		t.Fatalf("chunks = %d, want more than passive reply limit", len(chunks))
	}

	got, truncated := capQQPassiveReplyChunks(bot.OutboundMessage{
		ChatType:     bot.ChatDM,
		ReplyToMsgID: "msg-id",
	}, chunks)
	if !truncated {
		t.Fatal("capQQPassiveReplyChunks truncated = false, want true")
	}
	if len(got) != qqMaxPassiveReplyChunks {
		t.Fatalf("capped chunks = %d, want %d", len(got), qqMaxPassiveReplyChunks)
	}
	for _, chunk := range got {
		if len([]byte(chunk)) > qqMaxChunkBytes {
			t.Fatalf("chunk byte length = %d, want <= %d", len([]byte(chunk)), qqMaxChunkBytes)
		}
	}
	if !strings.Contains(got[len(got)-1], "Truncated") {
		t.Fatalf("last chunk = %q, want truncation notice", got[len(got)-1])
	}
}

func TestCapQQPassiveReplyChunksDoesNotCapNonPassiveReplies(t *testing.T) {
	chunks := splitQQMessage(strings.Repeat("chunk-", 1800), qqMaxChunkBytes)
	got, truncated := capQQPassiveReplyChunks(bot.OutboundMessage{
		ChatType: bot.ChatDM,
	}, chunks)
	if truncated {
		t.Fatal("capQQPassiveReplyChunks truncated non-passive reply")
	}
	if len(got) != len(chunks) {
		t.Fatalf("chunks = %d, want %d", len(got), len(chunks))
	}

	got, truncated = capQQPassiveReplyChunks(bot.OutboundMessage{
		ChatType:     bot.ChatDirect,
		ReplyToMsgID: "msg-id",
	}, chunks)
	if truncated {
		t.Fatal("capQQPassiveReplyChunks truncated direct/guild reply")
	}
	if len(got) != len(chunks) {
		t.Fatalf("chunks = %d, want %d", len(got), len(chunks))
	}
}

func TestStartValidatesQQCredentialsBeforeRunning(t *testing.T) {
	a := &adapter{}
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("Start() error = nil, want missing app_id error")
	}
	if a.cancel != nil {
		t.Fatal("Start() installed runtime cancel after validation failure")
	}
}

func TestQQExpiresInSecondsAcceptsNumberAndString(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		want  int
	}{
		{name: "number", value: float64(3600), want: 3600},
		{name: "string", value: "7200", want: 7200},
		{name: "blank", value: "", want: 0},
		{name: "missing", value: nil, want: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := qqExpiresInSeconds(tt.value)
			if err != nil {
				t.Fatalf("qqExpiresInSeconds() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("qqExpiresInSeconds() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSendMessageMarkdownFallbackDisablesMarkdown(t *testing.T) {
	t.Setenv("QQ_BOT_APP_SECRET", "secret")
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()

	var bodies []map[string]any
	sendCount := 0
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "bots.qq.com":
			return jsonResponse(200, map[string]any{"access_token": "token", "expires_in": 3600}), nil
		case "api.sgroup.qq.com":
			if req.Header.Get("Authorization") != "QQBot token" {
				t.Fatalf("authorization = %q, want QQBot token", req.Header.Get("Authorization"))
			}
			if req.Header.Get("X-Union-Appid") != "app-id" {
				t.Fatalf("x-union-appid = %q, want app-id", req.Header.Get("X-Union-Appid"))
			}
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			bodies = append(bodies, body)
			sendCount++
			if sendCount == 1 {
				return jsonResponse(400, map[string]any{"message": "markdown rejected"}), nil
			}
			return jsonResponse(200, map[string]any{"id": fmt.Sprintf("sent-%d", sendCount)}), nil
		default:
			t.Fatalf("unexpected request host: %s", req.URL.Host)
			return nil, nil
		}
	})

	a := &adapter{
		cfg:    config.QQBotConfig{AppID: "app-id", AppSecretEnv: "QQ_BOT_APP_SECRET"},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatType:     bot.ChatDM,
		ChatID:       "openid-user",
		Text:         "**bold**",
		ReplyToMsgID: "msg-id",
	})
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	_, err = a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatType:     bot.ChatDM,
		ChatID:       "openid-user",
		Text:         "**next**",
		ReplyToMsgID: "msg-id-2",
	})
	if err != nil {
		t.Fatalf("second send message: %v", err)
	}
	if len(bodies) != 3 {
		t.Fatalf("sent bodies = %d, want 3", len(bodies))
	}
	if bodies[0]["msg_type"] != float64(2) || bodies[0]["markdown"] == nil || bodies[0]["msg_seq"] != float64(1) {
		t.Fatalf("first body = %#v, want markdown msg_seq=1", bodies[0])
	}
	if bodies[1]["msg_type"] != float64(0) || bodies[1]["content"] != "**bold**" || bodies[1]["msg_seq"] != float64(2) {
		t.Fatalf("fallback body = %#v, want plain msg_seq=2", bodies[1])
	}
	if bodies[2]["msg_type"] != float64(0) || bodies[2]["content"] != "**next**" || bodies[2]["msg_seq"] != float64(3) {
		t.Fatalf("second body = %#v, want plain msg_seq=3", bodies[2])
	}
}

func TestSendMessageReturnsAllChunkIDs(t *testing.T) {
	t.Setenv("QQ_BOT_APP_SECRET", "secret")
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()

	sendCount := 0
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "bots.qq.com":
			return jsonResponse(200, map[string]any{"access_token": "token", "expires_in": 3600}), nil
		case "api.sgroup.qq.com":
			sendCount++
			return jsonResponse(200, map[string]any{"id": fmt.Sprintf("sent-%d", sendCount)}), nil
		default:
			t.Fatalf("unexpected request host: %s", req.URL.Host)
			return nil, nil
		}
	})

	text := strings.Repeat("chunk-", 1800)
	wantChunks := len(splitQQMessage(text, qqMaxChunkBytes))
	a := &adapter{
		cfg:    config.QQBotConfig{AppID: "app-id", AppSecretEnv: "QQ_BOT_APP_SECRET"},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	result, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatType: bot.ChatDM,
		ChatID:   "openid-user",
		Text:     text,
	})
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if len(result.MessageIDs) != wantChunks {
		t.Fatalf("message IDs = %v, want %d chunk IDs", result.MessageIDs, wantChunks)
	}
	if result.MessageID != fmt.Sprintf("sent-%d", wantChunks) {
		t.Fatalf("compatibility message ID = %q, want last chunk", result.MessageID)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, v any) *http.Response {
	data, _ := json.Marshal(v)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

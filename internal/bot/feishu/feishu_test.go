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
	if a.verificationTokenValid("") {
		t.Fatal("unconfigured verification token should deny all callers")
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

func TestHandleMessageTreatsTopicGroupAsGroup(t *testing.T) {
	a := &adapter{
		cfg:    config.FeishuBotConfig{RequireMention: true},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	a.handleMessage(context.Background(), feishuMsgEvent{
		MessageID: "msg-topic",
		ChatID:    "chat-topic",
		ChatType:  "topic_group",
		MsgType:   "text",
		Content:   `{"text":"hello"}`,
		Sender: feishuSender{SenderID: struct {
			UserID  string `json:"user_id"`
			OpenID  string `json:"open_id"`
			UnionID string `json:"union_id"`
		}{OpenID: "open-user"}},
		Mentions: []feishuMention{{Key: "@_user_1"}},
	})

	msg := <-a.msgCh
	if msg.ChatType != bot.ChatGroup {
		t.Fatalf("chat type = %q, want group", msg.ChatType)
	}
	if msg.ChatID != "chat-topic" || msg.UserID != "open-user" {
		t.Fatalf("message = %+v, want topic group routing", msg)
	}
}

func TestHandleMessageRequiresMentionInTopicGroup(t *testing.T) {
	a := &adapter{
		cfg:    config.FeishuBotConfig{RequireMention: true},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:  make(chan bot.InboundMessage, 1),
	}
	a.handleMessage(context.Background(), feishuMsgEvent{
		MessageID: "msg-topic",
		ChatID:    "chat-topic",
		ChatType:  "topic_group",
		MsgType:   "text",
		Content:   `{"text":"hello"}`,
		Sender: feishuSender{SenderID: struct {
			UserID  string `json:"user_id"`
			OpenID  string `json:"open_id"`
			UnionID string `json:"union_id"`
		}{OpenID: "open-user"}},
	})

	select {
	case msg := <-a.msgCh:
		t.Fatalf("message without mention was queued: %+v", msg)
	default:
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

// pngHeader 是合法 PNG 签名，足够 http.DetectContentType 识别为 image/png。
var pngHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}

func newTestAdapter(fetch func(ctx context.Context, messageID, key, typ string) ([]byte, string, error)) *adapter {
	return &adapter{
		cfg:           config.FeishuBotConfig{},
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		msgCh:         make(chan bot.InboundMessage, 1),
		fetchResource: fetch,
	}
}

func testSender(openID string) feishuSender {
	return feishuSender{SenderID: struct {
		UserID  string `json:"user_id"`
		OpenID  string `json:"open_id"`
		UnionID string `json:"union_id"`
	}{OpenID: openID}}
}

func TestHandleMessageDefersImageDownload(t *testing.T) {
	fetchCalls := 0
	a := newTestAdapter(func(ctx context.Context, messageID, key, typ string) ([]byte, string, error) {
		fetchCalls++
		if messageID != "msg-img" || key != "img-key-1" || typ != "image" {
			t.Fatalf("fetch args = %s/%s/%s, want msg-img/img-key-1/image", messageID, key, typ)
		}
		return pngHeader, "", nil
	})
	a.handleMessage(context.Background(), feishuMsgEvent{
		MessageID: "msg-img",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		MsgType:   "image",
		Content:   `{"image_key":"img-key-1"}`,
		Sender:    testSender("open-user"),
	})

	msg := <-a.msgCh
	if len(msg.Media) != 1 {
		t.Fatalf("media items = %d, want 1", len(msg.Media))
	}
	if fetchCalls != 0 {
		t.Fatalf("resource fetched %d times before gateway admission, want 0", fetchCalls)
	}
	data, _, err := msg.Media[0].Load(context.Background())
	if err != nil || !strings.HasPrefix(string(data), string(pngHeader)) {
		t.Fatalf("deferred load = %x, %v; want png bytes", data, err)
	}
	if fetchCalls != 1 {
		t.Fatalf("resource fetched %d times after load, want 1", fetchCalls)
	}
}

func TestHandleMessageFileDownloadFailureKeepsDeferredPlaceholder(t *testing.T) {
	fetchCalls := 0
	a := newTestAdapter(func(ctx context.Context, messageID, key, typ string) ([]byte, string, error) {
		fetchCalls++
		return nil, "", fmt.Errorf("boom")
	})
	a.handleMessage(context.Background(), feishuMsgEvent{
		MessageID: "msg-file",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		MsgType:   "file",
		Content:   `{"file_key":"file-key-1","file_name":"report.pdf"}`,
		Sender:    testSender("open-user"),
	})

	msg := <-a.msgCh
	if len(msg.Media) != 1 || fetchCalls != 0 {
		t.Fatalf("media items/fetches = %d/%d, want one deferred item and no pre-admission fetch", len(msg.Media), fetchCalls)
	}
	if _, _, err := msg.Media[0].Load(context.Background()); err == nil {
		t.Fatal("deferred load should report the injected failure")
	}
	if !strings.Contains(msg.Media[0].FailureText, "report.pdf") {
		t.Fatalf("fallback = %q, want download-failure placeholder naming the file", msg.Media[0].FailureText)
	}
}

func TestHandleMessageParsesPostContent(t *testing.T) {
	fetchCalls := 0
	a := newTestAdapter(func(ctx context.Context, messageID, key, typ string) ([]byte, string, error) {
		fetchCalls++
		if key != "post-img-1" || typ != "image" {
			t.Fatalf("fetch args = %s/%s, want post-img-1/image", key, typ)
		}
		return pngHeader, "", nil
	})
	a.handleMessage(context.Background(), feishuMsgEvent{
		MessageID: "msg-post",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		MsgType:   "post",
		Content:   `{"title":"周报","content":[[{"tag":"text","text":"进展见 "},{"tag":"a","text":"文档","href":"https://example.com/doc"},{"tag":"at","user_name":"张三"}],[{"tag":"img","image_key":"post-img-1"}]]}`,
		Sender:    testSender("open-user"),
	})

	msg := <-a.msgCh
	for _, want := range []string{"周报", "进展见", "文档 (https://example.com/doc)", "@张三"} {
		if !strings.Contains(msg.Text, want) {
			t.Fatalf("text = %q, want it to contain %q", msg.Text, want)
		}
	}
	if len(msg.Media) != 1 {
		t.Fatalf("media items = %d, want one deferred embedded image", len(msg.Media))
	}
	if fetchCalls != 0 {
		t.Fatalf("post image fetched %d times before gateway admission, want 0", fetchCalls)
	}
}

func TestHandleMessageUnsupportedTypeIgnored(t *testing.T) {
	a := newTestAdapter(nil)
	a.handleMessage(context.Background(), feishuMsgEvent{
		MessageID: "msg-audio",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		MsgType:   "audio",
		Content:   `{"file_key":"audio-key"}`,
		Sender:    testSender("open-user"),
	})

	select {
	case msg := <-a.msgCh:
		t.Fatalf("unsupported message type was queued: %+v", msg)
	default:
	}
}

func TestReplaceMentionPlaceholdersStripsBotAndNamesOthers(t *testing.T) {
	a := newTestAdapter(nil)
	a.botID = "ou-bot"
	got := a.replaceMentionPlaceholders("@_user_1 帮 @_user_2 看看这个", []mentionRef{
		{Key: "@_user_1", OpenID: "ou-bot", Name: "Reasonix"},
		{Key: "@_user_2", OpenID: "ou-zhang", Name: "张三"},
	})
	if got != "帮 @张三 看看这个" {
		t.Fatalf("text = %q, want bot mention stripped and peer mention named", got)
	}
}

func TestMentionGatingRequiresBotWhenIdentityKnown(t *testing.T) {
	a := newTestAdapter(nil)
	a.cfg.RequireMention = true
	a.botID = "ou-bot"
	a.handleMessage(context.Background(), feishuMsgEvent{
		MessageID: "msg-other",
		ChatID:    "chat-group",
		ChatType:  "group",
		MsgType:   "text",
		Content:   `{"text":"@_user_1 在吗"}`,
		Sender:    testSender("open-user"),
		Mentions: []feishuMention{{Key: "@_user_1", Name: "张三", ID: struct {
			OpenID string `json:"open_id"`
		}{OpenID: "ou-zhang"}}},
	})

	select {
	case msg := <-a.msgCh:
		t.Fatalf("message mentioning someone else was queued: %+v", msg)
	default:
	}
}

func TestBuildMarkdownCard(t *testing.T) {
	content, err := buildMarkdownCard("hello [docs](https://example.com)")
	if err != nil {
		t.Fatalf("buildMarkdownCard: %v", err)
	}
	var payload struct {
		Schema string `json:"schema"`
		Config struct {
			UpdateMulti bool `json:"update_multi"`
		} `json:"config"`
		Body struct {
			Elements []struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("card content should be valid json: %v", err)
	}
	if payload.Schema != "2.0" {
		t.Fatalf("schema = %q, want 2.0", payload.Schema)
	}
	// update_multi must be set or Im.Message.Patch (streaming) is rejected.
	if !payload.Config.UpdateMulti {
		t.Fatal("card config.update_multi = false, want true so the card is patchable")
	}
	if len(payload.Body.Elements) != 1 || payload.Body.Elements[0].Tag != "markdown" {
		t.Fatalf("elements = %#v, want one markdown element", payload.Body.Elements)
	}
	if payload.Body.Elements[0].Content != "hello [docs](https://example.com)" {
		t.Fatalf("content = %q, want original markdown", payload.Body.Elements[0].Content)
	}
}

func TestReplyFallbackOnlyForRecalledMessage(t *testing.T) {
	if isReplyFallbackError(fmt.Errorf("i/o timeout")) {
		t.Fatal("ambiguous transport errors must not fall back to Create")
	}
	if isReplyFallbackError(&feishuAPIError{op: "reply", code: 230013, msg: "no availability"}) {
		t.Fatal("permission errors must not fall back to Create")
	}
	if !isReplyFallbackError(&feishuAPIError{op: "reply", code: feishuReplyRecalledCode, msg: "recalled"}) {
		t.Fatal("a recalled target should fall back to Create")
	}
}

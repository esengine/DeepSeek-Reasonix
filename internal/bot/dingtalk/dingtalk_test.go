package dingtalk

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

func newTestHTTPClient() *http.Client {
	return &http.Client{}
}

// allowTestWebhook 把 httptest 桩 server 的 host 加入 webhook 白名单，
// 并仅为该 adapter 允许 HTTP，使发送测试可以打到桩地址。
func allowTestWebhook(t *testing.T, a *adapter, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse httptest url %q: %v", srv.URL, err)
	}
	a.webhookHosts = append(a.webhookHosts, u.Hostname())
	a.allowHTTPWebhook = true
}

func testAdapter(cfg config.DingtalkBotConfig) *adapter {
	return &adapter{
		cfg:          cfg,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		seen:         make(map[string]bool),
		webhooks:     make(map[string]string),
		msgChats:     make(map[string]string),
		httpClient:   newTestHTTPClient(),
		webhookHosts: append([]string(nil), dingtalkWebhookHosts...),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNormalizeDirectMessage(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{})
	msg := a.normalizeMessage(robotMessage{
		SenderStaffID:    "user-1",
		SenderNick:       "张三",
		ConversationID:   "cid-123",
		ConversationType: "1",
		MsgID:            "msg-1",
		MsgType:          "text",
		Text:             &robotTextContent{Content: "你好"},
		SessionWebhook:   "https://webhook/1",
	})
	if msg == nil {
		t.Fatal("direct message should be accepted")
	}
	if msg.Platform != bot.PlatformDingtalk {
		t.Fatalf("platform = %q, want dingtalk", msg.Platform)
	}
	if msg.ChatType != bot.ChatDM {
		t.Fatalf("chat type = %q, want dm", msg.ChatType)
	}
	if msg.ChatID != "cid-123" || msg.UserID != "user-1" || msg.Text != "你好" {
		t.Fatalf("unexpected message fields: %+v", msg)
	}
	if msg.UserName != "张三" {
		t.Fatalf("user name = %q, want 张三", msg.UserName)
	}
}

func TestNormalizeGroupStripsMention(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{BotName: "我的助手"})
	msg := a.normalizeMessage(robotMessage{
		SenderStaffID:    "user-1",
		SenderNick:       "李四",
		ConversationID:   "cid-grp",
		ConversationType: "2",
		MsgID:            "msg-g1",
		MsgType:          "text",
		Text:             &robotTextContent{Content: "@我的助手 今天天气如何"},
		SessionWebhook:   "https://webhook/2",
	})
	if msg == nil {
		t.Fatal("group message mentioning the bot should be accepted")
	}
	if msg.ChatType != bot.ChatGroup {
		t.Fatalf("chat type = %q, want group", msg.ChatType)
	}
	if msg.Text != "今天天气如何" {
		t.Fatalf("text = %q, want mention stripped", msg.Text)
	}
}

func TestNormalizeGroupRequiresMention(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{RequireMention: true, BotName: "我的助手"})
	// 未 @ 机器人 → 拒绝。
	plain := a.normalizeMessage(robotMessage{
		ConversationID:   "cid-grp",
		ConversationType: "2",
		MsgID:            "msg-g2",
		Text:             &robotTextContent{Content: "普通消息"},
	})
	if plain != nil {
		t.Fatal("group message without @bot should be rejected when require_mention is set")
	}
	// 单个 @ 机器人 → 剥离后为空文本。
	only := a.normalizeMessage(robotMessage{
		ConversationID:   "cid-grp",
		ConversationType: "2",
		MsgID:            "msg-g3",
		Text:             &robotTextContent{Content: "@我的助手"},
	})
	if only == nil || only.Text != "" {
		t.Fatalf("bare mention should pass gating with empty text, got %+v", only)
	}
	// 官方回调 isInAtList=true、正文无前导 @token → 按结构化字段放行。
	structured := a.normalizeMessage(robotMessage{
		ConversationID:   "cid-grp",
		ConversationType: "2",
		MsgID:            "msg-g4",
		Text:             &robotTextContent{Content: "今天天气如何"},
		IsInAtList:       true,
	})
	if structured == nil {
		t.Fatal("group message with isInAtList=true should pass gating even without leading @token")
	}
	if structured.Text != "今天天气如何" {
		t.Fatalf("text = %q, want original content (no mention prefix to strip)", structured.Text)
	}
	// isInAtList=false 且正文无前导 @ → 拒绝。
	noMention := a.normalizeMessage(robotMessage{
		ConversationID:   "cid-grp",
		ConversationType: "2",
		MsgID:            "msg-g5",
		Text:             &robotTextContent{Content: "今天天气如何"},
		IsInAtList:       false,
	})
	if noMention != nil {
		t.Fatal("group message with isInAtList=false should be rejected when require_mention is set")
	}
	// isInAtList=true 且正文残留前导 @token → 剥离。
	both := a.normalizeMessage(robotMessage{
		ConversationID:   "cid-grp",
		ConversationType: "2",
		MsgID:            "msg-g6",
		Text:             &robotTextContent{Content: "@我的助手 今天天气如何"},
		IsInAtList:       true,
	})
	if both == nil || both.Text != "今天天气如何" {
		t.Fatalf("mention + isInAtList should strip prefix, got %+v", both)
	}
}

func TestNormalizeDeduplicatesByMsgID(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{})
	first := a.normalizeMessage(robotMessage{
		ConversationID: "cid-1", ConversationType: "1", MsgID: "dup-1",
		Text: &robotTextContent{Content: "hi"},
	})
	if first == nil {
		t.Fatal("first delivery should be accepted")
	}
	second := a.normalizeMessage(robotMessage{
		ConversationID: "cid-1", ConversationType: "1", MsgID: "dup-1",
		Text: &robotTextContent{Content: "hi"},
	})
	if second != nil {
		t.Fatal("duplicate msgId should be dropped")
	}
}

func TestNormalizeMissingIDsRejected(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{})
	if msg := a.normalizeMessage(robotMessage{ConversationID: "cid", ConversationType: "1", MsgID: ""}); msg != nil {
		t.Fatal("empty msgId should be rejected")
	}
	if msg := a.normalizeMessage(robotMessage{ConversationID: "", ConversationType: "1", MsgID: "m"}); msg != nil {
		t.Fatal("empty chatId should be rejected")
	}
}

func TestSendRequiresSessionWebhook(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{ClientID: "id", ClientSecret: "secret"})
	_, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID: "cid-1",
		Text:   "hi",
	})
	if err == nil || !strings.Contains(err.Error(), "session webhook") {
		t.Fatalf("send without webhook should fail with a clear error, got %v", err)
	}
}

// TestNormalizeRecordsSessionWebhook: normalize 必须把钉钉官方域名的
// webhook 记入 chatID→webhook 映射表，并透传到 InboundMessage。
func TestNormalizeRecordsSessionWebhook(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{})
	msg := a.normalizeMessage(robotMessage{
		ConversationID:   "cid-web",
		ConversationType: "1",
		MsgID:            "msg-w1",
		Text:             &robotTextContent{Content: "hi"},
		SessionWebhook:   "https://api.dingtalk.com/v1.0/robot/oToMessages/send?sessionWebhook=abc",
	})
	if msg == nil {
		t.Fatal("message should be accepted")
	}
	if msg.SessionWebhook != "https://api.dingtalk.com/v1.0/robot/oToMessages/send?sessionWebhook=abc" {
		t.Fatalf("inbound session_webhook = %q, want learned value", msg.SessionWebhook)
	}
	if got := a.webhookFor("cid-web"); got != "https://api.dingtalk.com/v1.0/robot/oToMessages/send?sessionWebhook=abc" {
		t.Fatalf("webhook map = %q, want learned value", got)
	}
}

// TestNormalizeRejectsForeignWebhook: 非钉钉官方域名的 sessionWebhook 不得
// 记入映射表，也不得透传（防伪造回调注入任意回复目标）。
func TestNormalizeRejectsForeignWebhook(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{})
	msg := a.normalizeMessage(robotMessage{
		ConversationID:   "cid-web",
		ConversationType: "1",
		MsgID:            "msg-w2",
		Text:             &robotTextContent{Content: "hi"},
		SessionWebhook:   "http://169.254.169.254/latest/meta-data/",
	})
	if msg == nil {
		t.Fatal("message should be accepted")
	}
	if msg.SessionWebhook != "" {
		t.Fatalf("inbound session_webhook = %q, want empty for foreign host", msg.SessionWebhook)
	}
	if got := a.webhookFor("cid-web"); got != "" {
		t.Fatalf("webhook map = %q, want empty for foreign host", got)
	}
}

func TestValidDingtalkWebhookRejectsHTTPOfficialHost(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{})
	if a.validDingtalkWebhook("http://api.dingtalk.com/v1.0/robot/send") {
		t.Fatal("production webhook validation must require HTTPS")
	}
}

// TestSendUsesLearnedWebhook: 入站学习到 webhook 后，sendMessage 应 POST 到
// 该 webhook 而非 ReplyToMsgID（gateway 会把 ReplyToMsgID 填成消息 ID），
// 且不携带 access token（会话 webhook 无需认证）。
func TestSendUsesLearnedWebhook(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("x-acs-dingtalk-access-token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := testAdapter(config.DingtalkBotConfig{})
	allowTestWebhook(t, a, srv)
	a.httpClient = srv.Client()
	// 入站消息学习 webhook。
	if m := a.normalizeMessage(robotMessage{
		ConversationID: "cid-learn", ConversationType: "1", MsgID: "m1",
		Text: &robotTextContent{Content: "hi"}, SessionWebhook: srv.URL,
	}); m == nil {
		t.Fatal("inbound message should be accepted")
	}
	// 出站：ReplyToMsgID 是消息 ID（非 URL），必须忽略并查表。
	if _, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID:       "cid-learn",
		ChatType:     bot.ChatDM,
		Text:         "回复",
		ReplyToMsgID: "om-12345", // 模拟 gateway 填入的消息 ID
	}); err != nil {
		t.Fatalf("send via learned webhook failed: %v", err)
	}
	if !strings.Contains(gotBody, "回复") {
		t.Fatalf("webhook body = %q, want reply text", gotBody)
	}
	if gotAuth != "" {
		t.Fatalf("webhook request must not carry access token, got %q", gotAuth)
	}
}

func TestSendRejectsRedirectToForeignWebhook(t *testing.T) {
	targetHit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	foreignTarget := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", foreignTarget)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	a := testAdapter(config.DingtalkBotConfig{})
	allowTestWebhook(t, a, source)
	a.httpClient = source.Client()
	_, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID:         "cid-redirect",
		ChatType:       bot.ChatDM,
		Text:           "hi",
		SessionWebhook: source.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "redirect to non-dingtalk endpoint") {
		t.Fatalf("foreign redirect must be rejected, got %v", err)
	}
	if targetHit {
		t.Fatal("redirect target must not receive the webhook request")
	}
}

// TestSendRejectsForeignWebhook: 非钉钉官方域名的 webhook（SessionWebhook
// 或映射表）必须被拒绝，且不得发出任何请求——防止 SSRF 与 token 外泄。
func TestSendRejectsForeignWebhook(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := testAdapter(config.DingtalkBotConfig{})
	a.httpClient = srv.Client()
	a.token = "test-token"
	a.tokenAt = time.Now()
	// SessionWebhook 指向非白名单主机（内网元数据地址）→ 拒绝，不发请求。
	_, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID:         "cid-x",
		ChatType:       bot.ChatDM,
		Text:           "hi",
		SessionWebhook: "http://169.254.169.254/latest/meta-data/",
	})
	if err == nil || !strings.Contains(err.Error(), "not a dingtalk endpoint") {
		t.Fatalf("foreign session webhook must be rejected, got %v", err)
	}
	// ReplyToMsgID 为 URL 时同样被拒绝（不再作为 webhook 来源）。
	_, err = a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID:       "cid-x",
		ChatType:     bot.ChatDM,
		Text:         "hi",
		ReplyToMsgID: srv.URL,
	})
	if err == nil {
		t.Fatal("ReplyToMsgID URL must not be used as webhook")
	}
	// 映射表里的非白名单 webhook 也被拒绝。
	a.webhooks["cid-x"] = "https://evil.example.com/hook"
	_, err = a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID:   "cid-x",
		ChatType: bot.ChatDM,
		Text:     "hi",
	})
	if err == nil || !strings.Contains(err.Error(), "not a dingtalk endpoint") {
		t.Fatalf("foreign mapped webhook must be rejected, got %v", err)
	}
	if hit {
		t.Fatal("no request may reach a foreign webhook host")
	}
}

// TestSendPrefersSessionWebhookOverLearned: 入站消息透传的 SessionWebhook
// 优先于映射表（gateway 重启后持久化恢复场景，adapter 内存映射可能为空）。
func TestSendPrefersSessionWebhookOverLearned(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := testAdapter(config.DingtalkBotConfig{})
	allowTestWebhook(t, a, srv)
	a.httpClient = srv.Client()
	a.token = "test-token"
	a.tokenAt = time.Now()
	// 映射表里是旧 webhook，透传的是新 webhook，必须用新的。
	a.webhooks["cid-x"] = "https://old.example.com/hook"
	if _, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID:         "cid-x",
		ChatType:       bot.ChatDM,
		Text:           "透传发送",
		SessionWebhook: srv.URL,
	}); err != nil {
		t.Fatalf("send with session webhook failed: %v", err)
	}
	if !strings.HasPrefix(gotPath, "/") {
		t.Fatalf("request path = %q, want httptest path", gotPath)
	}
}

// TestSendPlainTextUsesMarkdown: 普通文本（无 Card）也必须以 markdown 类型
// 发送，否则钉钉按纯文本显示、不渲染 markdown 语法（与飞书一致）。
func TestSendPlainTextUsesMarkdown(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := testAdapter(config.DingtalkBotConfig{})
	allowTestWebhook(t, a, srv)
	a.httpClient = srv.Client()
	a.token = "test-token"
	a.tokenAt = time.Now()
	if _, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID:         "cid-md",
		ChatType:       bot.ChatDM,
		Text:           "**加粗** 和 `code`",
		SessionWebhook: srv.URL,
	}); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if !strings.Contains(gotBody, `"msgtype":"markdown"`) {
		t.Fatalf("plain text send must use markdown msgtype, got %s", gotBody)
	}
	if !strings.Contains(gotBody, "**加粗** 和 `code`") {
		t.Fatalf("markdown body should carry original text, got %s", gotBody)
	}
}

// TestSendMarkdownCard: Card 存在时发送 markdown 消息。
func TestSendMarkdownCard(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := testAdapter(config.DingtalkBotConfig{})
	allowTestWebhook(t, a, srv)
	a.httpClient = srv.Client()
	a.token = "test-token"
	a.tokenAt = time.Now()
	_, err := a.sendMessage(context.Background(), bot.OutboundMessage{
		ChatID:         "cid-card",
		ChatType:       bot.ChatDM,
		Text:           "**bold** content",
		SessionWebhook: srv.URL,
		Card:           &bot.InteractiveCard{Header: "标题"},
	})
	if err != nil {
		t.Fatalf("send markdown card failed: %v", err)
	}
	if !strings.Contains(gotBody, `"msgtype":"markdown"`) || !strings.Contains(gotBody, "**bold**") {
		t.Fatalf("webhook body = %q, want markdown payload", gotBody)
	}
}

// TestDecodeRobotMessageNestedJSONData: Stream 回调的 data 是 JSON 编码的
// 字符串，decodeRobotMessage 必须先解码字符串再解析消息（与 dsh transport.ts
// 的 JSON.parse(res.data) 一致），同时兼容 data 直接是对象的情况。
func TestDecodeRobotMessageNestedJSONData(t *testing.T) {
	inner, err := json.Marshal(robotMessage{
		SenderStaffID:    "user-nested",
		SenderNick:       "嵌套",
		ConversationID:   "cid-nested",
		ConversationType: "1",
		MsgID:            "msg-nested",
		MsgType:          "text",
		Text:             &robotTextContent{Content: "双层"},
		SessionWebhook:   "https://webhook/nested",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 双层编码：data 是 JSON 字符串，其内容为 robotMessage 的 JSON。
	nestedBytes, err := json.Marshal(string(inner))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := decodeRobotMessage(json.RawMessage(nestedBytes))
	if !ok {
		t.Fatal("nested string payload should decode")
	}
	if raw.MsgID != "msg-nested" || raw.ConversationID != "cid-nested" || raw.SessionWebhook != "https://webhook/nested" {
		t.Fatalf("unexpected decoded message: %+v", raw)
	}

	// 直接对象（兼容路径）。
	direct, ok := decodeRobotMessage(json.RawMessage(inner))
	if !ok {
		t.Fatal("direct object payload should decode")
	}
	if direct.MsgID != "msg-nested" {
		t.Fatalf("direct payload msg id = %q", direct.MsgID)
	}

	// 非法载荷。
	if _, ok := decodeRobotMessage(json.RawMessage(`"not-json`)); ok {
		t.Fatal("garbage payload should be rejected")
	}
}

func TestSplitMentionBotNameMismatch(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{BotName: "我的助手"})
	mentionsBot, rest := a.splitMention("@别人 你好")
	if mentionsBot {
		t.Fatal("mention of another user should not count as @bot")
	}
	if rest != "@别人 你好" {
		t.Fatalf("mismatched mention must keep the original text, got %q", rest)
	}
}

func TestClientCredentialsFromEnv(t *testing.T) {
	t.Setenv("DINGTALK_TEST_ID", "env-id")
	t.Setenv("DINGTALK_TEST_SECRET", "env-secret")
	a := testAdapter(config.DingtalkBotConfig{
		ClientIDEnv: "DINGTALK_TEST_ID",
		SecretEnv:   "DINGTALK_TEST_SECRET",
	})
	if got := a.clientID(); got != "env-id" {
		t.Fatalf("client id = %q, want env-id", got)
	}
	if got := a.clientSecret(); got != "env-secret" {
		t.Fatalf("client secret = %q, want env-secret", got)
	}
}

// TestStartRejectsMissingCredentials: Start 必须同步校验凭据并返回错误，而不是
// 标记 running 后由 runWithRetry 在后台静默失败（否则桌面端会显示绿色的
// "已连接" 状态，实际从未连上）。
func TestStartRejectsMissingCredentials(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{})
	err := a.Start(context.Background())
	if err == nil {
		t.Fatal("Start with no credentials should fail")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Fatalf("Start error = %q, want client_id message", err)
	}
}

func TestStartRejectsEmptySecret(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{ClientID: "app-key"})
	err := a.Start(context.Background())
	if err == nil {
		t.Fatal("Start with no secret should fail")
	}
	if !strings.Contains(err.Error(), "client_secret") {
		t.Fatalf("Start error = %q, want client_secret message", err)
	}
}

func TestStartReturnsWebSocketHandshakeFailure(t *testing.T) {
	var srv *httptest.Server
	gatewayCalls := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/gettoken":
			_, _ = io.WriteString(w, `{"access_token":"token","errcode":0}`)
		case "/v1.0/gateway/connections/open":
			gatewayCalls++
			endpoint := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
			_ = json.NewEncoder(w).Encode(gatewayEndpoint{Endpoint: endpoint, Ticket: "ticket-1"})
		case "/ws":
			http.Error(w, "handshake rejected", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	a := testAdapter(config.DingtalkBotConfig{ClientID: "app-key", ClientSecret: "secret"})
	a.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		localReq := req.Clone(req.Context())
		localReq.URL.Scheme = target.Scheme
		localReq.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(localReq)
	})}

	err = a.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("Start must report WebSocket handshake failure, got %v", err)
	}
	if gatewayCalls != 1 {
		t.Fatalf("gateway open calls = %d, want one initial ticket", gatewayCalls)
	}
}

// TestTestSendWithoutKnownChat: 还没有任何交互过的会话时，测试发送返回
// 可读错误，而不是发起真实请求。
func TestTestSendWithoutKnownChat(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{})
	if _, err := a.TestSend(context.Background(), "hi"); err == nil {
		t.Fatal("TestSend without a known chat should fail")
	} else if !strings.Contains(err.Error(), "requires a known chat") {
		t.Fatalf("error = %q, want readable known-chat hint", err.Error())
	}
}

// TestTestSendUsesLatestLearnedChat: 测试发送会发到最近交互过的会话
// （normalizeMessage 学到 webhook 后记录 lastChatID）。
func TestTestSendUsesLatestLearnedChat(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := testAdapter(config.DingtalkBotConfig{})
	allowTestWebhook(t, a, srv)
	a.httpClient = srv.Client()
	a.token = "test-token"
	a.tokenAt = time.Now()
	if m := a.normalizeMessage(robotMessage{
		ConversationID: "cid-latest", ConversationType: "1", MsgID: "m1",
		Text: &robotTextContent{Content: "hi"}, SessionWebhook: srv.URL,
	}); m == nil {
		t.Fatal("inbound message should be accepted")
	}
	if _, err := a.TestSend(context.Background(), "测试消息"); err != nil {
		t.Fatalf("TestSend failed: %v", err)
	}
	if !strings.Contains(gotBody, "测试消息") {
		t.Fatalf("webhook body = %q, want test text", gotBody)
	}
}

// TestAddPendingReactionPinsAndRecallsEmotion: 收到消息后 AddPendingReaction
// 贴 🤔思考中 表情，cleanup 撤回；emotion 请求体携带 robotCode/chat/message。
func TestAddPendingReactionPinsAndRecallsEmotion(t *testing.T) {
	var actions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		actions = append(actions, r.URL.Path+"|"+string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	oldURL := emotionURL
	emotionURL = srv.URL + "/v1.0/robot/emotion"
	t.Cleanup(func() { emotionURL = oldURL })

	a := testAdapter(config.DingtalkBotConfig{ClientID: "ding-appkey", ClientSecret: "secret"})
	a.httpClient = srv.Client()
	a.token = "test-token"
	a.tokenAt = time.Now()
	// 入站消息学习 chat。
	if m := a.normalizeMessage(robotMessage{
		ConversationID: "cid-emotion", ConversationType: "1", MsgID: "msg-emotion-1",
		Text: &robotTextContent{Content: "hi"}, SessionWebhook: "https://webhook/emotion",
	}); m == nil {
		t.Fatal("inbound message should be accepted")
	}
	cleanup, err := a.AddPendingReaction(context.Background(), "msg-emotion-1")
	if err != nil {
		t.Fatalf("AddPendingReaction failed: %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup must not be nil")
	}
	cleanup()

	if len(actions) != 2 {
		t.Fatalf("expected 2 emotion calls (reply+recall), got %d: %v", len(actions), actions)
	}
	if !strings.Contains(actions[0], "/reply") || !strings.Contains(actions[0], "🤔思考中") {
		t.Fatalf("first call should be reply with thinking emotion, got %q", actions[0])
	}
	if !strings.Contains(actions[0], "ding-appkey") || !strings.Contains(actions[0], "cid-emotion") || !strings.Contains(actions[0], "msg-emotion-1") {
		t.Fatalf("reply body missing robotCode/chat/message: %q", actions[0])
	}
	if !strings.Contains(actions[1], "/recall") {
		t.Fatalf("second call should be recall, got %q", actions[1])
	}
}

// TestAddPendingReactionUnknownMessage: 未记录过的 messageID 报可读错误。
func TestAddPendingReactionUnknownMessage(t *testing.T) {
	a := testAdapter(config.DingtalkBotConfig{ClientID: "ding-appkey", ClientSecret: "secret"})
	if _, err := a.AddPendingReaction(context.Background(), "unknown-msg"); err == nil {
		t.Fatal("unknown message should fail")
	} else if !strings.Contains(err.Error(), "unknown chat") {
		t.Fatalf("error = %q, want unknown-chat hint", err.Error())
	}
}

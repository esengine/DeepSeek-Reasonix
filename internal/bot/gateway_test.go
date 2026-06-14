package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// fakeAdapter 是一个内存中的假适配器，用于测试 BotGateway。
type fakeAdapter struct {
	mu       sync.Mutex
	platform Platform
	name     string
	msgCh    chan InboundMessage
	sent     []OutboundMessage
	started  bool
	startErr error
}

func newFakeAdapter(platform Platform, name string) *fakeAdapter {
	return &fakeAdapter{
		platform: platform,
		name:     name,
		msgCh:    make(chan InboundMessage, 16),
	}
}

func (f *fakeAdapter) Platform() Platform              { return f.platform }
func (f *fakeAdapter) Name() string                    { return f.name }
func (f *fakeAdapter) Messages() <-chan InboundMessage { return f.msgCh }

func (f *fakeAdapter) Start(ctx context.Context) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.mu.Lock()
	f.started = true
	f.mu.Unlock()
	return nil
}

func (f *fakeAdapter) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.msgCh != nil {
		close(f.msgCh)
		f.msgCh = nil
	}
	return nil
}

func (f *fakeAdapter) Send(ctx context.Context, msg OutboundMessage) (SendResult, error) {
	f.mu.Lock()
	f.sent = append(f.sent, msg)
	f.mu.Unlock()
	return SendResult{MessageID: "fake_msg_1"}, nil
}

func (f *fakeAdapter) SendTyping(ctx context.Context, chatID string) error { return nil }

func (f *fakeAdapter) sentMessages() []OutboundMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]OutboundMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

type fakeReactionAdapter struct {
	*fakeAdapter
	reactions []string
}

func (f *fakeReactionAdapter) AddPendingReaction(ctx context.Context, messageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactions = append(f.reactions, messageID)
	return nil
}

func TestFakeAdapterInterface(t *testing.T) {
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	if fa.Platform() != PlatformQQ {
		t.Error("wrong platform")
	}
	if fa.Name() != "fake-qq" {
		t.Error("wrong name")
	}

	ctx := context.Background()
	if err := fa.Start(ctx); err != nil {
		t.Fatal("start:", err)
	}
	if !fa.started {
		t.Error("should be started")
	}

	_, err := fa.Send(ctx, OutboundMessage{ChatID: "c1", Text: "hello"})
	if err != nil {
		t.Fatal("send:", err)
	}

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	if sent[0].Text != "hello" {
		t.Errorf("sent text = %q, want %q", sent[0].Text, "hello")
	}

	if err := fa.Stop(); err != nil {
		t.Fatal("stop:", err)
	}
}

func TestGatewayConstructAndStop(t *testing.T) {
	cfg := GatewayConfig{
		Model:         "test",
		MaxSteps:      10,
		WorkspaceRoot: ".",
		Enabled:       map[Platform]bool{PlatformQQ: true},
		Allowlist:     AllowlistConfig{Enabled: false},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, map[Platform]Adapter{
		PlatformQQ: newFakeAdapter(PlatformQQ, "fake-qq"),
	}, logger)

	// 网关不应该 panic
	if gw == nil {
		t.Fatal("gateway should not be nil")
	}
	gw.Stop()
}

func TestGatewayStartsHealthyAdaptersWhenOneFails(t *testing.T) {
	cfg := GatewayConfig{
		Enabled:   map[Platform]bool{PlatformFeishu: true, PlatformWeixin: true},
		Allowlist: AllowlistConfig{AllowAll: true},
	}
	good := newFakeAdapter(PlatformFeishu, "good-feishu")
	bad := newFakeAdapter(PlatformWeixin, "bad-weixin")
	bad.startErr = errors.New("missing token")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGatewayWithAdapterBindings(cfg, []AdapterBinding{
		{ID: "feishu-lark", Platform: PlatformFeishu, Adapter: good},
		{ID: "weixin-weixin", Platform: PlatformWeixin, Adapter: bad},
	}, logger)

	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("start should keep healthy adapters running: %v", err)
	}
	if got := gw.AdapterCount(); got != 1 {
		t.Fatalf("adapter count = %d, want 1", got)
	}
	if !good.started {
		t.Fatal("healthy adapter was not started")
	}
	if bad.started {
		t.Fatal("failing adapter should not be marked started")
	}
	startErr := gw.StartErrors()
	if len(startErr) != 1 || !strings.Contains(startErr[0].Error(), "weixin-weixin") {
		t.Fatalf("start errors = %#v, want wrapped connection error", startErr)
	}
}

func TestGatewayReturnsErrorWhenAllAdaptersFail(t *testing.T) {
	cfg := GatewayConfig{
		Enabled:   map[Platform]bool{PlatformWeixin: true},
		Allowlist: AllowlistConfig{AllowAll: true},
	}
	bad := newFakeAdapter(PlatformWeixin, "bad-weixin")
	bad.startErr = errors.New("missing token")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGatewayWithAdapterBindings(cfg, []AdapterBinding{
		{ID: "weixin-weixin", Platform: PlatformWeixin, Adapter: bad},
	}, logger)

	err := gw.Start(context.Background())
	if err == nil {
		t.Fatal("start should fail when every adapter fails")
	}
	if !strings.Contains(err.Error(), "weixin-weixin") {
		t.Fatalf("error = %v, want connection id", err)
	}
	if got := gw.AdapterCount(); got != 0 {
		t.Fatalf("adapter count = %d, want 0", got)
	}
	if len(gw.StartErrors()) != 1 {
		t.Fatalf("start errors = %#v, want one", gw.StartErrors())
	}
}

func TestGatewayAllowlistCheck(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Users: map[Platform][]string{
				PlatformQQ: {"allowed_user_1"},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if !gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, UserID: "allowed_user_1"}) {
		t.Error("allowed user should pass")
	}
	if gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, UserID: "unknown_user"}) {
		t.Error("unknown user should not pass")
	}
	// 不同平台
	if gw.checkAllowlist(PlatformFeishu, InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, UserID: "allowed_user_1"}) {
		t.Error("QQ allowlist should not apply to feishu")
	}
}

func TestGatewayAllowlistDoesNotApplyGroupsToDirectMessages(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Users: map[Platform][]string{
				PlatformQQ: {"allowed_user"},
			},
			Groups: map[Platform][]string{
				PlatformQQ: {"allowed_group"},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if !gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDirect, ChatID: "guild-dm", UserID: "allowed_user"}) {
		t.Error("direct message should not be rejected by group allowlist")
	}
	if gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatGroup, ChatID: "unknown_group", UserID: "allowed_user"}) {
		t.Error("unknown group should still be rejected by group allowlist")
	}
}

func TestGatewayAllowlistGatesOnOperatorNotCardRequester(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Users: map[Platform][]string{
				PlatformFeishu: {"requester"},
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	stranger := InboundMessage{Platform: PlatformFeishu, ChatType: ChatGroup, ChatID: "chat", UserID: "requester", OperatorID: "stranger"}
	if gw.checkAllowlist(PlatformFeishu, stranger) {
		t.Error("a non-allowlisted operator must be rejected even when the card carries an allowlisted requester id")
	}

	allowed := InboundMessage{Platform: PlatformFeishu, ChatType: ChatGroup, ChatID: "chat", UserID: "requester", OperatorID: "requester"}
	if !gw.checkAllowlist(PlatformFeishu, allowed) {
		t.Error("an allowlisted operator should pass")
	}
}

func TestGatewayAllowlistDisabledRejectsByDefault(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{Enabled: false},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, UserID: "any_user"}) {
		t.Error("disabled allowlist should reject unless allow_all is explicit")
	}
}

func TestGatewayAllowAll(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{AllowAll: true},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if !gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, UserID: "any_user"}) {
		t.Error("allow_all should allow everyone")
	}
}

func TestBuildSessionKeyIsolatesGroupUsersAndSharesThreads(t *testing.T) {
	alice := BuildSessionKey(SessionSource{
		Platform: PlatformQQ,
		ChatType: ChatGroup,
		ChatID:   "group-1",
		UserID:   "alice",
	})
	bob := BuildSessionKey(SessionSource{
		Platform: PlatformQQ,
		ChatType: ChatGroup,
		ChatID:   "group-1",
		UserID:   "bob",
	})
	if alice == bob {
		t.Fatal("group sessions should be isolated by user")
	}

	threadAlice := BuildSessionKey(SessionSource{
		Platform: PlatformQQ,
		ChatType: ChatThread,
		ChatID:   "group-1",
		ThreadID: "thread-1",
		UserID:   "alice",
	})
	threadBob := BuildSessionKey(SessionSource{
		Platform: PlatformQQ,
		ChatType: ChatThread,
		ChatID:   "group-1",
		ThreadID: "thread-1",
		UserID:   "bob",
	})
	if threadAlice != threadBob {
		t.Fatal("thread sessions should be shared within the same thread")
	}
}

func TestGatewayRejectsUnauthorizedBoundSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "private session")
	unauthorized := InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "dm-unauthorized",
		UserID:    "blocked-user",
		Text:      "/status",
		MessageID: "msg-1",
	}
	unauthorizedKey := BuildSessionKey(unauthorized.Session())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fa := newFakeAdapter(PlatformQQ, "fake-qq")
	gw := NewGateway(GatewayConfig{
		SessionDir: dir,
		Allowlist: AllowlistConfig{
			Enabled: true,
			Users: map[Platform][]string{
				PlatformQQ: {"allowed-user"},
			},
		},
	}, nil, logger)
	if err := gw.setSessionMapping(unauthorizedKey, sessionPath, ""); err != nil {
		t.Fatalf("setSessionMapping: %v", err)
	}

	gw.handleMessage(context.Background(), AdapterBinding{ID: string(PlatformQQ), Platform: PlatformQQ, Adapter: fa}, unauthorized)

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want permission rejection", len(sent))
	}
	if !strings.Contains(sent[0].Text, "没有使用此 bot 的权限") {
		t.Fatalf("unexpected rejection text: %q", sent[0].Text)
	}
	gw.mu.Lock()
	_, hasController := gw.controllers[unauthorizedKey]
	gw.mu.Unlock()
	if hasController {
		t.Fatal("unauthorized user should not open a bound session controller")
	}
}

func TestGatewayNormalizesNumericApprovalShortcutsOnlyWhenPending(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	key := "session-key"

	if _, ok := gw.normalizeApprovalShortcut(key, "1"); ok {
		t.Fatal("numeric text without a pending approval should stay a normal message")
	}

	gw.controllers[key] = &sessionState{
		pendingApprovals: map[string]event.Approval{
			"42": {ID: "42", Tool: "explore"},
		},
		lastApprovalID: "42",
	}

	got, ok := gw.normalizeApprovalShortcut(key, "1")
	if !ok || got != "/approve 42" {
		t.Fatalf("normalize 1 = %q,%v; want /approve 42,true", got, ok)
	}
	got, ok = gw.normalizeApprovalShortcut(key, "2")
	if !ok || got != "/deny 42" {
		t.Fatalf("normalize 2 = %q,%v; want /deny 42,true", got, ok)
	}
	gw.forgetPendingApproval(key, "42")
	if _, ok := gw.normalizeApprovalShortcut(key, "1"); ok {
		t.Fatal("numeric text after approval is forgotten should stay a normal message")
	}
}

func TestGatewayNormalizesNumericAskShortcutOnlyForSingleChoicePendingAsk(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	key := "session-key"

	if _, ok := gw.normalizeAskShortcut(key, "1"); ok {
		t.Fatal("numeric text without a pending ask should stay a normal message")
	}

	gw.controllers[key] = &sessionState{
		pendingAsks: map[string][]event.AskQuestion{
			"ask-1": {{
				ID:     "q1",
				Prompt: "Choose one",
				Options: []event.AskOption{
					{Label: "Allow once"},
					{Label: "Deny"},
				},
			}},
		},
		lastAskID: "ask-1",
	}

	got, ok := gw.normalizeAskShortcut(key, "2")
	if !ok || got != "/answer ask-1 2" {
		t.Fatalf("normalize 2 = %q,%v; want /answer ask-1 2,true", got, ok)
	}
	if _, ok := gw.normalizeAskShortcut(key, "1;2"); ok {
		t.Fatal("compound numeric text should stay a normal message")
	}

	gw.controllers[key].pendingAsks["ask-2"] = []event.AskQuestion{
		{ID: "q1", Prompt: "First", Options: []event.AskOption{{Label: "A"}}},
		{ID: "q2", Prompt: "Second", Options: []event.AskOption{{Label: "B"}}},
	}
	gw.controllers[key].lastAskID = "ask-2"
	if _, ok := gw.normalizeAskShortcut(key, "1"); ok {
		t.Fatal("numeric shortcut should not answer multi-question asks")
	}
}

func TestGatewaySessionOptionsUseConnectionToolApprovalOverride(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		Model:            "default-model",
		ToolApprovalMode: "auto",
		Channels: map[Platform]ChannelConfig{
			PlatformFeishu: {Model: "platform-model", ToolApprovalMode: "ask"},
		},
		ConnectionChannels: map[string]ChannelConfig{
			"feishu-lark": {Model: "lark-model", ToolApprovalMode: "yolo"},
		},
	}, nil, logger)

	model, _, mode := gw.sessionOptionsForMessage(InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-lark",
	})
	if model != "lark-model" || mode != "yolo" {
		t.Fatalf("lark session options = model %q mode %q, want lark-model/yolo", model, mode)
	}

	model, _, mode = gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu})
	if model != "platform-model" || mode != "ask" {
		t.Fatalf("platform session options = model %q mode %q, want platform-model/ask", model, mode)
	}
}

func TestGatewayNumericApprovalShortcutActiveWithoutPendingSendsGuidance(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{AllowAll: true}}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	binding := AdapterBinding{ID: "weixin-weixin", Domain: "weixin", Platform: PlatformWeixin, Adapter: adapter}
	msg := InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin-weixin",
		Domain:       "weixin",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "seed",
	}
	key := BuildSessionKey(msg.Session())
	if acquired, _ := gw.sessions.TryAcquire(key, msg); !acquired {
		t.Fatal("failed to mark session active")
	}

	msg.Text = "1"
	gw.handleMessage(context.Background(), binding, msg)

	sent := adapter.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].Text, "没有找到可匹配的待处理操作") {
		t.Fatalf("sent text = %q, want pending operation guidance", sent[0].Text)
	}
}

func TestGatewayApproveWithoutSessionSendsGuidance(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	msg := InboundMessage{ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "/approve 1"}

	gw.handleSlashCommand(context.Background(), adapter, "missing-session", msg)

	sent := adapter.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].Text, "没有找到当前会话中的待审批操作") {
		t.Fatalf("sent text = %q, want missing approval guidance", sent[0].Text)
	}
}

func TestGatewayYoloCommandUpdatesCurrentSessionAndConnectionDefault(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var persistedMode string
	var persistedConnection string
	gw := NewGateway(GatewayConfig{
		ToolApprovalMode: "ask",
		ConnectionChannels: map[string]ChannelConfig{
			"feishu-lark": {ToolApprovalMode: "ask"},
		},
		OnToolApprovalModeChange: func(msg InboundMessage, mode string) error {
			persistedConnection = msg.ConnectionID
			persistedMode = mode
			return nil
		},
	}, nil, logger)
	adapter := newFakeAdapter(PlatformFeishu, "fake-lark")
	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-lark",
		Domain:       "lark",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "/yolo on",
	}
	key := BuildSessionKey(msg.Session())
	ctrl := control.New(control.Options{})
	ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	gw.controllers[key] = &sessionState{ctrl: ctrl}

	gw.handleSlashCommand(context.Background(), adapter, key, msg)

	if got := ctrl.ToolApprovalMode(); got != control.ToolApprovalYolo {
		t.Fatalf("current session mode = %q, want yolo", got)
	}
	if got := gw.cfg.ConnectionChannels["feishu-lark"].ToolApprovalMode; got != control.ToolApprovalYolo {
		t.Fatalf("connection default mode = %q, want yolo", got)
	}
	if persistedConnection != "feishu-lark" || persistedMode != control.ToolApprovalYolo {
		t.Fatalf("persisted = %q/%q, want feishu-lark/yolo", persistedConnection, persistedMode)
	}
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "已开启 YOLO") {
		t.Fatalf("sent = %#v, want yolo confirmation", sent)
	}
}

func TestGatewayModeCommandSupportsAskAutoAndStatus(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		ConnectionChannels: map[string]ChannelConfig{
			"weixin-weixin": {ToolApprovalMode: "ask"},
		},
	}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	msg := InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin-weixin",
		Domain:       "weixin",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
	}
	key := BuildSessionKey(msg.Session())

	msg.Text = "/mode auto"
	gw.handleSlashCommand(context.Background(), adapter, key, msg)
	if got := gw.cfg.ConnectionChannels["weixin-weixin"].ToolApprovalMode; got != control.ToolApprovalAuto {
		t.Fatalf("/mode auto default = %q, want auto", got)
	}

	msg.Text = "/yolo off"
	gw.handleSlashCommand(context.Background(), adapter, key, msg)
	if got := gw.cfg.ConnectionChannels["weixin-weixin"].ToolApprovalMode; got != control.ToolApprovalAsk {
		t.Fatalf("/yolo off default = %q, want ask", got)
	}

	msg.Text = "/mode"
	gw.handleSlashCommand(context.Background(), adapter, key, msg)
	sent := adapter.sentMessages()
	if len(sent) != 3 {
		t.Fatalf("sent count = %d, want 3", len(sent))
	}
	if !strings.Contains(sent[2].Text, "当前工具审批模式：询问") {
		t.Fatalf("status = %q, want ask status", sent[2].Text)
	}
}

func TestGatewayHelpMentionsYoloCommands(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	msg := InboundMessage{ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "/help"}

	gw.handleSlashCommand(context.Background(), adapter, "session-key", msg)

	sent := adapter.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].Text, "/yolo on|off|auto|status") || !strings.Contains(sent[0].Text, "/mode yolo|ask|auto") {
		t.Fatalf("help = %q, want yolo commands", sent[0].Text)
	}
}

func TestGatewayAddsPendingReactionWhenAdapterSupportsIt(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	fa := &fakeReactionAdapter{fakeAdapter: newFakeAdapter(PlatformFeishu, "fake-feishu")}

	gw.addPendingReaction(context.Background(), PlatformFeishu, fa, InboundMessage{MessageID: "om_123"})

	if len(fa.reactions) != 1 || fa.reactions[0] != "om_123" {
		t.Fatalf("reactions = %#v, want [om_123]", fa.reactions)
	}
}

func TestGatewaySessionOptionsUseChannelOverride(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		Model:         "global-model",
		WorkspaceRoot: "/global",
		Channels: map[Platform]ChannelConfig{
			PlatformFeishu: {Model: "feishu-model", WorkspaceRoot: "/feishu"},
			PlatformWeixin: {WorkspaceRoot: "/weixin"},
		},
	}, nil, logger)

	model, root, mode := gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu})
	if model != "feishu-model" || root != "/feishu" {
		t.Fatalf("feishu options = %q,%q; want channel override", model, root)
	}
	if mode != "ask" {
		t.Fatalf("feishu tool approval mode = %q, want ask", mode)
	}

	model, root, mode = gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformWeixin})
	if model != "global-model" || root != "/weixin" {
		t.Fatalf("weixin options = %q,%q; want global model and channel root", model, root)
	}
	if mode != "ask" {
		t.Fatalf("weixin tool approval mode = %q, want ask", mode)
	}

	model, root, mode = gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformQQ})
	if model != "global-model" || root != "/global" {
		t.Fatalf("qq options = %q,%q; want global defaults", model, root)
	}
	if mode != "ask" {
		t.Fatalf("qq tool approval mode = %q, want ask", mode)
	}
}

func TestGatewaySessionOptionsPreferConnectionOverride(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		Model:         "global-model",
		WorkspaceRoot: "/global",
		Channels: map[Platform]ChannelConfig{
			PlatformFeishu: {Model: "feishu-model", WorkspaceRoot: "/feishu"},
		},
		ConnectionChannels: map[string]ChannelConfig{
			"feishu-lark": {Model: "lark-model", WorkspaceRoot: "/lark"},
		},
	}, nil, logger)

	model, root, mode := gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu, ConnectionID: "feishu-lark"})
	if model != "lark-model" || root != "/lark" {
		t.Fatalf("lark options = %q,%q; want connection override", model, root)
	}
	if mode != "ask" {
		t.Fatalf("lark tool approval mode = %q, want ask", mode)
	}
}

func TestGatewayRenderSessionListAndResolveRef(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello from saved session")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)

	list := gw.renderSessionList(5)
	id := shortSessionID(sessionPath)
	if !strings.Contains(list, id) || !strings.Contains(list, "hello from saved session") {
		t.Fatalf("session list missing saved session: %s", list)
	}

	resolved, err := gw.resolveSessionRef(id[:6])
	if err != nil {
		t.Fatalf("resolveSessionRef: %v", err)
	}
	if resolved != sessionPath {
		t.Fatalf("resolved = %q, want %q", resolved, sessionPath)
	}
}

func TestGatewaySessionMappingPersists(t *testing.T) {
	dir := t.TempDir()
	mappingPath := filepath.Join(dir, "mappings.json")
	sessionPath := filepath.Join(dir, "saved.jsonl")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionMappingPath: mappingPath}, nil, logger)
	gw.mappings["remote-1"] = SessionMapping{
		RemoteKey:   "remote-1",
		SessionPath: sessionPath,
		SessionID:   "saved",
	}
	if err := gw.saveSessionMappings(); err != nil {
		t.Fatalf("saveSessionMappings: %v", err)
	}

	gw2 := NewGateway(GatewayConfig{SessionMappingPath: mappingPath}, nil, logger)
	mapping, ok := gw2.sessionMapping("remote-1")
	if !ok || mapping.SessionPath != sessionPath || mapping.SessionID != "saved" {
		t.Fatalf("mapping not reloaded: ok=%v mapping=%+v", ok, mapping)
	}
	if !gw2.clearSessionMapping("remote-1") {
		t.Fatal("clearSessionMapping should return true")
	}
	gw3 := NewGateway(GatewayConfig{SessionMappingPath: mappingPath}, nil, logger)
	if _, ok := gw3.sessionMapping("remote-1"); ok {
		t.Fatal("mapping should be removed after clear")
	}
}

func TestGatewayEnsuresMappingForNewController(t *testing.T) {
	dir := t.TempDir()
	mappingPath := filepath.Join(dir, "mappings.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		SessionDir:         dir,
		SessionMappingPath: mappingPath,
		WorkspaceRoot:      "/workspace",
	}, nil, logger)
	ctrl := control.New(control.Options{SessionDir: dir, Label: "model/test"})

	gw.ensureControllerSessionMapping("remote-new", InboundMessage{Platform: PlatformQQ}, ctrl)

	if ctrl.SessionPath() == "" {
		t.Fatal("controller should receive a session path")
	}
	mapping, ok := gw.sessionMapping("remote-new")
	if !ok {
		t.Fatal("mapping should be recorded")
	}
	if mapping.SessionPath != ctrl.SessionPath() || mapping.WorkspaceRoot != "/workspace" {
		t.Fatalf("unexpected mapping: %+v ctrlPath=%q", mapping, ctrl.SessionPath())
	}

	gw2 := NewGateway(GatewayConfig{SessionMappingPath: mappingPath}, nil, logger)
	reloaded, ok := gw2.sessionMapping("remote-new")
	if !ok || reloaded.SessionPath != ctrl.SessionPath() {
		t.Fatalf("mapping not persisted: ok=%v mapping=%+v", ok, reloaded)
	}
}

func TestGatewayEnsuresMappingPreservesExistingControllerPath(t *testing.T) {
	dir := t.TempDir()
	existingPath := filepath.Join(dir, "existing.jsonl")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	ctrl := control.New(control.Options{SessionDir: dir, Label: "model/test"})
	ctrl.SetSessionPath(existingPath)

	gw.ensureControllerSessionMapping("remote-existing", InboundMessage{Platform: PlatformQQ}, ctrl)

	mapping, ok := gw.sessionMapping("remote-existing")
	if !ok || mapping.SessionPath != existingPath || ctrl.SessionPath() != existingPath {
		t.Fatalf("existing path should be preserved: ok=%v mapping=%+v ctrlPath=%q", ok, mapping, ctrl.SessionPath())
	}
}

func TestGatewayStatusIncludesRuntimeDetails(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	now := time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{
			Text:   "ship the daemon status panel",
			Status: control.GoalStatusRunning,
		},
		Run: agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait: agent.RuntimeWaitMeta{
			Kind:            "event",
			Reason:          "waiting for CI",
			EventID:         "run-123",
			EventSource:     "github.workflow_run",
			EventStatus:     "completed",
			EventConclusion: "success",
			Subject:         "PR #42",
			Since:           now.Add(-10 * time.Minute),
		},
		Scheduler: agent.RuntimeSchedMeta{
			Enabled:          true,
			DailyAt:          "09:00",
			Interval:         time.Hour,
			LastWakeupAt:     now,
			LastWakeupReason: "webhook",
			NextWakeupAt:     now.Add(time.Hour),
		},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit:    5,
			DailyWakeups:        2,
			DailyModelCallLimit: 7,
			DailyModelCalls:     3,
			DailyModelCostLimit: 1.25,
			DailyModelCost:      0.5,
			ModelCostCurrency:   "$",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test"})
	ctrl.SetGoal("ship the daemon status panel")
	gw.controllers["remote-status"] = &sessionState{ctrl: ctrl, createdAt: now, lastActive: now}
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-status", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/status",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	text := sent[0].Text
	for _, want := range []string{
		"会话: " + shortSessionID(sessionPath),
		"目标: ship the daemon status panel",
		"运行状态: waiting_event",
		"等待: event run-123 (PR #42)",
		"等待原因: waiting for CI",
		"事件条件: source=github.workflow_run status=completed conclusion=success",
		"调度: enabled daily=09:00 interval=1h0m0s",
		"上次唤醒:",
		"(webhook)",
		"下次唤醒:",
		"唤醒预算: 2/5",
		"模型调用预算: 3/7",
		"模型费用预算: $ 0.5000/1.2500",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, sessionPath) {
		t.Fatalf("status should not expose full session path:\n%s", text)
	}
}

func TestGatewayStatusUsesMappingWithoutActiveController(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{
			Text:   "resume mapped work",
			Status: control.GoalStatusRunning,
		},
		Run: agent.RuntimeRunMeta{Status: "idle"},
		Wait: agent.RuntimeWaitMeta{
			Kind:       "time",
			Reason:     "nap before retry",
			Until:      time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
			FilePaths:  nil,
			ApprovalID: "",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	if err := gw.setSessionMapping("remote-mapped", sessionPath, "/workspace"); err != nil {
		t.Fatalf("setSessionMapping: %v", err)
	}
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-mapped", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/status",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	text := sent[0].Text
	for _, want := range []string{
		"会话: " + shortSessionID(sessionPath),
		"目标: resume mapped work",
		"目标状态: running",
		"运行状态: idle",
		"等待: time",
		"等待原因: nap before retry",
		"等待到:",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, sessionPath) {
		t.Fatalf("status should not expose full session path:\n%s", text)
	}
}

func TestGatewayStatusAfterRestartUsesPersistedMappingGoal(t *testing.T) {
	dir := t.TempDir()
	mappingPath := filepath.Join(dir, "mappings.json")
	sessionPath := writeBotTestSession(t, dir, "hello")
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{
			Text:   "continue after gateway restart",
			Status: control.GoalStatusRunning,
		},
		Run: agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}
	msg := InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/status",
		MessageID: "msg-1",
	}
	key := BuildSessionKey(msg.Session())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir, SessionMappingPath: mappingPath}, nil, logger)
	if err := gw.setSessionMapping(key, sessionPath, "/workspace"); err != nil {
		t.Fatalf("setSessionMapping: %v", err)
	}

	restarted := NewGateway(GatewayConfig{SessionDir: dir, SessionMappingPath: mappingPath}, nil, logger)
	fa := newFakeAdapter(PlatformQQ, "fake-qq")
	restarted.handleSlashCommand(context.Background(), fa, key, msg)

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	text := sent[0].Text
	for _, want := range []string{
		"会话: " + shortSessionID(sessionPath),
		"目标: continue after gateway restart",
		"目标状态: running",
		"运行状态: idle",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status missing %q after restart:\n%s", want, text)
		}
	}
}

func TestGatewayRecordRuntimeWaitPersistsApprovalAndAsk(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal:   agent.RuntimeGoalMeta{Text: "needs a decision", Status: control.GoalStatusRunning},
		Budget: agent.RuntimeBudgetMeta{DailyWakeupLimit: 3, MaxGoalAutoTurns: 8, DailyWakeups: 1},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test"})
	ctrl.SetGoal("needs a decision")
	gw.controllers["remote-wait"] = &sessionState{ctrl: ctrl}

	gw.recordRuntimeWait("remote-wait", agent.RuntimeWaitMeta{
		Kind:       "approval",
		Reason:     "approval required",
		ApprovalID: "approval-1",
		Tool:       "bash",
		Subject:    "git push",
	})

	loaded, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta after approval wait: err=%v ok=%v", err, ok)
	}
	if loaded.Wait.Kind != "approval" || loaded.Wait.ApprovalID != "approval-1" ||
		loaded.Wait.Tool != "bash" || loaded.Wait.Subject != "git push" ||
		loaded.Run.Status != "waiting_approval" {
		t.Fatalf("approval wait not persisted: %+v", loaded)
	}
	if loaded.Budget.DailyWakeupLimit != 3 || loaded.Budget.MaxGoalAutoTurns != 8 || loaded.Budget.DailyWakeups != 1 {
		t.Fatalf("budget should be preserved: %+v", loaded.Budget)
	}

	gw.recordRuntimeWait("remote-wait", agent.RuntimeWaitMeta{
		Kind:    "ask",
		Reason:  "user answer required",
		AskID:   "ask-1",
		Subject: "Which release channel?",
	})

	loaded, ok, err = agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta after ask wait: err=%v ok=%v", err, ok)
	}
	if loaded.Wait.Kind != "ask" || loaded.Wait.AskID != "ask-1" ||
		loaded.Wait.Subject != "Which release channel?" || loaded.Run.Status != "waiting_ask" {
		t.Fatalf("ask wait not persisted: %+v", loaded)
	}
}

func TestGatewayRecordsAndClearsRuntimeWait(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test"})
	gw.controllers["remote-wait"] = &sessionState{ctrl: ctrl}

	gw.recordRuntimeWait("remote-wait", agent.RuntimeWaitMeta{
		Kind:       "approval",
		Reason:     "approval required",
		ApprovalID: "approval-1",
		Tool:       "shell",
		Subject:    "go test ./...",
	})

	meta, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta after record: ok=%v err=%v", ok, err)
	}
	if meta.Run.Status != "waiting_approval" || meta.Wait.ApprovalID != "approval-1" || meta.Wait.Tool != "shell" {
		t.Fatalf("wait not recorded: %+v", meta)
	}

	gw.clearRuntimeWait("remote-wait", "approval", "approval-1")

	meta, ok, err = agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta after clear: ok=%v err=%v", ok, err)
	}
	if meta.Wait.Kind != "" || meta.Run.Status != "running" {
		t.Fatalf("wait not cleared: %+v", meta)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sessionPath, 2)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: ok=%v err=%v", ok, err)
	}
	if len(events) != 2 || events[0].Type != "wait_started" || events[1].Type != "wait_cleared" {
		t.Fatalf("unexpected timeline: %+v", events)
	}
}

func TestGatewayApprovalsCommandUsesMappedSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "ship release", Status: control.GoalStatusRunning},
		Run:  agent.RuntimeRunMeta{Status: agent.RunStatusWaitingApproval},
		Wait: agent.RuntimeWaitMeta{
			Kind:       "approval",
			Reason:     "approval required",
			ApprovalID: "approval-1",
			Tool:       "shell",
			Subject:    "go test ./...",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	if err := gw.setSessionMapping("remote-approvals", sessionPath, "/workspace"); err != nil {
		t.Fatalf("setSessionMapping: %v", err)
	}
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-approvals", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/approvals",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	text := sent[0].Text
	for _, want := range []string{
		"待处理审批/提问（" + shortSessionID(sessionPath) + "）：",
		"审批: approval-1",
		"tool=shell",
		"subject=go test ./...",
		"原因: approval required",
		"命令: /approve approval-1 或 /deny approval-1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("approvals missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, sessionPath) {
		t.Fatalf("approvals should not expose full session path:\n%s", text)
	}
}

func TestGatewayApprovalsCommandShowsActiveAskOptions(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Run: agent.RuntimeRunMeta{Status: agent.RunStatusWaitingAsk},
		Wait: agent.RuntimeWaitMeta{
			Kind:   "ask",
			Reason: "user answer required",
			AskID:  "ask-1",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test"})
	gw.controllers["remote-ask"] = &sessionState{
		ctrl: ctrl,
		pendingAsks: map[string][]event.AskQuestion{"ask-1": {{
			ID:     "q1",
			Prompt: "Ship now?",
			Options: []event.AskOption{
				{Label: "yes"},
				{Label: "no"},
			},
		}}},
	}
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-ask", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/approvals",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	text := sent[0].Text
	for _, want := range []string{
		"提问: ask-1",
		"问题 q1: Ship now? [yes / no]",
		"命令: /answer ask-1 <选项>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ask approvals missing %q:\n%s", want, text)
		}
	}
}

func TestGatewayTimelineCommandUsesMappedSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	if err := agent.AppendRuntimeTimeline(sessionPath, agent.RuntimeTimelineEvent{
		Time:      now,
		Type:      "wait_started",
		Source:    "bot",
		RunStatus: "waiting_approval",
		WaitKind:  "approval",
		WaitID:    "approval-1",
		Tool:      "bash",
		Subject:   "git push origin feature/agentos-roadmap-todo",
		Reason:    "approval required",
	}); err != nil {
		t.Fatalf("AppendRuntimeTimeline: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	if err := gw.setSessionMapping("remote-timeline", sessionPath, "/workspace"); err != nil {
		t.Fatalf("setSessionMapping: %v", err)
	}
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-timeline", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/timeline 5",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	text := sent[0].Text
	for _, want := range []string{
		"最近运行事件（" + shortSessionID(sessionPath) + "）：",
		"wait_started",
		"source=bot",
		"run=waiting_approval",
		"wait=approval:approval-1",
		"tool=bash",
		"reason=approval required",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("timeline missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, sessionPath) {
		t.Fatalf("timeline should not expose full session path:\n%s", text)
	}
}

func TestGatewayTimelineCommandRequiresSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-empty", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/timeline",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "没有绑定 Reasonix session") {
		t.Fatalf("unexpected timeline response: %+v", sent)
	}
}

func TestGatewayWakeupsCommandFiltersTimeline(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	events := []agent.RuntimeTimelineEvent{
		{Time: now, Type: "wait_started", Source: "bot", WaitKind: "approval", WaitID: "approval-1"},
		{Time: now.Add(time.Minute), Type: "intent_queued", Source: "cron", Reason: "cron", EventID: "cron-1", RunStatus: "queued"},
		{Time: now.Add(2 * time.Minute), Type: "wakeup_budget_blocked", Source: "webhook", Reason: "daily budget exhausted", EventID: "delivery-1"},
		{Time: now.Add(3 * time.Minute), Type: "run_finished", Source: "daemon", RunStatus: "idle"},
		{Time: now.Add(4 * time.Minute), Type: "file_change_detected", Source: "file_watch", Reason: "file_change", RunStatus: "queued"},
	}
	for _, event := range events {
		if err := agent.AppendRuntimeTimeline(sessionPath, event); err != nil {
			t.Fatalf("AppendRuntimeTimeline: %v", err)
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	if err := gw.setSessionMapping("remote-wakeups", sessionPath, "/workspace"); err != nil {
		t.Fatalf("setSessionMapping: %v", err)
	}
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-wakeups", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/wakeups 2",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	text := sent[0].Text
	for _, want := range []string{
		"最近唤醒历史（" + shortSessionID(sessionPath) + "）：",
		"wakeup_budget_blocked",
		"source=webhook",
		"file_change_detected",
		"source=file_watch",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("wakeups missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"wait_started", "run_finished", "source=cron", sessionPath} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("wakeups should not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestGatewayWakeupsCommandRequiresSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-empty", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/wakeups",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "没有绑定 Reasonix session") {
		t.Fatalf("unexpected wakeups response: %+v", sent)
	}
}

func TestGatewayRecapCommandSummarizesTodayAndPendingDecision(t *testing.T) {
	dir := t.TempDir()
	sessionPath := writeBotTestSession(t, dir, "hello")
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Run: agent.RuntimeRunMeta{Status: agent.RunStatusWaitingApproval},
		Wait: agent.RuntimeWaitMeta{
			Kind:       "approval",
			Reason:     "approval required",
			ApprovalID: "approval-1",
			Tool:       "bash",
			Subject:    "git push fork feature/agentos-roadmap-todo",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}
	previous := time.Date(2026, 6, 12, 23, 0, 0, 0, time.Local)
	today := time.Date(2026, 6, 13, 9, 0, 0, 0, time.Local)
	events := []agent.RuntimeTimelineEvent{
		{Time: previous, Type: "run_finished", Source: "daemon", RunStatus: "idle"},
		{Time: today, Type: "intent_queued", Source: "cron", Reason: "cron", EventID: "cron-1", RunStatus: "queued"},
		{Time: today.Add(time.Minute), Type: "model_usage", Source: "cron", Model: "deepseek-reasoner", Total: 1234, Cost: 0.25, Currency: "USD"},
		{Time: today.Add(2 * time.Minute), Type: "wait_started", Source: "bot", WaitKind: "approval", WaitID: "approval-1", Tool: "bash", Subject: "git push fork feature/agentos-roadmap-todo", Reason: "approval required"},
		{Time: today.Add(3 * time.Minute), Type: "wakeup_budget_blocked", Source: "webhook", Reason: "daily budget exhausted", EventID: "delivery-1"},
		{Time: today.Add(4 * time.Minute), Type: "run_finished", Source: "daemon", RunStatus: "waiting_approval"},
	}
	for _, event := range events {
		if err := agent.AppendRuntimeTimeline(sessionPath, event); err != nil {
			t.Fatalf("AppendRuntimeTimeline: %v", err)
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{SessionDir: dir}, nil, logger)
	if err := gw.setSessionMapping("remote-recap", sessionPath, "/workspace"); err != nil {
		t.Fatalf("setSessionMapping: %v", err)
	}
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-recap", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/recap 2026-06-13",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	text := sent[0].Text
	for _, want := range []string{
		"任务复盘（" + shortSessionID(sessionPath) + "，2026-06-13）：",
		"自动处理: 唤醒 2 次，运行完成 1 次，等待用户 1 次，预算阻断 1 次。",
		"模型使用: 调用 1 次，tokens 1234，费用 USD 0.2500。",
		"最近事件:",
		"intent_queued",
		"model_usage",
		"待决策:",
		"- 审批 approval-1 tool=bash subject=git push fork feature/agentos-roadmap-todo",
		"原因: approval required",
		"命令: /approve approval-1 或 /deny approval-1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("recap missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"2026-06-12", sessionPath} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("recap should not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestGatewayRecapCommandRequiresSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	gw.handleSlashCommand(context.Background(), fa, "remote-empty", InboundMessage{
		Platform:  PlatformQQ,
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/recap",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "没有绑定 Reasonix session") {
		t.Fatalf("unexpected recap response: %+v", sent)
	}
}

func writeBotTestSession(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sessionPath := filepath.Join(dir, "saved-session.jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: content})
	if err := sess.Save(sessionPath); err != nil {
		t.Fatalf("Save session: %v", err)
	}
	return sessionPath
}

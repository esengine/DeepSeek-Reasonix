package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
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
	if !strings.Contains(sent[0].Text, "没有找到可匹配的待审批操作") {
		t.Fatalf("sent text = %q, want pending approval guidance", sent[0].Text)
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

	model, root := gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu})
	if model != "feishu-model" || root != "/feishu" {
		t.Fatalf("feishu options = %q,%q; want channel override", model, root)
	}

	model, root = gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformWeixin})
	if model != "global-model" || root != "/weixin" {
		t.Fatalf("weixin options = %q,%q; want global model and channel root", model, root)
	}

	model, root = gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformQQ})
	if model != "global-model" || root != "/global" {
		t.Fatalf("qq options = %q,%q; want global defaults", model, root)
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

	model, root := gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu, ConnectionID: "feishu-lark"})
	if model != "lark-model" || root != "/lark" {
		t.Fatalf("lark options = %q,%q; want connection override", model, root)
	}
}

package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"reasonix/internal/tool"
)

// fakeAdapter 是一个内存中的假适配器，用于测试 BotGateway。
type fakeAdapter struct {
	mu       sync.Mutex
	stopOnce sync.Once
	platform Platform
	name     string
	msgCh    chan InboundMessage
	sent     []OutboundMessage
	started  bool
	startErr error
}

type resultAdapter struct {
	*fakeAdapter
	result SendResult
	err    error
}

func (a *resultAdapter) Send(_ context.Context, msg OutboundMessage) (SendResult, error) {
	a.mu.Lock()
	a.sent = append(a.sent, msg)
	a.mu.Unlock()
	return a.result, a.err
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
	f.stopOnce.Do(func() {
		close(f.msgCh)
	})
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

type blockingSendAdapter struct {
	*fakeAdapter
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingSendAdapter(platform Platform, name string) *blockingSendAdapter {
	return &blockingSendAdapter{
		fakeAdapter: newFakeAdapter(platform, name),
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
}

func (f *blockingSendAdapter) Send(ctx context.Context, msg OutboundMessage) (SendResult, error) {
	f.once.Do(func() { close(f.entered) })
	select {
	case <-f.release:
	case <-ctx.Done():
		return SendResult{}, ctx.Err()
	}
	return f.fakeAdapter.Send(ctx, msg)
}

type fakeReactionAdapter struct {
	*fakeAdapter
	reactions []string
	cleanups  []string
}

type gatewayFakeProvider struct{}

func (gatewayFakeProvider) Name() string { return "fake" }

func (gatewayFakeProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

func (f *fakeReactionAdapter) AddPendingReaction(ctx context.Context, messageID string) (func(), error) {
	f.mu.Lock()
	f.reactions = append(f.reactions, messageID)
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.cleanups = append(f.cleanups, messageID)
	}, nil
}

func (f *fakeReactionAdapter) cleanupMessages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.cleanups))
	copy(out, f.cleanups)
	return out
}

type queueTestController struct {
	botController
	mu       sync.Mutex
	steers   []string
	canceled bool
}

func (c *queueTestController) Steer(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steers = append(c.steers, text)
}

func (c *queueTestController) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.canceled = true
}

func (c *queueTestController) SessionPath() string   { return "" }
func (c *queueTestController) WorkspaceRoot() string { return "" }

func (c *queueTestController) steered() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.steers))
	copy(out, c.steers)
	return out
}

func (c *queueTestController) wasCanceled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canceled
}

type rotatingBotController struct {
	botController
	path     string
	newPath  string
	newCalls int
	closed   bool
}

func (c *rotatingBotController) Running() bool { return false }
func (c *rotatingBotController) NewSession() error {
	c.newCalls++
	c.path = c.newPath
	return nil
}
func (c *rotatingBotController) SessionPath() string { return c.path }
func (c *rotatingBotController) Close()              { c.closed = true }

type runtimeStatusBotController struct {
	botController
	status        control.RuntimeStatus
	workspaceRoot string
	sessionPath   string
	closed        bool
}

func (c *runtimeStatusBotController) RuntimeStatus() control.RuntimeStatus { return c.status }
func (c *runtimeStatusBotController) WorkspaceRoot() string                { return c.workspaceRoot }
func (c *runtimeStatusBotController) SessionPath() string                  { return c.sessionPath }
func (c *runtimeStatusBotController) Close()                               { c.closed = true }

type blockingApprovalController struct {
	botController
	emit     func(event.Event)
	emitted  chan struct{}
	approved chan struct{}
	done     chan struct{}
	once     sync.Once
}

func (c *blockingApprovalController) RunTurn(ctx context.Context, input string) error {
	c.emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "appr-1", Tool: "bash", Subject: "sample command"}})
	close(c.emitted)
	select {
	case <-c.approved:
		close(c.done)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *blockingApprovalController) Approve(id string, allow, session, persist bool) {
	c.once.Do(func() { close(c.approved) })
}

type blockingAskController struct {
	botController
	emit     func(event.Event)
	emitted  chan struct{}
	answered chan []event.AskAnswer
	done     chan struct{}
	once     sync.Once
}

func (c *blockingAskController) RunTurn(ctx context.Context, input string) error {
	c.emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: "ask-1", Questions: []event.AskQuestion{{
		ID:     "q1",
		Header: "Planner",
		Prompt: "Which plan?",
		Options: []event.AskOption{
			{Label: "Small patch"},
			{Label: "Refactor"},
		},
	}}}})
	close(c.emitted)
	select {
	case <-c.answered:
		close(c.done)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *blockingAskController) AnswerQuestion(id string, answers []event.AskAnswer) {
	c.once.Do(func() { c.answered <- answers })
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
	defer gw.Stop()
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

func TestGatewaySendToAdapterReleasesLockBeforeSend(t *testing.T) {
	adapter := newBlockingSendAdapter(PlatformFeishu, "blocking-feishu")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGatewayWithAdapterBindings(GatewayConfig{}, []AdapterBinding{{
		ID:       "feishu-lark",
		Domain:   "lark",
		Platform: PlatformFeishu,
		Adapter:  adapter,
	}}, logger)

	sendDone := make(chan error, 1)
	go func() {
		_, err := gw.SendToAdapter(context.Background(), "feishu-lark", "lark", OutboundMessage{ChatID: "chat", Text: "hello"})
		sendDone <- err
	}()

	select {
	case <-adapter.entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("adapter send did not start")
	}

	updateDone := make(chan struct{})
	go func() {
		gw.UpdateConnectionToolApprovalMode("feishu-lark", "ask")
		close(updateDone)
	}()
	select {
	case <-updateDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("UpdateConnectionToolApprovalMode blocked behind SendToAdapter")
	}

	close(adapter.release)
	select {
	case err := <-sendDone:
		if err != nil {
			t.Fatalf("SendToAdapter returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendToAdapter did not finish after release")
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

func TestGatewayRejectsBeforeInboundEnrichment(t *testing.T) {
	adapter := newFakeAdapter(PlatformFeishu, "feishu")
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{
		Enabled: true,
		Users:   map[Platform][]string{PlatformFeishu: {"allowed-user"}},
	}}, map[Platform]Adapter{PlatformFeishu: adapter}, discardLogger())

	mediaLoads := 0
	nameLoads := 0
	gw.handleMessage(context.Background(), AdapterBinding{ID: "feishu", Platform: PlatformFeishu, Adapter: adapter}, InboundMessage{
		Platform: PlatformFeishu,
		ChatType: ChatDM,
		ChatID:   "chat",
		UserID:   "blocked-user",
		Media: []InboundMedia{{Load: func(context.Context) ([]byte, string, error) {
			mediaLoads++
			return []byte("payload"), "payload.txt", nil
		}}},
		ResolveUserName: func(context.Context) string {
			nameLoads++
			return "Blocked User"
		},
	})

	if mediaLoads != 0 || nameLoads != 0 {
		t.Fatalf("pre-admission enrichment calls = media:%d name:%d, want zero", mediaLoads, nameLoads)
	}
}

func TestGatewayRoleListsGrantAllowlistAdmission(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Admins: map[Platform][]string{
				PlatformFeishu: {"admin_user"},
			},
			Approvers: map[Platform][]string{
				PlatformFeishu: {"approver_user"},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if !gw.checkAllowlist(PlatformFeishu, InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, UserID: "admin_user"}) {
		t.Error("admin role should grant base bot admission")
	}
	if !gw.checkAllowlist(PlatformFeishu, InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, UserID: "approver_user"}) {
		t.Error("approver role should grant base bot admission")
	}
	if gw.checkAllowlist(PlatformFeishu, InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, UserID: "unknown_user"}) {
		t.Error("unknown user should still be rejected")
	}
}

func TestGatewayApproverRoleDoesNotGrantAdminCommands(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Approvers: map[Platform][]string{
				PlatformFeishu: {"approver_user"},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)
	msg := InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, UserID: "approver_user"}

	if !gw.checkCommandRole(PlatformFeishu, msg, "approver") {
		t.Error("approver should be allowed to run approver commands")
	}
	if gw.checkCommandRole(PlatformFeishu, msg, "admin") {
		t.Error("approver should not be allowed to run admin commands")
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

func TestGatewayGroupAllowlistStillNarrowsRoleAdmission(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Admins: map[Platform][]string{
				PlatformFeishu: {"admin_user"},
			},
			Groups: map[Platform][]string{
				PlatformFeishu: {"allowed_group"},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if !gw.checkAllowlist(PlatformFeishu, InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, ChatID: "direct", UserID: "admin_user"}) {
		t.Error("admin role admission should still allow direct messages")
	}
	if !gw.checkAllowlist(PlatformFeishu, InboundMessage{Platform: PlatformFeishu, ChatType: ChatGroup, ChatID: "allowed_group", UserID: "admin_user"}) {
		t.Error("admin role should pass in allowed group")
	}
	if gw.checkAllowlist(PlatformFeishu, InboundMessage{Platform: PlatformFeishu, ChatType: ChatGroup, ChatID: "unknown_group", UserID: "admin_user"}) {
		t.Error("admin role should still be rejected in an unknown group")
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

func TestGatewayNormalizesTaskGrantRecoveryShortcuts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	key := "recovery-key"
	gw.controllers[key] = &sessionState{
		pendingApprovals: map[string]event.Approval{
			"r1": {ID: "r1", Kind: "recovery", Recovery: &event.RecoveryApproval{CanGrantTask: true}},
		},
		lastApprovalID: "r1",
	}
	for input, want := range map[string]string{
		"1": "/recovery-continue r1",
		"2": "/recovery-continue-task r1",
		"3": "/recovery-revise r1",
	} {
		got, ok := gw.normalizeApprovalShortcut(key, input)
		if !ok || got != want {
			t.Fatalf("normalize %q = %q,%v; want %q,true", input, got, ok, want)
		}
	}
}

func TestGatewayNormalizesAskShortcutForPendingAsk(t *testing.T) {
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
	got, ok = gw.normalizeAskShortcut(key, "1;2")
	if !ok || got != "/answer ask-1 1;2" {
		t.Fatalf("normalize 1;2 = %q,%v; want /answer ask-1 1;2,true", got, ok)
	}
	got, ok = gw.normalizeAskShortcut(key, "freeform answer")
	if !ok || got != "/answer ask-1 freeform answer" {
		t.Fatalf("normalize freeform answer = %q,%v; want /answer ask-1 freeform answer,true", got, ok)
	}

	gw.controllers[key].pendingAsks["ask-2"] = []event.AskQuestion{
		{ID: "q1", Prompt: "First", Options: []event.AskOption{{Label: "A"}}},
		{ID: "q2", Prompt: "Second", Options: []event.AskOption{{Label: "B"}}},
	}
	gw.controllers[key].lastAskID = "ask-2"
	got, ok = gw.normalizeAskShortcut(key, "1")
	if !ok || got != "/answer ask-2 1" {
		t.Fatalf("normalize 1 on multi-question = %q,%v; want /answer ask-2 1,true", got, ok)
	}
	if _, ok := gw.normalizeAskShortcut(key, "/stop"); ok {
		t.Fatal("slash commands should not be normalized/routed by ask shortcut")
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

func TestGatewayNewSessionRemembersRotatedSessionPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var remembered string
	gw := NewGateway(GatewayConfig{
		OnSessionReady: func(msg InboundMessage, sessionID string) error {
			remembered = sessionID
			return nil
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
		Text:         "/new",
	}
	key := BuildSessionKey(msg.Session())
	sessionDir := t.TempDir()
	oldPath := agent.NewSessionPath(sessionDir, "old-model")
	exec := agent.New(gatewayFakeProvider{}, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: sessionDir, SessionPath: oldPath, Label: "fake-model"})
	leases := control.NewSessionLeaseKeeper()
	if err := leases.Rebind(oldPath); err != nil {
		t.Fatalf("bind old session lease: %v", err)
	}
	gw.controllers[key] = &sessionState{ctrl: ctrl, leases: leases, sessionPath: oldPath}

	gw.handleSlashCommand(context.Background(), adapter, key, msg)

	if remembered == "" || !strings.HasPrefix(remembered, "path:") {
		t.Fatalf("remembered session = %q, want path target", remembered)
	}
	if remembered == "path:"+oldPath {
		t.Fatalf("remembered session = %q, want rotated path", remembered)
	}
	if ctrl.SessionPath() == oldPath {
		t.Fatalf("controller session path was not rotated")
	}
	if got := leases.HeldPath(); got != agent.CanonicalSessionPath(ctrl.SessionPath()) {
		t.Fatalf("held lease = %q, want rotated path %q", got, agent.CanonicalSessionPath(ctrl.SessionPath()))
	}
	oldLease, err := agent.TryAcquireSessionLease(oldPath)
	if err != nil {
		t.Fatalf("old session lease was not released: %v", err)
	}
	oldLease.Release()
	gw.closeSessions()
}

func TestGatewayNewSessionLeaseFailureRetiresSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	readyCalls := 0
	gw := NewGateway(GatewayConfig{
		OnSessionReady: func(InboundMessage, string) error {
			readyCalls++
			return nil
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
		Text:         "/new",
	}
	key := BuildSessionKey(msg.Session())
	sessionDir := t.TempDir()
	oldPath := filepath.Join(sessionDir, "old.jsonl")
	newPath := filepath.Join(sessionDir, "new.jsonl")
	ctrl := &rotatingBotController{path: oldPath, newPath: newPath}
	leases := control.NewSessionLeaseKeeper()
	if err := leases.Rebind(oldPath); err != nil {
		t.Fatalf("bind old session lease: %v", err)
	}
	blocker, err := agent.TryAcquireSessionLease(newPath)
	if err != nil {
		t.Fatalf("hold rotated session lease: %v", err)
	}
	defer blocker.Release()
	gw.controllers[key] = &sessionState{ctrl: ctrl, leases: leases, sessionPath: oldPath}

	gw.handleSlashCommand(context.Background(), adapter, key, msg)

	gw.mu.Lock()
	_, exists := gw.controllers[key]
	gw.mu.Unlock()
	if exists {
		t.Fatal("lease-failed session remains registered")
	}
	if ctrl.newCalls != 1 || !ctrl.closed {
		t.Fatalf("controller lifecycle = new calls %d closed %v, want 1/true", ctrl.newCalls, ctrl.closed)
	}
	if got := leases.HeldPath(); got != "" {
		t.Fatalf("old lease remained held after retirement: %q", got)
	}
	oldLease, err := agent.TryAcquireSessionLease(oldPath)
	if err != nil {
		t.Fatalf("old session lease was not released after retirement: %v", err)
	}
	oldLease.Release()
	if readyCalls != 0 {
		t.Fatalf("session-ready callback ran %d times after failed creation", readyCalls)
	}
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "新会话创建失败") || strings.Contains(sent[0].Text, "已开始新会话") {
		t.Fatalf("sent messages = %+v, want a single creation-failed response", sent)
	}
}

func TestGatewayCloseSessionStateReleasesSessionLease(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	leases := control.NewSessionLeaseKeeper()
	if err := leases.Rebind(path); err != nil {
		t.Fatalf("bind session lease: %v", err)
	}
	ctrl := control.New(control.Options{})

	gw.closeSessionState(&sessionState{ctrl: ctrl, leases: leases})

	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("session lease was not released after controller close: %v", err)
	}
	lease.Release()
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

func TestGatewayUpdateConnectionToolApprovalModeUpdatesHashedActiveSessions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		ToolApprovalMode: "ask",
		ConnectionChannels: map[string]ChannelConfig{
			"feishu-lark": {ToolApprovalMode: "yolo"},
		},
	}, nil, logger)

	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-lark",
		Domain:       "lark",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
	}
	key := BuildSessionKey(msg.Session())
	if strings.HasPrefix(key, msg.ConnectionID) {
		t.Fatalf("test setup expected hashed key, got %q", key)
	}
	ctrl := control.New(control.Options{})
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	gw.controllers[key] = &sessionState{ctrl: ctrl, platform: msg.Platform, connectionID: msg.ConnectionID}

	gw.UpdateConnectionToolApprovalMode("feishu-lark", control.ToolApprovalAsk)

	if got := ctrl.ToolApprovalMode(); got != control.ToolApprovalAsk {
		t.Fatalf("active session mode = %q, want ask", got)
	}
	if got := gw.cfg.ConnectionChannels["feishu-lark"].ToolApprovalMode; got != control.ToolApprovalAsk {
		t.Fatalf("connection default mode = %q, want ask", got)
	}
}

func TestGatewayUpdateConnectionToolApprovalModeInheritsGatewayDefault(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		ToolApprovalMode: control.ToolApprovalAuto,
		ConnectionChannels: map[string]ChannelConfig{
			"feishu-lark":   {ToolApprovalMode: control.ToolApprovalYolo},
			"feishu-feishu": {ToolApprovalMode: control.ToolApprovalYolo},
		},
	}, nil, logger)

	larkMsg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-lark",
		Domain:       "lark",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
	}
	larkKey := BuildSessionKey(larkMsg.Session())
	larkCtrl := control.New(control.Options{})
	larkCtrl.SetToolApprovalMode(control.ToolApprovalYolo)
	gw.controllers[larkKey] = &sessionState{ctrl: larkCtrl, platform: larkMsg.Platform, connectionID: larkMsg.ConnectionID}

	otherCtrl := control.New(control.Options{})
	otherCtrl.SetToolApprovalMode(control.ToolApprovalYolo)
	gw.controllers["other-hashed-key"] = &sessionState{ctrl: otherCtrl, platform: PlatformFeishu, connectionID: "feishu-feishu"}

	gw.UpdateConnectionToolApprovalMode("feishu-lark", "")

	if got := gw.cfg.ConnectionChannels["feishu-lark"].ToolApprovalMode; got != "" {
		t.Fatalf("connection override = %q, want empty inherit", got)
	}
	if got := larkCtrl.ToolApprovalMode(); got != control.ToolApprovalAuto {
		t.Fatalf("lark active session mode = %q, want inherited auto", got)
	}
	if got := otherCtrl.ToolApprovalMode(); got != control.ToolApprovalYolo {
		t.Fatalf("other connection mode = %q, want unchanged yolo", got)
	}
}

func TestGatewayApprovalReplyUnblocksWedgedTurn(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{AllowAll: true}}, nil, logger)
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	binding := AdapterBinding{ID: "feishu", Platform: PlatformFeishu, Adapter: adapter}
	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "delete everything",
	}
	key := BuildSessionKey(msg.Session())
	sink := &sessionEventSink{}
	ctrl := &blockingApprovalController{
		emit:     sink.Emit,
		emitted:  make(chan struct{}),
		approved: make(chan struct{}),
		done:     make(chan struct{}),
	}
	gw.controllers[key] = &sessionState{
		ctrl:             ctrl,
		sink:             sink,
		pendingApprovals: make(map[string]event.Approval),
		pendingAsks:      make(map[string][]event.AskQuestion),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gw.dispatchLoop(ctx, binding)

	adapter.msgCh <- msg
	select {
	case <-ctrl.emitted:
	case <-time.After(2 * time.Second):
		t.Fatal("approval request was never emitted; turn did not start")
	}

	adapter.msgCh <- InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "/approve appr-1",
	}

	select {
	case <-ctrl.done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: /approve reply was not delivered while the turn blocked on approval")
	}
}

func TestGatewayAskReplyUnblocksWedgedTurn(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{AllowAll: true}}, nil, logger)
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	binding := AdapterBinding{ID: "feishu", Platform: PlatformFeishu, Adapter: adapter}
	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "choose a plan",
	}
	key := BuildSessionKey(msg.Session())
	sink := &sessionEventSink{}
	ctrl := &blockingAskController{
		emit:     sink.Emit,
		emitted:  make(chan struct{}),
		answered: make(chan []event.AskAnswer, 1),
		done:     make(chan struct{}),
	}
	gw.controllers[key] = &sessionState{
		ctrl:             ctrl,
		sink:             sink,
		pendingApprovals: make(map[string]event.Approval),
		pendingAsks:      make(map[string][]event.AskQuestion),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gw.dispatchLoop(ctx, binding)

	adapter.msgCh <- msg
	select {
	case <-ctrl.emitted:
	case <-time.After(2 * time.Second):
		t.Fatal("ask request was never emitted; turn did not start")
	}

	adapter.msgCh <- InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "1",
	}

	select {
	case <-ctrl.done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: ask reply was not delivered while the turn blocked on user choice")
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
	if !strings.Contains(sent[0].Text, "/projects") || !strings.Contains(sent[0].Text, "/attach session") || !strings.Contains(sent[0].Text, "/search all") {
		t.Fatalf("help = %q, want project/session commands", sent[0].Text)
	}
}

func TestGatewayProjectCommandsListAndUseProjectOverride(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := t.TempDir()
	alpha := filepath.Join(base, "alpha-project")
	beta := filepath.Join(base, "beta-project")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(beta, 0o755); err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(GatewayConfig{
		WorkspaceRoot: alpha,
		ConnectionChannels: map[string]ChannelConfig{
			"weixin-main": {WorkspaceRoot: beta},
		},
	}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	msg := InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin-main",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "/projects",
	}
	key := BuildSessionKey(msg.Session())

	gw.handleSlashCommand(context.Background(), adapter, key, msg)
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "alpha-project") || !strings.Contains(sent[0].Text, "beta-project") {
		t.Fatalf("/projects sent = %#v, want both projects", sent)
	}

	msg.Text = "/use project alpha"
	gw.handleSlashCommand(context.Background(), adapter, key, msg)
	_, root, _ := gw.sessionOptionsForMessage(msg)
	if canonicalBotPath(root) != canonicalBotPath(alpha) {
		t.Fatalf("workspace after /use project = %q, want %q", root, alpha)
	}

	msg.Text = "/use project default"
	gw.handleSlashCommand(context.Background(), adapter, key, msg)
	_, root, _ = gw.sessionOptionsForMessage(msg)
	if canonicalBotPath(root) != canonicalBotPath(beta) {
		t.Fatalf("workspace after /use project default = %q, want connection default %q", root, beta)
	}
}

func TestGatewaySessionsSearchAndAttachSessionOverride(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	projectRoot := filepath.Join(t.TempDir(), "attach-project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionDir := botSessionDir(projectRoot)
	sessionPath := filepath.Join(sessionDir, "attached.jsonl")
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "needle attach conversation"})
	if err := sess.Save(sessionPath); err != nil {
		t.Fatalf("Save session: %v", err)
	}
	if err := agent.UpdateSessionMeta(sessionPath, "model-a", "needle attach conversation", 1, true); err != nil {
		t.Fatalf("UpdateSessionMeta: %v", err)
	}
	gw := NewGateway(GatewayConfig{WorkspaceRoot: projectRoot}, nil, logger)
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-lark",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "/sessions search needle",
	}
	key := BuildSessionKey(msg.Session())

	gw.handleSlashCommand(context.Background(), adapter, key, msg)
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "needle attach") || !strings.Contains(sent[0].Text, "s1") {
		t.Fatalf("/sessions sent = %#v, want indexed session", sent)
	}

	msg.Text = "/attach session s1"
	gw.handleSlashCommand(context.Background(), adapter, key, msg)
	profile := gw.sessionProfileForMessage(msg)
	if canonicalBotPath(profile.sessionPath) != canonicalBotPath(sessionPath) {
		t.Fatalf("attached session path = %q, want %q", profile.sessionPath, sessionPath)
	}
	if canonicalBotPath(profile.workspaceRoot) != canonicalBotPath(projectRoot) {
		t.Fatalf("attached workspace root = %q, want %q", profile.workspaceRoot, projectRoot)
	}
}

func TestGatewayRuntimeOverridePreservesControllersWithActiveWork(t *testing.T) {
	tests := []struct {
		name         string
		status       control.RuntimeStatus
		admittedTurn bool
	}{
		{name: "foreground turn", status: control.RuntimeStatus{Running: true}},
		{name: "pending prompt", status: control.RuntimeStatus{PendingPrompt: true}},
		{name: "background job", status: control.RuntimeStatus{BackgroundJobs: 1}},
		{name: "admitted turn", admittedTurn: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			gw := NewGateway(GatewayConfig{}, nil, logger)
			msg := InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, ChatID: "chat", UserID: "user"}
			key := BuildSessionKey(msg.Session())
			ctrl := &runtimeStatusBotController{status: tc.status, workspaceRoot: "/old"}
			state := &sessionState{ctrl: ctrl, workspaceRoot: "/old"}
			gw.controllers[key] = state
			gw.sessionOverrides[key] = sessionRuntimeOverride{channel: ChannelConfig{WorkspaceRoot: "/old"}, label: "project:old"}
			if tc.admittedTurn {
				if result := gw.sessions.TryAcquireWithQueue(key, msg, QueueOptions{}); !result.Acquired {
					t.Fatalf("admit turn: %+v", result)
				}
			}

			text := gw.handleUseProjectCommand(key, "/use project default")
			if !strings.Contains(text, "请先完成或停止") {
				t.Fatalf("busy response = %q", text)
			}
			if ctrl.closed || gw.controllers[key] != state {
				t.Fatalf("active controller was replaced or closed: closed=%v installed=%v", ctrl.closed, gw.controllers[key] == state)
			}
			if override, ok := gw.sessionOverrides[key]; !ok || override.label != "project:old" {
				t.Fatalf("active override changed: %+v, present=%v", override, ok)
			}
		})
	}
}

func TestGatewayDefersProfileMismatchWhileBackgroundWorkIsActive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	newRoot := t.TempDir()
	gw := NewGateway(GatewayConfig{WorkspaceRoot: newRoot}, nil, logger)
	msg := InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, ChatID: "chat", UserID: "user"}
	key := BuildSessionKey(msg.Session())
	ctrl := &runtimeStatusBotController{
		status: control.RuntimeStatus{BackgroundJobs: 1}, workspaceRoot: t.TempDir(),
	}
	state := &sessionState{ctrl: ctrl, workspaceRoot: ctrl.workspaceRoot}
	gw.controllers[key] = state

	got := gw.getOrCreateSession(context.Background(), key, msg)
	if got != state || gw.controllers[key] != state || ctrl.closed {
		t.Fatalf("profile mismatch canceled active work: gotOld=%v installed=%v closed=%v", got == state, gw.controllers[key] == state, ctrl.closed)
	}
	if state.workspaceRoot == newRoot {
		t.Fatalf("deferred runtime change mutated the live profile to %q", state.workspaceRoot)
	}
}

func TestGatewaySearchAllSearchesIndexedProjects(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "needle.txt"), []byte("alpha\nunique-cross-project-needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(GatewayConfig{WorkspaceRoot: projectRoot}, nil, logger)

	text := gw.handleProjectSearchCommand(context.Background(), "/search all unique-cross-project-needle")
	if !strings.Contains(text, "needle.txt") || !strings.Contains(text, "unique-cross-project-needle") {
		t.Fatalf("search text = %q, want file hit", text)
	}
}

func TestSearchBotProjectsFallbackStopsAtLimit(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "one.txt"), []byte("first fallback needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "two.txt"), []byte("second fallback needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projects := []botProjectEntry{{
		ID:   "p1",
		Name: "project",
		Root: projectRoot,
	}}

	results, err := searchBotProjectsFallback(context.Background(), projects, []string{projectRoot}, "fallback needle", 1)
	if err != nil {
		t.Fatalf("fallback search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("fallback results = %d, want 1", len(results))
	}
	if results[0].ProjectID != "p1" || !strings.Contains(results[0].Text, "fallback needle") {
		t.Fatalf("fallback result = %#v, want project hit", results[0])
	}
}

func TestSearchBotProjectsFallbackHonorsContextCancel(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "needle.txt"), []byte("fallback needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projects := []botProjectEntry{{
		ID:   "p1",
		Name: "project",
		Root: projectRoot,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := searchBotProjectsFallback(ctx, projects, []string{projectRoot}, "fallback needle", 1)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fallback error = %v, want context canceled", err)
	}
	if len(results) != 0 {
		t.Fatalf("fallback results = %d, want 0 after canceled context", len(results))
	}
}

func TestGatewayAdminRoleRequiredForProjectIndexCommands(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Users:   map[Platform][]string{PlatformWeixin: []string{"user"}},
			Admins:  map[Platform][]string{PlatformWeixin: []string{"admin"}},
		},
	}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	msg := InboundMessage{
		Platform: PlatformWeixin,
		ChatType: ChatDM,
		ChatID:   "chat",
		UserID:   "user",
		Text:     "/projects",
	}
	key := BuildSessionKey(msg.Session())

	gw.handleSlashCommand(context.Background(), adapter, key, msg)

	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "没有执行此 bot 命令的权限") {
		t.Fatalf("sent = %#v, want permission denial", sent)
	}
}

func TestGatewayDefaultQueueSteersActiveTurn(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{AllowAll: true}}, nil, logger)
	adapter := &fakeReactionAdapter{fakeAdapter: newFakeAdapter(PlatformFeishu, "fake-feishu")}
	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "please adjust the current task",
		MessageID:    "m2",
	}
	key := BuildSessionKey(msg.Session())
	ctrl := &queueTestController{}
	gw.controllers[key] = &sessionState{ctrl: ctrl, sink: &sessionEventSink{}}
	if result := gw.sessions.TryAcquireWithQueue(key, msg, QueueOptions{Mode: QueueModeFollowup}); !result.Acquired {
		t.Fatalf("failed to mark session active: %+v", result)
	}

	gw.handleMessage(context.Background(), AdapterBinding{ID: "feishu-feishu", Platform: PlatformFeishu, Adapter: adapter}, msg)

	if got := ctrl.steered(); len(got) != 1 || got[0] != msg.Text {
		t.Fatalf("steers = %#v, want current message", got)
	}
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "并入当前任务") {
		t.Fatalf("sent = %#v, want steer acknowledgement", sent)
	}
	if pending := gw.sessions.PendingCount(key); pending != 0 {
		t.Fatalf("pending = %d, want 0", pending)
	}
	if cleaned := adapter.cleanupMessages(); len(cleaned) != 1 || cleaned[0] != "m2" {
		t.Fatalf("cleanup messages = %#v, want [m2]", cleaned)
	}
}

func TestGatewayDefaultQueueSteersMediaOnlyActiveTurn(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	}))
	defer imageServer.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		Allowlist:     AllowlistConfig{AllowAll: true},
		WorkspaceRoot: t.TempDir(),
	}, nil, logger)
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		MediaURLs:    []string{imageServer.URL + "/image.png"},
		MessageID:    "m-media",
	}
	key := BuildSessionKey(msg.Session())
	ctrl := &queueTestController{}
	gw.controllers[key] = &sessionState{ctrl: ctrl, sink: &sessionEventSink{}}
	if result := gw.sessions.TryAcquireWithQueue(key, msg, QueueOptions{Mode: QueueModeFollowup}); !result.Acquired {
		t.Fatalf("failed to mark session active: %+v", result)
	}

	gw.handleMessage(context.Background(), AdapterBinding{ID: "feishu-feishu", Platform: PlatformFeishu, Adapter: adapter}, msg)

	got := ctrl.steered()
	if len(got) != 1 || !strings.Contains(got[0], "Attachments:") || !strings.Contains(got[0], "@.reasonix/attachments/") {
		t.Fatalf("steers = %#v, want saved attachment reference", got)
	}
}

func TestGatewayQueueFollowupKeepsMessagesForLaterTurns(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{AllowAll: true}}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	msg := InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin-weixin",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "first followup",
	}
	key := BuildSessionKey(msg.Session())
	ctrl := &queueTestController{}
	gw.controllers[key] = &sessionState{ctrl: ctrl, sink: &sessionEventSink{}}
	if result := gw.sessions.TryAcquireWithQueue(key, msg, QueueOptions{Mode: QueueModeFollowup}); !result.Acquired {
		t.Fatalf("failed to mark session active: %+v", result)
	}
	gw.sessions.SetQueueMode(key, QueueModeFollowup)

	gw.handleMessage(context.Background(), AdapterBinding{ID: "weixin-weixin", Platform: PlatformWeixin, Adapter: adapter}, msg)
	second := msg
	second.Text = "second followup"
	gw.handleMessage(context.Background(), AdapterBinding{ID: "weixin-weixin", Platform: PlatformWeixin, Adapter: adapter}, second)

	if got := ctrl.steered(); len(got) != 0 {
		t.Fatalf("steers = %#v, want none in followup mode", got)
	}
	if pending := gw.sessions.PendingCount(key); pending != 2 {
		t.Fatalf("pending = %d, want 2", pending)
	}
	next := gw.sessions.Release(key)
	if next == nil || next.Text != "first followup" {
		t.Fatalf("first release = %#v, want first followup", next)
	}
	next = gw.sessions.Release(key)
	if next == nil || next.Text != "second followup" {
		t.Fatalf("second release = %#v, want second followup", next)
	}
}

func TestGatewayQueueInterruptCancelsAndKeepsNewestMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{AllowAll: true}}, nil, logger)
	adapter := newFakeAdapter(PlatformQQ, "fake-qq")
	msg := InboundMessage{
		Platform:     PlatformQQ,
		ConnectionID: "qq",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "newest request",
	}
	key := BuildSessionKey(msg.Session())
	ctrl := &queueTestController{}
	gw.controllers[key] = &sessionState{ctrl: ctrl, sink: &sessionEventSink{}}
	if result := gw.sessions.TryAcquireWithQueue(key, msg, QueueOptions{Mode: QueueModeFollowup}); !result.Acquired {
		t.Fatalf("failed to mark session active: %+v", result)
	}
	gw.sessions.SetQueueMode(key, QueueModeInterrupt)

	gw.handleMessage(context.Background(), AdapterBinding{ID: "qq", Platform: PlatformQQ, Adapter: adapter}, msg)

	if !ctrl.wasCanceled() {
		t.Fatal("controller was not canceled")
	}
	next := gw.sessions.Release(key)
	if next == nil || next.Text != "newest request" {
		t.Fatalf("release = %#v, want newest request", next)
	}
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "稍后处理这条新消息") {
		t.Fatalf("sent = %#v, want interrupt acknowledgement", sent)
	}
}

func TestGatewayUnknownDMGetsPairingCode(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		PairingEnabled: true,
		Allowlist:      AllowlistConfig{Enabled: true, Users: map[Platform][]string{PlatformFeishu: nil}},
	}, nil, logger)
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "hello",
	}

	gw.handleMessage(context.Background(), AdapterBinding{ID: "feishu-feishu", Platform: PlatformFeishu, Adapter: adapter}, msg)

	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "配对码") || !strings.Contains(sent[0].Text, "reasonix bot pairing approve") {
		t.Fatalf("sent = %#v, want pairing instructions", sent)
	}
	reqs, err := ListPairingRequests()
	if err != nil {
		t.Fatalf("list pairing: %v", err)
	}
	if len(reqs) != 1 || reqs[0].UserID != "user" || reqs[0].ChatID != "chat" {
		t.Fatalf("pairing requests = %+v, want one request for user/chat", reqs)
	}
}

func TestGatewayAdminRoleRequiredForYoloWhenConfigured(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Users:   map[Platform][]string{PlatformWeixin: []string{"user"}},
			Admins:  map[Platform][]string{PlatformWeixin: []string{"admin"}},
		},
	}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	msg := InboundMessage{
		Platform: PlatformWeixin,
		ChatType: ChatDM,
		ChatID:   "chat",
		UserID:   "user",
		Text:     "/yolo on",
	}
	key := BuildSessionKey(msg.Session())

	gw.handleSlashCommand(context.Background(), adapter, key, msg)

	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "没有执行此 bot 命令的权限") {
		t.Fatalf("sent = %#v, want permission denial", sent)
	}
}

func TestGatewayIgnoresOutboundEchoMessageID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		IgnoreSelfMessages: true,
		Allowlist:          AllowlistConfig{AllowAll: true},
	}, nil, logger)
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	msg := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		MessageID:    "incoming",
		Text:         "/status",
	}
	if err := gw.sendText(context.Background(), adapter, msg, "reply"); err != nil {
		t.Fatalf("sendText: %v", err)
	}
	echo := msg
	echo.MessageID = "fake_msg_1"

	gw.handleMessage(context.Background(), AdapterBinding{ID: "feishu-feishu", Platform: PlatformFeishu, Adapter: adapter}, echo)

	if sent := adapter.sentMessages(); len(sent) != 1 {
		t.Fatalf("sent count = %d, want only original outbound echo registration", len(sent))
	}
}

func TestGatewayIgnoresConfiguredSelfUserID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		IgnoreSelfMessages: true,
		SelfUserIDs:        map[Platform][]string{PlatformWeixin: []string{"bot-user"}},
		Allowlist:          AllowlistConfig{AllowAll: true},
	}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	msg := InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin-weixin",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "bot-user",
		MessageID:    "self-message",
		Text:         "/status",
	}

	gw.handleMessage(context.Background(), AdapterBinding{ID: "weixin-weixin", Platform: PlatformWeixin, Adapter: adapter}, msg)

	if sent := adapter.sentMessages(); len(sent) != 0 {
		t.Fatalf("sent count = %d, want self message ignored", len(sent))
	}
}

func TestGatewayAdapterHealthTracksSend(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	gw := NewGatewayWithAdapterBindings(GatewayConfig{
		Enabled:   map[Platform]bool{PlatformFeishu: true},
		Allowlist: AllowlistConfig{AllowAll: true},
	}, []AdapterBinding{{ID: "feishu-lark", Platform: PlatformFeishu, Domain: "lark", Adapter: adapter}}, logger)
	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer gw.Stop()

	if _, err := gw.SendToAdapter(context.Background(), "feishu-lark", "lark", OutboundMessage{ChatID: "chat", ChatType: ChatDM, Text: "hello"}); err != nil {
		t.Fatalf("SendToAdapter: %v", err)
	}

	health := gw.AdapterHealth()
	if len(health) != 1 {
		t.Fatalf("health count = %d, want 1", len(health))
	}
	if health[0].ID != "feishu-lark" || health[0].Status != "running" || health[0].Sends != 1 || health[0].SendErrors != 0 {
		t.Fatalf("health = %+v, want running send count", health[0])
	}
}

func TestGatewayControlServerStatusAndSend(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	gw := NewGatewayWithAdapterBindings(GatewayConfig{
		Enabled:        map[Platform]bool{PlatformFeishu: true},
		Allowlist:      AllowlistConfig{AllowAll: true},
		ControlEnabled: true,
		ControlAddr:    "127.0.0.1:0",
		ControlToken:   "secret",
	}, []AdapterBinding{{ID: "feishu-lark", Platform: PlatformFeishu, Domain: "lark", Adapter: adapter}}, logger)
	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer gw.Stop()

	statusURL := "http://" + gw.ControlAddr() + "/status"
	resp, err := http.Get(statusURL)
	if err != nil {
		t.Fatalf("GET /status without token: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /status without token status = %d, want 401", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, statusURL, nil)
	if err != nil {
		t.Fatalf("new status request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /status status = %d, want 200", resp.StatusCode)
	}
	var status controlStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Status != "running" || len(status.Adapters) != 1 || status.Adapters[0].ID != "feishu-lark" {
		t.Fatalf("status = %+v, want running feishu-lark", status)
	}

	req, err = http.NewRequest(http.MethodPost, "http://"+gw.ControlAddr()+"/send", strings.NewReader(`{"connection_id":"feishu-lark","domain":"lark","chat_id":"chat","chat_type":"dm","text":"hello"}`))
	if err != nil {
		t.Fatalf("new send request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /send: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /send status = %d, want 200", resp.StatusCode)
	}
	if sent := adapter.sentMessages(); len(sent) != 1 || sent[0].Text != "hello" {
		t.Fatalf("sent = %+v, want hello", sent)
	}
	if health := gw.AdapterHealth(); len(health) != 1 || health[0].Sends != 1 {
		t.Fatalf("health = %+v, want one send", health)
	}

	req, err = http.NewRequest(http.MethodGet, "http://"+gw.ControlAddr()+"/metrics", nil)
	if err != nil {
		t.Fatalf("new metrics request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	metricsBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(metricsBody), "reasonix_bot_adapter_sends_total") {
		t.Fatalf("GET /metrics status=%d body=%q, want adapter metrics", resp.StatusCode, string(metricsBody))
	}
}

func TestControlSendReportsAndTracksPartialDelivery(t *testing.T) {
	adapter := &resultAdapter{
		fakeAdapter: newFakeAdapter(PlatformFeishu, "partial-feishu"),
		result: SendResult{
			MessageID:  "media-2",
			MessageIDs: []string{"text-1", "media-1", "media-2"},
		},
		err: errors.New("media-3 failed"),
	}
	gw := NewGatewayWithAdapterBindings(GatewayConfig{
		IgnoreSelfMessages: true,
	}, []AdapterBinding{{ID: "feishu-lark", Platform: PlatformFeishu, Domain: "lark", Adapter: adapter}}, discardLogger())
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"connection_id":"feishu-lark","domain":"lark","chat_id":"chat","text":"hello"}`))
	recorder := httptest.NewRecorder()

	gw.handleControlSend(recorder, req)

	if recorder.Code != http.StatusMultiStatus {
		t.Fatalf("POST /send status = %d, want %d", recorder.Code, http.StatusMultiStatus)
	}
	var response controlSendResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode partial response: %v", err)
	}
	if !response.Partial || response.Error != "media-3 failed" || len(response.MessageIDs) != 3 {
		t.Fatalf("partial response = %+v", response)
	}
	for _, messageID := range response.MessageIDs {
		if !gw.isSelfMessage(InboundMessage{
			Platform:     PlatformFeishu,
			ConnectionID: "feishu-lark",
			Domain:       "lark",
			ChatID:       "chat",
			MessageID:    messageID,
		}) {
			t.Fatalf("delivered message %q was not registered for echo suppression", messageID)
		}
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

func TestGatewayStoresQueuedReactionCleanupBeforeControllerExists(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	fa := &fakeReactionAdapter{fakeAdapter: newFakeAdapter(PlatformFeishu, "fake-feishu")}
	first := InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-feishu",
		Domain:       "feishu",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
		Text:         "first",
		MessageID:    "om_first",
	}
	key := BuildSessionKey(first.Session())
	if acquired, merged := gw.sessions.TryAcquire(key, first); !acquired || merged {
		t.Fatalf("first TryAcquire = (%v, %v), want acquired without merge", acquired, merged)
	}

	queued := first
	queued.Text = "queued"
	queued.MessageID = "om_queued"
	cleanup := gw.addPendingReaction(context.Background(), PlatformFeishu, fa, queued)
	if acquired, merged := gw.sessions.TryAcquire(key, queued); acquired || !merged {
		t.Fatalf("queued TryAcquire = (%v, %v), want merged while active", acquired, merged)
	}
	gw.storeReactionCleanup(key, cleanup)
	if _, ok := gw.controllers[key]; ok {
		t.Fatal("test setup expected no controller state yet")
	}
	if cleaned := fa.cleanupMessages(); len(cleaned) != 0 {
		t.Fatalf("cleanup messages before flush = %#v, want none", cleaned)
	}

	gw.flushReactionCleanups(key, nil)
	cleaned := fa.cleanupMessages()
	if len(cleaned) != 1 || cleaned[0] != "om_queued" {
		t.Fatalf("cleanup messages = %#v, want [om_queued]", cleaned)
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

func TestGatewaySessionOptionsPreferConnectionSessionMappingWorkspace(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		Model:         "global-model",
		WorkspaceRoot: "/global",
		ConnectionChannels: map[string]ChannelConfig{
			"weixin-main": {
				WorkspaceRoot: "/connection",
				SessionMappings: []SessionMapping{
					{RemoteID: "group-1", ChatType: string(ChatGroup), UserID: "other", Scope: "project", WorkspaceRoot: "/other"},
					{RemoteID: "group-1", ChatType: string(ChatGroup), UserID: "user-1", Scope: "project", WorkspaceRoot: "/mapped"},
				},
			},
		},
	}, nil, logger)

	model, root, mode := gw.sessionOptionsForMessage(InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin-main",
		ChatType:     ChatGroup,
		ChatID:       "group-1",
		UserID:       "user-1",
	})
	if model != "global-model" || root != "/mapped" || mode != "ask" {
		t.Fatalf("mapped options = %q,%q,%q; want global model, mapped workspace, ask", model, root, mode)
	}

	_, root, _ = gw.sessionOptionsForMessage(InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin-main",
		ChatType:     ChatGroup,
		ChatID:       "group-1",
		UserID:       "new-user",
	})
	if root != "/connection" {
		t.Fatalf("unmapped group user workspace = %q, want connection default", root)
	}
}

func TestGatewaySessionOptionsAllowSessionMappingGlobalWorkspace(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		WorkspaceRoot: "/global-default",
		ConnectionChannels: map[string]ChannelConfig{
			"weixin-main": {
				WorkspaceRoot:   "/connection",
				SessionMappings: []SessionMapping{{RemoteID: "dm-1", Scope: "global"}},
			},
		},
	}, nil, logger)

	_, root, _ := gw.sessionOptionsForMessage(InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin-main",
		ChatType:     ChatDM,
		ChatID:       "dm-1",
	})
	if root != "" {
		t.Fatalf("global mapping workspace = %q, want empty global workspace", root)
	}
}

func TestSessionStateMatchesRuntimeRejectsWorkspaceOrModelMismatch(t *testing.T) {
	ctrl := control.New(control.Options{WorkspaceRoot: "/old"})
	defer ctrl.Close()
	state := &sessionState{ctrl: ctrl, model: "model-a"}

	if !sessionStateMatchesRuntime(state, sessionRuntimeProfile{model: "model-a", workspaceRoot: "/old"}) {
		t.Fatal("session should match the controller workspace and model")
	}
	if sessionStateMatchesRuntime(state, sessionRuntimeProfile{model: "model-a", workspaceRoot: "/new"}) {
		t.Fatal("session matched a different workspace root")
	}
	if sessionStateMatchesRuntime(state, sessionRuntimeProfile{model: "model-b", workspaceRoot: "/old"}) {
		t.Fatal("session matched a different model")
	}
}

func TestSessionStateMatchesRuntimeRejectsAttachedSessionPathMismatch(t *testing.T) {
	root := t.TempDir()
	pathA := filepath.Join(root, "a.jsonl")
	pathB := filepath.Join(root, "b.jsonl")
	ctrl := control.New(control.Options{WorkspaceRoot: root, SessionPath: pathA})
	defer ctrl.Close()
	state := &sessionState{ctrl: ctrl, model: "model-a", workspaceRoot: root, sessionPath: pathA}

	if !sessionStateMatchesRuntime(state, sessionRuntimeProfile{model: "model-a", workspaceRoot: root, sessionPath: pathA}) {
		t.Fatal("attached session should match the same pinned path")
	}
	if sessionStateMatchesRuntime(state, sessionRuntimeProfile{model: "model-a", workspaceRoot: root, sessionPath: pathB}) {
		t.Fatal("attached session matched a different path")
	}
	if sessionStateMatchesRuntime(state, sessionRuntimeProfile{model: "model-a", workspaceRoot: root}) {
		t.Fatal("attached session matched an unpinned profile")
	}
}

func TestGatewaySessionOptionsPreferRemoteRouteOverride(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{
		Model:            "global-model",
		WorkspaceRoot:    "/global",
		ToolApprovalMode: "ask",
		ConnectionChannels: map[string]ChannelConfig{
			"feishu-lark": {Model: "lark-model", WorkspaceRoot: "/lark", ToolApprovalMode: "auto"},
		},
		Routes: []RouteConfig{{
			ConnectionID: "feishu-lark",
			ChatType:     ChatGroup,
			ChatID:       "group-1",
			Channel:      ChannelConfig{Model: "route-model", WorkspaceRoot: "/route", ToolApprovalMode: "yolo"},
		}},
	}, nil, logger)

	model, root, mode := gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu, ConnectionID: "feishu-lark", ChatType: ChatGroup, ChatID: "group-1"})
	if model != "route-model" || root != "/route" || mode != "yolo" {
		t.Fatalf("route options = %q,%q,%q; want route override", model, root, mode)
	}
	model, root, mode = gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu, ConnectionID: "feishu-lark", ChatType: ChatGroup, ChatID: "group-2"})
	if model != "lark-model" || root != "/lark" || mode != "auto" {
		t.Fatalf("non-matching options = %q,%q,%q; want connection override", model, root, mode)
	}
}

func TestBotSessionDirUsesProjectWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	got := botSessionDir(root)
	if got == "" || got == botSessionDir("") {
		t.Fatalf("project session dir = %q, want project-specific dir", got)
	}
}

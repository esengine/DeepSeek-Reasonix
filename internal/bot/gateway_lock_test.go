package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
)

type closeProbeBotController struct {
	*control.Controller
	onClose func()
}

type stopWaitBotController struct {
	botController
	started chan struct{}
	release chan struct{}
}

func (c *stopWaitBotController) RunTurn(context.Context, string) error {
	close(c.started)
	<-c.release
	return nil
}

func (c *stopWaitBotController) SessionPath() string   { return "" }
func (c *stopWaitBotController) WorkspaceRoot() string { return "" }
func (c *stopWaitBotController) Close()                {}

type countingStopAdapter struct {
	*fakeAdapter
	mu        sync.Mutex
	stopCalls int
}

type cancelBlockingStartAdapter struct {
	*fakeAdapter
	entered chan struct{}
}

func (a *cancelBlockingStartAdapter) Start(ctx context.Context) error {
	close(a.entered)
	<-ctx.Done()
	return ctx.Err()
}

func (a *countingStopAdapter) Stop() error {
	a.mu.Lock()
	a.stopCalls++
	a.mu.Unlock()
	return a.fakeAdapter.Stop()
}

func (a *countingStopAdapter) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopCalls
}

func (c *closeProbeBotController) Close() {
	if c.onClose != nil {
		c.onClose()
	}
}

func TestBotGatewayStopClosesSessionsWithoutGatewayLock(t *testing.T) {
	gw := &BotGateway{
		controllers: map[string]*sessionState{},
	}
	closed := make(chan struct{}, 1)
	gw.controllers["session"] = &sessionState{
		ctrl: &closeProbeBotController{
			Controller: control.New(control.Options{}),
			onClose: func() {
				gw.mu.Lock()
				gw.mu.Unlock() //nolint:staticcheck // probe: lock must be immediately acquirable
				closed <- struct{}{}
			},
		},
	}

	done := make(chan struct{})
	go func() {
		gw.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked while closing a controller")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("controller Close was not called")
	}
	if len(gw.controllers) != 0 {
		t.Fatalf("controllers retained after Stop: %d", len(gw.controllers))
	}
}

func TestBotGatewayStopWaitsForDispatchHandler(t *testing.T) {
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	entered := make(chan struct{})
	release := make(chan struct{})
	gw := NewGatewayWithAdapterBindings(GatewayConfig{
		Enabled:   map[Platform]bool{PlatformFeishu: true},
		Allowlist: AllowlistConfig{AllowAll: true},
		OnInbound: func(InboundMessage) {
			close(entered)
			<-release
		},
	}, []AdapterBinding{{ID: "feishu-lark", Platform: PlatformFeishu, Adapter: adapter}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	adapter.msgCh <- InboundMessage{ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "/status"}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("dispatch handler did not start")
	}

	done := make(chan struct{})
	go func() {
		gw.Stop()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Stop returned while a dispatch handler was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the dispatch handler exited")
	}
}

func TestBotGatewayStopWaitsForTurn(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	gw := NewGatewayWithAdapterBindings(GatewayConfig{
		Enabled:   map[Platform]bool{PlatformWeixin: true},
		Allowlist: AllowlistConfig{AllowAll: true},
	}, []AdapterBinding{{ID: "weixin", Platform: PlatformWeixin, Adapter: adapter}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctrl := &stopWaitBotController{started: make(chan struct{}), release: make(chan struct{})}
	msg := InboundMessage{Platform: PlatformWeixin, ConnectionID: "weixin", ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "hello"}
	key := BuildSessionKey(msg.Session())
	gw.controllers[key] = &sessionState{ctrl: ctrl, sink: &sessionEventSink{}}
	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	adapter.msgCh <- msg
	select {
	case <-ctrl.started:
	case <-time.After(time.Second):
		t.Fatal("turn did not start")
	}

	done := make(chan struct{})
	go func() {
		gw.Stop()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Stop returned while a turn was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(ctrl.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the turn exited")
	}
}

type typingHoldAdapter struct {
	*fakeAdapter
	entered chan struct{} // one send per turn parked in SendTyping
	release chan struct{}
}

func (a *typingHoldAdapter) SendTyping(context.Context, string) error {
	a.entered <- struct{}{}
	<-a.release
	return nil
}

type cancelPublishBotController struct {
	botController
	closeEntered chan struct{} // one send when Close begins
	closeHold    chan struct{} // Close parks here, pinning Stop inside the closeSessions loop
	turnCtx      chan error    // ctx.Err() observed on RunTurn entry
}

func (c *cancelPublishBotController) RunTurn(ctx context.Context, _ string) error {
	c.turnCtx <- ctx.Err()
	return nil
}

func (c *cancelPublishBotController) SessionPath() string   { return "" }
func (c *cancelPublishBotController) WorkspaceRoot() string { return "" }
func (c *cancelPublishBotController) Close() {
	c.closeEntered <- struct{}{}
	<-c.closeHold
}

// Guards the cancel-publication window: runTurn publishes state.cancel under
// gw.mu only after the session is already visible in gw.controllers, so Stop
// can consume the field from another goroutine while the turn is still on its
// way to publication. TestBotGatewayStopWaitsForTurn stops only after RunTurn
// has begun (cancel already published) and never covers this window.
//
// Two sessions are needed because whichever state closeSessions visits first
// has its cancel read before Close signals the test — any write released off
// that signal is ordered after the read and invisible to the race detector.
// The first state's blocking Close pins Stop mid-loop instead, so the second
// state's cancel read happens after the turns were let through to publish.
// Run with -race: an unlocked read of state.cancel here is a data race.
func TestBotGatewayStopBeforeTurnCancelPublication(t *testing.T) {
	adapter := &typingHoldAdapter{
		fakeAdapter: newFakeAdapter(PlatformWeixin, "fake-weixin"),
		entered:     make(chan struct{}, 2),
		release:     make(chan struct{}),
	}
	gw := NewGatewayWithAdapterBindings(GatewayConfig{
		Enabled:   map[Platform]bool{PlatformWeixin: true},
		Allowlist: AllowlistConfig{AllowAll: true},
	}, []AdapterBinding{{ID: "weixin", Platform: PlatformWeixin, Adapter: adapter}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	closeEntered := make(chan struct{}, 2)
	closeHold := make(chan struct{})
	turnCtx := make(chan error, 2)
	chats := []string{"chat-a", "chat-b"}
	for _, chat := range chats {
		msg := InboundMessage{Platform: PlatformWeixin, ConnectionID: "weixin", ChatType: ChatDM, ChatID: chat, UserID: "user"}
		gw.controllers[BuildSessionKey(msg.Session())] = &sessionState{
			ctrl: &cancelPublishBotController{closeEntered: closeEntered, closeHold: closeHold, turnCtx: turnCtx},
			sink: &sessionEventSink{},
		}
	}
	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for _, chat := range chats {
		adapter.msgCh <- InboundMessage{Platform: PlatformWeixin, ConnectionID: "weixin", ChatType: ChatDM, ChatID: chat, UserID: "user", Text: "hello"}
	}
	for range chats {
		select {
		case <-adapter.entered:
		case <-time.After(time.Second):
			t.Fatal("turn did not reach the pre-publication window")
		}
	}

	done := make(chan struct{})
	go func() {
		gw.Stop()
		close(done)
	}()
	// Stop is parked inside the closeSessions loop: the first state's cancel is
	// consumed and its Close is held; the second state's cancel is still unread.
	select {
	case <-closeEntered:
	case <-time.After(time.Second):
		t.Fatal("Stop did not reach the session close loop")
	}
	// Let both turns publish their cancel funcs while Stop stays parked. The
	// sleep is deliberate and cannot become a channel handshake: observing the
	// publication would order the write before Stop's read and hide the race
	// from the detector.
	close(adapter.release)
	time.Sleep(200 * time.Millisecond)
	close(closeHold)

	for range chats {
		select {
		case err := <-turnCtx:
			if err == nil {
				t.Fatal("turn ran with a live context after Stop closed its session")
			}
		case <-time.After(time.Second):
			t.Fatal("turn did not run to completion")
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the late turns exited")
	}
}

func TestBotGatewayStopIsIdempotent(t *testing.T) {
	adapter := &countingStopAdapter{fakeAdapter: newFakeAdapter(PlatformFeishu, "fake-feishu")}
	gw := NewGatewayWithAdapterBindings(GatewayConfig{
		Enabled: map[Platform]bool{PlatformFeishu: true},
	}, []AdapterBinding{{ID: "feishu-lark", Platform: PlatformFeishu, Adapter: adapter}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	gw.Stop()
	gw.Stop()
	if got := adapter.calls(); got != 1 {
		t.Fatalf("adapter Stop calls = %d, want 1", got)
	}
}

func TestBotGatewayStopCancelsConcurrentStart(t *testing.T) {
	adapter := &cancelBlockingStartAdapter{
		fakeAdapter: newFakeAdapter(PlatformFeishu, "fake-feishu"),
		entered:     make(chan struct{}),
	}
	gw := NewGatewayWithAdapterBindings(GatewayConfig{
		Enabled: map[Platform]bool{PlatformFeishu: true},
	}, []AdapterBinding{{ID: "feishu-lark", Platform: PlatformFeishu, Adapter: adapter}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	startDone := make(chan error, 1)
	go func() { startDone <- gw.Start(context.Background()) }()
	select {
	case <-adapter.entered:
	case <-time.After(time.Second):
		t.Fatal("adapter Start did not begin")
	}

	stopDone := make(chan struct{})
	go func() {
		gw.Stop()
		close(stopDone)
	}()
	select {
	case err := <-startDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the in-progress Start")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after Start returned")
	}
}

// Guards the gw.cfg.Channels / gw.cfg.ConnectionChannels / gw.cfg.ToolApprovalMode
// snapshot locking: approval-mode writers mutate those under gw.mu while
// sessionOptionsForMessage and the project/session index builders read them.
// Run with -race; a lock-free read is a concurrent map read/write crash.
func TestBotGatewayToolApprovalModeConcurrentWithConfigReaders(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	gw := &BotGateway{
		cfg: GatewayConfig{
			WorkspaceRoot: t.TempDir(),
			Channels: map[Platform]ChannelConfig{
				PlatformFeishu: {ToolApprovalMode: control.ToolApprovalAsk},
			},
			ConnectionChannels: map[string]ChannelConfig{
				"feishu-lark": {ToolApprovalMode: control.ToolApprovalAsk},
			},
		},
		controllers:      map[string]*sessionState{},
		sessionOverrides: map[string]sessionRuntimeOverride{},
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Keep both sides finite. The former stop-driven writer kept allocating until
	// the reader returned and repeatedly tripped Go's Windows GC in normal CI.
	// The shared start edge orders neither loop after the other, so -race still
	// observes any missing map lock even if one goroutine happens to run first.
	const iterations = 200
	start := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		<-start
		modes := []string{control.ToolApprovalYolo, control.ToolApprovalAsk, control.ToolApprovalAuto}
		for i := 0; i < iterations; i++ {
			mode := modes[i%len(modes)]
			gw.UpdateConnectionToolApprovalMode("feishu-lark", mode)
			gw.mu.Lock()
			gw.updateToolApprovalModeDefaultLocked(InboundMessage{Platform: PlatformFeishu}, mode)
			gw.updateToolApprovalModeDefaultLocked(InboundMessage{}, mode)
			gw.mu.Unlock()
			runtime.Gosched()
		}
	}()

	close(start)
	connMsg := InboundMessage{Platform: PlatformFeishu, ConnectionID: "feishu-lark", ChatType: ChatDM, ChatID: "chat", UserID: "user"}
	for i := 0; i < iterations; i++ {
		gw.sessionOptionsForMessage(connMsg)
		gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu})
		projects := gw.buildProjectIndex()
		gw.buildSessionIndex(projects)
		runtime.Gosched()
	}
	<-writerDone
}

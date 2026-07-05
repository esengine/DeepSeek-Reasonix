package bot

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
)

type closeProbeBotController struct {
	*control.Controller
	onClose func()
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

	stop := make(chan struct{})
	var wg sync.WaitGroup
	defer func() {
		close(stop)
		wg.Wait()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		modes := []string{control.ToolApprovalYolo, control.ToolApprovalAsk, control.ToolApprovalAuto}
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			mode := modes[i%len(modes)]
			gw.UpdateConnectionToolApprovalMode("feishu-lark", mode)
			gw.mu.Lock()
			gw.updateToolApprovalModeDefaultLocked(InboundMessage{Platform: PlatformFeishu}, mode)
			gw.updateToolApprovalModeDefaultLocked(InboundMessage{}, mode)
			gw.mu.Unlock()
		}
	}()

	connMsg := InboundMessage{Platform: PlatformFeishu, ConnectionID: "feishu-lark", ChatType: ChatDM, ChatID: "chat", UserID: "user"}
	for i := 0; i < 200; i++ {
		gw.sessionOptionsForMessage(connMsg)
		gw.sessionOptionsForMessage(InboundMessage{Platform: PlatformFeishu})
		projects := gw.buildProjectIndex()
		gw.buildSessionIndex(projects)
	}
}

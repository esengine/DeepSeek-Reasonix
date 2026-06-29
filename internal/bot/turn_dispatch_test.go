package bot

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"reasonix/internal/control"
)

// approvalBlockingController is a botController whose RunTurn blocks the way a
// real turn does when it hits interactive tool approval: it parks inside
// RunTurn until Approve is called. Every other method is a harmless stub so the
// gateway's turn/approve path can drive it without a real controller.
type approvalBlockingController struct {
	botController // embedded nil interface: unused methods panic if ever called
	started       chan struct{}
	released      chan struct{}
	approved      chan struct{}
}

func newApprovalBlockingController() *approvalBlockingController {
	return &approvalBlockingController{
		started:  make(chan struct{}, 1),
		released: make(chan struct{}),
		approved: make(chan struct{}, 1),
	}
}

func (c *approvalBlockingController) RunTurn(ctx context.Context, input string) error {
	// Signal the turn is in-flight, then block as if waiting for ctrl.Approve.
	select {
	case c.started <- struct{}{}:
	default:
	}
	select {
	case <-c.released:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *approvalBlockingController) Approve(id string, allow, session, persist bool) {
	select {
	case c.approved <- struct{}{}:
	default:
	}
	// Unblock the parked RunTurn, mirroring the real approval handoff.
	select {
	case <-c.released:
	default:
		close(c.released)
	}
}

// Methods the turn/approve path touches but whose behavior is irrelevant here.
func (c *approvalBlockingController) SessionPath() string { return "" }

var _ botController = (*approvalBlockingController)(nil)
var _ control.Approvals = (*approvalBlockingController)(nil)

// TestGatewayApprovalReplyUnblocksTurnOffDispatchGoroutine guards the contract
// fixed in this PR: a turn that blocks inside RunTurn waiting for approval must
// not wedge the per-adapter dispatch loop. handleSlashCommand (the only caller
// of ctrl.Approve) runs on that same dispatch goroutine, so if runTurn ran
// inline the loop could never deliver the /approve reply that unblocks the turn
// (#4402, #4701, #4863). Running the turn on its own goroutine keeps the loop
// free to deliver it.
func TestGatewayApprovalReplyUnblocksTurnOffDispatchGoroutine(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{AllowAll: true}}, nil, logger)

	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	binding := AdapterBinding{ID: "weixin", Platform: PlatformWeixin, Adapter: adapter}

	ctrl := newApprovalBlockingController()
	msg := InboundMessage{
		Platform:     PlatformWeixin,
		ConnectionID: "weixin",
		ChatType:     ChatDM,
		ChatID:       "chat",
		UserID:       "user",
	}
	key := BuildSessionKey(msg.Session())
	// Pre-seed the session so runTurn reuses this fake controller instead of
	// building a real one via boot.Build.
	gw.controllers[key] = &sessionState{ctrl: ctrl, sink: &sessionEventSink{}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gw.dispatchLoop(ctx, binding)

	// First message: a normal turn that will block on approval inside RunTurn.
	turn := msg
	turn.Text = "do something that needs approval"
	adapter.msgCh <- turn

	select {
	case <-ctrl.started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn never started; dispatch loop did not run the turn")
	}

	// Second message: the /approve reply. If the turn ran inline on the dispatch
	// goroutine, the loop is parked in RunTurn and can never read this — the
	// session would wedge until restart. With the turn off the dispatch loop,
	// this reply is delivered and unblocks the turn.
	approve := msg
	approve.Text = "/approve some-id"
	adapter.msgCh <- approve

	select {
	case <-ctrl.approved:
	case <-time.After(2 * time.Second):
		t.Fatal("approval reply was never delivered: dispatch loop is wedged on the blocked turn")
	}

	// And the parked turn actually unblocks.
	select {
	case <-ctrl.released:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not unblock after approval")
	}
}

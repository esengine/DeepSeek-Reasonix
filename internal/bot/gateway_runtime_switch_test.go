package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
)

type failureAtomicBotController struct {
	botController
	workspaceRoot string
	sessionPath   string
	closed        bool
	turns         int
}

func (c *failureAtomicBotController) RuntimeStatus() control.RuntimeStatus {
	return control.RuntimeStatus{}
}
func (c *failureAtomicBotController) WorkspaceRoot() string { return c.workspaceRoot }
func (c *failureAtomicBotController) SessionPath() string   { return c.sessionPath }
func (c *failureAtomicBotController) Close()                { c.closed = true }
func (c *failureAtomicBotController) RunTurn(context.Context, string) error {
	c.turns++
	return nil
}

func TestModelSwitchBuildFailurePreservesControllerAndLease(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	workspace := t.TempDir()
	sessionPath := filepath.Join(workspace, "current.jsonl")
	gw := NewGateway(GatewayConfig{Model: "provider/old", WorkspaceRoot: workspace}, nil, logger)
	msg := InboundMessage{Platform: PlatformDingtalk, ChatType: ChatDM, ChatID: "chat", UserID: "user"}
	key := BuildSessionKey(msg.Session())
	ctrl := &failureAtomicBotController{workspaceRoot: workspace, sessionPath: sessionPath}
	leases := control.NewSessionLeaseKeeper()
	if err := leases.Rebind(sessionPath); err != nil {
		t.Fatalf("acquire current session lease: %v", err)
	}
	state := &sessionState{
		ctrl:             ctrl,
		leases:           leases,
		model:            "provider/old",
		workspaceRoot:    workspace,
		toolApprovalMode: control.ToolApprovalAsk,
		sessionPath:      sessionPath,
	}
	gw.controllers[key] = state
	gw.sessionOverrides[key] = sessionRuntimeOverride{
		channel:     ChannelConfig{Model: "provider/old", WorkspaceRoot: workspace},
		sessionPath: sessionPath,
	}
	gw.buildController = func(context.Context, boot.Options) (*control.Controller, error) {
		return nil, fmt.Errorf("forced build failure")
	}
	t.Cleanup(gw.closeSessions)

	got := gw.handleModelCommand(context.Background(), msg, "/model provider/new")
	if !strings.Contains(got, "当前会话保持不变") {
		t.Fatalf("model switch response = %q, want failure-atomic message", got)
	}
	if gw.controllers[key] != state || ctrl.closed {
		t.Fatalf("old controller changed after build failure: installed=%v closed=%v", gw.controllers[key] == state, ctrl.closed)
	}
	if model := gw.sessionOverrides[key].channel.Model; model != "provider/old" {
		t.Fatalf("runtime override model = %q, want provider/old", model)
	}
	if held := leases.HeldPath(); agent.CanonicalSessionPath(held) != agent.CanonicalSessionPath(sessionPath) {
		t.Fatalf("held lease path = %q, want %q", held, sessionPath)
	}
	if competing, err := agent.TryAcquireSessionLease(sessionPath); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		if competing != nil {
			competing.Release()
		}
		t.Fatalf("competing lease err = %v, want ErrSessionLeaseHeld", err)
	}
	if err := ctrl.RunTurn(context.Background(), "still usable"); err != nil || ctrl.turns != 1 {
		t.Fatalf("old controller unusable after failed switch: turns=%d err=%v", ctrl.turns, err)
	}
}

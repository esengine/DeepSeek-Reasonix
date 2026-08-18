package bot

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestGatewayIntentionalTransitionMovesLeaseAndMapping(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := sess.SaveIfAbsent(originalPath); err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(GatewayConfig{}, nil, logger)
	msg := InboundMessage{Platform: PlatformWeixin, ChatType: ChatDM, ChatID: "chat", UserID: "user"}
	key := BuildSessionKey(msg.Session())
	state := &sessionState{
		leases:      control.NewSessionLeaseKeeper(),
		sessionPath: originalPath,
	}
	state.onSessionTransition = gw.botSessionTransitionHandler(key, msg, state)
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Runner: exec, Executor: exec, SessionDir: dir, SessionPath: originalPath,
		Sink: event.Discard, OnSessionTransition: state.onSessionTransition,
	})
	state.ctrl = ctrl
	gw.controllers[key] = state
	gw.sessionOverrides[key] = sessionRuntimeOverride{sessionPath: originalPath}
	if err := rebindBotSessionWriteAuthority(state, originalPath); err != nil {
		t.Fatal(err)
	}
	defer state.leases.Release()

	if err := ctrl.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	targetPath := ctrl.SessionPath()
	gw.mu.Lock()
	statePath := state.sessionPath
	overridePath := gw.sessionOverrides[key].sessionPath
	gw.mu.Unlock()
	if statePath != targetPath || overridePath != targetPath {
		t.Fatalf("transition paths = state %q override %q, want %q", statePath, overridePath, targetPath)
	}
	if got := state.leases.HeldPath(); got != agent.CanonicalSessionPath(targetPath) {
		t.Fatalf("lease path = %q, want %q", got, agent.CanonicalSessionPath(targetPath))
	}
	if auth := exec.Session().WriteAuthority(); auth == nil || !auth.Covers(targetPath) {
		t.Fatal("bot transition published an unowned Session")
	}
}

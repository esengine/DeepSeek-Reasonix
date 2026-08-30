package bot

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// TestGatewayNewSessionPinsRotatedPathAgainstFallback: /new 旋转出新会话后，
// 下一条消息的 profile 必须解析到旋转后的新路径（而不是确定性 fallback 的旧
// 路径），否则 sessionStateMatchesRuntime 会重建旧会话、静默撤销 /new。
func TestGatewayNewSessionPinsRotatedPathAgainstFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	msg := InboundMessage{Platform: PlatformWeixin, ConnectionID: "weixin-weixin", Domain: "weixin", ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "/new"}
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

	rotated := ctrl.SessionPath()
	if rotated == oldPath {
		t.Fatalf("controller session path was not rotated")
	}
	profile := gw.sessionProfileForMessage(msg)
	if canonicalBotPath(profile.sessionPath) != canonicalBotPath(rotated) {
		t.Fatalf("next-message profile session path = %q, want rotated %q", profile.sessionPath, rotated)
	}
	if !sessionStateMatchesRuntime(gw.controllers[key], profile) {
		t.Fatalf("rotated controller should match the next-message profile")
	}
	gw.closeSessions()
}

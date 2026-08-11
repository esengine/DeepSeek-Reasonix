package cli

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func TestWebSlashRequestsFrontendHandoff(t *testing.T) {
	m := newTestChatTUI()
	m.modelRef = "deepseek/deepseek-v4-flash"
	ctrl := control.New(control.Options{SessionDir: t.TempDir()})
	ctrl.EnsureSessionPath()
	t.Cleanup(ctrl.Close)
	m.ctrl = ctrl
	cmd := m.runSlashCommand("/web")
	if !m.launchWebOnExit {
		t.Fatal("/web did not mark the TUI for a Web frontend handoff")
	}
	if m.launchWebResumePath != "" {
		t.Fatalf("fresh /web handoff resume path = %q, want a new Web session", m.launchWebResumePath)
	}
	wantID := agent.BranchID(ctrl.SessionPath())
	if m.launchWebSessionID != wantID {
		t.Fatalf("fresh /web handoff session id = %q, want %q", m.launchWebSessionID, wantID)
	}
	if m.launchWebModelRef != m.modelRef {
		t.Fatalf("fresh /web handoff model = %q, want %q", m.launchWebModelRef, m.modelRef)
	}
	args := strings.Join(webHandoffArgs(m.launchWebResumePath, m.launchWebSessionID, m.launchWebModelRef, "balanced"), " ")
	if strings.Contains(args, "--resume") || !strings.Contains(args, "--session-id "+wantID) {
		t.Fatalf("fresh /web handoff args = %q, must not resume the reserved path", args)
	}
	if cmd == nil {
		t.Fatal("/web did not request TUI shutdown")
	}
	msg := cmd()
	if _, ok := msg.(tuiShutdownMsg); !ok {
		t.Fatalf("/web command returned %T, want tuiShutdownMsg", msg)
	}
}

func TestWebSlashResumesMaterializedSession(t *testing.T) {
	m := newTestChatTUI()
	ctrl := control.New(control.Options{SessionDir: t.TempDir()})
	ctrl.EnsureSessionPath()
	t.Cleanup(ctrl.Close)
	path := ctrl.SessionPath()
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	m.ctrl = ctrl

	if cmd := m.runSlashCommand("/web"); cmd == nil {
		t.Fatal("/web did not request TUI shutdown")
	}
	if m.launchWebResumePath != path {
		t.Fatalf("materialized /web handoff resume path = %q, want %q", m.launchWebResumePath, path)
	}
	if m.launchWebSessionID != agent.BranchID(path) {
		t.Fatalf("materialized /web handoff session id = %q, want %q", m.launchWebSessionID, agent.BranchID(path))
	}
	args := strings.Join(webHandoffArgs(m.launchWebResumePath, m.launchWebSessionID, m.launchWebModelRef, "balanced"), " ")
	if !strings.Contains(args, "--resume "+path) || strings.Contains(args, "--session-id") {
		t.Fatalf("materialized /web handoff args = %q, want resume path %q", args, path)
	}
}

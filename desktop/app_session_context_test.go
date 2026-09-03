package main

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontext"
	"reasonix/internal/tool"
)

func installStubControllerWithCurrentPrompt(t *testing.T, app *App, tab *WorkspaceTab) *control.Controller {
	t.Helper()
	if tab == nil || tab.Ctrl == nil {
		t.Fatal("tab controller is required")
	}
	sys := systemPromptFrom(tab.Ctrl.History())
	if strings.TrimSpace(sys) == "" {
		t.Fatal("tab controller did not expose a system prompt")
	}
	sessionDir := tab.Ctrl.SessionDir()
	sessionPath := tab.Ctrl.SessionPath()
	workspaceRoot := tab.Ctrl.WorkspaceRoot()
	label := tab.Ctrl.Label()
	tab.Ctrl.Close()

	sess := agent.NewSession(sys)
	ag := agent.New(stubProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Runner:               ag,
		Executor:             ag,
		SessionDir:           sessionDir,
		SessionPath:          sessionPath,
		WorkspaceRoot:        workspaceRoot,
		Label:                label,
		SystemPrompt:         sys,
		SessionContextStatic: sessioncontext.Sections{Workspace: "Current workspace: " + strconv.Quote(workspaceRoot)},
		Sink:                 event.Discard,
	})
	tab.Ctrl = ctrl
	app.bindControllerDisplayRecorder(ctrl)
	return ctrl
}

func assertWorkspaceSessionContext(t *testing.T, messages []provider.Message, want, unwanted string) {
	t.Helper()
	for i := range slices.Backward(messages) {
		snapshot, ok := sessioncontext.Parse(messages[i].Content)
		if !ok || messages[i].Origin != provider.MessageOriginHost {
			continue
		}
		if !strings.Contains(snapshot.Sections.Workspace, "Current workspace: "+strconv.Quote(want)) {
			t.Fatalf("session-context missing workspace %q:\n%s", want, snapshot.Content)
		}
		if strings.Contains(snapshot.Sections.Workspace, "Current workspace: "+strconv.Quote(unwanted)) {
			t.Fatalf("session-context retained workspace %q:\n%s", unwanted, snapshot.Content)
		}
		return
	}
	t.Fatalf("history contains no valid host session-context: %+v", messages)
}

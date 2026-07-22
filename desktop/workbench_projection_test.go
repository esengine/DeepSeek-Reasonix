package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
)

func strptr(value string) *string { return &value }

func TestWorkbenchHistoryProjectionPreservesReasoningToolsAndCitations(t *testing.T) {
	page := workbenchHistoryPage(protocol.HistoryPage{
		Messages: []protocol.HistoryMessage{{
			Role: "assistant", Content: strptr("done"), Reasoning: strptr("thought"),
			MemoryCitations: []eventwire.MemoryCitation{{ID: "m1", Source: "MEMORY.md", LineStart: 2, LineEnd: 3}},
			ToolCalls:       []protocol.HistoryToolCall{{ID: "tc1", Name: "read_file", Arguments: strptr(`{"path":"a"}`), Summary: strptr("read a")}},
		}},
		StartTurn: 1, EndTurn: 1, TotalTurns: 1,
	})
	if len(page.Messages) != 1 || page.Messages[0].Content != "done" || page.Messages[0].Reasoning != "thought" {
		t.Fatalf("history projection lost message fields: %+v", page)
	}
	if got := page.Messages[0].ToolCalls; len(got) != 1 || got[0].Arguments != `{"path":"a"}` {
		t.Fatalf("history projection lost tool call: %+v", got)
	}
	if got := page.Messages[0].MemoryCitations; len(got) != 1 || got[0].ID != "m1" || got[0].LineEnd != 3 {
		t.Fatalf("history projection lost citation: %+v", got)
	}
}

func TestWorkbenchSnapshotProjectionUsesRemoteWorkspaceAndProfile(t *testing.T) {
	snapshot := protocol.SessionSnapshot{Meta: protocol.SessionMetaSnapshot{
		Title: "Remote", ResolvedProfile: protocol.ResolvedProfile{
			Model: "deepseek/deepseek-chat", Effort: "high", CollaborationMode: protocol.CollaborationNormal,
			TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAuto,
		},
	}}
	meta := workbenchMeta(snapshot, "/srv/app")
	if meta.WorkspaceRoot != "/srv/app" || meta.Label != "deepseek/deepseek-chat" || !meta.AutoApproveTools || meta.Bypass {
		t.Fatalf("meta projection = %+v", meta)
	}
}

func TestWorkbenchSnapshotProjectionPreservesCanonicalTodos(t *testing.T) {
	content := "Ship the fix"
	meta := workbenchMeta(protocol.SessionSnapshot{Todos: []protocol.TodoItem{{
		Content: &content, Status: protocol.TodoCompleted, ActiveForm: "Shipping the fix", Level: 1,
	}}}, "/srv/app")
	if meta.CanonicalTodos == nil {
		t.Fatal("canonical todos are unavailable, want projected remote snapshot")
	}
	if got := *meta.CanonicalTodos; len(got) != 1 || got[0].Content != content || got[0].Status != "completed" || got[0].ActiveForm != "Shipping the fix" || got[0].Level != 1 {
		t.Fatalf("canonical todos = %+v", got)
	}

	empty := workbenchMeta(protocol.SessionSnapshot{Todos: []protocol.TodoItem{}}, "/srv/app")
	if empty.CanonicalTodos == nil || len(*empty.CanonicalTodos) != 0 {
		t.Fatalf("empty canonical todos = %#v, want authoritative empty list", empty.CanonicalTodos)
	}

	unavailable := workbenchMeta(protocol.SessionSnapshot{}, "/srv/app")
	if unavailable.CanonicalTodos != nil {
		t.Fatalf("unavailable canonical todos = %#v, want omitted field", unavailable.CanonicalTodos)
	}
}

func TestWorkbenchContextProjectionPreservesAllSourceTotals(t *testing.T) {
	context := workbenchContextInfo(protocol.ContextView{
		UsedTokens: 10, WindowTokens: 100, TotalTokens: 30, SessionCacheHitTokens: 7, SessionCacheMissTokens: 2,
		Sources: []protocol.UsageSourceView{{Source: "executor", TotalTokens: 30, RequestCount: 2}},
	})
	if context.Used != 10 || context.Window != 100 || context.SessionTokens != 30 || context.CacheHitTokens != 7 {
		t.Fatalf("context projection = %+v", context)
	}
	if context.Sources["executor"].RequestCount != 2 {
		t.Fatalf("sources = %+v", context.Sources)
	}
}

func TestWorkbenchLateCallbackUsesCurrentProjectionTab(t *testing.T) {
	app := testAppWithOrderedTabs(t, "b", "a", "b")
	app.ctx = context.Background()
	events := make(chan wireEventTab, 1)
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name == "agent:event" && len(payload) == 1 {
			if event, ok := payload[0].(wireEventTab); ok {
				events <- event
			}
		}
	}
	k := app.workbench()
	_, generation, err := k.targets.BeginRemoteConnect("remote-host", "/srv/work")
	if err != nil {
		t.Fatal(err)
	}
	if err := k.targets.MarkRemoteConnected(generation); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := k.targets.ActivateRemote(generation); err != nil {
		t.Fatal(err)
	}
	k.mu.Lock()
	k.remoteGen = generation
	k.remoteTabID = "b"
	k.mu.Unlock()

	// This callback was captured while tab A was projected, but completes after
	// Remote was rebound to tab B. It must route through the current binding.
	callbacks := app.workbenchClientCallbacks(generation, "a")
	callbacks.OnSessionEvent(protocol.SessionEvent{
		Seq: 1, Event: eventwire.Event{Kind: "text", Text: "late"},
	})
	select {
	case got := <-events:
		if got.TabID != "b" {
			t.Fatalf("late callback tab = %q, want current projection b", got.TabID)
		}
	case <-time.After(time.Second):
		t.Fatal("late callback was not projected")
	}
}

func TestRemoteSavedSessionBindingsNeverFallBackToLocal(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := &App{}
	dir := desktopSessionDir("")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "20260721-remote-fallback.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "local-only message"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := app.ListSessions(); len(got) != 1 {
		t.Fatalf("local saved sessions = %d, want 1", len(got))
	}

	_, generation, err := app.workbench().targets.BeginRemoteConnect("remote-host", "/srv/work")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.workbench().targets.MarkRemoteConnected(generation); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := app.workbench().targets.ActivateRemote(generation); err != nil {
		t.Fatal(err)
	}
	if got := app.ListSessions(); len(got) != 0 {
		t.Fatalf("Remote history exposed Local sessions: %+v", got)
	}
	if got := app.ListTrashedSessions(); len(got) != 0 {
		t.Fatalf("Remote trash exposed Local sessions: %+v", got)
	}

	checks := []struct {
		name string
		run  func() error
	}{
		{"delete", func() error { return app.DeleteSession(path) }},
		{"delete recovery", func() error { return app.DeleteRecoveryCopy(path) }},
		{"restore", func() error { return app.RestoreSession(path) }},
		{"purge", func() error { return app.PurgeTrashedSession(path) }},
		{"purge recovery", func() error { return app.PurgeRecoveryCopy(path) }},
		{"rename", func() error { return app.RenameSession(path, "renamed") }},
		{"resume page", func() error { _, err := app.ResumeSessionPageForTab("", path, 10); return err }},
		{"resume", func() error { _, err := app.ResumeSessionForTab("", path); return err }},
		{"preview", func() error { _, err := app.PreviewSession(path); return err }},
		{"open channel", func() error { _, err := app.OpenChannelSessionForTab("", path); return err }},
		{"open channel page", func() error { _, err := app.OpenChannelSessionPageForTab("", path, 10); return err }},
		{"prompt history", func() error { _, err := app.ScanPromptHistory(""); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.run()
			if err == nil || !strings.Contains(err.Error(), "CAPABILITY_UNAVAILABLE") {
				t.Fatalf("error = %v, want Remote capability rejection", err)
			}
		})
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Remote binding changed Local session: %v", err)
	}
}

func TestRemoteUnsupportedCapabilitiesNeverFallBackToLocal(t *testing.T) {
	app := &App{}
	_, generation, err := app.workbench().targets.BeginRemoteConnect("remote-host", "/srv/work")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.workbench().targets.MarkRemoteConnected(generation); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := app.workbench().targets.ActivateRemote(generation); err != nil {
		t.Fatal(err)
	}

	for _, view := range []MemoryView{app.Memory(), app.MemoryForTab("local-tab")} {
		if view.Available || view.Docs == nil || view.Facts == nil || view.Archives == nil || view.Scopes == nil {
			t.Fatalf("Remote memory view = %+v, want unavailable non-nil collections", view)
		}
	}
	suggestions := app.MemorySuggestionsForTab("local-tab")
	if suggestions.Available || suggestions.Memories == nil || suggestions.Skills == nil {
		t.Fatalf("Remote memory suggestions = %+v, want unavailable non-nil collections", suggestions)
	}
	if status := app.AutoResearchStatus("local-tab"); status.OpenCriteria == nil {
		t.Fatalf("Remote auto-research status = %+v, want non-nil criteria", status)
	}
	if app.AutoResearchList("local-tab") == nil || app.AutoResearchFindings("local-tab", 10) == nil {
		t.Fatal("Remote auto-research reads returned nil collections")
	}

	checks := []struct {
		name string
		run  func() error
	}{
		{"remember", func() error { _, err := app.Remember("project", "local-only"); return err }},
		{"remember for tab", func() error { _, err := app.RememberForTab("local-tab", "project", "local-only"); return err }},
		{"forget", func() error { return app.Forget("local-only") }},
		{"forget for tab", func() error { return app.ForgetForTab("local-tab", "local-only") }},
		{"save doc", func() error { _, err := app.SaveDoc("REASONIX.md", "local-only"); return err }},
		{"save doc for tab", func() error { _, err := app.SaveDocForTab("local-tab", "REASONIX.md", "local-only"); return err }},
		{"accept memory suggestion", func() error { _, err := app.AcceptMemorySuggestion(MemorySuggestion{}); return err }},
		{"accept memory suggestion for tab", func() error { _, err := app.AcceptMemorySuggestionForTab("local-tab", MemorySuggestion{}); return err }},
		{"accept skill suggestion", func() error { _, err := app.AcceptSkillSuggestion(SkillSuggestion{}); return err }},
		{"accept skill suggestion for tab", func() error { _, err := app.AcceptSkillSuggestionForTab("local-tab", SkillSuggestion{}); return err }},
		{"open auto-research task", func() error { return app.AutoResearchOpenTask("local-tab") }},
		{"record auto-research evidence", func() error {
			return app.AutoResearchRecordEvidence("local-tab", "criterion", AutoResearchEvidenceView{})
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.run()
			if err == nil || !strings.Contains(err.Error(), "CAPABILITY_UNAVAILABLE") {
				t.Fatalf("error = %v, want Remote capability rejection", err)
			}
		})
	}
}

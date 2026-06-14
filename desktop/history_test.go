package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestHistoryMessagesIncludeAssistantReasoning(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "expanded prompt"},
		{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "thinking trace", ToolCalls: []provider.ToolCall{{
			ID: "call_1", Name: "bash", Arguments: `{"command":"pwd"}`,
		}}},
		{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_1", Content: "tool output", ReasoningContent: "ignored by frontend filter"},
		{Role: provider.RoleAssistant, ReasoningContent: "tool-call-only thinking"},
	}

	got := historyMessages(msgs, func(content string) string {
		if content != "expanded prompt" {
			t.Fatalf("unexpected user content passed to resolver: %q", content)
		}
		return "display prompt"
	})

	if len(got) != len(msgs) {
		t.Fatalf("history length = %d, want %d", len(got), len(msgs))
	}
	if got[0].Content != "display prompt" {
		t.Fatalf("user display content = %q, want display prompt", got[0].Content)
	}
	if got[1].Reasoning != "thinking trace" {
		t.Fatalf("assistant reasoning = %q, want thinking trace", got[1].Reasoning)
	}
	if len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "call_1" || got[1].ToolCalls[0].Name != "bash" || got[1].ToolCalls[0].Arguments != `{"command":"pwd"}` {
		t.Fatalf("assistant tool calls not preserved: %+v", got[1].ToolCalls)
	}
	if got[2].ToolCallID != "call_1" || got[2].ToolName != "bash" || got[2].Content != "tool output" {
		t.Fatalf("tool result details not preserved: %+v", got[2])
	}
	if got[2].Reasoning != "" {
		t.Fatalf("non-assistant reasoning should stay hidden, got %q", got[2].Reasoning)
	}
	if got[3].Reasoning != "tool-call-only thinking" {
		t.Fatalf("empty-content assistant reasoning = %q, want tool-call-only thinking", got[3].Reasoning)
	}
}

func TestPreviewSessionMessagesLoadsWithoutResuming(t *testing.T) {
	dir := t.TempDir()
	session := agent.NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "show history"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "saved reasoning"})
	path := filepath.Join(dir, "session.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := previewSessionMessages(dir, path)
	if err != nil {
		t.Fatalf("previewSessionMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("preview history length = %d, want 2", len(got))
	}
	if got[1].Reasoning != "saved reasoning" {
		t.Fatalf("preview reasoning = %q, want saved reasoning", got[1].Reasoning)
	}
}

func TestPreviewSessionMessagesIncludesProcessEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	body := strings.Join([]string{
		`{"kind":"phase","text":"Preparing context"}`,
		`{"kind":"notice","level":"warn","text":"Network changed"}`,
		`{"kind":"compaction_started","compaction":{"trigger":"manual"}}`,
		`{"kind":"compaction_done","compaction":{"trigger":"manual","messages":6,"summary":"Kept the current task.","archive":"/tmp/archive.jsonl"}}`,
		`{"type":"user.message","text":"hello"}`,
		`{"type":"model.final","content":"hi","reasoningContent":"thinking"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := previewSessionMessages(dir, path)
	if err != nil {
		t.Fatalf("previewSessionMessages: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("preview history length = %d, want 6: %+v", len(got), got)
	}
	if got[0].Role != "phase" || got[0].Content != "Preparing context" {
		t.Fatalf("phase event not preserved: %+v", got[0])
	}
	if got[1].Role != "notice" || got[1].Level != "warn" || got[1].Content != "Network changed" {
		t.Fatalf("notice event not preserved: %+v", got[1])
	}
	if got[2].Role != "compaction" || !got[2].Pending || got[2].Trigger != "manual" {
		t.Fatalf("pending compaction event not preserved: %+v", got[2])
	}
	if got[3].Role != "compaction" || got[3].Pending || got[3].Messages != 6 || got[3].Summary != "Kept the current task." || got[3].Archive != "/tmp/archive.jsonl" {
		t.Fatalf("finished compaction event not preserved: %+v", got[3])
	}
	if got[4].Role != "user" || got[5].Reasoning != "thinking" {
		t.Fatalf("conversation events not preserved: %+v", got[4:])
	}
}

func TestResumeSessionForTabTargetsSpecifiedTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	activePath := filepath.Join(dir, "active.jsonl")
	inactivePath := filepath.Join(dir, "inactive.jsonl")
	targetPath := filepath.Join(dir, "target.jsonl")
	writeHistoryTestSession(t, activePath, "active prompt")
	writeHistoryTestSession(t, inactivePath, "inactive prompt")
	writeHistoryTestSession(t, targetPath, "target prompt")

	activeExec := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
	inactiveExec := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
	activeCtrl := control.New(control.Options{Executor: activeExec, SessionDir: dir, SessionPath: activePath, Label: "active"})
	inactiveCtrl := control.New(control.Options{Executor: inactiveExec, SessionDir: dir, SessionPath: inactivePath, Label: "inactive"})
	defer activeCtrl.Close()
	defer inactiveCtrl.Close()

	app := &App{
		tabs: map[string]*WorkspaceTab{
			"active":   {ID: "active", Scope: "global", Ctrl: activeCtrl, Ready: true},
			"inactive": {ID: "inactive", Scope: "global", Ctrl: inactiveCtrl, Ready: true},
		},
		tabOrder:    []string{"active", "inactive"},
		activeTabID: "active",
	}

	got, err := app.ResumeSessionForTab("inactive", targetPath)
	if err != nil {
		t.Fatalf("ResumeSessionForTab: %v", err)
	}
	if activeCtrl.SessionPath() != activePath {
		t.Fatalf("active tab session path = %q, want %q", activeCtrl.SessionPath(), activePath)
	}
	if inactiveCtrl.SessionPath() != targetPath {
		t.Fatalf("inactive tab session path = %q, want %q", inactiveCtrl.SessionPath(), targetPath)
	}
	f := loadTabsFile()
	var savedInactive string
	for _, entry := range f.Tabs {
		if entry.ID == "inactive" {
			savedInactive = entry.SessionPath
			break
		}
	}
	if filepath.Clean(savedInactive) != filepath.Clean(targetPath) {
		t.Fatalf("saved inactive session path = %q, want %q", savedInactive, targetPath)
	}
	if len(got) != 1 || got[0].Content != "target prompt" {
		t.Fatalf("resumed history = %+v, want target prompt", got)
	}
}

func writeHistoryTestSession(t *testing.T, path, prompt string) {
	t.Helper()
	session := agent.NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: prompt})
	if err := session.Save(path); err != nil {
		t.Fatalf("Save %s: %v", path, err)
	}
}

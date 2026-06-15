package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/agent"
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
	if len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "call_1" || got[1].ToolCalls[0].Name != "bash" {
		t.Fatalf("assistant tool calls not preserved: %+v", got[1].ToolCalls)
	}
	if !got[1].ToolCalls[0].ArgumentsArchived || got[1].ToolCalls[0].Arguments != "" || got[1].ToolCalls[0].Subject != "pwd" {
		t.Fatalf("assistant tool call was not restored as lightweight metadata: %+v", got[1].ToolCalls[0])
	}
	if got[2].ToolCallID != "call_1" || got[2].ToolName != "bash" || got[2].Content != "" || !got[2].ToolResultArchived {
		t.Fatalf("tool result details not preserved: %+v", got[2])
	}
	if got[2].Reasoning != "" {
		t.Fatalf("non-assistant reasoning should stay hidden, got %q", got[2].Reasoning)
	}
	if got[3].Reasoning != "tool-call-only thinking" {
		t.Fatalf("empty-content assistant reasoning = %q, want tool-call-only thinking", got[3].Reasoning)
	}
}

func TestHistoryMessagesArchiveCompletedToolPayloads(t *testing.T) {
	largeArgs := `{"command":"` + strings.Repeat("printf x;", 300) + `"}`
	largeOutput := strings.Repeat("line of output\n", 600)
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "call_large", Name: "bash", Arguments: largeArgs,
		}}},
		{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_large", Content: largeOutput},
	}

	got := historyMessages(msgs, func(content string) string { return content })
	if len(got) != 2 {
		t.Fatalf("history length = %d, want 2", len(got))
	}
	call := got[0].ToolCalls[0]
	if !call.ArgumentsArchived {
		t.Fatalf("tool arguments were not marked archived: %+v", call)
	}
	if call.Arguments != "" {
		t.Fatalf("archived tool arguments should be omitted from initial history, got %d bytes", len(call.Arguments))
	}
	if call.Subject == "" {
		t.Fatalf("archived tool call should keep a collapsed subject: %+v", call)
	}
	if call.Summary == "" {
		t.Fatalf("archived tool call should keep a collapsed summary: %+v", call)
	}
	result := got[1]
	if !result.ToolResultArchived {
		t.Fatalf("tool result was not marked archived: %+v", result)
	}
	if result.Content != "" {
		t.Fatalf("archived successful tool output should be omitted from initial history, got %d bytes", len(result.Content))
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), largeArgs) || strings.Contains(string(encoded), largeOutput) {
		t.Fatalf("initial history JSON still contains large args/output: %d bytes", len(encoded))
	}
}

func TestHistoryMessagesKeepToolFileDiffMetadata(t *testing.T) {
	diff := "@@ -27 +27 @@\n-func save():\n+func save_file():\n"
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID:        "edit",
			Name:      "edit_file",
			Arguments: `{"path":"settings/settings_IO.gd","old_string":"func save():","new_string":"func save_file():"}`,
			Diff:      diff,
			Added:     1,
			Removed:   1,
		}}},
		{Role: provider.RoleTool, Name: "edit_file", ToolCallID: "edit", Content: "edited settings/settings_IO.gd"},
	}

	got := historyMessages(msgs, func(content string) string { return content })
	call := got[0].ToolCalls[0]
	if call.Diff != diff || call.Added != 1 || call.Removed != 1 {
		t.Fatalf("history tool diff metadata = diff:%q +%d -%d", call.Diff, call.Added, call.Removed)
	}
	if !call.ArgumentsArchived || call.Arguments != "" {
		t.Fatalf("tool arguments should still be archived: %+v", call)
	}
}

func TestHistoryMessagesKeepBoundedToolErrors(t *testing.T) {
	largeError := "error: " + strings.Repeat("permission denied ", 400)
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "call_error", Name: "bash", Arguments: `{"command":"rm protected"}`,
		}}},
		{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_error", Content: largeError},
	}

	got := historyMessages(msgs, func(content string) string { return content })
	result := got[1]
	if result.ToolResultError == "" {
		t.Fatalf("failed tool result should keep an error preview: %+v", result)
	}
	if result.Content != result.ToolResultError {
		t.Fatalf("tool result content and error preview diverged: content=%q error=%q", result.Content, result.ToolResultError)
	}
	if len(result.Content) >= len(largeError) {
		t.Fatalf("failed tool result preview was not bounded: got %d want < %d", len(result.Content), len(largeError))
	}
	if !strings.HasPrefix(result.Content, "error: permission denied") {
		t.Fatalf("failed tool result preview lost useful prefix: %q", result.Content[:min(len(result.Content), 80)])
	}
	if !result.ToolResultArchived {
		t.Fatalf("bounded failed tool result should still be marked archived for on-demand full data: %+v", result)
	}
}

func TestHistoryMessagesClipToolErrorsAtUTF8Boundary(t *testing.T) {
	largeError := "error: " + strings.Repeat("权限不足", 1000)
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "call_unicode_error", Name: "bash", Arguments: `{"command":"rm 受保护文件"}`,
		}}},
		{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_unicode_error", Content: largeError},
	}

	got := historyMessages(msgs, func(content string) string { return content })
	result := got[1]
	if result.ToolResultError == "" {
		t.Fatalf("failed tool result should keep an error preview: %+v", result)
	}
	if !utf8.ValidString(result.ToolResultError) {
		t.Fatalf("failed tool result preview is not valid UTF-8: %q", result.ToolResultError)
	}
	if len(result.ToolResultError) >= len(largeError) {
		t.Fatalf("failed tool result preview was not bounded: got %d want < %d", len(result.ToolResultError), len(largeError))
	}
}

func TestHistoryMessagesKeepTodoWriteArguments(t *testing.T) {
	args := `{"todos":[{"content":"A","status":"in_progress"}]}`
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "todo_1", Name: "todo_write", Arguments: args,
		}}},
		{Role: provider.RoleTool, Name: "todo_write", ToolCallID: "todo_1", Content: "Todos updated"},
	}

	got := historyMessages(msgs, func(content string) string { return content })
	call := got[0].ToolCalls[0]
	if call.ArgumentsArchived {
		t.Fatalf("todo_write arguments must remain available for restored todo panel: %+v", call)
	}
	if call.Arguments != args {
		t.Fatalf("todo_write arguments = %q, want %q", call.Arguments, args)
	}
}

func TestHistoryMessagesPreserveUnaddressableToolPayloads(t *testing.T) {
	args := `{"command":"legacy"}`
	output := "legacy output\n" + strings.Repeat("detail\n", 8)
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			Name: "bash", Arguments: args,
		}}},
		{Role: provider.RoleTool, Name: "bash", Content: output},
	}

	got := historyMessages(msgs, func(content string) string { return content })
	call := got[0].ToolCalls[0]
	if call.ArgumentsArchived {
		t.Fatalf("tool call without an id cannot be archived for later lookup: %+v", call)
	}
	if call.Arguments != args {
		t.Fatalf("tool call without an id should keep args, got %q", call.Arguments)
	}
	result := got[1]
	if result.ToolResultArchived {
		t.Fatalf("tool result without an id cannot be archived for later lookup: %+v", result)
	}
	if result.Content != output {
		t.Fatalf("tool result without an id should keep output, got %q", result.Content)
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
	dir := desktopSessionDir(globalTabWorkspaceRoot())
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
			"active": {
				ID:            "active",
				Scope:         "global",
				WorkspaceRoot: globalTabWorkspaceRoot(),
				Ctrl:          activeCtrl,
				Ready:         true,
				sink:          &tabEventSink{tabID: "active"},
				disabledMCP:   map[string]ServerView{},
			},
			"inactive": {
				ID:            "inactive",
				Scope:         "global",
				WorkspaceRoot: globalTabWorkspaceRoot(),
				SessionPath:   inactivePath,
				Ctrl:          inactiveCtrl,
				Ready:         true,
				sink:          &tabEventSink{tabID: "inactive"},
				disabledMCP:   map[string]ServerView{},
			},
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
	if inactiveCtrl.SessionPath() != inactivePath {
		t.Fatalf("original inactive controller session path = %q, want %q", inactiveCtrl.SessionPath(), inactivePath)
	}
	if app.tabs["inactive"].Ctrl == inactiveCtrl {
		t.Fatal("resume to a different sessionPath mutated the existing controller in place")
	}
	if app.tabs["inactive"].Ctrl.SessionPath() != targetPath {
		t.Fatalf("inactive tab session path = %q, want %q", app.tabs["inactive"].Ctrl.SessionPath(), targetPath)
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

func TestResumeSessionForTabDetachesRunningRuntimeForDifferentSessionPath(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	topicID := "topic_same"
	sessionA := filepath.Join(dir, "session-a.jsonl")
	sessionB := filepath.Join(dir, "session-b.jsonl")
	writeHistoryTestSession(t, sessionA, "session A prompt")
	writeHistoryTestSession(t, sessionB, "session B prompt")

	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrlA := control.New(control.Options{
		Runner:      runner,
		SessionDir:  dir,
		SessionPath: sessionA,
		Label:       "session-a",
		Sink:        event.Discard,
	})
	defer ctrlA.Close()

	app := NewApp()
	tab := &WorkspaceTab{
		ID:            "topic-tab",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		TopicID:       topicID,
		TopicTitle:    "Same topic",
		SessionPath:   sessionA,
		Ctrl:          ctrlA,
		Ready:         true,
		sink:          &tabEventSink{tabID: "topic-tab", app: app},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	ctrlA.Submit("keep running")
	<-runner.started

	got, err := app.ResumeSessionForTab(tab.ID, sessionB)
	if err != nil {
		t.Fatalf("ResumeSessionForTab: %v", err)
	}
	if !ctrlA.Running() {
		t.Fatal("session A controller was cancelled while resuming session B")
	}
	if ctrlA.SessionPath() != sessionA {
		t.Fatalf("session A controller path = %q, want %q", ctrlA.SessionPath(), sessionA)
	}
	detached := app.detachedSessions[sessionRuntimeKey(sessionA)]
	if detached == nil || detached.Ctrl != ctrlA {
		t.Fatalf("session A runtime was not detached: %+v", detached)
	}
	if detached.ID == tab.ID {
		t.Fatalf("detached runtime kept visible tab id %q", detached.ID)
	}
	if detached.sink == nil {
		t.Fatal("detached runtime lost its event sink")
	}
	if detached.sink.tabID == tab.ID {
		t.Fatalf("detached sink tab id = %q, want non-visible id", detached.sink.tabID)
	}
	if app.tabs[tab.ID].Ctrl == ctrlA {
		t.Fatal("visible tab still points at session A runtime after resuming session B")
	}
	if gotPath := app.tabs[tab.ID].Ctrl.SessionPath(); gotPath != sessionB {
		t.Fatalf("visible tab session path = %q, want %q", gotPath, sessionB)
	}
	if len(got) != 1 || got[0].Content != "session B prompt" {
		t.Fatalf("resumed history = %+v, want session B prompt", got)
	}

	visible := app.tabs[tab.ID]
	detached.sink.Emit(event.Event{Kind: event.TurnStarted})
	if visible.ActivityStatus != "" {
		t.Fatalf("detached runtime event changed visible tab status to %q", visible.ActivityStatus)
	}
	detached.sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		Name:   "read_file",
		Args:   `{"path":"detached.go","offset":3,"limit":7}`,
		Output: "package main",
	}})
	if got := detached.telemetrySnapshot().ReadFiles; len(got) != 1 || got[0].Path != "detached.go" || got[0].Offset != 3 || got[0].Limit != 7 {
		t.Fatalf("detached runtime read telemetry = %+v", got)
	}
	if got := visible.telemetrySnapshot().ReadFiles; len(got) != 0 {
		t.Fatalf("detached runtime read telemetry was recorded on visible tab: %+v", got)
	}
	detached.sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{PromptTokens: 42}})
	detached.sink.Emit(event.Event{Kind: event.TurnDone})
	if detached.usageTelemetry.PromptTokens != 42 {
		t.Fatalf("detached runtime usage was not recorded on detached tab: %+v", detached.usageTelemetry)
	}
	if detached.usageTelemetry.RequestCount != 1 {
		t.Fatalf("detached runtime request count = %d, want 1", detached.usageTelemetry.RequestCount)
	}
	if visible.usageTelemetry.PromptTokens != 0 {
		t.Fatalf("detached runtime usage was recorded on visible tab: %+v", visible.usageTelemetry)
	}
	if visible.saveAgain || visible.saving {
		t.Fatalf("detached runtime scheduled visible tab snapshot: saving=%v saveAgain=%v", visible.saving, visible.saveAgain)
	}

	close(runner.release)
	waitNotRunning(t, ctrlA)
}

func writeHistoryTestSession(t *testing.T, path, prompt string) {
	t.Helper()
	session := agent.NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: prompt})
	if err := session.Save(path); err != nil {
		t.Fatalf("Save %s: %v", path, err)
	}
}

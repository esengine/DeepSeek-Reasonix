package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

func TestHistoryMessagesIncludeAssistantReasoning(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "expanded prompt"},
		{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "thinking trace", WorkDurationMs: 24_000, ToolCalls: []provider.ToolCall{{
			ID: "call_1", Name: "bash", Arguments: `{"command":"pwd"}`,
		}}, MemoryCitations: []provider.MemoryCitation{{
			ID: "mem-1", Source: "Memory v5", Note: "use previous bash failure", Kind: "constraint",
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
	if got[0].SubmitText != "expanded prompt" {
		t.Fatalf("user submit text = %q, want expanded prompt", got[0].SubmitText)
	}
	if got[1].Reasoning != "thinking trace" {
		t.Fatalf("assistant reasoning = %q, want thinking trace", got[1].Reasoning)
	}
	if got[1].WorkDurationMs != 24_000 {
		t.Fatalf("assistant work duration = %d, want 24000", got[1].WorkDurationMs)
	}
	if len(got[1].MemoryCitations) != 1 || got[1].MemoryCitations[0].Note != "use previous bash failure" {
		t.Fatalf("assistant memory citations not preserved: %+v", got[1].MemoryCitations)
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

func TestHistoryMessagesDoNotReplayMemoryCompilerContract(t *testing.T) {
	raw := historyMemoryCompilerContract(t, "ship the refactor")
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: raw},
		{Role: provider.RoleAssistant, Content: "done"},
	}

	got := historyMessages(msgs, control.StripComposePrefixes)
	if len(got) != 2 {
		t.Fatalf("history length = %d, want 2: %+v", len(got), got)
	}
	if got[0].Content != "ship the refactor" {
		t.Fatalf("visible user content = %q, want source_event", got[0].Content)
	}
	if got[0].SubmitText != "" {
		t.Fatalf("raw Memory v5 contract should not be replay submitText, got %q", got[0].SubmitText)
	}
	assertNoHistoryMemoryContract(t, got[0].Content)
}

func TestHistoryMessagesStripActiveGoalFromVisibleUserContent(t *testing.T) {
	raw := "<active-goal>\nship the approval redesign\n</active-goal>\n\ncontinue implementation"
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: raw},
		{Role: provider.RoleAssistant, Content: "done"},
	}

	got := historyMessages(msgs, control.StripComposePrefixes)
	if len(got) != 2 {
		t.Fatalf("history length = %d, want 2: %+v", len(got), got)
	}
	if got[0].Content != "continue implementation" {
		t.Fatalf("visible user content = %q, want active-goal stripped", got[0].Content)
	}
	if strings.Contains(got[0].Content, "<active-goal>") || strings.Contains(got[0].Content, "ship the approval redesign") {
		t.Fatalf("active-goal leaked into visible history content: %+v", got[0])
	}
}

func TestHistoryMessagesCarryCheckpointTurnsAcrossHiddenSyntheticUsers(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first visible"},
		{Role: provider.RoleAssistant, Content: "first answer"},
		{Role: provider.RoleUser, Content: "Continue pursuing the active goal. If it is complete, provide the concise final result."},
		{Role: provider.RoleAssistant, Content: "hidden continuation"},
		{Role: provider.RoleUser, Content: "second visible"},
		{Role: provider.RoleAssistant, Content: "second answer"},
	}

	got := historyMessagesWithPlannerDisplays(
		msgs,
		func(content string) string { return content },
		nil,
		map[int]int{1: 0, 5: 2},
	)
	var users []HistoryMessage
	for _, msg := range got {
		if msg.Role == "user" {
			users = append(users, msg)
		}
	}
	if len(users) != 2 {
		t.Fatalf("visible users = %d, want 2: %+v", len(users), got)
	}
	if users[0].CheckpointTurn == nil || *users[0].CheckpointTurn != 0 {
		t.Fatalf("first checkpoint turn = %v, want 0", users[0].CheckpointTurn)
	}
	if users[1].CheckpointTurn == nil || *users[1].CheckpointTurn != 2 {
		t.Fatalf("second checkpoint turn = %v, want 2", users[1].CheckpointTurn)
	}
}

func TestHistoryPageFromMessagesWindowsByUserTurn(t *testing.T) {
	messages := []HistoryMessage{
		{Role: "notice", Content: "session restored"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "one"},
		{Role: "tool", ToolName: "bash", Content: "tool one"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "three"},
	}

	latest := historyPageFromMessages(messages, 0, 2)
	if latest.StartTurn != 1 || latest.EndTurn != 3 || latest.TotalTurns != 3 || !latest.HasOlder {
		t.Fatalf("latest page metadata = %+v, want turns 1-3/3 hasOlder", latest)
	}
	if len(latest.Messages) != 4 || latest.Messages[0].Content != "second" || latest.Messages[3].Content != "three" {
		t.Fatalf("latest page messages = %+v, want second and third turns", latest.Messages)
	}

	older := historyPageFromMessages(messages, latest.StartTurn, 2)
	if older.StartTurn != 0 || older.EndTurn != 1 || older.TotalTurns != 3 || older.HasOlder {
		t.Fatalf("older page metadata = %+v, want turns 0-1/3 no older", older)
	}
	if len(older.Messages) != 4 || older.Messages[0].Content != "session restored" || older.Messages[1].Content != "first" {
		t.Fatalf("older page messages = %+v, want prelude and first turn", older.Messages)
	}
}

func TestHistoryPageFromProviderMessagesWindowsVisibleUsers(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleUser, Content: "Continue pursuing the active goal. If it is complete, provide the concise final result."},
		{Role: provider.RoleAssistant, Content: "hidden continuation"},
		{Role: provider.RoleUser, Content: "second"},
		{Role: provider.RoleAssistant, Content: "two"},
		{Role: provider.RoleUser, Content: agent.MidTurnSteerPrefix + "\nupdate the plan"},
		{Role: provider.RoleUser, Content: "third"},
		{Role: provider.RoleAssistant, Content: "three"},
	}

	latest := historyPageFromProviderMessages(
		msgs,
		func(content string) string { return content },
		nil,
		map[int]int{1: 0, 5: 2, 8: 3},
		0,
		2,
	)
	if latest.StartTurn != 1 || latest.EndTurn != 3 || latest.TotalTurns != 3 || !latest.HasOlder {
		t.Fatalf("latest page metadata = %+v, want turns 1-3/3 hasOlder", latest)
	}
	if len(latest.Messages) != 5 {
		t.Fatalf("latest page length = %d, want 5: %+v", len(latest.Messages), latest.Messages)
	}
	if latest.Messages[0].Role != "user" || latest.Messages[0].Content != "second" {
		t.Fatalf("first latest message = %+v, want second user", latest.Messages[0])
	}
	if latest.Messages[0].CheckpointTurn == nil || *latest.Messages[0].CheckpointTurn != 2 {
		t.Fatalf("second user checkpoint = %v, want 2", latest.Messages[0].CheckpointTurn)
	}
	if latest.Messages[2].Role != "notice" || !strings.Contains(latest.Messages[2].Content, "update the plan") {
		t.Fatalf("steer message = %+v, want notice in second turn window", latest.Messages[2])
	}
	if latest.Messages[3].Role != "user" || latest.Messages[3].Content != "third" {
		t.Fatalf("third latest message = %+v, want third user", latest.Messages[3])
	}
	if latest.Messages[3].CheckpointTurn == nil || *latest.Messages[3].CheckpointTurn != 3 {
		t.Fatalf("third user checkpoint = %v, want 3", latest.Messages[3].CheckpointTurn)
	}

	older := historyPageFromProviderMessages(
		msgs,
		func(content string) string { return content },
		nil,
		map[int]int{1: 0, 5: 2, 8: 3},
		latest.StartTurn,
		2,
	)
	if older.StartTurn != 0 || older.EndTurn != 1 || older.TotalTurns != 3 || older.HasOlder {
		t.Fatalf("older page metadata = %+v, want turns 0-1/3 no older", older)
	}
	if len(older.Messages) != 4 || older.Messages[0].Role != "system" || older.Messages[1].Content != "first" || older.Messages[3].Content != "hidden continuation" {
		t.Fatalf("older page messages = %+v, want prelude and first visible turn", older.Messages)
	}
}

func TestHistoryCheckpointTurnsSkipsHiddenUsers(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "first visible"},
		{Role: provider.RoleAssistant, Content: "ok"},
		{Role: provider.RoleUser, Content: "Continue pursuing the active goal. If it is complete, provide the concise final result."},
		{Role: provider.RoleUser, Content: "second visible"},
	}
	got := historyCheckpointTurns(
		msgs,
		func(content string) string { return content },
		map[int]int{0: 0, 2: 1, 3: 2},
	)
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("checkpoint turns = %v, want [0 2]", got)
	}
}

func TestHistoryForTabRestoresPlannerDisplayAfterReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	handoff := strings.Join([]string{
		"# Reasonix executor handoff",
		"",
		"You are the executor now.",
		"",
		"Original task:",
		"fix the sandbox reload bug",
		"",
		"Planner output:",
		"inspect settings rebuild and preserve planner display",
		"",
		"Executor instructions:",
		"- apply the fix",
	}, "\n")

	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: handoff})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "executor kept working"})
	ag := agent.New(stubProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, SessionDir: dir, SessionPath: path, Sink: event.Discard})
	if err := recordSessionDisplay(dir, path, handoff, "fix the sandbox reload bug"); err != nil {
		t.Fatalf("recordSessionDisplay: %v", err)
	}

	app := &App{
		tabs:        map[string]*WorkspaceTab{},
		activeTabID: "planner_tab",
	}
	tab := &WorkspaceTab{ID: "planner_tab", Scope: "global", Ctrl: ctrl, Ready: true, disabledMCP: map[string]ServerView{}}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs[tab.ID] = tab

	tab.sink.Emit(event.Event{Kind: event.TurnStarted})
	tab.sink.Emit(event.Event{Kind: event.Phase, Text: "deepseek-v4-pro · planning", Source: event.UsageSourcePlanner})
	tab.sink.Emit(event.Event{Kind: event.Reasoning, Text: "planner thinking\n", Source: event.UsageSourcePlanner})
	tab.sink.Emit(event.Event{Kind: event.Text, Text: "planner visible plan", Source: event.UsageSourcePlanner})
	tab.sink.Emit(event.Event{Kind: event.Message, Text: "planner visible plan", Reasoning: "planner thinking\n", Source: event.UsageSourcePlanner})
	tab.sink.Emit(event.Event{Kind: event.TurnStarted})
	tab.sink.Emit(event.Event{Kind: event.TurnDone})
	waitForAutosaveIdle(t, tab)

	got := app.HistoryForTab(tab.ID)
	if len(got) != 5 {
		t.Fatalf("history length = %d, want user + planner phase + planner answer + executor answer (plus system skipped later by UI): %+v", len(got), got)
	}
	if got[1].Content != "fix the sandbox reload bug" {
		t.Fatalf("user display content = %q, want original prompt", got[1].Content)
	}
	if got[2].Role != "phase" || !strings.Contains(got[2].Content, "planning") {
		t.Fatalf("planner phase missing after reload: %+v", got)
	}
	if got[3].Role != "assistant" || got[3].Content != "planner visible plan" || got[3].Reasoning != "planner thinking\n" {
		t.Fatalf("planner assistant display missing after reload: %+v", got[3])
	}
	if got[4].Role != "assistant" || got[4].Content != "executor kept working" {
		t.Fatalf("executor answer missing after reload: %+v", got[4])
	}
}

func TestHistoryForTabUsesPinnedSessionBeforeControllerReady(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "pending-controller.jsonl")
	writeHistoryTestSession(t, path, "warm prompt")

	app := NewApp()
	tab := &WorkspaceTab{
		ID:            "pending",
		Scope:         "global",
		WorkspaceRoot: root,
		SessionPath:   path,
		Ready:         false,
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	got := app.HistoryForTab(tab.ID)
	if len(got) != 1 || got[0].Role != "user" || got[0].Content != "warm prompt" {
		t.Fatalf("pending controller history = %+v, want warm prompt", got)
	}
}

func historyMemoryCompilerContract(t *testing.T, sourceEvent string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"type": "memory_v5_execution_contract",
		"planner_ir": map[string]any{
			"source_event": sourceEvent,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return "<memory-compiler-execution>\n" + string(body) + "\n</memory-compiler-execution>"
}

func assertNoHistoryMemoryContract(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, "<memory-compiler-execution>") ||
		strings.Contains(text, "</memory-compiler-execution>") ||
		strings.Contains(text, "memory_v5_execution_contract") ||
		strings.Contains(text, "planner_ir") {
		t.Fatalf("history leaked Memory v5 contract content: %q", text)
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

func TestHistoryMessagesKeepRunSkillSubjectWhenArchived(t *testing.T) {
	args := `{"name":"code-reviewer","arguments":"review this branch"}`
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "call_skill", Name: "run_skill", Arguments: args,
		}}},
		{Role: provider.RoleTool, Name: "run_skill", ToolCallID: "call_skill", Content: "Skill completed"},
	}

	got := historyMessages(msgs, func(content string) string { return content })
	if len(got) != 2 {
		t.Fatalf("history length = %d, want 2", len(got))
	}
	call := got[0].ToolCalls[0]
	if !call.ArgumentsArchived || call.Arguments != "" {
		t.Fatalf("run_skill arguments should be archived after completion: %+v", call)
	}
	if call.Subject != "code-reviewer" {
		t.Fatalf("run_skill subject = %q, want code-reviewer", call.Subject)
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

func TestRebindTabToLoadedSessionReusesPreloadedTranscript(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	currentPath := filepath.Join(dir, "current.jsonl")
	targetPath := filepath.Join(dir, "target.jsonl")
	writeHistoryTestSession(t, currentPath, "current prompt")
	writeHistoryTestSession(t, targetPath, "target prompt")

	loaded, err := agent.LoadSession(targetPath)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if err := os.Remove(targetPath); err != nil {
		t.Fatalf("remove target session: %v", err)
	}

	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: currentPath, Label: "current", Sink: event.Discard})
	defer ctrl.Close()

	app := NewApp()
	tab := &WorkspaceTab{
		ID:            "tab",
		Scope:         "global",
		WorkspaceRoot: root,
		SessionPath:   currentPath,
		Ctrl:          ctrl,
		Ready:         true,
		sink:          &tabEventSink{tabID: "tab", app: app},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.rebindTabToLoadedSessionPath(tab, targetPath, loaded); err != nil {
		t.Fatalf("rebindTabToLoadedSessionPath: %v", err)
	}
	got := app.HistoryForTab(tab.ID)
	if len(got) != 1 || got[0].Content != "target prompt" {
		t.Fatalf("rebound history = %+v, want target prompt", got)
	}
	if gotPath := app.tabs[tab.ID].Ctrl.SessionPath(); gotPath != targetPath {
		t.Fatalf("rebound session path = %q, want %q", gotPath, targetPath)
	}
}

func TestRebindTabToLoadedSessionPersistsAndRestoresSessionProfile(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	currentPath := filepath.Join(dir, "current.jsonl")
	targetPath := filepath.Join(dir, "target.jsonl")
	writeHistoryTestSession(t, currentPath, "current prompt")
	writeHistoryTestSession(t, targetPath, "target prompt")
	if err := agent.SaveBranchMetaPreserveUpdated(targetPath, agent.BranchMeta{
		TokenMode:        boot.TokenModeFull,
		Mode:             "yolo",
		ToolApprovalMode: control.ToolApprovalYolo,
	}); err != nil {
		t.Fatalf("SaveBranchMetaPreserveUpdated target: %v", err)
	}

	loaded, err := agent.LoadSession(targetPath)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: currentPath, Label: "current", Sink: event.Discard})
	ctrl.SetMode(true, false)
	ctrl.SetToolApprovalMode(control.ToolApprovalAuto)
	defer ctrl.Close()

	app := NewApp()
	tab := &WorkspaceTab{
		ID:               "tab",
		Scope:            "global",
		WorkspaceRoot:    root,
		SessionPath:      currentPath,
		Ctrl:             ctrl,
		Ready:            true,
		tokenMode:        boot.TokenModeEconomy,
		mode:             "plan",
		toolApprovalMode: control.ToolApprovalAuto,
		sink:             &tabEventSink{tabID: "tab", app: app},
		disabledMCP:      map[string]ServerView{},
	}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.rebindTabToLoadedSessionPath(tab, targetPath, loaded); err != nil {
		t.Fatalf("rebindTabToLoadedSessionPath: %v", err)
	}

	currentMeta, ok, err := agent.LoadBranchMeta(currentPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta current ok=%v err=%v", ok, err)
	}
	if currentMeta.TokenMode != boot.TokenModeEconomy || currentMeta.Mode != "plan" || currentMeta.ToolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("current session profile = token:%q mode:%q approval:%q, want economy/plan/auto",
			currentMeta.TokenMode, currentMeta.Mode, currentMeta.ToolApprovalMode)
	}
	if got := currentTabTokenMode(tab); got != boot.TokenModeFull {
		t.Fatalf("rebound token mode = %q, want full", got)
	}
	if got := currentTabMode(tab); got != "yolo" {
		t.Fatalf("rebound mode = %q, want yolo", got)
	}
	if got := currentTabToolApprovalMode(tab); got != control.ToolApprovalYolo {
		t.Fatalf("rebound tool approval = %q, want yolo", got)
	}
}

func TestCloseTabPersistsSessionProfileBeforeRemovingVisibleTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	currentPath := filepath.Join(dir, "profile.jsonl")
	otherPath := filepath.Join(dir, "other.jsonl")
	writeHistoryTestSession(t, currentPath, "profile prompt")
	writeHistoryTestSession(t, otherPath, "other prompt")
	if err := agent.SaveBranchMetaPreserveUpdated(currentPath, agent.BranchMeta{}); err != nil {
		t.Fatalf("SaveBranchMetaPreserveUpdated current: %v", err)
	}

	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: currentPath, Label: "profile", Sink: event.Discard})
	ctrl.SetMode(true, false)
	ctrl.SetToolApprovalMode(control.ToolApprovalAuto)
	ctrl.SetGoal("finish the review")

	app := NewApp()
	tab := &WorkspaceTab{
		ID:               "profile",
		Scope:            "global",
		WorkspaceRoot:    root,
		SessionPath:      currentPath,
		Ctrl:             ctrl,
		Ready:            true,
		tokenMode:        boot.TokenModeEconomy,
		mode:             "plan",
		toolApprovalMode: control.ToolApprovalAuto,
		sink:             &tabEventSink{tabID: "profile", app: app},
		disabledMCP:      map[string]ServerView{},
	}
	other := &WorkspaceTab{
		ID:            "other",
		Scope:         "global",
		WorkspaceRoot: root,
		SessionPath:   otherPath,
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs[tab.ID] = tab
	app.tabs[other.ID] = other
	app.tabOrder = []string{tab.ID, other.ID}
	app.activeTabID = tab.ID

	if err := app.CloseTab(tab.ID); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}

	meta, ok, err := agent.LoadBranchMeta(currentPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta current ok=%v err=%v", ok, err)
	}
	if meta.TokenMode != boot.TokenModeEconomy || meta.Mode != "plan" || meta.ToolApprovalMode != control.ToolApprovalAuto || meta.Goal != "finish the review" {
		t.Fatalf("closed session profile = token:%q mode:%q approval:%q goal:%q, want economy/plan/auto/goal",
			meta.TokenMode, meta.Mode, meta.ToolApprovalMode, meta.Goal)
	}
}

func TestKeepOnlyVisibleTabPersistsRemovedSessionProfile(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	keepPath := filepath.Join(dir, "keep.jsonl")
	removedPath := filepath.Join(dir, "removed.jsonl")
	writeHistoryTestSession(t, keepPath, "keep prompt")
	writeHistoryTestSession(t, removedPath, "removed prompt")
	if err := agent.SaveBranchMetaPreserveUpdated(removedPath, agent.BranchMeta{}); err != nil {
		t.Fatalf("SaveBranchMetaPreserveUpdated removed: %v", err)
	}

	removedCtrl := control.New(control.Options{SessionDir: dir, SessionPath: removedPath, Label: "removed", Sink: event.Discard})
	removedCtrl.SetMode(true, false)
	removedCtrl.SetToolApprovalMode(control.ToolApprovalAuto)
	removedCtrl.SetGoal("keep this profile")

	app := NewApp()
	keep := &WorkspaceTab{
		ID:            "keep",
		Scope:         "global",
		WorkspaceRoot: root,
		SessionPath:   keepPath,
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	removed := &WorkspaceTab{
		ID:               "removed",
		Scope:            "global",
		WorkspaceRoot:    root,
		SessionPath:      removedPath,
		Ctrl:             removedCtrl,
		Ready:            true,
		tokenMode:        boot.TokenModeEconomy,
		mode:             "plan",
		toolApprovalMode: control.ToolApprovalAuto,
		sink:             &tabEventSink{tabID: "removed", app: app},
		disabledMCP:      map[string]ServerView{},
	}
	app.tabs[keep.ID] = keep
	app.tabs[removed.ID] = removed
	app.tabOrder = []string{keep.ID, removed.ID}
	app.activeTabID = removed.ID

	if _, err := app.keepOnlyVisibleTab(keep.ID); err != nil {
		t.Fatalf("keepOnlyVisibleTab: %v", err)
	}

	meta, ok, err := agent.LoadBranchMeta(removedPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta removed ok=%v err=%v", ok, err)
	}
	if meta.TokenMode != boot.TokenModeEconomy || meta.Mode != "plan" || meta.ToolApprovalMode != control.ToolApprovalAuto || meta.Goal != "keep this profile" {
		t.Fatalf("removed session profile = token:%q mode:%q approval:%q goal:%q, want economy/plan/auto/goal",
			meta.TokenMode, meta.Mode, meta.ToolApprovalMode, meta.Goal)
	}
}

func TestLoadTabSessionProfileIgnoresTerminalGoalState(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	sessionPath := filepath.Join(dir, "terminal-goal.jsonl")
	writeHistoryTestSession(t, sessionPath, "terminal prompt")
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		TokenMode:        boot.TokenModeEconomy,
		Mode:             "plan",
		ToolApprovalMode: control.ToolApprovalAuto,
		Goal:             "stale terminal goal",
	}); err != nil {
		t.Fatalf("SaveBranchMetaPreserveUpdated: %v", err)
	}
	if err := os.WriteFile(store.SessionGoalState(sessionPath), []byte(`{"goal":"stale terminal goal","status":"complete"}`), 0o644); err != nil {
		t.Fatalf("write goal state: %v", err)
	}

	profile := loadTabSessionProfile(sessionPath)
	if profile.goal != "" {
		t.Fatalf("loaded profile goal = %q, want terminal goal ignored", profile.goal)
	}
	if profile.tokenMode != boot.TokenModeEconomy || profile.mode != "plan" || profile.toolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("loaded profile = token:%q mode:%q approval:%q, want economy/plan/auto",
			profile.tokenMode, profile.mode, profile.toolApprovalMode)
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

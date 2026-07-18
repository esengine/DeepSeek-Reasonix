package liveview

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/eventwire"
)

func TestReducerDesktopReplayEquivalent(t *testing.T) {
	raw := []eventwire.Event{
		{Kind: kindTurnStarted},
		{Kind: kindReasoning, Text: "think-1"},
		{Kind: "notice", Level: "warn", Text: "first notice", Detail: "detail", Code: "n1"},
		{Kind: kindText, Text: "draft-"},
		{Kind: kindToolDispatch, Tool: &eventwire.Tool{ID: "tool-a", Name: "bash", Partial: true, ArgChars: 11, ParentID: "parent-a"}},
		{Kind: kindToolDispatch, Tool: &eventwire.Tool{ID: "tool-a", Name: "bash", Args: `{"cmd":"go test"}`, ReadOnly: true, Profile: &eventwire.Profile{Model: "worker", Effort: "high"}}},
		{Kind: kindToolProgress, Tool: &eventwire.Tool{ID: "tool-a", Output: "running\n"}},
		{Kind: kindRetrying, RetryAttempt: 1, RetryMax: 3},
		{Kind: kindToolResult, Tool: &eventwire.Tool{ID: "tool-a", Output: "ok", DurationMs: 7}},
		{Kind: kindToolProgress, Tool: &eventwire.Tool{ID: "tool-a", Output: "late\n"}},
		{Kind: kindUsage, Usage: &eventwire.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, Source: "executor", Cost: 1, Currency: "$"}},
		{Kind: "phase", Text: "executor"},
		{Kind: kindCompactionStarted, Compaction: &eventwire.Compaction{Trigger: "auto"}},
		{Kind: "notice", Level: "info", Text: "between compaction events"},
		{Kind: kindCompactionDone, Compaction: &eventwire.Compaction{Trigger: "auto", Messages: 8, Summary: "summary", Archive: "archive"}},
		{Kind: "guardian_assessment", Guardian: &eventwire.Guardian{ID: "g1", Tool: "bash", Subject: "go test", Outcome: "allow", RiskLevel: "low"}},
		{Kind: "steer", Text: "focus tests"},
		{Kind: kindMessage, Text: "final answer", MemoryCitations: []eventwire.MemoryCitation{{Source: "memory", LineStart: 3, Note: "why"}}},
		{Kind: kindApprovalRequest, Approval: &eventwire.Approval{ID: "prompt-a", Tool: "bash", Subject: "go test"}},
		{Kind: kindUsage, Usage: &eventwire.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7, Source: "subagent", Cost: 2, Currency: "$"}},
		{Kind: kindUsage, Usage: &eventwire.Usage{PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25, Source: "executor", Cost: 3, Currency: "$"}},
		{Kind: kindRetrying, RetryAttempt: 2, RetryMax: 3},
		{Kind: "memory_compiler_stats", MemoryCompiler: &eventwire.MemoryCompiler{Injected: true, TotalNodes: 99}},
		{Kind: "mcp_surface_ready", Text: "background"},
	}

	var reducer Reducer
	for _, event := range raw {
		reducer.Apply(event)
	}
	snapshot := reducer.Snapshot()
	want := replayDesktop(raw)
	got := replayDesktop(snapshot)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Desktop replay changed current Turn semantics\n got: %#v\nwant: %#v\nsnapshot: %#v", got, want, snapshot)
	}
	if containsKind(snapshot, "memory_compiler_stats") || containsKind(snapshot, "mcp_surface_ready") {
		t.Fatalf("background-only event leaked into snapshot: %+v", snapshot)
	}
	toolResult := findEvent(snapshot, kindToolResult)
	if toolResult == nil || toolResult.Tool == nil || toolResult.Tool.Output != "ok" {
		t.Fatalf("coalesced Tool result = %+v, want original final result", toolResult)
	}
	progress := findEventAfter(snapshot, kindToolProgress, kindToolResult)
	if progress == nil || progress.Tool == nil || progress.Tool.Output != "late\n" {
		t.Fatalf("post-result Tool progress = %+v", progress)
	}
}

func TestReducerCoalescesMoreThanTenThousandDeltas(t *testing.T) {
	raw := make([]eventwire.Event, 0, 12002)
	raw = append(raw, eventwire.Event{Kind: kindTurnStarted})
	for index := 0; index < 12001; index++ {
		kind := kindText
		prefix := "t"
		if index%2 == 0 {
			kind = kindReasoning
			prefix = "r"
		}
		event := eventwire.Event{Kind: kind, Text: prefix + strconv.Itoa(index) + ";"}
		raw = append(raw, event)
	}
	// TurnStarted is intentionally applied first so the reducer's live state has
	// the same empty assistant bubble as Desktop.
	var withStart Reducer
	for _, event := range raw {
		withStart.Apply(event)
	}
	snapshot := withStart.Snapshot()
	if len(snapshot) != 3 {
		t.Fatalf("12001 deltas reduced to %d events, want TurnStarted + two semantic streams", len(snapshot))
	}
	if got, want := replayDesktop(snapshot), replayDesktop(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("coalesced delta replay differs\n got: %#v\nwant: %#v", got, want)
	}
}

func TestToolWithoutWireIDPreservesDesktopFallbackWithoutInventingID(t *testing.T) {
	raw := []eventwire.Event{
		{Kind: kindTurnStarted},
		{Kind: kindToolDispatch, Tool: &eventwire.Tool{Name: "write_file", Partial: true, ArgChars: 123}},
		{Kind: kindToolDispatch, Tool: &eventwire.Tool{Name: "write_file", Args: `{"path":"a"}`}},
		{Kind: kindToolResult, Tool: &eventwire.Tool{Output: "done"}},
	}
	var reducer Reducer
	for _, event := range raw {
		reducer.Apply(event)
	}
	snapshot := reducer.Snapshot()
	for _, event := range snapshot {
		if event.Tool != nil && event.Tool.ID != "" {
			t.Fatalf("liveview invented a Host/business Tool ID %q in %+v", event.Tool.ID, event)
		}
	}
	if got, want := replayDesktop(snapshot), replayDesktop(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("no-ID Tool fallback changed\n got: %#v\nwant: %#v\nsnapshot: %#v", got, want, snapshot)
	}
}

func TestFullToolRefreshAfterResultPreservesObservableOrder(t *testing.T) {
	longArgs := strings.Repeat("x", 300)
	raw := []eventwire.Event{
		{Kind: kindTurnStarted},
		{Kind: kindToolDispatch, Tool: &eventwire.Tool{ID: "refresh-tool", Name: "write_file", Args: "short"}},
		{Kind: kindToolResult, Tool: &eventwire.Tool{ID: "refresh-tool", Output: "done"}},
		// Desktop applies a refreshed full dispatch to the already archived card.
		// Moving it before ToolResult would incorrectly archive its new arguments.
		{Kind: kindToolDispatch, Tool: &eventwire.Tool{ID: "refresh-tool", Name: "write_file", Args: longArgs, Refreshed: true}},
	}
	var reducer Reducer
	for _, event := range raw {
		reducer.Apply(event)
	}
	snapshot := reducer.Snapshot()
	if got, want := replayDesktop(snapshot), replayDesktop(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("post-result full dispatch moved across result\n got: %#v\nwant: %#v\nsnapshot: %#v", got, want, snapshot)
	}
	got := replayDesktop(snapshot)
	toolIndex := replayToolIndex(got.Items, "refresh-tool")
	if toolIndex < 0 {
		t.Fatal("refreshed Tool card is missing")
	}
	if got.Items[toolIndex].Tool.Args != longArgs {
		t.Fatalf("refreshed args = %q, want %d bytes", got.Items[toolIndex].Tool.Args, len(longArgs))
	}
}

func TestReducerRetainsDistinctToolsAndNoticesBeyond4096(t *testing.T) {
	const count = 5000
	var reducer Reducer
	reducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("tool-%04d", index)
		reducer.Apply(eventwire.Event{Kind: kindToolDispatch, Tool: &eventwire.Tool{ID: id, Name: "read", Args: id, ReadOnly: true}})
		reducer.Apply(eventwire.Event{Kind: kindToolResult, Tool: &eventwire.Tool{ID: id, Output: "done:" + id}})
		reducer.Apply(eventwire.Event{Kind: "notice", Level: "info", Text: fmt.Sprintf("notice-%04d", index)})
	}
	snapshot := reducer.Snapshot()
	tools := 0
	notices := make([]string, 0, count)
	for _, event := range snapshot {
		switch event.Kind {
		case kindToolDispatch:
			if event.Tool != nil && !event.Tool.Partial {
				tools++
			}
		case "notice":
			notices = append(notices, event.Text)
		}
	}
	if tools != count {
		t.Fatalf("retained Tool cards = %d, want %d", tools, count)
	}
	if len(notices) != count || notices[0] != "notice-0000" || notices[count-1] != "notice-4999" {
		t.Fatalf("retained notices = %d first=%q last=%q", len(notices), first(notices), last(notices))
	}
}

func TestReducerCoalescesMoreThan4096ToolProgressChunks(t *testing.T) {
	const count = 6000
	var reducer Reducer
	reducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	reducer.Apply(eventwire.Event{Kind: kindToolDispatch, Tool: &eventwire.Tool{ID: "stream-tool", Name: "bash", Args: `{"cmd":"long"}`}})
	var want strings.Builder
	for index := 0; index < count; index++ {
		chunk := strconv.Itoa(index) + ","
		want.WriteString(chunk)
		reducer.Apply(eventwire.Event{Kind: kindToolProgress, Tool: &eventwire.Tool{ID: "stream-tool", Output: chunk}})
	}
	snapshot := reducer.Snapshot()
	if len(snapshot) != 3 {
		t.Fatalf("6000 progress chunks reduced to %d events, want 3", len(snapshot))
	}
	progress := findEvent(snapshot, kindToolProgress)
	if progress == nil || progress.Tool == nil || progress.Tool.Output != want.String() {
		t.Fatalf("coalesced progress bytes = %d, want %d", toolOutputLen(progress), want.Len())
	}
}

func TestClearPromptUsesCurrentOpaqueID(t *testing.T) {
	var reducer Reducer
	reducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	reducer.Apply(eventwire.Event{Kind: kindApprovalRequest, Approval: &eventwire.Approval{ID: "approval-old", Tool: "bash", Subject: "old"}})
	reducer.Apply(eventwire.Event{Kind: kindAskRequest, Ask: &eventwire.Ask{ID: "ask-new", Questions: []eventwire.AskQuestion{{ID: "q1", Prompt: "new"}}}})
	if reducer.ClearPrompt("approval-old") {
		t.Fatal("old Approval ID cleared the newer Ask")
	}
	if reducer.ClearPrompt("wrong") {
		t.Fatal("wrong Prompt ID cleared the current Ask")
	}
	if prompt := retainedSnapshotPrompt(reducer.Snapshot()); prompt != "ask-new" {
		t.Fatalf("retained Prompt = %q, want ask-new", prompt)
	}
	if !reducer.ClearPrompt("ask-new") || retainedSnapshotPrompt(reducer.Snapshot()) != "" {
		t.Fatal("current Ask was not cleared")
	}

	reducer.Apply(eventwire.Event{Kind: kindApprovalRequest, Approval: &eventwire.Approval{ID: "approval-new", Tool: "bash", Subject: "new"}})
	if !reducer.ClearPrompt("approval-new") || retainedSnapshotPrompt(reducer.Snapshot()) != "" {
		t.Fatal("current Approval was not cleared")
	}
}

func TestSnapshotIsDeepCopy(t *testing.T) {
	var reducer Reducer
	reducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	message := eventwire.Event{Kind: kindMessage, Text: "answer", MemoryCitations: []eventwire.MemoryCitation{{Source: "source", Note: "note"}}}
	tool := eventwire.Event{Kind: kindToolDispatch, Tool: &eventwire.Tool{ID: "deep-tool", Name: "task", Args: "args", Profile: &eventwire.Profile{Model: "model", Effort: "high"}}}
	approval := eventwire.Event{Kind: kindApprovalRequest, Approval: &eventwire.Approval{
		ID: "deep-prompt", Tool: "mcp__srv__write", Subject: "subject",
		MCPTrust: &eventwire.MCPTrust{ChangedTools: []string{"write"}, ToolChanges: []eventwire.MCPToolChange{{Name: "write", Kind: "added"}}, Readers: []string{"read"}},
	}}
	usage := eventwire.Event{Kind: kindUsage, Usage: &eventwire.Usage{PromptTokens: 1, TotalTokens: 1, Source: "executor", CacheDiagnostics: &eventwire.CacheDiagnostics{PrefixChangeReasons: []string{"tools"}}}}
	future := eventwire.Event{Kind: "future_visible_event", Readiness: &eventwire.FinalReadiness{Missing: []string{"tests"}}, Guardian: &eventwire.Guardian{Usage: &eventwire.Usage{TotalTokens: 2}}}
	for _, event := range []eventwire.Event{message, tool, approval, usage, future} {
		reducer.Apply(event)
	}

	message.MemoryCitations[0].Note = "mutated input"
	tool.Tool.Profile.Model = "mutated input"
	approval.Approval.MCPTrust.ChangedTools[0] = "mutated input"
	approval.Approval.MCPTrust.ToolChanges[0].Name = "mutated input"
	usage.Usage.CacheDiagnostics.PrefixChangeReasons[0] = "mutated input"
	future.Readiness.Missing[0] = "mutated input"
	future.Guardian.Usage.TotalTokens = 99

	firstSnapshot := reducer.Snapshot()
	assertDeepValues(t, firstSnapshot)
	for index := range firstSnapshot {
		event := &firstSnapshot[index]
		if len(event.MemoryCitations) > 0 {
			event.MemoryCitations[0].Note = "mutated snapshot"
		}
		if event.Tool != nil && event.Tool.Profile != nil {
			event.Tool.Profile.Model = "mutated snapshot"
		}
		if event.Approval != nil && event.Approval.MCPTrust != nil {
			event.Approval.MCPTrust.ChangedTools[0] = "mutated snapshot"
			event.Approval.MCPTrust.ToolChanges[0].Name = "mutated snapshot"
		}
		if event.Usage != nil && event.Usage.CacheDiagnostics != nil {
			event.Usage.CacheDiagnostics.PrefixChangeReasons[0] = "mutated snapshot"
		}
		if event.Readiness != nil {
			event.Readiness.Missing[0] = "mutated snapshot"
		}
		if event.Guardian != nil && event.Guardian.Usage != nil {
			event.Guardian.Usage.TotalTokens = 100
		}
	}
	assertDeepValues(t, reducer.Snapshot())

	var askReducer Reducer
	askReducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	ask := eventwire.Event{Kind: kindAskRequest, Ask: &eventwire.Ask{ID: "deep-ask", Questions: []eventwire.AskQuestion{{
		ID: "q1", Prompt: "question", Options: []eventwire.AskOption{{Label: "A", Description: "first"}},
	}}}}
	askReducer.Apply(ask)
	ask.Ask.Questions[0].Options[0].Description = "mutated input"
	askSnapshot := askReducer.Snapshot()
	askEvent := findEvent(askSnapshot, kindAskRequest)
	if askEvent == nil || askEvent.Ask == nil || askEvent.Ask.Questions[0].Options[0].Description != "first" {
		t.Fatalf("Ask input alias leaked: %+v", askEvent)
	}
	askEvent.Ask.Questions[0].Options[0].Description = "mutated snapshot"
	again := findEvent(askReducer.Snapshot(), kindAskRequest)
	if again == nil || again.Ask.Questions[0].Options[0].Description != "first" {
		t.Fatalf("Ask snapshot alias leaked: %+v", again)
	}

	var streamReducer Reducer
	streamReducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	streamReducer.Apply(eventwire.Event{Kind: kindText, Text: "before"})
	streamSnapshot := streamReducer.Snapshot()
	streamReducer.Apply(eventwire.Event{Kind: kindText, Text: "-after"})
	if text := findEvent(streamSnapshot, kindText); text == nil || text.Text != "before" {
		t.Fatalf("later Apply mutated an earlier Snapshot: %+v", text)
	}
}

func TestTurnBoundariesBackgroundAndConcurrentCallers(t *testing.T) {
	var reducer Reducer
	reducer.Apply(eventwire.Event{Kind: "notice", Text: "old"})
	reducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	reducer.Apply(eventwire.Event{Kind: kindRetrying, RetryAttempt: 1, RetryMax: 2})
	reducer.Apply(eventwire.Event{Kind: "memory_compiler_stats", MemoryCompiler: &eventwire.MemoryCompiler{Injected: true}})
	if snapshot := reducer.Snapshot(); !containsKind(snapshot, kindRetrying) || containsText(snapshot, "old") {
		t.Fatalf("new Turn/background retry semantics = %+v", snapshot)
	}
	reducer.Apply(eventwire.Event{Kind: kindTurnDone, Outcome: "completed"})
	if snapshot := reducer.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("TurnDone retained live state: %+v", snapshot)
	}

	reducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	const workers = 8
	const perWorker = 700
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := 0; index < perWorker; index++ {
				reducer.Apply(eventwire.Event{Kind: "notice", Text: fmt.Sprintf("%d/%d", worker, index)})
				if index%100 == 0 {
					_ = reducer.Snapshot()
				}
			}
		}()
	}
	wait.Wait()
	snapshot := reducer.Snapshot()
	seen := make([]string, 0, workers*perWorker)
	for _, event := range snapshot {
		if event.Kind == "notice" {
			seen = append(seen, event.Text)
		}
	}
	if len(seen) != workers*perWorker {
		t.Fatalf("concurrent notices = %d, want %d", len(seen), workers*perWorker)
	}
	sort.Strings(seen)
	if seen[0] == seen[len(seen)-1] {
		t.Fatal("concurrent Apply lost distinct events")
	}
}

func TestResetClearsRetiringRuntimeState(t *testing.T) {
	var reducer Reducer
	reducer.Apply(eventwire.Event{Kind: kindTurnStarted})
	reducer.Apply(eventwire.Event{Kind: kindText, Text: "unfinished"})
	reducer.Apply(eventwire.Event{Kind: kindApprovalRequest, Approval: &eventwire.Approval{ID: "prompt-retiring", Tool: "bash", Subject: "retire"}})
	if len(reducer.Snapshot()) == 0 {
		t.Fatal("test setup did not create live state")
	}
	reducer.Reset()
	if snapshot := reducer.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("Reset retained retiring runtime state: %+v", snapshot)
	}
}

type replayState struct {
	Items           []replayItem
	Current         int
	Running         bool
	RetryAttempt    int
	RetryMax        int
	PromptKind      string
	PromptID        string
	TurnArgChars    int
	TurnTokens      int
	TurnTotalTokens int
	TurnCost        float64
	SessionTokens   int
	SessionCost     float64
	SessionCurrency string
	ContextUsed     int
	Usage           *eventwire.Usage
	Seq             int
}

type replayItem struct {
	Kind              string
	ID                string
	Text              string
	Detail            string
	Code              string
	Level             string
	Reasoning         string
	ReasoningComplete bool
	Streaming         bool
	Status            string
	Tool              *eventwire.Tool
	Archived          bool
	Compaction        *eventwire.Compaction
	MemoryCitations   []eventwire.MemoryCitation
}

func replayDesktop(events []eventwire.Event) replayState {
	state := replayState{Current: -1, SessionCurrency: "¥"}
	ensureAssistant := func() int {
		if state.Current >= 0 && state.Current < len(state.Items) && state.Items[state.Current].Kind == "assistant" {
			return state.Current
		}
		state.Items = append(state.Items, replayItem{Kind: "assistant", ID: fmt.Sprintf("a%d", state.Seq), Streaming: true})
		state.Current = len(state.Items) - 1
		state.Seq++
		return state.Current
	}
	for _, event := range events {
		if event.Kind == "memory_compiler_stats" || event.Kind == "mcp_surface_ready" {
			continue
		}
		if event.Kind == kindRetrying {
			state.RetryAttempt, state.RetryMax = event.RetryAttempt, event.RetryMax
			continue
		}
		state.RetryAttempt, state.RetryMax = 0, 0
		switch event.Kind {
		case kindTurnStarted:
			ensureAssistant()
			state.Running = true
			state.TurnArgChars = 0
			state.TurnTokens, state.TurnTotalTokens, state.TurnCost = 0, 0, 0
		case kindText, kindReasoning:
			index := ensureAssistant()
			delta := event.Text
			if delta == "" {
				delta = event.Reasoning
			}
			if event.Kind == kindText {
				state.Items[index].Text += delta
				if state.Items[index].Reasoning != "" {
					state.Items[index].ReasoningComplete = true
				}
			} else {
				state.Items[index].Reasoning += delta
				state.Items[index].ReasoningComplete = false
			}
		case kindMessage:
			var existing *replayItem
			if state.Current >= 0 && state.Current < len(state.Items) && state.Items[state.Current].Kind == "assistant" {
				existing = &state.Items[state.Current]
			}
			text := event.Text
			reasoning := event.Reasoning
			if existing != nil {
				if text == "" {
					text = existing.Text
				}
				if reasoning == "" {
					reasoning = existing.Reasoning
				}
			}
			if strings.TrimSpace(text) == "" && strings.TrimSpace(reasoning) == "" {
				if existing != nil && existing.Text == "" && existing.Reasoning == "" && len(existing.MemoryCitations) == 0 {
					state.Items = append(state.Items[:state.Current], state.Items[state.Current+1:]...)
				}
				state.Current = -1
				continue
			}
			index := ensureAssistant()
			state.Items[index].Text = text
			state.Items[index].Reasoning = reasoning
			state.Items[index].Streaming = false
			state.Items[index].ReasoningComplete = reasoning != ""
			state.Items[index].MemoryCitations = append([]eventwire.MemoryCitation(nil), event.MemoryCitations...)
			state.Current = -1
		case kindToolDispatch:
			replayToolDispatch(&state, event)
		case kindToolResult:
			replayToolResult(&state, event)
		case kindToolProgress:
			replayToolProgress(&state, event)
		case kindUsage:
			if !state.Running {
				continue
			}
			value := usageValue(event.Usage)
			state.TurnTokens += value.completion
			state.TurnTotalTokens += value.total
			state.SessionTokens += value.total
			state.TurnCost += value.cost
			state.SessionCost += value.cost
			state.TurnArgChars = 0
			if event.Usage != nil && event.Usage.Currency != "" {
				state.SessionCurrency = event.Usage.Currency
			}
			if event.Usage == nil || strings.TrimSpace(event.Usage.Source) == "" || strings.TrimSpace(event.Usage.Source) == "executor" {
				state.Usage = cloneUsage(event.Usage)
				if event.Usage != nil {
					state.ContextUsed = event.Usage.PromptTokens
				}
			}
		case "notice":
			state.Items = append(state.Items, replayItem{Kind: "notice", ID: fmt.Sprintf("n%d", state.Seq), Text: event.Text, Detail: event.Detail, Code: event.Code, Level: event.Level})
			state.Seq++
		case "phase":
			state.Items = append(state.Items, replayItem{Kind: "phase", ID: fmt.Sprintf("p%d", state.Seq), Text: event.Text})
			state.Seq++
		case "steer":
			state.Items = append(state.Items, replayItem{Kind: "notice", ID: fmt.Sprintf("s%d", state.Seq), Text: "↪ " + event.Text, Level: "info"})
			state.Seq++
		case "guardian_assessment":
			if event.Guardian != nil {
				level := "info"
				if event.Guardian.Outcome == "deny" {
					level = "warn"
				}
				state.Items = append(state.Items, replayItem{Kind: "notice", ID: fmt.Sprintf("g%d", state.Seq), Text: fmt.Sprintf("%+v", *event.Guardian), Level: level})
				state.Seq++
			}
		case kindCompactionStarted:
			state.Items = append(state.Items, replayItem{Kind: "compaction", ID: fmt.Sprintf("c%d", state.Seq), Compaction: cloneCompaction(event.Compaction), Status: "pending"})
			state.Seq++
		case kindCompactionDone:
			index := -1
			for itemIndex := len(state.Items) - 1; itemIndex >= 0; itemIndex-- {
				if state.Items[itemIndex].Kind == "compaction" && state.Items[itemIndex].Status == "pending" {
					index = itemIndex
					break
				}
			}
			if event.Compaction == nil || event.Compaction.Summary == "" {
				if index >= 0 {
					state.Items = append(state.Items[:index], state.Items[index+1:]...)
				}
				continue
			}
			if index < 0 {
				state.Items = append(state.Items, replayItem{Kind: "compaction", ID: fmt.Sprintf("c%d", state.Seq), Compaction: cloneCompaction(event.Compaction), Status: "done"})
				state.Seq++
			} else {
				state.Items[index].Compaction = cloneCompaction(event.Compaction)
				state.Items[index].Status = "done"
			}
		case kindApprovalRequest:
			state.Running = true
			state.PromptKind = kindApprovalRequest
			if event.Approval != nil {
				state.PromptID = event.Approval.ID
			}
		case kindAskRequest:
			state.Running = true
			state.PromptKind = kindAskRequest
			if event.Ask != nil {
				state.PromptID = event.Ask.ID
			}
		case kindTurnDone:
			state = replayState{Current: -1, SessionCurrency: "¥"}
		}
	}
	return state
}

func replayToolDispatch(state *replayState, event eventwire.Event) {
	if event.Tool == nil {
		return
	}
	tool := event.Tool
	if tool.Partial && tool.ArgChars > 0 {
		state.TurnArgChars = tool.ArgChars
	}
	if tool.Partial && tool.ID == "" {
		return
	}
	index := replayToolIndex(state.Items, tool.ID)
	if tool.Partial {
		if index >= 0 {
			item := &state.Items[index]
			if item.Status == "running" && item.Tool.Args == "" && tool.ArgChars > 0 {
				item.Tool.ArgChars = tool.ArgChars
			}
			return
		}
		copy := cloneTool(tool)
		state.Items = append(state.Items, replayItem{Kind: "tool", ID: tool.ID, Tool: copy, Status: "running"})
		state.Seq++
		return
	}
	if index >= 0 {
		item := &state.Items[index]
		next := cloneTool(tool)
		if next.Args == "" {
			next.Args = item.Tool.Args
		}
		if next.Profile == nil {
			next.Profile = cloneProfile(item.Tool.Profile)
		}
		next.ID = item.Tool.ID
		next.ParentID = item.Tool.ParentID
		next.ArgChars = 0
		next.Partial = false
		item.Tool = next
		return
	}
	id := tool.ID
	if id == "" {
		id = fmt.Sprintf("tool%d", state.Seq)
	}
	copy := cloneTool(tool)
	copy.ID = id
	state.Items = append(state.Items, replayItem{Kind: "tool", ID: id, Tool: copy, Status: "running"})
	state.Seq++
}

func replayToolResult(state *replayState, event eventwire.Event) {
	if event.Tool == nil {
		return
	}
	index := replayToolIndex(state.Items, event.Tool.ID)
	if index < 0 {
		for itemIndex := len(state.Items) - 1; itemIndex >= 0; itemIndex-- {
			if state.Items[itemIndex].Kind == "tool" && state.Items[itemIndex].Status == "running" {
				index = itemIndex
				break
			}
		}
	}
	if index < 0 {
		return
	}
	item := &state.Items[index]
	item.Tool.Output = event.Tool.Output
	item.Tool.Err = event.Tool.Err
	item.Tool.Truncated = event.Tool.Truncated
	item.Tool.DurationMs = event.Tool.DurationMs
	if event.Tool.Err != "" {
		item.Status = "error"
	} else {
		item.Status = "done"
	}
	// Desktop archives every completed result immediately. A later progress
	// event can still append a fresh visible tail to the archived card.
	for itemIndex := range state.Items {
		candidate := &state.Items[itemIndex]
		if candidate.Kind == "tool" && candidate.Status != "running" {
			if len(candidate.Tool.Args) > 200 {
				candidate.Tool.Args = candidate.Tool.Args[:200] + "…"
			}
			candidate.Tool.Output = ""
			candidate.Archived = true
		}
	}
}

func replayToolProgress(state *replayState, event eventwire.Event) {
	if event.Tool == nil || event.Tool.ID == "" {
		return
	}
	index := replayToolIndex(state.Items, event.Tool.ID)
	if index >= 0 {
		state.Items[index].Tool.Output += event.Tool.Output
	}
}

func replayToolIndex(items []replayItem, id string) int {
	if id == "" {
		return -1
	}
	for index := range items {
		if items[index].Kind == "tool" && items[index].ID == id {
			return index
		}
	}
	return -1
}

func assertDeepValues(t *testing.T, snapshot []eventwire.Event) {
	t.Helper()
	message := findEvent(snapshot, kindMessage)
	if message == nil || len(message.MemoryCitations) != 1 || message.MemoryCitations[0].Note != "note" {
		t.Fatalf("message deep copy = %+v", message)
	}
	tool := findEvent(snapshot, kindToolDispatch)
	if tool == nil || tool.Tool == nil || tool.Tool.Profile == nil || tool.Tool.Profile.Model != "model" {
		t.Fatalf("Tool deep copy = %+v", tool)
	}
	prompt := findEvent(snapshot, kindApprovalRequest)
	if prompt == nil || prompt.Approval == nil || prompt.Approval.MCPTrust == nil || prompt.Approval.MCPTrust.ChangedTools[0] != "write" || prompt.Approval.MCPTrust.ToolChanges[0].Name != "write" {
		t.Fatalf("Approval deep copy = %+v", prompt)
	}
	usage := findEvent(snapshot, kindUsage)
	if usage == nil || usage.Usage == nil || usage.Usage.CacheDiagnostics == nil || usage.Usage.CacheDiagnostics.PrefixChangeReasons[0] != "tools" {
		t.Fatalf("Usage deep copy = %+v", usage)
	}
	future := findEvent(snapshot, "future_visible_event")
	if future == nil || future.Readiness == nil || future.Readiness.Missing[0] != "tests" || future.Guardian == nil || future.Guardian.Usage == nil || future.Guardian.Usage.TotalTokens != 2 {
		t.Fatalf("future event deep copy = %+v", future)
	}
}

func containsKind(events []eventwire.Event, kind string) bool { return findEvent(events, kind) != nil }

func containsText(events []eventwire.Event, value string) bool {
	for _, event := range events {
		if event.Text == value {
			return true
		}
	}
	return false
}

func findEvent(events []eventwire.Event, kind string) *eventwire.Event {
	for index := range events {
		if events[index].Kind == kind {
			return &events[index]
		}
	}
	return nil
}

func findEventAfter(events []eventwire.Event, kind, after string) *eventwire.Event {
	seen := false
	for index := range events {
		if events[index].Kind == after {
			seen = true
			continue
		}
		if seen && events[index].Kind == kind {
			return &events[index]
		}
	}
	return nil
}

func retainedSnapshotPrompt(events []eventwire.Event) string {
	for _, event := range events {
		if id := retainedPromptID(event); id != "" {
			return id
		}
	}
	return ""
}

func cloneCompaction(in *eventwire.Compaction) *eventwire.Compaction {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func last(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func toolOutputLen(event *eventwire.Event) int {
	if event == nil || event.Tool == nil {
		return 0
	}
	return len(event.Tool.Output)
}

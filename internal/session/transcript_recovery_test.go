package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/transcript"
)

// The shape reproduces the reported long tool-driven turn without any copied
// user text: 147 messages, 46 assistants with visible content, 72 tool results.
func syntheticTranscriptRecoveryMessages() []provider.Message {
	messages := []provider.Message{
		{ID: "user", Role: provider.RoleUser, Content: "create a synthetic illustration", Origin: provider.MessageOriginUser},
		{ID: "introduction", Role: provider.RoleAssistant, Content: "Starting the illustration."},
	}
	for i := range 72 {
		callID := fmt.Sprintf("call-%02d", i)
		assistant := provider.Message{ID: fmt.Sprintf("assistant-%02d", i), Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{ID: callID, Name: "synthetic_tool", Arguments: `{}`}}}
		if i < 44 {
			assistant.Content = fmt.Sprintf("Iteration %02d is ready for inspection.", i)
			assistant.ReasoningContent = fmt.Sprintf("Synthetic reasoning for iteration %02d.", i)
		}
		messages = append(messages, assistant, provider.Message{ID: fmt.Sprintf("tool-%02d", i), Role: provider.RoleTool, Name: "synthetic_tool", ToolCallID: callID, Content: fmt.Sprintf("synthetic result %02d", i)})
	}
	return append(messages, provider.Message{ID: "final-answer", Role: provider.RoleAssistant, Content: "The synthetic illustration is complete.", ReasoningContent: "All requested checks completed.", WorkDurationMs: 933524})
}

func TestTranscriptLongTurnRemainsReachableAfterBoundedEvictionAndRestart(t *testing.T) {
	service, err := NewService("local", NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "synthetic-long-turn"})
	if err != nil {
		t.Fatal(err)
	}
	messages := syntheticTranscriptRecoveryMessages()
	if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: "turn-start", TurnID: "turn", Events: []Event{{Kind: "turn/start"}}}); err != nil {
		t.Fatal(err)
	}
	for i, message := range messages {
		if len(message.ToolCalls) > 0 {
			attempt, _ := json.Marshal(map[string]any{"id": message.ID, "messageId": message.ID, "action": "begin"})
			call, _ := json.Marshal(map[string]any{"id": message.ToolCalls[0].ID, "name": "synthetic_tool"})
			// Repeated records for one stable identity must not inflate counts.
			if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: fmt.Sprintf("sampling-%03d", i), TurnID: "turn", Events: []Event{
				{Kind: "assistant/attempt", Payload: attempt}, {Kind: "assistant/attempt", Payload: attempt},
				{Kind: "tool/call", Payload: call}, {Kind: "tool/call", Payload: call},
			}}); err != nil {
				t.Fatal(err)
			}
		}
		payload, err := json.Marshal(map[string]any{"message": message})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: fmt.Sprintf("message-%03d", i), TurnID: "turn", Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: "turn-end", TurnID: "turn", Events: []Event{{Kind: "turn/end", Payload: []byte(`{"status":"completed"}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := runtime.Transcript().Snapshot(transcript.PageRequest{Records: 32})
	if err != nil || before.TotalRecords > 96 || len(before.Records) > 32 {
		t.Fatalf("publisher exceeded resident budget: total=%d page=%d error=%v", before.TotalRecords, len(before.Records), err)
	}
	ref, query := runtime.Ref(), service.Query()
	// Materialize the locator synchronously so this reachability regression
	// does not depend on the unrelated asynchronous index polling budget.
	indexCtx, cancelIndex := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelIndex()
	if _, _, err := query.prepareHistoryIndex(indexCtx, ref); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]provider.Message)
	newest := windowReady(t, query, ref, HistoryWindowRequest{Anchor: "newest", Limit: 32})
	final := newest.Messages[len(newest.Messages)-1]
	if !final.TurnFinal || final.MessageID != "final-answer" || final.TurnDurationMs != 933524 {
		t.Fatalf("history lost the authoritative final identity or complete turn duration: %+v", final)
	}
	if final.SamplingCount == nil || *final.SamplingCount != 72 || final.ToolCount == nil || *final.ToolCount != 72 {
		t.Fatalf("history lost distinct attempt/tool counts: %+v", final)
	}
	page := newest
	for {
		if page.Status != "ready" || len(page.Messages) > 32 || page.SnapshotSequence != newest.SnapshotSequence {
			t.Fatalf("invalid bounded history page: %+v", page)
		}
		for _, stored := range page.Messages {
			body := []byte(stored.Inline)
			if stored.ContentRef != nil {
				body, err = query.ReadContent(t.Context(), ref, *stored.ContentRef, 0, stored.ContentRef.Bytes)
				if err != nil {
					t.Fatal(err)
				}
			}
			var message provider.Message
			if err := json.Unmarshal(body, &message); err != nil {
				t.Fatalf("decode %s: %v", stored.MessageID, err)
			}
			if _, duplicate := seen[message.ID]; duplicate {
				t.Fatalf("history repeated message %q", message.ID)
			}
			seen[message.ID] = message
		}
		if !page.HasOlder {
			break
		}
		if page.OlderCursor == "" {
			t.Fatal("evicted older history has no continuation")
		}
		page = windowReady(t, query, ref, HistoryWindowRequest{Anchor: "cursor", Cursor: page.OlderCursor, Limit: 32})
	}
	contentAssistants, toolResults := 0, 0
	for _, expected := range messages {
		actual, found := seen[expected.ID]
		if !found || actual.Content != expected.Content || actual.ReasoningContent != expected.ReasoningContent || actual.ToolCallID != expected.ToolCallID {
			t.Fatalf("message %q is missing or its body changed after eviction", expected.ID)
		}
		if actual.Role == provider.RoleAssistant && (actual.Content != "" || actual.ReasoningContent != "") {
			contentAssistants++
		}
		if actual.Role == provider.RoleTool {
			toolResults++
		}
	}
	if len(seen) != 147 || contentAssistants != 46 || toolResults != 72 {
		t.Fatalf("recovered shape: messages=%d visibleAssistants=%d tools=%d", len(seen), contentAssistants, toolResults)
	}
	// The oldest window must lead back to the newest; reclaiming one edge is
	// not deletion and must never strand the reader at an unloaded boundary.
	newerSeen := make(map[string]bool)
	for {
		for _, message := range page.Messages {
			newerSeen[message.MessageID] = true
		}
		if !page.HasNewer {
			break
		}
		if page.NewerCursor == "" {
			t.Fatal("older history has no forward continuation")
		}
		page = windowReady(t, query, ref, HistoryWindowRequest{Anchor: "cursor", Cursor: page.NewerCursor, Limit: 32})
	}
	if len(newerSeen) != len(messages) || !newerSeen["final-answer"] {
		t.Fatal("bidirectional paging did not reach all messages and final answer")
	}
	for _, id := range []string{"introduction", "assistant-00", "final-answer"} {
		location, err := query.LocateMessage(t.Context(), ref, id, newest.SnapshotSequence)
		if err != nil || location.Status != "ready" || location.MessageID != id {
			t.Fatalf("locate %q: status=%q error=%v", id, location.Status, err)
		}
	}
	if err := service.Close(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	binding, err := service.Open(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = binding.Release(context.Background()) })
	reopened := binding.Runtime()
	// This path constructs only session/query objects: reopening and following
	// must not need an Agent, provider, or a model request to reconstruct UI.
	initial, err := reopened.FollowTranscript(t.Context(), transcript.FollowRequest{})
	if err != nil || initial.Snapshot == nil {
		t.Fatalf("follow reopened session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = reopened.FollowTranscript(context.Background(), transcript.FollowRequest{Subscription: initial.Subscription, Close: true})
	})
	cut := initial.Snapshot
	if cut.Runtime.SamplingCount != 72 || cut.Runtime.ToolCount != 72 {
		t.Fatalf("restart lost distinct attempt/tool counts: %+v", cut.Runtime)
	}
	if cut.TotalRecords > 96 || cut.Runtime.Status != event.TurnCompleted || cut.Runtime.FinalMessageID != "final-answer" || cut.Runtime.DurationMs != 933524 {
		t.Errorf("reopened transcript lost terminal state or budget: records=%d runtime=%+v", cut.TotalRecords, cut.Runtime)
	}
	found := false
	for _, row := range cut.Records {
		found = found || row.Message.MessageID == "final-answer" && row.Message.Content == "The synthetic illustration is complete."
	}
	if !found || len(cut.ActiveAttempts) != 0 || len(cut.ActiveRecords) != 0 {
		t.Fatal("reopened completed follow omitted the final answer or manufactured an active turn")
	}
}

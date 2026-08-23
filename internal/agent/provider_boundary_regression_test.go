package agent

import (
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestNormalizeModelRequestMessagesDoesNotMutateInput(t *testing.T) {
	const createdAt = int64(99)
	const storedContent = "task\n<execution-policy>local policy</execution-policy>"
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: storedContent, CreatedAt: createdAt},
	}
	wantStored := append([]provider.Message(nil), msgs...)
	a := New(nil, tool.NewRegistry(), NewSession("system"), Options{}, event.Discard)

	got := a.normalizeModelRequestMessages(msgs)
	if got[1].CreatedAt != 0 || got[1].Content != "task" {
		t.Fatalf("provider message = %+v, want stripped local metadata", got[1])
	}
	if !reflect.DeepEqual(msgs, wantStored) {
		t.Fatalf("normalization mutated its input: got=%+v want=%+v", msgs, wantStored)
	}
}

func TestPressurePruneNeverPromotesRawToolContent(t *testing.T) {
	const rawOnlySentinel = "RAW-ONLY-PRESSURE-SENTINEL"
	bounded := strings.Repeat("b", toolPruneThresholdRunes+1)
	raw := strings.Repeat("r", 100) + rawOnlySentinel + strings.Repeat("r", toolPruneThresholdRunes+1)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: bounded, RawContent: raw},
	}}
	a := New(nil, tool.NewRegistry(), sess, Options{}, event.Discard)

	a.sess.compactionRunMu.Lock()
	advanced, err := a.pruneToolResultsToProjectionLocked(CompactionTriggerPressure)
	a.sess.compactionRunMu.Unlock()
	if err != nil || !advanced {
		t.Fatalf("pressure prune advanced=%v err=%v", advanced, err)
	}
	visible := modelInputMessages(a.modelVisibleMessages())
	if strings.Contains(joinContents(visible), rawOnlySentinel) {
		t.Fatal("pressure projection promoted local RawContent")
	}
	if got := visible[len(visible)-1]; !strings.Contains(got.Content, toolPruneMarker) || got.RawContent != "" {
		t.Fatalf("provider tool result = %+v, want pruned bounded Content without RawContent", got)
	}
	stored := sess.Snapshot()[2]
	if stored.Content != bounded || stored.RawContent != raw {
		t.Fatal("pressure prune modified canonical tool content")
	}
}

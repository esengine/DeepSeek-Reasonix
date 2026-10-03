package transcript

import (
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// A cancelled attempt persists its partial output through
// agent.InterruptedStreamRecord, which carries no message id when the stream
// never reached one. Two of them used to derive the same last-resort record
// address, and NewProjection then rejected the whole session.
func TestHistoryKeepsDerivedRecordIdentitiesUnique(t *testing.T) {
	messages := []provider.Message{
		{ID: "user-1", Role: provider.RoleUser, Content: "do the thing"},
		agent.InterruptedStreamRecord("", "first partial", ""),
		agent.InterruptedStreamRecord("", "second partial", ""),
	}
	rows := History(messages, HistoryOptions{})
	seen := make(map[string]int, len(rows))
	for index, row := range rows {
		if row.RecordID == "" {
			t.Fatalf("row %d has no record identity: %+v", index, row)
		}
		if previous, exists := seen[row.RecordID]; exists {
			t.Fatalf("rows %d and %d share record identity %q", previous, index, row.RecordID)
		}
		seen[row.RecordID] = index
	}
	if _, err := NewProjection(testIdentity, rows, 0); err != nil {
		t.Fatalf("projection rejected an interrupted-attempt history: %v", err)
	}
}

// Two persisted records that really do claim one canonical identity are
// corruption, not a derived collision. They must still fail the projection.
func TestHistoryKeepsCanonicalDuplicatesRejected(t *testing.T) {
	messages := []provider.Message{
		{ID: "user-1", Role: provider.RoleUser, Content: "do the thing"},
		{ID: "result-one", Role: provider.RoleTool, ToolCallID: "reused-call", Content: "first result"},
		{ID: "result-two", Role: provider.RoleTool, ToolCallID: "reused-call", Content: "second result"},
	}
	if _, err := NewProjection(testIdentity, History(messages, HistoryOptions{}), 0); err == nil {
		t.Fatal("projection accepted two records claiming tool:reused-call")
	}
}

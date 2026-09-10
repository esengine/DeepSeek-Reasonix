package provider

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestReadCompletionAndDiagnosticStayHostOnly(t *testing.T) {
	receipt := &ReadCompletion{ID: "run", Reads: []CompletedRead{{ReadID: "r", Path: "file", Snapshot: "s", Intent: "range", Verdict: "partial_read_sufficient", Covered: [][2]int{{0, 2}}}}}
	message := Message{Role: RoleTool, Name: LocalOnlyToolName, ToolCallID: LocalOnlyToolID, LocalOnly: true, ReadCompletion: receipt}
	b, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(b, &restored); err != nil || !reflect.DeepEqual(restored.ReadCompletion, receipt) {
		t.Fatalf("round trip: %s %v", b, err)
	}
	base := []Message{{Role: RoleUser, Content: "continue"}, {Role: RoleAssistant, Content: "done"}}
	withMeta := append([]Message(nil), base...)
	withMeta[1].ReadCompletion = receipt
	withMeta[1].ToolDiagnostic = json.RawMessage(`{"code":"WRITE_EVIDENCE_MISSING","path":"private"}`)
	withMeta = append(withMeta, restored)
	if got := ModelMessages(withMeta); !reflect.DeepEqual(got, base) {
		t.Fatalf("provider metadata leaked: %+v", got)
	}
	var legacy Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"legacy"}`), &legacy); err != nil || legacy.ReadCompletion != nil || legacy.ToolDiagnostic != nil {
		t.Fatal("legacy message changed")
	}
}

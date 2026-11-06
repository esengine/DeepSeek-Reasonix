package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// batch is one assistant turn issuing several reads and one test run, the shape
// a model produces when it works through a package: the run fails, the reads do not.
func batch(round, reads int, readBody string) []provider.Message {
	calls := make([]provider.ToolCall, 0, reads+1)
	for i := range reads {
		calls = append(calls, provider.ToolCall{ID: fmt.Sprintf("r%d-%d", round, i), Name: "read_file", Arguments: fmt.Sprintf(`{"path":"p%d/f%d.go"}`, round, i)})
	}
	calls = append(calls, provider.ToolCall{ID: fmt.Sprintf("t%d", round), Name: "bash", Arguments: `{"command":"go test ./..."}`})
	msgs := []provider.Message{{Role: provider.RoleAssistant, Content: "working", ReasoningContent: "plan", ToolCalls: calls}}
	for _, c := range calls[:reads] {
		msgs = append(msgs, provider.Message{Role: provider.RoleTool, ToolCallID: c.ID, Name: "read_file", Content: readBody})
	}
	exit := 1
	return append(msgs, provider.Message{Role: provider.RoleTool, ToolCallID: calls[reads].ID, Name: "bash",
		Content: goTestFailure(), ToolExecution: &provider.ToolExecution{ExitCode: &exit}})
}

// One failing call batched with many reads used to pin the whole batch, so the
// candidate exceeded the ceiling and a billed summary was thrown away.
func TestFailingBatchDoesNotBlockTheFold(t *testing.T) {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}}
	var blocked []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ContextMaintenanceEvent && e.Maintenance != nil && e.Maintenance.Status == "blocked" {
			blocked = append(blocked, e.Maintenance.Code)
		}
	})
	a := New(&fakeProvider{reply: "## Goal\ndigest"}, tool.NewRegistry(), sess,
		Options{ContextWindow: 20000, CompactRatio: 0.8, ArchiveDir: testenv.TempDir(t)}, sink)
	for round := range 3 {
		for _, m := range batch(round, 8, strings.Repeat("source line with detail\n", 85)) {
			sess.Add(m)
		}
	}
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("clean work narrative. ", 900)})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "next"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	outcome, reason, err := a.window().compactToProjection(context.Background(), CompactionTriggerPressure, "", compactionScope{}, false)
	if err != nil || outcome != CompactionInstalled {
		t.Fatalf("outcome %v, reason %q, err %v", outcome, reason, err)
	}
	if len(blocked) > 0 {
		t.Fatalf("fold blocked with %v", blocked)
	}
	if got := len(visibleContext(a)); got >= len(sess.Messages) {
		t.Fatalf("nothing was folded: %d visible of %d", got, len(sess.Messages))
	}
}

// When the failures a session retains would by themselves exceed the ceiling, a
// summary billed to sit beside them is rejected whole and the block never
// lifts. The fold has to give up retaining them before it asks for one.
func TestRetainedFailuresYieldWhenTheyAloneExceedTheCeiling(t *testing.T) {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}}
	prov := &fakeProvider{reply: "## Goal\ndigest"}
	a := New(prov, tool.NewRegistry(), sess,
		Options{ContextWindow: 20000, CompactRatio: 0.8, CompactionBudgets: CompactionBudgets{CheckpointCeilingRatio: 0.12}, ArchiveDir: testenv.TempDir(t)}, event.Discard)
	for round := range 40 {
		for _, m := range batch(round, 0, "") {
			sess.Add(m)
		}
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("clean work narrative. ", 60)})
	}
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	outcome, reason, err := a.window().compactToProjection(context.Background(), CompactionTriggerPressure, "", compactionScope{}, false)
	if err != nil || outcome != CompactionInstalled {
		t.Fatalf("outcome %v, reason %q, err %v", outcome, reason, err)
	}
}

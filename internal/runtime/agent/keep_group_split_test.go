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

// A failure is evidence about its own call. Keeping it must not pin the reads
// that ran beside it, and what stays has to be a valid transaction on its own.
func TestFailurePinsOnlyItsOwnCall(t *testing.T) {
	a := &Agent{}
	a.keepPolicy = KeepErrors
	region := append([]provider.Message{{Role: provider.RoleUser, Content: "go"}}, batch(0, 6, strings.Repeat("source line\n", 400))...)
	kept, fold, _, _ := a.window().partitionFoldForProjection(region)

	var keptResults, keptCalls int
	for _, m := range kept {
		if m.Role == provider.RoleTool {
			keptResults++
		}
		keptCalls += len(m.ToolCalls)
	}
	if keptResults != 1 || keptCalls != 1 {
		t.Fatalf("kept %d calls and %d results, want only the failed pair", keptCalls, keptResults)
	}
	for _, m := range provider.ModelMessages(kept) {
		if strings.Contains(m.Content, "interrupted") {
			t.Fatalf("the kept assistant turn still issues a call nothing answers: %q", m.Content)
		}
	}
	rendered := renderTranscript(fold)
	if strings.Count(rendered, "[assistant calls read_file]") != 6 || strings.Count(rendered, "[tool read_file result]") != 6 {
		t.Fatalf("the reads did not reach the summarizer with their calls:\n%s", rendered)
	}
}

// The folded siblings keep their calls, because the digest's coverage guard
// reads changed files and failed commands from the calls the fold holds.
func TestFoldedSiblingsKeepTheirCalls(t *testing.T) {
	a := &Agent{}
	a.keepPolicy = KeepErrors
	region := []provider.Message{{Role: provider.RoleUser, Content: "go"}}
	region = append(region, batch(0, 2, "body")...)
	_, fold, _, _ := a.window().partitionFoldForProjection(region)
	var calls []string
	for _, m := range fold {
		for _, tc := range m.ToolCalls {
			calls = append(calls, tc.ID)
		}
	}
	if len(calls) != 2 || calls[0] != "r0-0" || calls[1] != "r0-1" {
		t.Fatalf("fold holds calls %v, want the two reads", calls)
	}
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
		Options{ContextWindow: 20000, CompactRatio: 0.8, RecentKeep: 2,
			KeepPolicy: KeepErrors, ArchiveDir: testenv.TempDir(t)}, sink)
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
		Options{ContextWindow: 20000, CompactRatio: 0.8, RecentKeep: 2, KeepPolicy: KeepErrors,
			CompactionBudgets: CompactionBudgets{CheckpointCeilingRatio: 0.12}, ArchiveDir: testenv.TempDir(t)}, event.Discard)
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

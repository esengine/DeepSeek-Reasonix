package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// A hand-typed /compact over a history with failed commands: the card has to go
// up before any model call, and a failure that is folded rather than kept costs
// no diagnosis call, because only a retained failure reads the selection.
func TestManualCompactAnnouncesBeforeAnyModelCall(t *testing.T) {
	sess := foldableSessionOverForce(30)
	code := 1
	failure := strings.Join(padded("[INFO] step ", 40), "\n")
	for i := range 3 {
		id := fmt.Sprintf("c%d", i)
		sess.Messages = append(sess.Messages[:4], append([]provider.Message{
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
			{Role: provider.RoleTool, ToolCallID: id, Content: failure, ToolExecution: &provider.ToolExecution{State: tool.ShellStateFailed, ExitCode: &code}},
		}, sess.Messages[4:]...)...)
	}

	prov := &countingProvider{reply: "## Standing facts\n- never change the public API"}
	callsAtStart := -1
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.CompactionStarted && callsAtStart < 0 {
			callsAtStart = len(prov.got)
		}
	})
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 200_000, CompactRatio: 0.8, RecentKeep: 2,
		SessionPath: filepath.Join(testenv.TempDir(t), "session.jsonl"),
		WorkspaceID: "ws", ModelRef: "p/m", ArchiveDir: testenv.TempDir(t),
	}, sink)

	verdict, err := a.CompactNow(context.Background(), CompactRequest{})
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if !verdict.Compacted() {
		t.Fatalf("verdict = %+v, want a fold", verdict)
	}
	if callsAtStart != 0 {
		t.Fatalf("model calls before the card went up = %d, want 0", callsAtStart)
	}
	if len(prov.got) != 1 {
		t.Fatalf("model calls = %d, want only the summary: folded failures are not diagnosed", len(prov.got))
	}
}

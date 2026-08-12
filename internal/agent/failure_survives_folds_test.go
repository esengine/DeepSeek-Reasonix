package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// goTestFailure is shaped like the real thing on purpose: a failing `go test`
// log opens with "=== RUN", so the keep policy's text-prefix arm cannot see it
// and only ToolExecution can. Synthetic "error: ..." fixtures survive either
// way and would have hidden this.
func goTestFailure() string {
	lines := make([]string, 0, 40)
	for i := range 40 {
		if i == 20 {
			lines = append(lines, "--- FAIL: TestCanary (0.01s)")
			continue
		}
		lines = append(lines, fmt.Sprintf("=== RUN   TestCase%03d", i))
	}
	return strings.Join(lines, "\n")
}

// KeepErrors reads ToolExecution because text matching was proven to miss real
// failures. That record has to outlive the fold that first protects it: a
// projection is the next compaction's input, so a failure stripped at write time
// is invisible to the pass after next and the model silently stops seeing it.
func TestRecordedFailureSurvivesRepeatedCompaction(t *testing.T) {
	bulk := strings.Repeat("work output line with detail. ", 250)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}}
	a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess,
		Options{ContextWindow: 8000, CompactRatio: 0.85, RecentKeep: 2,
			KeepPolicy: KeepErrors, ArchiveDir: t.TempDir()}, event.Discard)

	exit := 1
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "running tests",
		ToolCalls: []provider.ToolCall{{ID: "canary", Name: "bash", Arguments: "{}"}}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "canary", Name: "bash",
		Content: goTestFailure(), ToolExecution: &provider.ToolExecution{ExitCode: &exit}})

	// Three folds: the first protects the failure, and the ones after it are
	// where the record used to be gone.
	for round := 1; round <= 3; round++ {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: bulk})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("continue %d", round)})
		if err := a.compact(context.Background(), "manual", "", true); err != nil {
			t.Fatalf("round %d compact: %v", round, err)
		}

		proj := visibleContext(a)
		var carrier *provider.Message
		for i, m := range proj {
			if strings.Contains(m.Content, "TestCanary") {
				carrier = &proj[i]
			}
		}
		if carrier == nil {
			t.Fatalf("round %d: the failure left the projection; the model is no longer told about it", round)
		}
		if carrier.ToolExecution == nil {
			t.Fatalf("round %d: the failure record was stripped, so the next fold cannot classify it", round)
		}
		for _, m := range provider.ModelMessages(proj) {
			if m.ToolExecution != nil {
				t.Fatalf("round %d: local shell metadata reached the provider request", round)
			}
		}
	}
}

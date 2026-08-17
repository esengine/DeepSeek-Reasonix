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

// goTestFailure is shaped like a real failing `go test` result so the test
// verifies that structured failures enter the ordinary summary prefix.
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

// Deprecated KeepErrors must not pin the failure verbatim. The full failure is
// summarized while canonical storage and local execution metadata stay intact.
func TestRecordedFailureEntersSummaryAndCanonicalStaysIntact(t *testing.T) {
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

	for round := 1; round <= 1; round++ {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: bulk})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("continue %d", round)})
		if err := a.compact(context.Background(), "manual", "", true); err != nil {
			t.Fatalf("round %d compact: %v", round, err)
		}

		if !strings.Contains(joinContents(a.svc.prov.(*fakeProvider).got), "TestCanary") {
			t.Fatalf("round %d: failure did not enter summary input", round)
		}
		for _, m := range provider.ModelMessages(visibleContext(a)) {
			if m.ToolExecution != nil {
				t.Fatalf("round %d: local shell metadata reached the provider request", round)
			}
		}
	}
	if !strings.Contains(joinContents(sess.Snapshot()), "TestCanary") {
		t.Fatal("canonical transcript lost the failure")
	}
}

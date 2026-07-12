package evidence

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestMetaToolsDoNotMutate(t *testing.T) {
	for _, name := range []string{
		"run_skill", "read_skill", "read_only_skill", "task", "read_only_task",
		"parallel_tasks", "explore", "research", "review", "security_review", "use_capability",
	} {
		if ToolCallMutates(name, json.RawMessage(`{}`), false) {
			t.Fatalf("%s must not count as mutation", name)
		}
	}
}

func TestMergeChildPropagatesRealWrites(t *testing.T) {
	parent := NewLedger()
	parent.Record(ReceiptFromToolCall("task", json.RawMessage(`{"prompt":"edit"}`), true, false))
	if _, ok := parent.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("task alone must not create a mutation index")
	}

	child := NewLedger()
	child.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/a.go"}`), true, false))
	child.Record(ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/a.go"}`), true, true))
	parent.MergeChild(child.Summary())

	idx, ok := parent.LatestSuccessfulMutationIndex()
	if !ok {
		t.Fatal("expected merged child write to count as mutation")
	}
	paths := parent.PathsSince(idx)
	wantPath := filepath.ToSlash("internal/a.go")
	if len(paths) != 1 || filepath.ToSlash(paths[0]) != wantPath {
		t.Fatalf("paths = %v, want %s", paths, wantPath)
	}
	if !parent.HasSuccessfulReviewAfter(idx) {
		t.Fatal("child read of mutated path should satisfy review")
	}
}

func TestClassifyMutationRisk(t *testing.T) {
	low := []Receipt{
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"docs/GUIDE.md"}`), true, false),
	}
	if got := ClassifyMutationRisk(low, 0); got != RiskLow {
		t.Fatalf("docs risk = %s, want low", got)
	}

	med := []Receipt{
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, false),
	}
	if got := ClassifyMutationRisk(med, 0); got != RiskMedium {
		t.Fatalf("prod risk = %s, want medium", got)
	}

	high := []Receipt{
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, false),
	}
	if got := ClassifyMutationRisk(high, 0); got != RiskHigh {
		t.Fatalf("auth risk = %s, want high", got)
	}

	opaque := []Receipt{
		{ToolName: "bash", Success: true, Mutation: true, Command: "some-unknown-writer"},
	}
	if got := ClassifyMutationRisk(opaque, 0); got != RiskHigh {
		t.Fatalf("opaque risk = %s, want high", got)
	}
}

func TestStructuredReviewReportGate(t *testing.T) {
	ledger := NewLedger()
	ledger.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/a.go"}`), true, false))
	mutation, ok := ledger.LatestSuccessfulMutationIndex()
	if !ok {
		t.Fatal("expected mutation")
	}

	raw := json.RawMessage(`{
		"kind":"review",
		"verdict":"pass",
		"reviewed_paths":["internal/a.go"],
		"findings":[]
	}`)
	ledger.Record(Receipt{ToolName: "review_report", Args: raw, Success: true})
	if !ledger.HasSuccessfulStructuredReviewAfter(ReviewKindReview, mutation, []string{"internal/a.go"}) {
		t.Fatal("expected structured review coverage")
	}

	block := json.RawMessage(`{
		"kind":"security",
		"verdict":"block",
		"reviewed_paths":["internal/a.go"],
		"findings":[{"severity":"critical","summary":"hardcoded secret","path":"internal/a.go","line":1}]
	}`)
	ledger.Record(Receipt{ToolName: "review_report", Args: block, Success: true})
	ok, blocking, _ := ledger.HasStructuredReviewAfter(ReviewKindSecurity, mutation, []string{"internal/a.go"})
	if !ok || !blocking {
		t.Fatalf("security block: ok=%v blocking=%v", ok, blocking)
	}
}

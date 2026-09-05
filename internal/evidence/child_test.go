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
	parent.Record(ReceiptFromToolCall("task", json.RawMessage(`{"prompt":"edit"}`), true, ToolFacts{}))
	if _, ok := parent.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("task alone must not create a mutation index")
	}

	child := NewLedger()
	child.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/a.go"}`), true, ToolFacts{WritesNamedPaths: true}))
	child.Record(ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/a.go"}`), true, ToolFacts{ReadOnly: true}))
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
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"docs/GUIDE.md"}`), true, ToolFacts{WritesNamedPaths: true}),
	}
	if got := ClassifyMutationRisk(low, 0, nil); got != RiskLow {
		t.Fatalf("docs risk = %s, want low", got)
	}

	med := []Receipt{
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, ToolFacts{WritesNamedPaths: true}),
	}
	if got := ClassifyMutationRisk(med, 0, nil); got != RiskMedium {
		t.Fatalf("prod risk = %s, want medium", got)
	}

	// Sensitivity is declared, never inferred from spelling. Undeclared, a
	// permission file is ordinary production code; declared, it is High.
	high := []Receipt{
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, ToolFacts{WritesNamedPaths: true}),
	}
	if got := ClassifyMutationRisk(high, 0, nil); got != RiskMedium {
		t.Fatalf("undeclared path risk = %s, want medium", got)
	}
	if got := ClassifyMutationRisk(high, 0, []string{"internal/permission/**"}); got != RiskHigh {
		t.Fatalf("declared-sensitive risk = %s, want high", got)
	}
	// The retired table matched any path spelling these words. A trace file is
	// not a data race and a blocked-outcome file is not a lock.
	for _, p := range []string{"cmd/e2ebench/phasetrace.go", "internal/agent/conclude_blocked.go"} {
		spurious := []Receipt{ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"`+p+`"}`), true, ToolFacts{WritesNamedPaths: true})}
		if got := ClassifyMutationRisk(spurious, 0, []string{"internal/permission/**"}); got != RiskMedium {
			t.Fatalf("%s risk = %s, want medium (spelling is not sensitivity)", p, got)
		}
	}

	// A proven path-less write cannot be scored by path, so it remains high risk.
	opaque := []Receipt{
		ReceiptFromToolCall("bash", json.RawMessage(`{"command":"printf hi > out.log"}`), true, ToolFacts{}),
	}
	if got := ClassifyMutationRisk(opaque, 0, nil); got != RiskHigh {
		t.Fatalf("opaque risk = %s, want high", got)
	}

	// A command the host merely failed to prove read-only names no change set,
	// so it scores nothing rather than summoning a reviewer with no files.
	unproven := []Receipt{
		ReceiptFromToolCall("bash", json.RawMessage(`{"command":"gofmt -l ."}`), true, ToolFacts{}),
	}
	if got := ClassifyMutationRisk(unproven, 0, nil); got != RiskLow {
		t.Fatalf("unproven risk = %s, want low", got)
	}

	// Privileged/opaque tools keep escalating to High even without paths.
	opaquePrivileged := []Receipt{
		{ToolName: "mcp__srv__write", Success: true, Mutation: true},
	}
	if got := ClassifyMutationRisk(opaquePrivileged, 0, nil); got != RiskHigh {
		t.Fatalf("privileged opaque risk = %s, want high", got)
	}

	// An opaque write alongside a declared-sensitive path still classifies High.
	opaqueHighPath := []Receipt{
		{ToolName: "bash", Success: true, Mutation: true},
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, ToolFacts{WritesNamedPaths: true}),
	}
	if got := ClassifyMutationRisk(opaqueHighPath, 0, []string{"internal/permission/**"}); got != RiskHigh {
		t.Fatalf("opaque+declared risk = %s, want high", got)
	}
}

func TestStructuredReviewReportGate(t *testing.T) {
	ledger := NewLedger()
	ledger.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/a.go"}`), true, ToolFacts{WritesNamedPaths: true}))
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
	reviewAuth := GrantReviewAuthority(ReviewKindReview)
	ledger.Record(Receipt{ToolName: "review_report", Args: raw, Success: true,
		ReportKind: ReviewKindReview, ReviewAuthority: &reviewAuth, SourceExecutionID: "exec-probe"})
	if !ledger.HasSuccessfulStructuredReviewAfter(ReviewKindReview, mutation, []string{"internal/a.go"}) {
		t.Fatal("expected structured review coverage")
	}

	block := json.RawMessage(`{
		"kind":"security",
		"verdict":"block",
		"reviewed_paths":["internal/a.go"],
		"findings":[{"severity":"critical","summary":"hardcoded secret","path":"internal/a.go","line":1}]
	}`)
	securityAuth := GrantReviewAuthority(ReviewKindSecurity)
	ledger.Record(Receipt{ToolName: "review_report", Args: block, Success: true,
		ReportKind: ReviewKindSecurity, ReviewAuthority: &securityAuth, SourceExecutionID: "exec-probe"})
	ok, blocking, _ := ledger.HasStructuredReviewAfter(ReviewKindSecurity, mutation, []string{"internal/a.go"})
	if !ok || !blocking {
		t.Fatalf("security block: ok=%v blocking=%v", ok, blocking)
	}
}

// The retired table matched substrings, so any tool whose name merely contained
// "plugin" or "tool" counted as a privileged surface. Identity is exact now:
// the MCP namespace the host mints, and two built-in installers.
func TestPrivilegedMutationMatchesIdentityNotFragments(t *testing.T) {
	privileged := []string{"mcp__srv__write", "mcp__plugin_acme_files__write", "install_source", "install_skill"}
	for _, name := range privileged {
		got := ClassifyMutationRisk([]Receipt{{ToolName: name, Success: true, Mutation: true}}, 0, nil)
		if got != RiskHigh {
			t.Errorf("%s risk = %s, want high", name, got)
		}
	}
	ordinary := []string{"plugin_lint", "list_plugins", "edit_file", "multi_edit", "install_sourcemap"}
	for _, name := range ordinary {
		r := ReceiptFromToolCall(name, json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, ToolFacts{})
		if got := ClassifyMutationRisk([]Receipt{r}, 0, nil); got == RiskHigh {
			t.Errorf("%s risk = high: a name fragment is not a privileged surface", name)
		}
	}
}

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

	// A path-less bash write cannot be scored by path, so it remains high risk.
	opaque := []Receipt{
		{ToolName: "bash", Success: true, Mutation: true, Command: "some-unknown-writer"},
	}
	if got := ClassifyMutationRisk(opaque, 0); got != RiskHigh {
		t.Fatalf("opaque risk = %s, want high", got)
	}

	// Privileged/opaque tools keep escalating to High even without paths.
	opaquePrivileged := []Receipt{
		{ToolName: "mcp__srv__write", Success: true, Mutation: true},
	}
	if got := ClassifyMutationRisk(opaquePrivileged, 0); got != RiskHigh {
		t.Fatalf("privileged opaque risk = %s, want high", got)
	}

	// An opaque write alongside a security-sensitive path still classifies High.
	opaqueHighPath := []Receipt{
		{ToolName: "bash", Success: true, Mutation: true},
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, false),
	}
	if got := ClassifyMutationRisk(opaqueHighPath, 0); got != RiskHigh {
		t.Fatalf("opaque+auth risk = %s, want high", got)
	}
}

func TestClassifyToolCallMutationRiskBeforeExecution(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     json.RawMessage
		readOnly bool
		want     RiskLevel
	}{
		{name: "documentation edit", toolName: "edit_file", args: json.RawMessage(`{"path":"docs/GUIDE.md"}`), want: RiskLow},
		{name: "production edit", toolName: "edit_file", args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), want: RiskMedium},
		{name: "sensitive edit", toolName: "edit_file", args: json.RawMessage(`{"path":"internal/auth/session.go"}`), want: RiskHigh},
		{name: "opaque writer", toolName: "mcp__srv__write", args: json.RawMessage(`{}`), want: RiskHigh},
		{name: "reader", toolName: "read_file", args: json.RawMessage(`{"path":"internal/auth/session.go"}`), readOnly: true, want: RiskLow},
		{name: "guarded shell reader", toolName: "bash", args: json.RawMessage(`{"command":"node --version; bash --version"}`), readOnly: true, want: RiskLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyToolCallMutationRisk(tt.toolName, tt.args, tt.readOnly); got != tt.want {
				t.Fatalf("projected risk = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAbsoluteMutationRiskIgnoresWorkspaceAndTestHarnessAncestors(t *testing.T) {
	low := ClassifyToolCallMutationRisk("edit_file", json.RawMessage(`{"path":"/tmp/TestBuildToolSchemas123/001/written.txt"}`), false)
	if low != RiskLow {
		t.Fatalf("absolute temp ancestor risk = %s, want low", low)
	}
	high := ClassifyToolCallMutationRisk("edit_file", json.RawMessage(`{"path":"/tmp/TestBuildToolSchemas123/001/internal/auth/session.go"}`), false)
	if high != RiskHigh {
		t.Fatalf("absolute sensitive suffix risk = %s, want high", high)
	}
	deep := ClassifyToolCallMutationRisk(
		"edit_file",
		json.RawMessage(`{"path":"/tmp/TestBuildToolSchemas123/001/internal/provider/openai/responses/client.go"}`),
		false,
	)
	if deep != RiskHigh {
		t.Fatalf("deep absolute sensitive suffix risk = %s, want high", deep)
	}
}

func TestWorkspaceRelativeMutationRiskPreservesDeepOwnerAndIgnoresCheckoutName(t *testing.T) {
	root := "/workspace/toolbox"
	ordinary := ClassifyToolCallMutationRiskWithin(
		root,
		"edit_file",
		json.RawMessage(`{"path":"/workspace/toolbox/internal/agent/worker.go"}`),
		false,
	)
	if ordinary != RiskMedium {
		t.Fatalf("ordinary path inside toolbox checkout = %s, want medium", ordinary)
	}
	deep := ClassifyToolCallMutationRiskWithin(
		root,
		"edit_file",
		json.RawMessage(`{"path":"/workspace/toolbox/internal/provider/openai/responses/client.go"}`),
		false,
	)
	if deep != RiskHigh {
		t.Fatalf("deep provider path = %s, want high", deep)
	}
	windows := ClassifyToolCallMutationRiskWithin(
		`C:\workspace\toolbox`,
		"edit_file",
		json.RawMessage(`{"path":"c:\\workspace\\toolbox\\internal\\sandbox\\linux\\seccomp\\policy.go"}`),
		false,
	)
	if windows != RiskHigh {
		t.Fatalf("Windows deep sandbox path = %s, want high", windows)
	}
}

func TestWorkspaceRelativeMutationRiskIgnoresLowRiskAncestorNames(t *testing.T) {
	root := "/workspace/docs/project"
	got := ClassifyToolCallMutationRiskWithin(
		root,
		"edit_file",
		json.RawMessage(`{"path":"/workspace/docs/project/internal/agent/worker.go"}`),
		false,
	)
	if got != RiskMedium {
		t.Fatalf("production path beneath docs-named ancestor = %s, want medium", got)
	}
}

func TestRiskPathWithinHonorsRootAndPathBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		root      string
		path      string
		want      string
		wantMatch bool
	}{
		{
			name:      "posix root",
			root:      "/",
			path:      "/internal/provider/client.go",
			want:      "internal/provider/client.go",
			wantMatch: true,
		},
		{
			name:      "windows drive root",
			root:      `C:\`,
			path:      `c:\internal\provider\client.go`,
			want:      "internal/provider/client.go",
			wantMatch: true,
		},
		{
			name:      "sibling prefix",
			root:      "/workspace/toolbox",
			path:      "/workspace/toolbox-copy/internal/agent.go",
			wantMatch: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, matched := riskPathWithin(tt.root, tt.path)
			if matched != tt.wantMatch || got != tt.want {
				t.Fatalf("riskPathWithin(%q, %q) = (%q, %v), want (%q, %v)", tt.root, tt.path, got, matched, tt.want, tt.wantMatch)
			}
		})
	}
}

func TestAbsoluteMutationRiskPreservesSensitiveOwnerBeforeTempShape(t *testing.T) {
	receipts := []Receipt{{
		ToolName: "edit_file",
		Success:  true,
		Write:    true,
		Mutation: true,
		Paths:    []string{"/workspace/provider/TestWorker123/001/internal/client.go"},
	}}
	if got := ClassifyMutationRisk(receipts, 0); got != RiskHigh {
		t.Fatalf("risk = %q, want %q", got, RiskHigh)
	}
}

func TestMutationRiskIncludesEarlierHighRiskMutation(t *testing.T) {
	ledger := NewLedger()
	ledger.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/auth/session.go"}`), true, false))
	ledger.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"docs/GUIDE.md"}`), true, false))

	latest, ok := ledger.LatestSuccessfulMutationIndex()
	if !ok {
		t.Fatal("expected mutations")
	}
	if got := ledger.MutationRiskAfter(latest); got != RiskLow {
		t.Fatalf("latest-only risk = %s, want low test precondition", got)
	}
	if got := ledger.MutationRisk(); got != RiskHigh {
		t.Fatalf("turn risk = %s, want earlier auth mutation to keep it high", got)
	}
}

func TestLedgerMutationRiskWithinPreservesDeepSensitiveOwner(t *testing.T) {
	root := "/workspace/toolbox"
	ledger := NewLedger()
	ledger.Record(ReceiptFromToolCall(
		"edit_file",
		json.RawMessage(`{"path":"/workspace/toolbox/internal/provider/openai/responses/client.go"}`),
		true,
		false,
	))
	if got := ledger.MutationRiskWithin(root); got != RiskHigh {
		t.Fatalf("workspace-normalized ledger risk = %s, want high", got)
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

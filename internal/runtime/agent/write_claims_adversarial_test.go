package agent

import (
	"path/filepath"
	"reasonix/internal/runtime/writeclaim"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/evidence"
)

// Layer 5: even if a write reaches the workspace through a surface the claim
// could not bind, the host reports it to the parent rather than staying silent.
func TestClaimViolationsSurfaceOutOfClaimMutations(t *testing.T) {
	root := testenv.TempDir(t)
	claim, err := writeclaim.NormalizeWritePaths(root, []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{filepath.Join(root, "auth", "token.go")}},
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{filepath.Join(root, "package.json")}},
	}}
	got := claimViolations(summary, claim)
	if len(got) != 1 || !strings.HasSuffix(got[0], "package.json") {
		t.Fatalf("violations = %v, want just the out-of-claim package.json", got)
	}
	block := formatHostReceipts(summary, claim)
	if !strings.Contains(block, "OUTSIDE DECLARED write_paths") {
		t.Fatalf("receipts block hides the violation: %q", block)
	}
}

// A writer that declared nothing is scheduled as whole-workspace, and the host
// deliberately enforces nothing inside the workspace for it. This test pins the
// real semantics so the SPEC claim stays honest.
func TestUndeclaredWriterHasNoIntraWorkspaceEnforcement(t *testing.T) {
	root := testenv.TempDir(t)
	whole, err := writeclaim.WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	if !whole.AllowsPath(filepath.Join(root, "any", "file.go")) {
		t.Fatal("a whole-workspace claim must allow every in-workspace path")
	}
	summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{filepath.Join(root, "package.json")}},
	}}
	if got := claimViolations(summary, whole); len(got) != 0 {
		t.Fatalf("violations = %v, want none: a whole-workspace claim cannot distinguish in-workspace writes", got)
	}
	// It still catches an escape out of the workspace entirely.
	outside := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{filepath.Join(testenv.TempDir(t), "elsewhere.go")}},
	}}
	if got := claimViolations(outside, whole); len(got) != 1 {
		t.Fatalf("violations = %v, want the out-of-workspace write flagged", got)
	}
}

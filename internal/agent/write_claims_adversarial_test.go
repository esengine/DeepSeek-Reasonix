package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// boundWriterFixture builds a workspace with an in-claim directory and an
// out-of-claim file at the root, plus a write_file bound to the claim.
func boundWriterFixture(t *testing.T) (root string, writer tool.Tool, inner *recordingWriter) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	inner = &recordingWriter{name: "write_file"}
	reg := tool.NewRegistry()
	reg.Add(inner)
	bound, _ := BindWritePaths(reg, claim, root, false)
	return root, mustGet(t, bound, "write_file"), inner
}

func mustRejectWrite(t *testing.T, writer tool.Tool, inner *recordingWriter, args string) {
	t.Helper()
	out, err := writer.Execute(context.Background(), json.RawMessage(args))
	if err == nil {
		t.Fatalf("write %s was allowed (result %q); it escapes the declared write_paths", args, out)
	}
	if !strings.Contains(err.Error(), "outside this subagent's declared write_paths") {
		t.Fatalf("unexpected rejection reason: %v", err)
	}
	if inner.calls != 0 {
		t.Fatalf("inner writer ran %d times; the boundary must reject before execution", inner.calls)
	}
}

// A symlink inside the claim that points out of it must not launder a write.
func TestWriteClaimBlocksSymlinkEscape(t *testing.T) {
	root, writer, inner := boundWriterFixture(t)
	link := filepath.Join(root, "auth", "link.json")
	if err := os.Symlink(filepath.Join(root, "package.json"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mustRejectWrite(t, writer, inner, `{"path":`+jsonPath(link)+`,"content":"x"}`)
}

func TestWriteClaimBlocksParentTraversal(t *testing.T) {
	root, writer, inner := boundWriterFixture(t)
	escape := filepath.Join(root, "auth", "..", "package.json")
	mustRejectWrite(t, writer, inner, `{"path":`+jsonPath(escape)+`,"content":"x"}`)
}

// move_file has two path arguments; a destination outside the claim is still an
// escape even when the source is legitimately inside it.
func TestWriteClaimChecksMoveDestination(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	inner := &recordingWriter{name: "move_file"}
	reg := tool.NewRegistry()
	reg.Add(inner)
	bound, _ := BindWritePaths(reg, claim, root, false)
	mover := mustGet(t, bound, "move_file")

	args := `{"source_path":` + jsonPath(filepath.Join(root, "auth", "a.go")) +
		`,"destination_path":` + jsonPath(filepath.Join(root, "escaped.go")) + `}`
	if _, err := mover.Execute(context.Background(), json.RawMessage(args)); err == nil {
		t.Fatal("move_file out of the claim was allowed")
	}
	if inner.calls != 0 {
		t.Fatalf("move_file ran %d times despite an out-of-claim destination", inner.calls)
	}
}

// A writer the host cannot path-scope is dropped, never silently trusted.
func TestWriteClaimDropsUnbindableWriters(t *testing.T) {
	root := t.TempDir()
	claim, err := NormalizeWritePaths(root, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	reg.Add(&recordingWriter{name: "deploy_release"})
	reg.Add(&recordingWriter{name: "read_notes", readOnly: true})
	bound, removed := BindWritePaths(reg, claim, root, false)
	if _, ok := bound.Get("deploy_release"); ok {
		t.Fatal("an unbindable writer survived the write_paths boundary")
	}
	if len(removed) != 1 || removed[0] != "deploy_release" {
		t.Fatalf("removed = %v, want [deploy_release]", removed)
	}
	if _, ok := bound.Get("read_notes"); !ok {
		t.Fatal("read-only tools must survive the boundary")
	}
}

// Layer 5: even if a write reaches the workspace through a surface the claim
// could not bind, the host reports it to the parent rather than staying silent.
func TestClaimViolationsSurfaceOutOfClaimMutations(t *testing.T) {
	root := t.TempDir()
	claim, err := NormalizeWritePaths(root, []string{"auth"})
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
	root := t.TempDir()
	whole, err := WholeWorkspaceWriteClaim(root)
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
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{filepath.Join(t.TempDir(), "elsewhere.go")}},
	}}
	if got := claimViolations(outside, whole); len(got) != 1 {
		t.Fatalf("violations = %v, want the out-of-workspace write flagged", got)
	}
}

func jsonPath(path string) string {
	b, _ := json.Marshal(path)
	return string(b)
}

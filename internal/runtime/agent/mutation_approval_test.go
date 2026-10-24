package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

func TestSharedMutationRejectsChangedApprovalPreview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("draft\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root}
	reg := tool.NewRegistry()
	reg.Add(ws.Tools("edit_file")[0])
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	gate := &approvalPreviewGate{allow: true, check: func() {
		lease, err := coordinator.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{path}})
		if err != nil {
			t.Fatal(err)
		}
		defer lease.End(mutation.Committed)
		if err := os.WriteFile(path, []byte("draft\nconcurrent contribution\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}}
	a := New(nil, reg, sessionstore.NewSession(""), Options{
		Gate: gate, CheckTargetAccess: ws.TargetAccessCheck(), MutationCoordinator: coordinator,
	}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "edit", Name: "edit_file",
		Arguments: `{"path":"note.txt","old_string":"draft","new_string":"ready"}`})
	if !out.blocked || out.refusalCode != "workspace.mutation_version_conflict" {
		t.Fatalf("outcome: %+v", out)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "draft\nconcurrent contribution\n" {
		t.Fatalf("stale approval mutated new content: %q, %v", got, err)
	}
	lease, err := coordinator.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	lease.End(mutation.NotExecuted)
}

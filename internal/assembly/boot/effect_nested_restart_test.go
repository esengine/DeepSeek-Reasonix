package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type nestedRestartProvider struct {
	nestedContinuationProvider
	resume string
	last   string
}

func (p *nestedRestartProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	parent := strings.Contains(firstUser(req), "NEST-CONT-PARENT") && !strings.Contains(firstUser(req), "NEST-CONT-LEAF")
	resume := false
	for _, message := range req.Messages {
		resume = resume || (message.Role == provider.RoleUser && strings.Contains(message.Content, "RESTART-RESUME"))
	}
	call := func(id, name string, args any) (<-chan provider.Chunk, error) {
		ch := make(chan provider.Chunk, 2)
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	if parent {
		if resume {
			if _, written := callResult(req, "parent-resumed"); !written {
				return call("parent-resumed", "write_file", map[string]string{"path": "resumed.txt", "content": "continued after rebuild\n"})
			}
			ch := make(chan provider.Chunk, 2)
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "continued retained parent"}
			ch <- provider.Chunk{Type: provider.ChunkDone}
			close(ch)
			return ch, nil
		}
		if _, written := callResult(req, "parent-original"); !written {
			return call("parent-original", "write_file", map[string]string{"path": "parent.txt", "content": "retained parent\n"})
		}
	} else if !strings.Contains(firstUser(req), "NEST-CONT-LEAF") && p.resume != "" {
		if _, called := callResult(req, "parent-after-rebuild"); !called {
			return call("parent-after-rebuild", "task", map[string]string{"prompt": "NEST-CONT-PARENT RESTART-RESUME", "continue_from": p.resume})
		}
		p.last = lastToolResult(req)
	}
	return p.nestedContinuationProvider.Stream(ctx, req)
}

func TestEffectInterruptedParentContinuesRetainedWorkAfterControllerRebuild(t *testing.T) {
	p := &nestedRestartProvider{nestedContinuationProvider: nestedContinuationProvider{first: func() error { return nil }}}
	opts := nestedExecutionOptions(t, p, "", "", event.Discard)
	ctrl, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	lease := bindPresenceSession(t, ctrl)
	t.Cleanup(ctrl.Close)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := ctrl.Run(t.Context(), "prepare nested work before rebuilding"); err != nil {
		t.Fatal(err)
	}
	catalog, err := ctrl.IsolationResults(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var parent, child isolation.ResultSummary
	for _, row := range catalog.Results {
		if row.Kind != "result" {
			continue
		}
		if row.ParentID == "" {
			parent = row
		} else {
			child = row
		}
	}
	if parent.ID == "" || child.ID == "" || parent.State != isolation.Interrupted {
		t.Fatalf("missing retained interrupted dependency graph: %+v", catalog)
	}
	store := delegation.NewSubagentStore(filepath.Join(ctrl.SessionDir(), "subagents"))
	artifacts, err := store.ListForParent(sessionstore.BranchID(ctrl.SessionPath()), opts.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.Meta.EnvironmentID == parent.ID {
			p.resume = artifact.Ref
		}
	}
	if p.resume == "" {
		t.Fatal("retained parent transcript cannot be discovered after failure")
	}
	preview, err := ctrl.PreviewIsolatedDiscard(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.DiscardIsolated(t.Context(), control.IsolatedDiscardRequest{ID: preview.ID, Revision: preview.Revision}); err != nil {
		t.Fatal(err)
	}
	path := ctrl.SessionPath()
	ctrl.Close()
	lease.Release()
	session, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	next, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Resume(session, path); err != nil {
		next.Close()
		t.Fatal(err)
	}
	bindPresenceSession(t, next)
	t.Cleanup(next.Close)
	next.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := next.Run(t.Context(), "continue retained parent after rebuild"); err != nil {
		t.Fatal(err)
	}
	catalog, err = next.IsolationResults(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range catalog.Results {
		if row.ID == parent.ID {
			found = row.Generation != parent.Generation && row.CandidateRoot == parent.CandidateRoot && row.State == isolation.Pending
		}
	}
	if !found {
		t.Fatalf("rebuilt runtime replaced or failed to continue its parent environment: %+v", catalog)
	}
	for name, want := range map[string]string{"parent.txt": "retained parent\n", "resumed.txt": "continued after rebuild\n"} {
		if raw, err := os.ReadFile(filepath.Join(parent.CandidateRoot, name)); err != nil || string(raw) != want {
			t.Fatalf("retained parent file %s: %q %v", name, raw, err)
		}
		if _, err := os.Stat(filepath.Join(opts.WorkspaceRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("unverified rebuild wrote the root source: %v", err)
		}
	}
}

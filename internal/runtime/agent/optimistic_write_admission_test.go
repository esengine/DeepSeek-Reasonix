package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/runtime/writeclaim"
)

type admissionNamedWriter struct {
	paths []string
	err   error
}

func (admissionNamedWriter) Name() string            { return "model_extra_named_writer" }
func (admissionNamedWriter) Description() string     { return "Write the supplied paths." }
func (admissionNamedWriter) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (admissionNamedWriter) ReadOnly() bool          { return false }
func (admissionNamedWriter) WritesNamedPaths() bool  { return true }
func (w admissionNamedWriter) WritePaths(json.RawMessage) ([]string, error) {
	return w.paths, w.err
}
func (admissionNamedWriter) Execute(context.Context, json.RawMessage) (string, error) {
	return "done", nil
}

func TestOptimisticNamedAdmissionUsesTheActorScopeAndReleases(t *testing.T) {
	root := testenv.TempDir(t)
	claim, err := writeclaim.OptimisticWorkspaceClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := writeclaim.WithSubagentWriteGrant(t.Context(), writeclaim.NewWriteGrant(claim))
	sched := writeclaim.NewSubagentScheduler(4, 4)
	a := &Agent{agentConfig: agentConfig{subagentDepth: 1}, svc: agentServices{writeScheduler: sched}}
	path := filepath.Join(root, "a.txt")
	writer := admissionNamedWriter{paths: []string{path}}
	release, err := a.reserveToolWrite(ctx, writer, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := writeclaim.NormalizeWritePaths(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if other, err := sched.ReserveWrite(reservation); !errors.Is(err, writeclaim.ErrWritePathBusy) {
		if other != nil {
			other()
		}
		t.Fatalf("live call was not exclusive: %v", err)
	}
	release()
	other, err := sched.ReserveWrite(reservation)
	if err != nil {
		t.Fatalf("reservation survived the call while its actor context remained live: %v", err)
	}
	other()
	if a.writeWorkspaceRoot != "" || a.observeRoot != "" {
		t.Fatal("claim admission enabled a workspace observation root")
	}
}

func TestOptimisticNamedAdmissionAcquiresTheWholeSetOrNothing(t *testing.T) {
	root := testenv.TempDir(t)
	claim, err := writeclaim.OptimisticWorkspaceClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := writeclaim.WithSubagentWriteGrant(t.Context(), writeclaim.NewWriteGrant(claim))
	sched := writeclaim.NewSubagentScheduler(4, 4)
	a := &Agent{agentConfig: agentConfig{subagentDepth: 1}, svc: agentServices{writeScheduler: sched}}
	aPath, bPath := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
	bClaim, err := writeclaim.NormalizeWritePaths(root, []string{bPath})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sched.ReserveWrite(bClaim)
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	if release, err := a.reserveToolWrite(ctx, admissionNamedWriter{paths: []string{aPath, bPath}}, nil, false, nil); !errors.Is(err, writeclaim.ErrWritePathBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("overlapping set accepted: %v", err)
	}
	aClaim, err := writeclaim.NormalizeWritePaths(root, []string{aPath})
	if err != nil {
		t.Fatal(err)
	}
	free, err := sched.ReserveWrite(aClaim)
	if err != nil {
		t.Fatalf("refused multi-path call retained its disjoint prefix: %v", err)
	}
	free()
}

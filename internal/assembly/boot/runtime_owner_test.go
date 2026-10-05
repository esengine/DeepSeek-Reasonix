package boot

import (
	"context"
	"testing"

	"reasonix/internal/ext/extension"
	"reasonix/internal/session/control"
)

func TestIndependentBuildRuntimeOwnersRemainActive(t *testing.T) {
	isolateConfigHome(t)
	first, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		first.Controller.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		first.Controller.Close()
		second.Controller.Close()
	})

	if first.Owner == nil || second.Owner == nil || first.Owner == second.Owner {
		t.Fatal("independent builds must own independent runtime lifecycle state")
	}
	if first.Controller.RuntimePhase() != control.RuntimePhaseActive {
		t.Fatalf("first phase = %s", first.Controller.RuntimePhase())
	}
	if second.Controller.RuntimePhase() != control.RuntimePhaseActive {
		t.Fatalf("second phase = %s", second.Controller.RuntimePhase())
	}
	if first.Owner.Gate.Published() != first.Snapshot.Generation() {
		t.Fatal("first owner lost its published generation after sibling build")
	}
	if second.Owner.Gate.Published() != second.Snapshot.Generation() {
		t.Fatal("second owner did not publish its own generation")
	}
}

func TestDeferredBuildPublishesOnlyAfterCommit(t *testing.T) {
	isolateConfigHome(t)
	owner := extension.NewRuntimeOwner()
	res, err := BuildRuntime(context.Background(), Options{
		RuntimeReload: RuntimeReload{Owner: owner},
		deferPublish:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(res.Controller.Close)

	if got := owner.Gate.Published(); got != 0 {
		t.Fatalf("replacement published before migration/commit: %d", got)
	}
	if phase := res.Controller.RuntimePhase(); phase != control.RuntimePhaseUnknown {
		t.Fatalf("unpublished replacement phase = %s", phase)
	}
	publishBuildResult(res)
	if got := owner.Gate.Published(); got != res.Snapshot.Generation() {
		t.Fatalf("published = %d, want %d", got, res.Snapshot.Generation())
	}
}

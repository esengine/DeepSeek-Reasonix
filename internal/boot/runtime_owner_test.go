package boot

import (
	"context"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/extension"
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

func TestRebuildDrainsOnlySameRuntimeOwner(t *testing.T) {
	isolateConfigHome(t)
	lineage, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		lineage.Controller.Close()
		t.Fatal(err)
	}
	oldGen := lineage.Snapshot.Generation()
	siblingGen := sibling.Snapshot.Generation()
	rebuilt, err := RebuildFrom(context.Background(), lineage, Options{})
	if err != nil {
		lineage.Controller.Close()
		sibling.Controller.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rebuilt.Controller.Close()
		sibling.Controller.Close()
	})

	if rebuilt.Owner != lineage.Owner {
		t.Fatal("same-session rebuild must reuse the previous runtime owner")
	}
	if rebuilt.Snapshot.Generation() != oldGen {
		if !rebuilt.Owner.Gate.IsDraining(oldGen) || rebuilt.Owner.Gate.AdmitNewWork(oldGen) {
			t.Fatal("rebuild must drain only its previous generation")
		}
	}
	if sibling.Owner.Gate.Published() != siblingGen || sibling.Controller.RuntimePhase() != control.RuntimePhaseActive {
		t.Fatal("rebuilding one lineage must not drain an independent session")
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

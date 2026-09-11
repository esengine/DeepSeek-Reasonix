package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/extension"
	"reasonix/internal/extensioncontract"
)

func TestRebuildFromNoOpPreservesCacheHash(t *testing.T) {
	isolateConfigHome(t)
	oldRes, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(func() {
		if oldRes.Controller != nil {
			oldRes.Controller.Close()
		}
	})
	if oldRes.Assembly == nil {
		t.Fatal("expected Assembly retained for reuse")
	}
	res, err := RebuildFrom(context.Background(), oldRes, Options{})
	if err != nil {
		t.Fatalf("RebuildFrom: %v", err)
	}
	t.Cleanup(func() {
		if res.Controller != nil {
			res.Controller.Close()
		}
	})
	if oldRes.Snapshot != nil && res.Snapshot != nil {
		if oldRes.Snapshot.CacheHash() != res.Snapshot.CacheHash() {
			t.Fatalf("cache hash churned on no-op: %s -> %s", oldRes.Snapshot.CacheHash(), res.Snapshot.CacheHash())
		}
	}
	if res.Plan != nil && res.Plan.PrefixChanged {
		t.Fatal("no-op plan should not set PrefixChanged")
	}
}

func TestPublishGateAdmissionAfterRebuild(t *testing.T) {
	isolateConfigHome(t)
	oldRes, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if oldRes.Controller != nil {
			oldRes.Controller.Close()
		}
	})
	oldGen := uint64(0)
	if oldRes.Snapshot != nil {
		oldGen = oldRes.Snapshot.Generation()
		oldRes.Controller.SetRuntimeGeneration(oldGen)
	}
	res, err := RebuildFrom(context.Background(), oldRes, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if res.Controller != nil {
			res.Controller.Close()
		}
	})
	// Old generation must not admit new work after publish of the new one.
	if oldGen != 0 && oldRes.Owner.Gate.AdmitNewWork(oldGen) {
		// Only fails if new gen was published and differs.
		if res.Snapshot != nil && res.Snapshot.Generation() != oldGen {
			t.Fatal("old generation still admits after rebuild publish")
		}
	}
	if res.Controller.RuntimePhase() != "Active" && res.Controller.RuntimePhase() != "Unknown" {
		// Active when generation is published.
		if res.Snapshot != nil && res.Snapshot.Generation() != 0 && res.Controller.RuntimePhase() != "Active" {
			t.Fatalf("new controller phase = %s", res.Controller.RuntimePhase())
		}
	}
}

func TestDoctorRuntimeReport(t *testing.T) {
	before := extension.RuntimeOwnerFallbackCount()
	_ = extension.RuntimeOwnerFromContext(context.Background())
	report := CollectRuntimeDoctor(nil)
	if report.RuntimeOwnerFallbacks <= before {
		t.Fatalf("runtime owner fallbacks = %d, want greater than %d", report.RuntimeOwnerFallbacks, before)
	}
	text := RenderRuntimeDoctorText(report)
	if !strings.Contains(text, "runtime owner fallbacks:") || (!strings.Contains(text, "metrics:") && !strings.Contains(text, "recoverability")) {
		t.Fatalf("unexpected doctor text:\n%s", text)
	}
	if _, err := RenderRuntimeDoctorJSON(report); err != nil {
		t.Fatal(err)
	}
}

func TestPlanClassifyIntegration(t *testing.T) {
	from, err := extension.BuildDependencyGraph([]extension.ComponentDescriptor{
		{ID: "plugin/a", Provides: []extensioncontract.Capability{{
			Key:     extensioncontract.CapabilityKey{Namespace: "plugin/a", Kind: "ui", ID: "x"},
			Version: "1.0.0", SchemaHash: "sha256:a",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := extension.BuildDependencyGraph([]extension.ComponentDescriptor{
		{ID: "plugin/a", Provides: []extensioncontract.Capability{{
			Key:     extensioncontract.CapabilityKey{Namespace: "plugin/a", Kind: "ui", ID: "x"},
			Version: "1.0.1", SchemaHash: "sha256:b",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := extension.DiffRuntimePlan(from, to, 1, 2)
	if plan.Kind != extension.SubgraphUIOnly {
		t.Fatalf("kind = %v", plan.Kind)
	}
	if plan.MayChangePrefix() {
		t.Fatal("UI-only must not affect cache")
	}
}

func TestRapidReloadPreservesAdmission(t *testing.T) {
	isolateConfigHome(t)
	var prev *BuildResult
	for i := range 3 {
		var res *BuildResult
		var err error
		if prev == nil {
			res, err = BuildRuntime(context.Background(), Options{})
		} else {
			res, err = RebuildFrom(context.Background(), prev, Options{})
		}
		if err != nil {
			t.Fatalf("reload %d: %v", i, err)
		}
		t.Cleanup(func() {
			if res.Controller != nil {
				res.Controller.Close()
			}
		})
		if res.Snapshot != nil {
			gen := res.Snapshot.Generation()
			if !res.Owner.Gate.AdmitNewWork(gen) {
				t.Fatalf("reload %d: gen %d not admitted", i, gen)
			}
		}
		if prev != nil && prev.Snapshot != nil && res.Snapshot != nil {
			if prev.Snapshot.Generation() != res.Snapshot.Generation() {
				if res.Owner.Gate.AdmitNewWork(prev.Snapshot.Generation()) {
					t.Fatalf("reload %d: old gen still admits", i)
				}
			}
		}
		prev = res
	}
}

func TestProviderDrainReloadClassify(t *testing.T) {
	from, err := extension.BuildDependencyGraph([]extension.ComponentDescriptor{
		{ID: "plugin/p", Provides: []extensioncontract.Capability{{
			Key:        extensioncontract.CapabilityKey{Namespace: "plugin/p", Kind: "provider", ID: "main"},
			Version:    "1.0.0",
			SchemaHash: "sha256:p1",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := extension.BuildDependencyGraph([]extension.ComponentDescriptor{
		{ID: "plugin/p", Provides: []extensioncontract.Capability{{
			Key:        extensioncontract.CapabilityKey{Namespace: "plugin/p", Kind: "provider", ID: "main"},
			Version:    "1.0.1",
			SchemaHash: "sha256:p2",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := extension.DiffRuntimePlan(from, to, 1, 2)
	if plan.Kind != extension.SubgraphProviderOnly {
		t.Fatalf("kind = %v want provider-only", plan.Kind)
	}
	if !plan.MayChangePrefix() {
		t.Fatal("provider change must conservatively check the prefix")
	}
	if !plan.ProviderChanged || plan.PrefixChanged {
		t.Fatalf("provider plan flags = provider:%v prefix:%v", plan.ProviderChanged, plan.PrefixChanged)
	}
	if !shouldReuseDiscovery(plan) {
		t.Fatal("provider-only should reuse skill/command/hook discovery")
	}
}

func TestActivationFailureDoesNotPublish(t *testing.T) {
	// Synthetic: BeginDrain without Publish must leave published gate unchanged
	// and never admit the failed generation.
	gate := extension.NewPublishGate()
	gate.Publish(1)
	gate.BeginDrain(99) // failed activation of gen 99
	if gate.Published() != 1 {
		t.Fatalf("published = %d", gate.Published())
	}
	if gate.AdmitNewWork(99) {
		t.Fatal("failed activation gen must not admit")
	}
	if !gate.IsDraining(99) {
		t.Fatal("failed gen should be marked draining for cleanup")
	}
}

func TestResumePolicyInDoctor(t *testing.T) {
	extension.RecordMessageSent(42, "m1", "test")
	report := CollectRuntimeDoctor(nil)
	// Force assess on gen 42 via DecideResume
	d := extension.DecideResumeDefault(42)
	if d.CleanRollback {
		t.Fatal("message send must block clean rollback")
	}
	if !d.AllowResume {
		t.Fatal("must still allow resume")
	}
	_ = report
}

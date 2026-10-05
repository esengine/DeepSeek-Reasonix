package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extensioncontract"
)

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

	d := extension.DecideResume(extension.RuntimeOwnerOrDefault(nil).Receipts, 42)
	if d.CleanRollback {
		t.Fatal("message send must block clean rollback")
	}
	if !d.AllowResume {
		t.Fatal("must still allow resume")
	}
	_ = report
}

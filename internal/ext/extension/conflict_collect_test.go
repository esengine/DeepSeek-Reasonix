package extension

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestConflictCollectRecordsAndResolves: under ConflictCollect a same-tier
// multi-source dispute is recorded on the snapshot diagnostics while the
// ordinary winner rules still resolve it, and the build succeeds.
func TestConflictCollectRecordsAndResolves(t *testing.T) {
	b := NewBuilder().WithConflictPolicy(ConflictCollect)
	b.AddContributor(
		staticContributor("a", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pa", "plugin"), Payload: "from-a"}),
		staticContributor("b", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pb", "plugin"), Payload: "from-b"}),
	)
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("ConflictCollect Build failed: %v", err)
	}
	winners := snap.Catalog().Get(KindCommand, "deploy")
	if len(winners) != 1 {
		t.Fatalf("catalog holds %d deploy commands, want 1 winner", len(winners))
	}
	if winners[0].Payload != "from-a" {
		t.Fatalf("winner = %v, want the first-registered contribution", winners[0].Payload)
	}
	diags := snap.Diagnostics()
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %v, want exactly one conflict entry", diags)
	}
	for _, want := range []string{"command", `"deploy"`, "pa", "pb"} {
		if !strings.Contains(diags[0], want) {
			t.Fatalf("diagnostic %q must contain %q (kind, canonical ID, both sources)", diags[0], want)
		}
	}
}

// TestConflictFailRemainsDefault: the same dispute stays a hard ConflictError
// without the collect opt-in.
func TestConflictFailRemainsDefault(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(
		staticContributor("a", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pa", "plugin"), Payload: "a"}),
		staticContributor("b", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pb", "plugin"), Payload: "b"}),
	)
	_, _, err := b.Build(context.Background())
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Build error = %v, want *ConflictError", err)
	}
}

// TestConflictCollectCrossTierIsNotADispute: shadowing across tiers is the
// ordinary winner rule, not a conflict — nothing lands on diagnostics.
func TestConflictCollectCrossTierIsNotADispute(t *testing.T) {
	b := NewBuilder().WithConflictPolicy(ConflictCollect)
	b.AddContributor(
		staticContributor("plugin", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pa", "plugin"), Payload: "from-plugin"}),
		staticContributor("project", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopeProject, "", "project"), Payload: "from-project"}),
	)
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	winners := snap.Catalog().Get(KindCommand, "deploy")
	if len(winners) != 1 || winners[0].Payload != "from-project" {
		t.Fatalf("winner = %+v, want the project-tier contribution", winners)
	}
	if diags := snap.Diagnostics(); len(diags) != 0 {
		t.Fatalf("cross-tier shadowing recorded diagnostics: %v", diags)
	}
}

// TestConflictCollectAdditiveKindsNeverConflict: hooks accumulate; an
// identical ID from two sources is not a dispute.
func TestConflictCollectAdditiveKindsNeverConflict(t *testing.T) {
	b := NewBuilder().WithConflictPolicy(ConflictCollect)
	b.AddContributor(
		staticContributor("a", Contribution{Kind: KindHook, ID: "PreToolUse#0", Source: src(ScopeProject, "", "project"), Payload: "hook-a"}),
		staticContributor("b", Contribution{Kind: KindHook, ID: "PreToolUse#0", Source: src(ScopeGlobal, "", "global"), Payload: "hook-b"}),
	)
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if got := snap.Catalog().Get(KindHook, "PreToolUse#0"); len(got) != 2 {
		t.Fatalf("additive hooks = %d, want both surviving", len(got))
	}
	if diags := snap.Diagnostics(); len(diags) != 0 {
		t.Fatalf("additive kinds recorded diagnostics: %v", diags)
	}
}

// TestConflictCollectSameSourceCollapses: duplicates from a single source
// resolve to the first registration without a diagnostic, mirroring today's
// first-root-wins discovery.
func TestConflictCollectSameSourceCollapses(t *testing.T) {
	b := NewBuilder().WithConflictPolicy(ConflictCollect)
	b.AddContributor(staticContributor("a",
		Contribution{Kind: KindSkill, ID: "review", Source: src(ScopeProject, "", "project"), Payload: "first"},
		Contribution{Kind: KindSkill, ID: "review", Source: src(ScopeProject, "", "project"), Payload: "second"},
	))
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	winners := snap.Catalog().Get(KindSkill, "review")
	if len(winners) != 1 || winners[0].Payload != "first" {
		t.Fatalf("winner = %+v, want the first registration", winners)
	}
	if diags := snap.Diagnostics(); len(diags) != 0 {
		t.Fatalf("same-source duplicates recorded diagnostics: %v", diags)
	}
}

// TestConflictCollectMultipleDisputesAllRecorded: every disputed ID lands on
// diagnostics in one pass, and each winner still resolves (here into the
// provider-visible tool schemas).
func TestConflictCollectMultipleDisputesAllRecorded(t *testing.T) {
	b := NewBuilder().WithConflictPolicy(ConflictCollect)
	b.AddContributor(
		staticContributor("a",
			Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopePlugin, "pa", "plugin"), Payload: schemaPayload("read_file", "a read")},
			Contribution{Kind: KindTool, ID: "write_file", Source: src(ScopePlugin, "pa", "plugin"), Payload: schemaPayload("write_file", "a write")},
		),
		staticContributor("b",
			Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopePlugin, "pb", "plugin"), Payload: schemaPayload("read_file", "b read")},
			Contribution{Kind: KindTool, ID: "write_file", Source: src(ScopePlugin, "pb", "plugin"), Payload: schemaPayload("write_file", "b write")},
		),
	)
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("ConflictCollect Build failed: %v", err)
	}
	if diags := snap.Diagnostics(); len(diags) != 2 {
		t.Fatalf("diagnostics = %v, want one entry per disputed tool", diags)
	}
	// Contributor "a" registered first at the top tier, so its schemas win.
	for _, s := range snap.ToolSchemas() {
		if !strings.HasPrefix(s.Description, "a ") {
			t.Fatalf("tool %s winner description = %q, want contributor a's schema", s.Name, s.Description)
		}
	}
}

// TestConflictCollectSlotConflictStillFails: replacement-slot disputes are
// not shadowing, so ConflictCollect does not downgrade them.
func TestConflictCollectSlotConflictStillFails(t *testing.T) {
	b := NewBuilder().WithConflictPolicy(ConflictCollect)
	b.AddContributor(
		staticContributor("a", Contribution{
			Kind: KindStrategy, ID: "strat-a",
			Source:  src(ScopePlugin, "pa", "plugin"),
			Payload: claimPayload{body: "a", slots: []Slot{SlotSystemPrompt}},
		}),
		staticContributor("b", Contribution{
			Kind: KindStrategy, ID: "strat-b",
			Source:  src(ScopePlugin, "pb", "plugin"),
			Payload: claimPayload{body: "b", slots: []Slot{SlotSystemPrompt}},
		}),
	)
	_, _, err := b.Build(context.Background())
	if err == nil {
		t.Fatal("ConflictCollect Build succeeded, want SlotConflictError")
	}
	var slotConflict *SlotConflictError
	if !errors.As(err, &slotConflict) {
		t.Fatalf("Build error = %v, want *SlotConflictError", err)
	}
}

// TestSnapshotDiagnosticsImmutable: the accessor returns a copy.
func TestSnapshotDiagnosticsImmutable(t *testing.T) {
	b := NewBuilder().WithConflictPolicy(ConflictCollect)
	b.AddContributor(
		staticContributor("a", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pa", "plugin"), Payload: "a"}),
		staticContributor("b", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pb", "plugin"), Payload: "b"}),
	)
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	diags := snap.Diagnostics()
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %v, want one entry", diags)
	}
	diags[0] = "mutated"
	if snap.Diagnostics()[0] == "mutated" {
		t.Fatal("Diagnostics returned the snapshot's internal slice")
	}
}

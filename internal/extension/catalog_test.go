package extension

import (
	"testing"

	"reasonix/internal/provider"
)

func src(scope Scope, pluginID, origin string) ContributionSource {
	return ContributionSource{Scope: scope, PluginID: pluginID, Origin: origin}
}

func schemaPayload(name, desc string) provider.ToolSchema {
	return provider.ToolSchema{Name: name, Description: desc, Parameters: []byte(`{"type":"object"}`)}
}

// TestCatalogSortOrder pins the documented iteration order: kind, then tier
// (highest first), priority (highest first), plugin ID, ID, order. The order
// is what makes catalog listings stable regardless of contributor
// registration order.
func TestCatalogSortOrder(t *testing.T) {
	c := NewCatalog()
	c.Add(
		Contribution{Kind: KindTool, ID: "write_file", Source: src(ScopeBuiltin, "", "builtin"), Order: 0},
		Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopeGlobal, "", "user"), Order: 0},
		Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopeProject, "", "project"), Priority: 0, Order: 0},
		Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopeBuiltin, "", "builtin"), Priority: 5, Order: 1},
		Contribution{Kind: KindCommand, ID: "pkg:build", Source: src(ScopePlugin, "pkg", "plugin"), Order: 1},
	)
	all := c.All()
	want := []struct {
		kind ContributionKind
		id   string
	}{
		{KindCommand, "pkg:build"}, // plugin tier outranks global tier
		{KindCommand, "deploy"},    // global tier
		{KindTool, "read_file"},    // project tier first within kind
		{KindTool, "read_file"},    // builtin tier next
		{KindTool, "write_file"},   // same tier, read_file < write_file
	}
	if len(all) != len(want) {
		t.Fatalf("All() returned %d entries, want %d", len(all), len(want))
	}
	for i, w := range want {
		if all[i].Kind != w.kind || all[i].ID != w.id {
			t.Fatalf("position %d: got (%s, %s), want (%s, %s)", i, all[i].Kind, all[i].ID, w.kind, w.id)
		}
	}
	if all[2].Source.Scope != ScopeProject || all[3].Source.Scope != ScopeBuiltin {
		t.Fatalf("tier order wrong: got %s then %s", all[2].Source.Scope, all[3].Source.Scope)
	}
}

// TestCatalogAccessors copies: mutating a returned slice must not leak back
// into the catalog — snapshots share catalogs, so this is part of snapshot
// immutability.
func TestCatalogAccessorCopies(t *testing.T) {
	c := NewCatalog()
	c.Add(
		Contribution{Kind: KindTool, ID: "a", Source: src(ScopeBuiltin, "", "builtin")},
		Contribution{Kind: KindHook, ID: "PreToolUse#0", Source: src(ScopeProject, "", "project")},
	)
	all := c.All()
	all[0].ID = "mutated"
	if c.All()[0].ID == "mutated" {
		t.Fatal("mutating All() result changed the catalog")
	}
	byKind := c.ByKind(KindTool)
	byKind[0].ID = "mutated"
	if c.ByKind(KindTool)[0].ID != "a" {
		t.Fatal("mutating ByKind() result changed the catalog")
	}
	if got := c.Get(KindTool, "a"); len(got) != 1 {
		t.Fatalf("Get(tool, a) = %d entries, want 1", len(got))
	}
	if got := c.Get(KindTool, "missing"); len(got) != 0 {
		t.Fatalf("Get(tool, missing) = %d entries, want 0", len(got))
	}
}

// TestCatalogFrozenPanics: adding to a frozen catalog is a wiring bug and
// must fail loudly, never silently mutate a published snapshot.
func TestCatalogFrozenPanics(t *testing.T) {
	c := NewCatalog()
	c.Add(Contribution{Kind: KindTool, ID: "a", Source: src(ScopeBuiltin, "", "builtin")})
	c.freeze()
	if !c.Frozen() {
		t.Fatal("Frozen() = false after freeze")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Add on frozen catalog did not panic")
		}
	}()
	c.Add(Contribution{Kind: KindTool, ID: "b", Source: src(ScopeBuiltin, "", "builtin")})
}

// TestCatalogConflicts covers the reporting half of the conflict rules:
// same-tier duplicates from different sources show up, cross-tier duplicates
// and additive kinds never do.
func TestCatalogConflicts(t *testing.T) {
	c := NewCatalog()
	c.Add(
		// Same command, same plugin tier, two plugins: conflict.
		Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pa", "plugin"), Order: 0},
		Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pb", "plugin"), Order: 0},
		// Same tool, project vs builtin: shadowing, not a conflict.
		Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopeProject, "", "project"), Order: 0},
		Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopeBuiltin, "", "builtin"), Order: 1},
		// Hooks are additive even at the same tier from different sources.
		Contribution{Kind: KindHook, ID: "PreToolUse#0", Source: src(ScopeProject, "", "project"), Order: 0},
		Contribution{Kind: KindHook, ID: "PreToolUse#0", Source: src(ScopeGlobal, "", "global"), Order: 0},
		// Same source contributing one ID twice: one source, no conflict.
		Contribution{Kind: KindSkill, ID: "review", Source: src(ScopeGlobal, "", "user"), Order: 0},
		Contribution{Kind: KindSkill, ID: "review", Source: src(ScopeGlobal, "", "user"), Order: 1},
	)
	conflicts := c.Conflicts()
	if len(conflicts) != 1 {
		t.Fatalf("Conflicts() = %v, want exactly the deploy conflict", conflicts)
	}
	got := conflicts[0]
	if got.Kind != KindCommand || got.ID != "deploy" {
		t.Fatalf("conflict = (%s, %s), want (command, deploy)", got.Kind, got.ID)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("conflict sources = %v, want both plugins", got.Sources)
	}
	ids := map[string]bool{}
	for _, s := range got.Sources {
		ids[s.PluginID] = true
	}
	if !ids["pa"] || !ids["pb"] {
		t.Fatalf("conflict sources missing plugin IDs: %v", got.Sources)
	}
}

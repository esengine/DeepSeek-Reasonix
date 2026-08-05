package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"testing"

	"reasonix/internal/provider"
)

// hashTestSnapshot builds a snapshot with two tools, an interceptor, and a
// claimed slot so every accessor has content to defend.
func hashTestSnapshot(t *testing.T, prompt string, readDesc string) *RuntimeSnapshot {
	t.Helper()
	b := NewBuilder().WithSystemPrompt(prompt).WithGeneration(3)
	b.AddContributor(staticContributor("mix",
		Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopeBuiltin, "", "builtin"), Payload: schemaPayload("read_file", readDesc)},
		Contribution{Kind: KindTool, ID: "bash", Source: src(ScopeBuiltin, "", "builtin"), Payload: schemaPayload("bash", "run commands")},
		Contribution{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: 0, Source: src(ScopePlugin, "pa", "plugin"), Payload: "i"},
		Contribution{Kind: KindStrategy, ID: "strat", Source: src(ScopePlugin, "pa", "plugin"), Payload: claimPayload{slots: []Slot{SlotContext}}},
	))
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	return snap
}

// TestSnapshotImmutable: a snapshot is shared across turns and frontends;
// every accessor must return copies so no consumer can edit another
// consumer's view.
func TestSnapshotImmutable(t *testing.T) {
	snap := hashTestSnapshot(t, "prompt", "read files")

	schemas := snap.ToolSchemas()
	originalDesc := schemas[0].Description
	schemas[0].Description = "mutated"
	if snap.ToolSchemas()[0].Description != originalDesc {
		t.Fatal("mutating ToolSchemas() result changed the snapshot")
	}

	chains := snap.InterceptorChain()
	chains[PointToolBefore][0].Priority = -999
	delete(chains, PointToolBefore)
	if len(snap.InterceptorChain()) != 1 {
		t.Fatal("deleting from InterceptorChain() result changed the snapshot")
	}
	if snap.InterceptorChain()[PointToolBefore][0].Priority != 0 {
		t.Fatal("mutating a chained contribution changed the snapshot")
	}

	repl := snap.Replacements()
	repl[SlotSystemPrompt] = src(ScopePlugin, "evil", "plugin")
	delete(repl, SlotContext)
	if len(snap.Replacements()) != 1 {
		t.Fatal("mutating Replacements() result changed the snapshot")
	}
	if _, ok := snap.Replacements()[SlotSystemPrompt]; ok {
		t.Fatal("injected a slot into the snapshot via the returned map")
	}

	// The frozen catalog refuses growth.
	defer func() {
		if recover() == nil {
			t.Fatal("Add on snapshot catalog did not panic")
		}
	}()
	snap.Catalog().Add(Contribution{Kind: KindTool, ID: "evil", Source: src(ScopeBuiltin, "", "builtin")})
}

// canonicalCacheHash recomputes the documented hash form independently:
// sha256-hex over the JSON of {"systemPrompt":..., "toolSchemas":[...]} with
// schemas sorted by (name, description, parameters). The test must match the
// implementation without calling it, so a drift in either is caught.
func canonicalCacheHash(t *testing.T, prompt string, schemas []provider.ToolSchema) (systemHash, toolsHash, cacheHash string) {
	t.Helper()
	sorted := make([]provider.ToolSchema, len(schemas))
	copy(sorted, schemas)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		if sorted[i].Description != sorted[j].Description {
			return sorted[i].Description < sorted[j].Description
		}
		return string(sorted[i].Parameters) < string(sorted[j].Parameters)
	})
	sum := sha256.Sum256([]byte(prompt))
	systemHash = hex.EncodeToString(sum[:])
	toolsJSON, err := json.Marshal(sorted)
	if err != nil {
		t.Fatalf("marshal schemas: %v", err)
	}
	sum = sha256.Sum256(toolsJSON)
	toolsHash = hex.EncodeToString(sum[:])
	combined, err := json.Marshal(struct {
		SystemPrompt string                `json:"systemPrompt"`
		ToolSchemas  []provider.ToolSchema `json:"toolSchemas"`
	}{SystemPrompt: prompt, ToolSchemas: sorted})
	if err != nil {
		t.Fatalf("marshal combined: %v", err)
	}
	sum = sha256.Sum256(combined)
	cacheHash = hex.EncodeToString(sum[:])
	return systemHash, toolsHash, cacheHash
}

// TestCacheHashCanonicalForm: CacheHash is a protocol — other tooling
// recomputes it to detect prefix drift — so it must equal the independently
// specified canonical form, byte for byte.
func TestCacheHashCanonicalForm(t *testing.T) {
	snap := hashTestSnapshot(t, "prompt v1", "read files")
	wantSystem, wantTools, wantCache := canonicalCacheHash(t, snap.SystemPrompt(), snap.ToolSchemas())
	if snap.CacheHash() != wantCache {
		t.Fatalf("CacheHash = %s, want %s", snap.CacheHash(), wantCache)
	}
	gotSystem, gotTools := snap.CacheShape()
	if gotSystem != wantSystem || gotTools != wantTools {
		t.Fatalf("CacheShape = (%s, %s), want (%s, %s)", gotSystem, gotTools, wantSystem, wantTools)
	}
}

// TestCacheHashStability: identical inputs hash identically (the provider
// cache survives a snapshot rebuild), and any provider-visible change moves
// the hash.
func TestCacheHashStability(t *testing.T) {
	a := hashTestSnapshot(t, "prompt v1", "read files")
	b := hashTestSnapshot(t, "prompt v1", "read files")
	if a.CacheHash() != b.CacheHash() {
		t.Fatal("identical builds produced different CacheHash")
	}

	changed := hashTestSnapshot(t, "prompt v1", "read files thoroughly")
	if changed.CacheHash() == a.CacheHash() {
		t.Fatal("changing one schema description did not change CacheHash")
	}
	_, aTools := a.CacheShape()
	changedSystem, changedTools := changed.CacheShape()
	if changedTools == aTools {
		t.Fatal("changing one schema description did not change the tools hash")
	}
	if aSys, _ := a.CacheShape(); changedSystem != aSys {
		t.Fatal("a tools-only change moved the system hash")
	}

	changedPrompt := hashTestSnapshot(t, "prompt v2", "read files")
	if changedPrompt.CacheHash() == a.CacheHash() {
		t.Fatal("changing the system prompt did not change CacheHash")
	}
}

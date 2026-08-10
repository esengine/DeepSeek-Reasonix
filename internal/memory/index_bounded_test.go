package memory

import (
	"strings"
	"testing"
	"time"
)

// TestIndexBoundedOrdersByRecency verifies the prefix index projection orders
// active facts by most-recently-updated first.
func TestIndexBoundedOrdersByRecency(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "oldest", Description: "old", Type: TypeProject, UpdatedAt: now.Add(-90 * 24 * time.Hour)},
		{Name: "newest", Description: "new", Type: TypeProject, UpdatedAt: now.Add(-2 * 24 * time.Hour)},
		{Name: "middle", Description: "mid", Type: TypeProject, UpdatedAt: now.Add(-30 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, defaultPrefixIndexMaxChars)
	pos := map[string]int{}
	for _, name := range []string{"newest", "middle", "oldest"} {
		pos[name] = strings.Index(got, "(project/"+name+".md)")
		if pos[name] < 0 {
			t.Fatalf("index missing %q:\n%s", name, got)
		}
	}
	if !(pos["newest"] < pos["middle"] && pos["middle"] < pos["oldest"]) {
		t.Fatalf("recency order wrong: %v\n%s", pos, got)
	}
	if strings.Contains(got, "more facts") || strings.Contains(got, "stale facts") {
		t.Fatalf("unexpected fold line:\n%s", got)
	}
}

// TestIndexBoundedFoldsStale verifies stale facts are hidden from the prefix
// projection and counted on a summary line instead of occupying budget.
func TestIndexBoundedFoldsStale(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "stale-one", Description: "old", Type: TypeProject, UpdatedAt: now.Add(-400 * 24 * time.Hour)}, // >180d → stale
		{Name: "stale-two", Description: "older", Type: TypeProject, UpdatedAt: now.Add(-300 * 24 * time.Hour)},
		{Name: "active", Description: "new", Type: TypeProject, UpdatedAt: now.Add(-1 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, defaultPrefixIndexMaxChars)
	if strings.Contains(got, "stale-one") || strings.Contains(got, "stale-two") {
		t.Fatalf("stale facts leaked into index:\n%s", got)
	}
	if !strings.Contains(got, "2 stale facts hidden") {
		t.Fatalf("missing stale fold line:\n%s", got)
	}
	if !strings.Contains(got, "active") {
		t.Fatalf("active fact missing:\n%s", got)
	}
}

// TestIndexBoundedAllStale verifies a fully stale set renders only the summary
// line, never an empty block.
func TestIndexBoundedAllStale(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "a", Description: "old", Type: TypeProject, UpdatedAt: now.Add(-400 * 24 * time.Hour)},
		{Name: "b", Description: "older", Type: TypeProject, UpdatedAt: now.Add(-300 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, defaultPrefixIndexMaxChars)
	if strings.Contains(got, ".md)") {
		t.Fatalf("stale index lines rendered:\n%s", got)
	}
	if !strings.Contains(got, "2 stale facts hidden") {
		t.Fatalf("missing stale fold line:\n%s", got)
	}
}

// TestIndexBoundedTruncatesByBudget verifies the character budget drops the
// tail while always keeping at least one line, and reports the omitted count.
func TestIndexBoundedTruncatesByBudget(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "first", Description: "newest fact", Type: TypeProject, UpdatedAt: now.Add(-1 * 24 * time.Hour)},
		{Name: "second", Description: "another fact", Type: TypeProject, UpdatedAt: now.Add(-2 * 24 * time.Hour)},
		{Name: "third", Description: "yet another", Type: TypeProject, UpdatedAt: now.Add(-3 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, 40) // room for roughly one line
	lines := strings.Count(got, "\n")
	if lines != 2 { // one index line + fold line
		t.Fatalf("expected 1 kept line + fold, got %d lines:\n%s", lines, got)
	}
	if !strings.Contains(got, "first") || strings.Contains(got, "second") || strings.Contains(got, "third") {
		t.Fatalf("budget kept wrong lines:\n%s", got)
	}
	if !strings.Contains(got, "+2 more facts") {
		t.Fatalf("missing omitted count:\n%s", got)
	}
}

// TestIndexBoundedDeterministic verifies identical inputs produce byte-identical
// output, which the cache-stable prefix depends on.
func TestIndexBoundedDeterministic(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "alpha", Description: "a", Type: TypeProject, UpdatedAt: now.Add(-1 * 24 * time.Hour)},
		{Name: "beta", Description: "b", Type: TypeProject, UpdatedAt: now.Add(-2 * 24 * time.Hour)},
	}
	first := renderBoundedIndex(memories, now, 200)
	second := renderBoundedIndex(memories, now, 200)
	if first != second {
		t.Fatalf("nondeterministic index:\n%q\nvs\n%q", first, second)
	}
}

// TestIndexBoundedMixedFold verifies the combined fold line when both stale
// facts and budget-omitted facts exist.
func TestIndexBoundedMixedFold(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "stale", Description: "old", Type: TypeProject, UpdatedAt: now.Add(-400 * 24 * time.Hour)},
		{Name: "keep", Description: "newest fact", Type: TypeProject, UpdatedAt: now.Add(-1 * 24 * time.Hour)},
		{Name: "cut", Description: "second fact", Type: TypeProject, UpdatedAt: now.Add(-2 * 24 * time.Hour)},
		{Name: "cut2", Description: "third fact", Type: TypeProject, UpdatedAt: now.Add(-3 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, 40) // room for ~1 active line
	if !strings.Contains(got, "+2 more facts (1 stale)") {
		t.Fatalf("missing combined fold line:\n%s", got)
	}
	if strings.Contains(got, "stale facts hidden") {
		t.Fatalf("wrong fold branch chosen:\n%s", got)
	}
}

// TestIndexBoundedZeroTimestampFallback verifies facts with a zero UpdatedAt
// are ordered by CreatedAt instead of sinking to the bottom.
func TestIndexBoundedZeroTimestampFallback(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "no-updated", Description: "created recently", Type: TypeProject, CreatedAt: now.Add(-1 * 24 * time.Hour)}, // UpdatedAt zero
		{Name: "updated", Description: "updated long ago", Type: TypeProject, UpdatedAt: now.Add(-90 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, defaultPrefixIndexMaxChars)
	if strings.Index(got, "(no-updated.md)") > strings.Index(got, "(updated.md)") {
		t.Fatalf("CreatedAt fallback ordering wrong:\n%s", got)
	}
}

// TestFoldLineNotManagedIndexLine pins that fold lines never match indexLineRe,
// so reindex/Delete keep treating them as unmanaged (and they never reach disk).
func TestFoldLineNotManagedIndexLine(t *testing.T) {
	for _, line := range []string{
		"- +9 more facts (3 stale) — search with the memory tool",
		"- 3 stale facts hidden — search with the memory tool",
	} {
		if indexLineRe.MatchString(line) {
			t.Fatalf("fold line %q matches managed index regex", line)
		}
	}
}

// TestIndexBoundedSuppressesShadowedGlobals verifies a global fact shadowed by
// a same-name project fact never appears alone in the bounded view: the project
// winner represents it, so a recency order cannot surface the shadowed global
// as authoritative when the winner was budget-cut or folded.
func TestIndexBoundedSuppressesShadowedGlobals(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{ID: "g-dup", Name: "dup", Description: "global side", Type: TypeProject, Scope: FactScopeGlobal, UpdatedAt: now.Add(-1 * 24 * time.Hour)},
		{ID: "p-dup", Name: "dup", Description: "project winner", Type: TypeProject, Scope: FactScopeProject, UpdatedAt: now.Add(-2 * 24 * time.Hour)},
		{ID: "p-own", Name: "own", Description: "unrelated", Type: TypeProject, Scope: FactScopeProject, UpdatedAt: now.Add(-3 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, defaultPrefixIndexMaxChars)
	if strings.Contains(got, "global side") {
		t.Fatalf("shadowed global leaked into the bounded view:\n%s", got)
	}
	if !strings.Contains(got, "project winner") {
		t.Fatalf("project winner missing from the bounded view:\n%s", got)
	}
	if !strings.Contains(got, "+1 more facts") {
		t.Fatalf("fold line must count the shadowed global:\n%s", got)
	}
}

// TestIndexBoundedFoldsExpired verifies hard-expired facts (explicit expires_at
// in the past) are folded like stale ones instead of rendering as active lines.
func TestIndexBoundedFoldsExpired(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "gone", Description: "expired fact", Type: TypeProject, Scope: FactScopeProject, ExpiresAt: now.Add(-1 * time.Hour)},
		{Name: "alive", Description: "active fact", Type: TypeProject, Scope: FactScopeProject, UpdatedAt: now.Add(-1 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, defaultPrefixIndexMaxChars)
	if strings.Contains(got, "expired fact") {
		t.Fatalf("expired fact rendered as active:\n%s", got)
	}
	if !strings.Contains(got, "1 stale facts hidden") {
		t.Fatalf("missing expired fold line:\n%s", got)
	}
}

// TestIndexBoundedClipsLongDescription verifies the bounded projection clips a
// single line's description to maxIndexDescriptionGraphemes so one long fresh
// fact cannot consume the whole budget; short descriptions stay whole and the
// on-disk index (renderQualifiedIndexLine) is untouched by the clip.
func TestIndexBoundedClipsLongDescription(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	longDesc := strings.Repeat("长描述", 100) // 200 graphemes, over the 120 cap
	memories := []Memory{
		{Name: "long", Description: longDesc, Type: TypeProject, UpdatedAt: now.Add(-1 * 24 * time.Hour)},
		{Name: "short", Description: "brief", Type: TypeProject, UpdatedAt: now.Add(-2 * 24 * time.Hour)},
	}
	got := renderBoundedIndex(memories, now, defaultPrefixIndexMaxChars)
	if strings.Contains(got, longDesc) {
		t.Fatalf("long description should be clipped in the bounded view:\n%s", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("clipped description should end with ellipsis:\n%s", got)
	}
	if !strings.Contains(got, "brief") {
		t.Fatalf("short description stays whole:\n%s", got)
	}
}

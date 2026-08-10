package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"reasonix/internal/textutil"
)

// defaultPrefixIndexMaxChars bounds the index projection rendered into the
// cached system-prompt prefix. The on-disk MEMORY.md stays complete; only the
// prefix-facing projection is capped, keeping every-turn prefix tokens bounded
// as memories accumulate. Roughly 300 tokens at 4 chars/token.
const defaultPrefixIndexMaxChars = 1200

// IndexBounded renders the index projection that loads into the cached prefix:
// stale facts (per memoryFreshness) fold into a summary line and the rest are
// ordered by recency, capped to a soft budget of maxChars (at least one line
// always renders). Deterministic in the memory set and now, so the prefix
// stays byte-stable within a session.
func (s Store) IndexBounded(now time.Time, maxChars int) string {
	memories := s.ListAll()
	if len(memories) == 0 {
		return ""
	}
	return renderBoundedIndex(memories, now, maxChars)
}

// maxIndexDescriptionGraphemes bounds one line's description in the prefix
// projection so a single long fresh fact cannot consume the whole budget; the
// on-disk index keeps descriptions whole.
const maxIndexDescriptionGraphemes = 120

// renderBoundedIndex is the deterministic core of IndexBounded, separated out
// for direct testing with a fixed clock and memory set.
func renderBoundedIndex(memories []Memory, now time.Time, maxChars int) string {
	// Shadowed globals (project overrides same-name global, #7995) are
	// represented by their project winner; the shadowed global is dropped so
	// recency can never surface it as authoritative when the winner was cut.
	shadowed := map[string]bool{}
	for _, o := range FindOverrides(memories) {
		shadowed[o.Global.ID] = true
	}
	active := make([]Memory, 0, len(memories))
	folded := 0     // stale + hard-expired facts
	suppressed := 0 // shadowed globals represented by their project winner
	for _, m := range memories {
		freshness := memoryFreshness(m, now)
		if freshness == FreshnessStale || freshness == FreshnessExpired {
			folded++
			continue
		}
		if shadowed[m.ID] {
			suppressed++
			continue
		}
		active = append(active, m)
	}
	// Most recently updated first; equal timestamps keep the stable name order
	// ListAll() already produced.
	sort.SliceStable(active, func(i, j int) bool {
		return memoryUpdatedAt(active[i]).After(memoryUpdatedAt(active[j]))
	})
	var b strings.Builder
	kept := 0
	for _, m := range active {
		clipped := m
		clipped.Description = textutil.ClipGraphemes(m.Description, maxIndexDescriptionGraphemes, "…")
		line := renderQualifiedIndexLine(clipped) + "\n"
		if kept > 0 && b.Len()+len(line) > maxChars {
			break
		}
		b.WriteString(line)
		kept++
	}
	omitted := len(active) - kept + suppressed
	switch {
	case omitted > 0:
		fmt.Fprintf(&b, "- +%d more facts", omitted)
		if folded > 0 {
			fmt.Fprintf(&b, " (%d stale)", folded)
		}
		b.WriteString(" — search with the memory tool\n")
	case folded > 0:
		fmt.Fprintf(&b, "- %d stale facts hidden — search with the memory tool\n", folded)
	}
	return b.String()
}

// memoryUpdatedAt returns the timestamp a fact is ordered by: UpdatedAt,
// falling back to CreatedAt when zero.
func memoryUpdatedAt(m Memory) time.Time {
	if !m.UpdatedAt.IsZero() {
		return m.UpdatedAt
	}
	return m.CreatedAt
}

package memory

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"reasonix/internal/base/retrieval"
)

const (
	aliasWeight        = 0.6
	recentUseWindow    = 30 * 24 * time.Hour
	recentUseBoost     = 1.1
	constraintFloor    = 0.08
	maxConstraintsHeld = 3
	galaxySeeds        = 3
	minGalaxyNameRunes = 2
)

func importanceWeight(m Memory) float64 {
	return 1 + 0.15*float64(m.ResolvedImportance()-ImportanceNormal)
}

func recentUseWeight(m Memory, now time.Time) float64 {
	if !m.LastUsedAt.IsZero() && now.Sub(m.LastUsedAt) < recentUseWindow {
		return recentUseBoost
	}
	return 1
}

func touchedGalaxies(query string, hits []RecallHit, docs []autoRecallDoc) map[string]bool {
	lower := strings.ToLower(query)
	touched := map[string]bool{}
	for _, doc := range docs {
		g := NormalizeGalaxy(doc.memory.Galaxy)
		if utf8.RuneCountInString(g) >= minGalaxyNameRunes && strings.Contains(lower, strings.ToLower(g)) {
			touched[g] = true
		}
	}
	for i, hit := range hits {
		if i >= galaxySeeds {
			break
		}
		if g := NormalizeGalaxy(hit.Memory.Galaxy); g != "" {
			touched[g] = true
		}
	}
	return touched
}

func constraintHitsFor(docs []autoRecallDoc, hits []RecallHit, queryTerms []string, query string, now time.Time, snippetRunes int) []RecallHit {
	sorted := append([]RecallHit(nil), hits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })
	best := 0.0
	if len(sorted) > 0 {
		best = sorted[0].Score
	}
	scored := map[string]float64{}
	for _, hit := range hits {
		scored[hit.Memory.ID] = hit.Score
	}
	touched := touchedGalaxies(query, sorted, docs)
	var out []RecallHit
	for _, doc := range docs {
		m := doc.memory
		if NormalizeType(string(m.Type)) != TypeConstraint {
			continue
		}
		freshness := memoryFreshness(m, now)
		if freshness == FreshnessExpired {
			continue
		}
		g := NormalizeGalaxy(m.Galaxy)
		inGalaxy := g != "" && touched[g]
		strong := best > 0 && scored[m.ID] >= best*constraintFloor && scored[m.ID] > 0
		if !inGalaxy && !strong {
			continue
		}
		out = append(out, RecallHit{
			Memory:    m,
			Score:     scored[m.ID],
			Freshness: freshness,
			Reason:    "constraint",
			Snippet:   retrieval.MakeSnippet(doc.text, query, queryTerms, snippetRunes),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Memory.ResolvedImportance() != out[j].Memory.ResolvedImportance() {
			return out[i].Memory.ResolvedImportance() > out[j].Memory.ResolvedImportance()
		}
		return out[i].Memory.ID < out[j].Memory.ID
	})
	if len(out) > maxConstraintsHeld {
		out = out[:maxConstraintsHeld]
	}
	return out
}

func withoutHits(hits, drop []RecallHit) []RecallHit {
	skip := map[string]bool{}
	for _, d := range drop {
		skip[d.Memory.ID] = true
	}
	out := make([]RecallHit, 0, len(hits))
	for _, hit := range hits {
		if !skip[hit.Memory.ID] {
			out = append(out, hit)
		}
	}
	return out
}

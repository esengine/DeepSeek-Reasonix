// reformulation.go — how a query changed, from token sets alone.
package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/base/retrieval"
)

// The host probe ranks every target first, so the engine is not what makes a
// model take more than one attempt. These classes are set arithmetic over the
// same tokenizer recall uses: no judge, nothing read out of prose.

const (
	reformFirst      = "First"            // nothing to compare against yet
	reformNearDup    = "NearDuplicate"    // the same query again, give or take a word
	reformNarrowing  = "Narrowing"        // kept everything and added terms
	reformBroadening = "Broadening"       // dropped terms, added none
	reformAnchored   = "HistoricalAnchor" // reached for a term the question does not contain
	reformShift      = "SynonymShift"     // traded terms for others
)

// nearDuplicateOverlap is where "reworded" stops and "asked again" begins.
const nearDuplicateOverlap = 0.8

func terms(s string) []string {
	return retrieval.Unique(retrieval.Tokens(s))
}

func termSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range terms(s) {
		out[t] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	shared := 0
	for t := range a {
		if b[t] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// classifyReformulation names how this query differs from the one before it.
func classifyReformulation(previous, current, question string) string {
	if previous == "" {
		return reformFirst
	}
	prev, cur := termSet(previous), termSet(current)
	if jaccard(prev, cur) >= nearDuplicateOverlap {
		return reformNearDup
	}
	added, dropped := diffTerms(cur, prev), diffTerms(prev, cur)
	// A term the question never used had to come from the model's own memory of
	// the history, which is a different move from rephrasing what it was asked.
	ask := termSet(question)
	for _, t := range added {
		if !ask[t] {
			return reformAnchored
		}
	}
	switch {
	case len(dropped) == 0:
		return reformNarrowing
	case len(added) == 0:
		return reformBroadening
	default:
		return reformShift
	}
}

func diffTerms(from, minus map[string]bool) []string {
	var out []string
	for t := range from {
		if !minus[t] {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// classifySearches labels each attempt and records which of the host probe's
// terms it left out. The probe is the oracle: it ranks the target first, so a
// query missing its terms says what the model did not think to ask for.
func classifySearches(m *contextMetrics, inst fixtureInstance) {
	probe := terms(inst.ProbeQuery)
	previous := ""
	for i := range m.Searches {
		s := &m.Searches[i]
		s.Reformulation = classifyReformulation(previous, s.Query, inst.Prompt)
		have := termSet(s.Query)
		for _, term := range probe {
			if !have[term] {
				s.MissingProbeTerms = append(s.MissingProbeTerms, term)
			}
		}
		previous = s.Query
	}
}

// queryAudit is the reformulation funnel across a set of runs.
type queryAudit struct {
	Runs, RunsThatSearched  int
	FirstQueryHit           int
	HitBySecond, HitByThird int
	NeverHit                int
	TotalSearches           int
	PostHitSearches         int
	HitThenRead             int
	EscapeBeforeFirstSearch int
	Reformulations          map[string]int
	MissedProbeTerms        map[string]int
}

func auditQueries(runs []contextMetrics) queryAudit {
	a := queryAudit{Reformulations: map[string]int{}, MissedProbeTerms: map[string]int{}}
	for _, r := range runs {
		if r.Contaminated {
			continue
		}
		a.Runs++
		a.TotalSearches += len(r.Searches)
		a.PostHitSearches += r.PostHitSearches
		if r.EscapeBeforeFirstRecall > 0 {
			a.EscapeBeforeFirstSearch++
		}
		if len(r.Searches) == 0 {
			continue
		}
		a.RunsThatSearched++
		hitAt := 0
		for i, s := range r.Searches {
			a.Reformulations[s.Reformulation]++
			for _, term := range s.MissingProbeTerms {
				a.MissedProbeTerms[term]++
			}
			if s.TargetRank > 0 && hitAt == 0 {
				hitAt = i + 1
			}
		}
		switch {
		case hitAt == 1:
			a.FirstQueryHit++
		case hitAt == 2:
			a.HitBySecond++
		case hitAt >= 3:
			a.HitByThird++
		default:
			a.NeverHit++
		}
		if hitAt > 0 && r.TargetRead {
			a.HitThenRead++
		}
	}
	return a
}

func (a queryAudit) report() string {
	var b strings.Builder
	b.WriteString("\n## Query reformulation\n")
	fmt.Fprintf(&b, "  runs=%d searched=%d  searches=%d (%.2f per searching run)\n",
		a.Runs, a.RunsThatSearched, a.TotalSearches, ratio(a.TotalSearches, a.RunsThatSearched))
	fmt.Fprintf(&b, "  target found by query 1: %d   by 2: %d   by 3+: %d   never: %d\n",
		a.FirstQueryHit, a.HitBySecond, a.HitByThird, a.NeverHit)
	fmt.Fprintf(&b, "  found then read: %d/%d   searches issued after the target was already back: %d\n",
		a.HitThenRead, a.FirstQueryHit+a.HitBySecond+a.HitByThird, a.PostHitSearches)
	fmt.Fprintf(&b, "  reached for the workspace before searching: %d\n", a.EscapeBeforeFirstSearch)
	b.WriteString("  reformulations: " + countsLine(a.Reformulations) + "\n")
	b.WriteString("  probe terms most often absent: " + topCounts(a.MissedProbeTerms, 8) + "\n")
	return b.String()
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func countsLine(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return strings.Join(parts, " ")
}

func topCounts(counts map[string]int, limit int) string {
	type kv struct {
		k string
		n int
	}
	all := make([]kv, 0, len(counts))
	for k, n := range counts {
		all = append(all, kv{k, n})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].k < all[j].k
	})
	parts := make([]string, 0, limit)
	for _, e := range all[:min(limit, len(all))] {
		parts = append(parts, fmt.Sprintf("%s=%d", e.k, e.n))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}

// queryLines lists every attempt, so a surprising audit can be read back.
func queryLines(runs []contextMetrics) string {
	var b strings.Builder
	b.WriteString("\n## Every query, in order\n")
	for _, r := range runs {
		for _, s := range r.Searches {
			rank := "miss"
			if s.TargetRank > 0 {
				rank = fmt.Sprintf("rank %d", s.TargetRank)
			}
			fmt.Fprintf(&b, "  %-24s %-14s #%d %-17s %-9s %q\n",
				r.Task, r.Arm, s.Ordinal, s.Reformulation, rank, s.Query)
			if len(s.MissingProbeTerms) > 0 && s.TargetRank == 0 {
				fmt.Fprintf(&b, "  %-24s %-14s    missing probe terms: %s\n",
					"", "", strings.Join(slices.Clone(s.MissingProbeTerms), " "))
			}
		}
	}
	return b.String()
}

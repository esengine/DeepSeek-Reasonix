// compact_search.go — finding a folded message when no address survived.
package agent

import (
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/base/retrieval"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// Search runs against the canonical transcript, never the projection: an
// address found here outlives the generations that lost it, whether to index
// eviction or to a re-fold canonicalOriginFor cannot place at all.

const (
	defaultRecallSearchLimit = 8
	maxRecallSearchLimit     = 20
	recallSnippetRunes       = 240
	// recallSearchNoise drops trailing hits far below the best one, so a query
	// whose terms are common does not return twenty near-zero rows.
	recallSearchNoise = 0.25
)

// Recall document kinds. tool_input is the call's arguments and tool_output its
// result; both address the assistant message that made the call.
const (
	recallKindUser      = "user_text"
	recallKindAssistant = "assistant_text"
	recallKindToolIn    = "tool_input"
	recallKindToolOut   = "tool_output"
)

// recallDoc is one searchable unit of the folded region.
type recallDoc struct {
	position int
	kind     string
	tool     string
	text     string
	counts   map[string]int
	length   int
}

// buildRecallDocs turns the folded canonical region into searchable units. A
// tool result is searched on its own text but addressed by the assistant call
// above it, because recallSpan reads a call together with its results:
// addressing the result would return output without the command behind it.
func buildRecallDocs(region []provider.Message) []recallDoc {
	callAt := map[string]int{}
	callName := map[string]string{}
	var docs []recallDoc
	add := func(pos int, kind, toolName, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		terms := retrieval.Tokens(text)
		if len(terms) == 0 {
			return
		}
		docs = append(docs, recallDoc{
			position: pos, kind: kind, tool: toolName, text: text,
			counts: retrieval.Counts(terms), length: len(terms),
		})
	}
	for i, m := range region {
		if m.LocalOnly {
			continue
		}
		for _, tc := range m.ToolCalls {
			callAt[tc.ID], callName[tc.ID] = i, tc.Name
			add(i, recallKindToolIn, tc.Name, tc.Name+" "+string(tc.Arguments))
		}
		switch m.Role {
		case provider.RoleUser:
			if !isCompactionSummary(m) {
				add(i, recallKindUser, "", messageSearchText(m))
			}
		case provider.RoleAssistant:
			add(i, recallKindAssistant, "", m.Content)
		case provider.RoleTool:
			pos, ok := callAt[m.ToolCallID]
			if !ok {
				continue
			}
			add(pos, recallKindToolOut, callName[m.ToolCallID], messageSearchText(m))
		}
	}
	return docs
}

// messageSearchText prefers the full body a bounded Content was cut from: the
// transcript still holds it, so a search that only saw the preview would miss
// what a read would return.
func messageSearchText(m provider.Message) string {
	if m.RawContent != "" {
		return m.RawContent
	}
	return m.Content
}

// searchFoldedRegion ranks the folded region against a query.
func searchFoldedRegion(region []provider.Message, query string, limit int) ([]tool.RecallHit, error) {
	terms, err := retrieval.QueryTerms(query)
	if err != nil {
		return nil, fmt.Errorf("recall: %w", err)
	}
	docs := buildRecallDocs(region)
	counts := make([]map[string]int, 0, len(docs))
	for _, d := range docs {
		counts = append(counts, d.counts)
	}
	df := retrieval.DocumentFrequency(counts)
	total, avgLen := len(docs), 0.0
	for _, d := range docs {
		avgLen += float64(d.length)
	}
	if total > 0 {
		avgLen /= float64(total)
	}

	type scored struct {
		doc   recallDoc
		score float64
	}
	var ranked []scored
	for _, d := range docs {
		if score := retrieval.BM25Score(d.counts, d.length, terms, df, total, avgLen); score > 0 {
			ranked = append(ranked, scored{doc: d, score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].doc.position < ranked[j].doc.position
	})
	ranked = retrieval.KeepTopRelativeScore(ranked, recallSearchNoise, func(s scored) float64 { return s.score })

	hits := make([]tool.RecallHit, 0, limit)
	seen := map[string]bool{}
	for _, s := range ranked {
		if len(hits) >= limit {
			break
		}
		// One line per (position, kind): a call and its result already share an
		// address, and reading it once returns both.
		key := fmt.Sprintf("%d\x00%s", s.doc.position, s.doc.kind)
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, tool.RecallHit{
			Position: s.doc.position, Kind: s.doc.kind, Tool: s.doc.tool,
			Snippet: retrieval.MakeSnippet(s.doc.text, query, terms, recallSnippetRunes),
		})
	}
	return hits, nil
}

// renderRecallHits is what the model reads back. Each line is an address it can
// pass straight to a read.
func renderRecallHits(query string, hits []tool.RecallHit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Folded context matching %q:\n", query)
	for _, h := range hits {
		fmt.Fprintf(&b, "\n#%d %s", h.Position, h.Kind)
		if h.Tool != "" {
			b.WriteString(" " + h.Tool)
		}
		b.WriteString("\n" + h.Snippet + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func normalizeRecallLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultRecallSearchLimit
	case limit > maxRecallSearchLimit:
		return maxRecallSearchLimit
	default:
		return limit
	}
}

// searchRecallLocked ranks the folded region and charges the snippets to this
// generation's recall budget. Callers hold compactionMu.
func (a *contextWindow) searchRecallLocked(region []provider.Message, query string, req tool.RecallRequest, budget, left int) (tool.RecallResult, error) {
	hits, err := searchFoldedRegion(region, query, normalizeRecallLimit(req.Limit))
	if err != nil {
		return tool.RecallResult{BudgetLeft: left}, err
	}
	if len(hits) == 0 {
		// Not an error: "it is not in the folded region" is an answer, and
		// failing the call would read as the search itself being broken.
		return tool.RecallResult{
			Text:     fmt.Sprintf("No folded message matches %q. The %d folded messages were all searched.", query, len(region)),
			Searched: len(region), BudgetLeft: left,
		}, nil
	}
	text := renderRecallHits(query, hits)
	cost := a.textTokens(text)
	if cost > left {
		return tool.RecallResult{BudgetLeft: left},
			fmt.Errorf("recall: %d tokens exceeds the %d left in this generation's recall budget — ask for fewer results", cost, left)
	}
	a.sess.win.compactionState.Recall.SpentTokens += cost
	return tool.RecallResult{
		Text: text, Hits: hits, Searched: len(region),
		Tokens: cost, BudgetLeft: budget - a.sess.win.compactionState.Recall.SpentTokens,
	}, nil
}

package agent

import (
	"encoding/json"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
)

// A digest carries what the fold changed and what failed; everything else
// becomes prose or nothing, and the model cannot tell which. An index line
// says what prose cannot: this happened, and here is where it still is.

// The heading carries its own legend; a separate explanatory line would be
// fixed overhead on every digest for the rest of the session.
const indexSectionHeading = "## Folded work index (#n = transcript position)"

// foldIndexEntry is one folded item reduced to a line.
type foldIndexEntry struct {
	Canonical int    // position in the canonical transcript, or -1 when unknown
	Kind      string // "user" | tool name
	Subject   string // path, command, or a user turn's opening words
	Note      string // "dropped" for a user turn past the budget, else ""
	rank      int    // lower survives the budget first
}

func (e foldIndexEntry) line() string {
	var b strings.Builder
	if e.Canonical >= 0 {
		fmt.Fprintf(&b, "#%d ", e.Canonical)
	}
	b.WriteString(e.Kind)
	if e.Subject != "" {
		b.WriteString("  " + e.Subject)
	}
	if e.Note != "" {
		b.WriteString("  (" + e.Note + ")")
	}
	return "- " + b.String()
}

// Ranks order what survives a bounded index. A user turn the budget could not
// hold comes first: its original is words nobody can re-derive. Failed
// exploration outranks a plain read — a path already tried is worth more.
const (
	rankDroppedUserTurn = iota
	rankFailedCall
	rankCommand
	rankRead
)

// buildFoldIndex reduces the fold region to index lines. origin maps a region
// position to its canonical index, or -1 for a message already folded once.
func buildFoldIndex(region []provider.Message, kept []bool, facts func(string) evidence.ToolFacts, origin func(int) int) []foldIndexEntry {
	var out []foldIndexEntry
	calls := map[string]provider.ToolCall{}
	callAt := map[string]int{}
	for i, m := range region {
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = tc
			callAt[tc.ID] = i
		}
		switch {
		case m.Role == provider.RoleUser && !m.LocalOnly && !isCompactionSummary(m):
			if i < len(kept) && kept[i] {
				continue // held verbatim; the projection still shows it
			}
			out = append(out, foldIndexEntry{
				Canonical: origin(i), Kind: "you", Subject: quotedOpening(m.Content),
				Note: "summary only", rank: rankDroppedUserTurn,
			})
		case m.Role == provider.RoleTool:
			call, ok := calls[m.ToolCallID]
			if !ok {
				continue
			}
			failed := isErrorMessage(m)
			rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), !failed, facts(call.Name))
			// Skip exactly what the digest is already on the hook for (see
			// compact_coverage.go): a change with a path, or a failed command.
			// Anything else falls to the index, so nothing lands between them.
			if coverageDemands(rec, failed) {
				continue
			}
			entry := foldIndexEntry{Canonical: origin(callAt[m.ToolCallID]), Kind: call.Name, rank: rankRead}
			switch {
			case rec.Command != "":
				entry.Subject, entry.rank = firstLine(rec.Command), rankCommand
			case len(rec.Paths) > 0:
				entry.Subject = strings.Join(rec.Paths, " ")
			default:
				entry.Subject = summarizeToolArgs(call.Arguments)
			}
			if failed {
				entry.Note, entry.rank = "failed", rankFailedCall
			}
			out = append(out, entry)
		}
	}
	return out
}

// renderFoldIndex writes the section, dropping the lowest-ranked entries when
// the budget binds. Ties keep transcript order so the section reads forward.
func (a *contextWindow) renderFoldIndex(entries []foldIndexEntry, budgetTokens int) string {
	if len(entries) == 0 || budgetTokens <= 0 {
		return ""
	}
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	// Stable by rank: sort.SliceStable would do, but the slice is small and the
	// selection has to stay in transcript order for rendering anyway.
	var chosen []int
	spent := a.textTokens(indexSectionHeading)
	for rank := rankDroppedUserTurn; rank <= rankRead; rank++ {
		for _, i := range order {
			if entries[i].rank != rank {
				continue
			}
			cost := a.textTokens(entries[i].line())
			if spent+cost > budgetTokens {
				continue
			}
			spent += cost
			chosen = append(chosen, i)
		}
	}
	if len(chosen) == 0 {
		return ""
	}
	keep := make([]bool, len(entries))
	for _, i := range chosen {
		keep[i] = true
	}
	var b strings.Builder
	b.WriteString(indexSectionHeading + "\n")
	for i, e := range entries {
		if keep[i] {
			b.WriteString(e.line() + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// splitFoldIndex separates a digest's prose from the index section this package
// appended to it. Keeping them apart is what lets a later fold re-summarize the
// prose without asking a model to rewrite lines it never wrote.
func splitFoldIndex(digest string) (prose, index string) {
	i := strings.Index(digest, indexSectionHeading)
	if i < 0 {
		return digest, ""
	}
	return strings.TrimRight(digest[:i], "\n "), strings.TrimSpace(digest[i:])
}

// mergeFoldIndex carries the previous index forward ahead of the new lines and
// trims from the oldest when the budget binds — an entry that has survived more
// folds is the one whose original is furthest out of reach.
func (a *contextWindow) mergeFoldIndex(previous, fresh string, budgetTokens int) string {
	previous, fresh = strings.TrimSpace(previous), strings.TrimSpace(fresh)
	if previous == "" {
		return fresh
	}
	lines := append(indexBodyLines(previous), indexBodyLines(fresh)...)
	if len(lines) == 0 {
		return ""
	}
	spent := a.textTokens(indexSectionHeading)
	first := 0
	for i, line := range slices.Backward(lines) {
		cost := a.textTokens(line)
		if spent+cost > budgetTokens {
			first = i + 1
			break
		}
		spent += cost
	}
	if first >= len(lines) {
		return ""
	}
	var b strings.Builder
	b.WriteString(indexSectionHeading + "\n")
	if first > 0 {
		fmt.Fprintf(&b, "- (%d older entries dropped; %s)\n", first, a.droppedIndexHint())
	}
	for _, l := range lines[first:] {
		b.WriteString(l + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func indexBodyLines(section string) []string {
	var out []string
	for line := range strings.SplitSeq(section, "\n") {
		trimmed := strings.TrimRight(line, " ")
		if strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "- (") {
			out = append(out, trimmed)
		}
	}
	return out
}

// quotedOpening is a user turn reduced to enough words to recognise it.
func quotedOpening(content string) string {
	const maxRunes = 60
	flat := strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if flat == "" {
		return ""
	}
	runes := []rune(flat)
	if len(runes) > maxRunes {
		flat = string(runes[:maxRunes]) + "…"
	}
	return strconv.Quote(flat)
}

// foldIndexBudget is the index's share of a checkpoint. A line costs about a
// dozen tokens, so one percent of the window addresses roughly a hundred items
// — shared by every generation, since the index is cumulative. It is still the
// cheapest thing a fold keeps: prose needs hundreds of tokens to say what a
// line says, and only a line can be recalled.
func (a *contextWindow) foldIndexBudget() int {
	const floor = 256
	// The bench axis scales the whole allowance, floor included: a floor that
	// survived would make the off arm a small index rather than none.
	scale := a.ablation.FoldIndex().Ratio()
	if scale <= 0 {
		return 0
	}
	window := a.effectiveContextWindow()
	if window <= 0 {
		return int(floor * scale)
	}
	return int(float64(max(floor, window/100)) * scale)
}

// stripFoldIndexFromDigests pulls the host-written index out of any prior
// digest in the fold input and returns it separately. The summarizer never
// sees it: rewriting lines it did not write only spends output budget and
// risks garbling addresses.
func stripFoldIndexFromDigests(fold []provider.Message) ([]provider.Message, string) {
	var carried []string
	out := make([]provider.Message, 0, len(fold))
	for _, m := range fold {
		if !isCompactionSummary(m) {
			out = append(out, m)
			continue
		}
		prose, index := splitFoldIndex(m.Content)
		if index != "" {
			carried = append(carried, index)
		}
		m.Content = prose
		out = append(out, m)
	}
	return out, strings.Join(carried, "\n")
}

// attachFoldIndex appends the merged index to a digest.
func (a *contextWindow) attachFoldIndex(digest, priorIndex string, entries []foldIndexEntry) string {
	budget := a.foldIndexBudget()
	merged := a.mergeFoldIndex(priorIndex, a.renderFoldIndex(entries, budget), budget)
	if merged == "" {
		return digest
	}
	return strings.TrimRight(digest, "\n") + "\n\n" + merged
}

// canonicalOriginFor maps a fold-region position back to its place in the
// canonical transcript, which is what an index address has to name. Positions
// inside a previous projection have no canonical address of their own — that
// content was already folded once — so they answer -1.
func (a *contextWindow) canonicalOriginFor(state sessionstore.CompactionState, canonical, msgs []provider.Message, head int) func(int) int {
	projected := len(state.Projection.Messages)
	// visibleInputForFold either returned canonical itself, or the projection
	// spliced with canonical[CoveredCount:]. The two cases differ only in where
	// the canonical run begins.
	if projected == 0 || len(msgs) == 0 || projected > len(msgs) {
		return func(i int) int {
			if pos := head + i; pos < len(canonical) {
				return pos
			}
			return -1
		}
	}
	covered := state.Projection.CoveredCount
	return func(i int) int {
		pos := head + i
		if pos < projected {
			return -1
		}
		if origin := covered + (pos - projected); origin < len(canonical) {
			return origin
		}
		return -1
	}
}

// droppedIndexHint names what can still reach a trimmed line. With search
// ablated the arm must not be told about a capability it does not have.
func (a *contextWindow) droppedIndexHint() string {
	if a.ablation.Off(ablation.RecallSearch) {
		return "the full transcript still holds them"
	}
	return "recall with a query still finds them"
}

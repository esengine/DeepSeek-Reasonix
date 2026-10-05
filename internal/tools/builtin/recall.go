package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
)

const maxRecallPositions = 8

func init() { tool.RegisterBuiltin(recallContext{}) }

// searchOff is the pre-search tool: it reads an address it was given and cannot
// look one up. A benchmark arm that ablates search installs this variant, so
// the arm is not shown a parameter its runtime refuses.
type recallContext struct{ searchOff bool }

// RecallWithoutSearch is the recall tool with its search half removed, for the
// no-recall-search arm.
func RecallWithoutSearch() tool.Tool { return recallContext{searchOff: true} }

func (recallContext) Name() string { return "recall" }

func (r recallContext) Description() string {
	if r.searchOff {
		return "Bring a folded part of this conversation back, addressed by the #n positions listed under \"Folded work index\" in a compaction summary. Prefer re-reading a file over recalling it — a current copy beats a folded one. Each compaction grants a fresh budget; a request past it is refused whole, not truncated."
	}
	return "Bring folded conversation back: read the #n positions listed under \"Folded work index\" in a compaction summary, or search the whole folded region by query when no index line names what you need. Prefer re-reading a file over recalling it — a current copy beats a folded one. One budget per compaction covers both, and a request past it is refused whole, not truncated."
}

func (r recallContext) Schema() json.RawMessage {
	if r.searchOff {
		return json.RawMessage(`{
"type":"object",
"additionalProperties":false,
"properties":{
  "positions":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"integer","minimum":0},"description":"The #n addresses from a folded-work index line, without the '#'."}
},
"required":["positions"]
}`)
	}
	return json.RawMessage(`{
"type":"object",
"additionalProperties":false,
"properties":{
  "positions":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"integer","minimum":0},"description":"Read these #n addresses, from an index line or a search hit, without the '#'."},
  "query":{"type":"string","description":"Search for these words instead. A tool result matches on its output and is addressed by its call."},
  "limit":{"type":"integer","minimum":1,"maximum":20,"description":"Search hits (default 8)."}
}
}`)
}

// ReadOnly is true in the permission/workspace sense: recall only reads this
// session's own canonical transcript.
func (recallContext) ReadOnly() bool { return true }

func (recallContext) PlanModeSafe() bool { return true }

func (r recallContext) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var request struct {
		Positions []int  `json:"positions"`
		Query     string `json:"query"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &request); err != nil {
		return "", fmt.Errorf("invalid recall args: %w", err)
	}
	// The arguments say which operation this is: positions read, a query
	// searches. Both together is refused rather than resolved, because either
	// reading has the tool doing something the caller did not ask for.
	query := strings.TrimSpace(request.Query)
	if r.searchOff {
		if query != "" {
			return "", fmt.Errorf("recall: this configuration reads addresses only")
		}
		if len(request.Positions) == 0 {
			return "", fmt.Errorf("recall: positions must name at least one #n address")
		}
	}
	if query == "" && len(request.Positions) == 0 {
		return "", fmt.Errorf("recall: give positions to read, or a query to search for them")
	}
	if len(request.Positions) > maxRecallPositions {
		return "", fmt.Errorf("recall: at most %d positions per call", maxRecallPositions)
	}
	recaller, ok := tool.ContextRecallerFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("recall is unavailable outside an active agent session")
	}
	result, err := recaller.RecallContext(ctx, tool.RecallRequest{
		Positions: request.Positions, Query: query, Limit: request.Limit,
	})
	if err != nil {
		return "", err
	}
	if query != "" {
		return renderRecallSearch(result), nil
	}
	return renderRecall(result), nil
}

// renderRecallSearch keeps the same footer shape as a read: the model reads one
// accounting line whichever operation it ran.
func renderRecallSearch(res tool.RecallResult) string {
	var b strings.Builder
	b.WriteString(res.Text)
	fmt.Fprintf(&b, "\n\n[searched %d folded messages; %d tokens; %d left before the next compaction",
		res.Searched, res.Tokens, res.BudgetLeft)
	if len(res.Hits) > 0 {
		b.WriteString("; read them with positions")
	}
	b.WriteString("]")
	return b.String()
}

// renderRecall puts the accounting last: the content is what the model is here
// to read, and a footer keeps the budget visible without framing every line.
func renderRecall(res tool.RecallResult) string {
	var b strings.Builder
	b.WriteString(res.Text)
	if !strings.HasSuffix(res.Text, "\n") {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n[recalled %s; %d tokens; %d left before the next compaction",
		positionList(res.Recalled), res.Tokens, res.BudgetLeft)
	if len(res.Missing) > 0 {
		fmt.Fprintf(&b, "; no longer addressable: %s", positionList(res.Missing))
	}
	b.WriteString("]")
	return b.String()
}

func positionList(positions []int) string {
	if len(positions) == 0 {
		return "nothing"
	}
	parts := make([]string, len(positions))
	for i, p := range positions {
		parts[i] = fmt.Sprintf("#%d", p)
	}
	return strings.Join(parts, ", ")
}

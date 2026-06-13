package agent

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"reasonix/internal/checkpoint"
	"reasonix/internal/provider"
)

// TurnBreakdown is a per-turn estimate of how many tokens its messages contribute
// to the next request, annotated with the user's prompt and file-change count.
type TurnBreakdown struct {
	Turn   int
	Prompt string
	Tokens int
	Files  int
}

// ContextBreakdown is a snapshot of what the next request would cost and what the
// session has spent so far, so the user can identify waste.
type ContextBreakdown struct {
	// Next-request estimate (local approximation, not the real tokenizer).
	SystemPromptTokens int
	ToolSchemaTokens   int
	PerToolSchemas     []ToolSchemaCost
	ConversationTokens int
	Turns              []TurnBreakdown
	TotalEstimated     int

	// Session aggregates from the API (real counts).
	Usage       SessionUsageMeta
	CacheHitPct float64

	// Compaction headroom.
	Window     int
	CompactPct float64

	// Verbose switches token formatting to raw integers instead of short
	// (e.g. "3.4M") so the caller can compare exact values across agents.
	Verbose bool
}

// Breakdown computes a context breakdown from the agent's current state.
func (a *Agent) Breakdown(schemas []provider.ToolSchema, cps *checkpoint.Store) *ContextBreakdown {
	b := &ContextBreakdown{}

	msgs := a.session.Snapshot()

	// System prompt — first message with role "system".
	for _, m := range msgs {
		if m.Role == provider.RoleSystem {
			b.SystemPromptTokens = estimateTextTokens(m.Content)
			break
		}
	}

	// Tool schemas.
	b.ToolSchemaTokens = 0
	b.PerToolSchemas = SchemaTokenCosts(schemas)
	for _, s := range b.PerToolSchemas {
		b.ToolSchemaTokens += s.Tokens
	}

	// Conversation — all non-system messages. Compute per-turn breakdown.
	convMsgs := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != provider.RoleSystem {
			convMsgs = append(convMsgs, m)
		}
	}
	b.ConversationTokens = estimateMessagesTokens(convMsgs)

	// Per-turn breakdown using checkpoint boundaries.
	b.Turns = turnsFromCheckpoints(msgs, cps)

	// Total estimate: system + tool schemas + conversation.
	// Note: this does not include API-level framing or tool-pairing overhead,
	// so it will be slightly lower than the real prompt token count.
	b.TotalEstimated = b.SystemPromptTokens + b.ToolSchemaTokens + b.ConversationTokens

	// Session usage.
	b.Usage = a.sessionUsage
	hit, miss := a.SessionCache()
	total := hit + miss
	if total > 0 {
		b.CacheHitPct = float64(hit) / float64(total) * 100
	}

	// Compaction headroom uses the last turn's prompt tokens (what's actually
	// in the window), not the cumulative sessionUsage.PromptTokens. For
	// sub-agent orchestrators that don't make direct API calls, fall back to
	// the total estimated next-request size (system + schemas + conversation).
	b.Window = a.contextWindow
	if b.Window > 0 {
		promptTokens := 0
		if lu := a.LastUsage(); lu != nil && lu.PromptTokens > 0 {
			promptTokens = lu.PromptTokens
		} else if b.TotalEstimated > 0 {
			promptTokens = b.TotalEstimated
		}
		if promptTokens > 0 {
			b.CompactPct = float64(promptTokens) / float64(b.Window) * 100
		}
	}

	return b
}

// turnsFromCheckpoints partitions messages into per-turn slices using checkpoint
// MsgIndex boundaries and returns a breakdown for each turn.
func turnsFromCheckpoints(msgs []provider.Message, cps *checkpoint.Store) []TurnBreakdown {
	if cps == nil {
		return nil
	}
	bounds := cps.Bounds()
	if len(bounds) == 0 {
		return nil
	}

	// Build a sorted list of (turn, startIndex) pairs.
	type boundary struct {
		turn  int
		start int
	}
	pairs := make([]boundary, 0, len(bounds))
	for turn, idx := range bounds {
		pairs = append(pairs, boundary{turn: turn, start: idx})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].start < pairs[j].start })

	cpsList := cps.List()
	fileCount := map[int]int{}
	for _, m := range cpsList {
		fileCount[m.Turn] = len(m.Paths)
	}

	var out []TurnBreakdown
	total := len(msgs)
	for i, p := range pairs {
		if p.start >= total {
			continue
		}
		end := total
		if i+1 < len(pairs) {
			end = pairs[i+1].start
		}
		if end > total {
			end = total
		}
		if p.start >= end {
			continue
		}
		seg := msgs[p.start:end]
		tokens := estimateMessagesTokens(seg)

		// Find prompt text from checkpoint metadata.
		prompt := ""
		for _, m := range cpsList {
			if m.Turn == p.turn {
				prompt = strings.TrimSpace(m.Prompt)
				break
			}
		}
		if len([]rune(prompt)) > 48 {
			prompt = string([]rune(prompt)[:48]) + "…"
		}

		out = append(out, TurnBreakdown{
			Turn:   p.turn,
			Prompt: prompt,
			Tokens: tokens,
			Files:  fileCount[p.turn],
		})
	}
	return out
}

// FormatBreakdown formats the breakdown as a multi-line string for the TUI.
func (b *ContextBreakdown) FormatBreakdown() string {
	var sb strings.Builder

	tok := shortTokenCount
	if b.Verbose {
		tok = rawTokenCount
	}

	// Title bar.
	sb.WriteString("── Context ─────────────────────────────────\n")

	// Next request estimate.
	sb.WriteString(fmt.Sprintf("Next request est. ~%s tokens\n", tok(b.TotalEstimated)))

	// System prompt.
	sysPct := pct(b.SystemPromptTokens, b.TotalEstimated)
	sb.WriteString(fmt.Sprintf("  System prompt:  %s (%s)\n", tok(b.SystemPromptTokens), sysPct))

	// Tool schemas.
	tsPct := pct(b.ToolSchemaTokens, b.TotalEstimated)
	sb.WriteString(fmt.Sprintf("  Tool schemas:   %s (%s)\n", tok(b.ToolSchemaTokens), tsPct))
	if len(b.PerToolSchemas) > 0 {
		// Show top 5 most expensive tools in a compact line.
		sorted := append([]ToolSchemaCost(nil), b.PerToolSchemas...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Tokens > sorted[j].Tokens })
		var parts []string
		for _, s := range sorted {
			parts = append(parts, fmt.Sprintf("%s %d", s.Name, s.Tokens))
			if len(parts) >= 5 {
				break
			}
		}
		if len(parts) > 0 {
			sb.WriteString("    " + strings.Join(parts, " · ") + "\n")
		}
	}

	// Conversation.
	if len(b.Turns) > 0 {
		convPct := pct(b.ConversationTokens, b.TotalEstimated)
		sb.WriteString(fmt.Sprintf("  Conversation:   %s (%s)\n", tok(b.ConversationTokens), convPct))
		for _, t := range b.Turns {
			fileTag := ""
			if t.Files > 0 {
				fileTag = fmt.Sprintf(" (%d file%s)", t.Files, plural(t.Files))
			}
			sb.WriteString(fmt.Sprintf("    t%d  %-48s %s%s\n", t.Turn, truncate(t.Prompt, 48), tok(t.Tokens), fileTag))
		}
	}

	// Cumulative totals broken down by input/output.
	cachedNew := ""
	if b.Usage.CacheHitTokens+b.Usage.CacheMissTokens > 0 {
		cachedNew = fmt.Sprintf(" (%s cached / %s new)", tok(b.Usage.CacheHitTokens), tok(b.Usage.CacheMissTokens))
	}
	reasoning := ""
	if b.Usage.ReasoningTokens > 0 {
		reasoning = fmt.Sprintf(" (%s reasoning)", tok(b.Usage.ReasoningTokens))
	}
	if b.Verbose {
		sb.WriteString(fmt.Sprintf("  Cumulative (all turns):  %d · %s%.4f\n",
			b.Usage.TotalTokens, b.Usage.Currency, b.Usage.Cost))
		sb.WriteString(fmt.Sprintf("    Input:   %s%s\n", tok(b.Usage.PromptTokens), cachedNew))
		sb.WriteString(fmt.Sprintf("    Output:  %s%s\n", tok(b.Usage.CompletionTokens), reasoning))
	} else {
		sb.WriteString(fmt.Sprintf("  Cumulative (all turns):  %s · %s%.4f\n",
			tok(b.Usage.TotalTokens), b.Usage.Currency, b.Usage.Cost))
		sb.WriteString(fmt.Sprintf("    Input:   %s%s\n", tok(b.Usage.PromptTokens), cachedNew))
		sb.WriteString(fmt.Sprintf("    Output:  %s%s\n", tok(b.Usage.CompletionTokens), reasoning))
	}

	// Compaction headroom.
	if b.Window > 0 {
		toCompact := (80.0 - b.CompactPct) // compact at 80% by default
		if toCompact < 0 {
			toCompact = 0
		}
		sb.WriteString(fmt.Sprintf("  Window: %s · %.0f%% used · %.0f%% to compact\n",
			tok(b.Window), b.CompactPct, toCompact))
	}

	return sb.String()
}

func rawTokenCount(n int) string {
	return strconv.Itoa(n)
}

func shortTokenCount(n int) string {
	switch {
	case n >= 999_950:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func pct(part, total int) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", float64(part)/float64(total)*100)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func plural(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}

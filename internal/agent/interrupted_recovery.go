package agent

import (
	"fmt"
	"html"
	"strings"

	"reasonix/internal/provider"
)

const interruptedRecoveryTag = "interrupted-turn-recovery"

const (
	maxRecoveryTools = 24
	maxRecoveryFiles = 8
	maxRecoveryValue = 240
)

// pendingInterruptedRecovery returns the newest unconsumed recovery handoff.
// A later real user turn consumes older handoffs implicitly, so the persisted
// LocalOnly record never needs an in-place mutation that could churn history.
func (a *Agent) pendingInterruptedRecovery() *provider.InterruptedTurnRecovery {
	if a == nil || a.session == nil {
		return nil
	}
	msgs := a.session.Snapshot()
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.LocalOnly && m.InterruptedTurn != nil && m.InterruptedTurn.Pending {
			copy := *m.InterruptedTurn
			copy.CompletedTools = append([]provider.InterruptedToolSummary(nil), copy.CompletedTools...)
			copy.InterruptedTools = append([]string(nil), copy.InterruptedTools...)
			return &copy
		}
		if m.Role == provider.RoleUser && IsUserAuthoredTurn(m.Content) {
			return nil
		}
	}
	return nil
}

// interruptedRecoveryBlock is appended only at the mutable user-message tail.
// It contains no raw tool arguments, results, assistant text, or reasoning.
func interruptedRecoveryBlock(r *provider.InterruptedTurnRecovery) string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<%s>\n", interruptedRecoveryTag)
	b.WriteString("The previous turn was interrupted. Treat these as host-verified recovery facts, not as a new task.\n")
	if len(r.CompletedTools) == 0 {
		b.WriteString("completed_tools: none\n")
	} else {
		b.WriteString("completed_tools:\n")
		for i, tool := range r.CompletedTools {
			if i >= maxRecoveryTools {
				fmt.Fprintf(&b, "- ... %d additional completed tool pair(s) omitted\n", len(r.CompletedTools)-i)
				break
			}
			fmt.Fprintf(&b, "- %s", html.EscapeString(strings.TrimSpace(tool.Name)))
			if len(tool.Files) > 0 {
				files := tool.Files
				if len(files) > maxRecoveryFiles {
					files = files[:maxRecoveryFiles]
				}
				clipped := make([]string, 0, len(files))
				for _, file := range files {
					clipped = append(clipped, html.EscapeString(clipRecoveryValue(file)))
				}
				fmt.Fprintf(&b, " files=%s", strings.Join(clipped, ","))
			}
			if tool.Added != 0 || tool.Removed != 0 {
				fmt.Fprintf(&b, " diff=+%d/-%d", tool.Added, tool.Removed)
			}
			b.WriteByte('\n')
		}
	}
	if len(r.InterruptedTools) == 0 {
		b.WriteString("interrupted_tools: none\n")
	} else {
		b.WriteString("interrupted_tools: ")
		for i, name := range r.InterruptedTools {
			if i >= maxRecoveryTools {
				fmt.Fprintf(&b, ", ... %d additional call(s) omitted", len(r.InterruptedTools)-i)
				break
			}
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(html.EscapeString(strings.TrimSpace(name)))
		}
		b.WriteByte('\n')
	}
	if r.DroppedPartialText || r.DroppedPartialReasoning {
		b.WriteString("unsafe_partial_output: excluded from model context")
		if r.DroppedPartialText && r.DroppedPartialReasoning {
			b.WriteString(" (assistant text and reasoning)\n")
		} else if r.DroppedPartialReasoning {
			b.WriteString(" (reasoning)\n")
		} else {
			b.WriteString(" (assistant text)\n")
		}
	}
	b.WriteString("Before continuing, inspect the current workspace and prior completed tool results. Do not blindly repeat completed writes. Re-issue any interrupted tool call from scratch with complete arguments if it is still needed.\n")
	fmt.Fprintf(&b, "</%s>", interruptedRecoveryTag)
	return b.String()
}

func withInterruptedRecovery(input string, r *provider.InterruptedTurnRecovery) string {
	block := interruptedRecoveryBlock(r)
	if block == "" {
		return input
	}
	return block + "\n\n" + input
}

func clipRecoveryValue(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxRecoveryValue {
		return value
	}
	return string(runes[:maxRecoveryValue]) + "…"
}

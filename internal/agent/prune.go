package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Tool-result maintenance is the free half of context management: stale tool
// results are re-derivable (files can be re-read, commands re-run), so rewriting
// them needs no summarizer call and never drops a message. tool_call/result
// pairing and assistant content (including signed reasoning) are untouched.
const (
	snippedMarker = "[snipped tool result — "
	prunedMarker  = "[elided tool result — "
	minPruneBytes = 1024
)

type toolResultMaintenanceMode int

const (
	toolResultSnip toolResultMaintenanceMode = iota
	toolResultPrune
)

// PruneStats reports one maintenance pass.
type PruneStats struct {
	Results    int
	SavedChars int
	Archive    string
	Mode       toolResultMaintenanceMode
	InputHash  string
	Force      bool
}

// SnipStaleToolResults shortens stale tool-result content older than the
// protected recent tail, archiving the originals first. Idempotent; a no-op
// when compaction is disabled (no context window).
func (a *Agent) SnipStaleToolResults() (PruneStats, error) {
	return a.maintainStaleToolResults(toolResultSnip)
}

// PruneStaleToolResults elides stale tool-result content older than the
// protected recent tail, archiving the originals first. It can upgrade already
// snipped results to a shorter placeholder.
func (a *Agent) PruneStaleToolResults() (PruneStats, error) {
	return a.maintainStaleToolResults(toolResultPrune)
}

func (a *Agent) maintainStaleToolResults(mode toolResultMaintenanceMode) (PruneStats, error) {
	st := PruneStats{Mode: mode}
	if a.contextWindow <= 0 {
		return st, nil
	}
	visible := a.modelVisibleMessages()
	next, st := a.applyToolResultMaintenanceView(visible, mode)
	st.Force = true
	if st.Results == 0 {
		return st, nil
	}
	before := a.currentProjectionVersion()
	if err := a.installPruneProjection(next, st); err != nil {
		return PruneStats{Mode: mode}, err
	}
	if a.currentProjectionVersion() == before {
		return PruneStats{Mode: mode}, nil
	}
	a.compactionMu.Lock()
	if a.compactionState.LastReceipt != nil {
		st.Archive = a.compactionState.LastReceipt.Archive
	}
	a.compactionMu.Unlock()
	return st, nil
}

func shouldMaintainToolResult(m provider.Message, mode toolResultMaintenanceMode) bool {
	if m.LocalOnly || m.Role != provider.RoleTool {
		return false
	}
	if strings.HasPrefix(m.Content, prunedMarker) {
		return false
	}
	if mode == toolResultSnip {
		return len(m.Content) >= minPruneBytes && !strings.HasPrefix(m.Content, snippedMarker)
	}
	if strings.HasPrefix(m.Content, snippedMarker) {
		return true
	}
	return len(m.Content) >= minPruneBytes
}

func rewriteToolResult(m provider.Message, mode toolResultMaintenanceMode, archive string, strategy snipStrategy) string {
	if mode == toolResultPrune {
		return pruneToolResult(m, archive)
	}
	return snipToolResult(m, archive, strategy)
}

func pruneToolResult(m provider.Message, archive string) string {
	if prior := originalToolArchive(m.Content); prior != "" {
		archive = prior
	}
	if archive == "" {
		archive = "not archived"
	}
	return fmt.Sprintf("%s%s, %d bytes archived to %s; re-run the tool if the data is needed again]", prunedMarker, m.Name, originalToolBytes(m.Content), archive)
}

func snipToolResult(m provider.Message, archive string, strategy snipStrategy) string {
	if archive == "" {
		archive = "not archived"
	}
	lines := strings.Split(m.Content, "\n")
	if len(lines) <= strategy.head+strategy.tail {
		headChars := minInt(strategy.headChars, len(m.Content)/2)
		tailChars := minInt(strategy.tailChars, len(m.Content)/4)
		return fmt.Sprintf("%s%s, %d bytes archived to %s; single large line truncated]\n%s\n[... %d bytes omitted ...]\n%s",
			snippedMarker, m.Name, len(m.Content), archive,
			firstRunes(m.Content, headChars),
			omittedBytes(m.Content, headChars, tailChars),
			lastRunes(m.Content, tailChars))
	}
	head := strings.Join(lines[:strategy.head], "\n")
	tail := strings.Join(lines[len(lines)-strategy.tail:], "\n")
	return fmt.Sprintf("%s%s, %d bytes archived to %s; showing first %d lines and last %d lines]\n%s\n[... %d lines omitted ...]\n%s",
		snippedMarker, m.Name, len(m.Content), archive, strategy.head, strategy.tail,
		head, len(lines)-strategy.head-strategy.tail, tail)
}

type snipStrategy struct {
	head      int
	tail      int
	headChars int
	tailChars int
}

// Defaults for tools that do not implement tool.SnipHinter, tiered by side
// effect. A read-only tool's output is front-loaded (the first lines are the
// answer), so it keeps a long head and short tail. A side-effecting tool — bash
// and any plugin — can carry a failure at either end (a build error at the tail,
// the command at the head), so it keeps both ends evenly. These are deliberately
// the only two defaults: a registered tool that fits neither must implement
// SnipHinter, and the contract test fails until it does.
var (
	defaultReadOnlySnip      = snipStrategy{head: 80, tail: 12, headChars: 10000, tailChars: 2000}
	defaultSideEffectingSnip = snipStrategy{head: 40, tail: 40, headChars: 8000, tailChars: 8000}
)

// snipStrategyFor resolves the snip geometry for a tool result by asking the
// registered tool itself (tool.SnipHinter), so the policy travels with the tool
// definition and a rename cannot silently desync it from a name-keyed table.
// When the tool is absent (e.g. an MCP server detached after producing the
// result) or declines to hint, it falls back to the ReadOnly-tiered default.
func (a *Agent) snipStrategyFor(name string) snipStrategy {
	if a.tools != nil {
		if t, ok := a.tools.Get(name); ok {
			if h, ok := t.(tool.SnipHinter); ok {
				return snipStrategyFromHint(h.SnipHint())
			}
			if t.ReadOnly() {
				return defaultReadOnlySnip
			}
			return defaultSideEffectingSnip
		}
	}
	return defaultReadOnlySnip
}

func snipStrategyFromHint(h tool.SnipHint) snipStrategy {
	return snipStrategy{head: h.Head, tail: h.Tail, headChars: h.HeadChars, tailChars: h.TailChars}
}

func originalToolBytes(content string) int {
	if strings.HasPrefix(content, snippedMarker) {
		end := strings.Index(content, " bytes archived to ")
		if end > len(snippedMarker) {
			fields := strings.Fields(content[len(snippedMarker):end])
			if len(fields) > 0 {
				var n int
				if _, err := fmt.Sscanf(fields[len(fields)-1], "%d", &n); err == nil && n > 0 {
					return n
				}
			}
		}
	}
	return len(content)
}

func originalToolArchive(content string) string {
	if !strings.HasPrefix(content, snippedMarker) {
		return ""
	}
	start := strings.Index(content, " bytes archived to ")
	if start < 0 {
		return ""
	}
	start += len(" bytes archived to ")
	end := strings.Index(content[start:], ";")
	if end < 0 {
		return ""
	}
	archive := strings.TrimSpace(content[start : start+end])
	if archive == "not archived" {
		return ""
	}
	return archive
}

func firstRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneBoundary(s, n) {
		n--
	}
	return s[:n]
}

func lastRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !isRuneBoundary(s, start) {
		start++
	}
	return s[start:]
}

func omittedBytes(s string, head, tail int) int {
	omitted := len(s) - head - tail
	if omitted < 0 {
		return 0
	}
	return omitted
}

func isRuneBoundary(s string, i int) bool {
	return i == 0 || i == len(s) || (i > 0 && i < len(s) && (s[i]&0xc0) != 0x80)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

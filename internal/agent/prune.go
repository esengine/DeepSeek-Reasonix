package agent

import (
	"strings"
	"unicode/utf8"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Legacy snip helpers still support compatibility storage. Their public APIs
// are no-ops; pressure-time Harness pruning uses the rune-based policy below.
const (
	snippedMarker = "[snipped tool result — "
	prunedMarker  = "[elided tool result — "
	minPruneBytes = 1024

	toolPruneThresholdRunes = 8192
	toolPruneHeadRunes      = 4096
	toolPruneTailRunes      = 1024
	toolPruneMarker         = "[... tool result middle pruned ...]"
)

func pruneToolResultContent(content string) (string, bool) {
	if utf8.RuneCountInString(content) <= toolPruneThresholdRunes {
		return content, false
	}
	headEnd := byteOffsetAfterRunes(content, toolPruneHeadRunes)
	tailStart := byteOffsetBeforeLastRunes(content, toolPruneTailRunes)
	var pruned strings.Builder
	pruned.Grow(headEnd + len(toolPruneMarker) + len(content) - tailStart)
	pruned.WriteString(content[:headEnd])
	pruned.WriteString(toolPruneMarker)
	pruned.WriteString(content[tailStart:])
	return pruned.String(), true
}

func byteOffsetAfterRunes(content string, count int) int {
	if count <= 0 {
		return 0
	}
	seen := 0
	for offset := range content {
		if seen == count {
			return offset
		}
		seen++
	}
	return len(content)
}

func byteOffsetBeforeLastRunes(content string, count int) int {
	offset := len(content)
	for range count {
		if offset == 0 {
			return 0
		}
		_, size := utf8.DecodeLastRuneInString(content[:offset])
		offset -= size
	}
	return offset
}

// pruneToolResultsToProjectionLocked installs a durable, model-visible prune
// projection. The caller owns compactionRunMu for the whole maintenance run;
// canonical storage, including RawContent, is never modified.
func (a *Agent) pruneToolResultsToProjectionLocked(trigger string) (bool, error) {
	canonical, transcriptVersion := a.sess.conversation.snapshotMessagesVersion()
	a.sess.compactionMu.Lock()
	stateSnapshot := a.sess.compactionState
	a.sess.compactionMu.Unlock()
	visible, _ := a.visibleInputForFold(stateSnapshot, canonical, transcriptVersion)
	projected := append([]provider.Message(nil), visible...)
	affected := 0
	for i := range projected {
		if projected[i].Role != provider.RoleTool {
			continue
		}
		source := projected[i].Content
		if projected[i].ProviderContent != "" {
			source = projected[i].ProviderContent
		}
		if pruned, changed := pruneToolResultContent(source); changed {
			projected[i].Content = pruned
			projected[i].RawContent = ""
			projected[i].ProviderContent = ""
			affected++
		}
	}
	if affected == 0 {
		return false, nil
	}
	return a.installMaintenanceProjection(maintenanceInstall{
		trigger: trigger, action: "prune", state: stateSnapshot,
		canonical: canonical, transcriptVersion: transcriptVersion,
		visible: visible, projected: projected, affected: affected,
	})
}

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

// SnipStaleToolResults is a no-op: automatic prune/snip projections are gone.
func (a *Agent) SnipStaleToolResults() (PruneStats, error) {
	return PruneStats{Mode: toolResultSnip}, nil
}

// PruneStaleToolResults is a no-op: automatic prune/snip projections are gone.
func (a *Agent) PruneStaleToolResults() (PruneStats, error) {
	return PruneStats{Mode: toolResultPrune}, nil
}

type snipStrategy struct {
	head      int
	tail      int
	headChars int
	tailChars int
}

var (
	defaultReadOnlySnip      = snipStrategy{head: 80, tail: 12, headChars: 10000, tailChars: 2000}
	defaultSideEffectingSnip = snipStrategy{head: 40, tail: 40, headChars: 8000, tailChars: 8000}
)

func (a *Agent) snipStrategyFor(name string) snipStrategy {
	if a.svc.tools != nil {
		if t, ok := a.svc.tools.Get(name); ok {
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

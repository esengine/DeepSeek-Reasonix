package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"
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
	projected = provider.ProjectionMessages(projected)
	sourceTokens := a.estimatedVisibleRequestTokens(visible)
	resultTokens := a.estimatedVisibleRequestTokens(projected)
	inputHash := a.contextMaintenanceInputHash(modelInputMessages(visible))
	outputHash := providerVisibleFingerprint(modelInputMessages(projected))
	projectionVersion := stateSnapshot.Projection.ProjectionVersion + 1
	now := time.Now().UTC()
	coveredHash := coveredPrefixHash(canonical, len(canonical))
	receipt := &ContextMaintenanceReceipt{
		OperationID: fmt.Sprintf("prune-%d-%s", projectionVersion, outputHash), Status: "applied", Action: "prune",
		Trigger: trigger, SourceProjection: stateSnapshot.Projection.ProjectionVersion, ProjectionVersion: projectionVersion,
		CoveredCount: len(canonical), CoveredPrefixHash: coveredHash, InputHash: inputHash, OutputHash: outputHash,
		InputTokens: sourceTokens, ResultTokens: resultTokens, SavedTokens: max(0, sourceTokens-resultTokens),
		AffectedToolResults: affected, CacheBreak: true, CreatedAt: now,
	}
	next := stateSnapshot
	next.SchemaVersion = compactionStateSchemaCurrent
	next.TranscriptVersion = transcriptVersion
	next.Generation++
	next.PromptCacheKey = a.currentPromptCacheKey()
	next.Projection = ContextProjection{
		Messages: projected, TranscriptVersion: transcriptVersion, ProjectionVersion: projectionVersion,
		CoveredCount: len(canonical), CoveredPrefixHash: coveredHash, SourceTokens: sourceTokens,
		ProjectionTokens: resultTokens, ViewInputHash: inputHash, ViewOutputHash: outputHash, CreatedAt: now,
	}
	next.LastReceipt = receipt
	next.UpdatedAt = now

	a.sess.compactionMu.Lock()
	current, currentVersion := a.sess.conversation.snapshotMessagesVersion()
	if currentVersion != transcriptVersion || len(current) != len(canonical) ||
		coveredPrefixHash(current, len(current)) != coveredHash ||
		a.sess.compactionState.Projection.ProjectionVersion != stateSnapshot.Projection.ProjectionVersion ||
		a.sess.compactionState.Generation != stateSnapshot.Generation {
		a.sess.compactionMu.Unlock()
		return false, errCompressStaleContext
	}
	previous := a.sess.compactionState
	a.sess.compactionState = next
	if err := a.persistCompactionStateLocked(); err != nil {
		a.sess.compactionState = previous
		a.sess.compactionMu.Unlock()
		if errors.Is(err, errCompressStaleContext) {
			return false, err
		}
		return false, fmt.Errorf("persist prune projection: %w", err)
	}
	a.sess.checkpointState = "applied"
	a.sess.compactionMu.Unlock()
	a.emitContextMaintenance(receipt)
	return true, nil
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

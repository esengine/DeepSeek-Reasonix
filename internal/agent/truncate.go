package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/provider"
)

// Truncation is the lossy last rung of overflow recovery, taken only when no
// summary can form: tool results outside the protected tail are elided
// oldest-first, then whole replay units are dropped, until the view fits under
// the target. It is a projection; canonical storage keeps every byte.
const (
	maintenanceActionTruncate = "truncate"
	elidedToolResultPrefix    = "[tool result elided to fit the context window"
	truncatedHistoryMarker    = "[earlier conversation truncated to fit the context window: %d messages removed]"
	// truncateProtectShare bounds the verbatim tail to this fraction of the
	// target so a rescue can always reclaim enough.
	truncateProtectShare = 4
)

func (a *Agent) truncateToProjectionLocked(trigger string, target int) (bool, error) {
	canonical, transcriptVersion := a.sess.conversation.snapshotMessagesVersion()
	a.sess.compactionMu.Lock()
	stateSnapshot := a.sess.compactionState
	a.sess.compactionMu.Unlock()
	visible, _ := a.visibleInputForFold(stateSnapshot, canonical, transcriptVersion)
	projected, affected := a.truncateView(visible, target)
	if affected == 0 {
		return false, nil
	}
	return a.installMaintenanceProjection(maintenanceInstall{
		trigger: trigger, action: maintenanceActionTruncate, state: stateSnapshot,
		canonical: canonical, transcriptVersion: transcriptVersion,
		visible: visible, projected: projected, affected: affected,
	})
}

// truncateView returns the truncated copy of visible and how many messages it
// changed; zero means the view already fits or nothing could be cut.
func (a *Agent) truncateView(visible []provider.Message, target int) ([]provider.Message, int) {
	total := a.estimatedVisibleRequestTokens(visible)
	if target <= 0 || total < target || len(visible) == 0 {
		return nil, 0
	}
	head := a.pinnedPrefixLen(visible)
	budget := max(1, min(a.recentTailBudget(), target/truncateProtectShare))
	protect := tailStart(visible, head, budget, a.tokPerChar(), minRecentKeep)
	projected := append([]provider.Message(nil), visible...)
	remaining, affected := total, 0
	for i := head; i < protect && remaining >= target; i++ {
		elided, ok := elideToolResult(projected[i])
		if !ok {
			continue
		}
		remaining -= a.messageTokens(projected[i]) - a.messageTokens(elided)
		projected[i] = elided
		affected++
	}
	if remaining >= target {
		var dropped int
		projected, dropped = a.dropOldestUnits(projected, head, protect, target)
		affected += dropped
	}
	if affected == 0 || a.estimatedVisibleRequestTokens(projected) >= total {
		return nil, 0
	}
	return projected, affected
}

func (a *Agent) messageTokens(m provider.Message) int {
	return a.estimatedPromptTokens([]provider.Message{m})
}

func elideToolResult(m provider.Message) (provider.Message, bool) {
	if m.Role != provider.RoleTool || m.LocalOnly || m.Content == "" || strings.HasPrefix(m.Content, elidedToolResultPrefix) {
		return m, false
	}
	out := m
	out.Content = fmt.Sprintf("%s: %d bytes]", elidedToolResultPrefix, len(m.Content))
	out.RawContent = ""
	out.ProviderContent = ""
	out.Images = nil
	return out, true
}

// dropOldestUnits removes whole replay units from the oldest end of the
// foldable region until the estimate fits. The latest session context,
// compaction digests, and pinned revisions survive behind one marker.
func (a *Agent) dropOldestUnits(msgs []provider.Message, head, protect, target int) ([]provider.Message, int) {
	if protect <= head {
		return msgs, 0
	}
	remaining := a.estimatedVisibleRequestTokens(msgs)
	latestContext := latestSessionContextIndex(msgs)
	var kept []provider.Message
	dropped, end := 0, head
	for _, u := range extractMessageUnits(msgs[head:protect]) {
		if remaining < target {
			break
		}
		for i := head + u.lo; i < head+u.hi; i++ {
			if i == latestContext || isCompactionSummary(msgs[i]) || IsPinnedContextRevision(msgs[i]) {
				kept = append(kept, msgs[i])
				continue
			}
			remaining -= a.messageTokens(msgs[i])
			dropped++
		}
		end = head + u.hi
	}
	if dropped == 0 {
		return msgs, 0
	}
	out := make([]provider.Message, 0, len(msgs)-dropped+1)
	out = append(out, msgs[:head]...)
	out = append(out, HostGeneratedUserMessage(fmt.Sprintf(truncatedHistoryMarker, dropped)))
	out = append(out, kept...)
	out = append(out, msgs[end:]...)
	return out, dropped
}

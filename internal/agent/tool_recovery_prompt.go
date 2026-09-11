package agent

import (
	"reasonix/internal/provider"
	"slices"
)

func confirmedRecoveryEffect(msgs []provider.Message, id provider.ActionIdentity) bool {
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			r := c.Recovery
			if r != nil && r.State == provider.ToolRunUserConfirmed && r.Identity.CanonicalTool == id.CanonicalTool && r.Identity.ArgumentDigest == id.ArgumentDigest && r.Identity.ResourceScope == id.ResourceScope {
				return true
			}
		}
	}
	return false
}

func (a *Agent) SilentToolRecovery() bool {
	r := a.transcriptInterruptedRecovery()
	return r != nil && r.SilentInterruption
}

// Durable effect evidence is independent of the old, consumable prompt-tail
// handoff. Facts remain bounded and never include raw arguments or output.
func (a *Agent) pendingInterruptedRecovery() *provider.InterruptedTurnRecovery {
	r := a.transcriptInterruptedRecovery()
	if a == nil || a.sess.conversation == nil {
		return r
	}
	seen := map[string]bool{}
	latest := map[string]*provider.ToolCallRecord{}
	for _, m := range a.Session().Snapshot() {
		for _, c := range m.ToolCalls {
			if c.Recovery != nil {
				latest[c.ID] = c.Recovery
			}
		}
	}
	if r != nil {
		r.UnknownTools = slices.DeleteFunc(slices.Clone(r.UnknownTools), func(c provider.InterruptedToolSummary) bool {
			record := latest[c.ID]
			return record != nil && record.State == provider.ToolRunUserConfirmed
		})
		for _, c := range r.UnknownTools {
			seen[c.ID] = true
		}
	}
	for _, record := range a.PendingToolRecovery() {
		if seen[record.Identity.CallID] {
			continue
		}
		if r == nil {
			r = &provider.InterruptedTurnRecovery{Pending: true}
		}
		r.UnknownTools = append(r.UnknownTools, provider.InterruptedToolSummary{ID: record.Identity.CallID, Name: record.Identity.CanonicalTool})
		seen[record.Identity.CallID] = true
	}
	for _, m := range a.Session().Snapshot() {
		for _, c := range m.ToolCalls {
			if c.Recovery == nil || c.Recovery.State != provider.ToolRunUserConfirmed || latest[c.ID] != c.Recovery {
				continue
			}
			if r == nil {
				r = &provider.InterruptedTurnRecovery{Pending: true}
			}
			r.UserConfirmedTools = append(r.UserConfirmedTools, provider.InterruptedToolSummary{ID: c.ID, Name: c.Recovery.Identity.CanonicalTool})
		}
	}
	return r
}

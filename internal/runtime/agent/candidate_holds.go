package agent

import "reasonix/internal/contract/provider"

// candidateHolds splits a candidate of total tokens into the parts a fold
// controls and the rest. The remainder is the recent tail, which is whatever
// the fold left in place after the summary.
func (a *contextWindow) candidateHolds(total, prefix int, kept []provider.Message, summary string) candidateHolds {
	h := candidateHolds{
		Prefix:  prefix,
		Kept:    a.estimatedPromptTokens(kept),
		Summary: a.estimatedPromptTokens([]provider.Message{{Role: provider.RoleUser, Content: summary}}),
	}
	h.Tail = max(total-h.Prefix-h.Kept-h.Summary, 0)
	return h
}

// acceptAttributed is acceptCheckpointCandidate whose size refusals name what
// the candidate is made of; the split is only worked out when one refuses.
func (a *contextWindow) acceptAttributed(trigger string, scope compactionScope, sourceTokens, candidateTokens, fixedPrefixTokens int, kept []provider.Message, summary string) error {
	err := a.acceptCheckpointCandidate(trigger, scope, sourceTokens, candidateTokens, fixedPrefixTokens)
	if err == nil {
		return nil
	}
	return attribute(err, a.candidateHolds(candidateTokens, fixedPrefixTokens, kept, summary))
}

// foldCannotFit refuses, before any summary is paid for, a fold whose
// candidate cannot get under the trigger whatever the summary says: the fixed
// prefix, the messages the keep policy protects and the recent tail already
// reach it.
func (a *contextWindow) foldCannotFit(trigger string, msgs []provider.Message, head, start int) (CompactionNoopReason, error) {
	prefix := a.estimatedPromptTokens(a.providerProjectionMessages(msgs[:head]))
	limit := a.compactTrigger()
	if a.effectiveContextWindow() <= 0 || limit <= 0 {
		return "", nil
	}
	if prefix >= limit {
		return NoopFixedPrefixAboveTrigger, rejectCheckpoint(NoopFixedPrefixAboveTrigger, "fixed prefix (%d tokens) already exceeds trigger (%d)", prefix, limit)
	}
	if trigger == CompactionTriggerManual && a.estimatedPromptTokens(msgs) < limit {
		return "", nil
	}
	kept, _ := a.partitionFold(msgs[head:start], msgs[start:])
	held := candidateHolds{Prefix: prefix, Kept: a.estimatedPromptTokens(kept), Tail: a.estimatedPromptTokens(msgs[start:])}
	if held.Prefix+held.Kept+held.Tail < limit {
		return "", nil
	}
	err := rejectCheckpoint(NoopCandidateAboveTrigger, "candidate cannot get under trigger %d even with an empty summary", limit)
	return NoopCandidateAboveTrigger, attribute(err, held)
}

package provider

import "reasonix/internal/nilutil"

// ReasoningReplayConverter is an adapter-owned, loss-aware request-view
// conversion. It must not mutate the input or fabricate provider proof. A true
// result means the converted message can be sent without protocol recovery.
type ReasoningReplayConverter interface {
	ConvertReasoningReplay(Message) (Message, bool)
}

func completeReplayEvidence(m Message) bool {
	if m.ReasoningStatus == "incomplete" || m.ReasoningStatus == "in_progress" {
		return false
	}
	switch m.ReasoningState {
	case "", ReasoningEmpty, ReasoningComplete:
		return true
	default:
		return false
	}
}

func compatibleReplayMessage(p Provider, m Message) (Message, bool) {
	if nilutil.IsNil(p) || m.Role != RoleAssistant || !completeReplayEvidence(m) {
		return m, false
	}
	converter, ok := p.(ReasoningReplayConverter)
	if !ok {
		return m, false
	}
	return converter.ConvertReasoningReplay(m)
}

// projectCompatibleReplay preserves the canonical history and allocates only
// when an adapter actually converts an otherwise unsupported historical block.
func projectCompatibleReplay(p Provider, msgs []Message, prefix int) ([]Message, bool) {
	work := msgs
	changed := false
	for i, m := range msgs[:prefix] {
		converted, ok := compatibleReplayMessage(p, m)
		if !ok {
			continue
		}
		if !changed {
			work = append([]Message(nil), msgs...)
			changed = true
		}
		work[i] = converted
	}
	return work, changed
}

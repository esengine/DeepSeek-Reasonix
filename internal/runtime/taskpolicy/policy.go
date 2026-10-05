// Package taskpolicy builds the host-side TaskPolicy that freezes one turn's
// verification level before the first model request. It never calls a
// classification model, and it never reads the user's wording: a turn is bound
// only by the frozen role setting and by plan mode, both structural signals.
//
// Limits the user wants come from the permission system, which sees the actual
// command. How much review a change owes is decided after the fact by the
// mutation receipts it produced — see evidence.Ledger.MutationRiskAfter.
package taskpolicy

import (
	"strconv"
	"strings"

	"reasonix/internal/contract/agentpreset"
)

// PolicyVersion is the diagnostic version stamped on every TaskPolicy.
const PolicyVersion = agentpreset.PolicyVersion

// Verification is the verification level for this turn.
type Verification = agentpreset.VerificationLevel

const (
	VerifyNone     = agentpreset.VerifyNone
	VerifyTargeted = agentpreset.VerifyTargeted
	VerifyFull     = agentpreset.VerifyFull
)

// Input is the host-trusted material used to derive a TaskPolicy.
type Input struct {
	// Preset is the frozen role setting for this turn.
	Preset agentpreset.AgentPreset
	// PlanMode is the collaboration plan-mode flag.
	PlanMode bool
}

// TaskPolicy is the authoritative host policy for one turn.
type TaskPolicy struct {
	Preset       agentpreset.AgentPreset
	Verification Verification
	// PlanModeReadOnly records that this turn was derived in plan mode. It is a
	// diagnostic and prompt signal only: planmode owns whether the phase admits
	// a call, and a second answer here is how the two drifted apart.
	PlanModeReadOnly bool
	// PolicyVersion is diagnostic only.
	PolicyVersion int
}

// Derive builds a TaskPolicy from host-trusted input without model calls.
func Derive(in Input) TaskPolicy {
	preset := agentpreset.Normalize(string(in.Preset))
	return TaskPolicy{
		Preset:           preset,
		Verification:     agentpreset.PolicyOf(preset).VerificationPolicy.Level,
		PlanModeReadOnly: in.PlanMode,
		PolicyVersion:    PolicyVersion,
	}
}

// ExecutionPolicyBlock renders the short provider-visible transient user block
// that freezes the role setting for this turn. Callers persist it in Message
// Content and keep the original user text in RawContent.
func ExecutionPolicyBlock(p TaskPolicy) string {
	var b strings.Builder
	b.WriteString(`<execution-policy preset="`)
	b.WriteString(p.Preset.String())
	b.WriteString(`" version="`)
	b.WriteString(strconv.Itoa(p.PolicyVersion))
	b.WriteString(`">`)
	b.WriteByte('\n')
	b.WriteString("verify=")
	b.WriteString(verifyName(p.Verification))
	if p.PlanModeReadOnly {
		b.WriteString("\nconstraint=plan-mode-read-only")
	}
	b.WriteString("\n</execution-policy>")
	return b.String()
}

func verifyName(v Verification) string {
	switch v {
	case VerifyTargeted:
		return "targeted"
	case VerifyFull:
		return "full"
	default:
		return "none"
	}
}

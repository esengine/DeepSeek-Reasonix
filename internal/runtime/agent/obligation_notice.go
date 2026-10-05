// Telling the model what the host now owes, at the moment it changes. The text
// is a projection of the ledger's own answer, so a turn whose notice is dropped
// is still gated on the same debts — nothing here is load-bearing for
// correctness, and nothing downstream may treat it as the record.
package agent

import (
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

// withObligationNotice is every notice a call owes the model about the host's
// debts: what it changed, and why a command that ran code settled none of them.
func (a *Agent) withObligationNotice(result string, before []evidence.Obligation, execution *tool.ShellExecution, readOnly bool) string {
	after := a.obligations()
	result = withObligationDelta(result, evidence.DiffObligations(before, after))
	if a.turn.uncountedCheckNoted || !uncountedCheck(after, execution, readOnly) {
		return result
	}
	a.turn.uncountedCheckNoted = true
	return strings.TrimRight(result, "\n") + "\n\n" + uncountedCheckNotice
}

// uncountedCheckNotice says why a command did not settle the verification the
// work owes. Without it the debt reads as a new change and nothing more, and
// the model concludes its check ran.
var uncountedCheckNotice = "host: this command ran code the host cannot prove read-only and is not a recognized check, " +
	"so it counts as a possible change rather than a check and the work is still unverified. " +
	evidence.VerificationCommandSummary()

// uncountedCheck reports a shell command that ran code, was not a recognized
// check, and left the work owing verification.
func uncountedCheck(after []evidence.Obligation, execution *tool.ShellExecution, readOnly bool) bool {
	if readOnly || execution == nil || execution.Verification != tool.ShellVerificationNotVerification {
		return false
	}
	for _, o := range after {
		if o.Kind == evidence.ObligationStaleVerification {
			return true
		}
	}
	return false
}

// withObligationDelta appends what this call did to the host's debts. A call
// can settle one and create another at once — establishing a change's scope
// discharges the unproven mutation and stales every check before it — so both
// halves travel together rather than as two notices that could arrive apart.
func withObligationDelta(result string, delta evidence.ObligationDelta) string {
	if delta.Empty() {
		return result
	}
	var b strings.Builder
	b.WriteString("host obligations changed:")
	for _, o := range delta.Discharged {
		b.WriteString("\n- settled: " + string(o.Kind))
		if o.Cause != "" {
			b.WriteString(" (" + firstLine(o.Cause) + ")")
		}
	}
	for _, o := range delta.Added {
		b.WriteString("\n- owed: " + string(o.Kind))
		if o.Cause != "" {
			b.WriteString(" (" + firstLine(o.Cause) + ")")
		}
		if o.Discharge != "" {
			b.WriteString("\n  settled by: " + o.Discharge)
		}
	}
	if strings.TrimSpace(result) == "" {
		return b.String()
	}
	return strings.TrimRight(result, "\n") + "\n\n" + b.String()
}

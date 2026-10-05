// subagent_handoff.go — how a delegated child closed, counted.
package event

import "reasonix/internal/base/nilutil"

// SubagentHandoffAudit is one child run's closing protocol, counted. It exists
// to answer whether a typed handoff is submitted naturally, before anything is
// built to insist on one — so it observes and never decides. Content-free:
// counts, enums and rounds, never a prompt, a path or a summary.
type SubagentHandoffAudit struct {
	// Entrance names the delegation path (read_only_task, fleet, …) and Depth
	// how deep the child sits. An aggregate hides an entrance that is failing.
	Entrance string
	Depth    int
	ReadOnly bool
	// Expected is set where the host actually appended the contract, never
	// inferred from a registry: the denominator has to be the host's own
	// instruction, not a guess about one.
	Expected bool
	// Exit says how the run ended. A provider timeout that produced no report
	// is not a compliance failure and must leave the denominator.
	Exit string // completed | error | cancelled | no_answer

	// Attempts counts complete_subtask calls, Accepted the ones the tool took,
	// Malformed the ones it refused. Missing with attempts is a schema
	// problem; missing without them is a protocol one.
	Attempts  int
	Accepted  int
	Malformed int

	// ReportRound and FinalRound say whether the report actually closed the
	// run: the contract asks for it as the final call, and "submitted it
	// eventually" is a different behaviour from "closed with it".
	ReportRound          int
	FinalRound           int
	ToolCallsAfterReport int

	// Closed and NeedsWork are the verdicts the host issued, counted when each
	// was issued. Accepted only says a call was well-formed; these say whether
	// the host considered the sub-task done.
	Closed    int
	NeedsWork int

	// What the host made of the claim, read from the adjudicated report rather
	// than re-derived here.
	ClaimedStatus     string
	AdjudicatedStatus string
	LoweredClaims     int
	Criteria          int
	Evidence          int
	Unresolved        int
}

// SubagentHandoffSink is an optional sink capability; implementations must keep
// it content-free, like every other audit channel.
type SubagentHandoffSink interface {
	RecordSubagentHandoff(SubagentHandoffAudit)
}

// RecordSubagentHandoff forwards one child's closing protocol only to sinks
// that explicitly opt in. Ordinary UI sinks receive nothing.
func RecordSubagentHandoff(s Sink, a SubagentHandoffAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if hs, ok := s.(SubagentHandoffSink); ok {
		hs.RecordSubagentHandoff(a)
	}
}

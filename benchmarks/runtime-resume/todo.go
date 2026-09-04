package main

import (
	"fmt"
	"slices"
)

// Contract verdicts for an arm that asserts a fact rather than comparing two
// readings: the note either satisfies what todoIdentityProjection claims about
// itself, or it does not.
const (
	verdictHolds    = "holds"
	verdictViolated = "VIOLATED"
)

// todoRows judge the host's step-identity note against the three things it
// claims: there is one of it, it rides the turn tail, and it names the identity
// the host holds now. Each is checked on both sides of the process boundary.
func todoRows(before, after Observation) []row {
	return []row{
		todoRow("host identity notes in the view", "exactly one note reaches the model",
			before, after, noteCount, exactlyOnce),
		todoRow("where the identity note sits", "the note rides the live tail, as a turn-tail note",
			before, after, notePlacement, ridesTheTail),
		todoRow("the identity the note carries", "the note names the host's current step ids",
			before, after, noteIdentity, matchesHost),
	}
}

func todoRow(semantic, contract string, before, after Observation,
	render func(Observation) string, holds func(Observation) bool) row {
	verdict := verdictHolds
	switch {
	case before.TodoNotes.ViewLen == 0 || after.TodoNotes.ViewLen == 0:
		verdict = verdictNotMeasured
	case !holds(before) || !holds(after):
		verdict = verdictViolated
	}
	return row{
		Semantic: semantic, Authority: "agent todoState, projected into the fold's output",
		Artifact:       "<stem>.context.json (the note is frozen into the body)",
		Reconstruction: contract,
		Before:         render(before), After: render(after), Verdict: verdict,
	}
}

func noteCount(o Observation) string { return fmt.Sprintf("%d", o.TodoNotes.Count) }

func exactlyOnce(o Observation) bool { return o.TodoNotes.Count == 1 }

// notePlacement reports the note's index against the end of the frozen body.
// An index below it means the note was frozen ahead of messages the splice adds
// after it, which is the opposite of riding the tail.
func notePlacement(o Observation) string {
	n := o.TodoNotes
	if n.Count == 0 {
		return "no note"
	}
	where := "after the live tail"
	if n.Indexes[len(n.Indexes)-1] < n.BodyLen {
		where = "inside the frozen body"
	}
	return fmt.Sprintf("index %v of %d, body ends at %d — %s", n.Indexes, n.ViewLen, n.BodyLen, where)
}

func ridesTheTail(o Observation) bool {
	n := o.TodoNotes
	return n.Count > 0 && n.Indexes[len(n.Indexes)-1] >= n.BodyLen
}

func noteIdentity(o Observation) string {
	n := o.TodoNotes
	return fmt.Sprintf("note %s / host %s", join(dedupe(n.IDs)), join(n.HostIDs))
}

func matchesHost(o Observation) bool {
	n := o.TodoNotes
	if n.Count == 0 || len(n.HostIDs) == 0 {
		return false
	}
	return slices.Equal(dedupe(n.IDs), n.HostIDs)
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

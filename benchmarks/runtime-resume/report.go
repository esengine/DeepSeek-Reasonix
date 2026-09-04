package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Verdicts. They are deliberately not booleans: "the host holds it verbatim"
// and "a fold over canonical artifacts happens to agree" are different claims
// about durability, and a row that was never established supports neither.
const (
	verdictPersisted   = "persisted-direct"
	verdictExact       = "reconstructed-exact"
	verdictLossy       = "reconstructed-lossy"
	verdictLost        = "lost"
	verdictNotMeasured = "not-measured"
	verdictChanged     = "changed"
	verdictStable      = "stable"
)

type row struct {
	Semantic       string `json:"semantic"`
	Authority      string `json:"authority"`
	Artifact       string `json:"artifact"`
	Reconstruction string `json:"reconstruction"`
	Before         string `json:"before"`
	After          string `json:"after"`
	Verdict        string `json:"verdict"`
}

type armResult struct {
	Arm     string       `json:"arm"`
	Asks    string       `json:"asks"`
	Invalid string       `json:"invalid,omitempty"`
	Rows    []row        `json:"rows"`
	Extra   *Observation `json:"extra,omitempty"`
	Before  Observation  `json:"before"`
	After   Observation  `json:"after"`
}

func classify(a arm, extra *Observation, before, after Observation) armResult {
	res := armResult{Arm: a.name, Asks: a.asks, Extra: extra, Before: before, After: after}
	prefix := prefixRow(before, after)
	res.Rows = append(res.Rows, prefix)
	if a.name == "system-swap" && prefix.Verdict != verdictChanged {
		res.Invalid = "the lever did not move the stable prefix, so this arm measures nothing"
	}
	if a.name == "exact" && prefix.Verdict != verdictStable {
		res.Invalid = "the stable prefix moved without a lever, so this arm is not the control it claims"
	}
	res.Rows = append(res.Rows,
		valueRow("system row in the session", "the transcript's own leading system message",
			"<stem>.jsonl", "loaded with the transcript",
			before.SystemHash, after.SystemHash, true),
		valueRow("canonical transcript", "session transcript + event log",
			"<stem>.jsonl, <stem>.events.jsonl", "LoadSession replays the append log",
			before.Transcript.Digest, after.Transcript.Digest, true),
		valueRow("active goal identity", "goalMachine (control)",
			"<stem>.goal-state.json", "restoreFromState on Resume",
			before.Goal.Goal, after.Goal.Goal, true),
		valueRow("goal status", "goalMachine (control)",
			"<stem>.goal-state.json", "restoreFromState on Resume",
			before.Goal.Status, after.Goal.Status, true),
		valueRow("todo host identity", "agent todoState (host-owned, never in the prompt)",
			"none verbatim; goal-state.json snapshots a terminal goal's list",
			"rebuilt from the transcript's todo_write / complete_step receipts",
			todoIDs(before), todoIDs(after), false),
		valueRow("open decisions", "approvalManager (control, in-memory)",
			"none", "none: derived from live pending approvals/asks",
			decisionIDs(before), decisionIDs(after), false),
		valueRow("compaction projection (stored)", "compaction sidecar",
			"<stem>.context.json", "LoadProjectionSidecar on bind",
			sidecarClaim(before), sidecarClaim(after), true),
		useRow("compaction projection (in use)", before, after),
		valueRow("run graph nodes", "event sink (GraphDelta)",
			"none observed", "fold GraphDelta events, if any survive",
			graphNodes(before), graphNodes(after), false),
		valueRow("run graph grants", "event sink (GraphDelta)",
			"none observed", "fold GraphDelta events, if any survive",
			graphMap(before.Graph.Grants), graphMap(after.Graph.Grants), false),
		valueRow("run graph wait causes", "event sink (GraphDelta)",
			"none observed", "fold GraphDelta events, if any survive",
			graphMap(before.Graph.Waits), graphMap(after.Graph.Waits), false),
	)
	switch {
	case a.name == armRefoldIntoBody && extra != nil:
		res.Rows = append(res.Rows, refoldRows(*extra, before, after, probeTurns+refoldTurns)...)
		if extra.Sidecar.Messages == 0 {
			res.Invalid = "the first fold stored no body, so the second had none to reach into"
		}
	case (a.name == armTailRewind || a.name == armCoveredRewind) && extra != nil:
		res.Rows = append(res.Rows, useRow("projection across the rewind, same process", *extra, before))
		res.Rows = append(res.Rows, rewindLandingRow(a.name, *extra, before))
		res.Invalid = rewindLandedWrong(a.name, *extra, before)
	}
	return res
}

// prefixRow is the arm's own control rather than a durability claim: it reports
// whether the stable prefix moved, which is the thing each arm sets up.
func prefixRow(before, after Observation) row {
	verdict := verdictStable
	if before.BootSystemHash != after.BootSystemHash {
		verdict = verdictChanged
	}
	return row{
		Semantic: "stable prefix identity (composed)", Authority: "boot assembly (recomposed per process)",
		Artifact: "none (composed from config, memory and instructions)", Reconstruction: "recomposed at boot",
		Before: before.BootSystemHash, After: after.BootSystemHash, Verdict: verdict,
	}
}

func valueRow(semantic, authority, artifact, reconstruction, before, after string, persisted bool) row {
	r := row{Semantic: semantic, Authority: authority, Artifact: artifact,
		Reconstruction: reconstruction, Before: before, After: after}
	switch {
	case before == "" || before == "-":
		r.Verdict = verdictNotMeasured
	case after == "" || after == "-":
		r.Verdict = verdictLost
	case before != after:
		r.Verdict = verdictLossy
	case persisted:
		r.Verdict = verdictPersisted
	default:
		r.Verdict = verdictExact
	}
	return r
}

func todoIDs(o Observation) string {
	var ids []string
	for _, t := range o.Todos {
		id := t.StepID
		if id == "" {
			id = "(no step_id)"
		}
		ids = append(ids, id+":"+t.Status)
	}
	return join(ids)
}

func decisionIDs(o Observation) string {
	var ids []string
	for _, d := range o.Decisions {
		ids = append(ids, string(d.Kind)+":"+d.ID)
	}
	return join(ids)
}

func sidecarClaim(o Observation) string {
	if !o.Sidecar.Present {
		return ""
	}
	return fmt.Sprintf("v%d msgs=%d covered=%d hash=%s",
		o.Sidecar.ProjectionVersion, o.Sidecar.Messages, o.Sidecar.CoveredCount, short(o.Sidecar.CoveredPrefixHash))
}

// rewindLandingRow says where the rewind actually landed relative to the fold.
// An arm that names a side and does not reach it is not measuring that side.
func rewindLandingRow(arm string, prerewind, after Observation) row {
	side := "above the fold boundary"
	if after.Transcript.Messages < prerewind.Sidecar.CoveredCount {
		side = "below the fold boundary"
	}
	return row{
		Semantic: "where the rewind landed", Authority: "checkpoint boundary against the sidecar",
		Artifact: "none: derived", Reconstruction: "canonical length after the rewind against CoveredCount before it",
		Before: fmt.Sprintf("covered=%d", prerewind.Sidecar.CoveredCount),
		After:  fmt.Sprintf("canonical=%d, %s", after.Transcript.Messages, side), Verdict: verdictStable,
	}
}

// rewindLandedWrong invalidates an arm that aimed at one side of the fold and
// reached the other. covered-rewind stopped removing folded history the moment
// coverage became a real boundary, and nothing in the matrix said so.
func rewindLandedWrong(arm string, prerewind, after Observation) string {
	below := after.Transcript.Messages < prerewind.Sidecar.CoveredCount
	switch {
	case arm == armCoveredRewind && !below:
		return "the rewind landed above the fold boundary, so it removed no folded history"
	case arm == armTailRewind && below:
		return "the rewind landed below the fold boundary, so it is not a tail-only truncation"
	}
	return ""
}

// useRow turns on whether the next request would ride the projection, not on
// the token counts beside it: a tail edit legitimately moves those, and letting
// them decide the verdict reported a surviving projection as a loss.
func useRow(semantic string, before, after Observation) row {
	r := valueRow(semantic, "projectionValid over the covered prefix",
		"none: a judgement, not a stored value",
		"revalidated against the canonical prefix on every read",
		projectionState(before), projectionState(after), false)
	r.Before, r.After = projectionUse(before), projectionUse(after)
	return r
}

// projectionState is the compared value: in use, not in use, or never measured.
func projectionState(o Observation) string {
	switch o.Context.CheckpointState {
	case "":
		return ""
	case "none":
		return "NOT-in-use"
	}
	return "in-use"
}

// projectionUse reads the host's own judgement rather than the stored claim.
// CheckpointState is "none" whenever validation failed, which is the difference
// between a sidecar that loaded and one the next request will actually ride.
// The label itself is not compared — "applied" and "restored" are the same
// answer reached from different sides — so the row turns on use and size.
func projectionUse(o Observation) string {
	if o.Context.CheckpointState == "" {
		return ""
	}
	use := "in-use"
	if o.Context.CheckpointState == "none" {
		use = "NOT-in-use"
	}
	return fmt.Sprintf("%s projected=%d canonical=%d", use,
		o.Context.ProjectedTokens, o.Context.CanonicalTokens)
}

func graphNodes(o Observation) string {
	if len(o.Graph.Nodes) == 0 {
		return ""
	}
	var ids []string
	for _, n := range o.Graph.Nodes {
		ids = append(ids, n.ID+":"+string(n.State))
	}
	return join(ids)
}

func graphMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return join(out)
}

func join(in []string) string {
	if len(in) == 0 {
		return ""
	}
	return strings.Join(in, ", ")
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func marshalResults(results []armResult) ([]byte, error) {
	return json.MarshalIndent(results, "", "  ")
}

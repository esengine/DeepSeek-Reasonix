package main

import (
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/agentgraph"
	"reasonix/internal/execgraph"
	"reasonix/internal/execjournal"
)

// What a lone delegation leaves behind, layer by layer. The columns are the
// ordinary ones: what the dying process held, and what the next one can still
// say. Nothing here is a fix — the arm exists to find which transition stops
// being recorded, because "single task has no graph" was the roadmap's guess
// and the live chain has since been unified.
func loneTaskRows(before, after Observation) []row {
	return []row{
		liveDelegationRow(before),
		parentStatusRow(before),
		durableOpeningRow(before, after),
		durableQueueRow(before, after),
		durableStartRow(before, after),
		durableSettleRow(before, after),
		delegationStoreRow(before, after),
		rebuiltDelegationRow(after),
		rebuiltDelegationIdentityRow(after),
		delegationInterruptionRow(before, after),
		identityJoinRow(before),
		fanOutControlRow(before),
	}
}

// ofLoneTask reports whether an id belongs to the delegation under measurement.
// The queued arm fills its ceiling with a fan-out, and a fan-out records
// everything — so without this the rows would read that record as the lone
// task's own and report a gap as closed.
func ofLoneTask(id string) bool {
	return id == taskCallID || strings.HasPrefix(id, taskCallID+"/")
}

const (
	liveAuthority    = "event sink (GraphDelta), in-process"
	journalAuthority = "execjournal, written by the orchestration"
	storeAuthority   = "sub-agent store"
	rebuildAuthority = "execgraph.Rebuild over journal + store"
)

// liveDelegationRow is what the dying process actually drew. A backgrounded run
// draws nothing at all — openDelegationGraph returns early for it — so this row
// separates "the graph was lost" from "there was never a node".
func liveDelegationRow(before Observation) row {
	var drawn []string
	for _, n := range before.Graph.Nodes {
		if n.Kind == agentgraph.KindWorker && ofLoneTask(n.ID) {
			drawn = append(drawn, n.ID+":"+string(n.State))
		}
	}
	sort.Strings(drawn)
	return row{
		Semantic: "the delegation the live graph drew", Authority: liveAuthority,
		Artifact: "none observed", Reconstruction: "in-process only",
		Before: orNone(join(drawn)), After: "—", Verdict: liveVerdict(len(drawn) > 0),
	}
}

func liveVerdict(drawn bool) string {
	if drawn {
		return verdictStable
	}
	return verdictNotMeasured
}

// parentStatusRow is the other live surface, and the only one a handed-off run
// has: the statuses the parent tool call was told.
func parentStatusRow(before Observation) row {
	var told []string
	for id, phases := range before.Progress {
		if ofLoneTask(id) {
			told = append(told, id+":"+strings.Join(phases, "→"))
		}
	}
	sort.Strings(told)
	return row{
		Semantic: "what the parent call was told", Authority: "subagent progress (ToolProgress)",
		Artifact: "none observed", Reconstruction: "in-process only",
		Before: orNone(join(told)), After: "—", Verdict: liveVerdict(len(told) > 0),
	}
}

func durableOpeningRow(before, after Observation) row {
	return valueRow("durable opening", journalAuthority, "<stem>.execution.jsonl",
		"read back by ExecutionHistory", executionIDs(before), executionIDs(after), true)
}

func durableQueueRow(before, after Observation) row {
	return valueRow("durable queue refusal", journalAuthority, "<stem>.execution.jsonl",
		"the scheduler's first refusal, with its cause",
		queuedCauses(before), queuedCauses(after), true)
}

func durableStartRow(before, after Observation) row {
	return valueRow("durable slot grant", journalAuthority, "<stem>.execution.jsonl",
		"written before the child can act", startedIDs(before), startedIDs(after), true)
}

func durableSettleRow(before, after Observation) row {
	return valueRow("durable settlement", journalAuthority, "<stem>.execution.jsonl",
		"the orchestration letting go of the execution",
		settledIDs(before), settledIDs(after), true)
}

// delegationStoreRow is the layer that already survives for a fan-out item. It
// is the reason this arm cannot be read off the graph alone: a store record with
// no opening leaves the run's grant, worker and scheduling unaccounted for.
func delegationStoreRow(before, after Observation) row {
	return valueRow("child the store kept", storeAuthority, "subagents/<ref>.meta.json",
		"ListSubagentsByParent over the transcript stem",
		childFacts(before), childFacts(after), true)
}

func rebuiltDelegationRow(after Observation) row {
	rebuilt := rebuiltGraph(after)
	var nodes []string
	for _, n := range rebuilt.Graph.Nodes {
		if n.Kind != agentgraph.KindGroup && ofLoneTask(n.ID) {
			nodes = append(nodes, n.ID+":"+string(n.State))
		}
	}
	sort.Strings(nodes)
	return valueRow("the node a restart rebuilds", rebuildAuthority, "<stem>.execution.jsonl",
		"execgraph.Rebuild", liveWorkers(after), join(nodes), false)
}

// rebuiltDelegationIdentityRow is what a reader sees on the rebuilt node beyond
// its state. Grant and worker are opening facts: the store cannot answer them,
// so an arm that only compared states would call a hollow node exact.
func rebuiltDelegationIdentityRow(after Observation) row {
	var got []string
	for _, n := range rebuiltGraph(after).Graph.Nodes {
		if n.Kind == agentgraph.KindGroup || !ofLoneTask(n.ID) {
			continue
		}
		got = append(got, fmt.Sprintf("%s grant=%s model=%s effort=%s wait=%s",
			n.ID, orDash(string(n.Grant)), orDash(n.Model), orDash(n.Effort), orDash(string(n.Wait))))
	}
	sort.Strings(got)
	return row{
		Semantic: "grant, worker and wait on the rebuilt node", Authority: rebuildAuthority,
		Artifact: "<stem>.execution.jsonl", Reconstruction: "opening facts, which no store record carries",
		Before: "declared at the opening", After: orNone(join(got)),
		Verdict: rebuiltIdentityVerdict(got),
	}
}

func rebuiltIdentityVerdict(got []string) string {
	if len(got) == 0 {
		return verdictLost
	}
	return verdictExact
}

func delegationInterruptionRow(before, after Observation) row {
	var got []string
	for _, i := range rebuiltGraph(after).Interrupted {
		if ofLoneTask(i.Execution) {
			got = append(got, i.Execution+":"+interruptionKind(i))
		}
	}
	sort.Strings(got)
	return valueRow("the interruption a restart names", rebuildAuthority, "<stem>.execution.jsonl",
		"an execution with no owner is named, never redrawn as running",
		loneDelegationAtDeath(before), join(got), false)
}

// loneDelegationAtDeath is the delegation as the dying process last knew it,
// from whichever surface knew: a foreground run has a node, a handed-off one
// has only the store's record, and an arm whose rows read the wrong surface
// would report a loss the other one never had.
func loneDelegationAtDeath(o Observation) string {
	if live := liveWorkers(o); live != "" {
		return live
	}
	return childFacts(o)
}

func interruptionKind(i execgraph.Interruption) string {
	if i.Started {
		return execjournal.InterruptedDuringExecution
	}
	return execjournal.InterruptedBeforeStart
}

// identityJoinRow is the question that decides whether Step 8 needs an identity
// at all: the live node, the call it hung under, the parent the store recorded,
// and the execution the journal opened. Four spellings, or one identity.
func identityJoinRow(before Observation) row {
	live, call, stored, opened := identitiesAtDeath(before)
	joined := stored != "" && (stored == call || strings.HasPrefix(live, stored))
	verdict := verdictViolated
	if joined {
		verdict = verdictHolds
	}
	return row{
		Semantic: "one identity or four", Authority: "the graph, the call, the store and the journal",
		Artifact:       "subagents/<ref>.meta.json, <stem>.execution.jsonl",
		Reconstruction: "the store's parent call is what a rebuild joins on",
		Before:         fmt.Sprintf("node=%s call=%s", orDash(live), orDash(call)),
		After:          fmt.Sprintf("store parent=%s journal=%s", orDash(stored), orDash(opened)),
		Verdict:        verdict,
	}
}

// identitiesAtDeath reads the four spellings out of the dying process's own
// observation, so a join that only holds after a restart cannot be claimed.
func identitiesAtDeath(o Observation) (live, call, stored, opened string) {
	for _, n := range o.Graph.Nodes {
		if n.Kind == agentgraph.KindWorker && ofLoneTask(n.ID) {
			live, call = n.ID, n.ParentID
			break
		}
	}
	for id := range o.Progress {
		if call == "" && ofLoneTask(id) {
			call = id
		}
	}
	for _, f := range o.Children.Facts {
		if ofLoneTask(f.ParentToolCallID) {
			stored = f.ParentToolCallID
			break
		}
	}
	for _, e := range o.Executions {
		if ofLoneTask(e.ID) {
			opened = e.ID
			break
		}
	}
	return live, call, stored, opened
}

func executionIDs(o Observation) string {
	var out []string
	for _, e := range o.Executions {
		if ofLoneTask(e.ID) {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return join(out)
}

func queuedCauses(o Observation) string {
	var out []string
	for _, e := range o.Executions {
		if e.Queued() && ofLoneTask(e.ID) {
			out = append(out, e.ID+":"+orDash(e.Cause))
		}
	}
	sort.Strings(out)
	return join(out)
}

func startedIDs(o Observation) string {
	var out []string
	for _, e := range o.Executions {
		if e.Started() && ofLoneTask(e.ID) {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return join(out)
}

func settledIDs(o Observation) string {
	var out []string
	for _, e := range o.Executions {
		if !e.SettledAt.IsZero() && ofLoneTask(e.ID) {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return join(out)
}

func childFacts(o Observation) string {
	var out []string
	for _, f := range o.Children.Facts {
		if ofLoneTask(f.ParentToolCallID) {
			out = append(out, f.ParentToolCallID+":"+f.Status)
		}
	}
	sort.Strings(out)
	return join(out)
}

func liveWorkers(o Observation) string {
	var out []string
	for _, n := range o.Graph.Nodes {
		if n.Kind == agentgraph.KindWorker && ofLoneTask(n.ID) {
			out = append(out, n.ID+":"+string(n.State))
		}
	}
	sort.Strings(out)
	return join(out)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// fanOutControlRow is the positive control the queued arm carries with it: the
// fleet that fills its ceiling runs through the same scheduler in the same
// session, and records everything. What this arm reports missing is missing at
// the entry point, not in the machinery.
func fanOutControlRow(before Observation) row {
	var recorded []string
	for _, e := range before.Executions {
		if !ofLoneTask(e.ID) {
			recorded = append(recorded, e.ID+lifecycleOf(e))
		}
	}
	sort.Strings(recorded)
	verdict := verdictNotMeasured
	if len(recorded) > 0 {
		verdict = verdictPersisted
	}
	return row{
		Semantic: "a fan-out in the same session, same scheduler", Authority: journalAuthority,
		Artifact: "<stem>.execution.jsonl", Reconstruction: "control: the machinery this arm reports missing",
		Before: orNone(join(recorded)), After: "—", Verdict: verdict,
	}
}

func lifecycleOf(e execjournal.Entry) string {
	marks := ""
	if e.Queued() {
		marks += " queued=" + orDash(e.Cause)
	}
	if e.Started() {
		marks += " started"
	}
	if !e.SettledAt.IsZero() {
		marks += " settled"
	}
	return marks
}

// loneTaskArmInvalid guards each arm's premise: the state it is built to die in
// has to have been reached, or its rows describe a different death.
func loneTaskArmInvalid(arm string, before Observation) string {
	phases := strings.Join(before.Progress[taskCallID], "→")
	switch arm {
	case armTaskCompleted:
		if !strings.Contains(phases, "completed") && len(before.Children.Facts) == 0 {
			return "the delegation never completed, so this arm measures a different death"
		}
	case armTaskRunning:
		if !holdsState(before, string(agentgraph.StateRunning)) {
			return "nothing was executing when the process died"
		}
	case armTaskBgQueued:
		if !strings.HasSuffix(phases, subagentQueued) {
			return "the delegation was not left queued for a slot: " + orNone(phases)
		}
	case armTaskBgRunning:
		if !strings.Contains(phases, subagentRunning) {
			return "the delegation never entered its job: " + orNone(phases)
		}
	}
	return ""
}

package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/agentgraph"
	"reasonix/internal/execgraph"
	"reasonix/internal/execjournal"
)

// How a run graph can stand after the process that built it exits. Three are
// designs a host may hold; two are defects, and not the same defect. Losing the
// graph costs a reader an explanation; a node still drawn as running with no
// owner costs them the truth — reviving actionable state is more dangerous than
// dropping it, and a running node is actionable state.
const (
	graphExact       = "reconstructed-exact"  // nodes, edges, grants, waits, states all back
	graphInterrupted = "interrupted-explicit" // the fan-out is legible and the live child is marked cut
	graphLossy       = "reconstructed-lossy"  // it is back, minus semantics nothing can re-derive
	graphLostSilent  = "LOST-SILENT"          // gone, with no provenance that it ever existed
	graphGhost       = "GHOST-RUNNING"        // still says running, with no live owner anywhere
	graphNotMeasured = "not-measured"         // the construct phase never established the shape
)

// graphRows classify what a restart inherited from a fan-out. There are two
// outcome rows rather than one because a fan-out that died mid-flight holds two
// populations with different answers: the items that had already settled, whose
// answers the store owns, and the one still executing, which nothing on disk
// has ever been told about. One label over both reports the better half.
func graphRows(before, after Observation) []row {
	return []row{
		ghostRow(before, after),
		dispatchTurnRow(before, after),
		settledOutcomeRow(before, after),
		interruptedOutcomeRow(before, after),
		graphStructureRow(before, after),
		graphSemanticsRow(before, after),
		childProvenanceRow(before, after),
		executionJournalRow(before, after),
		rebuildStateRow(before, after),
		rebuildTopologyRow(before, after),
		rebuildEnvelopeRow(before, after),
		rebuildInterruptedRow(before, after),
		rebuildIdentityRow(before, after),
		executionTopologyRow(before, after),
		executionKindRow(before, after),
		executionContextRow(before, after),
		graphObligationRow(before, after),
	}
}

// runningWorkers are the nodes that were executing when the process died. They
// are the arm's premise: an arm that establishes none has measured a fan-out
// nobody interrupted, whatever else it recorded.
func runningWorkers(o Observation) []string {
	var out []string
	for _, n := range o.Graph.Nodes {
		if n.Kind == agentgraph.KindWorker && n.State == agentgraph.StateRunning {
			out = append(out, n.ID)
		}
	}
	return out
}

// executedWorkers are the items that ran to a terminal state. Adopted nodes are
// excluded on purpose: nothing executed under them, so the store owes them no
// record and counting them would read a correct store as incomplete.
func executedWorkers(o Observation) []string {
	var out []string
	for _, n := range o.Graph.Nodes {
		if n.Kind != agentgraph.KindWorker || n.State == "" || n.State == agentgraph.StateAdopted {
			continue
		}
		if n.State.Terminal() {
			out = append(out, n.ID)
		}
	}
	return out
}

// ghostRow asks the one question with a single acceptable answer: does anything
// the new process can read still present this work as running? A graph node and
// a store record are both such a claim, and neither has an owner behind it —
// the goroutine that would have settled them died with the first process.
func ghostRow(before, after Observation) row {
	ghosts := append(runningWorkers(after), childrenInState(after, string(agent.SubagentRunning))...)
	verdict := verdictHolds
	if len(ghosts) > 0 {
		verdict = verdictViolated
	}
	return row{
		Semantic: "work still presented as running", Authority: "the graph and the sub-agent store",
		Artifact:       "subagents/<ref>.meta.json",
		Reconstruction: "nothing may read as running once its owner is gone",
		Before:         orNone(join(runningWorkers(before))) + " running at death",
		After:          orNone(join(ghosts)), Verdict: verdict,
	}
}

// dispatchTurnRow is the row underneath every other loss here: whether the turn
// that opened the fan-out reached the transcript at all. A turn is appended
// when it ends, so a process that dies inside one takes the request with it —
// and a graph reconstruction has nothing to attach to even in principle.
func dispatchTurnRow(before, after Observation) row {
	verdict := verdictPersisted
	switch {
	case !before.FanOutTurn:
		verdict = verdictNotMeasured
	case !after.FanOutTurn:
		verdict = verdictLost
	}
	return row{
		Semantic: "the turn that dispatched the fan-out", Authority: "session transcript + event log",
		Artifact: "<stem>.jsonl, <stem>.events.jsonl", Reconstruction: "appended when the turn ends",
		Before: recordedText(before), After: recordedText(after), Verdict: verdict,
	}
}

func recordedText(o Observation) string {
	if o.FanOutTurn {
		return fmt.Sprintf("in the transcript (%d rows)", o.Transcript.Messages)
	}
	return fmt.Sprintf("absent (%d rows)", o.Transcript.Messages)
}

// settledOutcomeRow classifies the items that had already finished. Their
// answers are what a later turn reads back, so this is the row that decides
// whether a fan-out's history is durable at all.
func settledOutcomeRow(before, after Observation) row {
	outcome, verdict := settledOutcome(before, after)
	return row{
		Semantic: "items that had settled before the death", Authority: "the graph fold and the sub-agent store",
		Artifact:       "subagents/<ref>.meta.json",
		Reconstruction: "one durable child record per item that executed",
		Before:         fmt.Sprintf("%d executed: %s", len(executedWorkers(before)), graphSummary(before)),
		After:          outcome, Verdict: verdict,
	}
}

func settledOutcome(before, after Observation) (string, string) {
	executed := len(executedWorkers(before))
	switch {
	case len(before.Graph.Nodes) == 0:
		return graphNotMeasured, verdictNotMeasured
	case graphIdentical(before, after):
		return graphExact, verdictExact
	case executed == 0:
		return graphNotMeasured, verdictNotMeasured
	case len(after.Children.Facts) == 0:
		return graphLostSilent, verdictViolated
	case len(after.Children.Facts) < executed:
		return fmt.Sprintf("%s (%d of %d children)", graphLossy, len(after.Children.Facts), executed), verdictLossy
	default:
		return graphLossy, verdictLossy
	}
}

// interruptedOutcomeRow classifies the item that was still executing. This is
// the row the arm exists for: the graph is gone either way, and what separates
// a defensible design from a durable gap is whether anything the new process
// can read still says that work was opened and cut.
func interruptedOutcomeRow(before, after Observation) row {
	outcome, verdict := interruptedOutcome(before, after)
	return row{
		Semantic: "the item still executing at the death", Authority: "the sub-agent store and the transcript",
		Artifact:       "subagents/<ref>.meta.json, <stem>.jsonl",
		Reconstruction: "an interrupted child record, or an unsettled call the transcript still owes",
		Before:         orNone(join(runningWorkers(before))), After: outcome, Verdict: verdict,
	}
}

func interruptedOutcome(before, after Observation) (string, string) {
	switch {
	case len(before.Graph.Nodes) == 0 || len(runningWorkers(before)) == 0:
		return graphNotMeasured, verdictNotMeasured
	case len(runningWorkers(after)) > 0 || len(childrenInState(after, string(agent.SubagentRunning))) > 0:
		return graphGhost, verdictViolated
	case len(after.InterruptedExecutions) > 0:
		return graphInterrupted, verdictLossy
	case len(childrenInState(after, string(agent.SubagentInterrupted))) > 0:
		return graphInterrupted, verdictLossy
	case len(after.Obligation.UnansweredCalls) > 0 || after.Obligation.InterruptionMarked > 0:
		return graphInterrupted, verdictLossy
	default:
		return graphLostSilent, verdictViolated
	}
}

// graphIdentical compares the whole graph, not its node count. Two graphs with
// the same nodes and different grants are not the same graph, and a comparison
// that only counted would have called that exact.
func graphIdentical(before, after Observation) bool {
	return graphNodes(before) == graphNodes(after) &&
		graphEdges(before) == graphEdges(after) &&
		graphMap(before.Graph.Grants) == graphMap(after.Graph.Grants) &&
		graphMap(before.Graph.Waits) == graphMap(after.Graph.Waits)
}

// graphStructureRow is the shape a reader asks for: which nodes, and the typed
// edges between them. Spawn, depends, context and adopt are separate facts, and
// a restart that kept the nodes while losing the edges kept a list, not a graph.
func graphStructureRow(before, after Observation) row {
	return valueRow("run graph edges", "event sink (GraphDelta)",
		"none observed", "fold GraphDelta events, if any survive",
		graphEdges(before), graphEdges(after), false)
}

// graphSemanticsRow is the authority envelope and the reason a node waited.
// They are called out apart from the structure because nothing else in the
// system records them: a child's persisted metadata carries its model and its
// profile, and says nothing about what it was allowed to touch.
func graphSemanticsRow(before, after Observation) row {
	return valueRow("run graph grants and waits", "event sink (GraphDelta)",
		"none observed", "no durable source carries an authority envelope",
		graphEnvelope(before), graphEnvelope(after), false)
}

func graphEnvelope(o Observation) string {
	grants, waits := graphMap(o.Graph.Grants), graphMap(o.Graph.Waits)
	if grants == "" && waits == "" {
		return ""
	}
	return fmt.Sprintf("grants[%s] waits[%s]", orNone(grants), orNone(waits))
}

// childProvenanceRow is what the store still owns, which is the evidence behind
// an interrupted classification. It is reported as its own row so a reader can
// see the reconstruction the label stands on rather than trusting the label.
func childProvenanceRow(before, after Observation) row {
	return valueRow("delegated runs the store owns", "SubagentStore, swept at boot",
		"subagents/<ref>.meta.json", "ListSubagentsByParent; CleanupStaleRunning marks stale running children interrupted",
		childSummary(before), childSummary(after), true)
}

// graphObligationRow is the last fallback a reader has: a fleet call the
// transcript records with no result is the host saying a fan-out was opened and
// never settled, even when nothing else remembers it.
func graphObligationRow(before, after Observation) row {
	return row{
		Semantic: "fan-out calls the transcript never settled", Authority: "the canonical transcript",
		Artifact: "<stem>.jsonl", Reconstruction: "tool calls with no result",
		Before: orNone(join(before.Obligation.UnansweredCalls)),
		After:  orNone(join(after.Obligation.UnansweredCalls)), Verdict: verdictStable,
	}
}

// rebuiltGraph is what the production fold makes of this session's durable
// facts. It is the same call a host would make, given the same inputs a
// restart has: the journal, the store, and no live executions.
func rebuiltGraph(o Observation) execgraph.Result {
	children := make([]execgraph.ChildOutcome, 0, len(o.Children.Facts))
	for _, f := range o.Children.Facts {
		children = append(children, execgraph.ChildOutcome{
			Execution: f.ParentToolCallID, Ref: f.Ref, Status: f.Status,
		})
	}
	return execgraph.Rebuild(o.Executions, children, nil)
}

// rebuildStateRow compares the states the fold produces against the ones the
// dying process drew. Nodes whose owner disappeared are excluded and reported
// by their own row: putting those back as running is the one outcome this
// whole line of work exists to prevent.
func rebuildStateRow(before, after Observation) row {
	rebuilt := rebuiltGraph(after)
	cut := map[string]bool{}
	for _, i := range rebuilt.Interrupted {
		cut[i.Execution] = true
	}
	return valueRow("rebuilt node states", "execgraph.Rebuild over journal + store",
		"<stem>.execution.jsonl + subagents/<ref>.meta.json",
		"recomputed from durable facts, never restored",
		statesExcept(graphOf(before), cut), statesExcept(rebuilt.Graph, cut), false)
}

// rebuildTopologyRow compares every typed edge. Spawn and depends are stated by
// the openings, adopt names its source, and context is derived — so a mismatch
// here says which of those four is not yet recoverable.
func rebuildTopologyRow(before, after Observation) row {
	return valueRow("rebuilt topology", "execgraph.Rebuild over journal + store",
		"<stem>.execution.jsonl", "spawn and depends stated, adopt named, context derived",
		edgeSummary(graphOf(before)), edgeSummary(rebuiltGraph(after).Graph), false)
}

// rebuildEnvelopeRow is the authority and the wait provenance together: what
// each item was allowed to touch, and why admission was first refused.
func rebuildEnvelopeRow(before, after Observation) row {
	return valueRow("rebuilt grants and wait causes", "execgraph.Rebuild over journal + store",
		"<stem>.execution.jsonl", "recorded at the opening and at the first refusal",
		envelopeSummary(graphOf(before)), envelopeSummary(rebuiltGraph(after).Graph), false)
}

// rebuildInterruptedRow accounts for the nodes the state row leaves out. They
// are not dropped: the fold names them as interruptions, split by whether they
// had reached a slot. A node the dying process showed live and the rebuild
// mentions nowhere would be a loss the state row could not see.
func rebuildInterruptedRow(before, after Observation) row {
	rebuilt := rebuiltGraph(after)
	var got []string
	for _, i := range rebuilt.Interrupted {
		kind := execjournal.InterruptedBeforeStart
		if i.Started {
			kind = execjournal.InterruptedDuringExecution
		}
		got = append(got, i.Execution+":"+kind)
	}
	sort.Strings(got)
	return valueRow("nodes the rebuild will not call live", "execgraph.Rebuild over journal + store",
		"<stem>.execution.jsonl", "an execution with no owner is named, never redrawn as running",
		liveAtDeath(before), join(got), false)
}

// liveAtDeath is what the dying process still showed as someone's work.
func liveAtDeath(o Observation) string {
	var out []string
	for _, n := range o.Graph.Nodes {
		if n.Kind == agentgraph.KindGroup {
			continue
		}
		switch n.State {
		case agentgraph.StateRunning:
			out = append(out, n.ID+":"+execjournal.InterruptedDuringExecution)
		case agentgraph.StateQueued, agentgraph.StatePending:
			out = append(out, n.ID+":"+execjournal.InterruptedBeforeStart)
		}
	}
	sort.Strings(out)
	return join(out)
}

// rebuildIdentityRow is what a reader sees on a node beyond its state: the
// label it was dispatched under, and which worker ran it. Only one of those is
// recorded, so this row is where a remaining gap shows up rather than hiding
// inside an exact verdict about states.
func rebuildIdentityRow(before, after Observation) row {
	return valueRow("rebuilt node identity", "execgraph.Rebuild over journal + store",
		"<stem>.execution.jsonl", "label from the opening; the worker override is recorded nowhere",
		identitySummary(graphOf(before)), identitySummary(rebuiltGraph(after).Graph), false)
}

func identitySummary(g agentgraph.Graph) string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind != agentgraph.KindWorker {
			continue
		}
		out = append(out, fmt.Sprintf("%s[label=%s profile=%s model=%s effort=%s]",
			n.ID, orNone(n.Label), orNone(n.Profile), orNone(n.Model), orNone(n.Effort)))
	}
	sort.Strings(out)
	return join(out)
}

// statesExcept renders worker states, skipping the executions whose owner is
// gone. Those are not a state the rebuild may assert, and comparing them would
// demand exactly the ghost this refuses to draw.
func statesExcept(g agentgraph.Graph, skip map[string]bool) string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind == agentgraph.KindGroup || skip[n.ID] {
			continue
		}
		out = append(out, n.ID+":"+string(n.State))
	}
	sort.Strings(out)
	return join(out)
}

func edgeSummary(g agentgraph.Graph) string {
	out := make([]string, 0, len(g.Edges))
	for _, e := range g.Edges {
		out = append(out, fmt.Sprintf("%s-%s->%s", e.From, e.Kind, e.To))
	}
	sort.Strings(out)
	return join(out)
}

func envelopeSummary(g agentgraph.Graph) string {
	var out []string
	for _, n := range g.Nodes {
		if n.Grant != "" {
			out = append(out, n.ID+"="+string(n.Grant))
		}
		if n.Wait != "" {
			out = append(out, n.ID+"@"+string(n.Wait))
		}
	}
	sort.Strings(out)
	return join(out)
}

// executionJournalRow is the record written before the work started, which is
// the only artifact here that can outlive an unfinished turn. It is what a
// restart reads to say a delegation existed at all.
func executionJournalRow(before, after Observation) row {
	return valueRow("delegations the journal recorded", "execution journal, written before dispatch",
		"<stem>.execution.jsonl", "folded from openings and settlings",
		executionSummary(before), executionSummary(after), true)
}

// executionTopologyRow is the ordering a restart inherits. An item that never
// started is explained by what it was held behind, so the topology has to
// survive the process that knew it — and survive unchanged, or the explanation
// is a different graph's.
func executionTopologyRow(before, after Observation) row {
	return valueRow("declared ordering between items", "the fan-out plan, recorded at the opening",
		"<stem>.execution.jsonl", "read back with the openings, never re-derived",
		topologySummary(before), topologySummary(after), true)
}

func topologySummary(o Observation) string {
	var out []string
	for _, e := range o.Executions {
		if len(e.DependsOn) == 0 {
			continue
		}
		up := append([]string(nil), e.DependsOn...)
		sort.Strings(up)
		out = append(out, e.ID+"<-"+strings.Join(up, "+"))
	}
	if len(out) == 0 {
		return ""
	}
	sort.Strings(out)
	return join(out)
}

// executionKindRow holds the two interruptions apart against what the dying
// process actually showed. The item the graph had running must come back as one
// that reached a slot; the one its dependency blocked must come back as one
// that did not. A classification agreeing with neither is a label, not a fact.
func executionKindRow(before, after Observation) row {
	running := runningWorkers(before)
	verdict := verdictHolds
	if len(after.InterruptedExecutions) == 0 {
		verdict = verdictNotMeasured
	}
	for _, e := range after.InterruptedExecutions {
		want := execjournal.InterruptedBeforeStart
		if slices.Contains(running, e.ID) {
			want = execjournal.InterruptedDuringExecution
		}
		if e.Interruption() != want {
			verdict = verdictViolated
		}
	}
	return row{
		Semantic: "how each interruption is classified", Authority: "the execution journal's start records",
		Artifact: "<stem>.execution.jsonl", Reconstruction: "started at death -> during-execution, never started -> before-start",
		Before: orNone(join(running)) + " running at death", After: interruptionKinds(after), Verdict: verdict,
	}
}

func interruptionKinds(o Observation) string {
	if len(o.InterruptedExecutions) == 0 {
		return "none"
	}
	out := make([]string, 0, len(o.InterruptedExecutions))
	for _, e := range o.InterruptedExecutions {
		out = append(out, e.ID+":"+e.Interruption())
	}
	sort.Strings(out)
	return join(out)
}

// executionContextRow is the other side of that record: what the next request
// is told. It must state provenance and never offer a continuation, so the
// count is checked against the interruptions rather than against a fixed
// number — a block with nothing behind it is a ghost in prose.
func executionContextRow(before, after Observation) row {
	verdict := verdictHolds
	if blocksOwed(before) != before.ModelSeesInterruptedExecution ||
		blocksOwed(after) != after.ModelSeesInterruptedExecution {
		verdict = verdictViolated
	}
	return row{
		Semantic: "what the next request says about them", Authority: "control, projected per request",
		Artifact: "none: derived, never stored", Reconstruction: "one block while an interrupted execution stands, none otherwise",
		Before: executionBlocks(before), After: executionBlocks(after), Verdict: verdict,
	}
}

func blocksOwed(o Observation) int {
	if len(o.InterruptedExecutions) > 0 {
		return 1
	}
	return 0
}

func executionBlocks(o Observation) string {
	return fmt.Sprintf("%d block(s) for %d interrupted", o.ModelSeesInterruptedExecution, len(o.InterruptedExecutions))
}

func executionSummary(o Observation) string {
	if len(o.Executions) == 0 {
		return ""
	}
	open, started := 0, 0
	for _, e := range o.Executions {
		if e.Open() {
			open++
		}
		if e.Started() {
			started++
		}
	}
	return fmt.Sprintf("%d opened, %d started, %d still open, %d with no owner here",
		len(o.Executions), started, open, len(o.InterruptedExecutions))
}

func childrenInState(o Observation, status string) []string {
	var out []string
	for _, f := range o.Children.Facts {
		if f.Status == status {
			out = append(out, f.Ref)
		}
	}
	return out
}

func childSummary(o Observation) string {
	if len(o.Children.Facts) == 0 {
		if o.Children.Err != "" {
			return "error: " + o.Children.Err
		}
		return ""
	}
	counts := map[string]int{}
	for _, f := range o.Children.Facts {
		counts[f.Status]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return fmt.Sprintf("%d child(ren): %s", len(o.Children.Facts), strings.Join(parts, " "))
}

// graphSummary is the fan-out as the dying process last saw it, which is the
// "before" every graph row is compared against.
func graphSummary(o Observation) string {
	if len(o.Graph.Nodes) == 0 {
		return "no fan-out"
	}
	counts := map[string]int{}
	for _, n := range o.Graph.Nodes {
		if n.Kind == agentgraph.KindWorker {
			counts[string(n.State)]++
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return fmt.Sprintf("%d node(s), %d edge(s): %s",
		len(o.Graph.Nodes), len(o.Graph.Edges), strings.Join(parts, " "))
}

// graphArmInvalid refuses an arm that never reached the state it names. The
// running arms need a child in flight at death; every fan-out arm needs a
// fan-out at all. Reporting a smaller shape as the thing that was interrupted
// is how a probe answers a question nobody asked.
func graphArmInvalid(arm string, before Observation) string {
	if len(before.Graph.Nodes) == 0 {
		return "no fan-out was published before the process exited, so nothing was measured"
	}
	if arm == armGraphCompleted {
		if len(runningWorkers(before)) > 0 {
			return "the fan-out was still running at exit, so this is not the completed arm"
		}
		return ""
	}
	if len(runningWorkers(before)) == 0 {
		return "no child was executing when the process died, so nothing was interrupted"
	}
	if arm == armGraphMixed {
		return mixedArmInvalid(before)
	}
	return ""
}

// mixedArmInvalid holds the density arm to its own premise. Its whole claim is
// that one death carried every reachable state at once, and an arm that reached
// three of them measures a different scenario than the one it reports.
func mixedArmInvalid(before Observation) string {
	var missing []string
	for _, state := range wantedStates(armGraphMixed) {
		if !holdsAll(agentgraph.Graph{Nodes: before.Graph.Nodes}, []agentgraph.NodeState{state}) {
			missing = append(missing, string(state))
		}
	}
	if len(missing) > 0 {
		return "the fan-out never reached " + join(missing) + ", so the arm is not the mixed shape it reports"
	}
	if !carriesGrant(before, agentgraph.GrantRead) || !carriesGrant(before, agentgraph.GrantWrite) {
		return "the fan-out carried only one kind of grant, so the envelope row measures one side of it"
	}
	return ""
}

func carriesGrant(o Observation, want agentgraph.Grant) bool {
	for _, got := range o.Graph.Grants {
		if got == string(want) {
			return true
		}
	}
	return false
}

// graphEdges renders the typed relations in a stable order. Sorting is not
// cosmetic: the fold keeps first-seen order, which two processes reach by
// different routes, and an unsorted comparison would report a reordering as a
// changed graph.
func graphEdges(o Observation) string {
	if len(o.Graph.Edges) == 0 {
		return ""
	}
	out := make([]string, 0, len(o.Graph.Edges))
	for _, e := range o.Graph.Edges {
		out = append(out, fmt.Sprintf("%s-%s->%s", e.From, e.Kind, e.To))
	}
	sort.Strings(out)
	return join(out)
}

package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
)

// The scheduler-wait arms. Each dies with one item ready and refused admission,
// and they are separate because the scheduler decides in a fixed order —
// slots, then writers, then claim. An arm that let an earlier cause stand would
// report it as the later one, so each has to clear every check above the one it
// is measuring.
const (
	// armWaitSlots fills the session's total ceiling with runs that never end.
	// The refused item is read-only: it clears every writer check by not being
	// a writer at all.
	armWaitSlots = "wait-slots"
	// armWaitWriters leaves total capacity free and fills the writer ceiling.
	// Its two writers declare disjoint paths, so a claim conflict cannot be
	// what refuses the second one.
	armWaitWriters = "wait-writers"
	// armWaitClaim leaves both ceilings free and overlaps two write paths. A
	// single fan-out cannot reach this: preflight refuses concurrent writers
	// whose claims overlap, so the holder is dispatched as a background fleet
	// and the refused writer comes from a later one.
	armWaitClaim = "wait-claim"
	// armWaitTransition asks what a wait cause means once the thing it named
	// stops being what holds the item back. The scheduler reports the cause
	// once, when the request queues; the blocker can change afterwards.
	armWaitTransition = "wait-transition"
)

func schedulerWaitArm(name string) bool {
	switch name {
	case armWaitSlots, armWaitWriters, armWaitClaim, armWaitTransition:
		return true
	}
	return false
}

// schedulerLimits are the ceilings an arm needs to reach its refusal. They are
// written to the project config, so the run makes the same judgement a person
// configuring the session would get.
func schedulerLimits(arm string) (total, writers int) {
	switch arm {
	case armTerminalCancelled:
		// One slot, so the group has an item running and items that never got
		// one — the split a cancellation is reported against.
		return 1, 1
	case armTaskBgQueued:
		// One slot, held by the fleet dispatched ahead of the delegation, so the
		// refusal the arm records is the scheduler's and not a race.
		return 1, 1
	case armUIGraphMixed:
		// One slot, so the item after the one that hangs is refused: the death
		// then holds a node that was queued and never admitted.
		return 1, 1
	case armWaitSlots:
		return 2, 1
	case armWaitWriters:
		return 6, 1
	case armWaitClaim:
		return 6, 3
	case armWaitTransition:
		return 3, 1
	}
	return 0, 0
}

// wantedCause is the refusal this arm exists to produce. The transition arm
// starts at slots and asks what happens to that answer afterwards.
func wantedCause(arm string) agentgraph.WaitCause {
	switch arm {
	case armWaitWriters:
		return agentgraph.WaitWriters
	case armWaitClaim:
		return agentgraph.WaitClaim
	}
	return agentgraph.WaitSlots
}

// waitingWorkers are the nodes the scheduler refused and has not admitted: the
// arm's premise. State is checked as well as the cause, because a node that
// went on to run still carries the cause it waited under.
func waitingWorkers(g agentgraph.Graph, cause agentgraph.WaitCause) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind == agentgraph.KindGroup || n.State != agentgraph.StateQueued {
			continue
		}
		if n.Wait == cause {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}

// waitForRefusal blocks until one item is queued under this arm's cause.
func waitForRefusal(sink *graphSink, arm string) error {
	cause := wantedCause(arm)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		graph, _ := sink.snapshot()
		if len(waitingWorkers(graph, cause)) > 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	graph, _ := sink.snapshot()
	return errUnexpected("an item queued under "+string(cause), waitStates(graph))
}

func waitStates(g agentgraph.Graph) string {
	out := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		out = append(out, fmt.Sprintf("%s:%s/%s", n.ID, n.State, orNone(string(n.Wait))))
	}
	sort.Strings(out)
	return orNone(join(out))
}

// schedTurn is the prompt that opens this arm's fan-out. The slots arm needs
// only one fleet; the others name both, and the latch order dispatches the
// holder first while the refused fleet waits on the holder's own signal.
func schedTurn(n int, arm string) string {
	if arm == armWaitSlots {
		return fmt.Sprintf("%s %s: dispatch the fan-out.", marker(n), fleetPairSentinel)
	}
	return fmt.Sprintf("%s %s %s: dispatch the fan-out.", marker(n), fleetHolderSentinel, fleetRefusedSentinel)
}

// runSchedulerWaitConstruct drives the arm to its refusal and exits while the
// item is still queued. The transition arm goes further: it frees the capacity
// the refusal named and asks what the reported cause does about it.
func runSchedulerWaitConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	go func() { _ = ctrl.Run(ctx, schedTurn(turn+1, arm)) }()
	if err := waitForRefusal(sink, arm); err != nil {
		return err
	}
	if arm == armWaitTransition {
		if err := releaseAndSettle(sink, prov); err != nil {
			return err
		}
	}
	if err := writeObservation(root, capture("construct", arm, bootSystem, ctrl, sink, root)); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// releaseAndSettle frees the run that filled total capacity, then waits for the
// graph to show it gone. What the refused item's cause does after that is the
// arm's question: the ceiling it named has room, and the writer ceiling that
// now holds it back was never reported.
func releaseAndSettle(sink *graphSink, prov *scripted) error {
	prov.releaseOnce()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		graph, _ := sink.snapshot()
		if settledWorkers(graph) > 0 && len(waitingWorkers(graph, agentgraph.WaitSlots)) > 0 {
			// Give any second report a chance to arrive before reading.
			time.Sleep(500 * time.Millisecond)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	graph, _ := sink.snapshot()
	return errUnexpected("capacity freed while a refusal still stands", waitStates(graph))
}

func settledWorkers(g agentgraph.Graph) int {
	n := 0
	for _, node := range g.Nodes {
		if node.Kind != agentgraph.KindGroup && node.State != "" && node.State.Terminal() {
			n++
		}
	}
	return n
}

// schedulerWaitRows report what a restart inherits about an item the scheduler
// refused. The classification is already right — STARTED settled that — so the
// question here is narrower: can the host still say why it never started?
func schedulerWaitRows(arm string, before, after Observation) []row {
	rows := []row{refusalRow(arm, before, after)}
	if arm == armWaitTransition {
		rows = append(rows, capacityFreedRow(before))
	}
	return append(rows,
		waitSeriesRow(arm, before),
		executionKindRow(before, after),
		waitProvenanceRow(before, after),
		schedulerGateRow(before, after),
	)
}

// capacityFreedRow is the transition arm's evidence. It freed the ceiling the
// refusal named and the item stayed queued, held by a different one — so what
// the reported cause does next is a question about the contract, not a race.
func capacityFreedRow(before Observation) row {
	return row{
		Semantic: "the ceiling the refusal named, once freed", Authority: "the arm, through a child it releases",
		Artifact: "none", Reconstruction: "an item settles while the refused one stays queued",
		Before: fmt.Sprintf("%d settled, still queued: %s", before.SettledWorkers, orNone(join(before.QueuedWorkers))),
		After:  "not applicable: the scheduler is gone", Verdict: verdictStable,
	}
}

// refusalRow is the arm's own control: it reached the refusal it names, and no
// earlier one. Reaching a different cause means an upper check intercepted it,
// and the arm measured someone else's question.
func refusalRow(arm string, before, after Observation) row {
	want := string(wantedCause(arm))
	verdict := verdictHolds
	if before.WaitCauses[want] == 0 {
		verdict = verdictNotMeasured
	}
	return row{
		Semantic: "the refusal this arm reached", Authority: "SubagentScheduler.canStartLocked",
		Artifact: "none: reported once through OnQueued", Reconstruction: "slots, then writers, then claim",
		Before: causeSummary(before), After: causeSummary(after), Verdict: verdict,
	}
}

// waitSeriesRow is what the transition arm exists for: how many times a cause
// was published for the item that never started. Once means the answer is the
// cause that queued it, not the one that held it last.
func waitSeriesRow(arm string, before Observation) row {
	return row{
		Semantic: "how often a wait cause was reported", Authority: "OnQueued, which fires once per request",
		Artifact: "none", Reconstruction: causeContract(before),
		Before: seriesSummary(before), After: "not applicable: the scheduler is gone",
		Verdict: verdictStable,
	}
}

// causeContract states what the observed reporting says the cause means. It is
// read off the series rather than assumed: one report can only be the cause
// that queued the request, and more than one would track the current blocker.
func causeContract(o Observation) string {
	for _, seq := range o.WaitSeries {
		if len(seq) > 1 {
			return "reported again as the blocker changes: the cause is the current one"
		}
	}
	return "reported once: the cause is the one that queued the request, not the one holding it now"
}

// waitProvenanceRow is what the batch was built to expose and then close. The
// refusal exists only at the moment the scheduler makes it, so it is recorded
// there; this compares what the graph showed against what a later process can
// still read back.
func waitProvenanceRow(before, after Observation) row {
	return valueRow("why the item never started", "the execution journal's queue records",
		"<stem>.execution.jsonl", "the cause the scheduler gave when it first refused admission",
		causeSummary(before), journalCauses(after), true)
}

// journalCauses is the refusal a restart reads back, counted the way the graph
// counts it so the two sides of the row are the same measurement.
func journalCauses(o Observation) string {
	counts := map[string]int{}
	for _, e := range o.Executions {
		if e.Queued() {
			counts[e.Cause]++
		}
	}
	if len(counts) == 0 {
		return ""
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
	return strings.Join(parts, " ")
}

// schedulerGateRow is the other half of what a queue record proves. An item
// only reaches the scheduler once its dependencies are answered, so a queued
// entry is durable evidence it crossed that gate — the one thing a dependency
// edge alone could never say.
func schedulerGateRow(before, after Observation) row {
	return valueRow("items proven to have reached the scheduler", "the execution journal's queue records",
		"<stem>.execution.jsonl", "a queue record implies the dependency gate was crossed",
		queuedIDs(before), queuedIDs(after), true)
}

func queuedIDs(o Observation) string {
	var out []string
	for _, e := range o.Executions {
		if e.Queued() {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return join(out)
}

func causeSummary(o Observation) string {
	if len(o.WaitCauses) == 0 {
		return ""
	}
	keys := make([]string, 0, len(o.WaitCauses))
	for k := range o.WaitCauses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, o.WaitCauses[k]))
	}
	return strings.Join(parts, " ")
}

func seriesSummary(o Observation) string {
	if len(o.WaitSeries) == 0 {
		return "none"
	}
	ids := make([]string, 0, len(o.WaitSeries))
	for id := range o.WaitSeries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id+":["+strings.Join(o.WaitSeries[id], "->")+"]")
	}
	return join(out)
}

// schedulerArmInvalid refuses an arm that did not reach its own refusal. Each
// arm exists to isolate one cause, and one that reached a different cause was
// intercepted by a check above it — reporting that as this arm's answer is how
// a probe measures the wrong ceiling and calls it evidence.
func schedulerArmInvalid(arm string, before Observation) string {
	want := string(wantedCause(arm))
	if before.WaitCauses[want] == 0 {
		return "no item was refused under " + want + "; the arm reached " + orNone(causeSummary(before))
	}
	for cause := range before.WaitCauses {
		if cause != want {
			return "an item was also refused under " + cause + ", so this arm does not isolate " + want
		}
	}
	return ""
}

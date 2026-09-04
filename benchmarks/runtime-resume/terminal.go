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

// The terminal-disposition arms. SETTLED says an opening is no longer active
// and nothing more, so these ask what that costs: two items can share every
// durable fact and mean opposite things, and a restart that cannot tell them
// apart has lost the answer a dependent needs, not a label a picture wanted.
const (
	// armTerminalAdopted reuses an earlier child's answer. It never runs and
	// still holds a final answer, which is the one terminal that is answered
	// without having executed.
	armTerminalAdopted = "terminal-adopted"
	// armTerminalSkippedDep is the same lifecycle with the opposite meaning: an
	// item whose dependency failed, which never ran and holds no answer.
	armTerminalSkippedDep = "terminal-skipped-dep"
	// armTerminalCancelled is the other way an item ends without an answer. It
	// was built to reach parallel_tasks' skipped branch and found it
	// unreachable: running[i] is set when an item is dispatched, not when it is
	// admitted, so a cancellation marks every unfinished item cancelled.
	armTerminalCancelled = "terminal-cancelled"
	// armTerminalContext asks whether a context edge has to be recorded at all,
	// or already follows from the ordering and what the upstream answered.
	armTerminalContext = "terminal-context"
	// armChildTerminal compares the graph's terminal for an executed item
	// against what the sub-agent store recorded for the same child. The store
	// is the owner of "what happened to this child", so a distinction the graph
	// draws and the store does not keep is one no reconstruction can recover.
	armChildTerminal = "child-terminal"
)

func terminalArm(name string) bool {
	switch name {
	case armTerminalAdopted, armTerminalSkippedDep, armTerminalCancelled, armTerminalContext, armChildTerminal:
		return true
	}
	return false
}

// terminalSentinels is what this arm's one prompt carries. The adopted arm
// needs a completed reference first, so it names the warm-up fleet too.
func terminalSentinels(arm string) string {
	switch arm {
	case armTerminalAdopted:
		return fleetWarmSentinel + " " + fleetMixedSentinel
	case armTerminalCancelled:
		return parallelSentinel
	case armChildTerminal:
		return fleetOutcomesSentinel
	}
	return fleetTerminalSentinel
}

// runTerminalConstruct lets the fan-out settle, then dies before the turn ends.
// The settling is the point: a fan-out publishes an item's skip only in its
// closing delta, so an arm that died mid-flight would never see one.
func runTerminalConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, turn int) error {
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	go func() { _ = ctrl.Run(runCtx, fanOutTurn(turn+1, terminalSentinels(arm))) }()
	if arm == armTerminalCancelled {
		if err := cancelBeforeStart(sink, cancelRun); err != nil {
			return err
		}
	}
	if arm == armChildTerminal {
		if err := cancelAfterOutcomes(sink, cancelRun); err != nil {
			return err
		}
	}
	if err := waitForTerminal(sink, arm); err != nil {
		return err
	}
	if err := writeObservation(root, capture("construct", arm, bootSystem, ctrl, sink, root)); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// cancelBeforeStart interrupts the group once one item is running and another
// is still waiting for a slot, which is the state parallel_tasks splits on. It
// cancels the turn's own context rather than calling Controller.Cancel: the
// synchronous headless Run does not pass through turn admission, so the gate
// that Cancel operates has nothing registered for it.
func cancelBeforeStart(sink *graphSink, cancelRun context.CancelFunc) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		graph, _ := sink.snapshot()
		if runningCount(graph) > 0 && len(queuedWorkers(graph)) > 0 {
			cancelRun()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	graph, _ := sink.snapshot()
	return errUnexpected("one item running and one still queued", waitStates(graph))
}

// cancelAfterOutcomes interrupts once one item has completed and another has
// failed, with a third still running. All three terminals then come from one
// group, so the store's record of them is compared under identical conditions.
func cancelAfterOutcomes(sink *graphSink, cancelRun context.CancelFunc) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		graph, _ := sink.snapshot()
		if len(nodesInState(graph, agentgraph.StateCompleted)) > 0 &&
			len(nodesInState(graph, agentgraph.StateFailed)) > 0 &&
			runningCount(graph) > 0 {
			cancelRun()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	graph, _ := sink.snapshot()
	return errUnexpected("one completed, one failed and one still running", waitStates(graph))
}

func runningCount(g agentgraph.Graph) int {
	n := 0
	for _, node := range g.Nodes {
		if node.Kind != agentgraph.KindGroup && node.State == agentgraph.StateRunning {
			n++
		}
	}
	return n
}

// wantedTerminal is the disposition this arm exists to produce.
func wantedTerminal(arm string) agentgraph.NodeState {
	if arm == armTerminalAdopted {
		return agentgraph.StateAdopted
	}
	if arm == armTerminalContext {
		return agentgraph.StateCompleted
	}
	if arm == armTerminalCancelled || arm == armChildTerminal {
		return agentgraph.StateCancelled
	}
	return agentgraph.StateSkipped
}

// waitForTerminal blocks until the graph shows this arm's disposition.
func waitForTerminal(sink *graphSink, arm string) error {
	want := wantedTerminal(arm)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		graph, _ := sink.snapshot()
		if len(nodesInState(graph, want)) > 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	graph, _ := sink.snapshot()
	return errUnexpected("an item in state "+string(want), waitStates(graph))
}

func nodesInState(g agentgraph.Graph, want agentgraph.NodeState) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind != agentgraph.KindGroup && n.State == want {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}

// terminalRows put the two lifecycles side by side. Adopted and skipped reach
// the same durable shape by different routes and mean opposite things to a
// dependent, so every row here compares what the graph knew against what the
// journal can still be asked.
func terminalRows(arm string, before, after Observation) []row {
	rows := []row{
		terminalStateRow(arm, before, after),
		terminalLifecycleRow(before, after),
		terminalAnswerRow(before, after),
	}
	if arm == armTerminalAdopted {
		rows = append(rows, adoptSourceRow(before, after))
	}
	if arm == armTerminalContext || arm == armTerminalSkippedDep {
		rows = append(rows, contextDerivabilityRow(before, after))
	}
	if arm == armChildTerminal {
		rows = append(rows, childOutcomeRow(before, after))
	}
	return rows
}

// terminalStateRow is the arm's own control and the loss in one line: which
// disposition the graph ended on, and whether anything still says so.
func terminalStateRow(arm string, before, after Observation) row {
	want := wantedTerminal(arm)
	verdict := verdictNotMeasured
	if len(nodesInState(graphOf(before), want)) > 0 {
		verdict = verdictHolds
	}
	return row{
		Semantic: "the disposition this arm reached", Authority: "the fan-out's closing delta",
		Artifact: "none observed", Reconstruction: "published once, when the group ends",
		Before: terminalSummary(before), After: terminalSummary(after), Verdict: verdict,
	}
}

// terminalLifecycleRow is the comparison that decides whether a disposition has
// to be recorded. Two items with the same durable lifecycle and opposite
// meanings cannot be told apart by anything a restart reads.
func terminalLifecycleRow(before, after Observation) row {
	return valueRow("the durable lifecycle each item left", "the execution journal",
		"<stem>.execution.jsonl", "opened / queued / started / settled, per item",
		lifecycleSummary(before), lifecycleSummary(after), true)
}

// terminalAnswerRow is what a dependent actually needs. Completed and adopted
// carry a final answer a dependent may start from; skipped and cancelled do
// not, and the difference is the whole reason the distinction exists.
func terminalAnswerRow(before, after Observation) row {
	return valueRow("items holding an answer a dependent may use", "agentgraph.NodeState.Answered",
		"none observed", "derived from the disposition, which is not recorded",
		answeredSummary(before), answeredSummary(after), false)
}

// adoptSourceRow is the provenance an adoption carries: whose answer stood in.
// Reuse is only visible if the source is nameable — otherwise the picture shows
// work that cost nothing with no way to say what paid for it.
func adoptSourceRow(before, after Observation) row {
	return valueRow("whose answer an adoption reused", "the adopt edge in the graph",
		"none observed", "no durable record names the source",
		adoptEdges(before), adoptEdges(after), false)
}

// contextDerivabilityRow asks whether the delivery edge follows from facts that
// are already durable. A context edge that always accompanies an ordering edge
// whose upstream answered is derivable; one that does not is a fact of its own.
func contextDerivabilityRow(before, after Observation) row {
	implied, actual := impliedContext(before), contextEdges(before)
	verdict := verdictHolds
	if implied != actual {
		verdict = verdictViolated
	}
	return row{
		Semantic:       "whether a context edge follows from the durable facts",
		Authority:      "the ordering edges and what each upstream answered",
		Artifact:       "<stem>.execution.jsonl for the ordering; the disposition is not recorded",
		Reconstruction: "an ordering edge whose upstream answered delivered its answer",
		Before:         "implied " + orNone(implied) + " / actual " + orNone(actual),
		After:          "implied " + orNone(impliedContext(after)) + " / actual: not observable",
		Verdict:        verdict,
	}
}

// executedTerminals are the graph's terminals for items that actually ran.
// Adopted and skipped are excluded: nothing executed under them, so the store
// owes them no record and counting them would read a correct store as short.
func executedTerminals(o Observation) map[string]int {
	out := map[string]int{}
	for _, n := range o.Graph.Nodes {
		if n.Kind == agentgraph.KindGroup || n.State == "" || !n.State.Terminal() {
			continue
		}
		if n.State == agentgraph.StateAdopted || n.State == agentgraph.StateSkipped {
			continue
		}
		out[string(n.State)]++
	}
	return out
}

// childOutcomeRow compares the two owners of the same fact. The graph draws
// completed, failed and cancelled apart; the store is what a restart reads. A
// terminal the graph distinguishes and the store folds into another is gone for
// good — no journal record could recover it, because the owner never kept it.
func childOutcomeRow(before, after Observation) row {
	graphTerminals := executedTerminals(before)
	verdict := verdictNotMeasured
	if len(graphTerminals) > 0 {
		verdict = verdictHolds
		for state := range graphTerminals {
			if !storeKnows(after, state) {
				verdict = verdictViolated
			}
		}
	}
	return row{
		Semantic:       "terminal outcome: what the graph drew, what the store kept",
		Authority:      "the graph's closing delta against SubagentStore.Meta.Status",
		Artifact:       "subagents/<ref>.meta.json",
		Reconstruction: "the store owns what happened to a child; every terminal it draws it must keep",
		Before:         "graph " + countSummary(graphTerminals) + " / store " + orNone(childStatusCounts(before)),
		After:          "store " + orNone(childStatusCounts(after)), Verdict: verdict,
	}
}

// storeKnows reports whether any child carries this terminal as its status.
func storeKnows(o Observation, state string) bool {
	for _, f := range o.Children.Facts {
		if f.Status == state {
			return true
		}
	}
	return false
}

func childStatusCounts(o Observation) string {
	counts := map[string]int{}
	for _, f := range o.Children.Facts {
		counts[f.Status]++
	}
	if len(counts) == 0 {
		return ""
	}
	return countSummary(counts)
}

func graphOf(o Observation) agentgraph.Graph {
	return agentgraph.Graph{Nodes: o.Graph.Nodes, Edges: o.Graph.Edges}
}

func terminalSummary(o Observation) string {
	if len(o.Graph.Nodes) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, n := range o.Graph.Nodes {
		if n.Kind != agentgraph.KindGroup && n.State != "" {
			counts[string(n.State)]++
		}
	}
	return countSummary(counts)
}

// lifecycleSummary renders each item's durable transitions, which is what a
// restart actually has. Two items printing the same line here are two items a
// restart cannot tell apart.
func lifecycleSummary(o Observation) string {
	if len(o.Executions) == 0 {
		return ""
	}
	out := make([]string, 0, len(o.Executions))
	for _, e := range o.Executions {
		marks := []string{"open"}
		if e.Disposition != "" && e.Disposition != "pending" {
			marks = append(marks, e.Disposition)
		}
		if e.Queued() {
			marks = append(marks, "queued("+e.Cause+")")
		}
		if e.Started() {
			marks = append(marks, "started")
		}
		if !e.SettledAt.IsZero() {
			marks = append(marks, "settled")
		}
		out = append(out, e.ID+":"+strings.Join(marks, "+"))
	}
	sort.Strings(out)
	return join(out)
}

func answeredSummary(o Observation) string {
	var out []string
	for _, n := range o.Graph.Nodes {
		if n.Kind != agentgraph.KindGroup && n.State.Answered() {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return join(out)
}

func adoptEdges(o Observation) string {
	var out []string
	for _, e := range o.Graph.Edges {
		if e.Kind == agentgraph.Adopt {
			out = append(out, e.To+"<-"+e.From)
		}
	}
	sort.Strings(out)
	return join(out)
}

func contextEdges(o Observation) string {
	var out []string
	for _, e := range o.Graph.Edges {
		if e.Kind == agentgraph.Context {
			out = append(out, e.To+"<-"+e.From)
		}
	}
	sort.Strings(out)
	return join(out)
}

// impliedContext is what the durable facts alone would predict: every ordering
// edge whose upstream ended up answered.
func impliedContext(o Observation) string {
	answered := map[string]bool{}
	for _, n := range o.Graph.Nodes {
		if n.State.Answered() {
			answered[n.ID] = true
		}
	}
	var out []string
	for _, e := range o.Executions {
		for _, up := range e.DependsOn {
			if answered[up] {
				out = append(out, e.ID+"<-"+up)
			}
		}
	}
	sort.Strings(out)
	return join(out)
}

func countSummary(counts map[string]int) string {
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

// terminalArmInvalid refuses an arm that never reached its disposition. Every
// row below it compares against a shape that was not established, and reporting
// those as evidence is how a probe answers a question nobody asked.
func terminalArmInvalid(arm string, before Observation) string {
	want := wantedTerminal(arm)
	if len(nodesInState(graphOf(before), want)) == 0 {
		return "no item reached " + string(want) + "; the fan-out ended as " + orNone(terminalSummary(before))
	}
	if arm == armTerminalContext && contextEdges(before) == "" {
		return "the fan-out delivered no upstream answer, so there is no context edge to derive"
	}
	if arm == armTerminalCancelled && len(nodesInState(graphOf(before), agentgraph.StateSkipped)) > 0 {
		return "an item was skipped rather than cancelled; parallel_tasks' skipped branch was thought unreachable"
	}
	return ""
}

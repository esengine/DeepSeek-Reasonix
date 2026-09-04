package main

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
)

// The derivability arms. Everything before them asked what survives; these ask
// what still has to be written down. A fact a restart can compute from what is
// already durable needs no record of its own, and adding one would give it a
// second owner for nothing.
const (
	// armDeriveSkipBoth cuts a branch with two upstreams that both end without
	// an answer. Which of them the graph names is the question: the durable
	// facts show two, and the picture shows one.
	armDeriveSkipBoth = "derive-skip-both"
	// armDeriveSkipFlip is the same graph with the two failures in the other
	// order. If the named cause moves while the durable facts do not, the cause
	// is history rather than a consequence of the end state.
	armDeriveSkipFlip = "derive-skip-flip"
	// armDeriveAnswered is the negative control and the context sample at once:
	// two upstreams that both answer, one by completing and one by adopting, so
	// the dependent must run and both deliveries must be accounted for.
	armDeriveAnswered = "derive-answered"
)

func deriveArm(name string) bool {
	switch name {
	case armDeriveSkipBoth, armDeriveSkipFlip, armDeriveAnswered:
		return true
	}
	return false
}

func deriveSentinels(arm string) string {
	if arm == armDeriveAnswered {
		return fleetWarmSentinel + " " + fleetDeriveSentinel
	}
	return fleetDeriveSentinel
}

// runDeriveConstruct lets the fan-out settle and dies before the turn ends, the
// way every terminal arm does: a skip is published only in the closing delta.
func runDeriveConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	go func() { _ = ctrl.Run(runCtx, fanOutTurn(turn+1, deriveSentinels(arm))) }()
	if err := waitForDerive(sink, arm, prov); err != nil {
		return err
	}
	if err := writeObservation(root, capture("construct", arm, bootSystem, ctrl, sink, root)); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// waitForDerive holds until the arm's shape is on the graph. The skip arms
// release their second failure once the first has been recorded, so which
// upstream the fan-out names is decided rather than raced.
func waitForDerive(sink *graphSink, arm string, prov *scripted) error {
	if arm == armDeriveAnswered {
		// Waiting on a count would be satisfied by the warm-up alone. The
		// delivery edge is what the arm compares, so it waits for that.
		return waitForShape(sink, func(g agentgraph.Graph) bool {
			return len(contextTargets(g)) > 0 && len(nodesInState(g, agentgraph.StateAdopted)) > 0
		}, "an adopted upstream and a delivered answer")
	}
	if err := waitForShape(sink, func(g agentgraph.Graph) bool {
		return len(nodesInState(g, agentgraph.StateSkipped)) > 0
	}, "a skipped dependent"); err != nil {
		return err
	}
	// The dependent is already skipped by the first failure; releasing the
	// second one now settles it without changing what was named.
	prov.releaseOnce()
	return waitForShape(sink, func(g agentgraph.Graph) bool {
		return len(nodesInState(g, agentgraph.StateFailed)) >= 2
	}, "both upstreams failed")
}

// contextTargets are the items the graph shows receiving an upstream answer.
func contextTargets(g agentgraph.Graph) []string {
	var out []string
	for _, e := range g.Edges {
		if e.Kind == agentgraph.Context {
			out = append(out, e.To)
		}
	}
	return out
}

func waitForShape(sink *graphSink, ok func(agentgraph.Graph) bool, want string) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		graph, _ := sink.snapshot()
		if ok(graph) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	graph, _ := sink.snapshot()
	return errUnexpected(want, waitStates(graph))
}

// answeredFromDurable is Answered() computed the way a restart would have to:
// an adoption says so on its own opening, and everything else is the terminal
// the sub-agent store kept, joined by the execution id the child records.
func answeredFromDurable(o Observation) map[string]bool {
	terminal := map[string]string{}
	for _, f := range o.Children.Facts {
		if f.ParentToolCallID != "" {
			terminal[f.ParentToolCallID] = f.Status
		}
	}
	out := map[string]bool{}
	for _, e := range o.Executions {
		out[e.ID] = e.Disposition == "adopted" || terminal[e.ID] == string(agent.SubagentCompleted)
	}
	return out
}

// impliedSkipped is the state a restart would derive: an item the orchestration
// released without ever starting it, ordered behind something that did not
// answer. Nothing here reads a disposition, because that is what is missing.
func impliedSkipped(o Observation) string {
	answered := answeredFromDurable(o)
	var out []string
	for _, e := range o.Executions {
		if e.Started() || e.Queued() || e.SettledAt.IsZero() || len(e.DependsOn) == 0 {
			continue
		}
		for _, up := range e.DependsOn {
			if !answered[up] {
				out = append(out, e.ID)
				break
			}
		}
	}
	sort.Strings(out)
	return join(out)
}

// impliedSkipCauses are every upstream that could have been named, and the two
// rules that would pick one: declaration order, and the earliest release. A
// cause is derivable only if some rule always agrees with the picture.
func impliedSkipCauses(o Observation) (all, byOrder, bySettle string) {
	answered := answeredFromDurable(o)
	byID := map[string]int{}
	for i, e := range o.Executions {
		byID[e.ID] = i
	}
	var every, first, earliest []string
	for _, e := range o.Executions {
		if e.Started() || e.Queued() || e.SettledAt.IsZero() || len(e.DependsOn) == 0 {
			continue
		}
		var unanswered []string
		for _, up := range e.DependsOn {
			if !answered[up] {
				unanswered = append(unanswered, up)
			}
		}
		if len(unanswered) == 0 {
			continue
		}
		every = append(every, e.ID+"<-{"+strings.Join(unanswered, ",")+"}")
		first = append(first, e.ID+"<-"+unanswered[0])
		pick := unanswered[0]
		for _, up := range unanswered[1:] {
			if i, j := byID[up], byID[pick]; o.Executions[i].SettledAt.Before(o.Executions[j].SettledAt) {
				pick = up
			}
		}
		earliest = append(earliest, e.ID+"<-"+pick)
	}
	sort.Strings(every)
	sort.Strings(first)
	sort.Strings(earliest)
	return join(every), join(first), join(earliest)
}

// actualSkipCause is the upstream the fan-out actually named, read off the node
// the graph settled. It is the picture's own answer, not a derivation.
func actualSkipCause(o Observation) string {
	var out []string
	for _, n := range o.Graph.Nodes {
		if n.State == agentgraph.StateSkipped && n.Err != "" {
			out = append(out, n.ID+": "+n.Err)
		}
	}
	sort.Strings(out)
	return orNone(join(out))
}

// impliedContextFromDurable predicts the delivery edges from durable facts
// alone: a started item receives an answer from every upstream that answered.
func impliedContextFromDurable(o Observation) string {
	answered := answeredFromDurable(o)
	var out []string
	for _, e := range o.Executions {
		if !e.Started() {
			continue
		}
		for _, up := range e.DependsOn {
			if answered[up] {
				out = append(out, e.ID+"<-"+up)
			}
		}
	}
	sort.Strings(out)
	return join(out)
}

// deriveRows are the three questions, kept apart. A state that derives says
// nothing about whether the cause does, and one arm's agreement says nothing
// about a rule that has only ever seen one sample.
func deriveRows(arm string, before, after Observation) []row {
	rows := []row{
		joinRow(before, after),
		skipStateRow(before, after),
		contextDeriveRow(before, after),
	}
	if arm != armDeriveAnswered {
		rows = append(rows, skipCauseRow(before, after))
	}
	return rows
}

// joinRow is what every derivation below stands on: the child records name the
// execution they ran for, so a restart can ask the store what each item ended
// as. Without it none of the other rows could be computed at all.
func joinRow(before, after Observation) row {
	return valueRow("executions the store can be asked about", "child metadata's parent tool-call id",
		"subagents/<ref>.meta.json", "joined to the journal by execution id",
		joinedExecutions(before), joinedExecutions(after), true)
}

func joinedExecutions(o Observation) string {
	var out []string
	for _, f := range o.Children.Facts {
		if f.ParentToolCallID != "" {
			out = append(out, f.ParentToolCallID+"="+f.Status)
		}
	}
	sort.Strings(out)
	return join(out)
}

// skipStateRow compares the state a restart would derive against the one the
// picture drew. Agreement means StateSkipped needs no record of its own.
func skipStateRow(before, after Observation) row {
	actual := join(nodesInState(graphOf(before), agentgraph.StateSkipped))
	implied := impliedSkipped(after)
	verdict := verdictHolds
	if implied != actual {
		verdict = verdictViolated
	}
	return row{
		Semantic: "skipped state: derived against drawn", Authority: "the journal joined to the store",
		Artifact:       "<stem>.execution.jsonl + subagents/<ref>.meta.json",
		Reconstruction: "settled, never started, ordered behind something that did not answer",
		Before:         "drawn " + orNone(actual), After: "derived " + orNone(implied), Verdict: verdict,
	}
}

// skipCauseRow keeps the harder question separate. The picture names one
// upstream; the durable facts may permit several, and two plausible rules for
// choosing between them are reported rather than one being assumed.
func skipCauseRow(before, after Observation) row {
	all, byOrder, bySettle := impliedSkipCauses(after)
	return row{
		Semantic:       "skipped cause: what the picture named, what the facts permit",
		Authority:      "the fan-out's own choice against the durable upstream set",
		Artifact:       "none: the reason text is not recorded",
		Reconstruction: "declaration order, or earliest released, over the upstreams that did not answer",
		Before:         actualSkipCause(before),
		After:          "candidates " + orNone(all) + " | by-order " + orNone(byOrder) + " | by-settle " + orNone(bySettle),
		Verdict:        verdictStable,
	}
}

// contextDeriveRow asks the same question of the delivery edge, now that a
// restart can tell which upstreams answered.
func contextDeriveRow(before, after Observation) row {
	actual, implied := contextEdges(before), impliedContextFromDurable(after)
	verdict := verdictHolds
	if implied != actual {
		verdict = verdictViolated
	}
	return row{
		Semantic: "context edges: derived against drawn", Authority: "the journal joined to the store",
		Artifact:       "<stem>.execution.jsonl + subagents/<ref>.meta.json",
		Reconstruction: "a started item receives from every upstream that answered",
		Before:         "drawn " + orNone(actual), After: "derived " + orNone(implied), Verdict: verdict,
	}
}

// deriveArmInvalid refuses an arm that did not reach the shape it compares.
func deriveArmInvalid(arm string, before Observation) string {
	if arm == armDeriveAnswered {
		if len(nodesInState(graphOf(before), agentgraph.StateAdopted)) == 0 {
			return "no item was adopted, so the arm never had an answered upstream that did not run"
		}
		if contextEdges(before) == "" {
			return "no answer was delivered, so there is no context edge to derive"
		}
		return ""
	}
	if len(nodesInState(graphOf(before), agentgraph.StateSkipped)) == 0 {
		return "no dependent was skipped; the fan-out ended as " + orNone(terminalSummary(before))
	}
	if len(nodesInState(graphOf(before), agentgraph.StateFailed)) < 2 {
		return "only one upstream ended without an answer, so the cause was never ambiguous"
	}
	return ""
}

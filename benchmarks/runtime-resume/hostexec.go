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

// The interactive host-initiated arms, named for the one surface they measure: a
// person typing /<skill> into a live session. The headless counterpart is not
// measured here — it is caller-owned and its cancellation need not match, and
// reading one for the other is how a real number gets the wrong subject.
const (
	armHostCompleted = "interactive-host-skill-completed"
	armHostQueued    = "interactive-host-skill-queued-crash"
	armHostRunning   = "interactive-host-skill-running-crash"
	armHostCancel    = "interactive-host-skill-cancel"
)

func hostExecArm(name string) bool {
	switch name {
	case armHostCompleted, armHostQueued, armHostRunning, armHostCancel:
		return true
	}
	return false
}

// The four identities one host-started run can be known by. They are printed
// apart because this is the first surface where they may not coincide: for a
// model-delegated run the execution and the parent call are one string, and
// that has only ever been true because a tool call produced every durable
// execution there was.
const (
	probeHostDispatch  = "probe:host-dispatch-id"
	probeHostChildRef  = "probe:host-child-ref"
	probeHostParentID  = "probe:host-parent-lineage"
	probeHostExecution = "probe:host-execution-id"
	// probeHostStanding is what the call itself was doing at the reading:
	// admitted or held back, its child in the provider or not.
	probeHostStanding = "probe:host-standing"
)

// hostSlashInput is what a person types. The task carries the child sentinel so
// the scripted provider knows how this arm's child behaves.
func hostSlashInput(arm string) string {
	child := childHang
	if arm == armHostCompleted {
		child = childDone
	}
	return "/" + probeSkillName + " " + child + " host-started work"
}

// runHostExecConstruct drives one interactive slash invocation and dies where
// this arm is about. The queued arm fills the ceiling first with a background
// fleet, which is dispatched through Run rather than Submit: Run is never
// admitted through the turn gate, so the slash the arm measures can still be.
func runHostExecConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	if arm == armHostQueued {
		go func() { _ = ctrl.Run(ctx, fanOutTurn(turn+1, fleetHolderSentinel)) }()
		if err := waitForCeiling(prov); err != nil {
			return err
		}
	}
	go ctrl.Submit(hostSlashInput(arm))
	if err := waitForHostExec(ctrl, prov, arm); err != nil {
		return err
	}
	stop := ""
	if arm == armHostCancel {
		stop = stopHostExec(ctrl, prov)
	}
	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	recordHostIdentities(&obs, ctrl, sink, prov, arm)
	if stop != "" {
		obs.Progress[probeStopReached] = []string{stop}
	}
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// recordHostIdentities writes down all four names this run could be known by,
// at one instant. A single "identity: none" would hide which of them exists and
// which does not, and that difference is the whole question.
func recordHostIdentities(obs *Observation, ctrl *control.Controller, sink *graphSink, prov *scripted, arm string) {
	obs.Progress[probeChildEntered] = []string{fmt.Sprint(prov.fleets.held(childEntered(hostChildSentinel(arm))))}
	obs.Progress[probeHostDispatch] = []string{orNone(join(hostDispatchIDs(sink)))}
	obs.Progress[probeHostChildRef] = []string{orNone(join(hostChildRefs(*obs)))}
	obs.Progress[probeHostParentID] = []string{orNone(join(hostParentLineage(*obs)))}
	obs.Progress[probeHostExecution] = []string{orNone(join(hostExecutions(*obs)))}
	obs.Progress[probeHostStanding] = []string{hostStanding(ctrl, prov, arm)}
}

func hostChildSentinel(arm string) string {
	if arm == armHostCompleted {
		return childDone
	}
	return childHang
}

// hostDispatchIDs are the synthetic event ids the host minted so the child's
// activity could nest under something on screen. They are UI identity, not a
// call the model made, which is why no persisted lineage may carry one.
func hostDispatchIDs(sink *graphSink) []string {
	var out []string
	for id := range sink.toolIDs() {
		if strings.HasPrefix(id, "slash-skill-") {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// hostChildRefs are the store's own artifact ids. They exist only once a child
// transcript has been prepared, which is why they cannot serve as the identity
// of work the scheduler is still holding back.
func hostChildRefs(o Observation) []string {
	var out []string
	for _, f := range o.Children.Facts {
		if f.Kind == "skill" {
			out = append(out, f.Ref+"="+f.Status)
		}
	}
	sort.Strings(out)
	return out
}

func hostParentLineage(o Observation) []string {
	var out []string
	for _, f := range o.Children.Facts {
		if f.Kind == "skill" {
			out = append(out, f.Ref+"→"+orDash(f.ParentToolCallID))
		}
	}
	sort.Strings(out)
	return out
}

func hostExecutions(o Observation) []string {
	var out []string
	for _, e := range o.Executions {
		if ofHostExecution(e.ID) || strings.Contains(e.Name, probeSkillName) {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return out
}

// ofHostExecution matches both names this work can go by: the execution the host
// owns, and the synthetic event id it mints for the screen. Reading only the
// second is how an arm reports absence when what changed is the identity.
func ofHostExecution(id string) bool {
	return strings.HasPrefix(id, "host-") || strings.HasPrefix(id, "slash-skill-")
}

func hostStanding(ctrl *control.Controller, prov *scripted, arm string) string {
	return fmt.Sprintf("turn=%v ceiling=%t child=%t", ctrl.Running(),
		prov.fleets.held(childEntered(childHold)),
		prov.fleets.held(childEntered(hostChildSentinel(arm))))
}

func waitForCeiling(prov *scripted) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if prov.fleets.held(childEntered(childHold)) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errUnexpected("a fan-out holding the ceiling", "none reached the provider")
}

// waitForHostExec blocks until the invocation stands where this arm dies. The
// queued arm asks a negative — the child never arrived while the ceiling was
// full — so it settles and asks again rather than reading a race.
func waitForHostExec(ctrl *control.Controller, prov *scripted, arm string) error {
	child := hostChildSentinel(arm)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		switch arm {
		case armHostQueued:
			if ctrl.Running() && !prov.fleets.held(childEntered(child)) {
				time.Sleep(2 * time.Second)
				if prov.fleets.held(childEntered(child)) {
					return errUnexpected("a host invocation the scheduler was still holding back", "its child ran")
				}
				return nil
			}
		case armHostCompleted:
			if prov.fleets.held(childLeft(child)) && !ctrl.Running() {
				return nil
			}
		default:
			if prov.fleets.held(childEntered(child)) {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errUnexpected("a host invocation "+hostWanted(arm), hostStanding(ctrl, prov, arm))
}

func hostWanted(arm string) string {
	switch arm {
	case armHostQueued:
		return "held back by a full ceiling"
	case armHostCompleted:
		return "that finished and closed its turn"
	}
	return "with its child inside the provider"
}

// stopHostExec stops the work the way a person does: the turn's own cancel. A
// slash invocation is admitted through the same gate a typed message is, so
// that gate is the owner of its stop.
func stopHostExec(ctrl *control.Controller, prov *scripted) string {
	stopped := time.Now()
	ctrl.Cancel()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && ctrl.Running() {
		time.Sleep(50 * time.Millisecond)
	}
	live := "gone"
	if prov.fleets.held(childEntered(childHang)) && !prov.fleets.held(childLeft(childHang)) {
		live = "still running"
	}
	return fmt.Sprintf("turn=%s work=%s ctx=%t after=%s", turnStanding(ctrl, armHostCancel), live,
		prov.fleets.held(childCtxDone(childHang)), time.Since(stopped).Round(time.Second))
}

// The verdicts this arm needs. They are separate words because the failure
// modes are separate facts, and one label over all of them would say less than
// any of them does.
const (
	// verdictIdentityGap is work the host admitted that no identity outliving
	// the process names. It is not about a lost record: none of the candidate
	// identities can carry one.
	verdictIdentityGap = "IDENTITY-GAP"
	// verdictResultDurable is an invocation whose child record survives while
	// the execution that produced it does not. A reader can see that a child of
	// this name was cut; nothing says which invocation it belonged to.
	verdictResultDurable = "record-only"
)

func hostExecRows(arm string, before, after Observation) []row {
	return []row{
		hostAdmissionRow(arm, before),
		hostIdentityRow(before),
		hostGraphRow(before),
		hostJournalRow(before, after),
		hostStoreRow(before, after),
		hostLineageOracleRow(before, after),
		hostPreStartIdentityRow(arm, before, after),
		hostCancelRow(arm, before),
		hostRestartRow(arm, before, after),
	}
}

func hostAdmissionRow(arm string, before Observation) row {
	entered := firstProgress(before, probeChildEntered) == "true"
	answer, verdict := "admitted: the child reached the provider", verdictHolds
	switch {
	case arm == armHostQueued && !entered:
		answer = "refused: the ceiling was full and the child never arrived"
	case arm == armHostQueued:
		answer, verdict = "not held back: the child ran with the ceiling full", verdictViolated
	case !entered:
		answer, verdict = "the child never arrived", verdictNotMeasured
	}
	return row{
		Semantic: "does it ask the session scheduler", Authority: skillAuthority,
		Artifact: "none observed", Reconstruction: "in-process only",
		Before: answer + " [" + firstProgress(before, probeHostStanding) + "]",
		After:  "—", Verdict: verdict,
	}
}

// hostIdentityRow prints all four names at once. This is the row the arm exists
// for: for a model-delegated run the execution and the parent call are the same
// string, and that has only ever held because a tool call produced every durable
// execution there was.
func hostIdentityRow(before Observation) row {
	return row{
		Semantic: "the four names this run could be known by", Authority: crossAuthority,
		Artifact:       "the event stream, subagents/<ref>.meta.json, <stem>.execution.jsonl",
		Reconstruction: "read apart, because they may not coincide here",
		Before: fmt.Sprintf("dispatch=%s child=%s lineage=%s execution=%s",
			firstProgress(before, probeHostDispatch), firstProgress(before, probeHostChildRef),
			firstProgress(before, probeHostParentID), firstProgress(before, probeHostExecution)),
		After: "—", Verdict: verdictHolds,
	}
}

func hostGraphRow(before Observation) row {
	var drawn []string
	for _, n := range before.Graph.Nodes {
		if ofHostExecution(n.ID) {
			drawn = append(drawn, n.ID+":"+string(n.State))
		}
	}
	sort.Strings(drawn)
	return row{
		Semantic: "does the live graph hold it", Authority: liveAuthority,
		Artifact: "none observed", Reconstruction: "in-process only",
		Before: fmt.Sprintf("%s (graph held %d node(s))", orNone(join(drawn)), len(before.Graph.Nodes)),
		After:  "—", Verdict: presenceVerdict(len(drawn) > 0),
	}
}

func hostJournalRow(before, after Observation) row {
	return presenceRow("does the journal record it", journalAuthority, "<stem>.execution.jsonl",
		"ExecutionHistory over the whole session", hostExecutions(before), hostExecutions(after))
}

func hostStoreRow(before, after Observation) row {
	return presenceRow("what the store holds for it", storeAuthority, "subagents/<ref>.meta.json",
		"ListSubagentsByParent over the transcript stem", hostChildRefs(before), hostChildRefs(after))
}

// hostLineageOracleRow is the contract Step 8 respected and this must not
// weaken. A synthetic host event id is UI identity; persisting one as a parent
// would say the model issued a call it never made.
func hostLineageOracleRow(before, after Observation) row {
	leaked := hostSyntheticLeaks(before)
	leaked = append(leaked, hostSyntheticLeaks(after)...)
	empty := hostLineageEmpty(before) && hostLineageEmpty(after)
	answer, verdict := "no persisted lineage names a synthetic host id, and every parent is empty", verdictHolds
	switch {
	case len(leaked) > 0:
		answer, verdict = "a synthetic host id was persisted as lineage: "+join(leaked), verdictViolated
	case !empty:
		answer, verdict = "a host-started run was filed under a parent call: "+
			join(hostParentLineage(after)), verdictViolated
	case len(hostChildRefs(before))+len(hostChildRefs(after)) == 0:
		verdict = verdictNotMeasured
		answer = "no child record exists to carry a lineage"
	}
	return row{
		Semantic: "synthetic host id persisted as lineage", Authority: storeAuthority,
		Artifact: "subagents/<ref>.meta.json", Reconstruction: "every persisted parent, read for host event ids",
		Before: answer, After: "—", Verdict: verdict,
	}
}

func hostSyntheticLeaks(o Observation) []string {
	var out []string
	for _, f := range o.Children.Facts {
		if strings.HasPrefix(f.ParentToolCallID, "slash-skill-") {
			out = append(out, f.Ref+"→"+f.ParentToolCallID)
		}
	}
	return out
}

func hostLineageEmpty(o Observation) bool {
	for _, f := range o.Children.Facts {
		if f.Kind == "skill" && strings.TrimSpace(f.ParentToolCallID) != "" {
			return false
		}
	}
	return true
}

// hostPreStartIdentityRow is what decides where an identity would have to be
// created. Work the scheduler is holding back has no child artifact yet, so a
// store ref cannot name it; if nothing else survives the process either, no
// candidate identity exists at the moment the work first became real.
func hostPreStartIdentityRow(arm string, before, after Observation) row {
	survives := len(hostExecutions(after)) > 0
	answer := "nothing that outlives the process names this work"
	verdict := verdictIdentityGap
	if survives {
		answer, verdict = "an execution the journal opened: "+join(hostExecutions(after)), verdictHolds
	}
	if arm != armHostQueued && firstProgress(before, probeChildEntered) != "true" {
		verdict = verdictNotMeasured
	}
	return row{
		Semantic: "an identity that exists before the child does", Authority: crossAuthority,
		Artifact:       "<stem>.execution.jsonl",
		Reconstruction: "the candidates, read against what the work needs",
		Before:         hostAtDeath(arm), After: answer, Verdict: verdict,
	}
}

func hostAtDeath(arm string) string {
	switch arm {
	case armHostQueued:
		return "admitted-domain work, held back, no child artifact yet"
	case armHostCompleted:
		return "ran and returned"
	}
	return "executing inside the provider"
}

func hostCancelRow(arm string, before Observation) row {
	if arm != armHostCancel {
		return row{
			Semantic: "what a stop reaches", Authority: "the turn's own cancel",
			Artifact: "none observed", Reconstruction: "in-process only",
			Before: "not stopped in this arm", After: "—", Verdict: verdictNotMeasured,
		}
	}
	return row{
		Semantic: "what a stop reaches", Authority: "the turn's own cancel",
		Artifact:       "subagents/<ref>.meta.json",
		Reconstruction: "the live stop and the record it left, kept apart",
		Before:         firstProgress(before, probeStopReached) + " store=" + orNone(join(hostChildRefs(before))),
		After:          "—", Verdict: verdictHolds,
	}
}

// hostRestartRow classifies what the next process inherits. An answer that
// survived while its execution did not is not a loss to the user, so it is not
// called one; work that was executing and left nothing is.
func hostRestartRow(arm string, before, after Observation) row {
	exec, children := hostExecutions(after), hostChildRefs(after)
	rebuilt := hostRebuilt(after)
	answer := fmt.Sprintf("execution=%s store=%s rebuilt=%s",
		orNone(join(exec)), orNone(join(children)), orNone(join(rebuilt)))
	return row{
		Semantic: "what a restart inherits", Authority: rebuildAuthority,
		Artifact:       "<stem>.execution.jsonl + subagents/<ref>.meta.json",
		Reconstruction: "execgraph.Rebuild, then the store beside it",
		Before:         hostAtDeath(arm), After: answer,
		Verdict: hostRestartVerdict(arm, before, exec, children),
	}
}

// hostRestartVerdict is driven by what a next process can actually hold, not by
// what the arm expected. A child record with nothing to place it is not silence:
// a reader can still see that a child of this name was cut. What it cannot do is
// say which invocation that was, and the identity row answers that separately —
// folding the two would report a surviving record as a total loss.
func hostRestartVerdict(arm string, before Observation, exec, children []string) string {
	switch {
	case len(exec) > 0:
		return verdictHolds
	case len(children) > 0:
		return verdictResultDurable
	case firstProgress(before, probeChildEntered) == "true" || arm == armHostQueued:
		return graphLostSilent
	default:
		return verdictNotMeasured
	}
}

// hostRebuilt is what a restart draws for this invocation. The whole rebuilt
// graph is wider than the arm — the queued arm fills its ceiling with a fan-out,
// and a fan-out records everything — so reading its nodes as the invocation's is
// how an arm reports a gap as closed by evidence belonging to something else.
func hostRebuilt(o Observation) []string {
	var out []string
	for _, n := range rebuiltGraph(o).Graph.Nodes {
		if n.Kind == agentgraph.KindGroup {
			continue
		}
		if ofHostExecution(n.ID) || strings.Contains(n.Label, probeSkillName) {
			out = append(out, n.ID+":"+string(n.State))
		}
	}
	sort.Strings(out)
	return out
}

// hostExecArmInvalid reports the premise this arm could not establish.
func hostExecArmInvalid(arm string, before Observation) string {
	entered := firstProgress(before, probeChildEntered) == "true"
	switch {
	case arm == armHostQueued && entered:
		return "the invocation ran while the ceiling was meant to be full"
	case arm != armHostQueued && !entered:
		return "the invocation's child never reached the provider"
	case len(hostDispatchIDs2(before)) == 0:
		return "the host minted no dispatch id, so this was not the slash surface"
	}
	return ""
}

// hostDispatchIDs2 reads the dispatch id back off the observation, where the
// construct phase recorded what only a live sink could see.
func hostDispatchIDs2(o Observation) []string {
	if got := firstProgress(o, probeHostDispatch); got != "none" && got != "unrecorded" {
		return strings.Split(got, ", ")
	}
	return nil
}

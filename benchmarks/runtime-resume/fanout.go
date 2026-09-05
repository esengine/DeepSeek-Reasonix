package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

// The three fan-out arms. Each dies at a different point in one fleet's life,
// because "what survives" has no single answer: an item that finished, one
// still executing, and one the graph knows about without ever running are
// carried by different host state, and only separating them says which.
const (
	// armGraphCompleted lets the fleet finish and the turn close before the
	// process exits. Nothing is interrupted, so a loss here is not about
	// interruption at all — it says execution provenance itself is ephemeral.
	armGraphCompleted = "graph-completed"
	// armGraphRunning kills the process with a child mid-execution. It asks the
	// one question a completed fleet cannot: what does the new process believe
	// about work whose owner is gone?
	armGraphRunning = "graph-running"
	// armGraphMixed carries every state at once — completed, running, adopted,
	// failed, skipped — plus a read grant, a write grant, and a dependency edge.
	// It is the density arm: one death, every semantic the graph claims to hold.
	armGraphMixed = "graph-mixed"
)

func graphArm(name string) bool {
	return name == armGraphCompleted || name == armGraphRunning || name == armGraphMixed
}

// Child sentinels. A child's prompt decides what its run does, and the probe
// reads the prompt off the request's last user message rather than scanning the
// whole conversation: the parent's own transcript quotes every fleet argument,
// so a scan would make the parent answer as its own children.
const (
	childDone = "PROBE-CHILD-DONE"
	childHang = "PROBE-CHILD-HANG"
	childFail = "PROBE-CHILD-FAIL"
)

// Parent sentinels, one per fleet the arm dispatches.
const (
	fleetWarmSentinel  = "PROBE-FLEET-WARM"
	fleetMixedSentinel = "PROBE-FLEET-MIXED"
	fleetPairSentinel  = "PROBE-FLEET-PAIR"
	// The scheduler arms. Holder fills a ceiling and never ends; refused asks
	// for admission the scheduler must deny. Two sentinels rather than one
	// because the claim and transition arms need the holder to be already
	// admitted before the second fleet is dispatched.
	fleetHolderSentinel  = "PROBE-FLEET-HOLDER"
	fleetRefusedSentinel = "PROBE-FLEET-REFUSED"
	// The terminal arms. Their fan-outs run to completion, because a skip is
	// published only in the closing delta; the turn is then held open so the
	// process still dies mid-turn.
	fleetTerminalSentinel = "PROBE-FLEET-TERMINAL"
	// Named so it is not contained in PROBE-PARALLEL-IDENTITY. Sentinels are
	// matched by substring, so one inside another dispatches a fan-out its arm
	// never asked for — this pair silently ran cancelParallel in the identity
	// arm, whose rows then read a scenario nobody wrote.
	parallelSentinel      = "PROBE-PARALLEL-CANCEL"
	fleetOutcomesSentinel = "PROBE-FLEET-OUTCOMES"
	fleetDeriveSentinel   = "PROBE-FLEET-DERIVE"
	fleetIdentitySentinel = "PROBE-FLEET-IDENTITY"
	parallelIdentity      = "PROBE-PARALLEL-IDENTITY"
	// The active-store arm. Its fan-out shares one ceiling with a lone
	// delegation, so the four populations it needs are reached in one death.
	fleetActiveSentinel = "PROBE-FLEET-ACTIVE"
)

// childHold is a child that reports holding its slot before it blocks, so a
// later fleet can be dispatched knowing the ceiling is already occupied. Timing
// it any other way races the scheduler.
const (
	childHold = "PROBE-CHILD-HOLD"
	// childRelease finishes only when the arm frees it, which is how capacity
	// is returned while a refusal is still standing.
	childRelease = "PROBE-CHILD-RELEASE"
	// childFailLate fails only once the arm frees it, so two failures in one
	// fan-out have a decided order instead of a raced one. Its wording shares no
	// prefix with childFail: the sentinels are matched by substring, so a name
	// containing another's makes the shorter one win and the delay disappear.
	childFailLate = "PROBE-CHILD-DELAYEDFAIL"
)

// probeClaimPath is the write path the claim arm overlaps. The refused writer
// asks for a path inside it, which is a conflict no ceiling change releases.
const (
	probeClaimPath  = "probe-claim"
	probeClaimInner = "probe-claim/inner"
)

// subagentRefPattern finds the references an earlier fleet's aggregate reported.
// The mixed arm adopts one of them, which is the only way to reach StateAdopted
// through the tool's own contract rather than by constructing the state.
var subagentRefPattern = regexp.MustCompile(`sa_[A-Za-z0-9_-]+`)

// childScript answers one delegated run. Hanging is expressed as a context read
// so the child holds its scheduler slot until the process dies — a sleep long
// enough to look the same would still be a race against the probe's deadline.
func (s *scripted) childScript(ctx context.Context, sentinel string) []provider.Chunk {
	// Reaching here is the child's own proof that it started, and returning is
	// its proof that something let it go: an arm asking whether a cancellation
	// reached a running child has no other way to know.
	s.fleets.first(childEntered(sentinel))
	defer s.fleets.first(childLeft(sentinel))
	switch sentinel {
	case childHold:
		// Reaching a provider call means the slot is already held, which is the
		// only moment a later fleet can be dispatched against a full ceiling
		// without racing it.
		s.holding.Do(func() { close(s.held) })
		<-ctx.Done()
		return nil
	case childRelease:
		<-s.release
		return append(text("Child released."), done())
	case childFailLate:
		<-s.release
		return []provider.Chunk{{Type: provider.ChunkError, Err: fmt.Errorf("probe child failed on release")}}
	case childHang:
		<-ctx.Done()
		// The context closed, and this is the only place that can say so: a
		// caller that stopped waiting leaves the same absence behind.
		s.fleets.first(childCtxDone(sentinel))
		return nil
	case childFail:
		return []provider.Chunk{{Type: provider.ChunkError, Err: fmt.Errorf("probe child failed on purpose")}}
	default:
		return append(text("Child answered."), done())
	}
}

// askedInPrompt reports whether this request's own user turns carry a sentinel.
// The user side only: a fleet's arguments live in an assistant message, so a
// whole-conversation scan makes a parent answer as one of its own children.
// Every user turn, not the last: the host appends its own notes after the
// prompt, and the last user message in a request is frequently one of those.
func askedInPrompt(req provider.Request, sentinel string) bool {
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, sentinel) {
			return true
		}
	}
	return false
}

// childSentinel reports which child script this request is, empty when the
// request is a parent turn.
func childSentinel(req provider.Request) string {
	for _, sentinel := range []string{childDone, childHang, childFail, childHold, childRelease, childFailLate} {
		if askedInPrompt(req, sentinel) {
			return sentinel
		}
	}
	return ""
}

// fanOutTurnRecorded reports whether the dispatching turn is in these messages.
// The probe reads its own sentinel rather than counting rows: a turn appended
// at some later point would still be there, and only the prompt says whether
// this is the turn the fan-out was opened in.
func fanOutTurnRecorded(msgs []provider.Message) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, fleetPairSentinel) || strings.Contains(m.Content, fleetMixedSentinel) {
			return true
		}
	}
	return false
}

// fleetCall renders one fleet dispatch. Nothing closes the round it opens: all
// three arms die inside the turn, so the host's final-answer readiness check
// never runs and the arms measure a process boundary rather than a task list.
func fleetCall(id string, tasks []map[string]any) []provider.Chunk {
	args, _ := json.Marshal(map[string]any{"tasks": tasks})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: id, Name: "fleet", Arguments: string(args)},
	}}
}

// warmFleet finishes two children so a later fleet has a completed reference to
// adopt. Both are read-only: nothing about the warm-up is under measurement.
func warmFleet() []provider.Chunk {
	return fleetCall("probe_fleet_warm", []map[string]any{
		{"id": "w1", "prompt": childDone + " warm one", "description": "warm one", "read_only": true},
		{"id": "w2", "prompt": childDone + " warm two", "description": "warm two", "read_only": true},
	})
}

// pairFleet is the two-item shape both single-state arms use. The running arm
// dies with the second item still executing; the completed arm lets both
// finish, so the two differ by when the process exits and nothing else.
func pairFleet(second string) []provider.Chunk {
	return fleetCall("probe_fleet_pair", []map[string]any{
		{"id": "p1", "prompt": childDone + " pair one", "description": "pair one", "read_only": true},
		{"id": "p2", "prompt": second + " pair two", "description": "pair two", "read_only": true},
	})
}

// mixedFleet is the density arm's dispatch: one item per terminal the graph can
// record, plus one still running, and the two grants told apart. The write item
// declares a path so preflight admits it beside the readers; it never writes,
// because the grant is the envelope under measurement, not the effect.
func mixedFleet(adoptRef string) []provider.Chunk {
	return fleetCall("probe_fleet_mixed", []map[string]any{
		// Model and effort are named so the identity row measures something: two
		// nodes agreeing that a field is empty proves nothing about recovering it.
		{"id": "m1", "prompt": childDone + " mixed one", "description": "completes", "read_only": true,
			"model": probeModelRef, "effort": "high"},
		{"id": "m2", "prompt": childFail + " mixed two", "description": "fails", "read_only": true},
		{"id": "m3", "prompt": childDone + " mixed three", "description": "skipped by m2", "read_only": true, "depends_on": []string{"m2"}},
		{"id": "m4", "adopt_ref": adoptRef, "description": "adopted"},
		{"id": "m5", "prompt": childHang + " mixed five", "description": "running at death", "write_paths": []string{"probe-fanout-out"}},
	})
}

// opens reports whether this round is the one that dispatches that fleet.
func (s *scripted) opens(req provider.Request, sentinel string) bool {
	return askedInPrompt(req, sentinel) && s.fleets.first(sentinel)
}

// opensRun is the same for the fan-out an arm is built around: the terminal and
// derive arms hold the turn open once theirs has gone out.
func (s *scripted) opensRun(req provider.Request, sentinel string) bool {
	if !s.opens(req, sentinel) {
		return false
	}
	s.opened.Store(true)
	return true
}

// fanOut answers a parent turn that opens a fleet, at most once per sentinel.
// The sentinel is read off the current user turn, and the latch is what stops
// the round that receives the aggregate from dispatching the same fleet again.
func (s *scripted) fanOut(req provider.Request) ([]provider.Chunk, bool) {
	switch {
	case s.opens(req, fleetWarmSentinel):
		return append(warmFleet(), done()), true
	case s.opens(req, fleetPairSentinel):
		if s.arm == armWaitSlots {
			return append(slotsFleet(), done()), true
		}
		second := childDone
		if s.arm == armGraphRunning {
			second = childHang
		}
		return append(pairFleet(second), done()), true
	case s.opens(req, fleetSettledSentinel):
		return append(settledFleet(), done()), true
	case s.opens(req, fleetLiveSentinel):
		return append(liveFleet(), done()), true
	case s.opens(req, fleetMixedSentinel):
		if s.arm == armTerminalAdopted {
			return append(adoptOnlyFleet(adoptableRef(req)), done()), true
		}
		if s.arm == armUIGraphMixed {
			return append(uiMixedFleet(adoptableRef(req)), done()), true
		}
		return append(mixedFleet(adoptableRef(req)), done()), true
	case s.opensRun(req, fleetIdentitySentinel):
		return append(identityFleet(adoptableRef(req)), done()), true
	case s.opens(req, parallelIdentity):
		return append(identityParallel(), done()), true
	case s.opensRun(req, fleetDeriveSentinel):
		return append(deriveFleet(s.arm, adoptableRef(req)), done()), true
	case s.opensRun(req, fleetOutcomesSentinel):
		return append(outcomesFleet(), done()), true
	case s.opensRun(req, fleetTerminalSentinel):
		return append(terminalFleet(s.arm), done()), true
	case s.opensRun(req, parallelSentinel):
		return append(cancelParallel(), done()), true
	case s.fleets.held(taskSentinel) && s.opens(req, fleetActiveSentinel):
		// The delegation goes first and its own child says when the slot is
		// taken. Dispatching before that lets the fan-out take the whole
		// ceiling, and the arm loses the entry point it exists to compare.
		<-s.held
		return append(activeStoreFleet(), done()), true
	case s.opens(req, fleetHolderSentinel):
		return append(holderFleet(s.arm), done()), true
	case s.opens(req, fleetRefusedSentinel):
		// The holder's own child says when its ceiling is occupied. Dispatching
		// before that lets the refused item win the race and start.
		<-s.held
		return append(refusedFleet(s.arm), done()), true
	}
	return nil, false
}

// holderFleet occupies the ceiling this arm measures. The slots arm needs no
// holder — its refused item is in the same fleet as the runs that fill the
// ceiling — so only the arms that need one dispatch it in the background.
func holderFleet(arm string) []provider.Chunk {
	tasks := []map[string]any{
		{"id": "h1", "prompt": childHold + " holder", "description": "holds the ceiling", "write_paths": []string{probeClaimPath}},
		{"id": "h2", "prompt": childHang + " filler", "description": "filler", "read_only": true},
	}
	if arm == armWaitTransition {
		// The second run is what the arm later releases: freeing total capacity
		// while the writer ceiling stays full is how the blocker changes.
		tasks[1] = map[string]any{"id": "h2", "prompt": childRelease + " releasable", "description": "released mid-wait", "read_only": true}
	}
	if bothHoldersReport(arm) {
		tasks[1] = map[string]any{"id": "h2", "prompt": childHold + " second holder", "description": "holds the ceiling", "read_only": true}
	}
	return backgroundFleetCall("probe_fleet_holder", tasks)
}

// bothHoldersReport names the arms that leave one slot free. Either holder may
// win it, so both report holding, and the refused delegation's own child is then
// the only arrival that means anything — a filler that hung under the same
// sentinel would be read as the subject having started.
func bothHoldersReport(arm string) bool {
	return queuedTaskArm(arm) || queuedSkillArm(arm) || arm == armHostQueued
}

// refusedFleet asks for admission the scheduler must deny, with every check
// above this arm's cause already cleared.
func refusedFleet(arm string) []provider.Chunk {
	refused := map[string]any{"id": "r1", "prompt": childHang + " refused", "description": "refused admission"}
	switch arm {
	case armWaitWriters:
		// Disjoint from the holder's path, so only the writer ceiling can
		// refuse it.
		refused["write_paths"] = []string{"probe-writers-own"}
	case armWaitClaim:
		refused["write_paths"] = []string{probeClaimInner}
	default:
		refused["write_paths"] = []string{"probe-transition-own"}
	}
	return fleetCall("probe_fleet_refused", []map[string]any{
		refused,
		{"id": "r2", "prompt": childHang + " sibling", "description": "sibling", "read_only": true},
	})
}

// terminalFleet runs to completion holding the disposition its arm measures.
// The skipped arm cuts a branch with a failure; the context arm delivers a real
// answer across an ordering edge, which is the only way a context edge appears.
func terminalFleet(arm string) []provider.Chunk {
	upstream := map[string]any{"id": "up", "prompt": childDone + " upstream", "description": "upstream", "read_only": true}
	if arm == armTerminalSkippedDep {
		upstream["prompt"] = childFail + " upstream"
	}
	return fleetCall("probe_fleet_terminal", []map[string]any{
		upstream,
		{"id": "down", "prompt": childDone + " downstream", "description": "downstream", "read_only": true, "depends_on": []string{"up"}},
	})
}

// cancelParallel opens more items than the session admits at once, so the arm
// can interrupt the group while one has started and another has not. That split
// is what parallel_tasks reports as cancelled against skipped.
func cancelParallel() []provider.Chunk {
	args, _ := json.Marshal(map[string]any{"tasks": []map[string]any{
		{"prompt": childHang + " first", "description": "started"},
		{"prompt": childHang + " second", "description": "never started"},
		{"prompt": childHang + " third", "description": "never started"},
	}})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: "probe_parallel", Name: "parallel_tasks", Arguments: string(args)},
	}}
}

// adoptOnlyFleet runs to completion with one item adopted and one that ran.
// Nothing hangs: the arm compares two settled items whose only difference is
// that one executed, which a fan-out still in flight could not show.
func adoptOnlyFleet(adoptRef string) []provider.Chunk {
	return fleetCall("probe_fleet_terminal", []map[string]any{
		{"id": "a1", "adopt_ref": adoptRef, "description": "adopted"},
		{"id": "a2", "prompt": childDone + " ran", "description": "ran", "read_only": true},
	})
}

// outcomesFleet reaches all three executed terminals in one group: one item
// completes, one fails, and two are still running when the arm interrupts. The
// store's record of each is then compared under identical conditions.
func outcomesFleet() []provider.Chunk {
	return fleetCall("probe_fleet_outcomes", []map[string]any{
		{"id": "o1", "prompt": childDone + " completes", "description": "completes", "read_only": true},
		{"id": "o2", "prompt": childFail + " fails", "description": "fails", "read_only": true},
		{"id": "o3", "prompt": childHang + " cancelled", "description": "cancelled", "read_only": true},
		{"id": "o4", "prompt": childHang + " cancelled too", "description": "cancelled too", "read_only": true},
	})
}

// identityFleet names a worker identity four ways: not at all, by model, by
// effort, and not at all because nothing runs. What the graph then shows for
// each is what says which layer of the resolution it is reporting.
func identityFleet(adoptRef string) []provider.Chunk {
	return fleetCall("probe_fleet_identity", []map[string]any{
		{"id": "i1", "prompt": childDone + " default", "description": "inherits", "read_only": true},
		{"id": "i2", "prompt": childDone + " override", "description": "names a model", "read_only": true,
			"model": probeAltModelRef, "effort": "high"},
		{"id": "i3", "prompt": childDone + " effort only", "description": "names an effort", "read_only": true,
			"effort": "low"},
		{"id": "i4", "adopt_ref": adoptRef, "description": "runs nothing"},
	})
}

// identityParallel asks the same of the other producer, which resolves through
// a shorter chain: it has no profile layer to consult.
func identityParallel() []provider.Chunk {
	args, _ := json.Marshal(map[string]any{"tasks": []map[string]any{
		{"prompt": childDone + " p default", "description": "inherits"},
		{"prompt": childDone + " p override", "description": "names a model",
			"model": probeAltModelRef, "effort": "high"},
	}})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: "probe_parallel_identity", Name: "parallel_tasks", Arguments: string(args)},
	}}
}

// deriveFleet orders one dependent behind two upstreams. The skip arms end both
// upstreams without an answer, one immediately and one on release, so which of
// them the fan-out names is decided by the order rather than by a race; the
// answered arm gives the dependent one completed and one adopted upstream, so
// it must run and both deliveries must be accounted for.
func deriveFleet(arm, adoptRef string) []provider.Chunk {
	first, second := childFail+" first", childFailLate+" second"
	if arm == armDeriveSkipFlip {
		first, second = childFailLate+" first", childFail+" second"
	}
	up := []map[string]any{
		{"id": "a", "prompt": first, "description": "upstream a", "read_only": true},
		{"id": "b", "prompt": second, "description": "upstream b", "read_only": true},
	}
	if arm == armDeriveAnswered {
		up = []map[string]any{
			{"id": "a", "prompt": childDone + " first", "description": "upstream a", "read_only": true},
			{"id": "b", "adopt_ref": adoptRef, "description": "upstream b"},
		}
	}
	return fleetCall("probe_fleet_derive", append(up, map[string]any{
		"id": "c", "prompt": childDone + " dependent", "description": "dependent",
		"read_only": true, "depends_on": []string{"a", "b"},
	}))
}

// activeStoreFleet reaches three fan-out populations under one ceiling. The
// first item settles and frees the slot the other two then contend for, so one
// executes and one is refused — which of them is the scheduler's to decide, and
// the arm reads the populations off the journal rather than naming them here.
func activeStoreFleet() []provider.Chunk {
	return fleetCall(activeFleetCallID, []map[string]any{
		{"id": "a1", "prompt": childDone + " settles", "description": "settles", "read_only": true},
		{"id": "a2", "prompt": childHang + " holds", "description": "contends for the freed slot",
			"read_only": true, "depends_on": []string{"a1"}},
		{"id": "a3", "prompt": childHang + " contends", "description": "contends for the freed slot",
			"read_only": true, "depends_on": []string{"a1"}},
	})
}

// slotsFleet fills the total ceiling and asks for one more, all read-only: a
// reader clears every writer check by not being a writer.
func slotsFleet() []provider.Chunk {
	return fleetCall("probe_fleet_refused", []map[string]any{
		{"id": "s1", "prompt": childHang + " one", "description": "fills a slot", "read_only": true},
		{"id": "s2", "prompt": childHang + " two", "description": "fills a slot", "read_only": true},
		{"id": "s3", "prompt": childHang + " three", "description": "refused admission", "read_only": true},
	})
}

// backgroundFleetCall dispatches a fleet that returns a job id at once. The
// holder has to keep holding while a later fleet is dispatched, which a
// foreground call cannot do: it does not return until every item has settled.
func backgroundFleetCall(id string, tasks []map[string]any) []provider.Chunk {
	args, _ := json.Marshal(map[string]any{"tasks": tasks, "run_in_background": true})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: id, Name: "fleet", Arguments: string(args)},
	}}
}

// adoptableRef picks a reference the warm-up fleet reported. It reads the
// conversation the model reads: the aggregate names each child's reference, and
// an arm that cannot find one has not established its premise.
func adoptableRef(req provider.Request) string {
	for _, m := range slices.Backward(req.Messages) {
		if found := subagentRefPattern.FindAllString(m.Content, -1); len(found) > 0 {
			return found[len(found)-1]
		}
	}
	return ""
}

// fanOutTurn is the prompt that opens one fleet.
func fanOutTurn(n int, sentinel string) string {
	return fmt.Sprintf("%s %s: dispatch the fan-out.", marker(n), sentinel)
}

// fanOutSentinels is what this arm's one prompt carries. The mixed arm names
// both of its fleets in the same turn: the warm-up has to finish before a
// reference can be adopted, and a fleet's aggregate returns into the same turn
// that dispatched it, so the second round dispatches the mixed fleet without
// the turn ever closing.
func fanOutSentinels(arm string) string {
	if arm == armUIGraphMixed {
		return fleetWarmSentinel + " " + fleetSettledSentinel + " " + fleetMixedSentinel
	}
	if arm == armGraphMixed {
		return fleetWarmSentinel + " " + fleetMixedSentinel
	}
	return fleetPairSentinel
}

// runFanOutConstruct drives the arm's fan-out and exits while the turn is still
// open. Every arm dies mid-turn, including the completed one: what separates
// them is which states the graph holds at that moment, and a clean shutdown
// would cancel a running child and settle its node — the one thing a process
// that dies mid-fan-out does not do.
func runFanOutConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, turn int) error {
	go func() { _ = ctrl.Run(ctx, fanOutTurn(turn+1, fanOutSentinels(arm))) }()
	if err := waitForFanOut(sink, arm); err != nil {
		return err
	}
	if err := writeObservation(root, capture("construct", arm, bootSystem, ctrl, sink, root)); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// waitForFanOut blocks until the graph shows the shape this arm was built to
// die with. Waiting on the shape rather than on a duration is what keeps the
// arm honest: a timer that fired early would report a smaller fan-out as if
// that were what the restart inherited.
func waitForFanOut(sink *graphSink, arm string) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		graph, _ := sink.snapshot()
		if fanOutReady(graph, arm) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	graph, _ := sink.snapshot()
	return errUnexpected("a fan-out holding "+wantedText(arm), nodeStates(graph))
}

// fanOutReady is the arm's premise expressed as a graph shape. The completed
// arm waits for every worker to settle, which is the opposite condition from
// the other two rather than a weaker version of it.
func fanOutReady(g agentgraph.Graph, arm string) bool {
	if arm == armGraphCompleted {
		return allWorkersSettled(g)
	}
	return holdsAll(g, wantedStates(arm))
}

// wantedStates are the states a mid-flight arm must hold at once. Skipped is
// not among them and cannot be: a fan-out publishes a skip only in its closing
// outcome delta, so nothing reads skipped until the group has ended and nothing
// is running. An item whose dependency already failed sits at pending until
// then, which is what the mixed arm dies holding.
func wantedStates(arm string) []agentgraph.NodeState {
	want := []agentgraph.NodeState{agentgraph.StateCompleted, agentgraph.StateRunning}
	if arm == armGraphMixed || arm == armUIGraphMixed {
		want = append(want, agentgraph.StateFailed, agentgraph.StateAdopted)
	}
	// The UI arm dies holding a settled fan-out beside a live one, so it also
	// reaches the two states the mixed arm cannot: a skip, which is published
	// only when a group closes, and an item a ceiling never admitted.
	if arm == armUIGraphMixed {
		want = append(want, agentgraph.StateSkipped, agentgraph.StatePending, agentgraph.StateQueued)
	}
	return want
}

func wantedText(arm string) string {
	if arm == armGraphCompleted {
		return "every worker settled"
	}
	return statesText(wantedStates(arm))
}

// allWorkersSettled reports whether every worker reached a terminal state. An
// empty state is not terminal here even though NodeState.Terminal calls it one:
// a node declared before it ran carries no state at all, and reading that as
// settled would let the arm die before the fan-out started.
func allWorkersSettled(g agentgraph.Graph) bool {
	seen := false
	for _, n := range g.Nodes {
		if n.Kind != agentgraph.KindWorker {
			continue
		}
		seen = true
		if n.State == "" || !n.State.Terminal() {
			return false
		}
	}
	return seen
}

// holdsAll reports whether the graph shows at least one worker in each state.
// Group nodes are excluded: a group is the call that owns the fan-out, and its
// own running state would satisfy the running requirement without any child
// being in flight.
func holdsAll(g agentgraph.Graph, want []agentgraph.NodeState) bool {
	for _, state := range want {
		found := false
		for _, n := range g.Nodes {
			if n.Kind == agentgraph.KindWorker && n.State == state {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func statesText(states []agentgraph.NodeState) string {
	out := make([]string, 0, len(states))
	for _, s := range states {
		out = append(out, string(s))
	}
	return join(out)
}

func nodeStates(g agentgraph.Graph) string {
	out := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		out = append(out, n.ID+":"+string(n.State))
	}
	return orNone(join(out))
}

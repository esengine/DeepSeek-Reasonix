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
)

// childHold is a child that reports holding its slot before it blocks, so a
// later fleet can be dispatched knowing the ceiling is already occupied. Timing
// it any other way races the scheduler.
const (
	childHold = "PROBE-CHILD-HOLD"
	// childRelease finishes only when the arm frees it, which is how capacity
	// is returned while a refusal is still standing.
	childRelease = "PROBE-CHILD-RELEASE"
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
	case childHang:
		<-ctx.Done()
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
	for _, sentinel := range []string{childDone, childHang, childFail, childHold, childRelease} {
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
		{"id": "m1", "prompt": childDone + " mixed one", "description": "completes", "read_only": true},
		{"id": "m2", "prompt": childFail + " mixed two", "description": "fails", "read_only": true},
		{"id": "m3", "prompt": childDone + " mixed three", "description": "skipped by m2", "read_only": true, "depends_on": []string{"m2"}},
		{"id": "m4", "adopt_ref": adoptRef, "description": "adopted"},
		{"id": "m5", "prompt": childHang + " mixed five", "description": "running at death", "write_paths": []string{"probe-fanout-out"}},
	})
}

// fanOut answers a parent turn that opens a fleet, at most once per sentinel.
// The sentinel is read off the current user turn, and the latch is what stops
// the round that receives the aggregate from dispatching the same fleet again.
func (s *scripted) fanOut(req provider.Request) ([]provider.Chunk, bool) {
	switch {
	case askedInPrompt(req, fleetWarmSentinel) && !s.warmed.Swap(true):
		return append(warmFleet(), done()), true
	case askedInPrompt(req, fleetPairSentinel) && !s.paired.Swap(true):
		if s.arm == armWaitSlots {
			return append(slotsFleet(), done()), true
		}
		second := childDone
		if s.arm == armGraphRunning {
			second = childHang
		}
		return append(pairFleet(second), done()), true
	case askedInPrompt(req, fleetMixedSentinel) && !s.mixed.Swap(true):
		return append(mixedFleet(adoptableRef(req)), done()), true
	case askedInPrompt(req, fleetHolderSentinel) && !s.holder.Swap(true):
		return append(holderFleet(s.arm), done()), true
	case askedInPrompt(req, fleetRefusedSentinel) && !s.refused.Swap(true):
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
	return backgroundFleetCall("probe_fleet_holder", tasks)
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
	if arm == armGraphMixed {
		want = append(want, agentgraph.StateFailed, agentgraph.StateAdopted)
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

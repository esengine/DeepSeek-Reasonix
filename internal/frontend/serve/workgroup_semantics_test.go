package serve

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/workgroup"
)

// Falsification, not measurement: each case asks whether a partition is legal
// on a turn the production assembly produced. Scripted turns are not a
// distribution, and the fold stays test-only until granularity is answered.

// groupsOf drives a turn, checks both readings still agree, and folds the cold
// one — the reading a reload has, which is the one a partition must work from.
func groupsOf(t *testing.T, tc traceCase, r workgroup.Rules) ([]workgroup.Call, []workgroup.Group) {
	t.Helper()
	reading := runTrace(t, tc)
	assertParity(t, reading)
	return workgroup.Fold(reading.replay, r)
}

func requireNoDuplicates(t *testing.T, groups []workgroup.Group) {
	t.Helper()
	if bad := workgroup.DuplicateMembership(groups); bad != "" {
		t.Fatalf("a call landed in more than one group: %s", bad)
	}
	if bad := workgroup.SealedOverAnOpenCall(groups); bad != "" {
		t.Fatalf("a group closed while a member was still running: %s", bad)
	}
}

// Several rounds of the same producer's work are one group. This is the whole
// reason WorkGroup is not the round: a round is a structural fact, and on real
// logs two of every three of them held a single call.
func TestWorkGroupMergesRoundsOfOneProducer(t *testing.T) {
	calls, groups := groupsOf(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{say("first"), callTodo("call-a", "a")},
		{say("second"), callTodo("call-b", "b")},
		{say("third"), callTodo("call-c", "c")},
		{say("done")},
	}}, workgroup.V1())
	requireNoDuplicates(t, groups)
	if len(groups) != 1 || strings.Join(groups[0].Members, ",") != "call-a,call-b,call-c" {
		t.Fatalf("three rounds of one producer = %v, want one group of all three", workgroup.Shape(groups))
	}
	// Settled says sat between every pair of them and cut nothing: message
	// adjacency is not an authority, which is what the real corpus showed.
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	rounds := map[string]bool{}
	for _, c := range calls {
		for _, r := range c.Rounds {
			rounds[r] = true
		}
	}
	if len(rounds) < 2 {
		t.Fatalf("the fixture stayed in %d round(s); it cannot show a group spanning rounds", len(rounds))
	}
}

// Two producers never share a group. Both of them do tool work here, so the
// rule has something to cut — a fixture where only one produces calls would
// pass with the rule switched off.
func TestWorkGroupSplitsAtTheProducer(t *testing.T) {
	_, groups := groupsOf(t, twoProducerCase(), workgroup.V1())
	requireNoDuplicates(t, groups)
	if len(groups) != 2 {
		t.Fatalf("groups = %v, want one per producer", workgroup.Shape(groups))
	}
	if groups[0].Source != "planner" || groups[1].Source != "executor" {
		t.Fatalf("groups = %v, want the planner's then the executor's", workgroup.Shape(groups))
	}
}

func twoProducerCase() traceCase {
	return traceCase{
		input: control.PlannerRouteMarker + " build it",
		planner: [][]provider.Chunk{
			{say("looking"), provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
				ID: "plan-read", Name: "read_file", Arguments: `{"path":"reasonix.toml"}`,
			}}},
			{say("the plan")},
		},
		executor: [][]provider.Chunk{{say("running"), callTodo("exec-a", "a")}, {say("done")}},
	}
}

// The barrier does not tear the call it landed in, and the group ends once that
// call has finished. Splitting in place is what produced 6 duplicated calls of
// 76 on a real corpus.
func TestBarrierKeepsTheCallWholeAndClosesTheGroupAfterIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		call provider.Chunk
	}{
		{"approval", provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-write", Name: "write_file", Arguments: `{"path":"note.txt","content":"hello"}`,
		}}},
		{"ask", provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-write", Name: "ask",
			Arguments: `{"questions":[{"header":"Store","question":"Which?","reason":"user_decision",` +
				`"options":[{"label":"sqlite"},{"label":"postgres"}]}]}`,
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, groups := groupsOf(t, traceCase{input: "go", interactive: true, executor: [][]provider.Chunk{
				{callTodo("call-before", "before")},
				{say("now the barrier"), tc.call},
				{say("done")},
			}}, workgroup.V1())
			requireNoDuplicates(t, groups)
			var barred *workgroup.Call
			for i := range calls {
				if calls[i].ID == "call-write" {
					barred = &calls[i]
				}
			}
			if barred == nil {
				t.Fatalf("the barred call did not materialize: %+v", calls)
			}
			if len(barred.Interruptions) == 0 {
				t.Fatalf("no barrier landed inside the call, so this proves nothing: %+v", *barred)
			}
			if !barred.SawDispatch || !barred.SawResult {
				t.Fatalf("the barrier tore the call apart: %+v", *barred)
			}
			last := groups[len(groups)-1]
			if last.ClosedBy != "barrier settled" {
				t.Fatalf("groups = %v, want the last one closed after the barred call settled",
					workgroup.Shape(groups))
			}
			if !slices.Contains(last.Members, "call-write") {
				t.Fatalf("the barred call is not in the group that closed for it: %v", workgroup.Shape(groups))
			}
		})
	}
}

// A provider-executed call is still the assistant working, so it is a member —
// even though it has no dispatch and the model never issued it.
func TestProviderCallIsAMember(t *testing.T) {
	_, groups := groupsOf(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{provider.Chunk{Type: provider.ChunkProviderTool, Text: "two results",
			ToolCall: &provider.ToolCall{ID: "srv-1", Name: "web_search", Arguments: `{"q":"x"}`}},
			say("summarised")},
	}}, workgroup.V1())
	requireNoDuplicates(t, groups)
	if len(groups) != 1 || !slices.Contains(groups[0].Members, "srv-1") {
		t.Fatalf("groups = %v, want the provider-side call as a member", workgroup.Shape(groups))
	}
}

// The host's own bookkeeping is not a step and does not cut. Counting it would
// credit the host to the model; cutting on it would fragment a turn every time
// a list advanced.
func TestHostBookkeepingIsNeitherAStepNorABoundary(t *testing.T) {
	_, groups := groupsOf(t, hostAdvanceCase(), workgroup.V1())
	requireNoDuplicates(t, groups)
	if len(groups) != 1 {
		t.Fatalf("groups = %v, want one — the host's advance cut the turn up", workgroup.Shape(groups))
	}
	for _, id := range groups[0].Members {
		if strings.HasPrefix(id, "host-advance") {
			t.Fatalf("the host's own bookkeeping counted as a step: %v", workgroup.Shape(groups))
		}
	}
}

// A sub-agent's calls stay inside the call that spawned them. Both facts point
// the same way here — the child carries a parent id and its own producer — and
// the rule reads the structural one.
func TestSubagentCallsAreNotPromoted(t *testing.T) {
	_, groups := groupsOf(t, subagentCase(), workgroup.V1())
	requireNoDuplicates(t, groups)
	if len(groups) != 1 || strings.Join(groups[0].Members, ",") != "call-task" {
		t.Fatalf("groups = %v, want only the parent call as a top-level member", workgroup.Shape(groups))
	}
}

func hostAdvanceCase() traceCase {
	return traceCase{input: "go", executor: [][]provider.Chunk{
		{say("planning"), provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-list", Name: "todo_write",
			Arguments: `{"todos":[{"content":"read the file","status":"in_progress"},{"content":"then write","status":"pending"}]}`,
		}}},
		{provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-read", Name: "read_file", Arguments: `{"path":"reasonix.toml"}`,
		}}},
		{provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-signoff", Name: "complete_step",
			Arguments: `{"step":"read the file","result":"read it","evidence":[{"kind":"files","summary":"looked at it","paths":["reasonix.toml"]}]}`,
		}}},
		// Work after the host's advance, or "does the host cut?" has nothing on
		// the far side to be cut from.
		{callTodo("call-after", "after")},
		{say("done")},
	}}
}

func subagentCase() traceCase {
	return traceCase{input: "go", executor: [][]provider.Chunk{
		{say("delegating"), provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-task", Name: "task",
			Arguments: `{"prompt":"read reasonix.toml and report","description":"probe"}`,
		}}},
		{provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "child-read", Name: "read_file", Arguments: `{"path":"reasonix.toml"}`,
		}}},
		{say("child done")},
		{say("parent done")},
	}}
}

// frame builds one wire frame for the two rules whose production shape this
// harness cannot reach. Both are marked as such where they are used: they show
// the fold implements the rule, GIVEN the state — not that a real run produces
// it.
func frame(kind string, tool *eventwire.Tool) eventwire.Event {
	return eventwire.Event{Kind: kind, Source: "executor", Tool: tool}
}

func toolFrame(id string, issuer event.ToolIssuer) *eventwire.Tool {
	return &eventwire.Tool{ID: id, Name: "bash", Issuer: string(issuer)}
}

// A line the user typed ends the group; nothing merges across it even when the
// producer never changes. MECHANISM GIVEN THE STATE, not a reachable shape:
// RunShell admits under its own guard, so a user-issued call lands between
// turns where the boundary already cuts. The rule holds if that changes.
func TestUserIssuedCallEndsTheGroupItLandsIn(t *testing.T) {
	frames := []eventwire.Event{
		{Kind: "turn_started"},
		frame("tool_dispatch", toolFrame("m1", event.IssuedByModel)),
		frame("tool_result", toolFrame("m1", event.IssuedByModel)),
		frame("tool_dispatch", toolFrame("u1", event.IssuedByUser)),
		frame("tool_result", toolFrame("u1", event.IssuedByUser)),
		frame("tool_dispatch", toolFrame("m2", event.IssuedByModel)),
		frame("tool_result", toolFrame("m2", event.IssuedByModel)),
		{Kind: "turn_done"},
	}
	_, groups := workgroup.Fold(frames, workgroup.V1())
	requireNoDuplicates(t, groups)
	if len(groups) != 2 {
		t.Fatalf("groups = %v, want the user's line to end the first and start a second",
			workgroup.Shape(groups))
	}
	for _, g := range groups {
		if slices.Contains(g.Members, "u1") {
			t.Fatalf("the user's own line counted as the assistant's work: %v", workgroup.Shape(groups))
		}
	}
}

// A turn nobody closed still has a scope. Closure is not completion: the group
// ends because the record does, and nothing about that says the turn finished.
//
// MECHANISM GIVEN THE STATE: 12 of 80 turns in the real corpus opened and never
// closed, so the shape is real; producing one on demand here is not.
func TestATurnWithNoTurnDoneStillCloses(t *testing.T) {
	frames := []eventwire.Event{
		{Kind: "turn_started"},
		frame("tool_dispatch", toolFrame("m1", event.IssuedByModel)),
		frame("tool_result", toolFrame("m1", event.IssuedByModel)),
	}
	_, groups := workgroup.Fold(frames, workgroup.V1())
	if len(groups) != 1 || groups[0].ClosedBy != "end of record" {
		t.Fatalf("groups = %v, want one closed by the record ending", workgroup.Shape(groups))
	}
	next := append(append([]eventwire.Event(nil), frames...), eventwire.Event{Kind: "turn_started"},
		frame("tool_dispatch", toolFrame("m2", event.IssuedByModel)),
		frame("tool_result", toolFrame("m2", event.IssuedByModel)))
	_, groups = workgroup.Fold(next, workgroup.V1())
	if len(groups) != 2 || groups[0].ClosedBy != "next turn" {
		t.Fatalf("groups = %v, want the next turn to close the unclosed one", workgroup.Shape(groups))
	}
}

// The sabotages. Each disables one part of the candidate and requires a fixture
// to notice. A rule nothing fails without is a rule the table was not testing.

// Splitting at the barrier rather than after the call it landed in is the shape
// a real corpus showed wrong. Atomizing calls first changed how the damage
// shows: not a call in two groups any more, but a group sealed while its member
// is still running, whose result then belongs to no group at all.
func TestSabotageBarrierSplitInPlaceSealsOverARunningCall(t *testing.T) {
	r := workgroup.V1()
	r.BarrierClosesAfterCall, r.BarrierSplitsInPlace = false, true
	_, groups := groupsOf(t, traceCase{input: "go", interactive: true, executor: [][]provider.Chunk{
		{callTodo("call-before", "before")},
		{say("now the barrier"), provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-write", Name: "write_file", Arguments: `{"path":"note.txt","content":"hello"}`,
		}}},
		{say("done")},
	}}, r)
	// Atomizing calls first already stops one from landing in two groups, so the
	// defect a misplaced boundary leaves is this: the group is sealed while its
	// member is still running, and the result lands outside every group.
	if workgroup.SealedOverAnOpenCall(groups) == "" {
		t.Fatalf("splitting at the barrier sealed no group over a running call (%v); "+
			"the invariant this table rests on is not being checked", workgroup.Shape(groups))
	}
}

// Counting the host's bookkeeping credits it to the assistant.
func TestSabotageCountingHostWorkAddsAStepNobodyTook(t *testing.T) {
	r := workgroup.V1()
	r.HostCounts = true
	_, groups := groupsOf(t, hostAdvanceCase(), r)
	var host bool
	for _, g := range groups {
		for _, id := range g.Members {
			host = host || strings.HasPrefix(id, "host-advance")
		}
	}
	if !host {
		t.Fatal("the host issued nothing this fixture could miscount, so the membership rule is untested here")
	}
}

// Excluding provider-executed work drops a call the assistant really made.
func TestSabotageExcludingProviderWorkLosesTheCall(t *testing.T) {
	r := workgroup.V1()
	r.ProviderCounts = false
	_, groups := groupsOf(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{provider.Chunk{Type: provider.ChunkProviderTool, Text: "two results",
			ToolCall: &provider.ToolCall{ID: "srv-1", Name: "web_search", Arguments: `{"q":"x"}`}},
			say("summarised")},
	}}, r)
	for _, g := range groups {
		if slices.Contains(g.Members, "srv-1") {
			t.Fatal("the provider-side call survived a rule that excludes it; membership is reading something else")
		}
	}
	if len(groups) != 0 {
		t.Fatalf("groups = %v, want none once the only call is excluded", workgroup.Shape(groups))
	}
}

// Ignoring the producer folds a planner's work and an executor's into one run.
func TestSabotageIgnoringTheProducerMergesTwoModels(t *testing.T) {
	r := workgroup.V1()
	r.SourceCuts = false
	_, groups := groupsOf(t, twoProducerCase(), r)
	if len(groups) != 1 {
		t.Fatalf("groups = %v, want the two producers wrongly merged into one — "+
			"if they stay apart, something other than the producer rule is splitting them",
			workgroup.Shape(groups))
	}
}

// Treating a line the user typed as transparent merges the assistant's work
// across it, which is what makes one continuous "Reasonix worked" block out of
// two separated by the user's own action.
func TestSabotageTransparentUserLineMergesAcrossIt(t *testing.T) {
	r := workgroup.V1()
	r.UserCuts = false
	frames := []eventwire.Event{
		{Kind: "turn_started"},
		frame("tool_dispatch", toolFrame("m1", event.IssuedByModel)),
		frame("tool_result", toolFrame("m1", event.IssuedByModel)),
		frame("tool_dispatch", toolFrame("u1", event.IssuedByUser)),
		frame("tool_result", toolFrame("u1", event.IssuedByUser)),
		frame("tool_dispatch", toolFrame("m2", event.IssuedByModel)),
		frame("tool_result", toolFrame("m2", event.IssuedByModel)),
		{Kind: "turn_done"},
	}
	_, groups := workgroup.Fold(frames, r)
	if len(groups) != 1 {
		t.Fatalf("groups = %v, want the two stretches wrongly merged across the user's line",
			workgroup.Shape(groups))
	}
}

// Cutting on the host's bookkeeping fragments a turn every time a list moves —
// which is the round-level shredding WorkGroup exists to avoid.
func TestSabotageCuttingOnHostBookkeepingFragmentsTheTurn(t *testing.T) {
	r := workgroup.V1()
	r.HostIsTransparent = false
	_, groups := groupsOf(t, hostAdvanceCase(), r)
	if len(groups) < 2 {
		t.Fatalf("groups = %v, want the turn fragmented once the host stops being transparent",
			workgroup.Shape(groups))
	}
}

// A round-ending tool is dispatched as a speculative partial and the run exits
// before its arguments finish: no full dispatch, no result. Real data held two
// — both conclude_no_changes — and every fixture above missed them, because the
// stub answers with complete tool calls and never streams one. Left open, such
// an id makes every later boundary seal a group over a running call.
func TestASpeculativePartialIsNotACall(t *testing.T) {
	reading := runTrace(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{callTodo("call-real", "real")},
		{provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{
			ID: "call-partial", Name: "conclude_no_changes",
		}}},
		{say("done")},
	}})
	assertParity(t, reading)
	// The shape has to be in the record, or this passes on a frame nobody sent.
	var partials, settled int
	for _, e := range reading.replay {
		if e.Tool == nil || e.Tool.ID != "call-partial" {
			continue
		}
		if e.Tool.Partial {
			partials++
		} else {
			settled++
		}
	}
	if partials == 0 || settled != 0 {
		t.Fatalf("the fixture recorded %d partial and %d settled frames for the "+
			"round-ending call; it has to be partial-only to test anything", partials, settled)
	}
	calls, groups := workgroup.Fold(reading.replay, workgroup.V1())
	requireNoDuplicates(t, groups)
	for _, c := range calls {
		if c.ID == "call-partial" {
			t.Fatalf("a partial-only dispatch materialized a call: %+v", c)
		}
	}
	if len(groups) != 1 || !slices.Contains(groups[0].Members, "call-real") {
		t.Fatalf("groups = %v, want the settled call alone", workgroup.Shape(groups))
	}
}

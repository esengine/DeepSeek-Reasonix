package control

import (
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/goaleval"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

// Can a real lifecycle produce a state where the criterion a task is held to
// and the declaration the gate reads are different commands? Nothing here
// assigns projectChecks, the checkpoint or the baseline: the two agents are
// built the way two boots build them, from two different declarations, and the
// restore is the controller's own resume path.
type driftSink struct {
	seen []event.VerificationContractDrift
}

func (s *driftSink) Emit(event.Event) {}

func (s *driftSink) RecordVerificationContractDrift(d event.VerificationContractDrift) {
	s.seen = append(s.seen, d)
}

// resumeUnderDeclarations starts a Goal in a process reading first, then
// resumes it in a process reading second, and returns what the second observed.
func resumeUnderDeclarations(t *testing.T, first, second string) (*agent.Agent, *driftSink) {
	t.Helper()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	declares := func(command string) agent.Options {
		if command == "" {
			return agent.Options{}
		}
		return agent.Options{ProjectChecks: []instruction.VerifyCheck{
			{Command: command, SourcePath: "REASONIX.md", Line: 5},
		}}
	}
	one := agent.New(&scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}},
		goalRegistry(), sessionstore.NewSession(""), declares(first), event.Discard)
	events := make(chan event.Event, 8)
	c1 := New(Options{
		Runner: one, Executor: one, SessionDir: dir, SessionPath: path, Label: "first",
		GoalEvaluator: &fakeGoalEvaluator{outcome: goaleval.OutcomeComplete, reason: "done"},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})
	c1.Submit("/goal do the work")
	waitGoalTurnDone(t, events)

	two := agent.New(&scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}},
		goalRegistry(), sessionstore.NewSession(""), declares(second), event.Discard)
	sink := &driftSink{}
	c2 := New(Options{Runner: two, Executor: two, SessionDir: dir, SessionPath: path,
		Label: "second", Sink: sink})
	c2.resume(sessionstore.NewSession(""), path, false)
	return two, sink
}

// The contract has to be an object with a lifetime before it can be an
// authority: frozen at Goal creation, carried by the checkpoint, and restored
// as the same generation and the same criteria.
func TestGoalVerificationContractSurvivesRestart(t *testing.T) {
	const checkA = "python3 check_a.py"
	_, sink := resumeUnderDeclarations(t, checkA, checkA)

	if len(sink.seen) != 1 {
		t.Fatalf("drift observations = %d, want one per resume carrying a contract", len(sink.seen))
	}
	got := sink.seen[0]
	wantA := evidence.VerificationIdentity(checkA)
	if got.Epoch != 1 {
		t.Errorf("restored epoch = %d, want the generation it was frozen at", got.Epoch)
	}
	if !slices.Equal(got.Frozen, []string{wantA}) || !slices.Equal(got.Current, []string{wantA}) {
		t.Errorf("frozen=%v current=%v, want both to name %q", got.Frozen, got.Current, wantA)
	}
	if got.Drift {
		t.Error("an unchanged declaration drifted from the contract frozen under it")
	}
}

// The reachable divergence, read through the contract rather than off a
// baseline field. What is asserted is that the two differ — not which governs.
func TestGoalVerificationContractDriftsWhenDeclarationChanges(t *testing.T) {
	const checkA, checkB = "python3 check_a.py", "python3 check_b.py"
	_, sink := resumeUnderDeclarations(t, checkA, checkB)

	if len(sink.seen) != 1 {
		t.Fatalf("drift observations = %d, want one", len(sink.seen))
	}
	got := sink.seen[0]
	if !got.Drift {
		t.Fatalf("frozen=%v current=%v reported no drift", got.Frozen, got.Current)
	}
	if !slices.Equal(got.Frozen, []string{evidence.VerificationIdentity(checkA)}) {
		t.Errorf("frozen = %v, want the criterion the Goal was accepted under", got.Frozen)
	}
	if !slices.Equal(got.Current, []string{evidence.VerificationIdentity(checkB)}) {
		t.Errorf("current = %v, want the declaration this process read", got.Current)
	}
}

// A Goal that began with nothing declared still froze a contract, so it is
// observed; the empty set is an acceptance, unlike a checkpoint that predates
// contracts and records none.
func TestGoalVerificationContractObservesAnEmptyDeclaration(t *testing.T) {
	_, sink := resumeUnderDeclarations(t, "", "python3 check_b.py")

	if len(sink.seen) != 1 {
		t.Fatalf("drift observations = %d, want one", len(sink.seen))
	}
	if got := sink.seen[0]; len(got.Frozen) != 0 || !got.Drift {
		t.Fatalf("frozen=%v drift=%v, want an empty contract that the new declaration drifts from",
			got.Frozen, got.Drift)
	}
}

func TestGoalResumeRestoresProjectCheckBaselineAcrossRestart(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	const checkA, checkB = "python3 check_a.py", "python3 check_b.py"
	declares := func(command string) agent.Options {
		return agent.Options{ProjectChecks: []instruction.VerifyCheck{
			{Command: command, SourcePath: "REASONIX.md", Line: 5},
		}}
	}

	// Process one reads the declaration naming A and runs one goal turn.
	first := agent.New(&scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}},
		goalRegistry(), sessionstore.NewSession(""), declares(checkA), event.Discard)
	events := make(chan event.Event, 8)
	c1 := New(Options{
		Runner: first, Executor: first, SessionDir: dir, SessionPath: path, Label: "first",
		GoalEvaluator: &fakeGoalEvaluator{outcome: goaleval.OutcomeComplete, reason: "done"},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})
	c1.Submit("/goal do the work")
	waitGoalTurnDone(t, events)

	wantA := evidence.VerificationIdentity(checkA)
	if got := first.DeliveryCheckpoint().BaselineChecks; !slices.Contains(got, wantA) {
		t.Fatalf("baseline after the first goal turn = %v, want the declared criterion %q captured", got, wantA)
	}

	// The declaration changes between the two processes. Process two reads it,
	// which is the only way its check list can differ from the first's.
	second := agent.New(&scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}},
		goalRegistry(), sessionstore.NewSession(""), declares(checkB), event.Discard)
	c2 := New(Options{Runner: second, Executor: second, SessionDir: dir, SessionPath: path, Label: "second"})
	c2.resume(sessionstore.NewSession(""), path, false)

	restored := second.DeliveryCheckpoint().BaselineChecks
	wantB := evidence.VerificationIdentity(checkB)
	switch {
	case slices.Contains(restored, wantA):
		t.Logf("REACHABLE: the resumed goal holds %q while the declaration now names %q", wantA, wantB)
	case len(restored) == 0:
		t.Fatalf("the resumed goal restored no baseline at all: the sidecar carried none, "+
			"so the divergent state has no producer here either (restored=%v)", restored)
	default:
		t.Fatalf("the resumed goal holds %v, not the criterion the task began under (%q): "+
			"the baseline was recaptured from the declaration this process read, "+
			"so baseline and current cannot differ", restored, wantA)
	}
}

package control

import (
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/contract"
	"reasonix/internal/runtime/goaleval"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/trustedstate"
)

// A goal accepts its contract in the process that starts it. The revision's
// record name rides the goal's persisted checkpoint, so a process that resumes
// the goal under a different declaration is held to the same revision, read
// back from Trusted Host State rather than re-derived from what it reads now.
func TestGoalResumeKeepsTheAcceptedContractRevision(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	store := trustedstate.Open(filepath.Join(dir, "trusted"), nil)
	const checkA, checkB = "python3 check_a.py", "python3 check_b.py"
	options := func(command string) agent.Options {
		return agent.Options{
			ProjectChecks: []instruction.VerifyCheck{{Command: command, SourcePath: "REASONIX.md", Line: 5}},
			EvidenceSeal:  &agent.EvidenceSeal{Store: store, Stream: "ws"},
		}
	}

	first := agent.New(&scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}},
		goalRegistry(), sessionstore.NewSession(""), options(checkA), event.Discard)
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

	accepted := first.DeliveryCheckpoint().Contract
	if accepted == "" {
		t.Fatal("the goal turn accepted no contract")
	}

	second := agent.New(&scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}},
		goalRegistry(), sessionstore.NewSession(""), options(checkB), event.Discard)
	c2 := New(Options{Runner: second, Executor: second, SessionDir: dir, SessionPath: path, Label: "second"})
	c2.resume(sessionstore.NewSession(""), path, false)

	if got := second.DeliveryCheckpoint().Contract; got != accepted {
		t.Fatalf("resumed goal names contract %q, want the revision the goal accepted (%q)", got, accepted)
	}
	c, err := contract.Load(trustedstate.Open(filepath.Join(dir, "trusted"), nil), accepted)
	if err != nil {
		t.Fatalf("a fresh process cannot read the accepted revision: %v", err)
	}
	want := contract.Criterion{
		ID: "command@" + evidence.VerificationIdentity(checkA), Source: contract.SourceProjectCheck, Required: true,
		Verifier: contract.Verifier{Kind: contract.VerifierCommand, Identity: evidence.VerificationIdentity(checkA)},
	}
	if c.Revision != 1 || !slices.EqualFunc(c.Criteria, []contract.Criterion{want}, contract.Criterion.Equal) {
		t.Fatalf("revision = %+v, want revision 1 holding only the check the goal began under", c)
	}
}

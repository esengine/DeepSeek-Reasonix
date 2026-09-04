package control

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/store"
	"reasonix/internal/testenv"
)

func barrierController(t *testing.T) *Controller {
	t.Helper()
	c := &Controller{controllerDeps: controllerDeps{
		approval: newApprovalManager(permission.Policy{}, "", 0),
		sink:     event.Discard,
	}}
	c.sessionPath = filepath.Join(testenv.TempDir(t), "s.jsonl")
	return c
}

func askQuestion() []event.AskQuestion {
	return []event.AskQuestion{{
		Header: "Boundary", Prompt: "Which side?",
		Options: []event.AskOption{{Label: "Below"}, {Label: "Above"}},
	}}
}

// TestBarrierIsDurableBeforeTheQuestionIsAnswerable pins the ordering the whole
// record exists for. A crash between registering an ask and writing it down
// leaves a person having been asked something the host has no record of owing,
// which is the state the falsifier found.
func TestBarrierIsDurableBeforeTheQuestionIsAnswerable(t *testing.T) {
	c := barrierController(t)
	go func() { _, _ = c.Ask(t.Context(), askQuestion()) }()

	deadline := time.Now().Add(5 * time.Second)
	for len(c.Decisions()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the ask never became observable")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if open := loadOpenBarriers(c.SessionPath()); len(open) != 1 {
		t.Fatalf("open barriers on disk = %d, want the question to be recorded before it is answerable", len(open))
	}
	// A live owner is waiting on it, so it is not an interrupted obligation.
	if got := c.InterruptedAdjudications(); len(got) != 0 {
		t.Fatalf("reported %d interrupted barriers while one is being waited on", len(got))
	}
}

// An answered question ends its barrier: the record is provenance for a wait
// that happened, not a claim that one is still owed.
func TestAnsweringClosesTheBarrier(t *testing.T) {
	c := barrierController(t)
	done := make(chan struct{})
	go func() { _, _ = c.Ask(t.Context(), askQuestion()); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	var id string
	for id == "" {
		if time.Now().After(deadline) {
			t.Fatal("the ask never became observable")
		}
		if d := c.Decisions(); len(d) > 0 {
			id = d[0].ID
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.AnswerQuestion(id, []event.AskAnswer{{QuestionID: "", Selected: []string{"Below"}}})
	<-done
	if open := loadOpenBarriers(c.SessionPath()); len(open) != 0 {
		t.Fatalf("barrier still open after the answer: %+v", open)
	}
}

// TestInterruptedBarrierIsDerivedAndStable is the shape a restart inherits: a
// record this process did not open, still open, with nobody waiting. It is
// derived rather than written, because the process that died did not get to
// write anything — and it must read the same on every later load, not only the
// first.
func TestInterruptedBarrierIsDerivedAndStable(t *testing.T) {
	c := barrierController(t)
	if err := c.openBarrier("7", string(DecisionAsk), "Which side?"); err != nil {
		t.Fatal(err)
	}
	next := &Controller{controllerDeps: controllerDeps{
		approval: newApprovalManager(permission.Policy{}, "", 0),
		sink:     event.Discard,
	}}
	next.sessionPath = c.SessionPath()
	for range 2 {
		got := next.InterruptedAdjudications()
		if len(got) != 1 || got[0].ID != "7" || got[0].Kind != string(DecisionAsk) {
			t.Fatalf("interrupted barriers = %+v, want the one nobody is waiting on", got)
		}
		if len(next.Decisions()) != 0 {
			t.Fatal("an interrupted barrier was offered as an answerable decision")
		}
	}
	if store.SessionAdjudication(c.SessionPath()) == "" {
		t.Fatal("no adjudication log path for the session")
	}
}

// A settled barrier projects nothing: the record is provenance for a wait that
// ended, and a model told about it would be told about work already closed.
func TestSettledBarriersProjectNothing(t *testing.T) {
	for _, status := range []string{barrierResolved, barrierCancelled} {
		c := barrierController(t)
		if err := c.openBarrier("3", string(DecisionAsk), "Which side?"); err != nil {
			t.Fatal(err)
		}
		c.closeBarrier("3", status)
		next := &Controller{controllerDeps: controllerDeps{
			approval: newApprovalManager(permission.Policy{}, "", 0), sink: event.Discard,
		}}
		next.sessionPath = c.SessionPath()
		if got := next.InterruptedAdjudications(); len(got) != 0 {
			t.Fatalf("%s barrier still reported as interrupted: %+v", status, got)
		}
		if got := next.RequestContext(); len(got) != 0 {
			t.Fatalf("%s barrier still projected to the model: %q", status, got)
		}
	}
}

// The interruption rides the request and nothing else. Composing a turn must
// not carry it: composed text becomes the canonical user message, so a block
// added there would be history a later fold could freeze — the mistake this
// whole line of work has been undoing.
func TestInterruptionNeverEntersTheComposedTurn(t *testing.T) {
	c := barrierController(t)
	if err := c.openBarrier("11", string(DecisionAsk), "Delete X?"); err != nil {
		t.Fatal(err)
	}
	next := &Controller{controllerDeps: controllerDeps{
		approval: newApprovalManager(permission.Policy{}, "", 0), sink: event.Discard,
	}}
	next.sessionPath = c.SessionPath()
	if got := next.RequestContext(); len(got) != 1 {
		t.Fatalf("request context = %v, want the interruption", got)
	}
	if composed := next.Compose("go ahead, delete it"); strings.Contains(composed, "interrupted-adjudication") {
		t.Fatalf("the composed turn carries the interruption, which persists it:\n%s", composed)
	}
}

// What the model is told must not read as answerable, and must not hand back an
// identity to answer with: an interrupted barrier has no owner to receive one.
func TestInterruptionContextOffersNoHandle(t *testing.T) {
	c := barrierController(t)
	if err := c.openBarrier("42", string(DecisionAsk), "Delete X?"); err != nil {
		t.Fatal(err)
	}
	next := &Controller{controllerDeps: controllerDeps{
		approval: newApprovalManager(permission.Policy{}, "", 0), sink: event.Discard,
	}}
	next.sessionPath = c.SessionPath()
	block := next.RequestContext()[0]
	if !strings.Contains(block, "Delete X?") {
		t.Fatalf("block does not say what was being asked:\n%s", block)
	}
	if !strings.Contains(block, "cannot be resumed") {
		t.Fatalf("block does not say the run is gone:\n%s", block)
	}
	if strings.Contains(block, "42") {
		t.Fatalf("block hands back an identity nobody can answer to:\n%s", block)
	}
	if len(next.Decisions()) != 0 {
		t.Fatal("an interrupted barrier reached the answerable decision surface")
	}
}

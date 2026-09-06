package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
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
	if h := AdjudicationHistory(c.SessionPath()); len(h) != 1 || !h[0].Open() {
		t.Fatalf("journal = %+v, want the question recorded before it is answerable", h)
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
	h := AdjudicationHistory(c.SessionPath())
	if len(h) != 1 || h[0].Disposition != barrierResolved {
		t.Fatalf("journal = %+v, want the barrier recorded as resolved", h)
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

// The fold's rules, stated as the cases a crash-written journal can hold. Each
// one is a way a naive reader would invent history the host never recorded.
func TestAdjudicationHistoryFoldRules(t *testing.T) {
	dir := testenv.TempDir(t)
	write := func(name string, recs ...barrierRecord) string {
		path := filepath.Join(dir, name+".jsonl")
		for _, rec := range recs {
			if err := appendBarrier(path, rec); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	open := func(id string) barrierRecord {
		return barrierRecord{Barrier: id, Kind: "ask", Status: barrierOpen, Summary: "Which side?"}
	}
	end := func(id, status string) barrierRecord {
		return barrierRecord{Barrier: id, Status: status}
	}

	cases := []struct {
		name  string
		recs  []barrierRecord
		want  []string // disposition per entry, "" for open
		count int
	}{
		{"open only stays open", []barrierRecord{open("1")}, []string{""}, 1},
		{"resolved is terminal", []barrierRecord{open("1"), end("1", barrierResolved)}, []string{barrierResolved}, 1},
		{"cancelled is terminal", []barrierRecord{open("1"), end("1", barrierCancelled)}, []string{barrierCancelled}, 1},
		{"the first terminal wins", []barrierRecord{open("1"), end("1", barrierResolved), end("1", barrierCancelled)}, []string{barrierResolved}, 1},
		{"a terminal alone invents nothing", []barrierRecord{end("9", barrierResolved)}, nil, 0},
		{"a repeated open does not restart one", []barrierRecord{open("1"), open("1")}, []string{""}, 1},
		{"a reopen after a terminal is refused", []barrierRecord{open("1"), end("1", barrierResolved), open("1")}, []string{barrierResolved}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AdjudicationHistory(write(tc.name, tc.recs...))
			if len(got) != tc.count {
				t.Fatalf("entries = %+v, want %d", got, tc.count)
			}
			for i, want := range tc.want {
				if got[i].Disposition != want {
					t.Fatalf("entry %d disposition = %q, want %q", i, got[i].Disposition, want)
				}
			}
		})
	}
}

// A torn tail is what a crash leaves; the fold reads past it rather than
// refusing the whole journal, and what came before still counts.
func TestAdjudicationHistorySurvivesATornTail(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "torn.jsonl")
	if err := appendBarrier(session, barrierRecord{Barrier: "1", Kind: "ask", Status: barrierOpen}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(store.SessionAdjudication(session), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"barrier":"2","stat`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if h := AdjudicationHistory(session); len(h) != 1 || h[0].ID != "1" || !h[0].Open() {
		t.Fatalf("journal = %+v, want the intact record", h)
	}
}

// TestSupersessionIsScopedToTheTurnThatInheritedIt is the whole point of the
// edge. A turn that takes an interruption over carries it for as long as that
// turn is running — including a retry after a crash, because the successor is
// read off the in-flight marker on disk rather than remembered in this process
// — and no later turn carries it at all.
func TestSupersessionIsScopedToTheTurnThatInheritedIt(t *testing.T) {
	c := barrierController(t)
	if err := c.openBarrier("5", string(DecisionAsk), "Delete X?"); err != nil {
		t.Fatal(err)
	}
	if len(c.RequestContext()) != 1 {
		t.Fatal("an unowned barrier is not being projected")
	}

	marker, err := agent.BeginSessionInFlightTurn(c.SessionPath(), 0, true)
	if err != nil {
		t.Fatal(err)
	}
	c.inheritInterruptions(marker.ID)

	// The turn that took it over still sees it, and it is no longer an
	// obligation nobody owns.
	if got := c.RequestContext(); len(got) != 1 {
		t.Fatalf("the successor turn was not given the interruption: %v", got)
	}
	if got := c.InterruptedAdjudications(); len(got) != 0 {
		t.Fatalf("an inherited barrier is still reported as unowned: %+v", got)
	}

	// A fresh process reading the same disk answers the same way while that
	// turn has not committed: the retry after a crash gets the same context.
	next := &Controller{controllerDeps: controllerDeps{
		approval: newApprovalManager(permission.Policy{}, "", 0), sink: event.Discard,
	}}
	next.sessionPath = c.SessionPath()
	if got := next.RequestContext(); len(got) != 1 {
		t.Fatalf("a restart mid-turn lost the inherited interruption: %v", got)
	}

	// Once that turn commits, nothing later carries it.
	if _, err := agent.ClearSessionInFlightTurnIfMatch(c.SessionPath(), marker); err != nil {
		t.Fatal(err)
	}
	if got := next.RequestContext(); len(got) != 0 {
		t.Fatalf("the interruption followed a later turn: %v", got)
	}
	entries := AdjudicationHistory(c.SessionPath())
	if len(entries) != 1 || entries[0].Disposition != barrierSuperseded || entries[0].SupersededBy != marker.ID {
		t.Fatalf("journal = %+v, want the barrier superseded by the turn that took it", entries)
	}
}

// TestLiveApprovalIsNotAnInterruptedBarrier covers the half of the live set the
// ask path never reaches. A tool approval and a question are both prompts this
// process is waiting on, and only the approvals map says so for the first: a
// live set built from the asks alone reports every open tool approval as an
// obligation nobody is waiting on, which is the state a crash leaves behind.
func TestLiveApprovalIsNotAnInterruptedBarrier(t *testing.T) {
	c := barrierController(t)
	id, _ := c.approval.registerDecisionKind("bash", "ls", "reads the tree", true, false, "", nil)
	if err := c.openBarrier(id, string(DecisionToolApproval), "bash ls"); err != nil {
		t.Fatalf("openBarrier: %v", err)
	}

	if got := c.InterruptedAdjudications(); len(got) != 0 {
		t.Fatalf("reported %d interrupted barriers while an approval is being waited on: %+v", len(got), got)
	}
	active, history := c.Adjudications()
	if len(active) != 0 || len(history) != 0 {
		t.Fatalf("active=%d history=%d, want a live approval in neither", len(active), len(history))
	}

	// The same record with nobody waiting on it is the interruption. Resolving
	// the approval drops it from the live set without closing the barrier,
	// which is exactly the shape a killed process leaves.
	c.approval.resolve(id)
	if got := c.InterruptedAdjudications(); len(got) != 1 || got[0].ID != id {
		t.Fatalf("interrupted = %+v, want the abandoned approval %s", got, id)
	}
}

// TestAskRefusesWhenTheBarrierCannotBeRecorded pins the ordering's failure
// side. The record is what makes a question answerable, so a question that
// could not be written down must not be asked: proceeding would put a prompt
// in front of a person that no crash-recovery pass can ever find.
func TestAskRefusesWhenTheBarrierCannotBeRecorded(t *testing.T) {
	c := barrierController(t)
	// A directory where the journal's file belongs: the append cannot open it,
	// and nothing else about the session changes.
	if err := os.MkdirAll(store.SessionAdjudication(c.SessionPath()), 0o755); err != nil {
		t.Fatalf("stage an unwritable journal: %v", err)
	}

	ans, err := c.Ask(t.Context(), askQuestion())
	if err == nil {
		t.Fatalf("Ask returned %+v with no error while its barrier could not be recorded", ans)
	}
	if d := c.Decisions(); len(d) != 0 {
		t.Fatalf("decisions = %+v, want the question never offered", d)
	}
}

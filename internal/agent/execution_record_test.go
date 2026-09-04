package agent

import (
	"context"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"reasonix/internal/agentgraph"
	"reasonix/internal/event"
	"reasonix/internal/execjournal"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// openingProbeSink reads the journal at the moment the graph first shows the
// fan-out's workers. That instant is the contract: the delta is the earliest
// point anything outside this call can learn the items exist, so whatever the
// journal holds then is what a crash one instruction later would leave behind.
type openingProbeSink struct {
	mu          sync.Mutex
	sessionPath string
	recorded    []string
	seen        bool
}

func (s *openingProbeSink) Emit(e event.Event) {
	if e.Kind != event.GraphDelta || e.Graph == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen || !hasWorker(e.Graph.Nodes) {
		return
	}
	s.seen = true
	for _, entry := range execjournal.History(s.sessionPath) {
		s.recorded = append(s.recorded, entry.ID)
	}
}

func (s *openingProbeSink) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.recorded...)
}

// runningProbeSink reads the journal at the moment the graph first shows one
// item running. A worker's running delta carries an outcome only, so the group's
// own node — declared running when the fan-out opened — is excluded by its kind.
type runningProbeSink struct {
	mu          sync.Mutex
	sessionPath string
	startedAt   map[string]bool
}

func (s *runningProbeSink) Emit(e event.Event) {
	if e.Kind != event.GraphDelta || e.Graph == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range e.Graph.Nodes {
		if n.State != agentgraph.StateRunning || n.Kind == agentgraph.KindGroup {
			continue
		}
		if s.startedAt == nil {
			s.startedAt = map[string]bool{}
		}
		s.startedAt[n.ID] = startedInJournal(s.sessionPath, n.ID)
	}
}

func (s *runningProbeSink) observed() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	maps.Copy(out, s.startedAt)
	return out
}

func startedInJournal(sessionPath, id string) bool {
	for _, e := range execjournal.History(sessionPath) {
		if e.ID == id {
			return e.Started()
		}
	}
	return false
}

func hasWorker(nodes []agentgraph.Node) bool {
	for _, n := range nodes {
		if n.Kind == agentgraph.KindWorker {
			return true
		}
	}
	return false
}

// fanOutJournalFixture assembles a real fleet over a scripted provider, bound to
// a session path the sub-agent store can resolve, and returns that path.
func fanOutJournalFixture(t *testing.T, prov *fleetScriptedFailureProvider) (*FleetTool, context.Context, string, *openingProbeSink) {
	t.Helper()
	root := testenv.TempDir(t)
	sessions := testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(filepath.Join(sessions, "subagents")), root, "base", "high").
		WithScheduler(NewSubagentScheduler(4, 4))
	sessionPath := filepath.Join(sessions, "probe.jsonl")
	sink := &openingProbeSink{sessionPath: sessionPath}
	ctx := withCallContext(context.Background(), "fleet-call", sink, nil, false)
	ctx = WithParentSession(ctx, "probe")
	ctx = WithTurnIdentity(ctx, "turn-1")
	return NewFleetTool(task), ctx, sessionPath, sink
}

// TestFanOutIsDurableBeforeTheGraphShowsIt is the ordering the journal exists
// for. A turn reaches the transcript only when it ends, so if the record were
// written after the dispatch became observable, a crash in between would leave
// a fan-out nothing on disk had ever heard of — which is the state this file
// was added to remove.
func TestFanOutIsDurableBeforeTheGraphShowsIt(t *testing.T) {
	fleet, ctx, _, sink := fanOutJournalFixture(t, &fleetScriptedFailureProvider{})

	if _, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"one","prompt":"one","read_only":true},
		{"id":"two","prompt":"two","read_only":true}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	recorded := sink.ids()
	if len(recorded) != 2 {
		t.Fatalf("journal held %v when the graph first showed the workers, want both items", recorded)
	}
	for _, want := range []string{"fleet-call/fleet-1", "fleet-call/fleet-2"} {
		if !slices.Contains(recorded, want) {
			t.Errorf("journal was missing %q at the opening delta; it held %v", want, recorded)
		}
	}
}

// TestFanOutRecordsIdentityGrantAndTurn holds the opening to what it claims to
// carry. A record that cannot name the turn it belonged to, or the authority
// the item held, does not let a later process say anything worth saying.
func TestFanOutRecordsIdentityGrantAndTurn(t *testing.T) {
	fleet, ctx, sessionPath, _ := fanOutJournalFixture(t, &fleetScriptedFailureProvider{})

	if _, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"reader","prompt":"reader","description":"read something","read_only":true},
		{"id":"writer","prompt":"writer","description":"write something","write_paths":["api"]}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	byID := map[string]execjournal.Entry{}
	for _, e := range execjournal.History(sessionPath) {
		byID[e.ID] = e
	}
	reader, writer := byID["fleet-call/fleet-1"], byID["fleet-call/fleet-2"]
	if reader.Grant != string(agentgraph.GrantRead) {
		t.Errorf("read-only item grant = %q, want %q", reader.Grant, agentgraph.GrantRead)
	}
	if writer.Grant != string(agentgraph.GrantWrite) {
		t.Errorf("path-bound item grant = %q, want %q", writer.Grant, agentgraph.GrantWrite)
	}
	for id, e := range map[string]execjournal.Entry{"reader": reader, "writer": writer} {
		if e.Turn != "turn-1" {
			t.Errorf("%s turn = %q, want the parent turn identity", id, e.Turn)
		}
		if e.Group != "fleet-call" {
			t.Errorf("%s group = %q, want the dispatching call", id, e.Group)
		}
	}
}

// TestCompletedFanOutLeavesNothingInterrupted is the negative control. Every
// item settled, so a later process must inherit no interruption at all — a
// journal that reported one would be worse than none: it would describe work
// that finished as work that was cut.
func TestCompletedFanOutLeavesNothingInterrupted(t *testing.T) {
	fleet, ctx, sessionPath, _ := fanOutJournalFixture(t, &fleetScriptedFailureProvider{})

	if _, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"one","prompt":"one","read_only":true},
		{"id":"two","prompt":"two","read_only":true}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	execjournal.Disown(sessionPath)
	if got := execjournal.Interrupted(sessionPath); len(got) != 0 {
		t.Fatalf("the next process inherited %+v; a completed fan-out interrupts nothing", got)
	}
}

// TestSkippedItemIsSettledByTheGroup: a branch cut upstream never runs and never
// reports, so only the group's own end can release it. Left open it would reach
// the next process as work that was interrupted, which nothing ever started.
func TestSkippedItemIsSettledByTheGroup(t *testing.T) {
	fleet, ctx, sessionPath, _ := fanOutJournalFixture(t, &fleetScriptedFailureProvider{})

	if _, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"research","prompt":"FAIL research","read_only":true},
		{"id":"implement","prompt":"implement","depends_on":["research"],"write_paths":["api"]}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	execjournal.Disown(sessionPath)
	if got := execjournal.Interrupted(sessionPath); len(got) != 0 {
		t.Fatalf("the next process inherited %+v; a skipped branch was never running", got)
	}
}

// TestFanOutIsDurableBeforeRunningIsObservable is the ordering the STARTED
// record exists for. The slot grant is the point the child becomes able to act,
// so if the record were written after that became observable, a crash in
// between would leave an execution that ran with nothing saying it had begun —
// indistinguishable from one that never reached a slot.
func TestFanOutIsDurableBeforeRunningIsObservable(t *testing.T) {
	root := testenv.TempDir(t)
	sessions := testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	sessionPath := filepath.Join(sessions, "probe.jsonl")
	sink := &runningProbeSink{sessionPath: sessionPath}
	task := NewTaskTool(&fleetScriptedFailureProvider{}, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(filepath.Join(sessions, "subagents")), root, "base", "high").
		WithScheduler(NewSubagentScheduler(4, 4))
	ctx := withCallContext(context.Background(), "fleet-call", sink, nil, false)
	ctx = WithTurnIdentity(WithParentSession(ctx, "probe"), "turn-1")

	if _, err := NewFleetTool(task).Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"one","prompt":"one","read_only":true},
		{"id":"two","prompt":"two","read_only":true}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	observed := sink.observed()
	if len(observed) == 0 {
		t.Fatal("no worker was ever observed running; the arm measured nothing")
	}
	for id, started := range observed {
		if !started {
			t.Errorf("%s was observed running while the journal had no start for it", id)
		}
	}
}

// TestBlockedItemIsNeverStarted is the negative control that proves the hook is
// on the slot grant rather than on the item being created or becoming runnable.
// A branch its dependency cut is opened and released without ever reaching a
// slot; if it carried a start, the two interruptions would collapse again.
func TestBlockedItemIsNeverStarted(t *testing.T) {
	fleet, ctx, sessionPath, _ := fanOutJournalFixture(t, &fleetScriptedFailureProvider{})

	if _, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"research","prompt":"FAIL research","read_only":true},
		{"id":"implement","prompt":"implement","depends_on":["research"],"write_paths":["api"]}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	byID := map[string]execjournal.Entry{}
	for _, e := range execjournal.History(sessionPath) {
		byID[e.ID] = e
	}
	if blocked := byID["fleet-call/fleet-2"]; blocked.Started() {
		t.Fatalf("the dependency-blocked item recorded a start at %v; it never reached a slot", blocked.StartedAt)
	}
	if ran := byID["fleet-call/fleet-1"]; !ran.Started() {
		t.Fatal("the item that ran recorded no start")
	}
}

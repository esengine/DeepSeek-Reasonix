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
	upstream    []string
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
		s.upstream = append(s.upstream, entry.DependsOn...)
	}
}

func (s *openingProbeSink) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.recorded...)
}

func (s *openingProbeSink) upstreamIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.upstream...)
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

// queuedProbeSink reads the journal the moment the graph first shows an item
// refused admission. A wait delta carries the cause and nothing else, so it is
// the earliest point anything outside the scheduler can learn the item waited.
type queuedProbeSink struct {
	mu          sync.Mutex
	sessionPath string
	recorded    map[string]string
}

func (s *queuedProbeSink) Emit(e event.Event) {
	if e.Kind != event.GraphDelta || e.Graph == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range e.Graph.Nodes {
		if n.Wait == "" {
			continue
		}
		if s.recorded == nil {
			s.recorded = map[string]string{}
		}
		s.recorded[n.ID] = causeInJournal(s.sessionPath, n.ID)
	}
}

func (s *queuedProbeSink) observed() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	maps.Copy(out, s.recorded)
	return out
}

func causeInJournal(sessionPath, id string) string {
	for _, e := range execjournal.History(sessionPath) {
		if e.ID == id && e.Queued() {
			return e.Cause
		}
	}
	return ""
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

// TestOrderingTopologyIsDurableWithTheOpening: an item that never starts is
// explained by what it was waiting for, and that has to survive the process
// that knew it. The plan is already decided when the fan-out opens, so the
// topology rides the opening rather than waiting for an edge nobody records.
func TestOrderingTopologyIsDurableWithTheOpening(t *testing.T) {
	fleet, ctx, sessionPath, sink := fanOutJournalFixture(t, &fleetScriptedFailureProvider{})

	if _, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"research","prompt":"FAIL research","read_only":true},
		{"id":"implement","prompt":"implement","depends_on":["research"],"write_paths":["api"]}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	if got := sink.upstreamIDs(); !slices.Contains(got, "fleet-call/fleet-1") {
		t.Errorf("journal held upstream %v when the graph first showed the workers, want the declared dependency", got)
	}
	byID := map[string]execjournal.Entry{}
	for _, e := range execjournal.History(sessionPath) {
		byID[e.ID] = e
	}
	blocked := byID["fleet-call/fleet-2"]
	if !slices.Equal(blocked.DependsOn, []string{"fleet-call/fleet-1"}) {
		t.Errorf("blocked item dependsOn = %v, want the item it was ordered behind", blocked.DependsOn)
	}
	if up := byID["fleet-call/fleet-1"].DependsOn; len(up) != 0 {
		t.Errorf("the first item dependsOn = %v, want none", up)
	}
}

// TestTopologyComesFromTheSameDeltaAsTheGraph is what keeps the two from
// drifting. A dependency the graph draws and the journal does not is a picture
// a restart cannot check; deriving both from one delta makes that unreachable
// rather than merely unlikely.
func TestTopologyComesFromTheSameDeltaAsTheGraph(t *testing.T) {
	delta := agentgraph.Delta{
		Nodes: []agentgraph.Node{
			{ID: "g", Kind: agentgraph.KindGroup, State: agentgraph.StateRunning},
			{ID: "g/a", Kind: agentgraph.KindWorker},
			{ID: "g/b", Kind: agentgraph.KindWorker},
			{ID: "src", Kind: agentgraph.KindExternal},
		},
		Edges: []agentgraph.Edge{
			{From: "g", To: "g/a", Kind: agentgraph.Spawn},
			{From: "g", To: "g/b", Kind: agentgraph.Spawn},
			{From: "g/a", To: "g/b", Kind: agentgraph.Depends},
			{From: "src", To: "g/a", Kind: agentgraph.Adopt},
			{From: "g/a", To: "g/b", Kind: agentgraph.Context},
		},
	}
	byID := map[string]execjournal.Opening{}
	for _, o := range fanOutOpenings(delta) {
		byID[o.ID] = o
	}
	if len(byID) != 2 {
		t.Fatalf("openings = %d, want one per worker; a group and an external node run nothing", len(byID))
	}
	if !slices.Equal(byID["g/b"].DependsOn, []string{"g/a"}) {
		t.Errorf("g/b dependsOn = %v, want the ordering edge only", byID["g/b"].DependsOn)
	}
	// Adopt names reuse and Context names delivery; neither holds an item back,
	// and reading them as ordering would explain a start that nothing blocked.
	if up := byID["g/a"].DependsOn; len(up) != 0 {
		t.Errorf("g/a dependsOn = %v, want none: an adopt edge is not an ordering edge", up)
	}
}

// TestRefusalIsDurableBeforeWaitingIsObservable is the third seam. The refusal
// exists only at the moment the scheduler makes it — by the time the item runs,
// the constraint is gone — so a record written after the wait becomes visible
// would leave a graph that says queued and a journal that never heard of it.
func TestRefusalIsDurableBeforeWaitingIsObservable(t *testing.T) {
	root := testenv.TempDir(t)
	sessions := testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	sessionPath := filepath.Join(sessions, "probe.jsonl")
	sink := &queuedProbeSink{sessionPath: sessionPath}
	task := NewTaskTool(&fleetScriptedFailureProvider{}, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(filepath.Join(sessions, "subagents")), root, "base", "high").
		WithScheduler(NewSubagentScheduler(1, 1))
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
		t.Fatal("no item was ever observed waiting; a ceiling of one should refuse the second")
	}
	for id, cause := range observed {
		if cause == "" {
			t.Errorf("%s was observed waiting while the journal had no refusal for it", id)
		}
	}
}

// TestAdmittedItemIsNeverQueued is the negative control for the seam. An item
// that is admitted immediately never waited, and a queue record for it would
// claim a refusal the scheduler never made.
func TestAdmittedItemIsNeverQueued(t *testing.T) {
	fleet, ctx, sessionPath, _ := fanOutJournalFixture(t, &fleetScriptedFailureProvider{})

	if _, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"one","prompt":"one","read_only":true},
		{"id":"two","prompt":"two","read_only":true}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	for _, e := range execjournal.History(sessionPath) {
		if e.Queued() {
			t.Errorf("%s recorded a refusal (%s); a ceiling of four refuses nothing here", e.ID, e.Cause)
		}
		if !e.Started() {
			t.Errorf("%s never started; both items should have been admitted", e.ID)
		}
	}
}

// TestBlockedItemNeverReachesTheScheduler is the row the truth table calls
// blocked-by-dependency, driven through a real fan-out. It is what separates a
// queued entry from an unqueued one: reaching the scheduler at all is proof the
// dependency gate was crossed, and this item never crossed it.
func TestBlockedItemNeverReachesTheScheduler(t *testing.T) {
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
	if blocked := byID["fleet-call/fleet-2"]; blocked.Queued() {
		t.Fatalf("the dependency-blocked item recorded a refusal (%s); it never reached the scheduler", blocked.Cause)
	}
}

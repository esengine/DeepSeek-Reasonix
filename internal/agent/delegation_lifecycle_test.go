package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/event"
	"reasonix/internal/execjournal"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// A lone delegation reaches the same runner a fan-out item does and, until this
// file, none of the same records: it drew itself running before asking for a
// slot, and left nothing on disk for a restart to read.

// loneFixture is one delegation's world: a persisted parent, a scheduler with
// the ceiling the test wants, and a sink that watches what is drawn.
func loneFixture(t *testing.T, prov *loneProvider, total int) (*TaskTool, context.Context, string, *loneSink) {
	t.Helper()
	root := testenv.TempDir(t)
	sessions := testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(filepath.Join(sessions, "subagents")), root, "base", "high").
		WithScheduler(NewSubagentScheduler(total, total))
	sessionPath := filepath.Join(sessions, "probe.jsonl")
	sink := &loneSink{sessionPath: sessionPath, node: loneNodeID}
	ctx := withCallContext(context.Background(), "lone-call", sink, nil, false)
	ctx = WithTurnIdentity(WithParentSession(ctx, "probe"), "turn-1")
	return task, ctx, sessionPath, sink
}

const loneNodeID = "lone-call/sub-1"

// loneSink records every state the delegation was drawn in, and reads the
// journal at each one: a picture the record has not caught up with is a claim a
// crash one instruction later would leave nothing behind for.
type loneSink struct {
	mu          sync.Mutex
	sessionPath string
	// node is the id family this sink watches, matched by prefix so a fan-out's
	// items are covered by the one the group drew them under.
	node     string
	states   []agentgraph.NodeState
	durable  []string
	statuses []string
}

func (s *loneSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Kind == event.ToolProgress && e.Tool.Name == event.SubagentProgressStatusName {
		s.statuses = append(s.statuses, e.Tool.Output)
		return
	}
	if e.Kind != event.GraphDelta || e.Graph == nil {
		return
	}
	for _, n := range e.Graph.Nodes {
		if !strings.HasPrefix(n.ID, s.node) || n.State == "" {
			continue
		}
		s.states = append(s.states, n.State)
		s.durable = append(s.durable, string(n.State)+"="+journalStanding(s.sessionPath, n.ID))
	}
}

func (s *loneSink) drawn() []agentgraph.NodeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agentgraph.NodeState(nil), s.states...)
}

func (s *loneSink) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.durable...)
}

func (s *loneSink) told() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.statuses...)
}

func (s *loneSink) sawState(want agentgraph.NodeState) bool {
	return slices.Contains(s.drawn(), want)
}

// journalStanding is what the record says about this execution right now.
func journalStanding(sessionPath, id string) string {
	for _, e := range execjournal.History(sessionPath) {
		if e.ID != id {
			continue
		}
		out := "opened"
		if e.Queued() {
			out += "+queued:" + e.Cause
		}
		if e.Started() {
			out += "+started"
		}
		if !e.SettledAt.IsZero() {
			out += "+settled"
		}
		return out
	}
	return "absent"
}

// loneProvider answers a child, blocking until the test lets it finish.
type loneProvider struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newLoneProvider() *loneProvider {
	return &loneProvider{entered: make(chan struct{}), release: make(chan struct{})}
}

func (p *loneProvider) Name() string { return "lone" }

func (p *loneProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
	case <-ctx.Done():
	}
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	close(ch)
	return ch, nil
}

func waitUntil(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestLoneDelegationIsRecordedBeforeItIsDrawn is the ordering the whole record
// exists for. The first delta is the earliest anything outside this call can
// learn the delegation exists, so what the journal holds then is what a crash
// one instruction later would leave behind.
func TestLoneDelegationIsRecordedBeforeItIsDrawn(t *testing.T) {
	prov := newLoneProvider()
	close(prov.release)
	task, ctx, sessionPath, sink := loneFixture(t, prov, 4)

	if _, err := task.Execute(ctx, json.RawMessage(`{"prompt":"work","description":"lone"}`)); err != nil {
		t.Fatalf("task: %v", err)
	}
	if got := sink.recorded(); len(got) == 0 || got[0] != "pending=opened" {
		t.Fatalf("first drawn state and the record behind it = %v, want the node drawn pending with its opening on disk", got)
	}
	entry := journalEntry(t, sessionPath, loneNodeID)
	if entry.Grant != string(agentgraph.GrantWrite) || entry.Turn != "turn-1" || entry.Group != "lone-call" {
		t.Errorf("opening = grant %q turn %q group %q, want the call's own authority and identity",
			entry.Grant, entry.Turn, entry.Group)
	}
	if entry.Worker == nil {
		t.Error("opening recorded no worker identity, so a rebuild cannot tell an inheritance from an unrecorded one")
	}
}

// TestHeldBackDelegationIsNeverDrawnRunning is the live lie this seam removes.
// While the scheduler is refusing admission, nothing may say the run started —
// a graph and a parent status are claims about now, and no restart corrects one.
func TestHeldBackDelegationIsNeverDrawnRunning(t *testing.T) {
	prov := newLoneProvider()
	task, ctx, sessionPath, sink := loneFixture(t, prov, 1)
	hold, err := task.scheduler.Acquire(context.Background(), AcquireRequest{Writer: false})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, runErr := task.Execute(ctx, json.RawMessage(`{"prompt":"work","description":"lone","read_only":true}`))
		done <- runErr
	}()

	waitUntil(t, "the delegation to be refused a slot", func() bool { return sink.sawState(agentgraph.StateQueued) })
	if sink.sawState(agentgraph.StateRunning) {
		t.Fatalf("drawn %v while the ceiling held it, want nothing past queued", sink.drawn())
	}
	for _, status := range sink.told() {
		if status == string(subagentPhaseRunning) {
			t.Fatalf("the parent was told %v while the ceiling held it", sink.told())
		}
	}
	if got := journalStanding(sessionPath, loneNodeID); got != "opened+queued:slots" {
		t.Fatalf("journal = %q, want the refusal and its cause recorded and no start", got)
	}
	if running := childrenInStore(t, sessionPath, SubagentRunning); len(running) > 0 {
		t.Errorf("store marked %v running before a slot was granted", running)
	}

	hold()
	close(prov.release)
	waitUntil(t, "the delegation to run once the slot freed", func() bool { return sink.sawState(agentgraph.StateRunning) })
	if err := <-done; err != nil {
		t.Fatalf("task: %v", err)
	}
	if got := journalStanding(sessionPath, loneNodeID); got != "opened+queued:slots+started+settled" {
		t.Fatalf("journal = %q, want the whole lifecycle", got)
	}
}

// TestBackgroundDelegationIsNotSettledAtHandoff holds the ownership line. The
// parent call returns a job id, not an ending: settling there would record work
// as let go of while its child is still executing.
func TestBackgroundDelegationIsNotSettledAtHandoff(t *testing.T) {
	prov := newLoneProvider()
	task, ctx, sessionPath, sink := loneFixture(t, prov, 4)
	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx = jobs.WithSession(jobs.WithManager(ctx, jm), "sess-lone")

	out, err := task.Execute(ctx, json.RawMessage(`{"prompt":"work","description":"lone","run_in_background":true}`))
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	<-prov.entered
	if got := journalStanding(sessionPath, loneNodeID); got != "opened+started" {
		t.Fatalf("journal at handoff = %q, want an execution started and not settled", got)
	}
	if !sink.sawState(agentgraph.StateRunning) {
		t.Fatalf("a backgrounded delegation was drawn %v, want it on the graph like any other", sink.drawn())
	}

	close(prov.release)
	if jobID := extractJobID(out); jobID != "" {
		jm.WaitForSession(context.Background(), "sess-lone", []string{jobID}, jobs.WaitOptions{Timeout: 5 * time.Second})
	}
	waitUntil(t, "the job to settle the execution it owns", func() bool {
		return journalStanding(sessionPath, loneNodeID) == "opened+started+settled"
	})
}

// TestBackgroundDelegationIsNotMarkedRunningWhileQueued holds the store to the
// same boundary as everything else. A backgrounded run has its transcript before
// it has a slot, which is exactly how "running" came to mean "a job was
// registered" — and a restart reading that would report work as interrupted
// mid-execution that had never been admitted.
func TestBackgroundDelegationIsNotMarkedRunningWhileQueued(t *testing.T) {
	prov := newLoneProvider()
	task, ctx, sessionPath, sink := loneFixture(t, prov, 1)
	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx = jobs.WithSession(jobs.WithManager(ctx, jm), "sess-lone")
	hold, err := task.scheduler.Acquire(context.Background(), AcquireRequest{Writer: false})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := task.Execute(ctx, json.RawMessage(`{"prompt":"work","description":"lone","run_in_background":true,"read_only":true}`)); err != nil {
		t.Fatalf("task: %v", err)
	}
	waitUntil(t, "the job to be refused a slot", func() bool { return sink.sawState(agentgraph.StateQueued) })
	if running := childrenInStore(t, sessionPath, SubagentRunning); len(running) > 0 {
		t.Errorf("store marked %v running while the job was still queued", running)
	}
	if got := journalStanding(sessionPath, loneNodeID); got != "opened+queued:slots" {
		t.Fatalf("journal = %q, want the refusal recorded and no start", got)
	}

	hold()
	waitUntil(t, "the job to take the slot", func() bool { return sink.sawState(agentgraph.StateRunning) })
	<-prov.entered
	if running := childrenInStore(t, sessionPath, SubagentRunning); len(running) == 0 {
		t.Error("store kept no running child once the slot was granted and the child was executing")
	}
	close(prov.release)
}

// TestEphemeralDelegationOpensNothing is the line this seam must not cross, and
// the graph is on the far side of it. The run graph projects durable facts and a
// reader returns to the snapshot after a gap, so a node no record justifies is
// one the snapshot cannot produce: drawing it puts research on screen that the
// next resync erases while its child still holds a slot.
func TestEphemeralDelegationOpensNothing(t *testing.T) {
	prov := newLoneProvider()
	close(prov.release)
	task, ctx, sessionPath, sink := loneFixture(t, prov, 4)

	if _, err := NewReadOnlyTaskTool(task).Execute(ctx, json.RawMessage(`{"prompt":"look","description":"research"}`)); err != nil {
		t.Fatalf("read_only_task: %v", err)
	}
	if entries := execjournal.History(sessionPath); len(entries) > 0 {
		t.Fatalf("ephemeral delegation wrote %d execution records, want none", len(entries))
	}
	if drawn := sink.drawn(); len(drawn) > 0 {
		t.Errorf("ephemeral delegation drew %v; no snapshot can produce those nodes", drawn)
	}
	// The caller is still told what it delegated. That surface is honestly
	// live-only — a tool call and its status — and claims nothing a later
	// reader is owed.
	if told := sink.told(); len(told) == 0 {
		t.Error("the caller was told nothing about the research it delegated")
	}
}

func journalEntry(t *testing.T, sessionPath, id string) execjournal.Entry {
	t.Helper()
	for _, e := range execjournal.History(sessionPath) {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("journal has no entry for %s", id)
	return execjournal.Entry{}
}

func childrenInStore(t *testing.T, sessionPath string, status SubagentStatus) []string {
	t.Helper()
	artifacts, err := ListSubagentsByParent(filepath.Dir(sessionPath), "probe")
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range artifacts {
		if a.Meta.Status == status {
			out = append(out, a.Ref)
		}
	}
	return out
}

// TestCancelledDelegationSettlesAndOwnsItsTerminal is the cancellation boundary
// read at the source: with the call's own context cancelled, the orchestration
// must let the execution go, and what the store holds afterwards depends on
// whether the child ever ran.
func TestCancelledDelegationSettlesAndOwnsItsTerminal(t *testing.T) {
	prov := newLoneProvider()
	task, ctx, sessionPath, _ := loneFixture(t, prov, 4)
	ctx, cancel := context.WithCancel(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = task.Execute(ctx, json.RawMessage(`{"prompt":"work","description":"lone","read_only":true}`))
	}()
	<-prov.entered
	cancel()
	<-done

	if got := journalStanding(sessionPath, loneNodeID); got != "opened+started+settled" {
		t.Fatalf("journal = %q, want a started execution the orchestration let go of", got)
	}
	if cancelled := childrenInStore(t, sessionPath, SubagentCancelled); len(cancelled) == 0 {
		t.Error("the child ran and was cancelled, and the store kept no cancelled record of it")
	}
}

// TestRefusedAdmissionLeavesNoChildTerminal is the ownership line 8c drew. A
// delegation the scheduler never admitted produced nothing, so the store is
// owed no record of it — and a failed one there outranks the journal on a
// rebuild, turning a cancellation into a failure that never happened.
func TestRefusedAdmissionLeavesNoChildTerminal(t *testing.T) {
	prov := newLoneProvider()
	task, ctx, sessionPath, sink := loneFixture(t, prov, 1)
	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx = jobs.WithSession(jobs.WithManager(ctx, jm), "sess-lone")
	hold, err := task.scheduler.Acquire(context.Background(), AcquireRequest{Writer: false})
	if err != nil {
		t.Fatal(err)
	}
	defer hold()

	out, err := task.Execute(ctx, json.RawMessage(`{"prompt":"work","description":"lone","run_in_background":true,"read_only":true}`))
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	waitUntil(t, "the job to be refused a slot", func() bool { return sink.sawState(agentgraph.StateQueued) })

	if jobID := extractJobID(out); jobID != "" {
		jm.KillForSession("sess-lone", jobID)
		jm.WaitForSession(context.Background(), "sess-lone", []string{jobID}, jobs.WaitOptions{Timeout: 5 * time.Second})
	}
	waitUntil(t, "the orchestration to let the refused delegation go", func() bool {
		return journalStanding(sessionPath, loneNodeID) == "opened+queued:slots+settled"
	})
	artifacts, _ := ListSubagentsByParent(filepath.Dir(sessionPath), "probe")
	for _, a := range artifacts {
		if a.Meta.ParentToolCallID == loneNodeID {
			t.Fatalf("store kept %q for a delegation that was never admitted", a.Meta.Status)
		}
	}
}

// A store that cannot record an execution stops it, at both entry points. The
// store is what an execution is addressed by once its call has returned, so a
// child allowed to act without one would have done work nothing can be asked
// about afterwards. The two are asserted together because drifting apart is
// exactly what put them on different sides of this boundary before.
func TestAStoreThatCannotRecordAnExecutionStopsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not refuse writes on Windows")
	}
	t.Run("lone delegation", func(t *testing.T) {
		task, ctx, sessionPath, sink, prov := refusingStoreFixture(t, loneNodeID, 1)
		if _, err := task.RunProfileSpec(ctx, ProfileExecSpec{
			Task:   TaskSpec{Objective: "work the store cannot record"},
			Worker: WorkerSpec{Kind: "task", Name: "task", SystemPrompt: "sys"},
			Grant:  CapabilityGrant{ReadOnly: true},
		}); err == nil {
			t.Fatal("a delegation the store could not record ran anyway")
		}
		assertRefusedBeforeActing(t, task, prov, sink, sessionPath, loneNodeID)
	})

	t.Run("fan-out item", func(t *testing.T) {
		task, ctx, sessionPath, sink, prov := refusingStoreFixture(t, fanOutCall+"/fleet-", 2)
		out, err := NewFleetTool(task).Execute(ctx, json.RawMessage(
			`{"tasks":[{"prompt":"one","read_only":true},{"prompt":"two","read_only":true}]}`))
		if err != nil {
			t.Fatalf("fleet: %v", err)
		}
		if !strings.Contains(out, "status: failed") {
			t.Fatalf("aggregate = %q, want items the store refused reported as failed", out)
		}
		assertRefusedBeforeActing(t, task, prov, sink, sessionPath, fanOutCall+"/fleet-1")
	})
}

const fanOutCall = "fanout-call"

// refusingStoreFixture is an ordinary world with one thing broken: the store's
// directory cannot be written to. The journal is reachable and the scheduler has
// capacity, so only the record of the execution itself is refused.
func refusingStoreFixture(t *testing.T, node string, total int) (*TaskTool, context.Context, string, *loneSink, *loneProvider) {
	t.Helper()
	root := testenv.TempDir(t)
	sessions := testenv.TempDir(t)
	storeDir := filepath.Join(sessions, "subagents")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Restored before the temp dir's own cleanup, which runs after this one and
	// cannot remove a directory it may not write to.
	t.Cleanup(func() { _ = os.Chmod(storeDir, 0o700) })
	if err := os.Chmod(storeDir, 0o500); err != nil {
		t.Fatal(err)
	}
	prov := newLoneProvider()
	// Released up front: a child that reaches the provider must fail the test
	// rather than hold it open until the deadline.
	close(prov.release)
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(storeDir), root, "base", "high").
		WithScheduler(NewSubagentScheduler(total, total))
	sessionPath := filepath.Join(sessions, "probe.jsonl")
	sink := &loneSink{sessionPath: sessionPath, node: node}
	callID := fanOutCall
	if node == loneNodeID {
		callID = "lone-call"
	}
	ctx := withCallContext(context.Background(), callID, sink, nil, false)
	ctx = WithTurnIdentity(WithParentSession(ctx, "probe"), "turn-1")
	return task, ctx, sessionPath, sink, prov
}

// assertRefusedBeforeActing is the whole contract of a refused record: the child
// never acted, nothing drew it running, the slot came back, and the journal
// keeps the start it honestly wrote — a residue nothing used, not a state to
// roll back.
func assertRefusedBeforeActing(t *testing.T, task *TaskTool, prov *loneProvider, sink *loneSink, sessionPath, execution string) {
	t.Helper()
	select {
	case <-prov.entered:
		t.Fatal("a child acted under an execution the store has no record of")
	default:
	}
	if sink.sawState(agentgraph.StateRunning) {
		t.Fatalf("drawn %v, want nothing running behind a store that refused the record", sink.drawn())
	}
	if got := journalStanding(sessionPath, execution); !strings.Contains(got, "+started") {
		t.Fatalf("journal = %q, want the slot grant kept as written", got)
	}
	total, _ := task.scheduler.Limits()
	for slot := range total {
		release, err := task.scheduler.Acquire(context.Background(), AcquireRequest{Nested: true})
		if err != nil {
			t.Fatalf("slot %d did not come back after the refusal: %v", slot+1, err)
		}
		defer release()
	}
}

package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
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
	sink := &loneSink{sessionPath: sessionPath}
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
	states      []agentgraph.NodeState
	durable     []string
	statuses    []string
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
		if n.ID != loneNodeID || n.State == "" {
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

// TestEphemeralDelegationRecordsNothing is the line this seam must not cross.
// read_only_task promises no durable host side effects, so wiring a lifecycle
// into the runner it shares must leave its research children out of the history
// a restart rebuilds from — a promise, not an omission to tidy up later.
func TestEphemeralDelegationRecordsNothing(t *testing.T) {
	prov := newLoneProvider()
	close(prov.release)
	task, ctx, sessionPath, sink := loneFixture(t, prov, 4)

	if _, err := NewReadOnlyTaskTool(task).Execute(ctx, json.RawMessage(`{"prompt":"look","description":"research"}`)); err != nil {
		t.Fatalf("read_only_task: %v", err)
	}
	if entries := execjournal.History(sessionPath); len(entries) > 0 {
		t.Fatalf("ephemeral delegation wrote %d execution records, want none", len(entries))
	}
	if !sink.sawState(agentgraph.StatePending) {
		t.Errorf("ephemeral delegation was drawn %v, want the caller still shown what it delegated", sink.drawn())
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

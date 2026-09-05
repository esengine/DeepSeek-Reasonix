package control

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

func backgroundJobEvent(id string) jobs.CompletionEvent {
	return jobs.CompletionEvent{
		ID: jobs.CompletionEventID(id), JobID: id, Kind: "task",
		Label: "backend sweep", Status: jobs.Done, FinishedAt: time.Now(),
	}
}

// An idle session runs the continuation itself: this is the wake-up the old
// path was missing, where the completion sat in a queue until the user spoke.
func TestJobCompletionStartsTurnWhenSessionIdle(t *testing.T) {
	c, runner, _ := newInboxDispatchController(t)
	if err := c.OnJobCompletion(context.Background(), backgroundJobEvent("task-1")); err != nil {
		t.Fatalf("OnJobCompletion = %v, want nil", err)
	}
	got := waitForInboxDispatch(t, runner)
	if !strings.Contains(got, "task-1") || !strings.Contains(got, "<background-jobs>") {
		t.Fatalf("dispatched turn = %q, want the background-jobs block for task-1", got)
	}
}

// The item is host-authored end to end: whoever reads the queue can tell it
// apart from something the user typed. Dispatch is held so the durable record
// can be read — a delivered item is acknowledged away by the turn it started.
func TestJobCompletionQueuesHostOriginItem(t *testing.T) {
	c, _, _ := newInboxDispatchController(t)
	held := make(chan struct{}, 1)
	c.inbox.mu.Lock()
	c.inbox.beforeDispatchSubmit = func(string) error {
		select {
		case held <- struct{}{}:
		default:
		}
		return errors.New("held for inspection")
	}
	c.inbox.scheduleDispatchRetry = func(time.Duration, func()) {}
	c.inbox.mu.Unlock()

	if err := c.OnJobCompletion(context.Background(), backgroundJobEvent("task-2")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatal("the continuation never reached dispatch")
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	items := st.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("queue has %d items, want 1", len(items))
	}
	if !items[0].Origin.IsHost() {
		t.Errorf("item origin = %q, want host", items[0].Origin)
	}
	if items[0].Source != hostContinuationSource {
		t.Errorf("item source = %q, want %q", items[0].Source, hostContinuationSource)
	}
	// It was enqueued as a steer and refused by an idle session, so the durable
	// record must show the follow-up it degraded into.
	if items[0].Intent != sessioninbox.IntentFollowup {
		t.Errorf("item intent = %q, want %q after an idle steer refusal", items[0].Intent, sessioninbox.IntentFollowup)
	}
}

// End to end over the real manager: a background job finishing is enough to
// give the session a turn, with nothing left queued for the next user message.
func TestBackgroundJobWakesSessionThroughManager(t *testing.T) {
	dir := testenv.TempDir(t)
	runner := &inboxDispatchRunner{inputs: make(chan string, 4)}
	jm := jobs.NewManager(event.Discard)
	c := New(Options{
		Runner: runner, Sink: event.Discard, Jobs: jm,
		SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"),
	})
	t.Cleanup(func() { c.Close(); c.autosaveWG.Wait() })

	j := jm.StartForSession(c.parentSessionID(), "task", "backend sweep",
		func(context.Context, io.Writer) (string, error) { return "done", nil })

	got := waitForInboxDispatch(t, runner)
	if !strings.Contains(got, j.ID) {
		t.Fatalf("dispatched turn = %q, want it to mention %s", got, j.ID)
	}
	if note := jm.DrainCompletedNoteForSession(c.parentSessionID()); note != "" {
		t.Errorf("note still queued after the wake-up: %q", note)
	}
}

// The same terminal event delivered twice is one continuation. Redelivery is
// how the crash path will be closed, so it must already be free.
func TestRepeatedJobCompletionIsIdempotent(t *testing.T) {
	c, runner, _ := newInboxDispatchController(t)
	ev := backgroundJobEvent("task-3")
	if err := c.OnJobCompletion(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	waitForInboxDispatch(t, runner)
	if err := c.OnJobCompletion(context.Background(), ev); err != nil {
		t.Fatalf("replayed OnJobCompletion = %v, want nil", err)
	}
	select {
	case extra := <-runner.inputs:
		t.Fatalf("replay started a second turn: %q", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

// A session with no durable inbox (headless run) must degrade to the old path
// rather than fail: the manager keeps its note when the bridge reports an error.
func TestJobCompletionWithoutSessionPathIsRefused(t *testing.T) {
	c := New(Options{Runner: &inboxDispatchRunner{inputs: make(chan string, 1)}, Sink: event.Discard})
	t.Cleanup(func() { c.Close(); c.autosaveWG.Wait() })
	if err := c.OnJobCompletion(context.Background(), backgroundJobEvent("task-4")); err == nil {
		t.Fatal("OnJobCompletion = nil, want an error so the legacy note survives")
	}
}

// A job owned by another session stays that session's business.
func TestJobCompletionForOtherSessionIsRefused(t *testing.T) {
	c, _, _ := newInboxDispatchController(t)
	ev := backgroundJobEvent("task-5")
	ev.SessionID = "some-other-session"
	if err := c.OnJobCompletion(context.Background(), ev); err == nil {
		t.Fatal("OnJobCompletion = nil, want an error for a foreign session")
	}
}

type liveTurnProvider struct {
	started  chan struct{}
	release  chan struct{}
	requests chan provider.Request
}

func (p *liveTurnProvider) Name() string { return "live-turn" }

func (p *liveTurnProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	select {
	case p.requests <- req:
	default:
	}
	ch := make(chan provider.Chunk, 2)
	select {
	case <-p.started:
		// Later rounds answer immediately.
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "applied"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	default:
		close(p.started)
	}
	go func() {
		defer close(ch)
		select {
		case <-p.release:
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "ready"}
			ch <- provider.Chunk{Type: provider.ChunkDone}
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

// A completion that arrives mid-turn reaches the model at the next model-round
// boundary, attributed to the host — not as a message the user sent.
func TestJobCompletionSteersLiveTurnAsHost(t *testing.T) {
	dir := testenv.TempDir(t)
	prov := &liveTurnProvider{
		started: make(chan struct{}), release: make(chan struct{}),
		requests: make(chan provider.Request, 8),
	}
	sess := agent.NewSession("sys")
	exec := agent.New(prov, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{
		Runner: exec, Executor: exec, Sink: event.Discard,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	release := sync.OnceFunc(func() { close(prov.release) })
	t.Cleanup(func() { release(); c.Close(); c.autosaveWG.Wait() })

	c.Submit("start something long")
	select {
	case <-prov.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the first provider round never started")
	}

	if err := c.OnJobCompletion(context.Background(), backgroundJobEvent("task-6")); err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if items := st.Snapshot().Items; len(items) != 1 || items[0].State != sessioninbox.StateSteerAccepted {
		t.Fatalf("items = %+v, want one steer_accepted item", items)
	}

	// Release the turn so the steer is consumed at the next round boundary.
	release()
	deadline := time.After(3 * time.Second)
	for {
		if hostSteerInHistory(c.History(), "task-6") {
			return
		}
		select {
		case <-deadline:
			t.Fatal("the completion never reached the model as a host notice")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func hostSteerInHistory(msgs []provider.Message, jobID string) bool {
	for _, m := range msgs {
		if m.Role != provider.RoleUser || !strings.Contains(m.Content, jobID) {
			continue
		}
		if strings.Contains(m.Content, agent.HostNoticePrefix) {
			return true
		}
	}
	return false
}

// Guidance the host authored but a dying turn never applied is recorded as the
// host's. Writing it back as the user's would put words in their mouth in the
// one transcript the next turn reads.
func TestUnappliedHostSteerKeepsHostAttribution(t *testing.T) {
	sess := agent.NewSession("sys")
	a := agent.New(&liveTurnProvider{
		started: make(chan struct{}), release: make(chan struct{}),
		requests: make(chan provider.Request, 1),
	}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	a.RecordUnappliedHostSteer("a background job finished")
	for _, m := range a.Session().Snapshot() {
		if strings.Contains(m.Content, "a background job finished") {
			if !strings.Contains(m.Content, agent.HostNoticePrefix) {
				t.Fatalf("unapplied host steer recorded as %q, want the host prefix", m.Content)
			}
			return
		}
	}
	t.Fatal("the unapplied host steer was not recorded at all")
}

// Display is persisted as the turn's RawContent and is what the pending queue
// and the transcript render. Host markup there reads as something the user
// typed — which is how rows of literal <background-jobs> filled the queue
// panel until it had pushed the transcript off the screen.
func TestBackgroundCompletionDisplayCarriesNoHostMarkup(t *testing.T) {
	display, submit := backgroundCompletionTexts(backgroundJobEvent("task-7"))
	if strings.ContainsAny(display, "<>") {
		t.Errorf("display = %q, want no markup", display)
	}
	if !strings.Contains(display, "task-7") || !strings.Contains(display, string(jobs.Done)) {
		t.Errorf("display = %q, want the job and its outcome", display)
	}
	if !strings.Contains(submit, "<background-jobs>") || !strings.Contains(submit, display) {
		t.Errorf("submit = %q, want the tagged block around %q", submit, display)
	}
}

// A failing build finishes thirty jobs in a few seconds. One turn per job is
// thirty turns each reporting one of them, and thirty rows in a queue that has
// no height of its own. While a continuation is still unread, later completions
// stay in the manager's note and ride that turn's <background-jobs> block.
func TestJobBurstFoldsIntoOneContinuation(t *testing.T) {
	c, _, _ := newInboxDispatchController(t)
	held := make(chan struct{}, 1)
	c.inbox.mu.Lock()
	c.inbox.beforeDispatchSubmit = func(string) error {
		select {
		case held <- struct{}{}:
		default:
		}
		return errors.New("held for inspection")
	}
	c.inbox.scheduleDispatchRetry = func(time.Duration, func()) {}
	c.inbox.mu.Unlock()

	if err := c.OnJobCompletion(context.Background(), backgroundJobEvent("burst-1")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatal("the first continuation never reached dispatch")
	}
	// An error is the contract with the manager: it keeps its own note, so the
	// folded job still reaches the model on the turn the queued item starts.
	for _, id := range []string{"burst-2", "burst-3"} {
		if err := c.OnJobCompletion(context.Background(), backgroundJobEvent(id)); err == nil {
			t.Fatalf("OnJobCompletion(%s) = nil, want a refusal so the note survives", id)
		}
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if items := st.Snapshot().Items; len(items) != 1 {
		t.Fatalf("queue has %d items, want 1 for a burst of three", len(items))
	}
}

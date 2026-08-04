package control

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/scheduler"
	"reasonix/internal/tool"
)

// blockingTurns is a scriptedTurns variant whose first Stream call blocks
// until release is closed, so a test can fire a scheduled task while the
// agent turn is provably mid-flight with its steer intake open. When err is
// set, the first Stream call returns it after release — a plain provider
// error (not a ctx interrupt), so the run loop exits without a recovery
// retry and any queued steer is flushed unapplied.
type blockingTurns struct {
	turns   [][]provider.Chunk
	call    int
	entered chan struct{} // closed when the first Stream call starts
	release chan struct{} // Stream blocks until closed
	err     error         // returned by the first Stream call after release
}

func (b *blockingTurns) Name() string { return "blocking" }

func (b *blockingTurns) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	i := b.call
	if i >= len(b.turns) {
		i = len(b.turns) - 1
	}
	b.call++
	if i == 0 {
		close(b.entered)
		<-b.release
		if b.err != nil {
			return nil, b.err
		}
	}
	ch := make(chan provider.Chunk, len(b.turns[i]))
	for _, chunk := range b.turns[i] {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRunScheduledTurnInjectsMidTurn is the core steering test: a due task
// firing while a foreground turn is mid-flight is injected into the running
// turn's message queue (labeled, persisted to the session for rewind/resume
// replay) and the notice shows the prompt preview.
func TestRunScheduledTurnInjectsMidTurn(t *testing.T) {
	prov := &blockingTurns{
		turns:   [][]provider.Chunk{textTurn("working..."), textTurn("done.")},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	reg := tool.NewRegistry()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

	turnDone := make(chan struct{}, 4)
	var notices []string
	var mu sync.Mutex
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e.Text)
			mu.Unlock()
		}
		if e.Kind == event.TurnDone {
			select {
			case turnDone <- struct{}{}:
			default:
			}
		}
	})
	sched := scheduler.New()
	c := New(Options{Runner: ag, Executor: ag, Sink: sink, Scheduler: sched})
	defer c.Close()

	c.Send("start the turn")
	<-prov.entered // the agent loop is now mid-flight

	// A dynamic task added with a future wakeup never fires on its own; we
	// deliver the fire manually, exactly like the ticker would. The task is
	// really in the scheduler, so the injection path's MarkStarted operates
	// on the live firing state.
	id, err := sched.Add("", "check the deploy", time.Now().Add(10*time.Minute), false, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	c.runScheduledTurn(scheduler.Task{ID: id, Prompt: "check the deploy"})

	// Injection is synchronous: the message is queued and the firing flag
	// consumed before the turn resumes.
	close(prov.release)
	select {
	case <-turnDone:
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}

	// The injected message is persisted to the session like any turn input.
	if !sessionHasUserText(ag, agent.MidTurnScheduledPrefix) {
		t.Error("session missing the scheduled-task wrapper")
	}
	if !sessionHasUserText(ag, "check the deploy") {
		t.Error("session missing the injected prompt")
	}
	// Rewind/resume replay consistency: SteerText recognizes the persisted
	// message and returns the live label+prompt text.
	for _, m := range ag.Session().Snapshot() {
		if text, ok := agent.SteerText(m.Content); ok {
			if want := "⏰ scheduled task " + id + ":\ncheck the deploy"; text != want {
				t.Errorf("replayed steer text = %q, want %q", text, want)
			}
			break
		}
	}

	// The notice carries the task id and the prompt preview.
	mu.Lock()
	got := append([]string(nil), notices...)
	mu.Unlock()
	found := false
	for _, n := range got {
		if strings.Contains(n, id) && strings.Contains(n, "injected") && strings.Contains(n, "check the deploy") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no injected notice with id+preview: %v", got)
	}
}

// TestRunScheduledTurnIdleGapRunsFullTurn: when no turn is active the fire
// still runs as a full scheduled turn — nothing is steered into any queue.
func TestRunScheduledTurnIdleGapRunsFullTurn(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("done.")}}
	reg := tool.NewRegistry()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

	turnDone := make(chan struct{}, 4)
	var notices []string
	var mu sync.Mutex
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e.Text)
			mu.Unlock()
		}
		if e.Kind == event.TurnDone {
			select {
			case turnDone <- struct{}{}:
			default:
			}
		}
	})
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	defer c.Close()

	c.runScheduledTurn(scheduler.Task{ID: "abcd1234", Prompt: "check deploy", CronExpr: "*/1 * * * *"})
	select {
	case <-turnDone:
	case <-time.After(10 * time.Second):
		t.Fatal("scheduled turn did not finish")
	}

	mu.Lock()
	got := append([]string(nil), notices...)
	mu.Unlock()
	for _, n := range got {
		if strings.Contains(n, "injected") {
			t.Fatalf("idle-gap fire was injected instead of run: %v", got)
		}
	}
	if len(got) != 1 || !strings.Contains(got[0], "abcd1234") || !strings.Contains(got[0], "running") {
		t.Fatalf("notices = %v, want exactly one 'scheduled task abcd1234 running'", got)
	}
	if sessionHasUserText(ag, agent.MidTurnScheduledPrefix) {
		t.Error("idle-gap fire must not leave a steering message in the session")
	}
}

// TestScheduledInjectionFiresOncePerDueSlot: a running turn spanning two due
// slots gets exactly one injection per delivered fire — MarkStarted consumes
// the firing flag at injection time, and a re-armed wakeup delivers again.
func TestScheduledInjectionFiresOncePerDueSlot(t *testing.T) {
	prov := &blockingTurns{
		turns:   [][]provider.Chunk{textTurn("working..."), textTurn("done.")},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	var notices []string
	var mu sync.Mutex
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e.Text)
			mu.Unlock()
		}
	})
	sched := scheduler.New()
	c := New(Options{Runner: ag, Executor: ag, Sink: sink, Scheduler: sched})
	defer c.Close()

	countInjected := func() int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, t := range notices {
			if strings.Contains(t, "injected") {
				n++
			}
		}
		return n
	}

	c.Send("start the turn")
	<-prov.entered

	// A dynamic task due immediately: the live ticker delivers it once.
	if _, err := sched.Add("", "loop prompt", time.Now(), false, false); err != nil {
		t.Fatalf("Add: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return countInjected() == 1 })
	// A long turn spanning several tick cycles must not re-fire the same slot.
	time.Sleep(2500 * time.Millisecond)
	if n := countInjected(); n != 1 {
		t.Fatalf("injections = %d after 2.5s of a long turn, want exactly 1 (firing-flag coalescing)", n)
	}

	// The next schedule_wakeup arms a NEW slot, which delivers again.
	sched.ScheduleWakeup(200 * time.Millisecond)
	waitFor(t, 5*time.Second, func() bool { return countInjected() == 2 })

	close(prov.release)
	waitIdleAdmission(t, c)
}

// TestScheduledDeletePreventsInjection: canceling a task before its fire
// means nothing is ever injected — no notice, no steering message.
func TestScheduledDeletePreventsInjection(t *testing.T) {
	prov := &blockingTurns{
		turns:   [][]provider.Chunk{textTurn("working..."), textTurn("done.")},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	var notices []string
	var mu sync.Mutex
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e.Text)
			mu.Unlock()
		}
	})
	sched := scheduler.New()
	c := New(Options{Runner: ag, Executor: ag, Sink: sink, Scheduler: sched})
	defer c.Close()

	c.Send("start the turn")
	<-prov.entered

	id, err := sched.Add("", "loop prompt", time.Now(), false, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !sched.Delete(id) {
		t.Fatalf("Delete(%s) = false", id)
	}

	time.Sleep(2500 * time.Millisecond)
	mu.Lock()
	for _, n := range notices {
		if strings.Contains(n, id) || strings.Contains(n, "injected") {
			t.Fatalf("deleted task still fired: %v", notices)
		}
	}
	mu.Unlock()
	if sessionHasUserText(ag, agent.MidTurnScheduledPrefix) {
		t.Error("deleted task left a steering message in the session")
	}

	close(prov.release)
	waitIdleAdmission(t, c)
}

// TestScheduledInjectionRearmsOnUnapplied covers the lost-fire window: the
// injection is accepted into the steer queue, but the turn dies before the
// message reaches the model (provider error → flushSteerQueue → unapplied).
// The fire must be re-armed so the next tick retries it instead of silently
// spending the wakeup.
func TestScheduledInjectionRearmsOnUnapplied(t *testing.T) {
	prov := &blockingTurns{
		turns:   [][]provider.Chunk{textTurn("working..."), textTurn("done.")},
		entered: make(chan struct{}),
		release: make(chan struct{}),
		err:     errors.New("provider died mid-turn"),
	}
	var notices []string
	var mu sync.Mutex
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e.Text)
			mu.Unlock()
		}
	})
	// The agent's own sink carries the unapplied-steer notice
	// (RecordUnappliedSteer emits on the agent sink, not the controller's),
	// so capture it too.
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, sink)
	sched := scheduler.New()
	c := New(Options{Runner: ag, Executor: ag, Sink: sink, Scheduler: sched})
	defer c.Close()

	c.Send("start the turn")
	<-prov.entered

	id, err := sched.Add("", "loop prompt", time.Now().Add(10*time.Minute), false, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	c.runScheduledTurn(scheduler.Task{ID: id, Prompt: "loop prompt"})

	close(prov.release) // first Stream returns the provider error
	waitIdleAdmission(t, c)

	// The fire was re-armed: the next tick retries it. The controller is
	// idle now, so the retry runs as a full scheduled turn ("running"
	// notice). Don't assert NextDue — the live ticker may already have
	// consumed the re-armed slot.
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, n := range notices {
			if strings.Contains(n, id) && strings.Contains(n, "running") {
				return true
			}
		}
		return false
	})

	mu.Lock()
	defer mu.Unlock()
	for _, n := range notices {
		if strings.Contains(n, "not applied") {
			return
		}
	}
	t.Fatalf("no unapplied-guidance notice after the lost fire: %v", notices)
}

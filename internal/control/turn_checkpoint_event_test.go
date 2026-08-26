package control

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type checkpointEventRunner struct {
	session   *agent.Session
	err       error
	started   chan struct{}
	wait      bool
	skipUser  bool
	localOnly bool
}

func (r *checkpointEventRunner) Run(ctx context.Context, input string) error {
	if !r.skipUser {
		r.session.Add(provider.Message{
			Role: provider.RoleUser, Content: input, LocalOnly: r.localOnly,
			CreatedAt: time.Now().UnixMilli(),
		})
	}
	if r.started != nil {
		close(r.started)
	}
	if r.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	return r.err
}

func newCheckpointEventController(runner *checkpointEventRunner) (*Controller, <-chan event.Event) {
	events := make(chan event.Event, 8)
	executor := agent.New(nil, tool.NewRegistry(), runner.session, agent.Options{}, event.Discard)
	controller := New(Options{
		Runner: runner, Executor: executor,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				events <- e
			}
		}),
	})
	return controller, events
}

func receiveCheckpointTurnDone(t *testing.T, events <-chan event.Event) event.Event {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for TurnDone")
		return event.Event{}
	}
}

func requireCheckpointTurn(t *testing.T, e event.Event, want int) {
	t.Helper()
	if e.CheckpointTurn == nil || *e.CheckpointTurn != want {
		t.Fatalf("TurnDone checkpoint = %v, want %d", e.CheckpointTurn, want)
	}
}

func TestTurnDoneCarriesValidatedCheckpointAcrossSuccessAndError(t *testing.T) {
	session := agent.NewSession("system")
	runner := &checkpointEventRunner{session: session}
	controller, events := newCheckpointEventController(runner)
	defer controller.Close()

	controller.Send("first prompt")
	first := receiveCheckpointTurnDone(t, events)
	if first.Err != nil {
		t.Fatalf("successful TurnDone error = %v", first.Err)
	}
	requireCheckpointTurn(t, first, 0)

	runner.err = errors.New("provider failed")
	controller.Send("second prompt")
	second := receiveCheckpointTurnDone(t, events)
	if second.Err == nil {
		t.Fatal("provider failure TurnDone must retain its error")
	}
	requireCheckpointTurn(t, second, 1)
}

func TestCancelledTurnDoneCarriesRetainedUserCheckpoint(t *testing.T) {
	session := agent.NewSession("system")
	started := make(chan struct{})
	runner := &checkpointEventRunner{session: session, started: started, wait: true}
	controller, events := newCheckpointEventController(runner)
	defer controller.Close()

	controller.Send("cancel this prompt")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
	controller.Cancel()
	done := receiveCheckpointTurnDone(t, events)
	if !done.Cancelled || done.Err != nil || done.Status != event.TurnInterrupted {
		t.Fatalf("cancelled TurnDone = %+v, want interrupted terminal without send error", done)
	}
	requireCheckpointTurn(t, done, 0)
}

func TestCancelBeforeRunnerAddsUserCarriesFallbackCheckpoint(t *testing.T) {
	session := agent.NewSession("system")
	started := make(chan struct{})
	runner := &checkpointEventRunner{session: session, started: started, wait: true, skipUser: true}
	controller, events := newCheckpointEventController(runner)
	defer controller.Close()

	controller.Send("cancel before user append")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
	controller.Cancel()
	done := receiveCheckpointTurnDone(t, events)
	requireCheckpointTurn(t, done, 0)
	messages := session.Snapshot()
	if len(messages) < 2 || messages[1].Role != provider.RoleUser ||
		!agent.IsUserAuthoredTurn(agent.UserMessageText(messages[1])) {
		t.Fatalf("cancel fallback messages = %+v, want a retained user prompt at the checkpoint boundary", messages)
	}
}

func TestTurnDoneOmitsUncommittedOrNonVisibleCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name      string
		skipUser  bool
		localOnly bool
	}{
		{name: "no user committed", skipUser: true},
		{name: "local-only user", localOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := agent.NewSession("system")
			runner := &checkpointEventRunner{session: session, skipUser: tc.skipUser, localOnly: tc.localOnly}
			controller, events := newCheckpointEventController(runner)
			defer controller.Close()

			controller.Send("blocked prompt")
			if done := receiveCheckpointTurnDone(t, events); done.CheckpointTurn != nil {
				t.Fatalf("uncommitted checkpoint leaked into TurnDone: %d", *done.CheckpointTurn)
			}
		})
	}
}

func TestTurnDoneRejectsCheckpointAfterSessionSwap(t *testing.T) {
	oldSession := agent.NewSession("system")
	completion := &guardedTurnCompletion{}
	ctx := context.WithValue(context.Background(), guardedTurnCompletionKey{}, completion)
	runner := &checkpointEventRunner{session: oldSession}
	controller, _ := newCheckpointEventController(runner)
	defer controller.Close()

	controller.beginCheckpoint(ctx, "old prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "old prompt", CreatedAt: time.Now().UnixMilli()})
	controller.executor.SetSession(agent.NewSession("replacement"))
	if got := controller.validatedCheckpointTurn(completion); got != nil {
		t.Fatalf("session-swapped checkpoint = %d, want nil", *got)
	}
}

func TestTurnDoneRejectsSameSessionCheckpointStoreCollision(t *testing.T) {
	session := agent.NewSession("system")
	completion := &guardedTurnCompletion{}
	ctx := context.WithValue(context.Background(), guardedTurnCompletionKey{}, completion)
	runner := &checkpointEventRunner{session: session}
	controller, _ := newCheckpointEventController(runner)
	defer controller.Close()

	controller.beginCheckpoint(ctx, "original prompt")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "original prompt", CreatedAt: time.Now().UnixMilli()})
	controller.checkpoints.rebind("", "")
	if turn, _, ok := controller.checkpoints.beginWithObserver("collision", 1, nil); !ok || turn != 0 {
		t.Fatalf("replacement checkpoint = (%d, %v), want colliding turn zero", turn, ok)
	}
	if got := controller.validatedCheckpointTurn(completion); got != nil {
		t.Fatalf("store-rebound checkpoint = %d, want nil", *got)
	}
}

func TestBlockedCandidateDoesNotLeakIntoNextTurn(t *testing.T) {
	session := agent.NewSession("system")
	runner := &checkpointEventRunner{session: session, skipUser: true}
	controller, events := newCheckpointEventController(runner)
	defer controller.Close()

	controller.Send("blocked before user append")
	if done := receiveCheckpointTurnDone(t, events); done.CheckpointTurn != nil {
		t.Fatalf("blocked TurnDone checkpoint = %d, want nil", *done.CheckpointTurn)
	}

	runner.skipUser = false
	controller.Send("next real prompt")
	requireCheckpointTurn(t, receiveCheckpointTurnDone(t, events), 1)
}

func TestParkedTurnsKeepIndependentCheckpointCandidates(t *testing.T) {
	session := agent.NewSession("system")
	runner := &checkpointEventRunner{session: session}
	events := make(chan event.Event, 2)
	firstDelivery := make(chan struct{})
	releaseFirst := make(chan struct{})
	var deliveries atomic.Int32
	executor := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	controller := New(Options{
		Runner: runner, Executor: executor,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind != event.TurnDone {
				return
			}
			if deliveries.Add(1) == 1 {
				close(firstDelivery)
				<-releaseFirst
			}
			events <- e
		}),
	})
	defer controller.Close()

	controller.Send("first prompt")
	select {
	case <-firstDelivery:
	case <-time.After(5 * time.Second):
		t.Fatal("first TurnDone delivery did not start")
	}
	controller.Send("parked second prompt")
	close(releaseFirst)
	requireCheckpointTurn(t, receiveCheckpointTurnDone(t, events), 0)
	requireCheckpointTurn(t, receiveCheckpointTurnDone(t, events), 1)
}

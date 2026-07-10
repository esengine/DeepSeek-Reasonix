package control

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type sideTestProvider struct {
	started   chan string
	release   chan struct{}
	done      chan struct{}
	text      string
	ignoreCtx bool
}

func (p *sideTestProvider) Name() string { return "side-test" }

func (p *sideTestProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.done != nil {
		defer close(p.done)
	}
	if p.started != nil {
		p.started <- lastUserContent(req.Messages)
	}
	if p.release != nil {
		if p.ignoreCtx {
			<-p.release
		} else {
			select {
			case <-p.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	text := p.text
	if text == "" {
		text = "side answer"
	}
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	close(ch)
	return ch, nil
}

func lastUserContent(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

func newSideTestAgent(sess *agent.Session, sink event.Sink, prov *sideTestProvider) *agent.Agent {
	return agent.New(prov, tool.NewRegistry(), sess, agent.Options{}, sink)
}

func drainEvent(ch <-chan event.Event, timeout time.Duration) (event.Event, bool) {
	select {
	case e := <-ch:
		return e, true
	case <-time.After(timeout):
		return event.Event{}, false
	}
}

type sideReturnNoError interface {
	ReturnFromSide()
}

var _ sideReturnNoError = (*Controller)(nil)

func TestSideStateDoesNotExposeHistory(t *testing.T) {
	if _, ok := reflect.TypeOf(SideState{}).FieldByName("History"); ok {
		t.Fatal("SideState should expose only active/runtime state")
	}
}

func TestStartSideCopiesMainHistoryAndAppendsBoundary(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "main answer"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)

	var childSession *agent.Session
	ctrl := New(Options{
		Executor: mainExec,
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			childSession = sess
			return newSideTestAgent(sess, sink, &sideTestProvider{}), nil
		},
	})

	if err := ctrl.StartSide(""); err != nil {
		t.Fatalf("StartSide: %v", err)
	}
	st := ctrl.SideState()
	if !st.Active {
		t.Fatal("side should be active")
	}
	if childSession == nil {
		t.Fatal("side factory did not receive a child session")
	}
	msgs := childSession.Snapshot()
	if len(msgs) != 4 {
		t.Fatalf("child messages len = %d, want 4", len(msgs))
	}
	if msgs[1].Content != "main question" || msgs[2].Content != "main answer" {
		t.Fatalf("child did not copy main history: %+v", msgs)
	}
	if msgs[3].Role != provider.RoleUser {
		t.Fatalf("boundary message missing: %+v", msgs[3])
	}
	for _, clause := range []string{
		"inherited parent history is reference context only",
		"Only messages after this boundary are active instructions",
		"separate from the main conversation",
		"read-only",
		"Mutation requests must be refused",
		"return to the main conversation",
	} {
		if !strings.Contains(msgs[3].Content, clause) {
			t.Fatalf("boundary message missing clause %q: %q", clause, msgs[3].Content)
		}
	}
	if len(mainSess.Snapshot()) != 3 {
		t.Fatalf("main session mutated, len=%d", len(mainSess.Snapshot()))
	}
}

func TestStartSideInlineInputRunsSideOnly(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	started := make(chan string, 1)

	ctrl := New(Options{
		Executor: mainExec,
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return newSideTestAgent(sess, sink, &sideTestProvider{started: started}), nil
		},
	})

	if err := ctrl.StartSide("explain this"); err != nil {
		t.Fatalf("StartSide: %v", err)
	}
	select {
	case got := <-started:
		if got != "explain this" {
			t.Fatalf("side input = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("side input was not submitted")
	}
	if len(mainSess.Snapshot()) != 2 {
		t.Fatalf("main session should not receive side input, got len=%d", len(mainSess.Snapshot()))
	}
}

func TestReturnFromSideCancelsAndDiscards(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	started := make(chan string, 1)
	done := make(chan struct{})

	ctrl := New(Options{
		Executor: mainExec,
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return newSideTestAgent(sess, sink, &sideTestProvider{started: started, release: make(chan struct{}), done: done}), nil
		},
	})

	if err := ctrl.StartSide("hold"); err != nil {
		t.Fatalf("StartSide: %v", err)
	}
	<-started
	ctrl.ReturnFromSide()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("side did not cancel")
	}
	if st := ctrl.SideState(); st.Active {
		t.Fatalf("side should be discarded: %+v", st)
	}
}

func TestReturnFromSideSuppressesDiscardedTurnDone(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	started := make(chan string, 1)
	done := make(chan struct{})
	release := make(chan struct{})
	sideEvents := make(chan event.Event, 8)

	ctrl := New(Options{
		Executor: mainExec,
		SideSink: event.FuncSink(func(e event.Event) {
			sideEvents <- e
		}),
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return newSideTestAgent(sess, sink, &sideTestProvider{started: started, release: release, done: done, ignoreCtx: true}), nil
		},
	})

	if err := ctrl.StartSide("hold"); err != nil {
		t.Fatalf("StartSide: %v", err)
	}
	<-started
	if e, ok := drainEvent(sideEvents, time.Second); !ok || e.Kind != event.TurnStarted {
		t.Fatalf("expected initial TurnStarted before discard, got %+v ok=%v", e, ok)
	}
	ctrl.ReturnFromSide()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("side runner did not return after ReturnFromSide")
	}
	select {
	case e := <-sideEvents:
		t.Fatalf("discarded side emitted stale event: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestReturnFromSideSuppressesDiscardedSideEvents(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	started := make(chan string, 1)
	done := make(chan struct{})
	release := make(chan struct{})
	sideEvents := make(chan event.Event, 8)

	ctrl := New(Options{
		Executor: mainExec,
		SideSink: event.FuncSink(func(e event.Event) {
			sideEvents <- e
		}),
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return newSideTestAgent(sess, sink, &sideTestProvider{started: started, release: release, done: done, text: "stale text", ignoreCtx: true}), nil
		},
	})

	if err := ctrl.StartSide("hold"); err != nil {
		t.Fatalf("StartSide: %v", err)
	}
	<-started
	if e, ok := drainEvent(sideEvents, time.Second); !ok || e.Kind != event.TurnStarted {
		t.Fatalf("expected initial TurnStarted before discard, got %+v ok=%v", e, ok)
	}
	ctrl.ReturnFromSide()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("side runner did not return after ReturnFromSide")
	}
	select {
	case e := <-sideEvents:
		t.Fatalf("discarded side emitted stale event: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStartSideDropsOldEventsAfterNewSideStarts(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	oldStarted := make(chan string, 1)
	oldDone := make(chan struct{})
	oldRelease := make(chan struct{})
	sideEvents := make(chan event.Event, 8)
	var mu sync.Mutex
	calls := 0

	ctrl := New(Options{
		Executor: mainExec,
		SideSink: event.FuncSink(func(e event.Event) {
			sideEvents <- e
		}),
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			mu.Lock()
			calls++
			call := calls
			mu.Unlock()
			if call == 1 {
				return newSideTestAgent(sess, sink, &sideTestProvider{started: oldStarted, release: oldRelease, done: oldDone, text: "old text", ignoreCtx: true}), nil
			}
			return newSideTestAgent(sess, sink, &sideTestProvider{text: "new text"}), nil
		},
	})

	if err := ctrl.StartSide("old"); err != nil {
		t.Fatalf("StartSide old: %v", err)
	}
	<-oldStarted
	if e, ok := drainEvent(sideEvents, time.Second); !ok || e.Kind != event.TurnStarted {
		t.Fatalf("expected old TurnStarted, got %+v ok=%v", e, ok)
	}
	ctrl.ReturnFromSide()
	if err := ctrl.StartSide(""); err != nil {
		t.Fatalf("StartSide new: %v", err)
	}
	close(oldRelease)
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("old side runner did not return")
	}
	select {
	case e := <-sideEvents:
		t.Fatalf("old side emitted after new side started: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStartSideRejectsSecondSideAndMissingFactory(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)

	ctrl := New(Options{Executor: mainExec})
	if err := ctrl.StartSide(""); err == nil {
		t.Fatal("StartSide without SideFactory should fail")
	}

	ctrl = New(Options{
		Executor: mainExec,
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return newSideTestAgent(sess, sink, &sideTestProvider{}), nil
		},
	})
	if err := ctrl.StartSide(""); err != nil {
		t.Fatalf("first StartSide: %v", err)
	}
	if err := ctrl.StartSide(""); err == nil {
		t.Fatal("second StartSide should fail")
	}
}

func TestStartSideRejectsNilChildAgent(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)

	ctrl := New(Options{
		Executor: mainExec,
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return nil, nil
		},
	})
	if err := ctrl.StartSide(""); !errors.Is(err, ErrSideUnavailable) {
		t.Fatalf("StartSide error = %v, want %v", err, ErrSideUnavailable)
	}
}

func TestStartSidePropagatesFactoryFailure(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	want := errors.New("factory failed")
	ctrl := New(Options{
		Executor: mainExec,
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return nil, want
		},
	})
	if err := ctrl.StartSide(""); !errors.Is(err, want) {
		t.Fatalf("StartSide error = %v, want %v", err, want)
	}
}

func TestSideStateReportsRuntimeAndHidesBoundary(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	started := make(chan string, 1)
	done := make(chan struct{})

	ctrl := New(Options{
		Executor: mainExec,
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return newSideTestAgent(sess, sink, &sideTestProvider{started: started, release: make(chan struct{}), done: done}), nil
		},
	})

	if err := ctrl.StartSide("hold"); err != nil {
		t.Fatalf("StartSide: %v", err)
	}
	defer func() {
		ctrl.ReturnFromSide()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("side did not cancel")
		}
	}()
	<-started
	st := ctrl.SideState()
	if !st.Active {
		t.Fatal("side should be active")
	}
	if !st.Runtime.Running || !st.Runtime.Cancellable {
		t.Fatalf("side runtime = %+v, want running and cancellable", st.Runtime)
	}
	if st.Runtime.CancelRequested {
		t.Fatalf("side runtime cancel requested before cancel: %+v", st.Runtime)
	}
}

func TestSideEventSinkIsSeparateFromMainSink(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	var mainMu sync.Mutex
	var mainEvents int
	sideEvents := make(chan event.Event, 4)
	ctrl := New(Options{
		Executor: mainExec,
		Sink: event.FuncSink(func(event.Event) {
			mainMu.Lock()
			mainEvents++
			mainMu.Unlock()
		}),
		SideSink: event.FuncSink(func(e event.Event) {
			sideEvents <- e
		}),
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return newSideTestAgent(sess, sink, &sideTestProvider{}), nil
		},
	})
	if err := ctrl.StartSide("side"); err != nil {
		t.Fatalf("StartSide: %v", err)
	}
	select {
	case <-sideEvents:
	case <-time.After(time.Second):
		t.Fatal("expected side event")
	}
	mainMu.Lock()
	defer mainMu.Unlock()
	if mainEvents != 0 {
		t.Fatalf("side events should not use main sink, got %d mainEvents", mainEvents)
	}
}

func TestReleaseResourcesCancelsRunningSide(t *testing.T) {
	mainSess := agent.NewSession("sys")
	mainSess.Add(provider.Message{Role: provider.RoleUser, Content: "main question"})
	mainExec := agent.New(nil, nil, mainSess, agent.Options{}, event.Discard)
	started := make(chan string, 1)
	done := make(chan struct{})
	sideEvents := make(chan event.Event, 8)

	ctrl := New(Options{
		Executor: mainExec,
		SideSink: event.FuncSink(func(e event.Event) {
			sideEvents <- e
		}),
		SideFactory: func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error) {
			return newSideTestAgent(sess, sink, &sideTestProvider{started: started, release: make(chan struct{}), done: done}), nil
		},
	})
	if err := ctrl.StartSide("hold"); err != nil {
		t.Fatalf("StartSide: %v", err)
	}
	<-started
	if e, ok := drainEvent(sideEvents, time.Second); !ok || e.Kind != event.TurnStarted {
		t.Fatalf("expected initial TurnStarted before cleanup, got %+v ok=%v", e, ok)
	}

	ctrl.ReleaseResources()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("side did not cancel on ReleaseResources")
	}
	if st := ctrl.SideState(); st.Active {
		t.Fatalf("side should be inactive after ReleaseResources: %+v", st)
	}
	select {
	case e := <-sideEvents:
		t.Fatalf("ReleaseResources side emitted stale event: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

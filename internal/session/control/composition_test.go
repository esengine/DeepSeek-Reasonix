package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/sessionstore"
)

func testComposition(t *testing.T, c *Controller) {
	t.Helper()
	d := agentcomposition.Builtins()[1]
	b := &agentcomposition.Binding{Resolved: d, Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}}
	b.Seal()
	c.BindComposition(b)
	c.ConfigureCompositions(t.TempDir(), nil)
}

func TestCompositionSwitchAndFirstTurnShareAdmission(t *testing.T) {
	c, _, done := newOneTurnController(t, testutil.Turn{Text: "answer"})
	testComposition(t, c)
	entered, finish := make(chan struct{}), make(chan struct{})
	c.ConfigureCompositions(t.TempDir(), func(context.Context, string) error { close(entered); <-finish; return errors.New("candidate failed") })
	result := make(chan error, 1)
	go func() { result <- c.SelectAgentComposition(t.Context(), "builtin:general") }()
	<-entered
	if got := c.SubmitHTTPFormat("first prompt", ""); got != turnDroppedRotating {
		t.Fatalf("admission=%v", got)
	}
	close(finish)
	if err := <-result; err == nil {
		t.Fatal("build failure hidden")
	}
	if c.CompositionBinding().Started {
		t.Fatal("refused prompt locked old session")
	}
	c.Submit("accepted prompt")
	waitForDone(t, done)
	if err := c.SelectAgentComposition(t.Context(), "builtin:general"); !errors.Is(err, agentcomposition.ErrLocked) {
		t.Fatalf("started switch=%v", err)
	}
	if err := c.ClearSession(); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectAgentComposition(t.Context(), "builtin:general"); !errors.Is(err, agentcomposition.ErrLocked) {
		t.Fatalf("clear unlocked binding: %v", err)
	}
}

func TestCompositionQueueAdmissionSurvivesRestartAndDeletion(t *testing.T) {
	c, _, _ := newOneTurnController(t)
	testComposition(t, c)
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	r, err := c.EnqueueInbox(InboxRequest{Submit: "queued work"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteInboxItem(r.ItemID); err != nil {
		t.Fatal(err)
	}
	loaded, err := sessionstore.LoadSession(c.SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Composition().Started {
		t.Fatal("queue acceptance was not durable")
	}
	if _, err := c.BeginCompositionSwitch(); !errors.Is(err, agentcomposition.ErrLocked) {
		t.Fatalf("queue deletion unlocked=%v", err)
	}
	c.Close()
	if _, err := c.EnqueueInbox(InboxRequest{Submit: "stale controller"}); err == nil || errors.Is(err, sessioninbox.ErrEmpty) {
		t.Fatalf("closed enqueue=%v", err)
	}
}

func TestCompositionAdmissionRemainsLockedWhenExecutionFailsBeforeUserMessage(t *testing.T) {
	for _, synchronous := range []bool{false, true} {
		t.Run(map[bool]string{false: "async", true: "sync"}[synchronous], func(t *testing.T) {
			c, _, done := newOneTurnController(t)
			testComposition(t, c)
			failure := errors.New("checkpoint failed before user message")
			body := func(context.Context) error { return failure }
			if synchronous {
				if err := c.runSynchronousTurn(t.Context(), nil, body); !errors.Is(err, failure) {
					t.Fatal(err)
				}
			} else {
				if c.runGuarded(body) != turnStarted {
					t.Fatal("turn not admitted")
				}
				waitForDone(t, done)
			}
			if !c.CompositionBinding().Started {
				t.Fatal("accepted prompt unlocked after early execution failure")
			}
			saved, err := sessionstore.LoadSession(c.SessionPath())
			if err != nil || !saved.Composition().Started {
				t.Fatalf("admission was not durable: %v", err)
			}
		})
	}
}

func TestCompositionAdmissionPersistenceFailureRejectsExecutionAndReleasesGate(t *testing.T) {
	for _, synchronous := range []bool{false, true} {
		t.Run(map[bool]string{false: "async", true: "sync"}[synchronous], func(t *testing.T) {
			c, _, _ := newOneTurnController(t)
			testComposition(t, c)
			blocker := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(blocker, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			c.SetSessionPath(filepath.Join(blocker, "session.jsonl"))
			called := false
			body := func(context.Context) error { called = true; return nil }
			if synchronous {
				if err := c.runSynchronousTurn(t.Context(), nil, body); err == nil {
					t.Fatal("failed latch save accepted a synchronous prompt")
				}
			} else if !c.runGuarded(body).Refused() {
				t.Fatal("failed latch save accepted an asynchronous prompt")
			}
			if called || c.Running() || c.CompositionBinding().Started {
				t.Fatal("failed admission ran or changed the old conversation")
			}
			release, err := c.BeginCompositionSwitch()
			if err != nil {
				t.Fatalf("failed admission stranded the old gate: %v", err)
			}
			release()
		})
	}
}

func TestCompositionPendingAdmissionCannotAcceptParkedWork(t *testing.T) {
	c, _, _ := newOneTurnController(t)
	testComposition(t, c)
	c.mu.Lock()
	c.gate.begin(func() {})
	c.gate.bindingPending = true
	c.mu.Unlock()
	if got := c.runGuardedOrPark(func(context.Context) error { return nil }); got != turnDroppedRunning {
		t.Fatalf("pending binding admitted volatile work: %v", got)
	}
	c.mu.Lock()
	c.gate.end()
	c.mu.Unlock()
}

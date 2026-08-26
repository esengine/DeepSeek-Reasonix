package control

import (
	"errors"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/permission"
)

type failingPromptAnswerSink struct{ err error }

func (s failingPromptAnswerSink) Emit(event.Event) {}
func (s failingPromptAnswerSink) EmitChecked(e event.Event) error {
	if e.Kind == event.PromptAnswered {
		return s.err
	}
	return nil
}

func TestApprovalResolutionRemainsPendingWhenPersistenceFails(t *testing.T) {
	m := newApprovalManager(permission.Policy{}, ToolApprovalAsk, 0)
	id, reply := m.register("write_file", "settings.json", "test")
	want := errors.New("ledger unavailable")

	if _, ok, err := m.resolveAfter(id, func(p pendingApproval) error {
		if p.reply != reply {
			t.Fatal("persistence callback received a different pending approval")
		}
		return want
	}); !errors.Is(err, want) || ok {
		t.Fatalf("first resolve = ok:%v err:%v, want retryable persistence failure", ok, err)
	}
	if got := m.peek(id); got.reply != reply {
		t.Fatal("failed persistence removed the pending approval")
	}
	if got, ok, err := m.resolveAfter(id, nil); err != nil || !ok || got.reply != reply {
		t.Fatalf("retry resolve = pending:%+v ok:%v err:%v", got, ok, err)
	}
}

func TestAskResolutionRemainsPendingWhenPersistenceFails(t *testing.T) {
	m := newApprovalManager(permission.Policy{}, ToolApprovalAsk, 0)
	id, reply := m.registerAsk(askProbeQuestions())
	m.markAskEmitted(id)
	want := errors.New("ledger unavailable")

	if _, ok, err := m.resolveAskAfter(id, func(p pendingAsk) error {
		if p.reply != reply {
			t.Fatal("persistence callback received a different pending ask")
		}
		return want
	}); !errors.Is(err, want) || ok {
		t.Fatalf("first answer = ok:%v err:%v, want retryable persistence failure", ok, err)
	}
	if _, pending := m.snapshotPrompts(); len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("failed persistence removed ask %q: %+v", id, pending)
	}
	if got, ok, err := m.resolveAskAfter(id, nil); err != nil || !ok || got.reply != reply {
		t.Fatalf("retry answer = pending:%+v ok:%v err:%v", got, ok, err)
	}
}

func TestClearKindDropsResolutionReservation(t *testing.T) {
	m := newApprovalManager(permission.Policy{}, ToolApprovalAsk, 0)
	id, _ := m.registerDecisionKind("recovery", "", "", true, true, "recovery", nil)
	m.approvalResolutions[id] = newPromptResolution()

	m.clearKind("recovery")
	if _, ok := m.approvalResolutions[id]; ok {
		t.Fatal("clearKind left a stale two-phase resolution reservation")
	}
}

func TestConcurrentDuplicateAskWaitsForSamePersistenceResult(t *testing.T) {
	m := newApprovalManager(permission.Policy{}, ToolApprovalAsk, 0)
	id, _ := m.registerAsk(askProbeQuestions())
	m.markAskEmitted(id)
	want := errors.New("ledger unavailable")
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		_, _, err := m.resolveAskAfter(id, func(pendingAsk) error {
			close(entered)
			<-release
			return want
		})
		firstDone <- err
	}()
	<-entered
	m.mu.Lock()
	attempt := m.askResolutions[id]
	m.mu.Unlock()
	if attempt == nil {
		t.Fatal("first answer did not reserve the prompt")
	}
	go func() {
		_, _, err := m.resolveAskAfter(id, nil)
		secondDone <- err
	}()
	<-attempt.joined
	close(release)
	if err := <-firstDone; !errors.Is(err, want) {
		t.Fatalf("first answer error = %v, want %v", err, want)
	}
	if err := <-secondDone; !errors.Is(err, want) {
		t.Fatalf("duplicate answer error = %v, want same %v", err, want)
	}
	if _, pending := m.snapshotPrompts(); len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("failed duplicate transaction did not remain retryable: %+v", pending)
	}
}

func TestApproveCheckedReturnsPersistenceFailureWithoutReleasingTool(t *testing.T) {
	want := errors.New("ledger unavailable")
	c := &Controller{
		sink:     failingPromptAnswerSink{err: want},
		approval: newApprovalManager(permission.Policy{}, ToolApprovalAsk, 0),
	}
	id, reply := c.approval.register("bash", "write output", "test")
	if err := c.approveChecked(id, true, false, false); !errors.Is(err, want) {
		t.Fatalf("approveChecked error = %v, want %v", err, want)
	}
	if got := c.approval.peek(id); got.reply != reply {
		t.Fatal("failed answer persistence removed the pending approval")
	}
	select {
	case <-reply:
		t.Fatal("tool resumed before PromptAnswered was durable")
	default:
	}

	c.sink = event.Discard
	if err := c.approveChecked(id, true, false, false); err != nil {
		t.Fatalf("retry approveChecked: %v", err)
	}
	select {
	case resolved := <-reply:
		if !resolved.allow {
			t.Fatal("retry did not preserve approval outcome")
		}
	default:
		t.Fatal("durable retry did not release the tool")
	}
	if err := c.approveChecked(id, true, false, false); err != nil {
		t.Fatalf("duplicate approval was not idempotent: %v", err)
	}
}

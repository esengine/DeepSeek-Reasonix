package control

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/agent"
)

// RejectAskUnattended must resolve a pending ask with ErrAskUnattended — not
// the empty-selection path, which cancels the active turn and kills unattended
// runs (#8238).
func TestRejectAskUnattendedFailsAskWithoutCancellingTurn(t *testing.T) {
	sink := &askProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})

	askErr := make(chan error, 1)
	go func() {
		_, err := c.Ask(context.Background(), askProbeQuestions())
		askErr <- err
	}()

	deadline := time.After(2 * time.Second)
	for {
		if asks, _ := sink.counts(); asks == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("AskRequest never emitted")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	id := sink.lastAskID()

	c.RejectAskUnattended(id)

	select {
	case err := <-askErr:
		if err == nil || err.Error() != agent.ErrAskUnattended.Error() {
			t.Fatalf("Ask error = %v, want ErrAskUnattended", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not resolve after RejectAskUnattended")
	}

	// No turn was ever started, so there is nothing to cancel; the point is
	// that RejectAskUnattended itself never arms cancellation — contrast
	// AnswerQuestion with no selections, which calls Cancel on active turns.
	if c.cancel != nil {
		t.Fatal("RejectAskUnattended armed turn cancellation")
	}
}

// The interactive dismiss contract (#6869) is unchanged: an empty-selection
// AnswerQuestion on an active turn still cancels it.
func TestEmptyAnswerStillCancelsActiveTurn(t *testing.T) {
	sink := &askProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})
	cancelled := make(chan struct{}, 1)
	ctx, cancelAsk := context.WithCancel(context.Background())
	defer cancelAsk()
	c.mu.Lock()
	c.cancel = func() {
		cancelled <- struct{}{}
		cancelAsk()
	}
	c.mu.Unlock()

	askErr := make(chan error, 1)
	go func() {
		_, err := c.Ask(ctx, askProbeQuestions())
		askErr <- err
	}()

	deadline := time.After(2 * time.Second)
	for {
		if asks, _ := sink.counts(); asks == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("AskRequest never emitted")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	id := sink.lastAskID()

	c.AnswerQuestion(id, nil)

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("empty-selection answer no longer cancels the active turn")
	}
	<-askErr
}

func (s *askProbeSink) lastAskID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.asks) == 0 {
		return ""
	}
	return s.asks[len(s.asks)-1].ID
}

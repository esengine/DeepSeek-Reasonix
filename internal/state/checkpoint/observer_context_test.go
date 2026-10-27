package checkpoint

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

func TestCancelledObserverAdmissionKeepsDurableUndo(t *testing.T) {
	s, dir, root, _, previous := committedCodeUndo(t)
	synctest.Test(t, func(t *testing.T) {
		if err := s.Barrier().EnterExclusive(); err != nil {
			t.Fatal(err)
		}
		observer := NewMutationObserver(ObserverOptions{Store: s})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			release, err := observer.BeginWriteContext(ctx)
			if release != nil {
				release()
			}
			done <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("preparation error = %v", err)
			}
		default:
			t.Error("observer did not leave cancelled wait")
			s.Barrier().ExitExclusive()
			<-done
			return
		}
		if s.Barrier().HasWriters() || !s.Barrier().Busy() {
			t.Error("observer changed the exclusive owner's state")
		}
		s.Barrier().ExitExclusive()
	})
	requireUndoOffer(t, s, previous.TransactionID)
	requireUndoOffer(t, New(dir, root), previous.TransactionID)
}

func TestObserverCancellationAfterDurablePreparationDoesNotReviveUndo(t *testing.T) {
	s, dir, root, _, _ := committedCodeUndo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.OnUndoChange(cancel)
	release, err := NewMutationObserver(ObserverOptions{Store: s}).BeginWriteContext(ctx)
	if release != nil {
		release()
		t.Error("cancelled preparation returned a writer permit")
	}
	if !errors.Is(err, context.Canceled) || s.Barrier().Busy() {
		t.Fatalf("post-preparation cancellation: err=%v, busy=%v", err, s.Barrier().Busy())
	}
	if undo, err := New(dir, root).AvailableUndo(); err != nil || undo != nil {
		t.Fatalf("cancelled preparation revived retired undo: %+v %v", undo, err)
	}
}

func TestObserverReleaseDoesNotReleaseAnotherWriter(t *testing.T) {
	s := New("", t.TempDir())
	o := NewMutationObserver(ObserverOptions{Store: s})
	first, err := o.BeginWrite()
	if err != nil {
		t.Fatal(err)
	}
	second, err := o.BeginWrite()
	if err != nil {
		first()
		t.Fatal(err)
	}
	defer second()
	first()
	generation := s.Barrier().Generation()
	first()
	if !s.Barrier().HasWriters() || s.Barrier().Generation() != generation {
		t.Fatal("repeated cleanup released another writer")
	}
	if s.Barrier().TryEnterExclusive() {
		s.Barrier().ExitExclusive()
		t.Fatal("rewind admitted while second writer was active")
	}
	second()
	if s.Barrier().Busy() {
		t.Fatal("final writer cleanup leaked admission")
	}
}

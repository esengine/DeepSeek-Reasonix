package jobs

import (
	"context"
	"errors"
	"io"
	"testing"

	"reasonix/internal/contract/event"
)

func TestSealJoinsAdmittedJobsAndRejectsLateStarts(t *testing.T) {
	m := NewManager(event.Discard)
	t.Cleanup(m.Close)
	entered, release := make(chan struct{}), make(chan struct{})
	_, err := m.StartOwnedForSession("owner", "task", "admitted", func(context.Context, io.Writer) (string, error) {
		close(entered)
		<-release
		return "done", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	done := m.Seal()
	select {
	case <-done:
		t.Fatal("sealed owner finished before admitted job exited")
	default:
	}
	_, err = m.StartOwnedForSession("another-session", "task", "late", func(context.Context, io.Writer) (string, error) {
		t.Error("sealed manager executed a late job")
		return "", nil
	})
	if !errors.Is(err, ErrAdmissionClosed) {
		t.Fatalf("late admission: %v", err)
	}
	if release, _, ok := m.ReserveStartForSession("owner", "task", 0); ok {
		release()
		t.Fatal("unlimited reservation bypassed sealed admission")
	}
	if m.Seal() != done {
		t.Fatal("seal changed join identity")
	}
	close(release)
	<-done
}

func TestSealAndWaitCancellationRetainsOwnerUntilPhysicalReturn(t *testing.T) {
	m := NewManager(event.Discard)
	t.Cleanup(m.Close)
	cancelled, release := make(chan struct{}), make(chan struct{})
	_, err := m.StartOwnedForSession("owner", "task", "non-cooperative", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		close(cancelled)
		<-release
		return "", ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	joined := make(chan error, 1)
	go func() { joined <- m.SealAndWait(ctx) }()
	cancel()
	<-cancelled
	select {
	case err := <-joined:
		t.Fatalf("cancel released a live owner: %v", err)
	default:
	}
	close(release)
	if err := <-joined; !errors.Is(err, context.Canceled) {
		t.Fatalf("join cancellation: %v", err)
	}
}

func TestDestroyRejectsLateAdmissionWithoutExecuting(t *testing.T) {
	m := NewManager(event.Discard)
	t.Cleanup(m.Close)
	m.BeginDestroySession("owner")
	_, err := m.StartOwnedForSession("owner", "task", "late", func(context.Context, io.Writer) (string, error) {
		t.Error("destroyed session executed a job")
		return "", nil
	})
	if !errors.Is(err, ErrAdmissionClosed) {
		t.Fatalf("destroy admission: %v", err)
	}
}

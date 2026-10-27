package checkpoint

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"testing/synctest"
	"time"
)

func TestWriterRegistrationDoesNotHoldRegistryDuringUndoCallback(t *testing.T) {
	s, _, _, _, _ := committedCodeUndo(t)
	o := NewMutationObserver(ObserverOptions{Store: s})
	queried := false
	s.OnUndoChange(func() {
		if !o.reg.mu.TryLock() {
			t.Error("undo callback cannot query shared writer registry")
			return
		}
		o.reg.mu.Unlock()
		_ = o.ActiveWriters()
		queried = true
	})
	if err := o.RegisterWriter("writer", "background", 1); err != nil {
		t.Fatal(err)
	}
	defer o.UnregisterWriter("writer")
	if !queried {
		t.Error("undo change callback was not observed")
	}
}

func TestWriterRegistrationWaitCanBeCancelledWithoutExclusiveRelease(t *testing.T) {
	s := New("", t.TempDir())
	o := NewMutationObserver(ObserverOptions{Store: s})
	synctest.Test(t, func(t *testing.T) {
		if err := s.Barrier().EnterExclusive(); err != nil {
			t.Fatal(err)
		}
		defer s.Barrier().ExitExclusive()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := o.RegisterWriterContext(ctx, "waiting", "background", 1)
			done <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("cancelled writer registration: %v", err)
			}
		default:
			t.Error("writer registration ignored cancellation")
			s.Barrier().ExitExclusive()
			<-done
			return
		}
		if s.Barrier().HasWriters() || !s.Barrier().Busy() || len(o.ActiveWriters()) != 0 {
			t.Error("cancelled registration changed the active exclusive owner")
		}
	})
}

func TestUnregisterPendingWriterDoesNotBlockOrResurrect(t *testing.T) {
	s := New("", t.TempDir())
	o := NewMutationObserver(ObserverOptions{Store: s})
	synctest.Test(t, func(t *testing.T) {
		if err := s.Barrier().EnterExclusive(); err != nil {
			t.Fatal(err)
		}
		defer s.Barrier().ExitExclusive()
		registered, removed := make(chan error, 1), make(chan struct{})
		go func() { registered <- o.RegisterWriter("retired", "background", 1) }()
		synctest.Wait()
		go func() { o.UnregisterWriter("retired"); close(removed) }()
		synctest.Wait()
		select {
		case <-removed:
		default:
			t.Error("unregister waited behind registration's barrier wait")
			s.Barrier().ExitExclusive()
			<-removed
			<-registered
			return
		}
		if err := <-registered; !errors.Is(err, context.Canceled) {
			t.Errorf("retired registration resurrected: %v", err)
		}
		if len(o.ActiveWriters()) != 0 || s.Barrier().HasWriters() {
			t.Error("retired pending writer retained resources")
		}
	})
}

func TestWriterRegistrationFollowerCancellationDoesNotCancelLeader(t *testing.T) {
	s := New("", t.TempDir())
	o := NewMutationObserver(ObserverOptions{Store: s})
	synctest.Test(t, func(t *testing.T) {
		if err := s.Barrier().EnterExclusive(); err != nil {
			t.Fatal(err)
		}
		leader := make(chan error, 1)
		go func() { leader <- o.RegisterWriter("shared", "background", 1) }()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		follower := make(chan error, 1)
		go func() {
			_, err := o.RegisterWriterContext(ctx, "shared", "background", 1)
			follower <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-follower; !errors.Is(err, context.Canceled) {
			t.Errorf("follower cancellation = %v", err)
		}
		s.Barrier().ExitExclusive()
		if err := <-leader; err != nil {
			t.Fatal(err)
		}
		release, err := o.RegisterWriterContext(t.Context(), "shared", "duplicate", 2)
		if err != nil {
			t.Fatal(err)
		}
		if writers := o.ActiveWriters(); len(writers) != 1 || writers[0].Kind != "background" {
			t.Errorf("duplicate writer changed leader: %+v", writers)
		}
		release()
		if s.Barrier().Busy() {
			t.Error("duplicate admission acquired another permit")
		}
	})
}

func TestWriterRegistrationOldCleanupCannotReleaseNewGeneration(t *testing.T) {
	s := New("", t.TempDir())
	o := NewMutationObserver(ObserverOptions{Store: s})
	ctx, cancel := context.WithCancel(t.Context())
	first, err := o.RegisterWriterContext(ctx, "reused", "background", 1)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if !s.Barrier().HasWriters() {
		t.Fatal("admission cancellation released an active background job")
	}
	first()
	second, err := o.RegisterWriterContext(t.Context(), "reused", "background", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	first()
	if writers := o.ActiveWriters(); !s.Barrier().HasWriters() || len(writers) != 1 || writers[0].Turn != 2 {
		t.Errorf("old cleanup retired a new generation: %+v", writers)
	}
}

func TestWriterRegistrationRetiredPreparationCannotRemoveReplacement(t *testing.T) {
	s, _, _, _, _ := committedCodeUndo(t)
	o := NewMutationObserver(ObserverOptions{Store: s})
	synctest.Test(t, func(t *testing.T) {
		entered, finish := make(chan struct{}), make(chan struct{})
		s.OnUndoChange(func() { close(entered); <-finish })
		done := make(chan error, 1)
		go func() { done <- o.RegisterWriter("reused", "old", 1) }()
		<-entered
		o.UnregisterWriter("reused")
		release, err := o.RegisterWriterContext(t.Context(), "reused", "new", 2)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		close(finish)
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("retired preparation error = %v", err)
		}
		if writers := o.ActiveWriters(); len(writers) != 1 || writers[0].Kind != "new" || !s.Barrier().HasWriters() {
			t.Errorf("old completion changed replacement: %+v", writers)
		}
	})
}

func TestWriterRegistrationCancelledAfterRetirementKeepsUndoRetired(t *testing.T) {
	s, dir, root, _, _ := committedCodeUndo(t)
	o := NewMutationObserver(ObserverOptions{Store: s})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.OnUndoChange(cancel)
	release, err := o.RegisterWriterContext(ctx, "cancelled", "background", 1)
	if release != nil || !errors.Is(err, context.Canceled) || s.Barrier().Busy() || len(o.ActiveWriters()) != 0 {
		t.Fatalf("post-retirement admission: cleanup=%t err=%v", release != nil, err)
	}
	if undo, err := New(dir, root).AvailableUndo(); err != nil || undo != nil {
		t.Fatalf("cancelled admission revived durable undo: %+v %v", undo, err)
	}
}

func TestWriterRegistrationPanicClosesFollowersAndPermit(t *testing.T) {
	s, _, _, _, _ := committedCodeUndo(t)
	o := NewMutationObserver(ObserverOptions{Store: s})
	synctest.Test(t, func(t *testing.T) {
		entered, fail := make(chan struct{}), make(chan struct{})
		s.OnUndoChange(func() { close(entered); <-fail; panic("callback panic") })
		panicked := make(chan any, 1)
		go func() {
			defer func() { panicked <- recover() }()
			_ = o.RegisterWriter("panic", "background", 1)
		}()
		<-entered
		follower := make(chan error, 1)
		go func() { follower <- o.RegisterWriter("panic", "background", 1) }()
		synctest.Wait()
		close(fail)
		if p := <-panicked; p != "callback panic" {
			t.Errorf("original panic = %v", p)
		}
		if err := <-follower; err == nil {
			t.Error("follower accepted a panicked preparation")
		}
		if s.Barrier().Busy() || len(o.ActiveWriters()) != 0 {
			t.Error("panic leaked writer resources")
		}
	})
}

func TestWriterRegistrationRegistryAvailableDuringStorePublication(t *testing.T) {
	s := New("", t.TempDir())
	o := NewMutationObserver(ObserverOptions{Store: s})
	first, err := o.RegisterWriterContext(t.Context(), "first", "background", 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := o.RegisterWriterContext(t.Context(), "second", "background", 1)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	removed, finished := make(chan struct{}), make(chan struct{})
	go func() { first(); close(removed) }()
	waitRegistry := func(count int) {
		t.Helper()
		watchdog := time.NewTimer(10 * time.Second)
		defer watchdog.Stop()
		for {
			if o.reg.mu.TryLock() {
				n := len(o.snapshotWritersLocked())
				o.reg.mu.Unlock()
				if n == count {
					return
				}
			}
			select {
			case <-watchdog.C:
				s.mu.Unlock()
				<-removed
				second()
				t.Fatal("registry unavailable while store publication is parked")
			default:
				runtime.Gosched()
			}
		}
	}
	waitRegistry(1)
	go func() { second(); close(finished) }()
	waitRegistry(0)
	s.mu.Unlock()
	<-removed
	<-finished
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeWriters) != 0 || s.Barrier().Busy() {
		t.Errorf("publication overtook newer removal: %+v", s.activeWriters)
	}
}

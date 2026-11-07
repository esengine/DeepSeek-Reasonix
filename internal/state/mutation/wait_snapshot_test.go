package mutation

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestWaitSnapshotCoalescesConcurrentReadersAndExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var pool waitSnapshotPool
		var reads atomic.Int32
		ready := make(chan struct{})
		read := func(context.Context) ([]record, error) {
			reads.Add(1)
			<-ready
			return []record{{ID: "holder"}}, nil
		}
		done := make(chan error, 32)
		for range 32 {
			go func() {
				r, err := pool.get(t.Context(), "registry", read)
				if err == nil && (len(r) != 1 || r[0].ID != "holder") {
					err = fmt.Errorf("unexpected records: %+v", r)
				}
				done <- err
			}()
		}
		synctest.Wait()
		if reads.Load() != 1 {
			t.Fatalf("concurrent readers performed %d scans", reads.Load())
		}
		close(ready)
		for range 32 {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.get(t.Context(), "registry", read); err != nil || reads.Load() != 1 {
			t.Fatalf("completed snapshot was not reused: %d %v", reads.Load(), err)
		}
		time.Sleep(retryInterval)
		if _, err := pool.get(t.Context(), "registry", read); err != nil || reads.Load() != 2 {
			t.Fatalf("expired snapshot was reused: %d %v", reads.Load(), err)
		}
	})
}

func TestWaitSnapshotCancelledFollowerDoesNotCancelReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var pool waitSnapshotPool
		finish, loaded := make(chan struct{}), make(chan error, 1)
		go func() {
			_, err := pool.get(t.Context(), "registry", func(context.Context) ([]record, error) {
				<-finish
				return []record{{ID: "intact"}}, nil
			})
			loaded <- err
		}()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		cancelled := make(chan error, 1)
		go func() {
			_, err := pool.get(ctx, "registry", func(context.Context) ([]record, error) {
				t.Error("follower became a second loader")
				return nil, nil
			})
			cancelled <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-cancelled; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		close(finish)
		if err := <-loaded; err != nil {
			t.Fatal(err)
		}
		r, err := pool.get(t.Context(), "registry", nil)
		if err != nil || len(r) != 1 || r[0].ID != "intact" {
			t.Fatalf("follower cancelled another owner: %+v %v", r, err)
		}
	})
}

func TestWaitSnapshotCancelledLoaderIsNotAnotherCallersError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var pool waitSnapshotPool
		ctx, cancel := context.WithCancel(t.Context())
		leader, follower := make(chan error, 1), make(chan error, 1)
		go func() {
			_, err := pool.get(ctx, "registry", func(ctx context.Context) ([]record, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
			leader <- err
		}()
		synctest.Wait()
		go func() {
			_, err := pool.get(t.Context(), "registry", func(context.Context) ([]record, error) {
				return []record{{ID: "retry"}}, nil
			})
			follower <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-leader; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := <-follower; err != nil {
			t.Fatalf("one caller's cancellation escaped into its peer: %v", err)
		}
	})
}

func TestWaitSnapshotCancellationWinsACompletedRead(t *testing.T) {
	var pool waitSnapshotPool
	ctx, cancel := context.WithCancel(t.Context())
	_, err := pool.get(ctx, "registry", func(context.Context) ([]record, error) {
		cancel()
		return []record{{ID: "completed"}}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled reader reported success: %v", err)
	}
}

func TestWaitSnapshotReaderPanicClosesAllWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var pool waitSnapshotPool
		finish := make(chan struct{})
		done := make(chan error, 8)
		for range 8 {
			go func() {
				_, err := pool.get(t.Context(), "registry", func(context.Context) ([]record, error) {
					<-finish
					panic("reader failed")
				})
				done <- err
			}()
		}
		synctest.Wait()
		close(finish)
		for range 8 {
			if err := <-done; !errors.Is(err, ErrCallback) {
				t.Fatalf("reader panic did not close with an error: %v", err)
			}
		}
	})
}

func TestWaitSnapshotPoolBoundsInFlightAndCompletedNamespaces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var pool waitSnapshotPool
		finish, done := make(chan struct{}), make(chan error, waitSnapshotLimit)
		for i := range waitSnapshotLimit {
			go func() {
				_, err := pool.get(t.Context(), fmt.Sprint(i), func(context.Context) ([]record, error) {
					<-finish
					return nil, nil
				})
				done <- err
			}()
		}
		synctest.Wait()
		var reads int
		read := func(context.Context) ([]record, error) { reads++; return nil, nil }
		if _, err := pool.get(t.Context(), "overflow", read); err != nil || reads != 1 {
			t.Fatalf("full in-flight table did not fall back to a real read: %d %v", reads, err)
		}
		if len(pool.entries) != waitSnapshotLimit {
			t.Fatalf("in-flight table grew: %d", len(pool.entries))
		}
		close(finish)
		for range waitSnapshotLimit {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		for i := range 2 * waitSnapshotLimit {
			if _, err := pool.get(t.Context(), fmt.Sprint("extra", i), read); err != nil {
				t.Fatal(err)
			}
		}
		if len(pool.entries) != waitSnapshotLimit {
			t.Fatalf("completed table grew: %d", len(pool.entries))
		}
	})
}

func TestWaitSnapshotOldReaderCannotForgetReplacement(t *testing.T) {
	var pool waitSnapshotPool
	old, _ := pool.entry("registry")
	old.expires = time.Now().Add(-time.Second)
	close(old.ready)
	current, _ := pool.entry("registry")
	pool.forget("registry", old)
	if actual, load := pool.entry("registry"); actual != current || load {
		t.Fatal("old cancellation removed a new observation")
	}
	close(current.ready)
}

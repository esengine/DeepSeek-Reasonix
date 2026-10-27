package checkpoint

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestBarrierCancelledWaitPreservesOwner(t *testing.T) {
	for _, owner := range []string{"writer", "exclusive"} {
		for _, waiter := range []string{"writer", "exclusive"} {
			if owner == "writer" && waiter == "writer" {
				continue
			}
			t.Run(owner+"/"+waiter, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					b := NewMutationBarrier()
					release := b.ExitWrite
					if owner == "writer" {
						if err := b.EnterWrite(); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := b.EnterExclusive(); err != nil {
							t.Fatal(err)
						}
						release = b.ExitExclusive
					}
					generation := b.Generation()
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					done := make(chan error, 1)
					go func() {
						if waiter == "writer" {
							done <- b.EnterWriteContext(ctx)
						} else {
							done <- b.EnterExclusiveContext(ctx)
						}
					}()
					synctest.Wait()
					cancel()
					synctest.Wait()
					select {
					case err := <-done:
						if !errors.Is(err, context.Canceled) {
							t.Errorf("wait error = %v", err)
						}
					default:
						t.Error("cancellation did not wake waiter")
						release()
						<-done
						return
					}
					if !b.Busy() || b.Generation() != generation || b.HasWriters() != (owner == "writer") {
						t.Error("cancelled waiter changed the owner's state")
					}
					if b.TryEnterExclusive() {
						t.Error("cancelled waiter released the owner's admission")
						b.ExitExclusive()
					}
					release()
					if !b.TryEnterExclusive() {
						t.Fatal("cancelled waiter left an admission behind")
					}
					b.ExitExclusive()
				})
			})
		}
	}
}

func TestBarrierContextRefusesCancelledFreeAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, exclusive := range []bool{false, true} {
		b := NewMutationBarrier()
		var err error
		if exclusive {
			err = b.EnterExclusiveContext(ctx)
		} else {
			err = b.EnterWriteContext(ctx)
		}
		if !errors.Is(err, context.Canceled) || b.Busy() || b.Generation() != 0 {
			t.Fatalf("cancelled free admission: err=%v, busy=%v, generation=%d", err, b.Busy(), b.Generation())
		}
	}
}

func TestBarrierContextDeadlineAndCloseWakeWaiters(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		for _, closeBarrier := range []bool{false, true} {
			synctest.Test(t, func(t *testing.T) {
				b := NewMutationBarrier()
				if err := b.EnterExclusive(); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					if exclusive {
						done <- b.EnterExclusiveContext(ctx)
					} else {
						done <- b.EnterWriteContext(ctx)
					}
				}()
				synctest.Wait()
				if closeBarrier {
					b.Close()
				} else {
					time.Sleep(time.Hour)
				}
				err := <-done
				if err == nil || (!closeBarrier && !errors.Is(err, context.DeadlineExceeded)) {
					t.Errorf("wait error = %v", err)
				}
				if !b.Busy() || b.HasWriters() || b.Generation() != 0 {
					t.Error("abandoned wait changed the exclusive owner")
				}
				b.ExitExclusive()
				if closeBarrier && (b.TryEnterWrite() || b.TryEnterExclusive()) {
					t.Error("closed barrier admitted a successor")
				}
			})
		}
	}
}

func TestBarrierContextLiveWaitersAndLegacyEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := NewMutationBarrier()
		if err := b.EnterExclusive(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 2)
		go func() { done <- b.EnterWriteContext(context.Background()) }()
		go func() { done <- b.EnterWrite() }()
		synctest.Wait()
		b.ExitExclusive()
		if first, second := <-done, <-done; first != nil || second != nil {
			t.Fatalf("live writers failed: %v %v", first, second)
		}
		b.ExitWrite()
		if !b.HasWriters() || b.TryEnterExclusive() {
			t.Fatal("one writer's release removed another writer")
		}
		b.ExitWrite()
		if err := b.EnterExclusiveContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		b.ExitExclusive()
	})
}

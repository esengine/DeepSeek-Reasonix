package workspacelease

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func prepareCall(t *testing.T, o *Owner, paths []string) (context.Context, *PreparedCall) {
	t.Helper()
	ctx, p, err := o.PreparePaths(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		o.mu.Lock()
		done := p.hold.done
		o.mu.Unlock()
		if done {
			return
		}
		if err := p.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return ctx, p
}

func requireClaim(t *testing.T, o *Owner, paths []string, busy bool) {
	t.Helper()
	err := within(t, o, paths, 100*time.Millisecond)
	if busy && err == nil {
		t.Fatalf("claimed protected extent %v", paths)
	}
	if !busy && err != nil {
		t.Fatalf("unexecuted extent %v stayed claimed: %v", paths, err)
	}
}

func TestPreparedCallAbortsOnlyItsUnexecutedExtent(t *testing.T) {
	for _, wide := range []bool{false, true} {
		t.Run(map[bool]string{false: "named", true: "whole"}[wide], func(t *testing.T) {
			a, b, root := yieldPair(t)
			a.BeginRun()
			b.BeginRun()
			old := []string{filepath.Join(root, "written")}
			if err := a.AcquirePaths(t.Context(), old); err != nil {
				t.Fatal(err)
			}
			paths := []string{filepath.Join(root, "refused")}
			if wide {
				paths = nil
			}
			_, p := prepareCall(t, a, paths)
			if err := p.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			requireClaim(t, b, []string{filepath.Join(root, "refused")}, false)
			requireClaim(t, b, old, true)
		})
	}
}

func TestPreparedCallsPreserveOverlappingPeersAndParallelRetention(t *testing.T) {
	a, b, root := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	x, y := []string{filepath.Join(root, "x")}, []string{filepath.Join(root, "y")}
	_, first := prepareCall(t, a, x)
	_, second := prepareCall(t, a, x)
	_, peer := prepareCall(t, a, y)
	var wg sync.WaitGroup
	wg.Go(func() { peer.Retain() })
	wg.Go(func() {
		if err := first.Close(t.Context()); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	requireClaim(t, b, x, true)
	if err := second.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, x, false)
	if err := peer.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, y, true)
}

func TestExplicitAcquirePromotesAPathAlreadyCoveredByPreparation(t *testing.T) {
	a, b, root := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	path := []string{filepath.Join(root, "retained")}
	_, p := prepareCall(t, a, path)
	if err := a.AcquirePaths(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, path, true)
}

func TestPreparedCallCannotReleaseANewerRun(t *testing.T) {
	a, b, root := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	path := []string{filepath.Join(root, "same")}
	_, old := prepareCall(t, a, path)
	a.EndRun()
	requireClaim(t, b, path, true)
	if err := old.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	a.BeginRun()
	_, current := prepareCall(t, a, path)
	old.Retain()
	if err := old.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, path, true)
	if err := current.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, path, false)
}

func TestPreparedCallYieldDoesNotPromoteOrResurrectAnAbortedClaim(t *testing.T) {
	for _, abortWhileYielded := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume_then_abort", true: "abort_then_resume"}[abortWhileYielded], func(t *testing.T) {
			a, b, root := yieldPair(t)
			a.BeginRun()
			b.BeginRun()
			paths := []string{filepath.Join(root, "pending")}
			ctx, p := prepareCall(t, a, paths)
			resume := a.Yield(ctx)
			requireClaim(t, b, paths, false)
			b.EndRun()
			if abortWhileYielded {
				if err := p.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := resume(t.Context()); abortWhileYielded && !errors.Is(err, ErrHoldEnded) || !abortWhileYielded && err != nil {
				t.Fatalf("resume after abort=%v: %v", abortWhileYielded, err)
			}
			if err := p.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			b.BeginRun()
			requireClaim(t, b, paths, false)
			if a.State().Acquired {
				t.Fatal("resume retained an aborted preparation")
			}
		})
	}
}

func TestPreparedCleanupFailurePreservesThePublishedExtent(t *testing.T) {
	a, b, root := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	x, y := []string{filepath.Join(root, "x")}, []string{filepath.Join(root, "y")}
	if err := a.AcquirePaths(t.Context(), x); err != nil {
		t.Fatal(err)
	}
	_, p := prepareCall(t, a, y)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup failure = %v", err)
	}
	if err := p.Close(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("repeated cleanup hid the original failure: %v", err)
	}
	requireClaim(t, b, x, true)
	requireClaim(t, b, y, true)
	a.EndRun()
	requireClaim(t, b, y, false)
}

func TestPreparedWholeAndEmptyHaveDifferentLifetimes(t *testing.T) {
	a, b, root := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	_, p := prepareCall(t, a, nil)
	requireClaim(t, b, []string{filepath.Join(root, "anything")}, true)
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, nil, false)
}

func TestPreparedCallFailedUpgradeDoesNotPublishAPartialSet(t *testing.T) {
	a, b, root := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	if err := a.AcquirePaths(t.Context(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := b.AcquirePaths(t.Context(), []string{"b"}); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		owner *Owner
		paths []string
	}{
		{a, []string{"free", "b"}}, {b, []string{"other", "a"}},
	} {
		if _, p, err := pair.owner.PreparePaths(t.Context(), pair.paths); !errors.Is(err, ErrConflict) || p != nil {
			t.Fatalf("opposite-order upgrade = %v, %v", p, err)
		}
	}
	c, err := New(root, filepath.Dir(a.lockPath), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.BeginRun()
	defer c.EndRun()
	requireClaim(t, c, []string{"free", "other"}, false)
}

func TestPreparedProbeProcess(t *testing.T) {
	if os.Getenv("REASONIX_PREPARED_PROBE") != "1" {
		return
	}
	o, err := New(os.Getenv("REASONIX_PROBE_ROOT"), os.Getenv("REASONIX_PROBE_LOCKS"), nil)
	if err != nil {
		t.Fatal(err)
	}
	o.BeginRun()
	defer o.EndRun()
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	err = o.AcquirePaths(ctx, []string{os.Getenv("REASONIX_PROBE_PATH")})
	wantBusy := os.Getenv("REASONIX_PROBE_BUSY") == "true"
	if wantBusy && !errors.Is(err, context.DeadlineExceeded) || !wantBusy && err != nil {
		t.Fatalf("cross-process acquisition = %v, wantBusy=%v", err, wantBusy)
	}
}

func TestPreparedCleanupPublishesToOtherProcesses(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	a, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.BeginRun()
	defer a.EndRun()
	if err := a.AcquirePaths(t.Context(), []string{"written"}); err != nil {
		t.Fatal(err)
	}
	_, p := prepareCall(t, a, []string{"pending"})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probe := func(path, busy string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestPreparedProbeProcess$")
		cmd.Env = append(os.Environ(), "REASONIX_PREPARED_PROBE=1", "REASONIX_PROBE_ROOT="+root,
			"REASONIX_PROBE_LOCKS="+locks, "REASONIX_PROBE_PATH="+path, "REASONIX_PROBE_BUSY="+busy)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("probe %s: %s, %v", path, output, err)
		}
	}
	probe("pending", "true")
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	probe("pending", "false")
	probe("written", "true")
}

func TestPreparedCleanupSerializesWithAParallelUpgrade(t *testing.T) {
	a, b, root := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	_, p := prepareCall(t, a, []string{"aborted"})
	entered, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	a.SetHolder(func() string { once.Do(func() { close(entered); <-unblock }); return "upgrading" })
	upgraded := make(chan error, 1)
	go func() { upgraded <- a.AcquirePaths(t.Context(), []string{"committed"}) }()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- p.Close(t.Context()) }()
	close(unblock)
	if err := <-upgraded; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, []string{filepath.Join(root, "aborted")}, false)
	requireClaim(t, b, []string{filepath.Join(root, "committed")}, true)
}

func TestPreparedCancelledCleanupCannotLeakAnInFlightCall(t *testing.T) {
	a, b, _ := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	_, p := prepareCall(t, a, []string{"old"})
	entered, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	a.SetHolder(func() string { once.Do(func() { close(entered); <-unblock }); return "upgrade" })
	upgraded := make(chan error, 1)
	go func() { upgraded <- a.AcquirePaths(t.Context(), []string{"new"}) }()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cleanup := p.Close(ctx)
	close(unblock)
	if err := <-upgraded; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(cleanup, context.Canceled) {
		t.Fatalf("cleanup=%v", cleanup)
	}
	a.EndRun()
	requireClaim(t, b, []string{"old", "new"}, false)
}

func TestPreparedCancelledAcquisitionLeavesNoContribution(t *testing.T) {
	a, b, _ := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	if err := a.AcquirePaths(t.Context(), []string{"held"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, p, err := b.PreparePaths(ctx, []string{"free", "held"}); p != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("acquisition=%v,%v", p, err)
	}
	a.EndRun()
	requireClaim(t, a, []string{"free", "held"}, false)
}

func TestPreparedPublicationIOFailureRetainsUntilRunEnd(t *testing.T) {
	a, b, _ := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	if err := a.AcquirePaths(t.Context(), []string{"written"}); err != nil {
		t.Fatal(err)
	}
	_, p := prepareCall(t, a, []string{"pending"})
	if err := os.Remove(a.scope.record); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a.scope.record, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(t.Context()); err == nil {
		t.Fatal("claim publication failure reported success")
	}
	requireClaim(t, b, []string{"written", "pending"}, true)
	a.EndRun()
	requireClaim(t, b, []string{"written", "pending"}, false)
}

func TestPreparedYieldedRunCannotResumeOverANewerRun(t *testing.T) {
	a, b, _ := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	ctx, old := prepareCall(t, a, []string{"old"})
	resume := a.Yield(ctx)
	a.EndRun()
	a.BeginRun()
	_, current := prepareCall(t, a, []string{"current"})
	for range 2 {
		if _, err := resume(t.Context()); !errors.Is(err, ErrHoldEnded) {
			t.Fatalf("expired resume did not fail closed: %v", err)
		}
	}
	old.Retain()
	if err := old.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, []string{"old"}, false)
	requireClaim(t, b, []string{"current"}, true)
	if err := current.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, b, []string{"current"}, false)
}

func TestPreparedRepeatedWritesDoNotInflateThePublishedExtent(t *testing.T) {
	a, _, _ := yieldPair(t)
	a.BeginRun()
	for range 500 {
		_, p, err := a.PreparePaths(t.Context(), []string{"same"})
		if err != nil {
			t.Fatal(err)
		}
		p.Retain()
		if err := p.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	_, p := prepareCall(t, a, []string{"aborted"})
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(a.scope.record)
	if err != nil {
		t.Fatal(err)
	}
	var record pathLeaseRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Paths) != 1 {
		t.Fatalf("500 calls inflated one retained resource to %d entries", len(record.Paths))
	}
}

func TestRepeatedResumePreservesAFailedRestoration(t *testing.T) {
	a, b, _ := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	if err := a.AcquirePaths(t.Context(), []string{"held"}); err != nil {
		t.Fatal(err)
	}
	resume := a.Yield(t.Context())
	if err := b.AcquirePaths(t.Context(), []string{"held"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range 2 {
		if _, err := resume(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("repeated restoration lost its failure: %v", err)
		}
	}
}

func TestPreparedCallClosedDuringReholdCannotReportSuccessfulRestoration(t *testing.T) {
	withWaitGrace(t, time.Nanosecond)
	root, locks := t.TempDir(), t.TempDir()
	waiting := make(chan struct{})
	var once sync.Once
	a, err := New(root, locks, func(w Wait) {
		if w.Outcome == WaitBegan {
			once.Do(func() { close(waiting) })
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.BeginRun()
	b.BeginRun()
	defer a.EndRun()
	defer b.EndRun()
	ctx, p := prepareCall(t, a, []string{"held"})
	resume := a.Yield(ctx)
	last := a.Yield(t.Context())
	if err := b.AcquirePaths(t.Context(), []string{"held"}); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	resumed := make(chan error, 1)
	go func() { _, err := resume(bounded); resumed <- err }()
	select {
	case <-waiting:
	case <-bounded.Done():
		t.Fatal("rehold never waited")
	}
	closedCtx, stop := context.WithCancel(t.Context())
	stop()
	if err := p.Close(closedCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup=%v", err)
	}
	b.EndRun()
	if err := <-resumed; !errors.Is(err, ErrHoldEnded) {
		t.Fatalf("closed rehold reported success: %v", err)
	}
	if _, err := last(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRegistrarWaitClosesForCleanupAndRefusedUpgrade(t *testing.T) {
	for _, refused := range []bool{false, true} {
		t.Run(map[bool]string{false: "cleanup", true: "upgrade_refused"}[refused], func(t *testing.T) {
			withWaitGrace(t, time.Nanosecond)
			root, locks := t.TempDir(), t.TempDir()
			log := &waitLog{}
			waiting := make(chan struct{})
			var noticeOnce sync.Once
			a, err := New(root, locks, func(w Wait) {
				log.note(w)
				if w.Outcome == WaitBegan {
					noticeOnce.Do(func() { close(waiting) })
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			b, err := New(root, locks, nil)
			if err != nil {
				t.Fatal(err)
			}
			a.BeginRun()
			b.BeginRun()
			defer a.EndRun()
			defer b.EndRun()
			if err := a.AcquirePaths(t.Context(), []string{"written"}); err != nil {
				t.Fatal(err)
			}
			_, p := prepareCall(t, a, []string{"pending"})
			if refused {
				if err := b.AcquirePaths(t.Context(), []string{"foreign"}); err != nil {
					t.Fatal(err)
				}
			}
			guard, err := tryLockFile(a.lockPath + ".guard")
			if err != nil {
				t.Fatal(err)
			}
			var guardOnce sync.Once
			unlock := func() { guardOnce.Do(guard) }
			defer unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if refused {
					_, _, err := a.PreparePaths(ctx, []string{"foreign"})
					done <- err
				} else {
					done <- p.Close(ctx)
				}
			}()
			select {
			case <-waiting:
			case <-ctx.Done():
				t.Fatal("registrar wait never opened")
			}
			if !a.State().Waiting {
				t.Error("active registrar wait missing from state")
			}
			unlock()
			err = <-done
			want := WaitAcquired
			if refused {
				want = WaitAbandoned
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("upgrade=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if a.State().Waiting || !log.sameAs(WaitBegan, want) {
				t.Fatalf("wait did not close: state=%+v, notices=%v", a.State(), log.outcomes())
			}
		})
	}
}

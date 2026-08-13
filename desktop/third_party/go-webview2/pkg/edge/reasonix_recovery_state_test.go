package edge

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReasonixRecoveryCompletesOnlyMatchingReloadNavigation(t *testing.T) {
	var state reasonixRecoveryState[string]
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	if !state.begin("renderer", now, 30*time.Second, time.Hour, nil) {
		t.Fatal("first recovery was rejected")
	}
	if _, ok := state.completeNavigation(41); ok {
		t.Fatal("completion before the reload navigation started consumed the recovery")
	}
	if !state.bindNavigation(42) {
		t.Fatal("native reload navigation was not bound")
	}
	if _, ok := state.completeNavigation(43); ok {
		t.Fatal("unrelated completion consumed the recovery")
	}
	if got, ok := state.completeNavigation(42); !ok || got != "renderer" {
		t.Fatalf("matching completion = (%q, %v)", got, ok)
	}
}

func TestReasonixRecoveryCompletionIsConsumedOnce(t *testing.T) {
	var state reasonixRecoveryState[int]
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	if !state.begin(7, now, 30*time.Second, time.Hour, nil) || !state.bindNavigation(99) {
		t.Fatal("failed to arm recovery")
	}

	const observers = 16
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	var completed atomic.Int32
	for i := 0; i < observers; i++ {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			if _, ok := state.completeNavigation(99); ok {
				completed.Add(1)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	if got := completed.Load(); got != 1 {
		t.Fatalf("matching completions = %d, want 1", got)
	}
}

func TestReasonixRecoveryCooldownStartsAfterAcceptedFailure(t *testing.T) {
	var state reasonixRecoveryState[string]
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	if !state.begin("first", now, 30*time.Second, time.Hour, nil) {
		t.Fatal("first recovery was rejected")
	}
	if _, ok := state.finish(); !ok {
		t.Fatal("first recovery was not consumed")
	}
	if state.begin("early", now.Add(29*time.Second), 30*time.Second, time.Hour, nil) {
		t.Fatal("cooldown accepted an early retry")
	}
	if !state.begin("next", now.Add(30*time.Second), 30*time.Second, time.Hour, nil) {
		t.Fatal("recovery remained blocked after cooldown")
	}
	_, _ = state.finish()
}

func TestRendererFailureRejectedByRecoveryIsStillObserved(t *testing.T) {
	now := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		setup func(*testing.T, *Chromium)
	}{
		{
			name: "recovery pending",
			setup: func(t *testing.T, chromium *Chromium) {
				if !chromium.processRecovery.begin(ProcessFailedDiagnostic{}, now, time.Minute, 0, nil) {
					t.Fatal("failed to arm pending recovery")
				}
			},
		},
		{
			name: "cooldown active",
			setup: func(t *testing.T, chromium *Chromium) {
				if !chromium.processRecovery.begin(ProcessFailedDiagnostic{}, now, time.Minute, 0, nil) {
					t.Fatal("failed to establish cooldown")
				}
				_, _ = chromium.processRecovery.finish()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chromium := &Chromium{}
			tt.setup(t, chromium)
			var observed []ProcessFailedDiagnostic
			SetProcessFailedObserver(func(diagnostic ProcessFailedDiagnostic) {
				observed = append(observed, diagnostic)
			})
			t.Cleanup(func() { SetProcessFailedObserver(nil) })
			reloads := 0
			diagnostic := ProcessFailedDiagnostic{Kind: COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED}
			chromium.handleFailedRendererRecovery(diagnostic, now.Add(time.Second), func() error {
				reloads++
				return nil
			})
			if reloads != 0 {
				t.Fatalf("reloads = %d, want 0", reloads)
			}
			if len(observed) != 1 || observed[0].Recovery != "not_applicable" {
				t.Fatalf("observed = %+v, want one not_applicable diagnostic", observed)
			}
		})
	}
}

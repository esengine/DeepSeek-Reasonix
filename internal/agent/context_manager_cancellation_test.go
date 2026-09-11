package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// Sampling Err before signalling makes the lock-wait interleaving deterministic:
// the first check returns nil even if cancellation arrives before it returns.
type prepareEntryContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *prepareEntryContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.entered) })
	return err
}

func TestPrepareRejectsCancelledContextBeforeMaintenance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		turns    int
		deadline bool
	}{
		{"below threshold", 0, false},
		{"above ceiling", 6, false},
		{"expired deadline", 6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &failingSummaryProvider{}
			a := agentOverForce(t, prov, foldableSessionOverForce(tc.turns))
			before, err := json.Marshal(a.modelVisibleMessages())
			if err != nil {
				t.Fatal(err)
			}
			version := a.currentProjectionVersion()
			canonical, canonicalVersion := a.sess.conversation.snapshotMessagesVersion()
			canonicalBefore, err := json.Marshal(canonical)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			want := error(context.Canceled)
			if tc.deadline {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
				want = context.DeadlineExceeded
			} else {
				cancel()
			}
			defer cancel()
			trigger := CompactionTriggerOverflow
			if tc.turns == 0 {
				trigger = CompactionTriggerPressure
			}
			_, err = a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: trigger})
			if !errors.Is(err, want) {
				t.Fatalf("Prepare error = %v, want %v", err, want)
			}
			after, err := json.Marshal(a.modelVisibleMessages())
			if err != nil {
				t.Fatal(err)
			}
			current, currentVersion := a.sess.conversation.snapshotMessagesVersion()
			canonicalAfter, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) || version != a.currentProjectionVersion() || a.sess.compactionState.LastReceipt != nil {
				t.Fatal("cancelled Prepare changed projection or receipt")
			}
			if string(canonicalBefore) != string(canonicalAfter) || canonicalVersion != currentVersion {
				t.Fatal("cancelled Prepare changed canonical history")
			}
			if prov.calls != 0 {
				t.Fatalf("cancelled Prepare called provider %d times", prov.calls)
			}
		})
	}
}

func TestPrepareRejectsCancellationWhileWaitingForMaintenance(t *testing.T) {
	prov := &failingSummaryProvider{}
	a := agentOverForce(t, prov, foldableSessionOverForce(6))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entry := &prepareEntryContext{Context: ctx, entered: make(chan struct{})}
	a.sess.compactionRunMu.Lock()
	result := make(chan error, 1)
	go func() {
		_, err := a.contextManager().Prepare(entry, ContextPreparePolicy{Trigger: CompactionTriggerOverflow})
		result <- err
	}()
	select {
	case <-entry.entered:
	case <-time.After(5 * time.Second):
		cancel()
		a.sess.compactionRunMu.Unlock()
		<-result
		t.Fatal("Prepare did not check cancellation before waiting for maintenance")
	}
	cancel()
	a.sess.compactionRunMu.Unlock()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Prepare error = %v, want cancellation", err)
	}
	if prov.calls != 0 || a.currentProjectionVersion() != 0 || a.sess.compactionState.LastReceipt != nil {
		t.Fatal("cancelled waiter entered maintenance")
	}
}

func TestPrepareAllowsLiveBelowThresholdContext(t *testing.T) {
	prov := &failingSummaryProvider{}
	a := agentOverForce(t, prov, foldableSessionOverForce(0))
	prepared, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Messages) == 0 || prov.calls != 0 || a.currentProjectionVersion() != 0 {
		t.Fatal("live fast path changed")
	}
}

func TestPreparePreservesLegacyNilContext(t *testing.T) {
	for _, manual := range []bool{false, true} {
		turns := 0
		if manual {
			turns = 6
		}
		a := agentOverForce(t, &fakeProvider{reply: "digest"}, foldableSessionOverForce(turns))
		var err error
		if manual {
			err = a.CompactNow(nil, "") //nolint:staticcheck // Exercise the legacy nil-context compatibility boundary.
		} else {
			err = a.PrepareContext(nil) //nolint:staticcheck // Exercise the legacy nil-context compatibility boundary.
		}
		if err != nil {
			t.Fatalf("manual=%v: %v", manual, err)
		}
	}
}

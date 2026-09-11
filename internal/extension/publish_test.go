package extension

import (
	"testing"
	"time"
)

func TestPublishGateStaleAndAdmit(t *testing.T) {
	g := NewPublishGate()
	g.Publish(2)
	if g.Published() != 2 {
		t.Fatalf("published = %d", g.Published())
	}
	// Only older than published is stale; equal is current.
	if !g.IsStale(1) || g.IsStale(2) || g.IsStale(0) || g.IsStale(3) {
		t.Fatal("stale checks failed")
	}
	if !g.AdmitNewWork(2) || g.AdmitNewWork(1) {
		t.Fatal("admit checks failed")
	}
	if got := g.DrainingGenerations(); len(got) != 0 {
		t.Fatalf("first publish unexpectedly drained generations: %v", got)
	}
	g.Publish(3)
	if !g.IsDraining(2) {
		t.Fatal("gen 2 should be draining")
	}
	if g.DropStale(2, "ui") != true {
		t.Fatal("drop stale")
	}
}

func TestPublishGateSweep(t *testing.T) {
	g := NewPublishGate().WithDrainTTL(time.Millisecond)
	g.Publish(1)
	g.Publish(2)
	g.mu.Lock()
	g.draining[1] = time.Now().Add(-time.Second)
	g.mu.Unlock()
	expired := g.SweepExpiredDrains()
	if len(expired) != 1 || expired[0] != 1 {
		t.Fatalf("expired = %v", expired)
	}
}

func TestPublishGateSweepAndForceExpireRecordsReceipt(t *testing.T) {
	// Isolate default store pollution by using a private gate + checking store
	// has a drain-timeout receipt for the expired gen.
	g := NewPublishGate().WithDrainTTL(time.Millisecond)
	g.Publish(10)
	g.Publish(11)
	g.mu.Lock()
	g.draining[10] = time.Now().Add(-time.Second)
	g.mu.Unlock()
	expired := g.SweepAndForceExpire()
	if len(expired) != 1 || expired[0] != 10 {
		t.Fatalf("expired = %v", expired)
	}
	if _, ok := g.receipts.Get("drain-timeout-10"); !ok {
		t.Fatal("expected drain-timeout receipt")
	}
	if g.IsDraining(10) {
		t.Fatal("gen 10 should no longer be draining")
	}
}

func TestPublishGateLateDrainCancelFiresImmediately(t *testing.T) {
	g := NewPublishGate()
	g.Publish(20)
	g.Publish(21)
	g.ForceExpireDrain(20)

	fired := false
	g.RegisterDrainCancel(20, func() { fired = true })
	if !fired {
		t.Fatal("cancel registered after force-expire must fire immediately")
	}
}

func TestPublishGateDrainCancelCanBeUnregistered(t *testing.T) {
	g := NewPublishGate()
	g.Publish(30)
	fired := false
	unregister := g.RegisterDrainCancel(30, func() { fired = true })
	unregister()
	unregister()

	g.mu.RLock()
	pending := len(g.drainCancels[30])
	g.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("pending drain cancels = %d, want 0", pending)
	}
	g.ForceExpireDrain(30)
	if fired {
		t.Fatal("unregistered drain cancel fired")
	}
}

func TestPublishGateDrainWatchSkipsColdPublishAndCoalesces(t *testing.T) {
	g := NewPublishGate().WithDrainTTL(time.Hour)
	g.Publish(1)
	g.ScheduleDrainWatch()
	g.mu.RLock()
	coldWatching := g.drainWatching
	g.mu.RUnlock()
	if coldWatching {
		t.Fatal("cold publish without a draining generation started a watcher")
	}

	g.Publish(2)
	g.ScheduleDrainWatch()
	g.ScheduleDrainWatch()
	g.mu.RLock()
	watching := g.drainWatching
	g.mu.RUnlock()
	if !watching {
		t.Fatal("active drain did not start its coalesced watcher")
	}
}

func TestPublishGateBoundsExpiredGenerations(t *testing.T) {
	g := NewPublishGate()
	g.expiredLimit = 2
	for gen := uint64(1); gen <= 3; gen++ {
		g.ForceExpireDrain(gen)
	}
	g.mu.RLock()
	expiredLen := len(g.expired)
	orderLen := len(g.expiredOrder)
	_, hasFirst := g.expired[1]
	_, hasSecond := g.expired[2]
	_, hasThird := g.expired[3]
	g.mu.RUnlock()
	if expiredLen != 2 || orderLen != 2 {
		t.Fatalf("expired retention = map:%d order:%d, want 2", expiredLen, orderLen)
	}
	if hasFirst {
		t.Fatal("oldest expired generation was not evicted")
	}
	if !hasSecond || !hasThird {
		t.Fatalf("latest expired generations retained = second:%v third:%v, want true/true", hasSecond, hasThird)
	}

	g.Publish(4)
	fired := false
	g.RegisterDrainCancel(1, func() { fired = true })
	if !fired {
		t.Fatal("late cancel for an evicted expired generation was retained")
	}
}

func TestLifecycleTransitions(t *testing.T) {
	r := NewLifecycleRegistry(5)
	r.Ensure("plugin/a")
	if err := r.Transition("plugin/a", ComponentPreparing, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Transition("plugin/a", ComponentActive, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Transition("plugin/a", ComponentInactive, ""); err == nil {
		t.Fatal("Active -> Inactive without Draining should fail")
	}
	if err := r.Transition("plugin/a", ComponentDraining, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Transition("plugin/a", ComponentInactive, ""); err != nil {
		t.Fatal(err)
	}
	st, ok := r.Status("plugin/a")
	if !ok || st.State != ComponentInactive {
		t.Fatalf("status = %+v", st)
	}
}

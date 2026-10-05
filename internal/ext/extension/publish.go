package extension

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PublishGate enforces the activate → publish → drain order for runtime
// generations. Only one generation is Published at a time; older generations
// are Draining and their late traffic must be dropped.
type PublishGate struct {
	mu            sync.RWMutex
	published     uint64
	draining      map[uint64]time.Time // gen → drain start
	drainTTL      time.Duration
	onStale       func(gen uint64, kind string)
	receipts      *ReceiptStore
	drainCancels  map[uint64]map[uint64]func()
	drainCancelID uint64
	expired       map[uint64]struct{}
	expiredOrder  []uint64
	expiredLimit  int
	drainWatching bool
}

const defaultExpiredGenerationLimit = 256

// NewPublishGate returns a gate with a default drain timeout of 30s.
func NewPublishGate() *PublishGate {
	return newPublishGate(NewReceiptStore())
}

func newPublishGate(receipts *ReceiptStore) *PublishGate {
	if receipts == nil {
		receipts = NewReceiptStore()
	}
	return &PublishGate{
		draining:     make(map[uint64]time.Time),
		drainTTL:     30 * time.Second,
		receipts:     receipts,
		drainCancels: make(map[uint64]map[uint64]func()),
		expired:      make(map[uint64]struct{}),
		expiredLimit: defaultExpiredGenerationLimit,
	}
}

// WithDrainTTL sets how long a draining generation is tracked before force-forget.
func (g *PublishGate) WithDrainTTL(d time.Duration) *PublishGate {
	if g != nil && d > 0 {
		g.mu.Lock()
		g.drainTTL = d
		g.mu.Unlock()
	}
	return g
}

// Published returns the currently published generation (0 if none).
func (g *PublishGate) Published() uint64 {
	if g == nil {
		return 0
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.published
}

// Publish atomically switches the published generation. The previous generation
// enters Draining. Publishing the same generation is a no-op.
func (g *PublishGate) Publish(gen uint64) {
	if g == nil || gen == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.published == gen {
		return
	}
	if g.published != 0 {
		if g.draining == nil {
			g.draining = make(map[uint64]time.Time)
		}
		g.draining[g.published] = time.Now()
	}
	delete(g.draining, gen)
	g.clearExpiredLocked(gen)
	g.published = gen
	DefaultLifecycleMetrics.Publishes.Add(1)
}

// BeginDrain marks gen as draining without changing the published pointer
// (used when an un-published activation fails and its resources are disposed).
func (g *PublishGate) BeginDrain(gen uint64) {
	if g == nil || gen == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.draining == nil {
		g.draining = make(map[uint64]time.Time)
	}
	g.clearExpiredLocked(gen)
	g.draining[gen] = time.Now()
	DefaultLifecycleMetrics.Drains.Add(1)
}

// IsStale reports whether messageGen must be dropped: it is non-zero and does
// not match the published generation.
func (g *PublishGate) IsStale(messageGen uint64) bool {
	if g == nil || messageGen == 0 {
		return false
	}
	g.mu.RLock()
	pub := g.published
	g.mu.RUnlock()
	return StaleGeneration(messageGen, pub)
}

// IsDraining reports whether gen is currently in the drain set.
func (g *PublishGate) IsDraining(gen uint64) bool {
	if g == nil || gen == 0 {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.draining[gen]
	return ok
}

// AdmitNewWork reports whether a new turn may be admitted for gen. Only the
// published generation admits new work; draining generations refuse.
func (g *PublishGate) AdmitNewWork(gen uint64) bool {
	if g == nil {
		return true
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.published == 0 {
		return true
	}
	return gen == g.published
}

// DropStale logs (via onStale) and returns true when messageGen is stale.
func (g *PublishGate) DropStale(messageGen uint64, kind string) bool {
	if !g.IsStale(messageGen) {
		return false
	}
	DefaultLifecycleMetrics.StaleDrops.Add(1)
	if g != nil && g.onStale != nil {
		g.onStale(messageGen, kind)
	}
	return true
}

// SweepExpiredDrains removes drain entries older than drainTTL.
func (g *PublishGate) SweepExpiredDrains() []uint64 {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var expired []uint64
	now := time.Now()
	for gen, started := range g.draining {
		if now.Sub(started) >= g.drainTTL {
			expired = append(expired, gen)
			delete(g.draining, gen)
			g.markExpiredLocked(gen)
		}
	}
	return expired
}

// DrainTimeoutError is returned when remaining in-flight work is cancelled
// after the drain TTL.
type DrainTimeoutError struct {
	Generation uint64
}

func (e *DrainTimeoutError) Error() string {
	return fmt.Sprintf("extension: generation %d drain timed out", e.Generation)
}

// RegisterDrainCancel registers a cancel func for gen and returns an idempotent
// unregister function. Remaining callbacks fire once when the generation is
// force-expired after drain TTL (or explicit ForceExpireDrain).
func (g *PublishGate) RegisterDrainCancel(gen uint64, cancel func()) func() {
	if g == nil || gen == 0 || cancel == nil {
		return func() {}
	}
	g.mu.Lock()
	_, expired := g.expired[gen]
	_, draining := g.draining[gen]
	// Generation IDs increase monotonically. Once an old expiry marker leaves
	// bounded retention, a generation below the published one is still stale
	// and its late registration must be cancelled instead of retained forever.
	forgottenExpired := !expired && !draining && g.published != 0 && gen < g.published
	if expired || forgottenExpired {
		g.mu.Unlock()
		cancel()
		return func() {}
	}
	if g.drainCancels == nil {
		g.drainCancels = make(map[uint64]map[uint64]func())
	}
	if g.drainCancels[gen] == nil {
		g.drainCancels[gen] = make(map[uint64]func())
	}
	g.drainCancelID++
	id := g.drainCancelID
	g.drainCancels[gen][id] = cancel
	g.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			if callbacks := g.drainCancels[gen]; callbacks != nil {
				delete(callbacks, id)
				if len(callbacks) == 0 {
					delete(g.drainCancels, gen)
				}
			}
			g.mu.Unlock()
		})
	}
}

// FireDrainCancels runs and clears all cancel callbacks for gen.
func (g *PublishGate) FireDrainCancels(gen uint64) {
	if g == nil || gen == 0 {
		return
	}
	g.mu.Lock()
	fns := g.drainCancels[gen]
	delete(g.drainCancels, gen)
	g.markExpiredLocked(gen)
	g.mu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn()
		}
	}
}

// ForceExpireDrain cancels remaining in-flight work for gen, then records a
// cleanup receipt and forgets the drain entry.
func (g *PublishGate) ForceExpireDrain(gen uint64) {
	if g == nil || gen == 0 {
		return
	}
	g.mu.Lock()
	fns := g.drainCancels[gen]
	delete(g.drainCancels, gen)
	delete(g.draining, gen)
	g.markExpiredLocked(gen)
	g.mu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn()
		}
	}
	g.receipts.Record(EffectReceipt{
		ID:                 fmt.Sprintf("drain-timeout-%d", gen),
		Generation:         gen,
		Class:              Irreversible,
		CompensationStatus: "not_applicable",
		Error:              (&DrainTimeoutError{Generation: gen}).Error(),
	})
	DefaultLifecycleMetrics.Drains.Add(1)
}

// SweepAndForceExpire removes expired drain entries, cancels in-flight work,
// and records a timeout receipt for each.
func (g *PublishGate) SweepAndForceExpire() []uint64 {
	expired := g.SweepExpiredDrains()
	for _, gen := range expired {
		g.FireDrainCancels(gen)
		g.receipts.Record(EffectReceipt{
			ID:                 fmt.Sprintf("drain-timeout-%d", gen),
			Generation:         gen,
			Class:              Irreversible,
			CompensationStatus: "not_applicable",
			Error:              (&DrainTimeoutError{Generation: gen}).Error(),
		})
		DefaultLifecycleMetrics.Drains.Add(1)
	}
	return expired
}

// ScheduleDrainWatch starts one background timer while a gate has active
// draining generations. Calls are coalesced so cold publishes do not allocate
// a timer goroutine and rapid publishes do not create one watcher per publish.
func (g *PublishGate) ScheduleDrainWatch() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if len(g.draining) == 0 || g.drainWatching {
		g.mu.Unlock()
		return
	}
	g.drainWatching = true
	ttl := g.drainTTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	wakeAfter := ttl
	now := time.Now()
	for _, started := range g.draining {
		remaining := ttl - now.Sub(started)
		if remaining < wakeAfter {
			wakeAfter = remaining
		}
	}
	if wakeAfter < 0 {
		wakeAfter = 0
	}
	g.mu.Unlock()
	go func() {
		timer := time.NewTimer(wakeAfter)
		defer timer.Stop()
		<-timer.C
		g.SweepAndForceExpire()
		g.mu.Lock()
		g.drainWatching = false
		watchAgain := len(g.draining) > 0
		g.mu.Unlock()
		if watchAgain {
			g.ScheduleDrainWatch()
		}
	}()
}

func (g *PublishGate) markExpiredLocked(gen uint64) {
	if gen == 0 {
		return
	}
	if _, ok := g.expired[gen]; ok {
		return
	}
	if g.expired == nil {
		g.expired = make(map[uint64]struct{})
	}
	g.expired[gen] = struct{}{}
	g.expiredOrder = append(g.expiredOrder, gen)
	limit := g.expiredLimit
	if limit < 1 {
		limit = defaultExpiredGenerationLimit
	}
	for len(g.expiredOrder) > limit {
		old := g.expiredOrder[0]
		g.expiredOrder = g.expiredOrder[1:]
		delete(g.expired, old)
	}
}

func (g *PublishGate) clearExpiredLocked(gen uint64) {
	if _, ok := g.expired[gen]; !ok {
		return
	}
	delete(g.expired, gen)
	for i, item := range g.expiredOrder {
		if item != gen {
			continue
		}
		copy(g.expiredOrder[i:], g.expiredOrder[i+1:])
		g.expiredOrder = g.expiredOrder[:len(g.expiredOrder)-1]
		break
	}
}

// DrainingGenerations returns generations currently in drain.
func (g *PublishGate) DrainingGenerations() []uint64 {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]uint64, 0, len(g.draining))
	for gen := range g.draining {
		out = append(out, gen)
	}
	return out
}

// DefaultPublishGate returns the compatibility owner gate. Product boot paths
// bind an isolated RuntimeOwner instead.
func DefaultPublishGate() *PublishGate {
	return RuntimeOwnerOrDefault(nil).Gate
}

// RegisterDrainCancel preserves the package-level compatibility API.
func RegisterDrainCancel(gen uint64, cancel func()) func() {
	return DefaultPublishGate().RegisterDrainCancel(gen, cancel)
}

// FireDrainCancels preserves the package-level compatibility API.
func FireDrainCancels(gen uint64) {
	DefaultPublishGate().FireDrainCancels(gen)
}

// AwaitReady waits until ctx is done or ready is closed. Used by activation
// transactions to gate publish on component readiness.
func AwaitReady(ctx context.Context, ready <-chan struct{}) error {
	if ready == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
		return nil
	}
}

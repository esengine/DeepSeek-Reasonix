package extension

import (
	"context"
	"sync"
	"sync/atomic"
)

// HostStreamRegistry tracks host-side provider streams (OpenAI/Anthropic/etc.)
// so generation drain can cancel in-flight HTTP reads without waiting for
// controller.Cancel of a still-published generation.
type HostStreamRegistry struct {
	mu     sync.Mutex
	byGen  map[uint64]map[uint64]context.CancelFunc
	nextID atomic.Uint64
	gate   *PublishGate
	// drainHooked remembers which generations already registered a
	// RegisterDrainCancel fan-in so we do not stack duplicate hooks.
	drainHooked map[uint64]struct{}
}

// DefaultHostStreams belongs to the compatibility runtime owner.
var DefaultHostStreams = DefaultRuntimeOwner.HostStreams

// NewHostStreamRegistry returns an empty registry.
func NewHostStreamRegistry(gates ...*PublishGate) *HostStreamRegistry {
	var gate *PublishGate
	if len(gates) > 0 {
		gate = gates[0]
	}
	return &HostStreamRegistry{
		byGen:       make(map[uint64]map[uint64]context.CancelFunc),
		drainHooked: make(map[uint64]struct{}),
		gate:        gate,
	}
}

// Track registers cancel for gen and returns untrack (safe to call once).
// When gen is 0, tracking is a no-op (no publish gate yet).
func (r *HostStreamRegistry) Track(gen uint64, cancel context.CancelFunc) (untrack func()) {
	if r == nil || gen == 0 || cancel == nil {
		return func() {}
	}
	id := r.nextID.Add(1)
	r.mu.Lock()
	if r.byGen[gen] == nil {
		r.byGen[gen] = make(map[uint64]context.CancelFunc)
	}
	r.byGen[gen][id] = cancel
	registerDrainHook := false
	if _, hooked := r.drainHooked[gen]; !hooked {
		r.drainHooked[gen] = struct{}{}
		registerDrainHook = true
	}
	r.mu.Unlock()
	if registerDrainHook {
		// Fan-in: one drain cancel per generation cancels every tracked stream.
		// Register outside r.mu because an already-expired generation fires the
		// callback synchronously and re-enters CancelGeneration.
		gate := r.gate
		if gate == nil {
			gate = DefaultPublishGate()
		}
		gate.RegisterDrainCancel(gen, func() { r.CancelGeneration(gen) })
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if m := r.byGen[gen]; m != nil {
				delete(m, id)
				if len(m) == 0 {
					delete(r.byGen, gen)
				}
			}
			r.mu.Unlock()
		})
	}
}

// CancelGeneration cancels every host stream still tracked for gen.
func (r *HostStreamRegistry) CancelGeneration(gen uint64) {
	if r == nil || gen == 0 {
		return
	}
	r.mu.Lock()
	m := r.byGen[gen]
	delete(r.byGen, gen)
	delete(r.drainHooked, gen)
	r.mu.Unlock()
	for _, c := range m {
		if c != nil {
			c()
		}
	}
}

// Count returns live tracked streams for gen (tests).
func (r *HostStreamRegistry) Count(gen uint64) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byGen[gen])
}

// TrackHostStream is a convenience over DefaultHostStreams.Track.
func TrackHostStream(gen uint64, cancel context.CancelFunc) (untrack func()) {
	return RuntimeOwnerOrDefault(nil).HostStreams.Track(gen, cancel)
}

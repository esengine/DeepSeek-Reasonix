package extension

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Backend is a replaceable provider or MCP backend behind a StableProxy.
type Backend interface {
	ID() string
	// Close drains in-flight work owned by this backend.
	Close(context.Context) error
}

// StableProxy presents a stable consumer-facing handle while backends roll.
// It does not alter provider-visible prompt/tool prefixes; cache identity
// remains owned by RuntimeSnapshot.CacheHash.
type StableProxy struct {
	mu       sync.RWMutex
	active   Backend
	draining []Backend
	closed   atomic.Bool
	// generation of the currently active backend registration.
	generation uint64
	// inFlight tracks CallCtx cancel funcs so Replace/Close/drain can abort
	// mid-call work instead of hanging across a backend roll.
	inFlight map[uint64]context.CancelFunc
	nextCall atomic.Uint64
}

// NewStableProxy returns an empty proxy.
func NewStableProxy() *StableProxy {
	return &StableProxy{inFlight: make(map[uint64]context.CancelFunc)}
}

// Active returns the current backend, or nil.
func (p *StableProxy) Active() Backend {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.active
}

// Generation returns the active backend generation.
func (p *StableProxy) Generation() uint64 {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.generation
}

// Replace swaps in a new backend and begins draining the previous one.
// Rolling replacement keeps the consumer pointer stable. In-flight CallCtx
// work is cancelled before the previous backend is closed.
func (p *StableProxy) Replace(ctx context.Context, next Backend, generation uint64) error {
	if p == nil {
		return fmt.Errorf("extension: nil StableProxy")
	}
	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		if next != nil {
			_ = next.Close(ctx)
		}
		return fmt.Errorf("extension: proxy closed")
	}
	prev := p.active
	p.active = next
	p.generation = generation
	if prev != nil {
		p.draining = append(p.draining, prev)
	}
	cancels := p.takeInFlightLocked()
	p.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	if prev != nil {
		if err := prev.Close(ctx); err != nil {
			return fmt.Errorf("drain previous backend: %w", err)
		}
		p.mu.Lock()
		out := p.draining[:0]
		for _, b := range p.draining {
			if b != prev {
				out = append(out, b)
			}
		}
		p.draining = out
		p.mu.Unlock()
	}
	return nil
}

// Close drains the active and any remaining backends after cancelling in-flight calls.
func (p *StableProxy) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	p.mu.Lock()
	active := p.active
	p.active = nil
	draining := append([]Backend(nil), p.draining...)
	p.draining = nil
	cancels := p.takeInFlightLocked()
	p.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	var first error
	if active != nil {
		first = active.Close(ctx)
	}
	for _, b := range draining {
		if err := b.Close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Call invokes fn with the active backend. If no backend is registered the
// call fails fast so consumers do not hang across a crash/replace window.
func (p *StableProxy) Call(fn func(Backend) error) error {
	return p.CallCtx(context.Background(), func(_ context.Context, b Backend) error {
		return fn(b)
	})
}

// CallCtx is Call with a parent context. The call is cancelled when the proxy
// is Replaced or Closed (drain of in-flight work).
func (p *StableProxy) CallCtx(ctx context.Context, fn func(context.Context, Backend) error) error {
	if p == nil || p.closed.Load() {
		return fmt.Errorf("extension: proxy unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	id := p.nextCall.Add(1)
	p.mu.Lock()
	if p.inFlight == nil {
		p.inFlight = make(map[uint64]context.CancelFunc)
	}
	p.inFlight[id] = cancel
	b := p.active
	p.mu.Unlock()
	defer func() {
		cancel()
		p.mu.Lock()
		delete(p.inFlight, id)
		p.mu.Unlock()
	}()
	if b == nil {
		return fmt.Errorf("extension: no active backend")
	}
	return fn(ctx, b)
}

// CancelInFlight aborts every outstanding CallCtx. Used by generation drain.
func (p *StableProxy) CancelInFlight() {
	if p == nil {
		return
	}
	p.mu.Lock()
	cancels := p.takeInFlightLocked()
	p.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (p *StableProxy) takeInFlightLocked() []context.CancelFunc {
	if p == nil || len(p.inFlight) == 0 {
		return nil
	}
	out := make([]context.CancelFunc, 0, len(p.inFlight))
	for id, c := range p.inFlight {
		out = append(out, c)
		delete(p.inFlight, id)
	}
	return out
}

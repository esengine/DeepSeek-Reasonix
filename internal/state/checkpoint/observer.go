package checkpoint

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"reasonix/internal/base/diff"
)

// writerRegistry is shared across parent/child observers so background writer
// registration is race-free under a single mutex.
type writerRegistry struct {
	mu      sync.Mutex
	publish sync.Mutex
	writers map[string]*writerRegistration
}

// MutationObserver is the host-side unified file mutation observer that replaces
// a single onPreEdit hook. It captures preimages before mutations and after
// fingerprints regardless of tool success/failure.
//
// The observer is passed through Agent Options/context to sub-agents. It does
// not change provider-visible tool names, schemas, or system prompts.
type MutationObserver struct {
	store *Store

	mu sync.Mutex
	// shared writers registry (parent and clones share the same pointer).
	reg *writerRegistry
	// ownershipTurn is the turn that owns the current observation context.
	// Foreground sub-agents inherit the parent turn; background ones keep the
	// turn that spawned them.
	ownershipTurn int
	// writerID identifies the current agent for AfterMutation bookkeeping.
	writerID string
	// background marks a background sub-agent writer.
	background bool
	// seq is a monotonic mutation counter shared across the store session.
	seq *atomic.Int64
}

// ObserverOptions configures a MutationObserver bound to a store.
type ObserverOptions struct {
	Store         *Store
	OwnershipTurn int
	WriterID      string
	Background    bool
	Seq           *atomic.Int64
}

// NewMutationObserver binds an observer to store. A nil store yields a no-op observer.
func NewMutationObserver(opts ObserverOptions) *MutationObserver {
	seq := opts.Seq
	if seq == nil {
		seq = &atomic.Int64{}
	}
	return &MutationObserver{
		store: opts.Store,
		reg: &writerRegistry{
			writers: map[string]*writerRegistration{},
		},
		ownershipTurn: opts.OwnershipTurn,
		writerID:      opts.WriterID,
		background:    opts.Background,
		seq:           seq,
	}
}

// CloneForSubagent returns a child observer that shares the store, mutation
// sequence, and writer registry but has its own writer identity and ownership turn.
func (o *MutationObserver) CloneForSubagent(writerID string, ownershipTurn int, background bool) *MutationObserver {
	if o == nil {
		return nil
	}
	return &MutationObserver{
		store:         o.store,
		reg:           o.reg,
		ownershipTurn: ownershipTurn,
		writerID:      writerID,
		background:    background,
		seq:           o.seq,
	}
}

// Store returns the underlying checkpoint store.
func (o *MutationObserver) Store() *Store {
	if o == nil {
		return nil
	}
	return o.store
}

// SetOwnershipTurn updates the turn that owns subsequent captures.
func (o *MutationObserver) SetOwnershipTurn(turn int) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.ownershipTurn = turn
	o.mu.Unlock()
}

// OwnershipTurn returns the current ownership turn.
func (o *MutationObserver) OwnershipTurn() int {
	if o == nil {
		return 0
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ownershipTurn
}

// BeginWrite retires undo durably before a tool or hook can modify files.
func (o *MutationObserver) BeginWrite() (func(), error) {
	return o.BeginWriteContext(context.Background())
}

// BeginWriteContext checks cancellation before admission and after preparation.
// Durable undo retirement already completed is not reversed on cancellation.
func (o *MutationObserver) BeginWriteContext(ctx context.Context) (permit func(), resultErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o == nil || o.store == nil {
		return nil, nil
	}
	barrier := o.store.Barrier()
	if err := barrier.EnterWriteContext(ctx); err != nil {
		return nil, err
	}
	release := sync.OnceFunc(barrier.ExitWrite)
	defer func() {
		if permit == nil {
			release()
		}
	}()
	if err := o.store.InvalidateUndo(); err != nil {
		return nil, fmt.Errorf("prepare workspace write: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return release, nil
}

// ActiveWriters returns a copy of currently registered writers.
func (o *MutationObserver) ActiveWriters() []ActiveWriter {
	if o == nil || o.reg == nil {
		return nil
	}
	o.reg.mu.Lock()
	defer o.reg.mu.Unlock()
	return o.snapshotWritersLocked()
}

// Caller must hold o.reg.mu.
func (o *MutationObserver) snapshotWritersLocked() []ActiveWriter {
	if o.reg == nil || len(o.reg.writers) == 0 {
		return nil
	}
	out := make([]ActiveWriter, 0, len(o.reg.writers))
	for _, w := range o.reg.writers {
		if w.release != nil {
			out = append(out, w.writer)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// HasActiveWriters reports whether any background writer is still running.
func (o *MutationObserver) HasActiveWriters() bool {
	return len(o.ActiveWriters()) > 0
}

// HasOtherActiveWriters reports whether a background writer other than this
// observer's own is still running: a background child is itself registered,
// and its own run is not another writer beside it.
func (o *MutationObserver) HasOtherActiveWriters() bool {
	for _, w := range o.ActiveWriters() {
		if w.ID != o.writerID {
			return true
		}
	}
	return false
}

// BeforeMutation captures the preimage for a known path before a tool or hook runs.
// Prefer this over the legacy Snapshot(diff.Change) path for built-in tools.
func (o *MutationObserver) BeforeMutation(path, tool string, source CaptureSource) {
	if o == nil || o.store == nil || path == "" {
		return
	}
	if source == "" {
		source = CaptureBeforeMutation
	}
	o.store.CaptureBefore(path, CaptureBeforeOpts{
		Tool:          tool,
		Source:        source,
		WriterID:      o.writerID,
		OwnershipTurn: o.OwnershipTurn(),
		Background:    o.background,
	})
}

// BeforeMutationFromChange is the Previewer-compatible path: uses OldText when
// provided for encoding-stable text captures, otherwise falls back to disk.
func (o *MutationObserver) BeforeMutationFromChange(ch diff.Change, tool string) {
	if o == nil || o.store == nil || ch.Path == "" {
		return
	}
	o.store.CaptureBeforeFromChange(ch, CaptureBeforeOpts{
		Tool:          tool,
		Source:        CapturePreviewer,
		WriterID:      o.writerID,
		OwnershipTurn: o.OwnershipTurn(),
		Background:    o.background,
	})
}

// AfterMutation re-reads the path after a tool attempt (success or failure) and
// records the after fingerprint under Reasonix ownership.
func (o *MutationObserver) AfterMutation(path, tool string) error {
	if o == nil || o.store == nil || path == "" {
		return nil
	}
	seq := o.seq.Add(1)
	return o.store.CaptureAfter(path, CaptureAfterOpts{
		Seq:           seq,
		Tool:          tool,
		Source:        CaptureAfterMutation,
		WriterID:      o.writerID,
		OwnershipTurn: o.OwnershipTurn(),
		Background:    o.background,
	})
}

// RecordGap attaches an explicit coverage gap (bash, hook, MCP, …).
func (o *MutationObserver) RecordGap(gap CoverageGap) {
	if o == nil || o.store == nil {
		return
	}
	o.store.RecordGap(gap)
}

// NoteCrossTurnBackgroundWriter records a gap when a new user turn begins while
// a background writer from an earlier turn is still active.
func (o *MutationObserver) NoteCrossTurnBackgroundWriter(newTurn int) {
	if o == nil {
		return
	}
	for _, w := range o.ActiveWriters() {
		if w.Turn < newTurn {
			o.RecordGap(CoverageGap{
				Reason: GapBackgroundWriter,
				Detail: "background writer from earlier turn still active",
				Tool:   w.Kind,
			})
		}
	}
}

// CaptureBeforeOpts configures a preimage capture.
type CaptureBeforeOpts struct {
	Tool          string
	Source        CaptureSource
	WriterID      string
	OwnershipTurn int
	Background    bool
}

// CaptureAfterOpts configures an after-fingerprint capture.
type CaptureAfterOpts struct {
	Seq           int64
	Tool          string
	Source        CaptureSource
	WriterID      string
	OwnershipTurn int
	Background    bool
}

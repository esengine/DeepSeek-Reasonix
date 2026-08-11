package browserhost

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
)

// Metrics is the process-local browser host counter set for doctor/UI.
type Metrics struct {
	InFlight             atomic.Int64
	AdmissionRejected    atomic.Uint64
	StaleResponseDrop    atomic.Uint64
	Timeouts             atomic.Uint64
	Cancels              atomic.Uint64
	IrreversibleReceipts atomic.Uint64
}

// Snapshot is a point-in-time metrics copy.
type MetricsSnapshot struct {
	InFlight             int64  `json:"inFlight"`
	AdmissionRejected    uint64 `json:"admissionRejected"`
	StaleResponseDrop    uint64 `json:"staleResponseDrop"`
	Timeouts             uint64 `json:"timeouts"`
	Cancels              uint64 `json:"cancels"`
	IrreversibleReceipts uint64 `json:"irreversibleReceipts"`
}

// Snapshot returns current counters.
func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		InFlight:             m.InFlight.Load(),
		AdmissionRejected:    m.AdmissionRejected.Load(),
		StaleResponseDrop:    m.StaleResponseDrop.Load(),
		Timeouts:             m.Timeouts.Load(),
		Cancels:              m.Cancels.Load(),
		IrreversibleReceipts: m.IrreversibleReceipts.Load(),
	}
}

// DefaultMetrics is the package-wide counter used by Bindings that do not
// receive a private metrics instance.
var DefaultMetrics Metrics

// Binding is one generation-scoped, plugin-scoped wrapper around a tab-bound
// Backend. Dispose cancels in-flight plugin browser work without stopping the
// app-global BrowserCoordinator or Chromium session.
type Binding struct {
	backend    Backend
	owner      *extension.RuntimeOwner
	generation uint64
	pluginID   string
	metrics    *Metrics

	mu     sync.Mutex
	closed bool
	cancel context.CancelFunc
	ctx    context.Context
	seq    atomic.Uint64
}

// BindingOptions configures one generation-scoped binding.
type BindingOptions struct {
	Backend    Backend
	Owner      *extension.RuntimeOwner
	Generation uint64
	PluginID   string
	Metrics    *Metrics
}

// NewBinding creates a generation-scoped browser host binding. Backend may be
// nil (every call returns browser_unavailable).
func NewBinding(opts BindingOptions) *Binding {
	metrics := opts.Metrics
	if metrics == nil {
		metrics = &DefaultMetrics
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &Binding{
		backend:    opts.Backend,
		owner:      opts.Owner,
		generation: opts.Generation,
		pluginID:   opts.PluginID,
		metrics:    metrics,
		cancel:     cancel,
		ctx:        ctx,
	}
	if opts.Owner != nil && opts.Generation != 0 {
		// Drain of this generation cancels plugin browser work for this binding.
		opts.Owner.Gate.RegisterDrainCancel(opts.Generation, cancel)
	}
	return b
}

// Dispose cancels in-flight calls for this generation/plugin only.
func (b *Binding) Dispose() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// List implements host/browser/tab/list.
func (b *Binding) List(ctx context.Context, _ protocol.BrowserTabListParams) (protocol.BrowserTabListResult, error) {
	tabs, err := call(b, ctx, false, "list", func(c context.Context) (any, error) {
		if b.backend == nil {
			return nil, protocol.MustProtocolError(protocol.ErrBrowserUnavailable)
		}
		return b.backend.List(c)
	})
	if err != nil {
		return protocol.BrowserTabListResult{}, err
	}
	out := tabs.([]protocol.BrowserTab)
	if out == nil {
		out = []protocol.BrowserTab{}
	}
	return protocol.BrowserTabListResult{Tabs: out}, nil
}

// Open implements host/browser/tab/open and records an irreversible receipt.
func (b *Binding) Open(ctx context.Context, p protocol.BrowserTabOpenParams) (protocol.BrowserTabOpenResult, error) {
	tab, err := call(b, ctx, true, "open", func(c context.Context) (any, error) {
		if b.backend == nil {
			return nil, protocol.MustProtocolError(protocol.ErrBrowserUnavailable)
		}
		return b.backend.Open(c, p)
	})
	if err != nil {
		return protocol.BrowserTabOpenResult{}, err
	}
	return protocol.BrowserTabOpenResult{Tab: tab.(protocol.BrowserTab)}, nil
}

// Snapshot implements host/browser/tab/snapshot (no irreversible receipt).
func (b *Binding) Snapshot(ctx context.Context, p protocol.BrowserTabSnapshotParams) (protocol.BrowserTabSnapshotResult, error) {
	res, err := call(b, ctx, false, "snapshot", func(c context.Context) (any, error) {
		if b.backend == nil {
			return nil, protocol.MustProtocolError(protocol.ErrBrowserUnavailable)
		}
		return b.backend.Snapshot(c, p)
	})
	if err != nil {
		return protocol.BrowserTabSnapshotResult{}, err
	}
	return res.(protocol.BrowserTabSnapshotResult), nil
}

// Wait implements host/browser/tab/wait (no irreversible receipt).
func (b *Binding) Wait(ctx context.Context, p protocol.BrowserTabWaitParams) (protocol.BrowserTabWaitResult, error) {
	tab, err := call(b, ctx, false, "wait", func(c context.Context) (any, error) {
		if b.backend == nil {
			return nil, protocol.MustProtocolError(protocol.ErrBrowserUnavailable)
		}
		return b.backend.Wait(c, p)
	})
	if err != nil {
		return protocol.BrowserTabWaitResult{}, err
	}
	return protocol.BrowserTabWaitResult{Tab: tab.(protocol.BrowserTab)}, nil
}

// Act implements host/browser/tab/act and records an irreversible receipt.
func (b *Binding) Act(ctx context.Context, p protocol.BrowserTabActParams) (protocol.BrowserTabActResult, error) {
	tab, err := call(b, ctx, true, "act", func(c context.Context) (any, error) {
		if b.backend == nil {
			return nil, protocol.MustProtocolError(protocol.ErrBrowserUnavailable)
		}
		return b.backend.Act(c, p)
	})
	if err != nil {
		return protocol.BrowserTabActResult{}, err
	}
	return protocol.BrowserTabActResult{Tab: tab.(protocol.BrowserTab)}, nil
}

func call(b *Binding, ctx context.Context, irreversible bool, op string, fn func(context.Context) (any, error)) (any, error) {
	if b == nil {
		return nil, protocol.MustProtocolError(protocol.ErrBrowserUnavailable)
	}
	if err := b.admit(); err != nil {
		return nil, err
	}
	// Merge caller cancel, binding dispose, and a drain registration.
	opCtx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	b.metrics.InFlight.Add(1)
	defer b.metrics.InFlight.Add(-1)

	started := time.Now()
	receiptID := ""
	if irreversible {
		receiptID = b.beginReceipt(op, started)
	}

	result, err := fn(opCtx)
	// Drop results that arrived after generation was superseded.
	if err2 := b.admitPublished(); err2 != nil {
		b.metrics.StaleResponseDrop.Add(1)
		if irreversible && receiptID != "" {
			b.finishReceipt(receiptID, started, err2)
		}
		return nil, err2
	}
	if err != nil {
		b.noteError(err)
		if irreversible && receiptID != "" {
			// Crash/timeout/cancel after send is uncertain: still record.
			if shouldRecordUncertain(err) {
				b.finishReceipt(receiptID, started, err)
			}
		}
		return nil, err
	}
	if irreversible && receiptID != "" {
		b.finishReceipt(receiptID, started, nil)
	}
	return result, nil
}

func (b *Binding) admit() error {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		b.metrics.Cancels.Add(1)
		return protocol.MustProtocolError(protocol.ErrBrowserCancelled)
	}
	return b.admitPublished()
}

func (b *Binding) admitPublished() error {
	if b.owner == nil || b.generation == 0 {
		return nil
	}
	if b.owner.Gate.Published() != b.generation {
		b.metrics.AdmissionRejected.Add(1)
		return protocol.MustProtocolError(protocol.ErrStaleGeneration)
	}
	return nil
}

func (b *Binding) noteError(err error) {
	if err == nil {
		return
	}
	var pe *protocol.ProtocolError
	if errors.As(err, &pe) {
		switch pe.Reason {
		case protocol.ErrBrowserTimeout:
			b.metrics.Timeouts.Add(1)
		case protocol.ErrBrowserCancelled, protocol.ErrStaleGeneration:
			b.metrics.Cancels.Add(1)
		}
	}
	if errors.Is(err, context.Canceled) {
		b.metrics.Cancels.Add(1)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		b.metrics.Timeouts.Add(1)
	}
}

func shouldRecordUncertain(err error) bool {
	if err == nil {
		return false
	}
	var pe *protocol.ProtocolError
	if errors.As(err, &pe) {
		switch pe.Reason {
		case protocol.ErrInvalidParams, protocol.ErrCapabilityNotDeclared, protocol.ErrStaleGeneration,
			protocol.ErrBrowserUnavailable, protocol.ErrBrowserPermissionDenied,
			protocol.ErrBrowserTabNotFound, protocol.ErrBrowserOriginMismatch, protocol.ErrBrowserStaleRef:
			// Never reached the irreversible external side-effect, or clearly rejected.
			return pe.Reason == protocol.ErrBrowserUnavailable ||
				pe.Reason == protocol.ErrBrowserTimeout ||
				pe.Reason == protocol.ErrBrowserCancelled ||
				pe.Reason == protocol.ErrBrowserTabBusy ||
				pe.Reason == protocol.ErrInternal
		case protocol.ErrBrowserTimeout, protocol.ErrBrowserCancelled, protocol.ErrBrowserTabBusy, protocol.ErrInternal:
			return true
		}
	}
	// Transport / crash / cancel: uncertain.
	return true
}

func (b *Binding) beginReceipt(op string, started time.Time) string {
	if b.owner == nil || b.owner.Receipts == nil {
		return ""
	}
	seq := b.seq.Add(1)
	id := fmt.Sprintf("browser/%s/%s/%d/%d", b.pluginID, op, b.generation, seq)
	// Do not store URL, text, ref, or page content.
	b.owner.Receipts.Record(extension.EffectReceipt{
		ID:         id,
		Owner:      "plugin/" + b.pluginID,
		Generation: b.generation,
		Component:  "plugin/" + b.pluginID,
		Class:      extension.Irreversible,
		StartedAt:  started,
	})
	return id
}

func (b *Binding) finishReceipt(id string, started time.Time, err error) {
	if b.owner == nil || b.owner.Receipts == nil || id == "" {
		return
	}
	r := extension.EffectReceipt{
		ID:                 id,
		Owner:              "plugin/" + b.pluginID,
		Generation:         b.generation,
		Component:          "plugin/" + b.pluginID,
		Class:              extension.Irreversible,
		StartedAt:          started,
		CompletedAt:        time.Now(),
		CompensationStatus: "not_applicable",
	}
	if err != nil {
		// Uncertain outcome after send: mark error without claiming rollback.
		r.Error = "uncertain"
	}
	b.owner.Receipts.Record(r)
	b.metrics.IrreversibleReceipts.Add(1)
}

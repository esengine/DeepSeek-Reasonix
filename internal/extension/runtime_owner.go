package extension

import (
	"context"
	"fmt"
	"sync/atomic"
)

// RuntimeOwner owns generation-scoped lifecycle state for one logical
// controller/session lineage. Rebuilds reuse the owner; independent sessions
// receive independent owners so publishing one runtime never drains another.
type RuntimeOwner struct {
	Gate        *PublishGate
	Receipts    *ReceiptStore
	FilePriors  *FilePriorStore
	Messages    *MessageSendGuard
	HostStreams *HostStreamRegistry
	receiptSeq  atomic.Uint64
}

// NewRuntimeOwner returns an isolated runtime lifecycle owner.
func NewRuntimeOwner() *RuntimeOwner {
	priors := NewFilePriorStore()
	messages := NewMessageSendGuard()
	receipts := newReceiptStore(defaultReceiptGenerationLimit, defaultReceiptPerGenerationLimit, func(r EffectReceipt) {
		if r.Class == Compensatable {
			priors.Forget(r.ID)
		}
		messages.ForgetReceipt(r)
	})
	gate := newPublishGate(receipts)
	owner := &RuntimeOwner{
		Gate:       gate,
		Receipts:   receipts,
		FilePriors: priors,
		Messages:   messages,
	}
	owner.HostStreams = NewHostStreamRegistry(gate)
	return owner
}

// DefaultRuntimeOwner preserves package-level compatibility for callers that
// have not yet supplied an explicit owner. Product boot paths use isolated
// owners instead.
var DefaultRuntimeOwner = NewRuntimeOwner()

var defaultRuntimeOwnerFallbacks atomic.Uint64

// RuntimeOwnerOrDefault returns owner when it is explicitly bound and records
// compatibility fallbacks when a caller has not supplied one. Product boot
// paths bind an owner before constructing a runtime; the counter makes missed
// wiring observable in doctor diagnostics instead of silently sharing state.
func RuntimeOwnerOrDefault(owner *RuntimeOwner) *RuntimeOwner {
	if owner != nil {
		return owner
	}
	defaultRuntimeOwnerFallbacks.Add(1)
	return DefaultRuntimeOwner
}

// RuntimeOwnerFallbackCount returns the number of process-local compatibility
// owner fallbacks observed since startup.
func RuntimeOwnerFallbackCount() uint64 {
	return defaultRuntimeOwnerFallbacks.Load()
}

// ContextWithRuntimeOwner binds owner to provider/agent work derived from ctx.
func ContextWithRuntimeOwner(ctx context.Context, owner *RuntimeOwner) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if owner == nil {
		return ctx
	}
	return context.WithValue(ctx, runtimeOwnerContextKey{}, owner)
}

// RuntimeOwnerFromContext returns the bound owner, falling back to the package
// compatibility owner for callers outside the product boot path. The fallback
// is counted for doctor diagnostics.
func RuntimeOwnerFromContext(ctx context.Context) *RuntimeOwner {
	if ctx != nil {
		if owner, ok := ctx.Value(runtimeOwnerContextKey{}).(*RuntimeOwner); ok && owner != nil {
			return owner
		}
	}
	return RuntimeOwnerOrDefault(nil)
}

type runtimeOwnerContextKey struct{}

// RecordProviderSubmit records one irreversible provider request.
func (o *RuntimeOwner) RecordProviderSubmit(generation uint64, streamID, owner string) {
	o = RuntimeOwnerOrDefault(o)
	o.Receipts.Record(EffectReceipt{
		ID:                 "provider-submit:" + streamID,
		Owner:              owner,
		Generation:         generation,
		Class:              Irreversible,
		CompensationStatus: "not_applicable",
	})
}

// RecordMessageSentOnce records a user-visible send exactly once per
// generation/message pair in this runtime lineage.
func (o *RuntimeOwner) RecordMessageSentOnce(generation uint64, messageID, owner string) bool {
	o = RuntimeOwnerOrDefault(o)
	if !o.Messages.TryRecord(generation, messageID) {
		return false
	}
	o.Receipts.Record(EffectReceipt{
		ID:                 "message-sent:" + messageID,
		Owner:              owner,
		Component:          messageID,
		Generation:         generation,
		Class:              Irreversible,
		CompensationStatus: "not_applicable",
	})
	return true
}

// RecordMessageSent records a user-visible send without applying deduplication.
func (o *RuntimeOwner) RecordMessageSent(generation uint64, messageID, owner string) {
	o = RuntimeOwnerOrDefault(o)
	o.Receipts.Record(EffectReceipt{
		ID:                 "message-sent:" + messageID,
		Owner:              owner,
		Generation:         generation,
		Class:              Irreversible,
		CompensationStatus: "not_applicable",
	})
}

// RecordFileWrite captures prior state under a unique receipt ID. Repeated
// writes to the same path never overwrite an earlier generation's evidence.
func (o *RuntimeOwner) RecordFileWrite(path string, hadPrior bool, prior []byte) string {
	o = RuntimeOwnerOrDefault(o)
	gen := o.Gate.Published()
	id := fmt.Sprintf("file-write:%d:%d", gen, o.receiptSeq.Add(1))
	retained := o.FilePriors.Capture(id, path, prior, hadPrior)
	status := "prior_captured"
	if !retained {
		status = "prior_truncated"
	}
	o.Receipts.Record(EffectReceipt{
		ID:                 id,
		Owner:              "write_file",
		Generation:         gen,
		Class:              Compensatable,
		CompensationStatus: status,
		Error:              fmt.Sprintf("prior_bytes=%d retained=%t", len(prior), retained),
	})
	return id
}

// ApplyFileWriteCompensation restores prior file state and updates this
// lineage's receipt without touching another runtime owner.
func (o *RuntimeOwner) ApplyFileWriteCompensation(receiptID string) error {
	o = RuntimeOwnerOrDefault(o)
	if err := o.FilePriors.Compensate(receiptID); err != nil {
		o.Receipts.Record(EffectReceipt{
			ID:                 receiptID,
			Class:              Compensatable,
			CompensationStatus: "failed",
			Error:              err.Error(),
		})
		return err
	}
	o.FilePriors.Forget(receiptID)
	o.Receipts.Record(EffectReceipt{
		ID:                 receiptID,
		Class:              Compensatable,
		CompensationStatus: "applied",
	})
	return nil
}

// DecideResume evaluates recovery evidence owned by this runtime lineage.
func (o *RuntimeOwner) DecideResume(generation uint64) ResumeDecision {
	o = RuntimeOwnerOrDefault(o)
	return DecideResume(o.Receipts, generation)
}

// AssessRecoverability evaluates recovery evidence owned by this lineage.
func (o *RuntimeOwner) AssessRecoverability(generation uint64) Recoverability {
	o = RuntimeOwnerOrDefault(o)
	return o.Receipts.AssessRecoverability(generation)
}

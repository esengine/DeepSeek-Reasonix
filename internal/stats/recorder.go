package stats

import (
	"context"
	"strings"
	"sync"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

// Recorder is a passthrough event.Sink that snapshots token usage (event.Usage)
// and completed turns (event.TurnDone) into the daily stats files. It observes
// only; it never alters the event stream.
//
// Wire it around the frontend sink at the boot layer so every entry point
// (desktop, CLI, serve) records consistently; Source distinguishes them.
type Recorder struct {
	inner      event.Sink
	writer     *Writer
	dispatcher *recordDispatcher
	source     string
}

var _ event.OptionalSinkCapabilities = (*Recorder)(nil)

const recorderQueueSize = 2048

type dispatchItem struct {
	record record
	flush  chan struct{}
}

// recordDispatcher keeps filesystem latency off provider/UI event goroutines.
// Dispatchers are shared per state directory, so controller rebuilds do not
// create one goroutine per recorder instance.
type recordDispatcher struct {
	writer *Writer
	queue  chan dispatchItem
}

var recorderDispatchers = struct {
	sync.Mutex
	byDir map[string]*recordDispatcher
}{byDir: map[string]*recordDispatcher{}}

func dispatcherFor(writer *Writer) *recordDispatcher {
	if writer == nil || writer.dir == "" {
		return nil
	}
	recorderDispatchers.Lock()
	defer recorderDispatchers.Unlock()
	if dispatcher := recorderDispatchers.byDir[writer.dir]; dispatcher != nil {
		return dispatcher
	}
	dispatcher := &recordDispatcher{writer: writer, queue: make(chan dispatchItem, recorderQueueSize)}
	recorderDispatchers.byDir[writer.dir] = dispatcher
	go dispatcher.run()
	return dispatcher
}

func existingDispatcher(dir string) *recordDispatcher {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	recorderDispatchers.Lock()
	defer recorderDispatchers.Unlock()
	return recorderDispatchers.byDir[dir]
}

func (d *recordDispatcher) run() {
	for item := range d.queue {
		if item.flush != nil {
			close(item.flush)
			continue
		}
		_ = d.writer.Append(item.record)
	}
}

func (d *recordDispatcher) enqueue(rec record) {
	if d == nil {
		return
	}
	// Statistics are observational. A full queue may lose a record, but it must
	// never apply backpressure to model streaming or turn completion.
	select {
	case d.queue <- dispatchItem{record: rec}:
	default:
	}
}

func (d *recordDispatcher) flush(ctx context.Context) error {
	if d == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	select {
	case d.queue <- dispatchItem{flush: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewRecorder wraps inner with usage recording. source labels every record
// (desktop/cli/serve/...); an empty source keeps records unlabelled.
func NewRecorder(inner event.Sink, dir, source string) *Recorder {
	writer := NewWriter(dir)
	writer.usage = managerForUsage(writer.dir)
	return &Recorder{
		inner: inner, writer: writer, dispatcher: dispatcherFor(writer), source: strings.TrimSpace(source),
	}
}

// Emit forwards user-visible events unchanged, then queues any usage/turn
// record without waiting for filesystem I/O. Request-only usage is internal
// accounting for failed provider calls, so it is persisted without surfacing a
// zero-token receipt in the wrapped frontend.
func (r *Recorder) Emit(e event.Event) {
	requestOnly := e.Kind == event.Usage && e.Usage != nil && e.Usage.TotalTokens <= 0 && e.Usage.RequestCount > 0
	if r != nil && r.inner != nil && !requestOnly {
		r.inner.Emit(e)
	}
	if r != nil && r.writer != nil && e.Kind == event.Usage {
		r.recordUsage(e)
	} else if r != nil && r.writer != nil && e.Kind == event.GuardianAssessment && e.Guardian.Usage != nil {
		r.recordProviderUsage(e.ModelRef, e.Guardian.Usage, nil, "")
	} else if r != nil && r.writer != nil && e.Kind == event.TurnDone {
		r.recordTurnCompletion()
	}
}

// RecordTurnCompletion records synchronous controller runs that deliberately do
// not emit TurnDone into the UI event stream.
func (r *Recorder) RecordTurnCompletion() {
	r.recordTurnCompletion()
	if r != nil {
		event.RecordTurnCompletion(r.inner)
	}
}

func (r *Recorder) recordTurnCompletion() {
	if r == nil || r.dispatcher == nil {
		return
	}
	r.dispatcher.enqueue(record{Timestamp: time.Now(), Source: r.source, Turn: true})
}

// Flush waits until records already accepted by this recorder's shared queue
// have been written. Production event paths never call Flush; it exists for
// shutdown/verification boundaries that can explicitly tolerate waiting.
func (r *Recorder) Flush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if err := r.dispatcher.flush(ctx); err != nil {
		return err
	}
	if r.writer != nil && r.writer.usage != nil {
		if catalog := r.writer.usage.catalog.Load(); catalog != nil {
			return catalog.Flush(ctx)
		}
	}
	return nil
}

// Flush waits for records already queued for dir. It is primarily useful when
// a caller must read its own just-recorded statistics deterministically.
func Flush(ctx context.Context, dir string) error {
	dir = strings.TrimSpace(dir)
	if err := existingDispatcher(dir).flush(ctx); err != nil {
		return err
	}
	if manager := existingUsageManager(dir); manager != nil {
		if catalog := manager.catalog.Load(); catalog != nil {
			return catalog.Flush(ctx)
		}
	}
	return nil
}

// RecordReadinessAudit forwards audit receipts to the wrapped sink.
func (r *Recorder) RecordReadinessAudit(a evidence.ReadinessAudit) {
	event.RecordReadinessAudit(r.inner, a)
}

// RecordProtocolRecovery preserves the wrapped sink's audit capability.
func (r *Recorder) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	event.RecordProtocolRecovery(r.inner, a)
}

// RecordContractShadow preserves the wrapped sink's audit capability.
func (r *Recorder) RecordContractShadow(a event.ContractShadowAudit) {
	event.RecordContractShadow(r.inner, a)
}

// RecordCompletionReport preserves the wrapped sink's audit capability.
func (r *Recorder) RecordDelegationAudit(a evidence.DelegationAudit) {
	event.RecordDelegationAudit(r.inner, a)
}

func (r *Recorder) RecordCompletionReport(a event.CompletionReportAudit) {
	event.RecordCompletionReport(r.inner, a)
}

// RecordOutcomeProgress preserves the wrapped sink's audit capability.
func (r *Recorder) RecordOutcomeProgress(sample evidence.OutcomeSample) {
	event.RecordOutcomeProgress(r.inner, sample)
}

// RecordMemoryRecall preserves the wrapped sink's audit capability.
func (r *Recorder) RecordMemoryRecall(a event.MemoryRecallAudit) {
	event.RecordMemoryRecall(r.inner, a)
}

// RecordDelegationAdmission preserves the wrapped sink's audit capability.
func (r *Recorder) RecordDelegationAdmission(a event.DelegationAdmissionAudit) {
	event.RecordDelegationAdmission(r.inner, a)
}

func (r *Recorder) RecordWorkspaceMutation(m event.WorkspaceMutation) {
	event.RecordWorkspaceMutation(r.inner, m)
}

func (r *Recorder) RecordRunBudget(sample event.RunBudgetSample) {
	event.RecordRunBudget(r.inner, sample)
}

func (r *Recorder) recordUsage(e event.Event) {
	r.recordProviderUsage(e.ModelRef, e.Usage, e.CostQuote, e.UsageSource)
}

func (r *Recorder) recordProviderUsage(modelRef string, usage *provider.Usage, quote *billing.CostQuote, usageSource string) {
	if usage == nil || (usage.TotalTokens <= 0 && usage.RequestCount <= 0) {
		return
	}
	// Recording is best-effort: a stats file failure (disk full, permissions)
	// must never interrupt the event stream, matching telemetry's append idiom.
	rec := record{
		Timestamp:   time.Now(),
		ModelRef:    modelRef,
		Source:      r.source,
		Prompt:      usage.PromptTokens,
		Completion:  usage.CompletionTokens,
		Reasoning:   usage.ReasoningTokens,
		CacheHit:    usage.CacheHitTokens,
		CacheMiss:   usage.CacheMissTokens,
		Total:       usage.TotalTokens,
		Requests:    usageRequestCount(usage),
		UsageSource: strings.TrimSpace(usageSource),
	}
	if quote != nil {
		rec.CostAmount = quote.Original.Amount
		rec.CostCurrency = quote.Original.Currency
		rec.PricingFingerprint = quote.PricingFingerprint
		rec.RateDate = quote.RateDate
		rec.RateBand = quote.RateBand
		rec.RatedAt = quote.RatedAt
		rec.IncompleteReason = quote.IncompleteReason
		rec.BillingMode = quote.BillingMode
		rec.CostEstimated = quote.Estimated
		rec.LegacyEstimate = quote.LegacyEstimate
		costComplete := quote.CostComplete
		displayComplete := quote.DisplayComplete
		rec.CostComplete = &costComplete
		rec.DisplayComplete = &displayComplete
		rec.DisplayStatus = quote.DisplayStatus
		rec.AggregateMode = quote.AggregateMode
		for _, total := range quote.OriginalTotals {
			rec.OriginalTotals = append(rec.OriginalTotals, total.Currency+":"+total.Amount)
		}
		if quote.Selected != nil {
			rec.SelectedAmount = quote.Selected.Amount
			rec.SelectedCurrency = quote.Selected.Currency
			rec.SelectedCost = quote.Selected.Float64()
		}
		if v, ok := quote.Valuations["CNY"]; ok {
			rec.ValuationCNY = v.Money.Amount
		}
		if v, ok := quote.Valuations["USD"]; ok {
			rec.ValuationUSD = v.Money.Amount
		}
	}
	r.dispatcher.enqueue(rec)
}

func usageRequestCount(usage *provider.Usage) int {
	if usage != nil && usage.RequestCount > 0 {
		return usage.RequestCount
	}
	return 1
}

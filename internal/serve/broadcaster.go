package serve

import (
	"encoding/json"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

type subscription struct {
	all bool
}

const (
	subscriberBufferSize      = 128
	subscriberPriorityReserve = 32
)

// Broadcaster is the event.Sink the controllers emit to in server mode. It
// marshals each event once and fans it out to every connected SSE subscriber.
// A slow subscriber's buffer is allowed to drop rather than back-pressure the
// agent goroutine — a browser that can't keep up loses intermediate frames, not
// the whole session (it can refetch /history).
type Broadcaster struct {
	mu              sync.Mutex
	subs            map[chan []byte]subscription
	ledgers         map[string]*billing.Ledger
	current         string
	displayCurrency string
}

// NewBroadcaster returns an empty Broadcaster ready to accept subscribers.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs:    map[chan []byte]subscription{},
		ledgers: map[string]*billing.Ledger{},
	}
}

// SetDisplayCurrency rebinds the session ledger to a stored valuation. Empty
// keeps automatic mode: a single original currency is selected and mixed
// currencies remain buckets.
func (b *Broadcaster) SetDisplayCurrency(currency string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.displayCurrency = billing.NormalizeCurrency(currency)
	b.mu.Unlock()
}

// SetCurrentSession records the controller shown by current-only subscribers.
// Untagged events remain compatible and are attributed to this session.
func (b *Broadcaster) SetCurrentSession(path string) {
	if b == nil {
		return
	}
	path = agent.CanonicalSessionPath(path)
	b.mu.Lock()
	b.current = path
	b.mu.Unlock()
}

// CurrentSession reports the session currently selected by Serve.
func (b *Broadcaster) CurrentSession() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.current
}

// ResetSession clears the current session usage ledger for legacy callers.
func (b *Broadcaster) ResetSession() {
	b.ResetSessionPath("")
}

// ResetSessionPath clears one session ledger without affecting detached
// sessions. Empty selects the current session.
func (b *Broadcaster) ResetSessionPath(path string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if path == "" {
		path = b.current
	} else {
		path = agent.CanonicalSessionPath(path)
	}
	delete(b.ledgers, path)
	b.mu.Unlock()
}

// SessionCostQuote returns the current aggregate quote without repricing.
func (b *Broadcaster) SessionCostQuote() billing.CostQuote {
	return b.SessionCostQuoteFor("")
}

// SessionCostQuoteFor returns one session's aggregate quote. Empty selects
// the current session so existing single-session callers keep their contract.
func (b *Broadcaster) SessionCostQuoteFor(path string) billing.CostQuote {
	if b == nil {
		return billing.AggregateQuotes(nil, "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ledgerLocked(path).Total(b.displayCurrency)
}

func (b *Broadcaster) ledgerLocked(path string) *billing.Ledger {
	if path == "" {
		path = b.current
	} else {
		path = agent.CanonicalSessionPath(path)
	}
	ledger := b.ledgers[path]
	if ledger == nil {
		ledger = billing.NewLedger()
		b.ledgers[path] = ledger
	}
	return ledger
}

// Emit marshals the event to JSON and delivers it to every subscriber. Drops to
// a subscriber whose buffer is full rather than blocking. A marshal failure is
// dropped silently — one bad event shouldn't stall the stream.
func (b *Broadcaster) Emit(e event.Event) {
	if e.SessionPath != "" {
		e.SessionPath = agent.CanonicalSessionPath(e.SessionPath)
	}
	wired := eventwire.ToWire(e)
	b.mu.Lock()
	observedCurrent := b.current
	b.mu.Unlock()
	wired.SessionCurrent = e.SessionPath != "" && e.SessionPath == observedCurrent
	data, err := json.Marshal(wired)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current != observedCurrent {
		wired.SessionCurrent = e.SessionPath != "" && e.SessionPath == b.current
		data, err = json.Marshal(wired)
		if err != nil {
			return
		}
	}
	if e.Kind == event.Usage && e.Usage != nil && e.CostQuote != nil {
		b.ledgerLocked(e.SessionPath).Add(*e.CostQuote, billing.UsageTokens{
			PromptTokens: e.Usage.PromptTokens, CompletionTokens: e.Usage.CompletionTokens,
			CacheHitTokens: e.Usage.CacheHitTokens, CacheMissTokens: e.Usage.CacheMissTokens,
			CacheWriteTokens: e.Usage.CacheWriteTokens, CacheWriteBilledTokens: e.Usage.CacheWriteBilledTokens,
			Estimated: e.Usage.Estimated,
		}, time.Now().UTC())
	}
	for ch, sub := range b.subs {
		if !sub.all && e.SessionPath != "" && e.SessionPath != b.current {
			continue
		}
		enqueueSubscriberFrame(ch, data, e.Kind)
	}
}

// EmitTo delivers an event only to the supplied subscriber. It is used for
// connection-local recovery frames, such as replaying a prompt to a browser
// that attached after the original event was emitted. Normal runtime events
// should continue to use Emit so every subscriber receives them.
func (b *Broadcaster) EmitTo(target <-chan []byte, e event.Event) {
	if e.SessionPath != "" {
		e.SessionPath = agent.CanonicalSessionPath(e.SessionPath)
	}
	wired := eventwire.ToWire(e)
	b.mu.Lock()
	observedCurrent := b.current
	b.mu.Unlock()
	wired.SessionCurrent = e.SessionPath != "" && e.SessionPath == observedCurrent
	data, err := json.Marshal(wired)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current != observedCurrent {
		wired.SessionCurrent = e.SessionPath != "" && e.SessionPath == b.current
		data, err = json.Marshal(wired)
		if err != nil {
			return
		}
	}
	for ch, sub := range b.subs {
		if (<-chan []byte)(ch) != target {
			continue
		}
		if !sub.all && e.SessionPath != "" && e.SessionPath != b.current {
			return
		}
		enqueueSubscriberFrame(ch, data, e.Kind)
		return
	}
}

// eventIsPriority keeps lifecycle and terminal frames out of the high-volume
// delta budget. Slow subscribers may recover text/history over HTTP, but they
// must still learn that a turn, prompt, or foreground-session transition ended.
func eventIsPriority(kind event.Kind) bool {
	switch kind {
	case event.Reasoning, event.Text, event.ToolProgress, event.StreamAttempt:
		return false
	default:
		return true
	}
}

// eventMustReachSubscriber identifies lifecycle truth that cannot be recovered
// by refetching history. The reserved queue budget protects these frames from
// deltas; if other priority traffic also exhausts that budget, the newest
// terminal/routing frame evicts a recoverable queued frame instead of vanishing.
func eventMustReachSubscriber(kind event.Kind, data []byte) bool {
	switch kind {
	case event.TurnDone, event.SessionChanged:
		return true
	case event.Notice:
		return wireFrameMustReachSubscriber(data)
	default:
		return false
	}
}

func enqueueSubscriberFrame(ch chan []byte, data []byte, kind event.Kind) {
	priority := eventIsPriority(kind)
	if !priority && len(ch) >= cap(ch)-subscriberPriorityReserve {
		return
	}
	select {
	case ch <- data:
		return
	default:
		// A slow subscriber exhausted even the priority reserve. Ordinary
		// priority events remain lossy, but lifecycle truth gets one slot by
		// evicting an older frame while Broadcaster.mu serializes producers.
		if !eventMustReachSubscriber(kind, data) || !evictRecoverableSubscriberFrame(ch) {
			return
		}
	}
	// Broadcaster.mu excludes every producer, and eviction leaves at least one
	// slot. A concurrent consumer can only create more capacity, so this send is
	// bounded while guaranteeing the terminal frame is retained.
	ch <- data
}

func evictRecoverableSubscriberFrame(ch chan []byte) bool {
	queued := len(ch)
	if queued < cap(ch) {
		return true
	}
	frames := make([][]byte, 0, queued)
drain:
	for range queued {
		select {
		case frame := <-ch:
			frames = append(frames, frame)
		default:
			break drain
		}
	}
	if len(frames) == 0 {
		return true
	}
	evict := -1
	for i, frame := range frames {
		if !wireFrameMustReachSubscriber(frame) {
			evict = i
			break
		}
	}
	if evict < 0 {
		// A bounded queue cannot retain an unbounded run of terminal frames.
		// Prefer the latest lifecycle truth over an older one in that degenerate
		// case; normal saturation always finds a recoverable delta/status frame.
		evict = 0
	}
	for i, frame := range frames {
		if i != evict {
			ch <- frame
		}
	}
	return true
}

func wireFrameMustReachSubscriber(data []byte) bool {
	var frame eventwire.Event
	if json.Unmarshal(data, &frame) != nil {
		return false
	}
	return frame.Kind == "turn_done" || frame.Kind == "session_changed" ||
		frame.Kind == "notice" && frame.Code == event.NoticeCodeBackgroundJobFinished
}

// Subscribe registers a new SSE client and returns its channel plus an
// unsubscribe func the handler must call (defer) when the client disconnects.
func (b *Broadcaster) Subscribe() (<-chan []byte, func()) {
	return b.subscribe(false)
}

// SubscribeAll receives tagged frames from current and detached sessions.
// Desktop uses it to maintain per-session runtime state; browser clients keep
// using Subscribe and see only the selected session.
func (b *Broadcaster) SubscribeAll() (<-chan []byte, func()) {
	return b.subscribe(true)
}

func (b *Broadcaster) subscribe(all bool) (<-chan []byte, func()) {
	ch := make(chan []byte, subscriberBufferSize)
	b.mu.Lock()
	b.subs[ch] = subscription{all: all}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}

// Subscribers reports the current connection count (for diagnostics/tests).
func (b *Broadcaster) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

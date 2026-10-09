package serve

import (
	"encoding/json"
	"reasonix/internal/contract/pricing"
	"sync"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

// Broadcaster is the event.Sink the controller emits to in server mode: one
// marshal, fanned out to every subscriber. A slow one loses frames rather than
// back-pressuring the agent, so frames worth keeping are numbered and held for
// a while — a drop becomes something the client can ask for again, through the
// same numbers on either transport. See SubscribeFrom.
type Broadcaster struct {
	mu              sync.Mutex
	subs            map[*subscriber]struct{}
	ledger          *pricing.Ledger
	displayCurrency string
	// The transport's, not the session's: numbering outlives /new and /resume, so
	// resuming across one is told to rebuild rather than handed another
	// conversation's frames under numbers it already has.
	seq                    int64
	replay                 replayLog
	initialNoticesReplayed bool
}

// NewBroadcaster returns an empty Broadcaster ready to accept subscribers.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: map[*subscriber]struct{}{}, ledger: pricing.NewLedger()}
}

// droppable reports whether losing this frame degrades the view rather than
// breaking it. A streaming delta is superseded by the text after it and the
// full version is in /history; a prompt, a result or a turn boundary has no
// successor carrying it, and a frontend that misses one waits forever on a
// state that already changed.
func droppable(k event.Kind) bool {
	switch k {
	case event.Text, event.Reasoning, event.ToolProgress, event.CompactionProgress:
		return true
	}
	return false
}

// SetDisplayCurrency rebinds the session ledger to a stored valuation. Empty
// keeps automatic mode: a single original currency is selected and mixed
// currencies remain buckets.
func (b *Broadcaster) SetDisplayCurrency(currency string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.displayCurrency = pricing.NormalizeCurrency(currency)
	b.mu.Unlock()
}

// DisplayCurrency is the valuation this session reads costs in. Empty is
// automatic mode — which is also what a wallet wants: no preference means show
// it in the currency the provider keeps it in.
func (b *Broadcaster) DisplayCurrency() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.displayCurrency
}

// ResetSession clears the usage ledger for /new, /resume and /fork, and drops
// the replay tail with it: those frames describe a conversation no client is
// looking at any more. The sequence keeps counting, so a client resuming across
// the switch lands before the log's first frame and is told to refetch.
func (b *Broadcaster) ResetSession() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.ledger = pricing.NewLedger()
	b.replay.reset()
	b.initialNoticesReplayed = false
	b.mu.Unlock()
}

// SessionCostQuote returns the current aggregate quote without repricing.
func (b *Broadcaster) SessionCostQuote() pricing.CostQuote {
	if b == nil {
		return pricing.AggregateQuotes(nil, "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ledger == nil {
		b.ledger = pricing.NewLedger()
	}
	return b.ledger.Total(b.displayCurrency)
}

// Emit numbers the event, records it for replay when it is one a client cannot
// afford to miss, and hands it to every subscriber. Never blocks: a subscriber
// that has fallen behind loses frames, and the number is what lets it notice
// and ask for them back. A marshal failure is dropped silently — one bad event
// shouldn't stall the stream.
func (b *Broadcaster) Emit(e event.Event) {
	drop := droppable(e.Kind)
	b.mu.Lock()
	defer b.mu.Unlock()
	w := eventwire.ToWire(e)
	if !drop {
		b.seq++
		w.Seq = b.seq
	}
	data, err := json.Marshal(w)
	if err != nil {
		return
	}
	if !drop {
		b.replay.add(b.seq, e.Kind, data)
	}
	if e.Kind == event.Notice && e.Code == event.NoticeCodeDisplayCurrency {
		b.displayCurrency = pricing.NormalizeCurrency(e.Detail)
	}
	if e.Kind == event.Usage && e.Usage != nil && e.CostQuote != nil {
		if b.ledger == nil {
			b.ledger = pricing.NewLedger()
		}
		b.ledger.Add(*e.CostQuote, pricing.UsageTokens{
			PromptTokens: e.Usage.PromptTokens, CompletionTokens: e.Usage.CompletionTokens,
			CacheHitTokens: e.Usage.CacheHitTokens, CacheMissTokens: e.Usage.CacheMissTokens,
			CacheWriteTokens: e.Usage.CacheWriteTokens, CacheWriteBilledTokens: e.Usage.CacheWriteBilledTokens,
			Estimated: e.Usage.Estimated,
		}, time.Now().UTC())
	}
	for s := range b.subs {
		s.push(Frame{Seq: w.Seq, Data: data}, drop)
	}
}

// EmitTo delivers an event only to the supplied subscriber: connection-local
// recovery, such as replaying a prompt to a browser that attached after it was
// asked. Unnumbered by definition — replaying it to a second client would be
// replaying a conversation that client never had. Runtime events use Emit.
func (b *Broadcaster) EmitTo(target <-chan Frame, e event.Event) {
	data, err := json.Marshal(eventwire.ToWire(e))
	if err != nil {
		return
	}
	drop := droppable(e.Kind)
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		if (<-chan Frame)(s.ch) != target {
			continue
		}
		s.push(Frame{Data: data}, drop)
		return
	}
}

// Subscribe registers a client that wants the stream from here on.
func (b *Broadcaster) Subscribe() (<-chan Frame, func()) {
	return b.SubscribeFrom(0)
}

// SubscribeFrom registers a client resuming after frame `after` — an SSE
// reconnect carries Last-Event-ID, the shell's bus carries what it last
// forwarded. What the log still holds goes out before the live stream and
// before Emit can see the subscriber, so the resume has no seam. A gap the log
// cannot close is announced instead: the first sequence the client can trust.
func (b *Broadcaster) SubscribeFrom(after int64) (<-chan Frame, func()) {
	return b.subscribeFrom(after, false)
}

// SubscribeInitialNotices attaches a window without a resume cursor. Only the
// first such window for a session receives notices emitted before it connected.
// They are unnumbered copies: skipped non-notice history must not look like a
// gap that prompts the client to replay it.
func (b *Broadcaster) SubscribeInitialNotices() (<-chan Frame, func()) {
	return b.subscribeFrom(0, true)
}

func (b *Broadcaster) subscribeFrom(after int64, initialNotices bool) (<-chan Frame, func()) {
	s := newSubscriber()
	b.mu.Lock()
	if initialNotices && !b.initialNoticesReplayed {
		b.initialNoticesReplayed = true
		for _, f := range b.replay.frames {
			if f.kind != event.Notice {
				continue
			}
			var notice eventwire.Event
			if json.Unmarshal(f.data, &notice) != nil {
				continue
			}
			notice.Seq = 0
			data, err := json.Marshal(notice)
			if err == nil {
				s.push(Frame{Data: data}, false)
			}
		}
	} else if after > 0 && after < b.seq {
		missed, complete := b.replay.since(after)
		if gap, from := b.gapFrame(missed); !complete && gap != nil {
			s.push(Frame{Seq: from, Data: gap}, false)
		}
		for _, f := range missed {
			s.push(Frame{Seq: f.seq, Data: f.data}, false)
		}
	}
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s.ch, func() {
		b.mu.Lock()
		_, ok := b.subs[s]
		delete(b.subs, s)
		b.mu.Unlock()
		if ok {
			s.close()
		}
	}
}

// Watermark is the last numbered frame. A client compares it against what it
// has seen to notice that the frame ending a turn never arrived — the one case
// no later frame would reveal on its own.
func (b *Broadcaster) Watermark() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Replay answers what a client missed after a given frame, and whether the log
// could still account for all of it. It serves the transport that has no
// connection to re-establish: the desktop shell's bus delivers frames without
// anything resembling a reconnect, so its client asks in a request instead of
// carrying Last-Event-ID into a new stream. Both arrive at the same log.
func (b *Broadcaster) Replay(after int64) (frames []json.RawMessage, complete bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if after >= b.seq {
		return nil, true
	}
	missed, ok := b.replay.since(after)
	for _, f := range missed {
		frames = append(frames, json.RawMessage(f.data))
	}
	return frames, ok
}

// gapFrame tells a client where the recoverable stream starts. Seq is the first
// frame it is about to receive (or the current watermark when the log has
// nothing left), so everything before that has to come from the transcript.
func (b *Broadcaster) gapFrame(have []replayFrame) (data []byte, from int64) {
	from = b.seq
	if len(have) > 0 {
		from = have[0].seq
	}
	data, err := json.Marshal(eventwire.Event{Kind: "stream_gap", Seq: from})
	if err != nil {
		return nil, from
	}
	return data, from
}

// Subscribers reports the current connection count (for diagnostics/tests).
func (b *Broadcaster) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

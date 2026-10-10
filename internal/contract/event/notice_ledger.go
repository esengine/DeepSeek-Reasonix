package event

import "sync"

// NoticeLedger remembers which notices a lineage's previous build showed, so a
// rebuild that re-derives the same notice from the same state says nothing.
// A notice's identity is its level, code, text and detail.
type NoticeLedger struct {
	mu   sync.Mutex
	seen map[noticeKey]struct{}
}

type noticeKey struct {
	level              Level
	code, text, detail string
}

// Reset forgets everything shown, for when the surface that showed it is
// gone: the next build emits every notice that still holds.
func (l *NoticeLedger) Reset() {
	l.mu.Lock()
	l.seen = nil
	l.mu.Unlock()
}

// NewNoticeLedger returns an empty ledger: the first build emits everything.
func NewNoticeLedger() *NoticeLedger { return &NoticeLedger{} }

// Begin opens one build's window over sink. Notices that the previous
// successful build already showed are dropped; everything else, and every
// non-notice event, passes through.
func (l *NoticeLedger) Begin(sink Sink) *NoticeWindow {
	l.mu.Lock()
	defer l.mu.Unlock()
	return &NoticeWindow{AuditForwarder: AuditForwarder{Inner: sink}, ledger: l, inner: sink, prev: l.seen, cur: map[noticeKey]struct{}{}}
}

// NoticeWindow is the sink for one build. After Close it forwards unchanged,
// so the runtime that outlives the build keeps emitting freely.
type NoticeWindow struct {
	AuditForwarder
	ledger *NoticeLedger
	inner  Sink
	prev   map[noticeKey]struct{}

	mu     sync.Mutex
	cur    map[noticeKey]struct{}
	closed bool
}

func (w *NoticeWindow) Emit(e Event) {
	if e.Kind != Notice {
		w.inner.Emit(e)
		return
	}
	k := noticeKey{e.Level, e.Code, e.Text, e.Detail}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		w.inner.Emit(e)
		return
	}
	w.cur[k] = struct{}{}
	_, shown := w.prev[k]
	w.mu.Unlock()
	if !shown {
		w.inner.Emit(e)
	}
}

// Close ends the window. A successful build's notices replace the ledger, so a
// notice that disappears and later returns is shown again; a failed build
// leaves the old runtime serving, so what it showed is added to what was seen.
func (w *NoticeWindow) Close(ok bool) {
	w.mu.Lock()
	w.closed = true
	cur := w.cur
	w.mu.Unlock()
	w.ledger.mu.Lock()
	defer w.ledger.mu.Unlock()
	if ok {
		w.ledger.seen = cur
		return
	}
	merged := make(map[noticeKey]struct{}, len(w.ledger.seen)+len(cur))
	for k := range w.ledger.seen {
		merged[k] = struct{}{}
	}
	for k := range cur {
		merged[k] = struct{}{}
	}
	w.ledger.seen = merged
}

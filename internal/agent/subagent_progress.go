package agent

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/event"
)

// Sub-agent progress previews. A tracker per child run converts the child's
// Reasoning/Text/Notice/Retrying events into reserved ToolProgress channel
// events (event.SubagentProgress*Name) that local frontends render as progress
// cards. A shared merger per parent task group paces and bounds the previews:
// one pending slot per (child, channel), a 250ms merge window, and a group
// budget of 32 non-terminal preview events/sec round-robined across children
// so one hot sub-agent cannot starve the rest. The child's Message, and the
// child's own reasoning/text bodies, never leave the progress pipeline.

// subagentProgressPhase is one of the fixed states the status channel carries.
type subagentProgressPhase string

const (
	subagentPhaseQueued     subagentProgressPhase = "queued"
	subagentPhaseRunning    subagentProgressPhase = "running"
	subagentPhaseReasoning  subagentProgressPhase = "reasoning"
	subagentPhaseResponding subagentProgressPhase = "responding"
	subagentPhaseTool       subagentProgressPhase = "tool"
	subagentPhaseRetrying   subagentProgressPhase = "retrying"
	subagentPhaseCompleted  subagentProgressPhase = "completed"
	subagentPhasePartial    subagentProgressPhase = "partial"
	subagentPhaseFailed     subagentProgressPhase = "failed"
	subagentPhaseCancelled  subagentProgressPhase = "cancelled"
)

// Progress pacing and memory bounds. Preview slots merge for up to
// subagentProgressMergeWindow before one event per (child, channel) is emitted;
// a parent task group caps non-terminal preview events at
// subagentProgressGroupEventsPerSec, round-robined across children. Terminal
// events and the pre-terminal synchronous flush bypass both limits — the flush
// is inherently bounded by the per-child pending budget below.
const (
	subagentProgressMergeWindow       = 250 * time.Millisecond
	subagentProgressGroupEventsPerSec = 32
	subagentProgressGroupBurst        = subagentProgressGroupEventsPerSec

	// Per-child pending-send budget: reasoning/text/notice slots share 8 KiB,
	// with a per-channel cap so one channel cannot crowd out the response
	// preview. When the shared budget overflows, the notice slot is dropped
	// first, then reasoning, then text — each keeping a UTF-8-safe tail.
	subagentProgressMaxPendingBytes = 8 << 10
	subagentProgressReasoningCap    = 8 << 10
	subagentProgressTextCap         = 8 << 10
	subagentProgressNoticeCap       = 2 << 10
)

// progressClock isolates time so tests drive merge windows with a fake clock.
type progressClock interface {
	Now() time.Time
	NewTimer(d time.Duration) progressTimer
}

// progressTimer mirrors the *time.Timer surface the merger needs.
type progressTimer interface {
	C() <-chan time.Time
	Reset(d time.Duration) bool
	Stop() bool
}

type realProgressClock struct{}

func (realProgressClock) Now() time.Time { return time.Now() }

func (realProgressClock) NewTimer(d time.Duration) progressTimer {
	return realProgressTimer{t: time.NewTimer(d)}
}

type realProgressTimer struct{ t *time.Timer }

func (r realProgressTimer) C() <-chan time.Time        { return r.t.C }
func (r realProgressTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }
func (r realProgressTimer) Stop() bool                 { return r.t.Stop() }

// subagentProgressChannel identifies one preview channel.
type subagentProgressChannel int

const (
	subagentProgressChanReasoning subagentProgressChannel = iota
	subagentProgressChanText
	subagentProgressChanNotice
)

func (c subagentProgressChannel) name() string {
	switch c {
	case subagentProgressChanReasoning:
		return event.SubagentProgressReasoningName
	case subagentProgressChanText:
		return event.SubagentProgressTextName
	default:
		return event.SubagentProgressNoticeName
	}
}

func (c subagentProgressChannel) cap() int {
	switch c {
	case subagentProgressChanReasoning:
		return subagentProgressReasoningCap
	case subagentProgressChanText:
		return subagentProgressTextCap
	default:
		return subagentProgressNoticeCap
	}
}

// progressSlot is the single pending slot for one (child, channel): at most one
// unsent merged slice per child+channel, so pending preview memory is bounded
// by construction. dueAt is the earliest time the merged slice may be sent.
type progressSlot struct {
	buf       string
	truncated bool
	dirty     bool
	dueAt     time.Time
	lastSend  time.Time
}

// progressStatusSlot holds the latest unsent phase for one child. Ordinary
// phase transitions share the group preview budget with content previews (a
// fleet of phase-flapping children must not exceed the 32 events/s contract);
// only the initial queued/running states and the terminal event bypass it.
type progressStatusSlot struct {
	phase    subagentProgressPhase
	dirty    bool
	dueAt    time.Time
	lastSend time.Time
}

// subagentProgressMerger paces and bounds progress previews for one parent
// task group (a single task, a parallel_tasks call, or a fleet). It owns one
// flusher goroutine that emits due slots round-robin; every owner must Close it
// after all children finish so no timer or goroutine outlives the group.
type subagentProgressMerger struct {
	mu            sync.Mutex
	clock         progressClock
	sink          event.Sink // the same sink the group's dispatch events flow through
	groupParentID string     // the group's own call ID (progress events' ParentID)

	slots  map[string]map[subagentProgressChannel]*progressSlot
	status map[string]*progressStatusSlot
	order  []string // child IDs in registration order, for round-robin
	rr     int      // rotating scan start for fairness

	tokens     float64 // preview budget: subagentProgressGroupEventsPerSec
	lastRefill time.Time

	timer  progressTimer
	wake   chan struct{}
	done   chan struct{}
	wg     sync.WaitGroup
	closed bool

	// truncatedPending marks children whose buffered content was dropped by a
	// budget trim while no event carried the Truncated flag yet; the flag is
	// propagated to the next actually-emitted preview channel.
	truncatedPending map[string]bool
}

func newSubagentProgressMerger(clock progressClock, sink event.Sink, groupParentID string) *subagentProgressMerger {
	now := clock.Now()
	m := &subagentProgressMerger{
		clock:            clock,
		sink:             sink,
		groupParentID:    groupParentID,
		slots:            make(map[string]map[subagentProgressChannel]*progressSlot),
		status:           make(map[string]*progressStatusSlot),
		tokens:           subagentProgressGroupBurst,
		lastRefill:       now,
		wake:             make(chan struct{}, 1),
		done:             make(chan struct{}),
		timer:            clock.NewTimer(0),
		truncatedPending: make(map[string]bool),
	}
	m.wg.Add(1)
	go m.run()
	return m
}

// Close stops the flusher goroutine and drops any pending state. The owner
// calls it only after every child has finished (each child's finish flushed
// its own slots), so Close never discards a needed preview.
func (m *subagentProgressMerger) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.mu.Unlock()
	close(m.done)
	m.wg.Wait()
}

// directStatus sends a status event immediately (bypassing the merge slot and
// group budget) and records the send on the child's status slot so the next
// transition still merges for the 250ms window after this send. Used for the
// guaranteed-first states (queued/running); terminal events go through
// flushChild instead.
func (m *subagentProgressMerger) directStatus(childID string, phase subagentProgressPhase) {
	m.mu.Lock()
	st := m.status[childID]
	if st == nil {
		st = &progressStatusSlot{}
		m.status[childID] = st
		m.ensureOrderLocked(childID)
	}
	st.lastSend = m.clock.Now()
	st.dirty = false
	st.phase = phase
	m.mu.Unlock()
	parentID := m.groupParentID
	if parentID == childID {
		parentID = ""
	}
	m.sink.Emit(event.Event{
		Kind: event.ToolProgress,
		Tool: event.Tool{
			ID: childID, Name: event.SubagentProgressStatusName,
			ParentID: parentID, Output: string(phase),
		},
	})
}

// statusEvent queues a phase transition for a child. The first transition per
// child sends immediately; later transitions merge into the status slot.
func (m *subagentProgressMerger) statusEvent(childID string, phase subagentProgressPhase) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	st := m.status[childID]
	if st == nil {
		st = &progressStatusSlot{}
		m.status[childID] = st
		m.ensureOrderLocked(childID)
	}
	if !st.dirty {
		st.dirty = true
		// The first status send is immediate; later transitions merge for the
		// 250ms window after the previous send.
		dueAt := m.clock.Now()
		if !st.lastSend.IsZero() {
			if after := st.lastSend.Add(subagentProgressMergeWindow); after.After(dueAt) {
				dueAt = after
			}
		}
		st.dueAt = dueAt
	}
	st.phase = phase
	m.wakeLocked()
}

// deltaEvent appends a text delta to a child's preview slot. The slot is the
// only pending slice for that (child, channel); overflow keeps a UTF-8-safe
// tail and marks the round truncated.
func (m *subagentProgressMerger) deltaEvent(childID string, ch subagentProgressChannel, delta string) {
	if delta == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if _, ok := m.slots[childID]; !ok {
		m.slots[childID] = make(map[subagentProgressChannel]*progressSlot)
		m.ensureOrderLocked(childID)
	}
	sl := m.slots[childID][ch]
	if sl == nil {
		sl = &progressSlot{}
		m.slots[childID][ch] = sl
	}
	if !sl.dirty {
		sl.dirty = true
		sl.dueAt = m.clock.Now().Add(subagentProgressMergeWindow)
	}
	sl.buf += delta
	if len(sl.buf) > ch.cap() {
		sl.buf = utf8SafeTail(sl.buf, ch.cap())
		sl.truncated = true
	}
	m.trimToBudgetLocked(childID)
	m.wakeLocked()
}

// flushChild synchronously emits everything pending for the child and then the
// terminal status event. Terminal events bypass merge windows and the group
// budget; the flush is bounded by the per-child pending budget. Called by the
// tracker's finish before any terminal is delivered, and only once per child.
func (m *subagentProgressMerger) flushChild(childID string, terminal subagentProgressPhase, durationMs int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	st := m.status[childID]
	if st != nil && st.dirty {
		phase := st.phase
		st.dirty = false
		m.emitStatusLocked(childID, phase, 0)
	}
	for c := subagentProgressChanReasoning; c <= subagentProgressChanNotice; c++ {
		if sl := m.slots[childID][c]; sl != nil && sl.dirty {
			m.emitDeltaLocked(childID, c, sl)
		}
	}
	m.emitStatusLocked(childID, terminal, durationMs)
	// A budget trim that dropped content with no channel left to carry the
	// Truncated flag is surfaced as a truncated notice so frontends still know
	// some preview content was lost.
	if m.truncatedPending[childID] {
		m.emitToolProgressLocked(childID, event.SubagentProgressNoticeName, "", true, 0)
	}
	// Release per-child state; later events for this child are ignored by the
	// tracker's own done flag, and the flusher has nothing left to wake for.
	delete(m.status, childID)
	delete(m.slots, childID)
	delete(m.truncatedPending, childID)
	m.removeOrderLocked(childID)
}

// run is the merger's flusher loop: drain due slots, then sleep until the
// earliest deadline, a wake, or Close. The loop never holds the mutex while
// sleeping, so queueing trackers never block on it.
func (m *subagentProgressMerger) run() {
	defer m.wg.Done()
	defer m.timer.Stop()
	for {
		m.mu.Lock()
		for m.stepLocked() {
		}
		closed := m.closed
		clean := m.allCleanLocked()
		if !clean && !closed {
			d := m.nextDeadlineLocked()
			m.mu.Unlock()
			m.timer.Reset(d)
			select {
			case <-m.done:
				return
			case <-m.timer.C():
			case <-m.wake:
			}
			continue
		}
		m.mu.Unlock()
		if closed {
			return
		}
		select {
		case <-m.done:
			return
		case <-m.wake:
		}
	}
}

// stepLocked emits at most one non-terminal progress event, round-robining
// across children. Status transitions and content previews share the group
// budget; the initial queued/running (directStatus) and terminal events
// bypass it. Returns false when nothing can be emitted right now.
func (m *subagentProgressMerger) stepLocked() bool {
	m.refillLocked()
	n := len(m.order)
	if n == 0 {
		return false
	}
	now := m.clock.Now()
	for i := range n {
		idx := (m.rr + i) % n
		childID := m.order[idx]
		if m.tokens < 1 {
			// Budget exhausted: leave the round-robin position in place so no
			// child is skipped once a token refills.
			return false
		}
		if st := m.status[childID]; st != nil && st.dirty && !now.Before(st.dueAt) {
			m.rr = (idx + 1) % n
			phase := st.phase
			st.dirty = false
			st.lastSend = now
			m.tokens--
			m.emitStatusLocked(childID, phase, 0)
			return true
		}
		for c := subagentProgressChanReasoning; c <= subagentProgressChanNotice; c++ {
			if sl := m.slots[childID][c]; sl != nil && sl.dirty && !now.Before(sl.dueAt) {
				m.rr = (idx + 1) % n
				m.tokens--
				m.emitDeltaLocked(childID, c, sl)
				return true
			}
		}
	}
	return false
}

func (m *subagentProgressMerger) allCleanLocked() bool {
	for _, st := range m.status {
		if st.dirty {
			return false
		}
	}
	for _, chs := range m.slots {
		for _, sl := range chs {
			if sl.dirty {
				return false
			}
		}
	}
	return true
}

// nextDeadlineLocked returns the wait until the earliest due slot or the next
// preview budget token. A zero result means "wake immediately".
func (m *subagentProgressMerger) nextDeadlineLocked() time.Duration {
	now := m.clock.Now()
	var next time.Time
	consider := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	for _, st := range m.status {
		if st.dirty {
			consider(st.dueAt)
		}
	}
	for _, chs := range m.slots {
		for _, sl := range chs {
			if sl.dirty {
				consider(sl.dueAt)
			}
		}
	}
	if m.tokens < 1 {
		refillAt := m.lastRefill.Add(time.Duration((1 - m.tokens) * float64(time.Second) / subagentProgressGroupEventsPerSec))
		consider(refillAt)
	}
	if next.IsZero() {
		return 0
	}
	if d := next.Sub(now); d > 0 {
		return d
	}
	return 0
}

func (m *subagentProgressMerger) refillLocked() {
	now := m.clock.Now()
	if now.After(m.lastRefill) {
		elapsed := now.Sub(m.lastRefill).Seconds()
		m.tokens += elapsed * subagentProgressGroupEventsPerSec
		if m.tokens > subagentProgressGroupBurst {
			m.tokens = subagentProgressGroupBurst
		}
		m.lastRefill = now
	}
}

func (m *subagentProgressMerger) emitStatusLocked(childID string, phase subagentProgressPhase, durationMs int64) {
	m.emitToolProgressLocked(childID, event.SubagentProgressStatusName, string(phase), false, durationMs)
}

func (m *subagentProgressMerger) emitDeltaLocked(childID string, ch subagentProgressChannel, sl *progressSlot) {
	if sl.buf == "" {
		sl.dirty = false
		return
	}
	buf, truncated := sl.buf, sl.truncated
	// Carry a pending trim-truncation on the next actually-emitted channel.
	if m.truncatedPending[childID] {
		truncated = true
		delete(m.truncatedPending, childID)
	}
	sl.buf, sl.truncated, sl.dirty = "", false, false
	sl.lastSend = m.clock.Now()
	m.emitToolProgressLocked(childID, ch.name(), buf, truncated, 0)
}

func (m *subagentProgressMerger) emitToolProgressLocked(childID, name, output string, truncated bool, durationMs int64) {
	parentID := m.groupParentID
	if parentID == childID {
		parentID = ""
	}
	m.sink.Emit(event.Event{
		Kind: event.ToolProgress,
		Tool: event.Tool{
			ID: childID, Name: name, ParentID: parentID,
			Output: output, Truncated: truncated, DurationMs: durationMs,
		},
	})
}

// trimToBudgetLocked keeps the child's pending total at or under
// subagentProgressMaxPendingBytes, dropping the lowest-priority channel's
// content first (notice < reasoning < text) so the response preview survives.
// Every drop marks the child's pending-truncation flag so the loss is
// propagated on the next actually-emitted channel (or a truncated notice at
// flush when nothing else carries it).
func (m *subagentProgressMerger) trimToBudgetLocked(childID string) {
	if m.pendingBytesLocked(childID) <= subagentProgressMaxPendingBytes {
		return
	}
	if sl := m.slots[childID][subagentProgressChanNotice]; sl != nil && sl.dirty && sl.buf != "" {
		sl.buf = ""
		sl.truncated = true
		m.truncatedPending[childID] = true
	}
	for _, ch := range []subagentProgressChannel{subagentProgressChanReasoning, subagentProgressChanText} {
		over := m.pendingBytesLocked(childID) - subagentProgressMaxPendingBytes
		if over <= 0 {
			return
		}
		sl := m.slots[childID][ch]
		if sl == nil || !sl.dirty || sl.buf == "" {
			continue
		}
		keep := len(sl.buf) - over
		if keep <= 0 {
			sl.buf = ""
		} else {
			sl.buf = utf8SafeTail(sl.buf, keep)
		}
		sl.truncated = true
		m.truncatedPending[childID] = true
	}
}

func (m *subagentProgressMerger) pendingBytesLocked(childID string) int {
	total := 0
	for _, sl := range m.slots[childID] {
		if sl.dirty {
			total += len(sl.buf)
		}
	}
	return total
}

func (m *subagentProgressMerger) ensureOrderLocked(childID string) {
	if slices.Contains(m.order, childID) {
		return
	}
	m.order = append(m.order, childID)
}

func (m *subagentProgressMerger) removeOrderLocked(childID string) {
	for i, id := range m.order {
		if id == childID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			return
		}
	}
}

func (m *subagentProgressMerger) wakeLocked() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// utf8SafeTail returns the last maxBytes bytes of s, trimmed to a rune
// boundary so a multi-byte character is never split.
func utf8SafeTail(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[len(s)-maxBytes:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// subagentProgressTracker is the per-child state machine installed between a
// sub-agent run and its parent sink. It converts the child's reasoning/text/
// notice/retrying into preview slots on the group merger, forwards tool
// activity unchanged, and guarantees exactly one terminal status event.
type subagentProgressTracker struct {
	mu         sync.Mutex
	merger     *subagentProgressMerger
	childID    string
	sink       event.Sink // forwards real tool events (the subSinkFor wrapper)
	phase      subagentProgressPhase
	started    time.Time
	ownsMerger bool
	done       bool
}

// subagentProgressSink retains all host-only audit capabilities while the
// visible event stream is reduced to progress, tool, and usage events.
type subagentProgressSink struct {
	event.AuditForwarder
	tracker *subagentProgressTracker
}

var _ event.OptionalSinkCapabilities = (*subagentProgressSink)(nil)

// newSubagentProgressTracker creates (or joins) the group merger and returns a
// tracker for one child run. wrapSink is the sink the child's real tool events
// already flow through; the tracker's own preview events are emitted through
// the merger's sink — the same sink the child's dispatch card flowed through —
// so preview IDs always match the card IDs the frontend sees.
func newSubagentProgressTracker(ctx context.Context, wrapSink event.Sink) *subagentProgressTracker {
	parentID, parent, _, ok := CallContext(ctx)
	merger := subagentProgressMergerFromContext(ctx)
	owns := false
	if merger == nil {
		// Not part of a parent task group: own a merger that emits through
		// the same sink the dispatch event flowed through (the call context's
		// raw sink; Discard for headless/direct-execute runs).
		sink := event.Discard
		if ok && parent != nil {
			sink = parent
		}
		merger = newSubagentProgressMerger(realProgressClock{}, sink, parentID)
		owns = true
	}
	return &subagentProgressTracker{
		merger:     merger,
		childID:    parentID,
		sink:       wrapSink,
		started:    merger.clock.Now(),
		ownsMerger: owns,
	}
}

// queued marks the background registration state; running marks execution
// start (or the moment a background job acquires its execution slot).
// queued marks the background registration state; running marks execution
// start (or the moment a background job acquires its slot). Both are emitted
// synchronously — not through the merging status slot — so the first visible
// states can never be merged away by a faster follow-up transition: a
// background job that grabs its slot microseconds after registration must not
// hide the queued state.
func (t *subagentProgressTracker) queued() {
	t.emitStatusDirect(subagentPhaseQueued)
}

func (t *subagentProgressTracker) running() {
	t.emitStatusDirect(subagentPhaseRunning)
}

func (t *subagentProgressTracker) emitStatusDirect(p subagentProgressPhase) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return
	}
	t.phase = p
	t.merger.directStatus(t.childID, p)
}

func (t *subagentProgressTracker) setPhase(p subagentProgressPhase) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.setPhaseLocked(p)
}

// setPhaseLocked records a phase change and queues the status event; repeat
// transitions of the same phase do not re-queue.
func (t *subagentProgressTracker) setPhaseLocked(p subagentProgressPhase) {
	if t.done || t.phase == p {
		return
	}
	t.phase = p
	t.merger.statusEvent(t.childID, p)
}

// wrap returns the sink the child agent emits into: reasoning/text/notice/
// retrying become preview slots; tool activity and usage pass through
// unchanged (the child's Message and anything else stay dropped, as before).
// Events arriving after the terminal are ignored.
func (t *subagentProgressTracker) wrap() event.Sink {
	return &subagentProgressSink{
		AuditForwarder: event.AuditForwarder{Inner: t.sink},
		tracker:        t,
	}
}

func (s *subagentProgressSink) Emit(e event.Event) {
	t := s.tracker
	t.mu.Lock()
	if t.done {
		t.mu.Unlock()
		return
	}
	switch e.Kind {
	case event.Reasoning:
		t.setPhaseLocked(subagentPhaseReasoning)
		t.merger.deltaEvent(t.childID, subagentProgressChanReasoning, e.Text)
	case event.Text:
		t.setPhaseLocked(subagentPhaseResponding)
		t.merger.deltaEvent(t.childID, subagentProgressChanText, e.Text)
	case event.Notice:
		text := e.Text
		if text == "" {
			text = e.Detail
		}
		t.merger.deltaEvent(t.childID, subagentProgressChanNotice, text)
	case event.Retrying:
		t.setPhaseLocked(subagentPhaseRetrying)
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		t.setPhaseLocked(subagentPhaseTool)
	}
	t.mu.Unlock()
	switch e.Kind {
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		t.sink.Emit(e)
	case event.Usage:
		if e.UsageSource == "" {
			e.UsageSource = event.UsageSourceSubagent
		}
		t.sink.Emit(e)
	}
}

// finish flushes pending previews, emits the single terminal status, and — if
// the tracker owns its merger — closes it. ctxErr non-nil maps to cancelled,
// a typed partial outcome maps to partial, other errors to failed, and success
// to completed. Idempotent: late events and repeated calls are ignored.
func (t *subagentProgressTracker) finish(ctxErr, runErr error) {
	t.mu.Lock()
	if t.done {
		t.mu.Unlock()
		return
	}
	t.done = true
	phase := subagentPhaseCompleted
	if ctxErr != nil {
		phase = subagentPhaseCancelled
	} else if runErr != nil {
		phase = subagentPhaseFailed
		var subErr *SubagentRunError
		if errors.As(runErr, &subErr) && subErr.Outcome.Status == SubagentOutcomePartial {
			phase = subagentPhasePartial
		}
	}
	durationMs := t.merger.clock.Now().Sub(t.started).Milliseconds()
	t.mu.Unlock()
	t.merger.flushChild(t.childID, phase, durationMs)
	if t.ownsMerger {
		t.merger.Close()
	}
}

// subagentProgressMergerKey carries the group merger in the child's context so
// parallel_tasks/fleet children share one pacing budget per parent call.
type subagentProgressMergerKey struct{}

func withSubagentProgressMerger(ctx context.Context, m *subagentProgressMerger) context.Context {
	return context.WithValue(ctx, subagentProgressMergerKey{}, m)
}

func subagentProgressMergerFromContext(ctx context.Context) *subagentProgressMerger {
	m, _ := ctx.Value(subagentProgressMergerKey{}).(*subagentProgressMerger)
	return m
}

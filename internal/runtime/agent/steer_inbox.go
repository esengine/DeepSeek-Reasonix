// steer_inbox.go — the mid-turn guidance a running turn will accept.
package agent

import (
	"reasonix/internal/state/sessionstore"
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// steerEntry is one mid-turn guidance admission. host marks the ones the
// runtime queued for itself, which the model must not read as the user
// speaking.
type steerEntry struct {
	itemID string
	load   func() (string, error)
	// text is a fallback when load is nil (legacy Steer(string) path).
	text string
	host bool
	// via is the paired device the guidance was sent from; nil is the window.
	via *provider.Via
}

// steerInbox is the guidance admitted while a Run is executing. The queue and
// the two flags share one lifetime — opened on the way into Run, closed on the
// way out — so they move as one state rather than as separate flags a caller
// could leave in a combination no Run ever produces.
type steerInbox struct {
	mu    sync.Mutex
	queue []steerEntry
	// drainedQueue is true when the queue emptied on the last take, which is
	// what tells a caller its guidance has been picked up.
	drainedQueue bool
	// running is true while a Run is executing. Intake is open only then.
	running bool
}

// open admits guidance for the Run that is starting.
func (s *steerInbox) open() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drainedQueue = false
	s.running = true
}

// admit queues guidance and reports whether intake was open. On false nothing
// was queued and the caller has to deliver it another way.
func (s *steerInbox) admit(e steerEntry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return false
	}
	s.queue = append(s.queue, e)
	s.drainedQueue = false
	return true
}

// take removes the next entry. Loading its body is the caller's, so no inbox
// lock is held across the read.
func (s *steerInbox) take() (steerEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return steerEntry{}, false
	}
	e := s.queue[0]
	s.queue = s.queue[1:]
	s.drainedQueue = len(s.queue) == 0
	return e, true
}

// remove drops the entry for itemID before anything reads it, reporting
// whether it was still queued. This lock is the only place that can tell "not
// read yet" from "already on its way to the model" without racing take, which
// is what makes a cancel safe to act on rather than a guess.
func (s *steerInbox) remove(itemID string) bool {
	if itemID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.queue {
		if e.itemID != itemID {
			continue
		}
		s.queue = append(s.queue[:i], s.queue[i+1:]...)
		return true
	}
	return false
}

// DropSteer takes back guidance this turn accepted but has not read yet. It
// reports false once the run loop has taken the entry, which is the moment the
// text stops being the user's to withdraw.
func (a *Agent) DropSteer(itemID string) bool {
	return a.steer.remove(itemID)
}

// drained reports that the queue emptied on the last take.
func (s *steerInbox) drained() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drainedQueue
}

func (s *steerInbox) pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// closeIfIdle closes intake only when nothing is waiting, which is what makes
// the check and the close one step: guidance accepted before it keeps the loop
// alive, and anything arriving after is refused.
func (s *steerInbox) closeIfIdle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) > 0 {
		return false
	}
	s.running = false
	return true
}

// close ends intake and hands back whatever was never consumed.
func (s *steerInbox) close() []steerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.queue
	s.queue = nil
	if len(pending) > 0 {
		s.drainedQueue = true
	}
	s.running = false
	return pending
}

// SteerHostItem is SteerItem for a durable item the runtime authored. The
// attribution travels with the entry rather than with the call site, so every
// path that renders or persists it later says the same thing about who spoke.
func (a *Agent) SteerHostItem(itemID string, load func() (string, error)) bool {
	return a.queueSteer(steerEntry{itemID: itemID, load: load, host: true})
}

// RecordUnappliedHostSteer is RecordUnappliedSteer for runtime-authored
// guidance. Recording it as the user's would put words in their mouth in the
// one transcript a later turn reads back.
func (a *Agent) RecordUnappliedHostSteer(text string, itemID ...string) {
	a.recordUnappliedSteer(text, true, itemID...)
}

func (a *Agent) recordUnappliedSteer(text string, host bool, itemID ...string) {
	if a == nil || a.sess.conversation == nil {
		return
	}
	id := ""
	if len(itemID) > 0 {
		id = itemID[0]
	}
	a.sess.conversation.Add(provider.Message{
		Role:       provider.RoleTool,
		Content:    a.withTurnPreferences(sessionstore.MidTurnSteerMessage(text, host)),
		ToolCallID: provider.LocalOnlyToolID,
		Name:       provider.LocalOnlyToolName,
		LocalOnly:  true,
	})
	a.svc.sink.Emit(event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Code:   event.NoticeCodeUnappliedSteer,
		Text:   UnappliedSteerNotice(text),
		Detail: text,
		ItemID: id,
	})
}

// Steer queues a message for mid-turn injection and reports whether an active
// turn accepted it; on false nothing was queued and the caller delivers it
// another way, typically as a new turn. The active check keeps a steer landing
// between the exit flush and running=false from sitting unconsumed and unsaved.
func (a *Agent) Steer(text string) bool {
	return a.SteerItem("", func() (string, error) { return text, nil })
}

// SteerHostNotice queues runtime-authored guidance on the same path as Steer,
// attributed to the host instead of the user.
func (a *Agent) SteerHostNotice(text string) bool {
	return a.queueSteer(steerEntry{load: func() (string, error) { return text, nil }, host: true})
}

// SteerItem queues durable-inbox guidance identified by itemID. load is called
// only when the entry is consumed so the agent does not retain every body.
func (a *Agent) SteerItem(itemID string, load func() (string, error)) bool {
	return a.SteerItemFrom(itemID, load, nil)
}

// SteerItemFrom is SteerItem for guidance sent from a paired device, which the
// delivered message and its announcement name.
func (a *Agent) SteerItemFrom(itemID string, load func() (string, error), via *provider.Via) bool {
	return a.queueSteer(steerEntry{itemID: itemID, load: load, via: via})
}

func (a *Agent) queueSteer(e steerEntry) bool {
	return a.steer.admit(e)
}

// SteerConsumed returns true when the steer queue became empty after the last consume.
func (a *Agent) SteerConsumed() bool {
	return a.steer.drained()
}

func (a *Agent) consumeSteer() (text string, e steerEntry, ok bool) {
	e, ok = a.steer.take()
	if !ok {
		return "", steerEntry{}, false
	}
	if e.load != nil {
		t, err := e.load()
		if err != nil {
			return "", e, false
		}
		return t, e, true
	}
	return e.text, e, true
}

// closeSteerIntakeIfIdle atomically closes the normal-completion race between
// the final queue check and Run returning. A steer accepted before this check
// keeps the loop alive; one arriving after it is rejected so the host can keep
// the user's draft and retry it as a regular follow-up.
func (a *Agent) closeSteerIntakeIfIdle() bool {
	return a.steer.closeIfIdle()
}

// flushSteerQueue ends the turn's steer intake. Guidance that arrived too late
// to be consumed is persisted for transcript visibility but marked local-only:
// replaying it to the model on the next unrelated user turn can execute a stale
// historical task (#7045). An explicit warning keeps the transcript honest
// without presenting the text as successfully applied guidance (#6238).
func (a *Agent) flushSteerQueue() {
	for _, e := range a.steer.close() {
		text := e.text
		if e.load != nil {
			if t, err := e.load(); err == nil {
				text = t
			}
		}
		a.recordUnappliedSteer(text, e.host, e.itemID)
	}
}

// UnappliedSteerNotice returns the durable warning shown for guidance that was
// accepted during an abnormal turn exit but never reached a provider request.
func UnappliedSteerNotice(text string) string {
	return "Guidance was not applied because the turn ended before it could be processed. Send it again if it is still needed:\n" + text
}

// RecordUnappliedSteer stores guidance that could not affect its intended
// in-flight turn. The orphan-tool sentinel makes older readers drop the record
// during wire normalization, while current readers use LocalOnly to exclude it
// before every provider request. itemID correlates the notice with the durable
// session inbox entry when one exists.
func (a *Agent) RecordUnappliedSteer(text string, itemID ...string) {
	a.recordUnappliedSteer(text, false, itemID...)
}

func (a *Agent) steerQueueLen() int {
	return a.steer.pending()
}

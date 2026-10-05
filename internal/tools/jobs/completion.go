// completion.go — what a terminal job publishes, and to whom.
package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/base/nilutil"
	"reasonix/internal/contract/event"
)

// completionEventVersion is the identity's schema suffix. A reader that stores
// deliveries by event ID keeps them distinguishable if the shape ever changes.
const completionEventVersion = "terminal-v1"

// CompletionEvent is one job reaching a terminal status with its result already
// durable. Its ID is derived from the job, not minted per publish, so a
// delivery path that runs twice can tell a replay from a second completion.
type CompletionEvent struct {
	// ID is deterministic: completion/<job-id>/terminal-v1.
	ID        string
	SessionID string
	JobID     string
	Kind      string
	Label     string
	Status    Status

	// ResultRef is the artifact log path when one survives, else empty; the job
	// id always reaches the result through the manager.
	ResultRef string

	// Note is the one line the legacy drain path folds into the next turn.
	Note string

	FinishedAt time.Time
}

// CompletionEventID is the deterministic identity for a job's terminal event.
func CompletionEventID(jobID string) string {
	return fmt.Sprintf("completion/%s/%s", jobID, completionEventVersion)
}

// CompletionObserver receives a job's terminal event once that status is
// observable to every other reader. Returning nil means the event reached
// durable storage, never that a model consumed it; on error the manager keeps
// its own queued note.
type CompletionObserver interface {
	OnJobCompletion(context.Context, CompletionEvent) error
}

// SetCompletionObserver installs (or clears) the observer after construction,
// which is the order the controller is built in.
func (m *Manager) SetCompletionObserver(o CompletionObserver) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.completionObserver = o
	m.mu.Unlock()
}

// publishCompletion hands the event to the observer and, when it reports the
// event durable, drops the manager's own copy so the completion is not folded
// into the next user turn as well.
func (m *Manager) publishCompletion(ev CompletionEvent) {
	if ev.ID == "" {
		return
	}
	m.mu.Lock()
	observer := m.completionObserver
	m.mu.Unlock()
	if observer == nil {
		return
	}
	if err := observer.OnJobCompletion(m.root, ev); err != nil {
		return
	}
	m.ackCompletion(ev.ID)
}

// artifactRefLocked is the job's own log path once it is complete. Callers hold
// j.mu.
func (j *Job) artifactRefLocked() string {
	if !j.artifact.complete {
		return ""
	}
	return j.artifact.path
}

// ackCompletion drops a queued note the observer took responsibility for. An ID
// already drained by a turn that started meanwhile is not an error.
func (m *Manager) ackCompletion(eventID string) {
	if eventID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	remaining := m.completed[:0]
	for _, item := range m.completed {
		if item.eventID == eventID {
			continue
		}
		remaining = append(remaining, item)
	}
	m.completed = remaining
}

// recordCompletion queues the finished-job summary for DrainCompletedNote and
// emits a closing Notice. It returns the event publishCompletion hands on; a
// zero event means nothing was queued and there is nothing to publish.
func (m *Manager) recordCompletion(parentSession, id, kind, label, resultRef string, st Status, err error) CompletionEvent {
	tag := id
	if label != "" {
		tag = fmt.Sprintf("%s (%s)", id, label)
	}
	parentSession = strings.TrimSpace(parentSession)
	shouldEmit := false
	ev := CompletionEvent{
		ID: CompletionEventID(id), SessionID: parentSession, JobID: id,
		Kind: kind, Label: label, Status: st, ResultRef: resultRef,
		Note: fmt.Sprintf("%s — %s", tag, st), FinishedAt: time.Now(),
	}
	m.mu.Lock()
	if parentSession != "" && m.destroying[parentSession] {
		m.mu.Unlock()
		return CompletionEvent{}
	}
	m.completed = append(m.completed, completion{
		sessionID: parentSession,
		text:      ev.Note,
		eventID:   ev.ID,
	})
	active := m.active
	shouldEmit = active == "" || parentSession == "" || active == parentSession
	m.mu.Unlock()

	if !nilutil.IsNil(m.taskRecorder) {
		m.taskRecorder.RecordDone(id, st, err)
	}

	level, text := event.LevelInfo, fmt.Sprintf("background %s finished: %s", kind, id)
	detail := ""
	switch st {
	case Failed:
		level, text = event.LevelWarn, fmt.Sprintf("background %s failed: needs attention", kind)
		detail = fmt.Sprintf("background %s failed: %s — %v", kind, id, err)
	case Killed:
		text = fmt.Sprintf("background %s killed: %s", kind, id)
	}
	if shouldEmit {
		m.sink.Emit(event.Event{Kind: event.Notice, Level: level, Text: text, Detail: detail})
	}
	return ev
}

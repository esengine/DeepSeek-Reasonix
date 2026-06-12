// Package jobs is the session-scoped background-job registry behind the agent's
// background tools (bash run_in_background, task run_in_background) and the
// bash_output / kill_shell / wait tools. A Manager owns a context whose lifetime
// is the session, NOT a single turn — so a job started in one turn keeps running
// across turns and is cancelled only when the controller closes (or kill_shell is
// called). Tools reach the Manager through the call context (WithManager /
// FromContext), the same injection pattern the `ask` tool uses for the asker.
//
// The Manager emits a user-visible Notice when a job starts and finishes, and
// accumulates a one-line completion summary that the controller drains into the
// next turn (DrainCompletedNote) so the model itself learns of completions.
package jobs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/nilutil"
)

// Status is a job's lifecycle state.
type Status string

const (
	Running Status = "running"
	Done    Status = "done"
	Failed  Status = "failed"
	Killed  Status = "killed"
)

// View is a read-only snapshot of a job for the status bar.
type View struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"` // unix milliseconds
}

// Result is one job's terminal (or current) state returned by Wait.
type Result struct {
	ID     string
	Kind   string
	Label  string
	Status Status
	Output string // the terminal result text, or the streamed buffer when no result was set
}

// Job is one background job. The mutex guards the streaming buffer and the
// terminal fields; the run goroutine writes them, readers (Output/Wait/snapshots)
// take the same lock.
type Job struct {
	ID        string
	Kind      string // "bash" | "task"
	Label     string
	SessionID string

	mu         sync.Mutex
	buf        bytes.Buffer
	readOffset int
	status     Status
	result     string
	resultRead bool // result already surfaced by Output (task jobs stream nothing to buf)
	startedAt  int64
	cancel     context.CancelFunc
	done       chan struct{}
}

// Manager is the session's background-job table. It is safe for concurrent use.
type Manager struct {
	sink   event.Sink
	root   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu         sync.Mutex
	seq        int
	jobs       map[string]*Job
	order      []string
	completed  []completion // finished-job summaries awaiting drain into the next turn
	active     string
	destroying map[string]bool
}

type completion struct {
	sessionID string
	text      string
}

// NewManager returns a Manager whose jobs run under a fresh session-scoped
// context (cancelled by Close). sink receives job-lifecycle notices; pass the
// session's synchronized sink (event.Sync) since jobs emit from goroutines.
func NewManager(sink event.Sink) *Manager {
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	root, cancel := context.WithCancel(context.Background())
	return &Manager{sink: sink, root: root, cancel: cancel, jobs: map[string]*Job{}, destroying: map[string]bool{}}
}

// jobWriter appends a job's streamed output under its lock so a concurrent
// Output read never races the producing goroutine.
type jobWriter struct{ j *Job }

func (w jobWriter) Write(p []byte) (int, error) {
	w.j.mu.Lock()
	defer w.j.mu.Unlock()
	return w.j.buf.Write(p)
}

// Start launches run on a goroutine under the manager's session context and
// returns the job immediately. run streams output to the writer and returns the
// terminal result text (a task's final answer; a bash job streams everything to
// the buffer and returns ""). The job is marked killed when its context was
// cancelled, failed on any other error, else done.
func (m *Manager) Start(kind, label string, run func(ctx context.Context, out io.Writer) (string, error)) *Job {
	return m.StartForSession("", kind, label, run)
}

// StartForSession launches a job owned by parentSession. Session-scoped readers
// only see jobs whose owner matches the active session.
func (m *Manager) StartForSession(parentSession, kind, label string, run func(ctx context.Context, out io.Writer) (string, error)) *Job {
	parentSession = strings.TrimSpace(parentSession)
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("%s-%d", kind, m.seq)
	ctx, cancel := context.WithCancel(m.root)
	j := &Job{ID: id, Kind: kind, Label: label, SessionID: parentSession, status: Running, startedAt: nowMs(), cancel: cancel, done: make(chan struct{})}
	m.jobs[id] = j
	m.order = append(m.order, id)
	m.mu.Unlock()

	m.emitIfActive(parentSession, event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: startedText(kind, id, label)})

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		result, err := run(ctx, jobWriter{j})

		var st Status
		switch {
		case ctx.Err() != nil:
			st = Killed
		case err != nil:
			st = Failed
			if result == "" {
				result = err.Error()
			}
		default:
			st = Done
		}
		// Queue the drain note (and emit the closing Notice) BEFORE publishing the
		// terminal status. Wait(nil)/resolve only block on Running jobs, so if the
		// status flipped to terminal before the note was queued, a Wait could observe
		// completion, skip j.done, and DrainCompletedNote would race ahead of the
		// bookkeeping (the TestDrainMultiple -race flake). Recording first makes an
		// observed terminal status imply the note is already queued.
		m.recordCompletion(parentSession, id, kind, label, st, err)

		j.mu.Lock()
		j.result = result
		if j.status != Killed { // a concurrent Kill already published Killed — keep it
			j.status = st
		}
		j.mu.Unlock()
		close(j.done)
	}()
	return j
}

// recordCompletion queues the finished-job summary for DrainCompletedNote and
// emits a closing Notice (warn for a failure, info otherwise).
func (m *Manager) recordCompletion(parentSession, id, kind, label string, st Status, err error) {
	tag := id
	if label != "" {
		tag = fmt.Sprintf("%s (%s)", id, label)
	}
	parentSession = strings.TrimSpace(parentSession)
	shouldEmit := false
	m.mu.Lock()
	if parentSession != "" && m.destroying[parentSession] {
		m.mu.Unlock()
		return
	}
	m.completed = append(m.completed, completion{
		sessionID: parentSession,
		text:      fmt.Sprintf("%s — %s", tag, st),
	})
	active := m.active
	shouldEmit = active == "" || parentSession == "" || active == parentSession
	m.mu.Unlock()

	level, text := event.LevelInfo, fmt.Sprintf("background %s finished: %s", kind, id)
	switch st {
	case Failed:
		level, text = event.LevelWarn, fmt.Sprintf("background %s failed: %s — %v", kind, id, err)
	case Killed:
		text = fmt.Sprintf("background %s killed: %s", kind, id)
	}
	if shouldEmit {
		m.sink.Emit(event.Event{Kind: event.Notice, Level: level, Text: text})
	}
}

func (m *Manager) get(id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

// Output returns the job's output produced since the last Output call plus its
// current status. ok is false when the id is unknown.
func (m *Manager) Output(id string) (text string, status Status, ok bool) {
	return m.OutputForSession("", id)
}

// OutputForSession returns output only when id belongs to parentSession. Empty
// parentSession preserves the legacy unscoped behavior.
func (m *Manager) OutputForSession(parentSession, id string) (text string, status Status, ok bool) {
	j := m.get(id)
	if j == nil || !sessionMatches(parentSession, j.SessionID) {
		return "", "", false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	full := j.buf.String()
	text = full[j.readOffset:]
	j.readOffset = len(full)
	// A task job streams nothing to the buffer — its answer lands in result. Once
	// it is terminal with no buffered output, surface that result once so a task's
	// answer is visible here too (bash_output's description promises task support).
	if text == "" && j.status != Running && j.result != "" && !j.resultRead {
		text = j.result
		j.resultRead = true
	}
	return text, j.status, true
}

// Kill cancels a running job. Returns false when the id is unknown or the job has
// already finished.
func (m *Manager) Kill(id string) bool {
	return m.KillForSession("", id)
}

// KillForSession cancels a running job only when it belongs to parentSession.
// Empty parentSession preserves the legacy unscoped behavior.
func (m *Manager) KillForSession(parentSession, id string) bool {
	j := m.get(id)
	if j == nil || !sessionMatches(parentSession, j.SessionID) {
		return false
	}
	j.mu.Lock()
	running := j.status == Running
	if running {
		// Flip to Killed synchronously so Output/Wait reflect the kill the instant
		// it's requested, not whenever the run goroutine's cmd.Run returns (which
		// trails by WaitDelay while a cancelled process tree tears down). The
		// goroutine still sets Killed + records completion on return; this only
		// fires when the job is actually Running, so a job that just finished
		// keeps its real terminal status.
		j.status = Killed
	}
	j.mu.Unlock()
	if !running {
		return false
	}
	j.cancel()
	return true
}

// Wait blocks until the named jobs (or every currently-running job when ids is
// empty) reach a terminal state, or ctx is cancelled, or timeoutSec elapses
// (0 = no timeout). It returns each target's snapshot regardless of why it
// returned, so a timeout still reports partial progress.
func (m *Manager) Wait(ctx context.Context, ids []string, timeoutSec int) []Result {
	return m.WaitForSession(ctx, "", ids, timeoutSec)
}

// WaitForSession waits only on jobs owned by parentSession. Empty parentSession
// preserves the legacy unscoped behavior.
func (m *Manager) WaitForSession(ctx context.Context, parentSession string, ids []string, timeoutSec int) []Result {
	targets := m.resolve(parentSession, ids)
	if len(targets) == 0 {
		return nil
	}
	var timeout <-chan time.Time
	if timeoutSec > 0 {
		t := time.NewTimer(time.Duration(timeoutSec) * time.Second)
		defer t.Stop()
		timeout = t.C
	}
	for _, j := range targets {
		select {
		case <-j.done:
		case <-ctx.Done():
			return m.results(targets)
		case <-timeout:
			return m.results(targets)
		}
	}
	return m.results(targets)
}

// resolve maps requested ids to jobs; an empty list selects all running jobs.
func (m *Manager) resolve(parentSession string, ids []string) []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Job
	if len(ids) == 0 {
		for _, id := range m.order {
			j := m.jobs[id]
			if !sessionMatches(parentSession, j.SessionID) {
				continue
			}
			j.mu.Lock()
			running := j.status == Running
			j.mu.Unlock()
			if running {
				out = append(out, j)
			}
		}
		return out
	}
	for _, id := range ids {
		if j := m.jobs[id]; j != nil && sessionMatches(parentSession, j.SessionID) {
			out = append(out, j)
		}
	}
	return out
}

func (m *Manager) results(targets []*Job) []Result {
	out := make([]Result, 0, len(targets))
	for _, j := range targets {
		j.mu.Lock()
		text := j.result
		if text == "" {
			text = j.buf.String()
		}
		out = append(out, Result{ID: j.ID, Kind: j.Kind, Label: j.Label, Status: j.status, Output: text})
		j.mu.Unlock()
	}
	return out
}

// Running returns a snapshot of the still-running jobs (for the status bar).
func (m *Manager) Running() []View {
	return m.RunningForSession("")
}

// RunningForSession returns still-running jobs owned by parentSession. Empty
// parentSession preserves the legacy unscoped behavior.
func (m *Manager) RunningForSession(parentSession string) []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []View
	for _, id := range m.order {
		j := m.jobs[id]
		if !sessionMatches(parentSession, j.SessionID) {
			continue
		}
		j.mu.Lock()
		if j.status == Running {
			out = append(out, View{ID: j.ID, Kind: j.Kind, Label: j.Label, Status: string(j.status), StartedAt: j.startedAt})
		}
		j.mu.Unlock()
	}
	return out
}

// DrainCompletedNote returns (and clears) a one-line summary of jobs that
// finished since the last drain, for the controller to fold into the next turn
// so the model learns of completions. "" when nothing finished.
func (m *Manager) DrainCompletedNote() string {
	return m.DrainCompletedNoteForSession("")
}

// DrainCompletedNoteForSession drains completion notes for parentSession only.
// Notes for other sessions stay queued until that session becomes active again.
// Empty parentSession preserves the legacy unscoped behavior.
func (m *Manager) DrainCompletedNoteForSession(parentSession string) string {
	m.mu.Lock()
	var c []string
	if strings.TrimSpace(parentSession) == "" {
		for _, item := range m.completed {
			c = append(c, item.text)
		}
		m.completed = nil
	} else {
		remaining := m.completed[:0]
		for _, item := range m.completed {
			if item.sessionID == parentSession {
				c = append(c, item.text)
			} else {
				remaining = append(remaining, item)
			}
		}
		m.completed = remaining
	}
	m.mu.Unlock()
	if len(c) == 0 {
		return ""
	}
	return "Background jobs finished since your last message: " + strings.Join(c, "; ") +
		". Read their output with bash_output or wait if you still need it."
}

// SetActiveSession controls which session receives lifecycle notices for jobs
// that finish asynchronously. Empty active session preserves legacy behavior.
func (m *Manager) SetActiveSession(parentSession string) {
	m.mu.Lock()
	m.active = strings.TrimSpace(parentSession)
	m.mu.Unlock()
}

// DestroySession marks a parent session as being removed from active use and
// cancels its running jobs. The returned channels close when each cancelled job
// has fully unwound.
func (m *Manager) DestroySession(parentSession string) []<-chan struct{} {
	parentSession = strings.TrimSpace(parentSession)
	if parentSession == "" {
		return nil
	}
	var cancels []context.CancelFunc
	var done []<-chan struct{}
	m.mu.Lock()
	m.destroying[parentSession] = true
	remaining := m.completed[:0]
	for _, item := range m.completed {
		if item.sessionID != parentSession {
			remaining = append(remaining, item)
		}
	}
	m.completed = remaining
	for _, id := range m.order {
		j := m.jobs[id]
		if !sessionMatches(parentSession, j.SessionID) {
			continue
		}
		j.mu.Lock()
		if j.status == Running {
			j.status = Killed
			cancels = append(cancels, j.cancel)
			done = append(done, j.done)
		}
		j.mu.Unlock()
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return done
}

// IsDestroying reports whether parentSession is in the destroy window. Empty
// parent sessions are never considered destroyed.
func (m *Manager) IsDestroying(parentSession string) bool {
	parentSession = strings.TrimSpace(parentSession)
	if parentSession == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.destroying[parentSession]
}

// FinishDestroySession ends the destroy window after all owned jobs have unwound
// and persistent cleanup/move work has completed.
func (m *Manager) FinishDestroySession(parentSession string) {
	parentSession = strings.TrimSpace(parentSession)
	if parentSession == "" {
		return
	}
	m.mu.Lock()
	delete(m.destroying, parentSession)
	m.mu.Unlock()
}

// Close cancels the session context and waits for every background job goroutine
// to return before unblocking. Jobs observe the cancel through their run context
// (exec.CommandContext kills a bash job's process), so the wait is bounded. This
// matters for callers tearing down a t.TempDir: without the wait, RemoveAll can
// race a job goroutine that still holds a file under that dir.
func (m *Manager) Close() {
	m.cancel()
	m.wg.Wait()
}

func nowMs() int64 { return time.Now().UnixMilli() }

func startedText(kind, id, label string) string {
	if label != "" {
		return fmt.Sprintf("background %s started: %s (%s)", kind, id, label)
	}
	return fmt.Sprintf("background %s started: %s", kind, id)
}

func (m *Manager) emitIfActive(parentSession string, ev event.Event) {
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if active == "" || strings.TrimSpace(parentSession) == "" || active == strings.TrimSpace(parentSession) {
		m.sink.Emit(ev)
	}
}

func sessionMatches(filter, jobSession string) bool {
	filter = strings.TrimSpace(filter)
	return filter == "" || strings.TrimSpace(jobSession) == filter
}

// --- call-context injection (mirrors agent.CallContext) ---

type ctxKey struct{}
type sessionCtxKey struct{}

// WithManager stamps ctx with the job manager so tools can reach it via
// FromContext. The agent sets this on every tool call's context.
func WithManager(ctx context.Context, m *Manager) context.Context {
	return context.WithValue(ctx, ctxKey{}, m)
}

// FromContext returns the job manager set by the agent, if any. ok is false for a
// plain context (headless tests, calls outside the run loop).
func FromContext(ctx context.Context) (*Manager, bool) {
	m, ok := ctx.Value(ctxKey{}).(*Manager)
	return m, ok && m != nil
}

// WithSession stamps ctx with the active parent session ID for session-scoped job
// operations.
func WithSession(ctx context.Context, parentSession string) context.Context {
	return context.WithValue(ctx, sessionCtxKey{}, strings.TrimSpace(parentSession))
}

// SessionFromContext returns the active parent session ID for job ownership and
// filtering. Empty means no session scope is available.
func SessionFromContext(ctx context.Context) string {
	session, _ := ctx.Value(sessionCtxKey{}).(string)
	return strings.TrimSpace(session)
}

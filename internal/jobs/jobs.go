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
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/secrets"
)

var renamePath = os.Rename

// Status is a job's lifecycle state.
type Status string

const (
	Running Status = "running"
	Done    Status = "done"
	Failed  Status = "failed"
	Killed  Status = "killed"
)

// DefaultTeardownGrace bounds Close and destroy waits for non-cooperative jobs.
const DefaultTeardownGrace = 15 * time.Second

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

// TeardownJob identifies a job that is still unwinding after teardown waited.
type TeardownJob struct {
	ID     string
	Kind   string
	Label  string
	Waited time.Duration
}

// TeardownResult reports jobs that did not unwind within the teardown grace.
type TeardownResult struct {
	TimedOut []TeardownJob
}

// HasTimedOut reports whether teardown returned before every job had unwound.
func (r TeardownResult) HasTimedOut() bool { return len(r.TimedOut) > 0 }

type teardownTarget struct {
	info TeardownJob
	done <-chan struct{}
}

// SessionTeardown is the destroy handle for a session's owned background jobs.
type SessionTeardown struct {
	SessionID string
	targets   []teardownTarget
}

// Async reports whether the handle has jobs to wait on.
func (h SessionTeardown) Async() bool { return len(h.targets) > 0 }

// DoneChannels returns each target's completion channel for legacy callers.
func (h SessionTeardown) DoneChannels() []<-chan struct{} {
	out := make([]<-chan struct{}, 0, len(h.targets))
	for _, target := range h.targets {
		out = append(out, target.done)
	}
	return out
}

// Job is one background job. The mutex guards the streaming buffer and the
// terminal fields; the run goroutine writes them, readers (Output/Wait/snapshots)
// take the same lock.
type Job struct {
	ID        string
	Kind      string // "bash" | "task"
	Label     string
	SessionID string

	mu          sync.Mutex
	tail        []byte
	readOffset  int64
	status      Status
	result      string
	resultRead  bool // result already surfaced by Output (task jobs stream nothing to buf)
	startedAt   int64
	finishedAt  int64
	activityAt  int64
	runReturned bool
	cancel      context.CancelFunc
	done        chan struct{}
	stalled     bool

	artifactPath     string
	artifactMetaPath string
	artifactFile     *os.File
	artifactComplete bool
	artifactErr      string
	tombstone        bool
}

// Manager is the session's background-job table. It is safe for concurrent use.
type Manager struct {
	sink   event.Sink
	root   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu           sync.Mutex
	seq          int
	jobs         map[string]*Job
	order        []string
	completed    []completion // finished-job summaries awaiting drain into the next turn
	active       string
	destroying   map[string]bool
	artifactDirs map[string]string
	loaded       map[string]bool
	tempRoot     string

	stalledWarning time.Duration
	teardownGrace  time.Duration
}

type completion struct {
	sessionID string
	text      string
}

// Option configures a Manager.
type Option func(*Manager)

// WithStalledWarningAfter enables one stalled warning per job after d without
// job-owned visible output. A non-positive duration disables stalled warnings.
func WithStalledWarningAfter(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.stalledWarning = d
		}
	}
}

// WithTeardownGrace overrides the Close/destroy grace window. Tests can set a
// short value; production uses DefaultTeardownGrace.
func WithTeardownGrace(d time.Duration) Option {
	return func(m *Manager) {
		if d >= 0 {
			m.teardownGrace = d
		}
	}
}

// TeardownGrace reports the manager's configured close/destroy wait window.
func (m *Manager) TeardownGrace() time.Duration { return m.teardownGrace }

// NewManager returns a Manager whose jobs run under a fresh session-scoped
// context (cancelled by Close). sink receives job-lifecycle notices; pass the
// session's synchronized sink (event.Sync) since jobs emit from goroutines.
func NewManager(sink event.Sink, opts ...Option) *Manager {
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	root, cancel := context.WithCancel(context.Background())
	tempRoot, _ := os.MkdirTemp("", "reasonix-jobs-*")
	m := &Manager{
		sink:          sink,
		root:          root,
		cancel:        cancel,
		jobs:          map[string]*Job{},
		destroying:    map[string]bool{},
		artifactDirs:  map[string]string{},
		loaded:        map[string]bool{},
		tempRoot:      tempRoot,
		teardownGrace: DefaultTeardownGrace,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m
}

// jobWriter appends a job's streamed output under its lock so a concurrent
// Output read never races the producing goroutine.
type jobWriter struct{ j *Job }

func (w jobWriter) Write(p []byte) (int, error) {
	redacted := []byte(secrets.Redact(string(p)))
	w.j.mu.Lock()
	defer w.j.mu.Unlock()
	w.j.activityAt = nowMs()
	w.j.tail = appendTail(w.j.tail, redacted, defaultTailBytes)
	if w.j.artifactFile != nil {
		if _, err := w.j.artifactFile.Write(redacted); err != nil {
			w.j.artifactErr = err.Error()
		}
	}
	return len(p), nil
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
	label = secrets.Redact(label)
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("%s-%d", kind, m.seq)
	ctx, cancel := context.WithCancel(m.root)
	startedAt := nowMs()
	logPath, metaPath, file, artifactErr := m.openArtifactLocked(parentSession, id)
	j := &Job{
		ID:               id,
		Kind:             kind,
		Label:            label,
		SessionID:        parentSession,
		status:           Running,
		startedAt:        startedAt,
		activityAt:       startedAt,
		cancel:           cancel,
		done:             make(chan struct{}),
		artifactPath:     logPath,
		artifactMetaPath: metaPath,
		artifactFile:     file,
		artifactComplete: artifactErr == "",
		artifactErr:      artifactErr,
	}
	key := jobKey(parentSession, id)
	m.jobs[key] = j
	m.order = append(m.order, key)
	m.mu.Unlock()

	m.emitIfActive(parentSession, event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: startedText(kind, id, label)})

	m.wg.Add(1)
	if m.stalledWarning > 0 {
		m.wg.Add(1)
		go m.monitorStalled(parentSession, j)
	}
	go func() {
		defer m.wg.Done()
		result, err := runRecovered(ctx, jobWriter{j}, run)
		j.mu.Lock()
		j.runReturned = true
		j.mu.Unlock()

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
		finishedAt := nowMs()
		if result != "" {
			result = secrets.Redact(result)
			j.mu.Lock()
			if j.artifactFile != nil {
				if _, writeErr := j.artifactFile.WriteString(result); writeErr != nil {
					j.artifactErr = writeErr.Error()
				}
			} else {
				j.result = result
			}
			j.tail = appendTail(j.tail, []byte(result), defaultTailBytes)
			j.mu.Unlock()
		}
		targetDir := m.artifactTargetDirForJob(j)
		j.mu.Lock()
		if j.artifactFile != nil {
			if closeErr := j.artifactFile.Close(); closeErr != nil && j.artifactErr == "" {
				j.artifactErr = closeErr.Error()
			}
			j.artifactFile = nil
		}
		if j.artifactErr != "" {
			j.artifactComplete = false
		}
		j.finishedAt = finishedAt
		if targetDir != "" {
			if moveErr := j.moveArtifactToDirLocked(targetDir); moveErr != nil {
				j.noteArtifactErr("migration: " + moveErr.Error())
			}
		}
		metaErr := m.writeJobMetaLocked(j, st)
		if metaErr != nil {
			j.noteArtifactErr("metadata: " + metaErr.Error())
		}
		j.mu.Unlock()
		// Queue the drain note (and emit the closing Notice) BEFORE publishing the
		// terminal status. Wait(nil)/resolve only block on Running jobs, so if the
		// status flipped to terminal before the note was queued, a Wait could observe
		// completion, skip j.done, and DrainCompletedNote would race ahead of the
		// bookkeeping (the TestDrainMultiple -race flake). Recording first makes an
		// observed terminal status imply the note is already queued.
		m.recordCompletion(parentSession, id, kind, label, st, err)

		j.mu.Lock()
		if j.status != Killed { // a concurrent Kill already published Killed — keep it
			j.status = st
		}
		if j.artifactPath != "" && j.artifactComplete {
			j.result = ""
			j.tail = nil
		}
		j.mu.Unlock()
		close(j.done)
	}()
	return j
}

func runRecovered(ctx context.Context, out io.Writer, run func(context.Context, io.Writer) (string, error)) (result string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error: panic: %v\n%s", r, debug.Stack())
		}
	}()
	return run(ctx, out)
}

func (m *Manager) openArtifactLocked(parentSession, id string) (logPath, metaPath string, file *os.File, artifactErr string) {
	dir := m.artifactDirLocked(parentSession)
	if dir == "" {
		return "", "", nil, "artifact directory unavailable"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return filepath.Join(dir, id+jobLogExt), filepath.Join(dir, id+jobMetaExt), nil, err.Error()
	}
	logPath = filepath.Join(dir, id+jobLogExt)
	metaPath = filepath.Join(dir, id+jobMetaExt)
	f, err := os.Create(logPath)
	if err != nil {
		return logPath, metaPath, nil, err.Error()
	}
	return logPath, metaPath, f, ""
}

func (m *Manager) artifactDirLocked(parentSession string) string {
	parentSession = strings.TrimSpace(parentSession)
	if parentSession != "" {
		if dir := strings.TrimSpace(m.artifactDirs[parentSession]); dir != "" {
			return dir
		}
	}
	if strings.TrimSpace(m.tempRoot) == "" {
		return ""
	}
	if parentSession == "" {
		return filepath.Join(m.tempRoot, "default")
	}
	return filepath.Join(m.tempRoot, parentSession)
}

func (m *Manager) writeJobMetaLocked(j *Job, st Status) error {
	if j.artifactMetaPath == "" {
		return nil
	}
	return writeMeta(j.artifactMetaPath, artifactMeta{
		ID:               j.ID,
		Kind:             j.Kind,
		Label:            j.Label,
		SessionID:        j.SessionID,
		Status:           st,
		StartedAt:        j.startedAt,
		FinishedAt:       j.finishedAt,
		ArtifactComplete: j.artifactComplete && j.artifactErr == "",
		ArtifactError:    j.artifactErr,
		LogPath:          filepath.Base(j.artifactPath),
	})
}

func (m *Manager) artifactTargetDirForJob(j *Job) string {
	if j == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session := strings.TrimSpace(j.SessionID)
	if session == "" {
		return ""
	}
	return strings.TrimSpace(m.artifactDirs[session])
}

func (j *Job) noteArtifactErr(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	if j.artifactErr == "" {
		j.artifactErr = msg
	} else {
		j.artifactErr += "; " + msg
	}
	j.artifactComplete = false
}

func (j *Job) moveArtifactToDirLocked(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" || j.artifactPath == "" {
		return nil
	}
	if filepath.Clean(filepath.Dir(j.artifactPath)) == filepath.Clean(dir) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	newLogPath := filepath.Join(dir, filepath.Base(j.artifactPath))
	if err := moveArtifactFile(j.artifactPath, newLogPath); err != nil {
		return err
	}
	j.artifactPath = newLogPath
	if j.artifactMetaPath != "" {
		j.artifactMetaPath = filepath.Join(dir, filepath.Base(j.artifactMetaPath))
	}
	return nil
}

func (m *Manager) monitorStalled(parentSession string, j *Job) {
	defer m.wg.Done()
	timer := time.NewTimer(m.stalledWarning)
	defer timer.Stop()
	for {
		select {
		case <-j.done:
			return
		case <-timer.C:
			j.mu.Lock()
			if j.runReturned || j.status != Running {
				j.mu.Unlock()
				return
			}
			idle := time.Since(time.UnixMilli(j.activityAt))
			if idle >= m.stalledWarning && !j.stalled {
				j.stalled = true
				j.mu.Unlock()
				m.recordStalled(parentSession, j.ID, j.Kind, j.Label)
				return
			}
			wait := m.stalledWarning - idle
			if wait <= 0 {
				wait = m.stalledWarning
			}
			j.mu.Unlock()
			timer.Reset(wait)
		}
	}
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
}

func (m *Manager) recordStalled(parentSession, id, kind, label string) {
	tag := id
	if label != "" {
		tag = fmt.Sprintf("%s (%s)", id, label)
	}
	parentSession = strings.TrimSpace(parentSession)
	m.mu.Lock()
	if parentSession != "" && m.destroying[parentSession] {
		m.mu.Unlock()
		return
	}
	text := fmt.Sprintf("%s may be stalled — still running after %s with no visible output. Inspect it with wait or bash_output, or stop it with kill_shell.", tag, m.stalledWarning.Round(time.Second))
	m.completed = append(m.completed, completion{sessionID: parentSession, text: text})
	active := m.active
	shouldEmit := active == "" || parentSession == "" || active == parentSession
	m.mu.Unlock()
	if shouldEmit {
		m.sink.Emit(event.Event{
			Kind:  event.Notice,
			Level: event.LevelWarn,
			Text:  fmt.Sprintf("background %s may be stalled: %s — still running after %s with no visible output; inspect with wait/bash_output or stop with kill_shell", kind, id, m.stalledWarning.Round(time.Second)),
		})
	}
}

func (m *Manager) get(parentSession, id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.findJobLocked(parentSession, id)
}

func (m *Manager) findJobLocked(parentSession, id string) *Job {
	parentSession = strings.TrimSpace(parentSession)
	id = strings.TrimSpace(id)
	if parentSession != "" {
		return m.jobs[jobKey(parentSession, id)]
	}
	for _, key := range m.order {
		j := m.jobs[key]
		if j != nil && j.ID == id {
			return j
		}
	}
	return nil
}

// Output returns the job's output produced since the last Output call plus its
// current status. ok is false when the id is unknown.
func (m *Manager) Output(id string) (text string, status Status, ok bool) {
	return m.OutputForSession("", id)
}

// OutputForSession returns output only when id belongs to parentSession. Empty
// parentSession preserves the legacy unscoped behavior.
func (m *Manager) OutputForSession(parentSession, id string) (text string, status Status, ok bool) {
	j := m.get(parentSession, id)
	if j == nil {
		return "", "", false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.artifactPath != "" {
		text = j.readArtifactSinceOffsetLocked()
	} else {
		full := string(j.tail)
		if j.readOffset < int64(len(full)) {
			text = full[j.readOffset:]
			j.readOffset = int64(len(full))
		}
	}
	// A task job streams nothing to the buffer — its answer lands in result. Once
	// it is terminal with no buffered output, surface that result once so a task's
	// answer is visible here too (bash_output's description promises task support).
	if text == "" && j.status != Running && j.result != "" && !j.resultRead {
		text = j.result
		j.resultRead = true
	}
	if j.artifactErr != "" {
		if text != "" {
			text += "\n"
		}
		text += "job artifact incomplete: " + j.artifactErr
	}
	return text, j.status, true
}

func (j *Job) readArtifactSinceOffsetLocked() string {
	f, err := os.Open(j.artifactPath)
	if err != nil {
		if j.artifactErr == "" {
			j.artifactErr = err.Error()
		}
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		if j.artifactErr == "" {
			j.artifactErr = err.Error()
		}
		return ""
	}
	size := info.Size()
	if j.readOffset > size {
		j.readOffset = size
		return ""
	}
	if _, err := f.Seek(j.readOffset, io.SeekStart); err != nil {
		if j.artifactErr == "" {
			j.artifactErr = err.Error()
		}
		return ""
	}
	b, err := io.ReadAll(f)
	if err != nil {
		if j.artifactErr == "" {
			j.artifactErr = err.Error()
		}
		return ""
	}
	text := string(b)
	j.readOffset = size
	return text
}

func (j *Job) readArtifactAllLocked() string {
	if j.artifactPath == "" {
		return ""
	}
	b, err := os.ReadFile(j.artifactPath)
	if err != nil {
		if j.artifactErr == "" {
			j.artifactErr = err.Error()
		}
		return ""
	}
	return string(b)
}

// Kill cancels a running job. Returns false when the id is unknown or the job has
// already finished.
func (m *Manager) Kill(id string) bool {
	return m.KillForSession("", id)
}

// KillForSession cancels a running job only when it belongs to parentSession.
// Empty parentSession preserves the legacy unscoped behavior.
func (m *Manager) KillForSession(parentSession, id string) bool {
	j := m.get(parentSession, id)
	if j == nil {
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
		for _, key := range m.order {
			j := m.jobs[key]
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
		if j := m.findJobLocked(parentSession, id); j != nil {
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
		if text == "" && j.artifactPath != "" {
			text = j.readArtifactAllLocked()
		}
		if text == "" {
			text = string(j.tail)
		}
		if j.artifactErr != "" {
			if text != "" {
				text += "\n"
			}
			text += "job artifact incomplete: " + j.artifactErr
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
	for _, key := range m.order {
		j := m.jobs[key]
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

// HasUnfinishedForSession reports whether parentSession owns any job whose
// goroutine has not fully exited yet. Empty parentSession preserves the legacy
// unscoped behavior.
func (m *Manager) HasUnfinishedForSession(parentSession string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range m.order {
		j := m.jobs[key]
		if !sessionMatches(parentSession, j.SessionID) {
			continue
		}
		select {
		case <-j.done:
		default:
			return true
		}
	}
	return false
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
	return "Background job updates since your last message: " + strings.Join(c, "; ") +
		". Read their output with bash_output or wait if you still need it."
}

// SetActiveSession controls which session receives lifecycle notices for jobs
// that finish asynchronously. Empty active session preserves legacy behavior.
func (m *Manager) SetActiveSession(parentSession string) {
	m.mu.Lock()
	m.active = strings.TrimSpace(parentSession)
	m.mu.Unlock()
}

// SetActiveSessionPath binds a parent session id to its persistent transcript
// path, migrates any temporary artifacts, and loads completed job tombstones from
// the session sidecar.
func (m *Manager) SetActiveSessionPath(parentSession, sessionPath string) {
	parentSession = strings.TrimSpace(parentSession)
	sessionPath = strings.TrimSpace(sessionPath)
	m.mu.Lock()
	m.active = parentSession
	if parentSession == "" || sessionPath == "" {
		m.mu.Unlock()
		return
	}
	oldDir := m.artifactDirLocked(parentSession)
	adoptDefault := false
	if _, hasDir := m.artifactDirs[parentSession]; !hasDir && m.hasUnscopedJobsLocked() {
		oldDir = m.artifactDirLocked("")
		adoptDefault = true
	}
	newDir := ArtifactDir(sessionPath)
	m.artifactDirs[parentSession] = newDir
	loaded := m.loaded[parentSession]
	m.mu.Unlock()

	if oldDir != "" && newDir != "" && oldDir != newDir {
		oldSession := parentSession
		if adoptDefault {
			oldSession = ""
		}
		if err := m.migrateArtifactDirForSession(oldSession, oldDir, newDir); err != nil {
			if adoptDefault {
				m.mu.Lock()
				m.adoptUnscopedJobsLocked(parentSession)
				m.mu.Unlock()
			}
			m.recordArtifactMigrationError(parentSession, err)
		} else {
			m.mu.Lock()
			if adoptDefault {
				m.adoptUnscopedJobsLocked(parentSession)
			}
			m.mu.Unlock()
		}
	}
	if !loaded {
		m.loadSessionArtifacts(parentSession, newDir)
	}
}

func (m *Manager) hasUnscopedJobsLocked() bool {
	for _, j := range m.jobs {
		if j != nil && strings.TrimSpace(j.SessionID) == "" {
			return true
		}
	}
	return false
}

func (m *Manager) adoptUnscopedJobsLocked(parentSession string) {
	parentSession = strings.TrimSpace(parentSession)
	if parentSession == "" {
		return
	}
	for i := range m.completed {
		if strings.TrimSpace(m.completed[i].sessionID) == "" {
			m.completed[i].sessionID = parentSession
		}
	}
	for oldKey, j := range m.jobs {
		if j == nil || strings.TrimSpace(j.SessionID) != "" {
			continue
		}
		newKey := jobKey(parentSession, j.ID)
		if existing := m.jobs[newKey]; existing != nil && existing != j {
			j.mu.Lock()
			j.artifactErr = "migration: job id collision while adopting temporary session"
			j.artifactComplete = false
			j.mu.Unlock()
			continue
		}
		delete(m.jobs, oldKey)
		j.SessionID = parentSession
		m.jobs[newKey] = j
		for i, key := range m.order {
			if key == oldKey {
				m.order[i] = newKey
			}
		}
	}
}

func (m *Manager) recordArtifactMigrationError(parentSession string, err error) {
	text := "job artifact migration failed: " + err.Error()
	m.mu.Lock()
	for _, j := range m.jobs {
		if j == nil || !sessionMatches(parentSession, j.SessionID) {
			continue
		}
		j.mu.Lock()
		if j.artifactErr == "" {
			j.artifactErr = "migration: " + err.Error()
			j.artifactComplete = false
		}
		j.mu.Unlock()
	}
	active := m.active
	m.mu.Unlock()
	if active == "" || active == parentSession {
		m.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Job artifact migration failed.", Detail: text})
	}
}

type artifactMigrationJob struct {
	job     *Job
	wasOpen bool
}

func (m *Manager) migrateArtifactDirForSession(parentSession, oldDir, newDir string) error {
	locked := m.lockArtifactJobsForMigration(parentSession, oldDir)
	defer unlockArtifactMigrationJobs(locked)
	skip := openArtifactMigrationFiles(locked)
	migrateErr := migrateArtifactDirSkipping(oldDir, newDir, skip)
	if migrateErr == nil {
		rebaseArtifactMigrationJobs(locked, newDir)
	}
	return migrateErr
}

func (m *Manager) lockArtifactJobsForMigration(parentSession, dir string) []artifactMigrationJob {
	parentSession = strings.TrimSpace(parentSession)
	dir = filepath.Clean(strings.TrimSpace(dir))
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		if j == nil || strings.TrimSpace(j.SessionID) != parentSession {
			continue
		}
		jobs = append(jobs, j)
	}
	m.mu.Unlock()
	sort.Slice(jobs, func(i, k int) bool {
		return jobs[i].ID < jobs[k].ID
	})
	locked := make([]artifactMigrationJob, 0, len(jobs))
	for _, j := range jobs {
		j.mu.Lock()
		if !artifactPathInDir(j.artifactPath, dir) {
			j.mu.Unlock()
			continue
		}
		locked = append(locked, artifactMigrationJob{job: j, wasOpen: j.artifactFile != nil})
	}
	return locked
}

func artifactPathInDir(path, dir string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	dir = filepath.Clean(strings.TrimSpace(dir))
	if path == "." || dir == "." {
		return false
	}
	return filepath.Dir(path) == dir
}

func openArtifactMigrationFiles(jobs []artifactMigrationJob) map[string]bool {
	skip := map[string]bool{}
	for _, item := range jobs {
		j := item.job
		if j == nil || j.artifactFile == nil {
			continue
		}
		if j.artifactPath != "" {
			skip[filepath.Base(j.artifactPath)] = true
		}
		if j.artifactMetaPath != "" {
			skip[filepath.Base(j.artifactMetaPath)] = true
		}
	}
	return skip
}

func rebaseArtifactMigrationJobs(jobs []artifactMigrationJob, dir string) {
	for _, item := range jobs {
		j := item.job
		if j == nil || item.wasOpen {
			continue
		}
		if j.artifactPath != "" {
			j.artifactPath = filepath.Join(dir, filepath.Base(j.artifactPath))
		}
		if j.artifactMetaPath != "" {
			j.artifactMetaPath = filepath.Join(dir, filepath.Base(j.artifactMetaPath))
		}
	}
}

func unlockArtifactMigrationJobs(jobs []artifactMigrationJob) {
	for i := len(jobs) - 1; i >= 0; i-- {
		if jobs[i].job != nil {
			jobs[i].job.mu.Unlock()
		}
	}
}

func migrateArtifactDir(src, dst string) error {
	return migrateArtifactDirSkipping(src, dst, nil)
}

func migrateArtifactDirSkipping(src, dst string, skip map[string]bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if skip[entry.Name()] {
			continue
		}
		if err := moveArtifactFile(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	_ = os.Remove(src)
	return nil
}

func moveArtifactFile(src, dst string) error {
	if err := renamePath(src, dst); err == nil {
		return nil
	}
	if err := copyArtifactFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyArtifactFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	return nil
}

func (m *Manager) loadSessionArtifacts(parentSession, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		m.mu.Lock()
		m.loaded[parentSession] = true
		m.mu.Unlock()
		return
	}
	var loaded []*Job
	maxSeq := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != jobMetaExt {
			continue
		}
		meta, err := readMeta(filepath.Join(dir, entry.Name()))
		if err != nil || strings.TrimSpace(meta.ID) == "" {
			continue
		}
		id := strings.TrimSpace(meta.ID)
		if seq := maxJobSeq(id); seq > maxSeq {
			maxSeq = seq
		}
		done := make(chan struct{})
		close(done)
		logPath := filepath.Join(dir, id+jobLogExt)
		if strings.TrimSpace(meta.LogPath) != "" {
			logPath = filepath.Join(dir, filepath.Base(meta.LogPath))
		}
		loaded = append(loaded, &Job{
			ID:               id,
			Kind:             meta.Kind,
			Label:            meta.Label,
			SessionID:        parentSession,
			status:           meta.Status,
			startedAt:        meta.StartedAt,
			finishedAt:       meta.FinishedAt,
			activityAt:       meta.FinishedAt,
			done:             done,
			artifactPath:     logPath,
			artifactMetaPath: filepath.Join(dir, id+jobMetaExt),
			artifactComplete: meta.ArtifactComplete,
			artifactErr:      meta.ArtifactError,
			tombstone:        true,
		})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range loaded {
		key := jobKey(parentSession, j.ID)
		if _, exists := m.jobs[key]; exists {
			continue
		}
		m.jobs[key] = j
		m.order = append(m.order, key)
	}
	if maxSeq > m.seq {
		m.seq = maxSeq
	}
	m.loaded[parentSession] = true
}

// BeginDestroySession marks a parent session as being removed from active use
// and cancels its running jobs. WaitTeardown waits for the returned handle.
func (m *Manager) BeginDestroySession(parentSession string) SessionTeardown {
	parentSession = strings.TrimSpace(parentSession)
	if parentSession == "" {
		return SessionTeardown{}
	}
	var cancels []context.CancelFunc
	var targets []teardownTarget
	m.mu.Lock()
	m.destroying[parentSession] = true
	remaining := m.completed[:0]
	for _, item := range m.completed {
		if item.sessionID != parentSession {
			remaining = append(remaining, item)
		}
	}
	m.completed = remaining
	for _, key := range m.order {
		j := m.jobs[key]
		if !sessionMatches(parentSession, j.SessionID) {
			continue
		}
		j.mu.Lock()
		switch j.status {
		case Running:
			j.status = Killed
			cancels = append(cancels, j.cancel)
			targets = append(targets, teardownTarget{info: TeardownJob{ID: j.ID, Kind: j.Kind, Label: j.Label}, done: j.done})
		case Killed:
			targets = append(targets, teardownTarget{info: TeardownJob{ID: j.ID, Kind: j.Kind, Label: j.Label}, done: j.done})
		}
		j.mu.Unlock()
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return SessionTeardown{SessionID: parentSession, targets: targets}
}

// DestroySession preserves the legacy channel-based destroy API.
func (m *Manager) DestroySession(parentSession string) []<-chan struct{} {
	return m.BeginDestroySession(parentSession).DoneChannels()
}

// WaitTeardown waits for a destroy handle to unwind up to grace. A timed-out
// result means the caller should defer physical cleanup until the jobs exit.
func (m *Manager) WaitTeardown(ctx context.Context, h SessionTeardown, grace time.Duration) TeardownResult {
	result, timedOut := waitTeardownTargets(ctx, h.targets, grace)
	if timedOut {
		m.emitTeardownTimeout("destroy session "+h.SessionID, result)
	}
	return result
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
	delete(m.artifactDirs, parentSession)
	delete(m.loaded, parentSession)
	m.purgeSessionLocked(parentSession)
	m.mu.Unlock()
}

func (m *Manager) purgeSessionLocked(parentSession string) {
	kept := m.order[:0]
	for _, key := range m.order {
		j := m.jobs[key]
		if j == nil || sessionMatches(parentSession, j.SessionID) {
			delete(m.jobs, key)
			continue
		}
		kept = append(kept, key)
	}
	m.order = kept
}

// Close cancels the session context and waits briefly for every background job
// goroutine to return before unblocking. If a non-cooperative job ignores
// cancellation, cleanup of the temporary artifact root continues in the
// background after the goroutines eventually unwind.
func (m *Manager) Close() {
	_ = m.CloseWithGrace(m.teardownGrace)
}

// CloseAsync cancels the manager and returns immediately. It is used when a
// caller has already begun session-specific teardown and owns the delayed
// persistent cleanup, but still needs the manager's root context and temporary
// artifact root released eventually.
func (m *Manager) CloseAsync() {
	m.cancel()
	go func() {
		m.wg.Wait()
		m.removeTempRoot()
	}()
}

// CloseWithGrace is Close with an explicit wait window, used by tests and
// callers that need to surface non-cooperative jobs.
func (m *Manager) CloseWithGrace(grace time.Duration) TeardownResult {
	m.cancel()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	result, timedOut := waitTeardownTargets(context.Background(), m.closeTargets(), grace, done)
	if timedOut {
		m.emitTeardownTimeout("close", result)
		go func() {
			<-done
			m.removeTempRoot()
		}()
		return result
	}
	m.removeTempRoot()
	return result
}

func waitTeardownTargets(ctx context.Context, targets []teardownTarget, grace time.Duration, allDone ...<-chan struct{}) (TeardownResult, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	var timeout <-chan time.Time
	if grace >= 0 {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		timeout = timer.C
	}
	if len(allDone) > 0 && allDone[0] != nil {
		select {
		case <-allDone[0]:
			return TeardownResult{}, false
		case <-ctx.Done():
			return teardownTimedOut(targets, time.Since(start)), false
		case <-timeout:
			return teardownTimedOut(targets, time.Since(start)), true
		}
	}
	for _, target := range targets {
		select {
		case <-target.done:
		case <-ctx.Done():
			return teardownTimedOut(targets, time.Since(start)), false
		case <-timeout:
			return teardownTimedOut(targets, time.Since(start)), true
		}
	}
	return TeardownResult{}, false
}

func teardownTimedOut(targets []teardownTarget, waited time.Duration) TeardownResult {
	var out []TeardownJob
	for _, target := range targets {
		select {
		case <-target.done:
			continue
		default:
		}
		info := target.info
		info.Waited = waited
		out = append(out, info)
	}
	return TeardownResult{TimedOut: out}
}

func (m *Manager) closeTargets() []teardownTarget {
	m.mu.Lock()
	defer m.mu.Unlock()
	var targets []teardownTarget
	for _, key := range m.order {
		j := m.jobs[key]
		if j == nil {
			continue
		}
		select {
		case <-j.done:
			continue
		default:
		}
		j.mu.Lock()
		switch j.status {
		case Running:
			j.status = Killed
			targets = append(targets, teardownTarget{info: TeardownJob{ID: j.ID, Kind: j.Kind, Label: j.Label}, done: j.done})
		case Killed:
			targets = append(targets, teardownTarget{info: TeardownJob{ID: j.ID, Kind: j.Kind, Label: j.Label}, done: j.done})
		}
		j.mu.Unlock()
	}
	return targets
}

func (m *Manager) emitTeardownTimeout(action string, result TeardownResult) {
	if len(result.TimedOut) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "background job teardown timed out during %s", strings.TrimSpace(action))
	for i, job := range result.TimedOut {
		if i == 0 {
			b.WriteString(": ")
		} else {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s kind=%s", job.ID, job.Kind)
		if strings.TrimSpace(job.Label) != "" {
			fmt.Fprintf(&b, " label=%q", job.Label)
		}
		if job.Waited > 0 {
			fmt.Fprintf(&b, " waited=%s", job.Waited.Round(time.Millisecond))
		}
	}
	m.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Background job teardown timed out.", Detail: b.String()})
}

func (m *Manager) removeTempRoot() {
	if m.tempRoot != "" {
		_ = os.RemoveAll(m.tempRoot)
	}
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

func jobKey(parentSession, id string) string {
	return strings.TrimSpace(parentSession) + "\x00" + strings.TrimSpace(id)
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

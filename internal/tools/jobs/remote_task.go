package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

var (
	// ErrRemoteTaskIncomplete refuses a checkpoint missing an identity field.
	ErrRemoteTaskIncomplete = errors.New("incomplete remote task checkpoint")
	// ErrRemoteTaskOwned refuses a checkpoint another live job already follows.
	ErrRemoteTaskOwned = errors.New("remote task already has a live owner")
)

type remoteJob struct {
	task      tool.RemoteTask
	startErr  error
	finishErr error
	settled   bool
	release   func()
	terminal  *remoteTaskTerminal
	delivery  remoteDelivery
	ready     *CompletionEvent
}

type remoteDelivery uint8

const (
	remoteInline remoteDelivery = iota
	remoteBackground
	remotePublished
)

func (m *Manager) remoteCompletion(j *Job, ref string, st Status, err error) CompletionEvent {
	j.mu.Lock()
	remote := j.remote
	if remote != nil && (!remote.settled || st == Interrupted) {
		j.mu.Unlock()
		return CompletionEvent{}
	}
	j.mu.Unlock()
	ev := m.recordCompletion(j.SessionID, j.ID, j.Kind, j.Label, ref, st, err)
	if remote == nil {
		return ev
	}
	m.ackCompletion(ev.ID)
	j.mu.Lock()
	remote.ready = &ev
	j.mu.Unlock()
	return CompletionEvent{}
}

func (m *Manager) promoteRemote(j *Job) {
	j.mu.Lock()
	remote := j.remote
	if remote.delivery != remoteInline {
		j.mu.Unlock()
		return
	}
	remote.delivery = remoteBackground
	j.mu.Unlock()
	m.publishRemoteCompletion(j)
}

func (m *Manager) publishRemoteCompletion(j *Job) {
	j.mu.Lock()
	remote := j.remote
	select {
	case <-j.done:
	default:
		j.mu.Unlock()
		return
	}
	if remote == nil || remote.delivery != remoteBackground || remote.ready == nil || remote.ready.ID == "" {
		j.mu.Unlock()
		return
	}
	remote.delivery = remotePublished
	ev := *remote.ready
	j.mu.Unlock()
	m.mu.Lock()
	if !m.destroying[ev.SessionID] {
		m.completed = append(m.completed, completion{sessionID: ev.SessionID, text: ev.Note, eventID: ev.ID})
	}
	m.mu.Unlock()
	m.publishCompletion(ev)
}

var remoteTaskClaims sync.Map

func claimRemoteTask(path string) (func(), bool) {
	if _, exists := remoteTaskClaims.LoadOrStore(path, struct{}{}); exists {
		return nil, false
	}
	return func() { remoteTaskClaims.Delete(path) }, true
}

type remoteTaskRecord struct {
	Version int             `json:"version"`
	Task    tool.RemoteTask `json:"task"`
}

type remoteTaskTerminal struct {
	Version    int    `json:"version"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	ResultFile string `json:"resultFile,omitempty"`
}

func (m *Manager) SetRemoteTaskResumer(resume tool.RemoteTaskResumer) {
	m.mu.Lock()
	m.remoteResumer = resume
	m.mu.Unlock()
}

func remotePath(j *Job, suffix string) string {
	return strings.TrimSuffix(j.artifact.metaPath, jobMetaExt) + suffix
}

func (m *Manager) prepareRemote(j *Job) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.artifact.err != "" || j.artifact.metaPath == "" {
		j.remote.startErr = fmt.Errorf("remote task checkpoint unavailable: %s", j.artifact.err)
	} else {
		path := remotePath(j, ".remote-task")
		release, ok := claimRemoteTask(path)
		if !ok {
			j.remote.startErr = ErrRemoteTaskOwned
		} else {
			j.remote.release = release
			j.remote.startErr = createSidecar(path, remoteTaskRecord{Version: 1, Task: j.remote.task})
			if j.remote.startErr != nil {
				release()
				j.remote.release = nil
			}
		}
	}
	return j.remote.startErr
}

// StartRemoteForSession checkpoints accepted work before its first poll.
func (m *Manager) StartRemoteForSession(session string, task tool.RemoteTask, run tool.RemoteTaskRun) (tool.RemoteTaskHandle, error) {
	if err := validatePathSegment(session, "parentSession"); err != nil {
		return tool.RemoteTaskHandle{}, err
	}
	if task.Server == "" || task.Tool == "" || task.TaskID == "" || task.Binding == "" || task.Protocol == "" || task.Deadline <= 0 || run == nil {
		return tool.RemoteTaskHandle{}, ErrRemoteTaskIncomplete
	}
	remote := &remoteJob{task: task}
	j := m.StartForSession(session, "mcp_task", task.Server+"."+task.Tool,
		m.remoteRunner(session, remote, run), func(j *Job) { j.remote = remote })
	if remote.startErr != nil {
		return tool.RemoteTaskHandle{}, remote.startErr
	}
	return tool.RemoteTaskHandle{
		ID: j.ID,
		Wait: func(ctx context.Context) (json.RawMessage, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-j.done:
			}
			j.mu.Lock()
			err := remote.finishErr
			j.mu.Unlock()
			raw, readErr := m.readRemoteResult(j)
			if err != nil {
				return raw, err
			}
			return raw, readErr
		},
		Cancel:  func() { m.KillForSession(session, j.ID) },
		Promote: func() { m.promoteRemote(j) },
	}, nil
}

func (m *Manager) remoteRunner(session string, remote *remoteJob, run tool.RemoteTaskRun) func(context.Context, io.Writer) (string, error) {
	return func(ctx context.Context, out io.Writer) (string, error) {
		if remote.release != nil {
			defer remote.release()
		}
		ctx = tool.WithRemoteTaskDetach(ctx, func() bool { return m.root.Err() != nil })
		j, _ := ctx.Value(jobCtxKey{}).(*Job)
		if remote.terminal != nil {
			var err error
			if remote.terminal.Error != "" {
				err = errors.New(remote.terminal.Error)
			}
			j.mu.Lock()
			remote.settled, remote.finishErr = true, err
			j.mu.Unlock()
			return remote.terminal.Output, err
		}
		result, err := run(ctx, out)
		resultFile := ""
		if len(result.Result) > 0 {
			ref, saveErr := m.SaveToolResultForSession(session, j.ID, result.Result)
			if saveErr != nil {
				err = errors.Join(err, fmt.Errorf("retain remote result: %w", saveErr))
				result.Terminal = false
			} else {
				resultFile, _ = resultFilename(ref)
				result.Output += "\n[MCP result artifact: " + ref + "]"
			}
		}
		j.mu.Lock()
		defer j.mu.Unlock()
		if result.Terminal {
			terminal := remoteTaskTerminal{Version: 1, Output: result.Output, ResultFile: resultFile}
			if err != nil {
				terminal.Error = err.Error()
			}
			if saveErr := writeSidecar(remotePath(j, ".remote-terminal"), terminal); saveErr != nil {
				err = errors.Join(err, fmt.Errorf("retain remote terminal state: %w", saveErr))
			} else {
				remote.settled = true
			}
		}
		remote.finishErr = err
		return result.Output, err
	}
}

func (m *Manager) readRemoteResult(j *Job) (json.RawMessage, error) {
	j.mu.Lock()
	dir, terminalPath := filepath.Dir(j.artifact.metaPath), remotePath(j, ".remote-terminal")
	j.mu.Unlock()
	b, err := os.ReadFile(terminalPath)
	if err != nil {
		return nil, err
	}
	var terminal remoteTaskTerminal
	if err := json.Unmarshal(b, &terminal); err != nil {
		return nil, err
	}
	if terminal.Version != 1 || terminal.ResultFile == "" || filepath.Base(terminal.ResultFile) != terminal.ResultFile {
		return nil, fmt.Errorf("unsupported remote result reference")
	}
	b, err = os.ReadFile(filepath.Join(dir, terminal.ResultFile))
	if err != nil {
		return nil, err
	}
	var saved toolResultArtifact
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, err
	}
	if saved.Version != 1 || !json.Valid(saved.Result) {
		return nil, fmt.Errorf("unsupported remote result artifact")
	}
	return saved.Result, nil
}

func (m *Manager) markRemoteCancellation(j *Job) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.remote == nil {
		return nil
	}
	return writeSidecar(remotePath(j, ".remote-cancel"), struct {
		Version int `json:"version"`
	}{1})
}

func (m *Manager) resumeRemoteJobs(session, sessionPath string, loaded []*Job) {
	m.mu.Lock()
	resume := m.remoteResumer
	m.mu.Unlock()
	if resume == nil || m.sessionOwnershipProbe == nil || !m.sessionOwnershipProbe(sessionPath) {
		for _, j := range loaded {
			if j.Kind != "mcp_task" {
				continue
			}
			if _, err := os.Stat(remotePath(j, ".remote-task")); err == nil {
				m.mu.Lock()
				m.loaded[session] = false
				m.mu.Unlock()
				break
			}
		}
		return
	}
	for _, j := range loaded {
		if err := m.resumeRemoteJob(j, resume); err != nil {
			m.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
				Text: "Remote task recovery is unavailable; the original submission was not repeated.", Detail: j.ID + ": " + err.Error()})
		}
	}
}

func (m *Manager) resumeRemoteJob(j *Job, resume tool.RemoteTaskResumer) error {
	m.mu.Lock()
	registered := m.jobs[jobKey(j.SessionID, j.ID)] == j
	m.mu.Unlock()
	if !registered {
		return nil
	}
	if j.Kind != "mcp_task" || validatePathSegment(j.ID, "job") != nil {
		return nil
	}
	path := remotePath(j, ".remote-task")
	release, claimed := claimRemoteTask(path)
	if !claimed {
		return nil
	}
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	b, err := os.ReadFile(remotePath(j, ".remote-task"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var record remoteTaskRecord
	if err := json.Unmarshal(b, &record); err != nil {
		return err
	}
	if record.Version != 1 || record.Task.TaskID == "" || record.Task.Server == "" {
		return fmt.Errorf("unsupported remote task checkpoint")
	}
	if j.status == Done {
		return nil
	}
	cancelBytes, cancelErr := os.ReadFile(remotePath(j, ".remote-cancel"))
	if cancelErr != nil && !os.IsNotExist(cancelErr) {
		return cancelErr
	}
	record.Task.CancelRequested = cancelErr == nil
	if record.Task.CancelRequested {
		var marker struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(cancelBytes, &marker); err != nil {
			return err
		}
		if marker.Version != 1 {
			return fmt.Errorf("unsupported remote cancellation checkpoint")
		}
	}
	if j.status == Killed && !record.Task.CancelRequested {
		return nil
	}
	run := func(ctx context.Context, out io.Writer) (tool.RemoteTaskResult, error) {
		return resume(ctx, record.Task, out)
	}
	var replay *remoteTaskTerminal
	terminalBytes, err := os.ReadFile(remotePath(j, ".remote-terminal"))
	if err == nil {
		if j.status != Interrupted && j.artifact.complete {
			return nil
		}
		var terminal remoteTaskTerminal
		if err := json.Unmarshal(terminalBytes, &terminal); err != nil {
			return err
		}
		if terminal.Version != 1 {
			return fmt.Errorf("unsupported remote terminal checkpoint")
		}
		replay = &terminal
	} else if !os.IsNotExist(err) {
		return err
	}
	j.mu.Lock()
	select {
	case <-j.done:
	default:
		j.mu.Unlock()
		return nil
	}
	f, err := os.OpenFile(j.artifact.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		j.mu.Unlock()
		return err
	}
	if err = f.Chmod(0o600); err != nil {
		j.mu.Unlock()
		_ = f.Close()
		return err
	}
	ctx, cancel := context.WithCancel(m.root)
	j.remote = &remoteJob{task: record.Task, release: release, terminal: replay, delivery: remoteBackground}
	j.runReturned = false
	j.status, j.cancel, j.done = Running, cancel, make(chan struct{})
	j.times.finished = 0
	j.artifact.file, j.artifact.complete, j.artifact.err = f, true, ""
	if err = m.writeJobMetaLocked(j, Running); err != nil {
		j.status = Interrupted
		j.artifact.close()
		close(j.done)
		cancel()
		j.mu.Unlock()
		return err
	}
	j.mu.Unlock()
	ctx = WithSession(ctx, j.SessionID)
	ctx = context.WithValue(ctx, jobCtxKey{}, j)
	started = true
	m.launchJob(ctx, j, m.remoteRunner(j.SessionID, j.remote, run))
	return nil
}

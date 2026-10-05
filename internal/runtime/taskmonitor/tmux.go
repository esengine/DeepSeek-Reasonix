package taskmonitor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TmuxRunner is the narrow command surface used by Adapter. Implementations
// must pass arguments as an array; callers never construct a shell command.
type TmuxRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type execTmuxRunner struct{ binary string }

func (r execTmuxRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binary, args...)
	return cmd.Output()
}

// Mapping records only resources created by this adapter.
type TmuxMapping struct {
	SchemaVersion int       `json:"schema_version"`
	TaskID        string    `json:"task_id"`
	ProjectDir    string    `json:"project_dir"`
	Session       string    `json:"session"`
	Window        string    `json:"window"`
	Pane          string    `json:"pane"`
	OwnerToken    string    `json:"owner_token,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	Stale         bool      `json:"stale"`
}

type TmuxResult struct {
	SchemaVersion int          `json:"schema_version"`
	TaskID        string       `json:"task_id"`
	Available     bool         `json:"available"`
	Idempotent    bool         `json:"idempotent"`
	Mapping       *TmuxMapping `json:"mapping,omitempty"`
	Error         *CtrlError   `json:"error,omitempty"`
}

// TmuxAdapter maps tasks to user-visible tmux windows. It never changes task
// state; the Task Store remains the sole source of truth.
type TmuxAdapter struct {
	store  Store
	runner TmuxRunner
	base   string
}

func NewTmuxAdapter(store Store, baseDir string) *TmuxAdapter {
	return &TmuxAdapter{store: store, runner: newDefaultTmuxRunner(), base: baseDir}
}

func NewTmuxAdapterWithRunner(store Store, baseDir string, runner TmuxRunner) *TmuxAdapter {
	return &TmuxAdapter{store: store, runner: runner, base: baseDir}
}

func newDefaultTmuxRunner() TmuxRunner {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return nil
	}
	return execTmuxRunner{binary: path}
}

func (a *TmuxAdapter) Attach(ctx context.Context, projectDir, taskID, requestedSession string) TmuxResult {
	if err := validateTmuxName(requestedSession); err != nil {
		return tmuxError(taskID, ErrTmuxInvalidName, err.Error())
	}
	snap, err := a.store.GetTask(ctx, projectDir, taskID)
	if err != nil {
		return tmuxError(taskID, ErrTmuxTaskError, "task lookup failed")
	}
	if snap == nil {
		return tmuxError(taskID, ErrTaskNotFound, "task not found")
	}
	if a.runner == nil {
		return tmuxUnavailable(taskID)
	}
	old, err := a.load(projectDir, taskID)
	if err != nil {
		return tmuxError(taskID, ErrTmuxMappingFailed, "mapping read failed")
	}
	if old != nil {
		if err := a.validateMapping(projectDir, taskID, old); err != nil {
			if err := a.removeMapping(projectDir, taskID); err != nil {
				return tmuxError(taskID, ErrTmuxMappingFailed, "invalid mapping removal failed")
			}
		} else if !old.Stale && a.ownsSession(ctx, old) {
			return TmuxResult{SchemaVersion: 1, TaskID: taskID, Available: true, Idempotent: true, Mapping: old}
		} else {
			old.Stale = true
			if err := a.save(projectDir, *old); err != nil {
				return tmuxError(taskID, ErrTmuxMappingFailed, "mapping update failed")
			}
		}
	}
	session := requestedSession
	if session == "" {
		session = defaultTmuxSessionName(taskID)
	}
	if err := validateTmuxName(session); err != nil {
		return tmuxError(taskID, ErrTmuxInvalidName, err.Error())
	}
	ownerToken, err := newTmuxOwnerToken()
	if err != nil {
		return tmuxError(taskID, ErrTmuxMappingFailed, "mapping ownership creation failed")
	}
	window := "task"
	if _, err := a.runner.Run(ctx, "new-session", "-d", "-s", session, "-n", window); err != nil {
		return tmuxError(taskID, ErrTmuxCommandFailed, "tmux session creation failed")
	}
	m := &TmuxMapping{SchemaVersion: 1, TaskID: taskID, ProjectDir: projectDir, Session: session, Window: window, Pane: session + ":" + window + ".0", OwnerToken: ownerToken, CreatedAt: time.Now().UTC()}
	if _, err := a.runner.Run(ctx, "set-option", "-t", tmuxSessionPaneTarget(session), tmuxOwnerOption, ownerToken); err != nil {
		// set-option may have reached the tmux server even when the client
		// reports an error. Clean up only through the ownership-checked command.
		_ = a.killOwnedSession(ctx, m)
		return tmuxError(taskID, ErrTmuxCommandFailed, "tmux ownership marker failed")
	}
	if err := a.save(projectDir, *m); err != nil {
		_ = a.killOwnedSession(ctx, m)
		return tmuxError(taskID, ErrTmuxMappingFailed, "mapping write failed")
	}
	return TmuxResult{SchemaVersion: 1, TaskID: taskID, Available: true, Mapping: m}
}

func (a *TmuxAdapter) Status(ctx context.Context, projectDir, taskID string) TmuxResult {
	m, err := a.load(projectDir, taskID)
	if err != nil {
		return tmuxError(taskID, ErrTmuxMappingFailed, "mapping read failed")
	}
	if m == nil {
		return TmuxResult{SchemaVersion: 1, TaskID: taskID, Available: a.runner != nil}
	}
	if err := a.validateMapping(projectDir, taskID, m); err != nil {
		return tmuxError(taskID, ErrTmuxMappingFailed, "mapping ownership validation failed")
	}
	if a.runner == nil {
		m.Stale = true
		return TmuxResult{SchemaVersion: 1, TaskID: taskID, Available: false, Mapping: m}
	}
	if !a.ownsSession(ctx, m) {
		m.Stale = true
		if err := a.save(projectDir, *m); err != nil {
			return tmuxError(taskID, ErrTmuxMappingFailed, "mapping update failed")
		}
	}
	return TmuxResult{SchemaVersion: 1, TaskID: taskID, Available: true, Mapping: m}
}

func (a *TmuxAdapter) Open(ctx context.Context, projectDir, taskID string) TmuxResult {
	r := a.Status(ctx, projectDir, taskID)
	if r.Mapping == nil || r.Mapping.Stale {
		return r
	}
	if a.runner == nil {
		return tmuxUnavailable(taskID)
	}
	if _, err := a.runner.Run(ctx, "switch-client", "-t", r.Mapping.Pane); err != nil {
		r.Error = &CtrlError{Code: ErrTmuxCommandFailed, Message: "tmux open failed"}
	}
	return r
}

func (a *TmuxAdapter) Detach(ctx context.Context, projectDir, taskID string) TmuxResult {
	m, err := a.load(projectDir, taskID)
	if err != nil {
		return tmuxError(taskID, ErrTmuxMappingFailed, "mapping read failed")
	}
	if m == nil {
		return TmuxResult{SchemaVersion: 1, TaskID: taskID, Available: a.runner != nil, Idempotent: true}
	}
	if err := a.validateMapping(projectDir, taskID, m); err != nil {
		if err := a.removeMapping(projectDir, taskID); err != nil {
			return tmuxError(taskID, ErrTmuxMappingFailed, "invalid mapping removal failed")
		}
		return tmuxError(taskID, ErrTmuxMappingFailed, "mapping ownership validation failed")
	}
	if a.runner != nil && !m.Stale {
		if a.ownsSession(ctx, m) {
			if err := a.killOwnedSession(ctx, m); err != nil && a.ownsSession(ctx, m) {
				return tmuxError(taskID, ErrTmuxCommandFailed, "tmux detach failed")
			}
			if a.ownsSession(ctx, m) {
				return tmuxError(taskID, ErrTmuxCommandFailed, "tmux detach failed")
			}
		} else {
			m.Stale = true
		}
	}
	if err := a.removeMapping(projectDir, taskID); err != nil {
		return tmuxError(taskID, ErrTmuxMappingFailed, "mapping removal failed")
	}
	return TmuxResult{SchemaVersion: 1, TaskID: taskID, Available: a.runner != nil, Idempotent: false, Mapping: m}
}

const (
	tmuxOwnerOption       = "@reasonix-owner"
	tmuxOwnerTokenBytes   = 16
	defaultTmuxNamePrefix = "reasonix-"

	ErrTmuxUnavailable   = "tmux_unavailable"
	ErrTmuxInvalidName   = "tmux_invalid_name"
	ErrTmuxCommandFailed = "tmux_command_failed"
	ErrTmuxMappingFailed = "tmux_mapping_failed"
	ErrTmuxTaskError     = "tmux_task_error"
)

func defaultTmuxSessionName(taskID string) string {
	candidate := defaultTmuxNamePrefix + taskID
	if len(candidate) <= 64 {
		return candidate
	}
	sum := sha256.Sum256([]byte(taskID))
	return defaultTmuxNamePrefix + hex.EncodeToString(sum[:16])
}

func newTmuxOwnerToken() (string, error) {
	raw := make([]byte, tmuxOwnerTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func tmuxUnavailable(taskID string) TmuxResult {
	return tmuxError(taskID, ErrTmuxUnavailable, "tmux is not available")
}

func tmuxError(taskID, code, message string) TmuxResult {
	return TmuxResult{SchemaVersion: 1, TaskID: taskID, Error: &CtrlError{Code: code, Message: message}}
}

func validateTmuxName(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > 64 {
		return errors.New("tmux name contains invalid characters")
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return errors.New("tmux name contains invalid characters")
		}
	}
	return nil
}

func (a *TmuxAdapter) validateMapping(projectDir, taskID string, m *TmuxMapping) error {
	if m == nil || m.SchemaVersion != 1 || m.TaskID != taskID {
		return errors.New("tmux mapping identity mismatch")
	}
	if err := validateTmuxName(m.Session); err != nil || m.Session == "" || m.Window != "task" || m.Pane != m.Session+":"+m.Window+".0" {
		return errors.New("tmux mapping target is invalid")
	}
	if len(m.OwnerToken) != tmuxOwnerTokenBytes*2 {
		return errors.New("tmux mapping owner token is missing")
	}
	if _, err := hex.DecodeString(m.OwnerToken); err != nil {
		return errors.New("tmux mapping owner token is invalid")
	}
	wantRoot, err := NewFileStore(a.base).taskRoot(projectDir)
	if err != nil {
		return err
	}
	gotRoot, err := NewFileStore(a.base).taskRoot(m.ProjectDir)
	if err != nil {
		return err
	}
	wantRoot, err = filepath.Abs(wantRoot)
	if err != nil {
		return err
	}
	gotRoot, err = filepath.Abs(gotRoot)
	if err != nil {
		return err
	}
	if filepath.Clean(gotRoot) != filepath.Clean(wantRoot) {
		return errors.New("tmux mapping project mismatch")
	}
	return nil
}

func (a *TmuxAdapter) ownsSession(ctx context.Context, m *TmuxMapping) bool {
	if a.runner == nil || m == nil {
		return false
	}
	out, err := a.runner.Run(ctx, "show-options", "-v", "-t", tmuxSessionPaneTarget(m.Session), tmuxOwnerOption)
	return err == nil && strings.TrimSpace(string(out)) == m.OwnerToken
}

// killOwnedSession performs the ownership comparison and destructive action in
// one tmux server command queue. A separate show-options + kill-session pair
// would allow the named session to be replaced between the check and the kill.
func (a *TmuxAdapter) killOwnedSession(ctx context.Context, m *TmuxMapping) error {
	if a.runner == nil || m == nil {
		return nil
	}
	condition := fmt.Sprintf("#{==:#{%s},%s}", tmuxOwnerOption, m.OwnerToken)
	killCommand := "kill-session -t =" + m.Session
	_, err := a.runner.Run(ctx, "if-shell", "-t", tmuxSessionPaneTarget(m.Session), "-F", condition, killCommand, "")
	return err
}

func tmuxSessionPaneTarget(session string) string {
	return "=" + session + ":"
}

func (a *TmuxAdapter) removeMapping(projectDir, taskID string) error {
	path, err := a.mappingPath(projectDir, taskID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove tmux mapping: %w", err)
	}
	return nil
}

func (a *TmuxAdapter) mappingPath(projectDir, taskID string) (string, error) {
	id, err := safeID(taskID)
	if err != nil {
		return "", err
	}
	root, err := NewFileStore(a.base).taskRoot(projectDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, ".tmux", id+".json")
	if err := rejectSymlinkChain(root, path); err != nil {
		return "", err
	}
	return path, nil
}

func (a *TmuxAdapter) load(projectDir, taskID string) (*TmuxMapping, error) {
	path, err := a.mappingPath(projectDir, taskID)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m TmuxMapping
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (a *TmuxAdapter) save(projectDir string, m TmuxMapping) error {
	path, err := a.mappingPath(projectDir, m.TaskID)
	if err != nil {
		return err
	}
	root, err := NewFileStore(a.base).taskRoot(projectDir)
	if err != nil {
		return err
	}
	if _, err := prepareTaskDir(root, ".tmux"); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmux-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return nil
}

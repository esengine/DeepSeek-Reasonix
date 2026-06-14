package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/fileutil"
)

// RuntimeMeta is the dynamic sidecar record that tracks the agent's runtime
// state — active goal, run status, and scheduler hints. It lives beside the
// session file at <session>.runtime.json and is the authoritative source for
// resumable state (desktop tab profiles are UI-only).
//
// The sidecar is independent of BranchMeta: branch meta is structural (tree
// lineage, topic), runtime meta is ephemeral execution state.
type RuntimeMeta struct {
	Version       int               `json:"version"`
	SessionID     string            `json:"session_id"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Model         string            `json:"model,omitempty"`
	WorkspaceRoot string            `json:"workspace_root,omitempty"`
	Goal          RuntimeGoalMeta   `json:"goal,omitempty"`
	Run           RuntimeRunMeta    `json:"run,omitempty"`
	Wait          RuntimeWaitMeta   `json:"wait,omitempty"`
	Scheduler     RuntimeSchedMeta  `json:"scheduler,omitempty"`
	FileWatch     RuntimeWatchMeta  `json:"file_watch,omitempty"`
	Budget        RuntimeBudgetMeta `json:"budget,omitempty"`
}

// RuntimeTimelineEvent is an append-only event record for daemon/runtime
// observability. It intentionally lives outside the transcript and provider
// prompt path.
type RuntimeTimelineEvent struct {
	Time       time.Time `json:"time"`
	Type       string    `json:"type"`
	Source     string    `json:"source,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	EventID    string    `json:"event_id,omitempty"`
	PayloadRef string    `json:"payload_ref,omitempty"`
	Step       string    `json:"step,omitempty"`
	Model      string    `json:"model,omitempty"`
	RunStatus  string    `json:"run_status,omitempty"`
	GoalStatus string    `json:"goal_status,omitempty"`
	WaitKind   string    `json:"wait_kind,omitempty"`
	WaitID     string    `json:"wait_id,omitempty"`
	Tool       string    `json:"tool,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	Prompt     int       `json:"prompt_tokens,omitempty"`
	Completion int       `json:"completion_tokens,omitempty"`
	Total      int       `json:"total_tokens,omitempty"`
	CacheHit   int       `json:"cache_hit_tokens,omitempty"`
	CacheMiss  int       `json:"cache_miss_tokens,omitempty"`
	Reasoning  int       `json:"reasoning_tokens,omitempty"`
	Finish     string    `json:"finish_reason,omitempty"`
	Cost       float64   `json:"cost,omitempty"`
	Currency   string    `json:"currency,omitempty"`
	Message    string    `json:"message,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// RuntimeGoalMeta captures the active goal's lifecycle.
type RuntimeGoalMeta struct {
	Text        string    `json:"text,omitempty"`
	Status      string    `json:"status,omitempty"` // running|complete|blocked|stopped
	Turns       int       `json:"turns,omitempty"`
	BlockCount  int       `json:"block_count,omitempty"`
	BlockReason string    `json:"block_reason,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

const (
	RunStatusIdle            = "idle"
	RunStatusQueued          = "queued"
	RunStatusPendingContinue = "pending_continue" // legacy alias for queued
	RunStatusRunning         = "running"
	RunStatusWaitingApproval = "waiting_approval"
	RunStatusWaitingAsk      = "waiting_ask"
	RunStatusWaitingEvent    = "waiting_event"
	RunStatusWaitingTime     = "waiting_time"
	RunStatusWaitingFile     = "waiting_file"
	RunStatusBlocked         = "blocked"
	RunStatusComplete        = "complete"
	RunStatusFailed          = "failed"
	RunStatusStopped         = "stopped"
	RunStatusInterrupted     = "interrupted"
)

// NormalizeRunStatus folds legacy or equivalent persisted values into the
// canonical run state machine. Unknown values are returned unchanged so older
// sidecars remain inspectable instead of being silently erased.
func NormalizeRunStatus(status string) string {
	switch status {
	case RunStatusPendingContinue:
		return RunStatusQueued
	default:
		return status
	}
}

// IsKnownRunStatus reports whether status is part of the runtime state machine.
func IsKnownRunStatus(status string) bool {
	switch NormalizeRunStatus(status) {
	case "",
		RunStatusIdle,
		RunStatusQueued,
		RunStatusRunning,
		RunStatusWaitingApproval,
		RunStatusWaitingAsk,
		RunStatusWaitingEvent,
		RunStatusWaitingTime,
		RunStatusWaitingFile,
		RunStatusBlocked,
		RunStatusComplete,
		RunStatusFailed,
		RunStatusStopped,
		RunStatusInterrupted:
		return true
	default:
		return false
	}
}

// IsRunInFlight reports whether a run state means the daemon/controller should
// not start another model turn for the same session.
func IsRunInFlight(status string) bool {
	switch NormalizeRunStatus(status) {
	case RunStatusQueued, RunStatusRunning:
		return true
	default:
		return false
	}
}

// RuntimeRunMeta captures the run loop's lifecycle.
type RuntimeRunMeta struct {
	Status           string    `json:"status,omitempty"` // idle|queued|running|waiting_*|blocked|complete|failed|stopped|interrupted
	LastTurnAt       time.Time `json:"last_turn_at,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	ResumeCount      int       `json:"resume_count,omitempty"`
	LastWakeupReason string    `json:"last_wakeup_reason,omitempty"`
}

// RuntimeWaitMeta captures a user-owned wait condition that paused a run.
type RuntimeWaitMeta struct {
	Kind            string    `json:"kind,omitempty"` // approval|ask|event|time|file
	Reason          string    `json:"reason,omitempty"`
	ApprovalID      string    `json:"approval_id,omitempty"`
	AskID           string    `json:"ask_id,omitempty"`
	Tool            string    `json:"tool,omitempty"`
	Subject         string    `json:"subject,omitempty"`
	EventSource     string    `json:"event_source,omitempty"`
	EventID         string    `json:"event_id,omitempty"`
	EventStatus     string    `json:"event_status,omitempty"`
	EventConclusion string    `json:"event_conclusion,omitempty"`
	FilePaths       []string  `json:"file_paths,omitempty"`
	Until           time.Time `json:"until,omitempty"`
	Since           time.Time `json:"since,omitempty"`
}

// RuntimeSchedMeta holds scheduler/wakeup state for future cron/webhook use.
type RuntimeSchedMeta struct {
	// Schedule configuration (set by user via /goal schedule or API).
	DailyAt  string        `json:"daily_at,omitempty"` // "HH:MM", interpreted in Timezone when set.
	Timezone string        `json:"timezone,omitempty"` // IANA timezone for DailyAt, e.g. "Asia/Shanghai".
	Interval time.Duration `json:"interval,omitempty"` // fixed interval between wakeups, e.g. 1h
	Enabled  bool          `json:"enabled,omitempty"`  // whether scheduling is active

	// Runtime state (managed by the scheduler).
	NextWakeupAt      time.Time `json:"next_wakeup_at,omitempty"`
	LastWakeupAt      time.Time `json:"last_wakeup_at,omitempty"`
	LastWakeupReason  string    `json:"last_wakeup_reason,omitempty"`
	LastWakeupEventID string    `json:"last_wakeup_event_id,omitempty"`
	LastWakeupKey     string    `json:"last_wakeup_key,omitempty"`
}

// RuntimeWatchMeta holds file-watch wakeup configuration for a session.
type RuntimeWatchMeta struct {
	// Paths are watched by the daemon and may be relative to the workspace root.
	Paths []string `json:"paths,omitempty"`
	// IgnorePatterns are glob patterns skipped in addition to daemon defaults.
	IgnorePatterns []string `json:"ignore_patterns,omitempty"`
	// Debounce delays wakeup until the file changes settle.
	Debounce time.Duration `json:"debounce,omitempty"`
	// Enabled controls whether file-change wakeups are active.
	Enabled bool `json:"enabled,omitempty"`
}

// RuntimeBudgetMeta holds deterministic automatic-wakeup budget state.
type RuntimeBudgetMeta struct {
	// DailyWakeupLimit limits automatic daemon wakeups per UTC day. 0 disables the limit.
	DailyWakeupLimit int `json:"daily_wakeup_limit,omitempty"`
	// MaxGoalAutoTurns caps automatic continuation turns for one goal. 0 uses the built-in default.
	MaxGoalAutoTurns int `json:"max_goal_auto_turns,omitempty"`
	// DailyModelCallLimit limits model completions per UTC day. 0 disables the limit.
	DailyModelCallLimit int `json:"daily_model_call_limit,omitempty"`
	// DailyModelCostLimit limits estimated model spend per UTC day. 0 disables the limit.
	DailyModelCostLimit float64 `json:"daily_model_cost_limit,omitempty"`
	// DailyWakeups counts automatic wakeups reserved in the current UTC day window.
	DailyWakeups int `json:"daily_wakeups,omitempty"`
	// DailyModelCalls counts model completions observed in the current UTC day window.
	DailyModelCalls int `json:"daily_model_calls,omitempty"`
	// DailyModelCost accumulates estimated model spend in the current UTC day window.
	DailyModelCost float64 `json:"daily_model_cost,omitempty"`
	// ModelCostCurrency is the display currency for DailyModelCost.
	ModelCostCurrency string `json:"model_cost_currency,omitempty"`
	// WindowStartedAt is the UTC start of the current accounting day.
	WindowStartedAt time.Time `json:"window_started_at,omitempty"`
	// LastBlockedAt records the most recent budget-blocked wakeup.
	LastBlockedAt time.Time `json:"last_blocked_at,omitempty"`
	// LastBlockedReason explains the most recent budget block.
	LastBlockedReason string `json:"last_blocked_reason,omitempty"`
}

const runtimeMetaVersion = 1

// RuntimeMetaPath returns the path to the runtime sidecar for a session file.
func RuntimeMetaPath(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionPath + ".runtime.json"
}

// RuntimeTimelinePath returns the append-only runtime timeline path.
func RuntimeTimelinePath(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionPath + ".runtime.timeline.jsonl"
}

// LoadRuntimeMeta reads the runtime sidecar. Returns (meta, true, nil) on
// success, (zero, false, nil) if the file does not exist, and (zero, false, err)
// on read/decode failure. A corrupt file is an error — callers decide whether to
// treat it as fatal or advisory.
func LoadRuntimeMeta(sessionPath string) (RuntimeMeta, bool, error) {
	path := RuntimeMetaPath(sessionPath)
	if path == "" {
		return RuntimeMeta{}, false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RuntimeMeta{}, false, nil
		}
		return RuntimeMeta{}, false, err
	}
	var m RuntimeMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return RuntimeMeta{}, false, fmt.Errorf("decode runtime meta %s: %w", path, err)
	}
	return m, true, nil
}

// SaveRuntimeMeta atomically writes the runtime sidecar to disk. It stamps
// Version and UpdatedAt automatically.
func SaveRuntimeMeta(sessionPath string, m RuntimeMeta) error {
	path := RuntimeMetaPath(sessionPath)
	if path == "" {
		return fmt.Errorf("empty session path")
	}
	m.Version = runtimeMetaVersion
	m.UpdatedAt = time.Now().UTC()
	if m.SessionID == "" {
		m.SessionID = BranchID(sessionPath)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".runtime.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

// RemoveRuntimeMeta deletes the runtime sidecar if it exists.
func RemoveRuntimeMeta(sessionPath string) error {
	path := RuntimeMetaPath(sessionPath)
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AppendRuntimeTimeline appends one timeline event beside a session runtime
// sidecar. The write is best-effort atomic at the single-line append level.
func AppendRuntimeTimeline(sessionPath string, e RuntimeTimelineEvent) error {
	path := RuntimeTimelinePath(sessionPath)
	if path == "" {
		return nil
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(e); err != nil {
		return err
	}
	return nil
}

// LoadRuntimeTimeline loads the most recent timeline events. limit <= 0 returns
// all events.
func LoadRuntimeTimeline(sessionPath string, limit int) ([]RuntimeTimelineEvent, bool, error) {
	path := RuntimeTimelinePath(sessionPath)
	if path == "" {
		return nil, false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()
	var events []RuntimeTimelineEvent
	dec := json.NewDecoder(f)
	for {
		var e RuntimeTimelineEvent
		if err := dec.Decode(&e); err != nil {
			if err == io.EOF {
				break
			}
			return nil, false, fmt.Errorf("decode runtime timeline %s: %w", path, err)
		}
		events = append(events, e)
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, true, nil
}

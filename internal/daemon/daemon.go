// Package daemon implements the Reasonix background agent service. It holds a
// session registry, exposes a localhost-only HTTP control API, and manages
// lifecycle transitions (interrupted recovery, goal continuation). A single
// daemon instance per user is enforced via a lockfile in the sessions directory.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// DefaultAddr is the localhost-only address the daemon listens on.
const DefaultAddr = "127.0.0.1:19840"

// Daemon is the long-running background service that tracks sessions and
// provides the control API.
type Daemon struct {
	addr       string
	sessionDir string
	logger     *slog.Logger

	mu              sync.RWMutex
	registry        map[string]*SessionEntry // session ID → entry
	server          *http.Server
	scheduler       *Scheduler
	fileWatcher     *FileWatcher
	webhookCfg      *WebhookConfig
	lockPath        string
	token           string
	intentCh        chan RunIntent
	activeRuns      map[string]*ActiveRun
	maxConcurrent   int
	buildController ControllerFactory
}

// SessionEntry is one tracked session in the registry.
type SessionEntry struct {
	ID           string            `json:"id"`
	Path         string            `json:"path"`
	Runtime      agent.RuntimeMeta `json:"runtime"`
	DiscoveredAt time.Time         `json:"discovered_at"`
}

// RunIntent is the normalized wakeup request consumed by the daemon worker.
type RunIntent struct {
	SessionID   string    `json:"session_id"`
	SessionPath string    `json:"session_path,omitempty"`
	Source      string    `json:"source"`
	Reason      string    `json:"reason,omitempty"`
	EventID     string    `json:"event_id,omitempty"`
	Context     string    `json:"context,omitempty"`
	Priority    int       `json:"priority,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ActiveRun is the daemon-owned controller currently driving a session.
type ActiveRun struct {
	Intent    RunIntent
	StartedAt time.Time
	Cancel    context.CancelFunc
	Control   *control.Controller
	Approvals map[string]event.Approval
	Asks      map[string]event.Ask
}

// ControllerFactory builds a resumed controller for a daemon worker intent.
type ControllerFactory func(context.Context, *Daemon, *SessionEntry, event.Sink) (*control.Controller, error)

// StatusResponse is the JSON body of GET /status.
type StatusResponse struct {
	Status      string            `json:"status"`
	Addr        string            `json:"addr"`
	Sessions    int               `json:"sessions"`
	Uptime      string            `json:"uptime"`
	PID         int               `json:"pid"`
	FileWatcher *FileWatcherStats `json:"file_watcher,omitempty"`
}

// SessionsResponse is the JSON body of GET /sessions.
type SessionsResponse struct {
	Sessions []SessionView `json:"sessions"`
}

// TimelineResponse is the JSON body of GET /timeline.
type TimelineResponse struct {
	SessionID string                       `json:"session_id"`
	Events    []agent.RuntimeTimelineEvent `json:"events"`
}

// ApprovalDeskResponse is the JSON body of GET /approvals.
type ApprovalDeskResponse struct {
	Items []ApprovalDeskItem `json:"items"`
}

// ApprovalDeskItem is one approval or ask currently blocking a run.
type ApprovalDeskItem struct {
	SessionID  string                 `json:"session_id"`
	Kind       string                 `json:"kind"` // approval|ask
	ID         string                 `json:"id,omitempty"`
	Tool       string                 `json:"tool,omitempty"`
	Subject    string                 `json:"subject,omitempty"`
	Reason     string                 `json:"reason,omitempty"`
	GoalText   string                 `json:"goal_text,omitempty"`
	GoalStatus string                 `json:"goal_status,omitempty"`
	RunStatus  string                 `json:"run_status,omitempty"`
	Active     bool                   `json:"active,omitempty"`
	Since      time.Time              `json:"since,omitempty"`
	Questions  []ApprovalDeskQuestion `json:"questions,omitempty"`
}

// ApprovalDeskQuestion is a frontend-friendly view of an ask question.
type ApprovalDeskQuestion struct {
	ID      string               `json:"id,omitempty"`
	Header  string               `json:"header,omitempty"`
	Prompt  string               `json:"prompt,omitempty"`
	Options []ApprovalDeskOption `json:"options,omitempty"`
	Multi   bool                 `json:"multi,omitempty"`
}

// ApprovalDeskOption is a frontend-friendly view of an ask option.
type ApprovalDeskOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// SessionView is the public representation of a session in the API.
type SessionView struct {
	ID                  string     `json:"id"`
	Path                string     `json:"path"`
	GoalText            string     `json:"goal_text,omitempty"`
	GoalStatus          string     `json:"goal_status,omitempty"`
	RunStatus           string     `json:"run_status,omitempty"`
	WaitKind            string     `json:"wait_kind,omitempty"`
	WaitReason          string     `json:"wait_reason,omitempty"`
	WaitID              string     `json:"wait_id,omitempty"`
	WaitTool            string     `json:"wait_tool,omitempty"`
	WaitSubject         string     `json:"wait_subject,omitempty"`
	Active              bool       `json:"active,omitempty"`
	Scope               string     `json:"scope,omitempty"`
	WorkspaceRoot       string     `json:"workspace_root,omitempty"`
	TopicID             string     `json:"topic_id,omitempty"`
	TopicTitle          string     `json:"topic_title,omitempty"`
	NextWakeupAt        *time.Time `json:"next_wakeup_at,omitempty"`
	DailyWakeupLimit    int        `json:"daily_wakeup_limit,omitempty"`
	DailyWakeups        int        `json:"daily_wakeups,omitempty"`
	MaxGoalAutoTurns    int        `json:"max_goal_auto_turns,omitempty"`
	DailyModelCallLimit int        `json:"daily_model_call_limit,omitempty"`
	DailyModelCalls     int        `json:"daily_model_calls,omitempty"`
	DailyModelCostLimit float64    `json:"daily_model_cost_limit,omitempty"`
	DailyModelCost      float64    `json:"daily_model_cost,omitempty"`
	ModelCostCurrency   string     `json:"model_cost_currency,omitempty"`
	BudgetBlockedReason string     `json:"budget_blocked_reason,omitempty"`
	Scheduled           bool       `json:"scheduled,omitempty"`
	Watched             bool       `json:"watched,omitempty"`
}

// Options configures daemon creation.
type Options struct {
	Addr              string
	SessionDir        string
	Logger            *slog.Logger
	Webhook           *WebhookConfig
	Token             string
	ControllerFactory ControllerFactory
	MaxConcurrentRuns int
}

// New creates a Daemon but does not start it.
func New(opts Options) *Daemon {
	addr := opts.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	sessionDir := opts.SessionDir
	if sessionDir == "" {
		sessionDir = config.SessionDir()
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	factory := opts.ControllerFactory
	if factory == nil {
		factory = defaultControllerFactory
	}
	maxConcurrent := opts.MaxConcurrentRuns
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &Daemon{
		addr:            addr,
		sessionDir:      sessionDir,
		logger:          logger,
		webhookCfg:      opts.Webhook,
		registry:        make(map[string]*SessionEntry),
		token:           strings.TrimSpace(opts.Token),
		intentCh:        make(chan RunIntent, 128),
		activeRuns:      make(map[string]*ActiveRun),
		maxConcurrent:   maxConcurrent,
		buildController: factory,
	}
}

// Start acquires the lockfile, scans existing sessions, recovers interrupted
// state, starts the scheduler, and starts the HTTP server. It blocks until ctx
// is cancelled or the server fails.
func (d *Daemon) Start(ctx context.Context) error {
	if err := d.acquireLock(); err != nil {
		return fmt.Errorf("daemon already running or lock error: %w", err)
	}
	defer d.releaseLock()
	if err := d.ensureToken(); err != nil {
		return fmt.Errorf("daemon token: %w", err)
	}

	d.scanSessions()
	d.recoverInterrupted()

	// Start the scheduler.
	d.scheduler = NewScheduler(d, d.logger)
	go d.scheduler.Start(ctx)
	go d.runIntentWorker(ctx)

	// Start the file watcher.
	d.fileWatcher = NewFileWatcher(d, d.logger)
	d.restoreFileWatches()
	go d.fileWatcher.Start(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", d.withAuth(d.handleStatus))
	mux.HandleFunc("GET /sessions", d.withAuth(d.handleSessions))
	mux.HandleFunc("GET /timeline", d.withAuth(d.handleTimeline))
	mux.HandleFunc("GET /approvals", d.withAuth(d.handleApprovals))
	mux.HandleFunc("GET /budgets", d.withAuth(d.handleBudgets))
	mux.HandleFunc("POST /continue-goal", d.withAuth(d.handleContinueGoal))
	mux.HandleFunc("POST /stop", d.withAuth(d.handleStop))
	mux.HandleFunc("POST /schedule", d.withAuth(d.handleSchedule))
	mux.HandleFunc("POST /budget", d.withAuth(d.handleBudget))
	mux.HandleFunc("POST /wait-event", d.withAuth(d.handleWaitEvent))
	mux.HandleFunc("POST /wait-time", d.withAuth(d.handleWaitTime))
	mux.HandleFunc("POST /wait-file", d.withAuth(d.handleWaitFile))
	mux.HandleFunc("POST /webhook", d.handleWebhook)
	mux.HandleFunc("POST /watch", d.withAuth(d.handleWatch))
	mux.HandleFunc("POST /approvals/approve", d.withAuth(d.handleApprove))
	mux.HandleFunc("POST /approvals/deny", d.withAuth(d.handleApprove))
	mux.HandleFunc("POST /asks/answer", d.withAuth(d.handleAnswer))

	d.server = &http.Server{
		Addr:    d.addr,
		Handler: mux,
	}

	ln, err := net.Listen("tcp", d.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", d.addr, err)
	}
	d.logger.Info("daemon started", "addr", d.addr, "sessions", len(d.registry))

	errCh := make(chan error, 1)
	go func() {
		if err := d.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		d.scheduler.Stop()
		d.fileWatcher.Stop()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.server.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (d *Daemon) restoreFileWatches() {
	if d.fileWatcher == nil {
		return
	}
	d.mu.RLock()
	entries := make([]*SessionEntry, 0, len(d.registry))
	for _, entry := range d.registry {
		entries = append(entries, entry)
	}
	d.mu.RUnlock()

	for _, entry := range entries {
		watch := entry.Runtime.FileWatch
		if !watch.Enabled || len(watch.Paths) == 0 {
			continue
		}
		d.fileWatcher.Register(entry.ID, fileWatchConfigForEntry(entry))
	}
}

func fileWatchConfigForEntry(entry *SessionEntry) FileWatchConfig {
	watch := entry.Runtime.FileWatch
	return FileWatchConfig{
		Paths:          resolveFileWatchPaths(entry.Runtime.WorkspaceRoot, watch.Paths),
		IgnorePatterns: append([]string(nil), watch.IgnorePatterns...),
		Debounce:       watch.Debounce,
		Enabled:        watch.Enabled && len(watch.Paths) > 0,
	}
}

func resolveFileWatchPaths(workspaceRoot string, paths []string) []string {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if filepath.IsAbs(path) || workspaceRoot == "" {
			resolved = append(resolved, path)
			continue
		}
		resolved = append(resolved, filepath.Join(workspaceRoot, path))
	}
	return resolved
}

// --- Lock management ---

func (d *Daemon) lockFile() string {
	return LockFile(d.sessionDir)
}

func (d *Daemon) tokenFile() string {
	return TokenFile(d.sessionDir)
}

// LogFile returns the default daemon log path for a session dir.
func LogFile(sessionDir string) string {
	return filepath.Join(sessionDir, ".daemon.log")
}

// TokenFile returns the path where the local daemon API token is stored.
func TokenFile(sessionDir string) string {
	return filepath.Join(sessionDir, ".daemon.token")
}

// GenerateToken returns a fresh local API token for daemon authentication.
func GenerateToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// RotateToken writes a fresh daemon API token to the session dir token file and
// returns the token. Running daemon processes keep their in-memory token until
// restarted.
func RotateToken(sessionDir string) (string, error) {
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	if err := writeTokenFile(TokenFile(sessionDir), token); err != nil {
		return "", err
	}
	return token, nil
}

// LockFile returns the daemon single-instance lock path for a session dir.
func LockFile(sessionDir string) string {
	return filepath.Join(sessionDir, ".daemon.lock")
}

func (d *Daemon) ensureToken() error {
	if strings.TrimSpace(d.token) != "" {
		return nil
	}
	path := d.tokenFile()
	if b, err := os.ReadFile(path); err == nil {
		d.token = strings.TrimSpace(string(b))
		if d.token != "" {
			return nil
		}
	}
	token, err := GenerateToken()
	if err != nil {
		return err
	}
	d.token = token
	return writeTokenFile(path, d.token)
}

func writeTokenFile(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (d *Daemon) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.token == "" {
			next(w, r)
			return
		}
		got := strings.TrimSpace(r.Header.Get("X-Reasonix-Daemon-Token"))
		if got == "" {
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			got = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
		if got != d.token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (d *Daemon) acquireLock() error {
	d.lockPath = d.lockFile()
	if err := os.MkdirAll(filepath.Dir(d.lockPath), 0o755); err != nil {
		return err
	}
	// Try to create exclusively.
	f, err := os.OpenFile(d.lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			// Check if the PID is still alive.
			if d.isLockStale() {
				os.Remove(d.lockPath)
				f, err = os.OpenFile(d.lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
				if err != nil {
					return err
				}
			} else {
				return fmt.Errorf("lockfile exists and process is alive")
			}
		} else {
			return err
		}
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return nil
}

func (d *Daemon) isLockStale() bool {
	data, err := os.ReadFile(d.lockFile())
	if err != nil {
		return true
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return true
	}
	var pid int
	if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
		return true
	}
	// Check if process exists via kill(pid, 0).
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return true
	}
	return false
}

func (d *Daemon) releaseLock() {
	if d.lockPath != "" {
		os.Remove(d.lockPath)
	}
}

// --- Session scanning ---

func (d *Daemon) scanSessions() {
	d.scanDir(d.sessionDir)
	// Also scan project session dirs.
	projectsDir := filepath.Join(filepath.Dir(d.sessionDir), "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessDir := filepath.Join(projectsDir, e.Name(), "sessions")
		d.scanDir(sessDir)
	}
}

func (d *Daemon) scanDir(dir string) {
	pattern := filepath.Join(dir, "*.runtime.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, runtimePath := range matches {
		// Derive session path: strip .runtime.json
		sessionPath := strings.TrimSuffix(runtimePath, ".runtime.json")
		m, ok, err := agent.LoadRuntimeMeta(sessionPath)
		if err != nil || !ok {
			continue
		}
		id := agent.BranchID(sessionPath)
		d.mu.Lock()
		d.registry[id] = &SessionEntry{
			ID:           id,
			Path:         sessionPath,
			Runtime:      m,
			DiscoveredAt: time.Now(),
		}
		d.mu.Unlock()
	}
}

// recoverInterrupted marks any session with an in-flight or controller-owned
// user wait as "interrupted" — these were killed mid-flight and no longer have a
// live controller to receive approvals/answers. Daemon-owned event/time/file waits
// are dormant waits and survive restart. Does NOT auto-resume.
func (d *Daemon) recoverInterrupted() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, entry := range d.registry {
		waitingForController := entry.Runtime.Run.Status == agent.RunStatusWaitingApproval || entry.Runtime.Run.Status == agent.RunStatusWaitingAsk
		if entry.Runtime.Run.Status == agent.RunStatusRunning || waitingForController {
			prev := entry.Runtime.Run.Status
			entry.Runtime.Run.Status = agent.RunStatusInterrupted
			entry.Runtime.Run.LastError = "daemon startup recovery from " + prev
			// Persist the change.
			if err := agent.SaveRuntimeMeta(entry.Path, entry.Runtime); err != nil {
				d.logger.Warn("recover interrupted: save failed", "id", entry.ID, "err", err)
			} else {
				d.appendTimeline(entry.Path, agent.RuntimeTimelineEvent{
					Type:       "run_interrupted",
					Source:     "daemon_startup",
					Step:       "deterministic",
					RunStatus:  entry.Runtime.Run.Status,
					GoalStatus: entry.Runtime.Goal.Status,
					WaitKind:   entry.Runtime.Wait.Kind,
					WaitID:     firstNonEmpty(entry.Runtime.Wait.ApprovalID, entry.Runtime.Wait.AskID, entry.Runtime.Wait.EventID),
					Message:    "recovered from " + prev,
				})
				d.logger.Info("recovered interrupted session", "id", entry.ID)
			}
		}
	}
}

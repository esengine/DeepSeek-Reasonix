package repair

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

const (
	startupStateVersion = 1
	defaultCrashWindow  = 5 * time.Minute
	defaultFailureLimit = 3
	// StartupHealthDelay keeps an updated build probationary long enough to catch
	// failures that happen just after the WebView first paints.
	StartupHealthDelay = 30 * time.Second
)

type StartupState struct {
	SchemaVersion       int    `json:"schemaVersion"`
	Phase               string `json:"phase"`
	Version             string `json:"version,omitempty"`
	InstallProfile      string `json:"installProfile,omitempty"`
	UpdateFromVersion   string `json:"updateFromVersion,omitempty"`
	UpdateToVersion     string `json:"updateToVersion,omitempty"`
	PID                 int    `json:"pid,omitempty"`
	SafeMode            bool   `json:"safeMode,omitempty"`
	ConsecutiveFailures int    `json:"consecutiveFailures,omitempty"`
	WindowStartedAt     string `json:"windowStartedAt,omitempty"`
	StartedAt           string `json:"startedAt,omitempty"`
	UpdatedAt           string `json:"updatedAt,omitempty"`
	Error               string `json:"error,omitempty"`
}

// PreviousRunObservation is a privacy-safe description of a startup record
// whose owner is no longer alive. PID and filesystem paths remain local.
type PreviousRunObservation struct {
	Abnormal       bool
	Phase          string
	Version        string
	InstallProfile string
	UpdateFrom     string
	UpdateTo       string
	UptimeBucket   string
}

type StartupTracker struct {
	path         string
	now          func() time.Time
	processAlive func(int) bool
}

func NewStartupTracker(path string) *StartupTracker {
	if path == "" {
		if root := config.MemoryUserDir(); root != "" {
			path = filepath.Join(root, "repair", "startup-state.json")
		}
	}
	return &StartupTracker{path: path, now: time.Now, processAlive: startupProcessAlive}
}

func (t *StartupTracker) Path() string { return t.path }

func (t *StartupTracker) Read() (StartupState, error) {
	if t.path == "" {
		return StartupState{}, nil
	}
	b, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return StartupState{}, nil
		}
		return StartupState{}, err
	}
	var state StartupState
	if err := json.Unmarshal(b, &state); err != nil {
		return StartupState{}, err
	}
	return state, nil
}

func (t *StartupTracker) SafeModeRecommended() bool {
	state, err := t.Read()
	if err != nil || !incompleteStartupPhase(state.Phase) {
		return false
	}
	if (state.Phase == "starting" || state.Phase == "ready") && state.PID > 0 && t.processAlive(state.PID) {
		return false
	}
	return nextFailureCount(state, t.now(), defaultCrashWindow) >= defaultFailureLimit
}

// ObservePreviousRun reports an unclean prior process without mutating its
// record. A healthy process that later vanished is observable here even though
// it is intentionally not counted toward the startup crash-loop threshold.
func (t *StartupTracker) ObservePreviousRun() PreviousRunObservation {
	state, err := t.Read()
	if err != nil || state.Phase == "" || state.Phase == "clean-exit" {
		return PreviousRunObservation{}
	}
	if runningStartupPhase(state.Phase) && state.PID > 0 && t.processAlive(state.PID) {
		return PreviousRunObservation{}
	}
	return PreviousRunObservation{
		Abnormal:       true,
		Phase:          state.Phase,
		Version:        state.Version,
		InstallProfile: state.InstallProfile,
		UpdateFrom:     state.UpdateFromVersion,
		UpdateTo:       state.UpdateToVersion,
		UptimeBucket:   startupUptimeBucket(state),
	}
}

func startupUptimeBucket(state StartupState) string {
	started, startErr := time.Parse(time.RFC3339Nano, state.StartedAt)
	updated, updateErr := time.Parse(time.RFC3339Nano, state.UpdatedAt)
	if startErr != nil || updateErr != nil || updated.Before(started) {
		return "unknown"
	}
	switch d := updated.Sub(started); {
	case d < 30*time.Second:
		return "s_0_30"
	case d < 2*time.Minute:
		return "m_0_2"
	case d < 10*time.Minute:
		return "m_2_10"
	case d < time.Hour:
		return "m_10_60"
	case d < 6*time.Hour:
		return "h_1_6"
	default:
		return "h_6_plus"
	}
}

// lock serializes the tracker's cross-process read-modify-write cycles. Lock
// failures degrade to lock-free operation: startup must never be wedged by a
// lock problem, and the pre-lock behavior is the acceptable fallback.
func (t *StartupTracker) lock() func() {
	if t.path == "" {
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return func() {}
	}
	unlock, err := lockRepairStateFile(t.path)
	if err != nil {
		return func() {}
	}
	return unlock
}

func (t *StartupTracker) Begin(version string, safeMode bool) (StartupState, error) {
	// Two cold starts can race here before the Wails single-instance lock
	// exists; the file lock makes read-check-write atomic so the loser cannot
	// clobber the winner's record (the loser exits via os.Exit soon after).
	unlock := t.lock()
	defer unlock()
	now := t.now().UTC()
	previous, err := t.Read()
	if err != nil {
		previous = StartupState{}
	}
	// A live owner in any running phase wins. With the Wails single-instance
	// lock, a duplicate launch forwards its args to the running app and exits
	// through os.Exit without OnShutdown, so letting its Begin overwrite the
	// state would turn repeated shortcut clicks during a healthy run into a
	// fake crash loop that triggers recovery/Safe Mode.
	if runningStartupPhase(previous.Phase) && previous.PID > 0 && t.processAlive(previous.PID) {
		return previous, nil
	}
	failures := 0
	windowStart := now
	if incompleteStartupPhase(previous.Phase) {
		failures = nextFailureCount(previous, now, defaultCrashWindow) - 1
		if parsed, parseErr := time.Parse(time.RFC3339Nano, previous.WindowStartedAt); parseErr == nil && now.Sub(parsed) <= defaultCrashWindow {
			windowStart = parsed
		}
	}
	state := StartupState{
		SchemaVersion:       startupStateVersion,
		Phase:               "starting",
		Version:             version,
		PID:                 os.Getpid(),
		SafeMode:            safeMode,
		ConsecutiveFailures: failures + 1,
		WindowStartedAt:     windowStart.Format(time.RFC3339Nano),
		StartedAt:           now.Format(time.RFC3339Nano),
		UpdatedAt:           now.Format(time.RFC3339Nano),
	}
	return state, t.write(state)
}

// MarkLaunchContext attaches bounded, non-secret release metadata to the
// currently-owned startup record for later crash attribution.
func (t *StartupTracker) MarkLaunchContext(installProfile, updateFrom, updateTo string) error {
	unlock := t.lock()
	defer unlock()
	state, err := t.Read()
	if err != nil {
		return err
	}
	if state.PID != os.Getpid() {
		return nil
	}
	state.InstallProfile = installProfile
	state.UpdateFromVersion = updateFrom
	state.UpdateToVersion = updateTo
	state.UpdatedAt = t.now().UTC().Format(time.RFC3339Nano)
	return t.write(state)
}

// Heartbeat refreshes UpdatedAt without changing the startup phase. It makes a
// later hard exit attributable to a coarse uptime bucket.
func (t *StartupTracker) Heartbeat() error {
	unlock := t.lock()
	defer unlock()
	state, err := t.Read()
	if err != nil {
		return err
	}
	if state.PID != os.Getpid() || !runningStartupPhase(state.Phase) {
		return nil
	}
	state.UpdatedAt = t.now().UTC().Format(time.RFC3339Nano)
	return t.write(state)
}

func incompleteStartupPhase(phase string) bool {
	return phase == "starting" || phase == "ready" || phase == "failed"
}

// runningStartupPhase reports whether the recorded owner process may still be
// alive: "failed" and "clean-exit" are terminal, everything else describes a
// process between Begin and its exit.
func runningStartupPhase(phase string) bool {
	return phase == "starting" || phase == "ready" || phase == "healthy"
}

func nextFailureCount(state StartupState, now time.Time, window time.Duration) int {
	started, err := time.Parse(time.RFC3339Nano, state.WindowStartedAt)
	if err != nil || now.Sub(started) > window || now.Before(started) {
		return 1
	}
	return state.ConsecutiveFailures + 1
}

func (t *StartupTracker) MarkReady() error   { return t.transition("ready", "") }
func (t *StartupTracker) MarkHealthy() error { return t.transition("healthy", "") }
func (t *StartupTracker) MarkClean() error   { return t.transition("clean-exit", "") }
func (t *StartupTracker) MarkFailed(err error) error {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return t.transition("failed", message)
}

func (t *StartupTracker) transition(phase, message string) error {
	unlock := t.lock()
	defer unlock()
	state, err := t.Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Ownership: never rewrite a record owned by another live process (a
	// duplicate REASONIX_DEV instance, or a raced cold start about to exit).
	// Records left by dead owners may transition freely — Guard's post-rollback
	// MarkClean legitimately clears a crashed desktop's state.
	if state.PID > 0 && state.PID != os.Getpid() && t.processAlive(state.PID) {
		return nil
	}
	state.SchemaVersion = startupStateVersion
	state.Phase = phase
	state.Error = message
	state.UpdatedAt = t.now().UTC().Format(time.RFC3339Nano)
	if phase == "healthy" || phase == "clean-exit" {
		state.ConsecutiveFailures = 0
		state.WindowStartedAt = ""
	}
	return t.write(state)
}

func (t *StartupTracker) write(state StartupState) error {
	if t.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fileutil.AtomicWriteFile(t.path, b, 0o600)
}

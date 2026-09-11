package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/filelock"
)

// StartupState is the legacy startup-state.json shape written by v1.18-v1.19.
// v1.20 reads it only to attach bounded diagnostics to the next crash report;
// it never writes the record or uses it to select a launch mode.
type StartupState struct {
	SchemaVersion     int    `json:"schemaVersion"`
	Phase             string `json:"phase"`
	Version           string `json:"version,omitempty"`
	InstallProfile    string `json:"installProfile,omitempty"`
	UpdateFromVersion string `json:"updateFromVersion,omitempty"`
	UpdateToVersion   string `json:"updateToVersion,omitempty"`
	PID               int    `json:"pid,omitempty"`
	StartedAt         string `json:"startedAt,omitempty"`
	UpdatedAt         string `json:"updatedAt,omitempty"`
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

// StartupTracker is a one-shot adapter for legacy startup records.
type StartupTracker struct {
	path         string
	processAlive func(int) bool
}

func NewStartupTracker(path string) *StartupTracker {
	if path == "" {
		if root := config.MemoryUserDir(); root != "" {
			path = filepath.Join(root, "repair", "startup-state.json")
		}
	}
	return &StartupTracker{path: path, processAlive: startupProcessAlive}
}

func (t *StartupTracker) Read() (StartupState, error) {
	return readStartupState(t.path)
}

func readStartupState(path string) (StartupState, error) {
	if path == "" {
		return StartupState{}, nil
	}
	b, err := os.ReadFile(path)
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

// ObservePreviousRun atomically claims a completed legacy record and reports
// an unclean prior process at most once. A record owned by a live legacy
// process is never touched, and no observation can alter startup behavior.
func (t *StartupTracker) ObservePreviousRun() PreviousRunObservation {
	if t.path == "" {
		return PreviousRunObservation{}
	}
	release, err := filelock.TryAcquire(t.path + ".claim.lock")
	if err != nil {
		return PreviousRunObservation{}
	}
	defer release()

	state, err := t.Read()
	if err != nil || state.Phase == "" {
		return PreviousRunObservation{}
	}
	if runningStartupPhase(state.Phase) && state.PID > 0 && t.processAlive(state.PID) {
		return PreviousRunObservation{}
	}

	claimed := t.path + ".claimed-" + time.Now().UTC().Format("20060102T150405.000000000")
	if err := os.Rename(t.path, claimed); err != nil {
		// Another launch may already have claimed the same record.
		return PreviousRunObservation{}
	}
	defer os.Remove(claimed)

	// Re-read the claimed bytes so a legacy writer that completed between the
	// initial read and rename cannot be misclassified from a stale snapshot.
	state, err = readStartupState(claimed)
	if err != nil || state.Phase == "" {
		// A legacy owner may still have been replacing the file while this old
		// format was claimed. Preserve ambiguous bytes instead of turning a
		// partial write into evidence loss.
		_ = os.Rename(claimed, t.path)
		return PreviousRunObservation{}
	}
	if state.Phase == "clean-exit" {
		return PreviousRunObservation{}
	}
	if runningStartupPhase(state.Phase) && state.PID > 0 && t.processAlive(state.PID) {
		// This is only possible if the legacy owner changed state during the
		// claim window. Restore its record when the original path is still free.
		_ = os.Rename(claimed, t.path)
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

func runningStartupPhase(phase string) bool {
	return phase == "starting" || phase == "ready" || phase == "healthy"
}

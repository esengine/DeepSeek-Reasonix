package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/config"
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

// StartupTracker is a read-only adapter for legacy startup records.
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

// ObservePreviousRun reports an unclean prior process without mutating its
// record. No observation can alter startup behavior.
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

func runningStartupPhase(phase string) bool {
	return phase == "starting" || phase == "ready" || phase == "healthy"
}

package schedule

import "time"

// SchemaVersion is the manifest layout this build writes. A newer manifest
// loads read-only.
const SchemaVersion = 1

const (
	MaxPromptBytes = 8 << 10
	MaxLabelRunes  = 48
	MaxRuns        = 500
	RunRetention   = 90 * 24 * time.Hour
	MaxManifest    = 8 << 20
)

type TriggerKind string

const (
	TriggerAt    TriggerKind = "at"
	TriggerEvery TriggerKind = "every"
)

type Status string

const (
	StatusActive      Status = "active"
	StatusPaused      Status = "paused"
	StatusExpired     Status = "expired"
	StatusCircuitOpen Status = "circuit-open"
)

type RunState string

const (
	RunClaimed       RunState = "claimed"
	RunRunning       RunState = "running"
	RunSucceeded     RunState = "succeeded"
	RunFailed        RunState = "failed"
	RunBlocked       RunState = "blocked"
	RunBudgetStopped RunState = "budget_stopped"
	RunInterrupted   RunState = "interrupted"
	RunSkipped       RunState = "skipped"
)

// PauseReason is a typed code, never prose.
type PauseReason string

const (
	PauseUser     PauseReason = "user"
	PauseCorrupt  PauseReason = "corrupt"
	PauseBreaker  PauseReason = "breaker"
	PauseLifetime PauseReason = "lifetime_budget"
	PauseFailures PauseReason = "consecutive_failures"
)

const ConfirmedByHuman = "human-frontend"

type Trigger struct {
	Kind     TriggerKind `json:"kind"`
	At       time.Time   `json:"at,omitzero"`
	EverySec int64       `json:"everySec,omitempty"`
	Anchor   time.Time   `json:"anchor,omitzero"`
}

type Target struct {
	Workspace string `json:"workspace"`
}

type Model struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort,omitempty"`
}

// Grant carries no network, MCP, subagent or memory-write field: absence is the deny.
type Grant struct {
	Tools        []string `json:"tools,omitempty"`
	ReadOnlyBash bool     `json:"readOnlyBash,omitempty"`
	ReadRoots    []string `json:"readRoots,omitempty"`
}

type Budget struct {
	PerRunTokens   int64 `json:"perRunTokens"`
	PerRunWallSec  int64 `json:"perRunWallSec"`
	PerRunSteps    int64 `json:"perRunSteps"`
	MaxRunsPerDay  int64 `json:"maxRunsPerDay"`
	LifetimeTokens int64 `json:"lifetimeTokens"`
}

type Confirmation struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Digest string    `json:"digest"`
}

type Schedule struct {
	ID                  string       `json:"id"`
	Label               string       `json:"label,omitempty"`
	Trigger             Trigger      `json:"trigger"`
	Target              Target       `json:"target"`
	Prompt              string       `json:"prompt"`
	Model               Model        `json:"model"`
	Grant               Grant        `json:"grant"`
	Budget              Budget       `json:"budget"`
	Confirmed           Confirmation `json:"confirmed"`
	Status              Status       `json:"status"`
	PausedReason        PauseReason  `json:"pausedReason,omitempty"`
	ConsecutiveFailures int          `json:"consecutiveFailures,omitempty"`
	SpentLifetimeTokens int64        `json:"spentLifetimeTokens,omitempty"`
	CreatedAt           time.Time    `json:"createdAt"`
	ExpiresAt           time.Time    `json:"expiresAt"`
	UpdatedAt           time.Time    `json:"updatedAt"`
}

// Run is one claimed slot. Charged is the reservation (the full per-run cap)
// from claim until settlement, then the settled figure.
type Run struct {
	TriggerID  string    `json:"triggerId"`
	ScheduleID string    `json:"scheduleId"`
	SlotAt     time.Time `json:"slotAt"`
	ClaimedAt  time.Time `json:"claimedAt"`
	EndedAt    time.Time `json:"endedAt,omitzero"`
	State      RunState  `json:"state"`
	SkipReason string    `json:"skipReason,omitempty"`
	Observed   int64     `json:"observedTokens,omitempty"`
	Charged    int64     `json:"chargedTokens"`
	PerRunCap  int64     `json:"perRunCap"`
	// StartHash is the sha256 of the token MarkRunning issued; the token itself
	// travels only over the supervisor's pipe. Started is set by Start.
	StartHash string `json:"startHash,omitempty"`
	Started   bool   `json:"started,omitempty"`
}

type Manifest struct {
	SchemaVersion int         `json:"schemaVersion"`
	Revision      int64       `json:"revision"`
	PausedAll     bool        `json:"pausedAll,omitempty"`
	PausedReason  PauseReason `json:"pausedReason,omitempty"`
	Schedules     []Schedule  `json:"schedules"`
	Runs          []Run       `json:"runs"`
}

func (r Run) inFlight() bool { return r.State == RunClaimed || r.State == RunRunning }

func (m *Manifest) schedule(id string) *Schedule {
	for i := range m.Schedules {
		if m.Schedules[i].ID == id {
			return &m.Schedules[i]
		}
	}
	return nil
}

func (m *Manifest) run(triggerID string) *Run {
	for i := range m.Runs {
		if m.Runs[i].TriggerID == triggerID {
			return &m.Runs[i]
		}
	}
	return nil
}

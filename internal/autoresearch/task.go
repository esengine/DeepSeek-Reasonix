package autoresearch

import "time"

const (
	StatusRunning  = "running"
	StatusBlocked  = "blocked"
	StatusComplete = "complete"
	StatusStopped  = "stopped"
	StatusInvalid  = "invalid"
)

// Task is a read-only view of a historical AutoResearch archive.
type Task struct {
	ID   string
	Root string
	Spec TaskSpec
}

type AllowedOperations struct {
	Write   bool `json:"write"`
	Network bool `json:"network"`
	Publish bool `json:"publish"`
}

type SuccessCriterion struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type TaskSpec struct {
	TaskID            string             `json:"task_id"`
	Goal              string             `json:"goal"`
	Scope             []string           `json:"scope"`
	NonGoals          []string           `json:"non_goals"`
	AllowedOperations AllowedOperations  `json:"allowed_operations"`
	SuccessCriteria   []SuccessCriterion `json:"success_criteria"`
}

type Progress struct {
	Status           string    `json:"status"`
	Iteration        int       `json:"iteration"`
	CurrentDirection string    `json:"current_direction"`
	StaleCount       int       `json:"stale_count"`
	PivotCount       int       `json:"pivot_count"`
	BlockedReason    string    `json:"blocked_reason"`
	UpdatedAt        time.Time `json:"updated_at"`
}

const (
	FindingSourceCommand = "command"
	FindingSourceFile    = "file"
	FindingSourceManual  = "manual"
)

// Finding.Kind is an opaque string. Unknown historical values must round-trip.
type Finding struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Summary   string    `json:"summary"`
	Source    string    `json:"source"`
	Command   string    `json:"command,omitempty"`
	Paths     []string  `json:"paths,omitempty"`
	Accepted  bool      `json:"accepted"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	HeartbeatStartingTurn = "starting_turn"
	HeartbeatTurnDone     = "turn_done"
	HeartbeatWarning      = "warning"
)

type Heartbeat struct {
	Status    string    `json:"status"`
	Iteration int       `json:"iteration"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type DirectionTried struct {
	Fingerprint        string `json:"fingerprint"`
	Summary            string `json:"summary"`
	FirstSeenIteration int    `json:"first_seen_iteration"`
	LastSeenIteration  int    `json:"last_seen_iteration"`
	Count              int    `json:"count"`
}

type CriterionSummary struct {
	ID            string `json:"id"`
	Description   string `json:"description"`
	Required      bool   `json:"required"`
	EvidenceCount int    `json:"evidence_count"`
	Status        string `json:"status"`
}

type Summary struct {
	TaskID             string             `json:"task_id"`
	Goal               string             `json:"goal"`
	Status             string             `json:"status"`
	Iteration          int                `json:"iteration"`
	CurrentDirection   string             `json:"current_direction"`
	StaleCount         int                `json:"stale_count"`
	PivotCount         int                `json:"pivot_count"`
	PivotRequired      bool               `json:"pivot_required"`
	LastHeartbeatAt    time.Time          `json:"last_heartbeat_at,omitempty"`
	FindingCount       int                `json:"finding_count"`
	OpenCriteria       []CriterionSummary `json:"open_criteria"`
	Blocker            string             `json:"blocker"`
	TaskPath           string             `json:"task_path"`
	NextRequiredAction string             `json:"next_required_action"`
}

type ValidationError struct {
	File  string `json:"file"`
	Field string `json:"field"`
	Error string `json:"error"`
}

type ValidationReport struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
}

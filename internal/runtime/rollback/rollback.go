package rollback

import (
	"os"

	"reasonix/internal/runtime/snapshot"
)

type Signals struct {
	RecentExecutions     int      `json:"recent_executions"`
	RecentFailures       int      `json:"recent_failures"`
	BudgetViolations     int      `json:"budget_violations,omitempty"`
	SandboxViolations    int      `json:"sandbox_violations,omitempty"`
	CanaryDivergences    int      `json:"canary_divergences,omitempty"`
	OscillationIndex     float64  `json:"oscillation_index"`
	CorruptedMemoryNodes int      `json:"corrupted_memory_nodes"`
	ActiveStrategies     int      `json:"active_strategies"`
	RejectedStrategies   int      `json:"rejected_strategies"`
	Reasons              []string `json:"reasons,omitempty"`
}

type Decision struct {
	ShouldRollback bool           `json:"should_rollback"`
	SnapshotID     string         `json:"snapshot_id,omitempty"`
	Severity       string         `json:"severity,omitempty"`
	FailureClasses []FailureClass `json:"failure_classes,omitempty"`
	Reasons        []string       `json:"reasons,omitempty"`
}

type FailureClass string

const (
	FailureTransient          FailureClass = "transient"
	FailureBudgetViolation    FailureClass = "budget_violation"
	FailureSandboxViolation   FailureClass = "sandbox_violation"
	FailureControlOscillation FailureClass = "control_oscillation"
	FailureMemoryCorruption   FailureClass = "memory_corruption"
	FailureStrategyCollapse   FailureClass = "strategy_collapse"
	FailureCanaryDivergence   FailureClass = "canary_divergence"
)

func Evaluate(signals Signals) Decision {
	decision := Decision{}
	if signals.RecentExecutions > 0 && signals.RecentFailures*2 >= signals.RecentExecutions && signals.RecentFailures >= 3 {
		decision.ShouldRollback = true
		decision.FailureClasses = appendFailureClass(decision.FailureClasses, FailureTransient)
		decision.Reasons = append(decision.Reasons, "execution failure spike")
	}
	if signals.BudgetViolations >= 2 {
		decision.ShouldRollback = true
		decision.FailureClasses = appendFailureClass(decision.FailureClasses, FailureBudgetViolation)
		decision.Reasons = append(decision.Reasons, "repeated budget violation")
	}
	if signals.SandboxViolations > 0 {
		decision.ShouldRollback = true
		decision.FailureClasses = appendFailureClass(decision.FailureClasses, FailureSandboxViolation)
		decision.Reasons = append(decision.Reasons, "sandbox violation")
	}
	if signals.OscillationIndex >= 0.8 {
		decision.ShouldRollback = true
		decision.FailureClasses = appendFailureClass(decision.FailureClasses, FailureControlOscillation)
		decision.Reasons = append(decision.Reasons, "control oscillation")
	}
	if signals.CorruptedMemoryNodes >= 3 {
		decision.ShouldRollback = true
		decision.FailureClasses = appendFailureClass(decision.FailureClasses, FailureMemoryCorruption)
		decision.Reasons = append(decision.Reasons, "memory corruption")
	}
	if signals.ActiveStrategies == 0 && signals.RejectedStrategies > 0 {
		decision.ShouldRollback = true
		decision.FailureClasses = appendFailureClass(decision.FailureClasses, FailureStrategyCollapse)
		decision.Reasons = append(decision.Reasons, "strategy collapse")
	}
	if signals.CanaryDivergences >= 2 {
		decision.ShouldRollback = true
		decision.FailureClasses = appendFailureClass(decision.FailureClasses, FailureCanaryDivergence)
		decision.Reasons = append(decision.Reasons, "repeated canary divergence")
	}
	decision.Severity = severityForClasses(decision.FailureClasses)
	decision.Reasons = append(decision.Reasons, signals.Reasons...)
	return decision
}

func EvaluateWithSnapshot(root string, signals Signals) Decision {
	decision := Evaluate(signals)
	if !decision.ShouldRollback {
		return decision
	}
	snap, err := snapshot.LatestStable(root)
	if err != nil {
		if os.IsNotExist(err) {
			decision.Reasons = append(decision.Reasons, "no stable snapshot available")
			return decision
		}
		decision.Reasons = append(decision.Reasons, "snapshot lookup failed: "+err.Error())
		return decision
	}
	decision.SnapshotID = snap.ID
	return decision
}

func appendFailureClass(existing []FailureClass, next FailureClass) []FailureClass {
	for _, class := range existing {
		if class == next {
			return existing
		}
	}
	return append(existing, next)
}

func severityForClasses(classes []FailureClass) string {
	if len(classes) == 0 {
		return "none"
	}
	for _, class := range classes {
		switch class {
		case FailureMemoryCorruption, FailureControlOscillation, FailureSandboxViolation:
			return "high"
		}
	}
	return "medium"
}

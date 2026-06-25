package canary

import (
	"hash/fnv"
	"strings"
)

type Mode string

const (
	SafeMode           Mode = "SAFE_MODE"
	CanaryMode         Mode = "CANARY_MODE"
	FullProductionMode Mode = "FULL_PRODUCTION_MODE"
)

type Policy struct {
	Mode           Mode    `json:"mode"`
	TrafficPercent int     `json:"traffic_percent"`
	StabilityScore float64 `json:"stability_score,omitempty"`
	MinStableRuns  int     `json:"min_stable_runs"`
}

type Evaluation struct {
	Mode    Mode     `json:"mode"`
	Enabled bool     `json:"enabled"`
	Reasons []string `json:"reasons,omitempty"`
}

type BehaviorSample struct {
	Decision        string   `json:"decision,omitempty"`
	Strategy        string   `json:"strategy,omitempty"`
	Outcome         string   `json:"outcome,omitempty"`
	Steps           []string `json:"steps,omitempty"`
	DecisionReasons []string `json:"decision_reasons,omitempty"`
}

type BehaviorDiff struct {
	Diverged    bool              `json:"diverged"`
	Reasons     []string          `json:"reasons,omitempty"`
	Attribution CausalAttribution `json:"attribution,omitempty"`
}

type CausalAttribution struct {
	PrimaryCause string         `json:"primary_cause,omitempty"`
	Factors      []CausalFactor `json:"factors,omitempty"`
}

type CausalFactor struct {
	Layer    string `json:"layer"`
	Cause    string `json:"cause"`
	Evidence string `json:"evidence"`
	Severity string `json:"severity"`
}

// DefaultPolicy is the policy used when no canary state has been initialised.
// A local single-user runtime has no "traffic" to split, so the default is full
// production (always enabled) rather than a 10% canary. This matches what
// normalizeProductionState seeds, keeping a single source of truth and avoiding
// the previous mismatch where DefaultPolicy claimed 10% while the runtime ran at
// 100%.
func DefaultPolicy() Policy {
	return Policy{
		Mode:           FullProductionMode,
		TrafficPercent: 100,
		MinStableRuns:  5,
	}
}

func Evaluate(policy Policy, key string) Evaluation {
	policy = Normalize(policy)
	switch policy.Mode {
	case SafeMode:
		return Evaluation{Mode: SafeMode, Enabled: false, Reasons: []string{"safe mode blocks production hardening execution"}}
	case FullProductionMode:
		return Evaluation{Mode: FullProductionMode, Enabled: true, Reasons: []string{"full production mode enabled"}}
	default:
		enabled := bucket(key) < policy.TrafficPercent
		reason := "canary traffic excluded"
		if enabled {
			reason = "canary traffic included"
		}
		return Evaluation{Mode: CanaryMode, Enabled: enabled, Reasons: []string{reason}}
	}
}

func Promote(policy Policy, stableRuns int, stabilityScore float64) Policy {
	policy = Normalize(policy)
	policy.StabilityScore = stabilityScore
	if policy.Mode == SafeMode {
		policy.Mode = CanaryMode
		policy.TrafficPercent = 5
		return policy
	}
	if policy.Mode != CanaryMode {
		return policy
	}
	if stableRuns < policy.MinStableRuns || stabilityScore < 0.85 {
		return policy
	}
	switch {
	case policy.TrafficPercent < 10:
		policy.TrafficPercent = 10
	case policy.TrafficPercent < 25:
		policy.TrafficPercent = 25
	case policy.TrafficPercent < 50:
		policy.TrafficPercent = 50
	case policy.TrafficPercent < 100:
		policy.TrafficPercent = 100
	default:
		policy.Mode = FullProductionMode
	}
	return policy
}

func Normalize(policy Policy) Policy {
	if policy.Mode == "" {
		policy.Mode = CanaryMode
	}
	if policy.TrafficPercent <= 0 {
		policy.TrafficPercent = 10
	}
	if policy.TrafficPercent > 100 {
		policy.TrafficPercent = 100
	}
	if policy.Mode == SafeMode {
		policy.TrafficPercent = 0
	}
	if policy.Mode == FullProductionMode {
		policy.TrafficPercent = 100
	}
	if policy.MinStableRuns <= 0 {
		policy.MinStableRuns = 5
	}
	return policy
}

func CompareBehavior(canary, baseline BehaviorSample) BehaviorDiff {
	if baseline.Decision == "" && baseline.Strategy == "" && baseline.Outcome == "" && len(baseline.Steps) == 0 {
		return BehaviorDiff{
			Reasons: []string{"baseline unavailable"},
			Attribution: CausalAttribution{
				PrimaryCause: "baseline_unavailable",
				Factors: []CausalFactor{{
					Layer:    "canary",
					Cause:    "missing_baseline",
					Evidence: "no baseline behavior sample is available for comparison",
					Severity: "low",
				}},
			},
		}
	}
	diff := BehaviorDiff{}
	if strings.TrimSpace(canary.Decision) != strings.TrimSpace(baseline.Decision) {
		diff.Diverged = true
		diff.Reasons = append(diff.Reasons, "decision diverged")
		diff.Attribution.Factors = append(diff.Attribution.Factors, CausalFactor{
			Layer:    "control",
			Cause:    "decision_changed",
			Evidence: compareEvidence(canary.Decision, baseline.Decision),
			Severity: "high",
		})
	}
	if strings.TrimSpace(canary.Strategy) != strings.TrimSpace(baseline.Strategy) {
		diff.Diverged = true
		diff.Reasons = append(diff.Reasons, "strategy diverged")
		diff.Attribution.Factors = append(diff.Attribution.Factors, CausalFactor{
			Layer:    "strategy",
			Cause:    "strategy_changed",
			Evidence: compareEvidence(canary.Strategy, baseline.Strategy),
			Severity: "medium",
		})
	}
	if strings.TrimSpace(canary.Outcome) != strings.TrimSpace(baseline.Outcome) {
		diff.Diverged = true
		diff.Reasons = append(diff.Reasons, "outcome diverged")
		diff.Attribution.Factors = append(diff.Attribution.Factors, CausalFactor{
			Layer:    "execution",
			Cause:    "outcome_changed",
			Evidence: compareEvidence(canary.Outcome, baseline.Outcome),
			Severity: "high",
		})
	}
	if strings.Join(canary.Steps, "\x00") != strings.Join(baseline.Steps, "\x00") {
		diff.Diverged = true
		diff.Reasons = append(diff.Reasons, "execution steps diverged")
		diff.Attribution.Factors = append(diff.Attribution.Factors, CausalFactor{
			Layer:    "execution_plan",
			Cause:    "steps_changed",
			Evidence: compareEvidence(strings.Join(canary.Steps, ","), strings.Join(baseline.Steps, ",")),
			Severity: "medium",
		})
	}
	if len(diff.Reasons) == 0 {
		diff.Reasons = []string{"behavior matches baseline"}
		diff.Attribution.PrimaryCause = "none"
	} else {
		diff.Attribution.PrimaryCause = primaryCause(diff.Attribution.Factors)
		for _, reason := range canary.DecisionReasons {
			reason = strings.TrimSpace(reason)
			if reason == "" {
				continue
			}
			diff.Attribution.Factors = append(diff.Attribution.Factors, CausalFactor{
				Layer:    "runtime",
				Cause:    "decision_reason",
				Evidence: reason,
				Severity: "low",
			})
			if len(diff.Attribution.Factors) >= 6 {
				break
			}
		}
	}
	return diff
}

func primaryCause(factors []CausalFactor) string {
	for _, severity := range []string{"high", "medium", "low"} {
		for _, factor := range factors {
			if factor.Severity == severity && factor.Cause != "" {
				return factor.Cause
			}
		}
	}
	return "unknown"
}

func compareEvidence(current, baseline string) string {
	current = strings.TrimSpace(current)
	baseline = strings.TrimSpace(baseline)
	if current == "" {
		current = "<empty>"
	}
	if baseline == "" {
		baseline = "<empty>"
	}
	return "current=" + current + " baseline=" + baseline
}

func bucket(key string) int {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "default"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % 100)
}

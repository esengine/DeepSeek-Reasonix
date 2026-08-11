// Package guardian implements an LLM-driven safety reviewer that evaluates tool
// calls before they execute. It replaces the interactive human approval step for
// "ask" permission decisions: instead of prompting the user, a dedicated sub-agent
// with read-only tools inspects the call against a safety policy and returns
// allow/deny with a structured risk assessment.
package guardian

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed guardian_policy.md
var EmbeddedPolicy []byte

// Assessment is the structured output the guardian model must produce.
type Assessment struct {
	RiskLevel         string `json:"risk_level"`
	UserAuthorization string `json:"user_authorization"`
	Outcome           string `json:"outcome"`
	Rationale         string `json:"rationale"`
}

// ParseOutcome maps a decision string.
func ParseOutcome(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "allow":
		return "allow"
	case "deny":
		return "deny"
	default:
		return "deny"
	}
}

// ParseAssessment extracts a GuardianAssessment from the guardian model's raw
// output text. Accepts a JSON object directly or a JSON object wrapped in prose
// (first { to last }). Non-JSON output returns an error (triggering fail-closed).
func ParseAssessment(text string) (Assessment, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Assessment{}, fmt.Errorf("guardian review produced empty output")
	}
	var a Assessment
	if err := json.Unmarshal([]byte(text), &a); err == nil {
		return normalizeAssessment(a)
	}
	// Try to extract the first JSON object from prose wrapping.
	if start := strings.IndexByte(text, '{'); start >= 0 {
		if end := strings.LastIndexByte(text, '}'); end > start {
			slice := text[start : end+1]
			if err := json.Unmarshal([]byte(slice), &a); err == nil {
				return normalizeAssessment(a)
			}
		}
	}
	return Assessment{}, fmt.Errorf("guardian output is not valid JSON: %q", firstRunesStr(text, 120))
}

func normalizeAssessment(a Assessment) (Assessment, error) {
	outcome := ParseOutcome(a.Outcome)

	if a.RiskLevel == "" {
		if outcome == "allow" {
			a.RiskLevel = "low"
		} else {
			a.RiskLevel = "high"
		}
	}
	riskLevel, err := normalizePolicyEnum("risk_level", a.RiskLevel, validRiskLevels)
	if err != nil {
		return Assessment{}, err
	}
	a.RiskLevel = riskLevel

	if a.UserAuthorization == "" {
		a.UserAuthorization = "unknown"
	}
	userAuthorization, err := normalizePolicyEnum("user_authorization", a.UserAuthorization, validUserAuthorizations)
	if err != nil {
		return Assessment{}, err
	}
	a.UserAuthorization = userAuthorization

	if strings.TrimSpace(a.Rationale) == "" {
		if outcome == "allow" {
			a.Rationale = "guardian review returned a low-risk allow decision"
		} else {
			a.Rationale = "guardian review returned a deny decision without a specific rationale"
		}
	}
	a.Outcome = outcome
	return enforcePolicyRules(a)
}

var validRiskLevels = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

var validUserAuthorizations = map[string]bool{
	"unknown": true,
	"low":     true,
	"medium":  true,
	"high":    true,
}

func normalizePolicyEnum(field, value string, valid map[string]bool) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if !valid[normalized] {
		return "", fmt.Errorf("guardian output has unknown %s %q", field, value)
	}
	return normalized, nil
}

// enforcePolicyRules applies hard safety constraints that the guardian model
// prompt cannot override. These rules are the final backstop: even if the model
// produces allow for a critical-risk operation, the code forces deny.
func enforcePolicyRules(a Assessment) (Assessment, error) {
	// Rule 1: critical risk is always deny.
	if a.RiskLevel == "critical" && a.Outcome != "deny" {
		a.Outcome = "deny"
		if a.Rationale == "guardian review returned a low-risk allow decision" {
			a.Rationale = "guardian review returned a critical-risk action with allow outcome — forced deny"
		}
	}
	// Rule 2: high risk must have at least medium user authorization.
	if a.RiskLevel == "high" && a.Outcome == "allow" {
		if a.UserAuthorization != "medium" && a.UserAuthorization != "high" {
			a.Outcome = "deny"
			if a.Rationale == "guardian review returned a low-risk allow decision" {
				a.Rationale = "guardian review allowed a high-risk action without sufficient user authorization — forced deny"
			}
		}
	}
	// Re-normalize: downstream code checks a.Outcome directly.
	return a, nil
}

// DenyReason builds the model-facing reason string when the guardian denies a call.
func DenyReason(a Assessment) string {
	return fmt.Sprintf("guardian denied: risk=%s, authorization=%s. %s",
		a.RiskLevel, a.UserAuthorization, a.Rationale)
}

// CircuitBreakerReason builds the message injected when the circuit breaker trips.
func CircuitBreakerReason(consecutive, recent int) string {
	return fmt.Sprintf("Guardian auto-review has denied too many requests this turn (%d consecutive, %d in recent window). Stop the current approach, report the situation to the user, and request explicit instructions before continuing.",
		consecutive, recent)
}

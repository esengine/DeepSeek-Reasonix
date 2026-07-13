package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/capability"
	"reasonix/internal/evidence"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// SeedCapabilityRoute installs the turn's route decision into the capability ledger.
func (a *Agent) SeedCapabilityRoute(decision capability.RouteDecision) {
	if a == nil {
		return
	}
	if a.capabilityLedger == nil {
		a.capabilityLedger = capability.NewLedger()
	}
	a.capabilityLedger.Reset()
	a.capabilityLedger.SeedCandidates(decision)
	a.capabilityPreferReminded = false
	a.capabilityRequireMissSeen = false
	a.capabilityPreferMissSeen = false
}

// CapabilityLedger returns the turn-scoped capability ledger (may be nil).
func (a *Agent) CapabilityLedger() *capability.Ledger {
	if a == nil {
		return nil
	}
	return a.capabilityLedger
}

// CapabilityAudit returns the non-persisted capability metrics sink (may be nil).
func (a *Agent) CapabilityAudit() *capability.Audit {
	if a == nil {
		return nil
	}
	return a.capabilityAudit
}

func (a *Agent) noteCapabilityInvocation(toolName string, args json.RawMessage, callErr error) {
	if a == nil || a.capabilityLedger == nil {
		return
	}
	// Successful/failed proxied MCP calls execute the resolved target
	// directly, so this is the single audit point for action=call (inspect,
	// decline, and resolve-time unavailability are counted in ResolveCall,
	// which returns before this runs).
	if toolName == "use_capability" && a.capabilityAudit != nil {
		var p struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(args, &p)
		if strings.EqualFold(strings.TrimSpace(p.Action), "call") {
			a.capabilityAudit.RecordMCPProxy(false, true, callErr != nil)
		}
	}
	id := capabilityIDFromToolCall(toolName, args)
	if id == "" {
		return
	}
	if callErr != nil {
		a.capabilityLedger.MarkFailed(id, callErr.Error())
		if a.capabilityAudit != nil && strings.HasPrefix(id, "skill:") {
			a.capabilityAudit.RecordSkill(true, errors.Is(callErr, skill.ErrInvocationUnavailable))
		}
		return
	}
	a.capabilityLedger.MarkSucceeded(id)
	if a.capabilityAudit != nil && strings.HasPrefix(id, "skill:") {
		a.capabilityAudit.RecordSkill(false, false)
	}
}

func capabilityIDFromToolCall(toolName string, args json.RawMessage) string {
	switch toolName {
	case "run_skill", "read_skill", "read_only_skill", "explore", "research", "review", "security_review":
		var p struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(args, &p)
		name := strings.TrimSpace(p.Name)
		if name == "" {
			// Dedicated wrappers use the tool name as the skill name.
			switch toolName {
			case "explore", "research", "review", "security_review":
				name = toolName
			}
		}
		if name == "security_review" {
			name = "security-review"
		}
		if name == "" {
			return ""
		}
		return "skill:" + name
	case "use_capability":
		var p struct {
			CapabilityID string `json:"capability_id"`
		}
		_ = json.Unmarshal(args, &p)
		return strings.TrimSpace(p.CapabilityID)
	default:
		if server, raw, ok := splitMCP(toolName); ok {
			return "mcp-tool:" + server + "/" + raw
		}
	}
	return ""
}

func splitMCP(name string) (server, raw string, ok bool) {
	const prefix = "mcp__"
	if !strings.HasPrefix(name, prefix) {
		return "", "", false
	}
	rest := name[len(prefix):]
	parts := strings.SplitN(rest, "__", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// capabilityGateFailure is checked during final readiness for Delivery.
func (a *Agent) capabilityGateFailure() string {
	if a == nil || !a.deliveryProfile || a.capabilityLedger == nil {
		return ""
	}
	gate := a.capabilityLedger.CheckFinalGate()
	if gate.Reason == "" {
		// A clean gate after an earlier miss this turn is a recovery — the
		// model was nudged and then actually invoked the capability.
		if a.capabilityRequireMissSeen || a.capabilityPreferMissSeen {
			if a.capabilityAudit != nil {
				a.capabilityAudit.RecordGateRecovery(a.capabilityRequireMissSeen, a.capabilityPreferMissSeen)
			}
			a.capabilityRequireMissSeen = false
			a.capabilityPreferMissSeen = false
		}
		return ""
	}
	if gate.PreferRemind && !a.capabilityPreferReminded {
		for _, id := range gate.PreferIDs {
			a.capabilityLedger.MarkReminded(id)
		}
		a.capabilityPreferReminded = true
		a.capabilityPreferMissSeen = true
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordGate(false, true, false)
		}
		return gate.Reason
	}
	if gate.UnavailableOK {
		// Host-proven unavailable: allow final answer that reports the blocker,
		// but do not treat it as successful delivery. The reason is returned so
		// the model is nudged once; if it still claims success, missing mutation
		// / sign-off gates still apply. For pure capability blockers with no
		// mutation, we surface the reason and allow the loop-guard path.
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordGate(true, false, false)
		}
		// Do not hard-block forever: once reported, allow final if no mutation pending.
		if _, ok := a.evidence.LatestSuccessfulMutationIndex(); !ok {
			return ""
		}
		return gate.Reason
	}
	if len(gate.RequireIDs) > 0 {
		a.capabilityRequireMissSeen = true
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordGate(true, false, false)
		}
		return gate.Reason
	}
	if len(gate.PreferIDs) > 0 {
		a.capabilityPreferMissSeen = true
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordGate(false, true, false)
		}
		return gate.Reason
	}
	return gate.Reason
}

// deliveryReviewGateFailure enforces risk-adaptive structured review after the
// latest mutation. Low keeps the existing light review; Medium requires review;
// High requires review + security_review with structured reports.
func (a *Agent) deliveryReviewGateFailure() string {
	if a == nil || !a.deliveryProfile || a.evidence == nil {
		return ""
	}
	mutation, ok := a.evidence.LatestSuccessfulMutationIndex()
	if !ok {
		return ""
	}
	risk := a.evidence.MutationRiskAfter(mutation)
	paths := productionPaths(a.evidence.PathsSince(mutation))
	hasReviewTool := a.tools != nil && (toolPresent(a.tools, "review") || toolPresent(a.tools, "run_skill"))
	hasSecurityTool := a.tools != nil && (toolPresent(a.tools, "security_review") || toolPresent(a.tools, "run_skill"))
	switch risk {
	case evidence.RiskLow:
		// Existing light review (read/diff) already checked elsewhere.
		return ""
	case evidence.RiskMedium:
		if !hasReviewTool {
			// Test/minimal registries without review keep the light review gate.
			return ""
		}
		ok, blocking, report := a.evidence.HasStructuredReviewAfter(evidence.ReviewKindReview, mutation, paths)
		if blocking {
			if a.capabilityAudit != nil {
				a.capabilityAudit.RecordReviewBlock(false)
			}
			return "structured review reported blocking findings; fix them and re-run review"
		}
		if !ok {
			return "medium-risk changes require a successful review after the latest mutation (run the review skill; its subagent submits review_report)" + reviewCoverageHint(paths)
		}
		if report != nil {
			a.pendingReviewWarnings = append(a.pendingReviewWarnings, report.WarningSummaries()...)
		}
	case evidence.RiskHigh:
		if !hasReviewTool && !hasSecurityTool {
			return "high-risk changes require review and security_review tools after the latest mutation"
		}
		okR, blockR, repR := a.evidence.HasStructuredReviewAfter(evidence.ReviewKindReview, mutation, paths)
		if blockR {
			if a.capabilityAudit != nil {
				a.capabilityAudit.RecordReviewBlock(false)
			}
			return "structured review reported blocking findings; fix them and re-run review"
		}
		if !okR {
			return "high-risk changes require review with review_report after the latest mutation" + reviewCoverageHint(paths)
		}
		okS, blockS, repS := a.evidence.HasStructuredReviewAfter(evidence.ReviewKindSecurity, mutation, paths)
		if blockS {
			if a.capabilityAudit != nil {
				a.capabilityAudit.RecordReviewBlock(true)
			}
			return "security_review reported blocking findings; fix them and re-run security_review"
		}
		if !okS {
			return "high-risk changes require security_review with review_report after the latest mutation" + reviewCoverageHint(paths)
		}
		if repR != nil {
			a.pendingReviewWarnings = append(a.pendingReviewWarnings, repR.WarningSummaries()...)
		}
		if repS != nil {
			a.pendingReviewWarnings = append(a.pendingReviewWarnings, repS.WarningSummaries()...)
		}
	}
	return ""
}

func reviewCoverageHint(paths []string) string {
	if len(paths) == 0 {
		return "; the mutation did not report file paths, so first inspect `git status --short` and `git diff` to identify the changed files, then submit reviewed_paths for the files inspected"
	}
	return " covering: " + strings.Join(paths, ", ")
}

func toolPresent(reg *tool.Registry, name string) bool {
	if reg == nil {
		return false
	}
	_, ok := reg.Get(name)
	return ok
}

func productionPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		// Skip pure test/doc paths for coverage requirements when mixed sets exist.
		lower := strings.ToLower(p)
		if strings.HasSuffix(lower, "_test.go") || strings.Contains(lower, "/docs/") {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return paths
	}
	return out
}

// ReviewWarnings returns warn-level review findings collected this turn.
func (a *Agent) ReviewWarnings() []string {
	if a == nil {
		return nil
	}
	return append([]string(nil), a.pendingReviewWarnings...)
}

// FormatReviewWarningsForSummary builds a short appendix for the final answer.
func FormatReviewWarningsForSummary(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	return "Review warnings:\n- " + strings.Join(warnings, "\n- ")
}

// ensure string used
var _ = fmt.Sprintf

package agent

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// capabilityGateState is one user turn's gate memory, scoped to the same turn
// as the ledger it reads: whether the prefer reminder has already been spent,
// and which kind of miss was reported, so a later clean gate is audited as a
// recovery instead of a first pass. Zeroing the struct is the turn reset.
type capabilityGateState struct {
	preferReminded  bool
	requireMissSeen bool
	preferMissSeen  bool
}

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
	a.capabilityGate = capabilityGateState{}
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
		if a.capabilityGate.requireMissSeen || a.capabilityGate.preferMissSeen {
			if a.capabilityAudit != nil {
				a.capabilityAudit.RecordGateRecovery(a.capabilityGate.requireMissSeen, a.capabilityGate.preferMissSeen)
			}
			a.capabilityGate.requireMissSeen = false
			a.capabilityGate.preferMissSeen = false
		}
		return ""
	}
	if gate.PreferRemind && !a.capabilityGate.preferReminded {
		for _, id := range gate.PreferIDs {
			a.capabilityLedger.MarkReminded(id)
		}
		a.capabilityGate.preferReminded = true
		a.capabilityGate.preferMissSeen = true
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
		if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); !ok {
			return ""
		}
		return gate.Reason
	}
	if len(gate.RequireIDs) > 0 {
		a.capabilityGate.requireMissSeen = true
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordGate(true, false, false)
		}
		return gate.Reason
	}
	if len(gate.PreferIDs) > 0 {
		a.capabilityGate.preferMissSeen = true
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordGate(false, true, false)
		}
		return gate.Reason
	}
	return gate.Reason
}

// reviewGateFailure says what the turn still owes for structured review. Two
// questions the gate keeps apart: a blocking verdict the host actually
// received is honored at every role setting, while whether a review is owed at
// all is the delivery contract's to demand.
func (a *Agent) reviewGateFailure() string {
	if a == nil || a.task.ledger == nil {
		return ""
	}
	if a.subagentDepth > 0 {
		// Structured review is the parent's contract. A child's mutation
		// receipts merge into the parent ledger (mergeChildEvidence), so the
		// parent cannot final-answer without review coverage of those writes.
		// Demanding review_report inside a depth-capped sub-agent — which may
		// not even have the review tools — wedges the child against a gate it
		// cannot satisfy. The light post-mutation review (read the touched
		// file or run git diff/status) still applies via finalReadinessCheck.
		return ""
	}
	baseline, ok := a.task.ledger.UnreviewedMutationBaseline()
	if !ok {
		return ""
	}
	// What the change set is, and how fresh a review of it must be, are read
	// from different indices on purpose: see UnreviewedMutationBaseline.
	freshness, ok := a.task.ledger.LatestProvenMutationIndex()
	if !ok || freshness < baseline {
		freshness = baseline
	}
	paths := productionPaths(a.task.ledger.PathsSince(baseline))
	// Read whatever a review said, asked for or not.
	for _, report := range a.task.ledger.ReviewReportsAfter(freshness) {
		a.collectReviewWarnings(&report)
	}
	if msg := a.blockingReviewFailure(freshness); msg != "" {
		return msg
	}
	if !a.deliveryProfile {
		return ""
	}
	return a.reviewObligationFailure(baseline, freshness, paths)
}

// blockingReviewFailure honors a review that ran and said no. Coverage excuses
// a review that is missing, never one that looked and refused. A fix is a
// mutation, which moves freshness past the refusal, so this blocks until the
// turn changes something rather than forever.
func (a *Agent) blockingReviewFailure(freshness int) string {
	report, ok := a.task.ledger.BlockingReviewAfter(freshness)
	if !ok {
		return ""
	}
	security := report.Kind == evidence.ReviewKindSecurity
	if a.capabilityAudit != nil {
		a.capabilityAudit.RecordReviewBlock(security)
	}
	if security {
		return "security_review reported blocking findings; fix them and re-run security_review"
	}
	return "structured review reported blocking findings; fix them and re-run review"
}

// reviewObligationFailure is the risk-adaptive demand: how much review the
// change set owes, read off the mutation receipts rather than the request.
// Only High buys one, because independence is all a structured review adds:
// medium once took self-inspection instead, where the reader is the author.
func (a *Agent) reviewObligationFailure(baseline, freshness int, paths []string) string {
	a.emitTurnPhase(event.TurnPhaseReviewing)
	risk := a.task.ledger.MutationRiskAfter(baseline, a.projectSensitivePaths)
	hasReviewTool := a.svc.tools != nil && (toolPresent(a.svc.tools, "review") || toolPresent(a.svc.tools, "run_skill") || toolPresent(a.svc.tools, "use_capability"))
	hasSecurityTool := a.svc.tools != nil && (toolPresent(a.svc.tools, "security_review") || toolPresent(a.svc.tools, "run_skill") || toolPresent(a.svc.tools, "use_capability"))
	switch risk {
	case evidence.RiskLow, evidence.RiskMedium:
		// Verification and full self-inspection carry these, and both are
		// demanded by finalReadinessCheck whatever the risk.
		return ""
	case evidence.RiskHigh:
		if !hasReviewTool && !hasSecurityTool {
			return "high-risk changes require review and security_review tools after the latest mutation"
		}
		okR, blockR, _ := a.task.ledger.HasStructuredReviewAfter(evidence.ReviewKindReview, freshness, paths)
		if !okR || blockR {
			return "high-risk changes require review with review_report after the latest mutation" +
				a.reviewProofHint(evidence.ReviewKindReview) + reviewCoverageHint(paths)
		}
		okS, blockS, _ := a.task.ledger.HasStructuredReviewAfter(evidence.ReviewKindSecurity, freshness, paths)
		if !okS || blockS {
			return "high-risk changes require security_review with review_report after the latest mutation" +
				a.reviewProofHint(evidence.ReviewKindSecurity) + reviewCoverageHint(paths)
		}
	}
	return ""
}

func (a *Agent) collectReviewWarnings(report *evidence.ReviewReport) {
	if report == nil {
		return
	}
	a.turn.reviewWarnings = append(a.turn.reviewWarnings, report.WarningSummaries()...)
}

func reviewCoverageHint(paths []string) string {
	if len(paths) == 0 {
		// Name what the host needs, not the command to get it. Naming one sent
		// the model to `git diff` in workspaces that are not repositories, where
		// the instruction it was given had no way to succeed.
		return "; the change reported no file paths, so establish which files it touched — by whatever this workspace supports — and submit those in reviewed_paths"
	}
	// Slash-canonical, as review_report's own rejection renders them: the model
	// reads this list and echoes it back into reviewed_paths.
	slash := make([]string, 0, len(paths))
	for _, p := range paths {
		slash = append(slash, filepath.ToSlash(p))
	}
	return " covering: " + strings.Join(slash, ", ")
}

func toolPresent(reg *tool.Registry, name string) bool {
	if reg == nil {
		return false
	}
	_, ok := reg.Get(name)
	return ok
}

// productionPaths is what review coverage must name. Supporting material is
// dropped only when production code is also in the set; a repository store is
// dropped always, since asking for a diff of `.git` is asking for the one thing
// that says nothing about the work.
func productionPaths(paths []string) []string {
	var production, supporting []string
	for _, p := range paths {
		switch evidence.ClassifyPath(p) {
		case evidence.PathVCSStore:
		case evidence.PathProduction:
			production = append(production, p)
		default:
			supporting = append(supporting, p)
		}
	}
	if len(production) > 0 {
		return production
	}
	return supporting
}

// ReviewWarnings returns warn-level review findings collected this turn.
func (a *Agent) ReviewWarnings() []string {
	if a == nil {
		return nil
	}
	return append([]string(nil), a.turn.reviewWarnings...)
}

// reportReviewWarnings surfaces what the gate let through. A warn verdict is
// the reviewer saying it could not establish the change was clean, so a turn
// that ships on one owes the user that sentence — collecting it and never
// reading it is what made "conditional pass" indistinguishable from a pass.
func (a *Agent) reportReviewWarnings() {
	if a == nil || a.svc.sink == nil || len(a.turn.reviewWarnings) == 0 {
		return
	}
	seen := map[string]bool{}
	kept := make([]string, 0, len(a.turn.reviewWarnings))
	for _, w := range a.turn.reviewWarnings {
		if w = strings.TrimSpace(w); w != "" && !seen[w] {
			seen[w] = true
			kept = append(kept, w)
		}
	}
	a.turn.reviewWarnings = nil
	if len(kept) == 0 {
		return
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
		Text:   "Review shipped with unresolved warnings.",
		Detail: strings.Join(kept, "; ")})
}

// reviewProofHint names why a report the turn did submit does not close its
// obligation. Without it the gate repeats a demand the model believes it has
// already met, and the rule it invents for that outlives the turn.
func (a *Agent) reviewProofHint(kind evidence.ReviewKind) string {
	switch a.task.ledger.ReviewProofGapFor(kind) {
	case evidence.ReviewProofUnattributed:
		return "; the " + string(kind) + " report you have was submitted by a run the host cannot name, so it proves nothing — start the reviewer through its own tool, not read_only_skill"
	case evidence.ReviewProofUngranted:
		return "; the " + string(kind) + " report you have came from a run not authorized to answer for this, so it proves nothing — start the reviewer that is"
	}
	return ""
}

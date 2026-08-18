package taskcontract

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/evidence"
)

// RebuildFacts is the only allowed source material for a contract replay.
type RebuildFacts struct {
	Plan                    *PlanFacts
	GoalCriteria            []PlanCriterion
	Todos                   []evidence.TodoItem
	ProjectChecks           []string
	Receipts                []evidence.Receipt
	TestsForbidden          bool
	RequireFullVerification bool
	WorkspaceRoot           string
	HasApprovedPlan         bool
	HasActiveGoal           bool
}

// Rebuild constructs a contract purely from plan, goal, todo, checks, and
// receipts. The same fact sequence always yields the same contract.
func Rebuild(facts RebuildFacts) *Contract {
	var c *Contract
	switch {
	case facts.Plan != nil:
		c = FromPlan("", *facts.Plan)
	default:
		c = New("")
	}
	for _, criterion := range facts.GoalCriteria {
		id := strings.TrimSpace(criterion.ID)
		if id == "" {
			id = fmt.Sprintf("g%d", len(c.Requirements)+1)
		}
		c.AddRequirement(id, criterion.Text, true)
	}
	if facts.Plan == nil {
		for i, todo := range facts.Todos {
			c.AddRequirement(fmt.Sprintf("t%d", i+1), todo.Content, true)
			if todo.Status == "completed" {
				c.Resolve(fmt.Sprintf("t%d", i+1), Satisfied)
			}
		}
	}
	if facts.HasApprovedPlan {
		c.promoteCriteriaStrict(ReasonApprovedPlan)
	} else if facts.HasActiveGoal {
		c.promoteCriteriaStrict(ReasonActiveGoal)
	}
	for _, command := range facts.ProjectChecks {
		c.AddCheck(command)
	}
	for i, rec := range facts.Receipts {
		c.AbsorbReceipt(i+1, rec, facts.WorkspaceRoot, facts.TestsForbidden, facts.RequireFullVerification)
	}
	return c
}

// AbsorbReceipt folds one frozen receipt. Denied or failed writers do not
// create post-success obligations; later related writes stale old proofs.
func (c *Contract) AbsorbReceipt(seq int, rec evidence.Receipt, workspaceRoot string, testsForbidden, requireFullVerification bool) {
	if c == nil {
		return
	}
	c.Observe(rec)
	if rec.ToolName == "complete_step" && rec.Success {
		c.satisfyKindAfter(ObligationSignoff, seq, rec)
		c.resolveCitedCriteria(rec)
	}
	if !rec.Success {
		return
	}
	profile := profileFromReceipt(rec)
	if profile.MutatesState() {
		c.invalidateAfterWrite(seq, profile.TargetKeys())
		mapping := MapWriter(profile, seq, workspaceRoot, testsForbidden)
		for _, o := range mapping.PostSuccess {
			c.addObligation(o)
		}
		if requireFullVerification && (profile.WorkspaceWrite || profile.RepoMetadata) {
			enforcement := EnforcementStrict
			if testsForbidden {
				enforcement = EnforcementAdvisory
			}
			c.addObligation(newObligation(ObligationFullVerify, enforcement, ReasonUserConstraint, seq, profile.TargetKeys()))
		}
		c.satisfyKindAfter(ObligationActionReceipt, seq, rec)
		return
	}
	c.satisfyFromReceipt(seq, rec, profile)
}

func profileFromReceipt(rec evidence.Receipt) evidence.EffectProfile {
	args := rec.Args
	if rec.Command != "" && (len(args) == 0 || string(args) == "null") {
		if raw, err := json.Marshal(map[string]string{"command": rec.Command}); err == nil {
			args = raw
		}
	}
	profile := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName:       rec.ToolName,
		Args:           args,
		ActualPaths:    rec.Paths,
		StaticReadOnly: rec.Read && !rec.Write && !rec.Mutation,
	})
	if len(rec.Paths) > 0 || rec.Command != "" {
		return profile
	}
	name := strings.ToLower(strings.TrimSpace(rec.ToolName))
	if name == "bash" || name == "shell" || rec.Write && (profile.RepoMetadata || profile.HostState || profile.ExternalState || profile.Destructive) {
		return profile
	}
	if len(profile.Targets) == 0 && (profile.OpaqueWriter() || profile.Reason == evidence.ReasonOpaqueWriter) && !profile.RepoMetadata && !profile.HostState && !profile.ExternalState {
		// Executed MCP/proxy with no persisted paths is not a workspace writer.
		return evidence.EffectProfile{Known: true, ReadOnly: true, Reason: evidence.ReasonReadOnly}
	}
	return profile
}

func (c *Contract) addObligation(o Obligation) {
	for i := range c.Obligations {
		if c.Obligations[i].Kind == o.Kind && sameTargets(c.Obligations[i].Targets, o.Targets) && !c.obligationSatisfied(c.Obligations[i]) {
			if o.Enforcement > c.Obligations[i].Enforcement {
				c.Obligations[i].Enforcement = o.Enforcement
			}
			if o.Since > c.Obligations[i].Since {
				c.Obligations[i].Since = o.Since
			}
			return
		}
	}
	c.Obligations = append(c.Obligations, cloneObligation(o))
}

func (c *Contract) promoteCriteriaStrict(origin ReasonCode) {
	hasRequired := false
	for _, req := range c.Requirements {
		if !req.Required {
			continue
		}
		hasRequired = true
		c.addObligation(Obligation{
			Kind:        ObligationCriteria,
			Enforcement: EnforcementStrict,
			Origin:      origin,
		})
		break
	}
	if !hasRequired {
		return
	}
	c.addObligation(Obligation{Kind: ObligationTodo, Enforcement: EnforcementStrict, Origin: origin})
}

func (c *Contract) invalidateAfterWrite(seq int, targets []evidence.TargetKey) {
	for i := range c.Obligations {
		o := &c.Obligations[i]
		if !invalidatedByWrite(o.Kind) {
			continue
		}
		if !c.obligationSatisfied(*o) {
			continue
		}
		if o.Kind != ObligationFullVerify && len(o.Targets) > 0 && len(targets) > 0 && !targetOverlap(o.Targets, targets) {
			continue
		}
		o.SatisfiedBy = nil
		o.Since = seq
	}
}

func invalidatedByWrite(kind ObligationKind) bool {
	switch kind {
	case ObligationTargetedVerify, ObligationFullVerify, ObligationDiffReview,
		ObligationIndependentReview, ObligationSecurityReview, ObligationSignoff:
		return true
	default:
		return false
	}
}

func (c *Contract) satisfyFromReceipt(seq int, rec evidence.Receipt, profile evidence.EffectProfile) {
	if rec.ToolName == "todo_write" && rec.Success {
		c.syncActiveGoalTodos(rec.Todos)
		c.satisfyKindAfter(ObligationTodo, seq, rec)
	}
	if rec.Command != "" && evidence.IsVerificationCommand(rec.Command) && rec.Success {
		if verificationReceiptFailed(rec) {
			c.invalidateVerificationProofs(seq)
			return
		}
		c.satisfyKindAfter(ObligationTargetedVerify, seq, rec)
		if c.fullVerificationSatisfied(rec) {
			c.satisfyKindAfter(ObligationFullVerify, seq, rec)
		}
	}
	c.satisfyReviewReceipt(seq, rec, profile)
}

func (c *Contract) syncActiveGoalTodos(todos []evidence.TodoItem) {
	if !c.hasObligationOrigin(ReasonActiveGoal) || len(todos) == 0 {
		return
	}
	statusByText := make(map[string]string, len(todos))
	for _, todo := range todos {
		statusByText[strings.TrimSpace(todo.Content)] = strings.TrimSpace(todo.Status)
	}
	for i := range c.Requirements {
		status, ok := statusByText[strings.TrimSpace(c.Requirements[i].Text)]
		if !ok {
			continue
		}
		if status == "completed" {
			c.Requirements[i].Status = Satisfied
		} else {
			c.Requirements[i].Status = Pending
		}
	}
}

func (c *Contract) hasObligationOrigin(origin ReasonCode) bool {
	for _, obligation := range c.Obligations {
		if obligation.Origin == origin {
			return true
		}
	}
	return false
}

func (c *Contract) fullVerificationSatisfied(rec evidence.Receipt) bool {
	hasDeclaredChecks := false
	requiresBroadFallback := false
	for _, check := range c.Checks {
		if check.Kind != CheckCommand {
			continue
		}
		hasDeclaredChecks = true
		if check.Status != Satisfied {
			return false
		}
		if strings.TrimSpace(check.Command) == "" {
			requiresBroadFallback = true
		}
	}
	if hasDeclaredChecks && !requiresBroadFallback {
		return true
	}
	return evidence.IsFullVerificationCommand(rec.Command)
}

func (c *Contract) satisfyReviewReceipt(seq int, rec evidence.Receipt, profile evidence.EffectProfile) {
	if !rec.Success {
		return
	}
	if rec.ToolName == "review_report" {
		report, err := evidence.ParseReviewReport(rec.Args)
		if err != nil {
			return
		}
		covers := func(o Obligation) bool { return report.CoversPaths(obligationTargetPaths(o.Targets)) }
		if report.HasBlockingFinding() {
			c.invalidateKindMatching(ObligationDiffReview, seq, covers)
			switch report.Kind {
			case evidence.ReviewKindReview:
				c.invalidateKindMatching(ObligationIndependentReview, seq, covers)
			case evidence.ReviewKindSecurity:
				c.invalidateKindMatching(ObligationSecurityReview, seq, covers)
			}
			return
		}
		c.satisfyKindAfterMatching(ObligationDiffReview, seq, rec, covers)
		switch report.Kind {
		case evidence.ReviewKindReview:
			c.satisfyKindAfterMatching(ObligationIndependentReview, seq, rec, covers)
		case evidence.ReviewKindSecurity:
			c.satisfyKindAfterMatching(ObligationSecurityReview, seq, rec, covers)
		}
		return
	}
	if evidence.ReceiptShowsWholeGitDiff(rec) {
		c.satisfyKindAfter(ObligationDiffReview, seq, rec)
		return
	}
	toolName := strings.ToLower(strings.TrimSpace(rec.ToolName))
	if toolName == "bash" || toolName == "shell" {
		c.satisfyKindAfterMatching(ObligationDiffReview, seq, rec, func(o Obligation) bool {
			paths := obligationTargetPaths(o.Targets)
			if len(paths) == 0 {
				return false
			}
			for _, path := range paths {
				if !evidence.ReceiptShowsContentForPath(rec, path) {
					return false
				}
			}
			return true
		})
		return
	}
	if !rec.Read {
		return
	}
	observed := make([]string, 0, len(profile.Targets))
	for _, target := range profile.Targets {
		if path := strings.TrimSpace(target.Path); path != "" {
			observed = append(observed, path)
		}
	}
	coverage := evidence.ReviewReport{ReviewedPaths: observed}
	c.satisfyKindAfterMatching(ObligationDiffReview, seq, rec, func(o Obligation) bool {
		return coverage.CoversPaths(obligationTargetPaths(o.Targets))
	})
}

func (c *Contract) invalidateVerificationProofs(seq int) {
	for i := range c.Obligations {
		o := &c.Obligations[i]
		if o.Kind != ObligationTargetedVerify && o.Kind != ObligationFullVerify {
			continue
		}
		o.SatisfiedBy = nil
		o.Since = seq
	}
}

func (c *Contract) invalidateKindMatching(kind ObligationKind, seq int, matches func(Obligation) bool) {
	for i := range c.Obligations {
		o := &c.Obligations[i]
		if o.Kind != kind || !matches(*o) {
			continue
		}
		o.SatisfiedBy = nil
		o.Since = seq
	}
}

func obligationTargetPaths(targets []evidence.TargetKey) []string {
	var paths []string
	for _, target := range targets {
		kind, path, ok := strings.Cut(string(target), ":")
		if !ok || path == "" || kind != "file" && kind != "dir" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func (c *Contract) satisfyKindAfter(kind ObligationKind, seq int, rec evidence.Receipt) {
	c.satisfyKindAfterMatching(kind, seq, rec, func(Obligation) bool { return true })
}

func (c *Contract) satisfyKindAfterMatching(kind ObligationKind, seq int, rec evidence.Receipt, matches func(Obligation) bool) {
	if rec.Verification == evidence.VerificationFailed {
		return
	}
	for i := range c.Obligations {
		o := &c.Obligations[i]
		if o.Kind != kind || seq < o.Since || !matches(*o) {
			continue
		}
		if containsInt(o.SatisfiedBy, seq) {
			continue
		}
		o.SatisfiedBy = append(copyInts(o.SatisfiedBy), seq)
	}
}

func (c *Contract) obligationSatisfied(o Obligation) bool {
	if o.Kind == ObligationTodo && (o.Origin == ReasonApprovedPlan || o.Origin == ReasonActiveGoal) {
		return c.hasRequiredRequirement()
	}
	if o.Kind == ObligationCriteria && (o.Origin == ReasonApprovedPlan || o.Origin == ReasonActiveGoal) {
		return c.requiredRequirementsSatisfied()
	}
	return len(o.SatisfiedBy) > 0
}

func (c *Contract) hasRequiredRequirement() bool {
	for _, req := range c.Requirements {
		if req.Required {
			return true
		}
	}
	return false
}

func (c *Contract) requiredRequirementsSatisfied() bool {
	found := false
	for _, req := range c.Requirements {
		if !req.Required {
			continue
		}
		found = true
		if req.Status != Satisfied {
			return false
		}
	}
	return found
}

func sameTargets(a, b []evidence.TargetKey) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[evidence.TargetKey]int, len(a))
	for _, k := range a {
		seen[k]++
	}
	for _, k := range b {
		if seen[k] == 0 {
			return false
		}
		seen[k]--
	}
	return true
}

func targetOverlap(a, b []evidence.TargetKey) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	seen := make(map[evidence.TargetKey]bool, len(a))
	for _, k := range a {
		seen[k] = true
	}
	for _, k := range b {
		if seen[k] {
			return true
		}
	}
	return false
}

func (c *Contract) resolveCitedCriteria(rec evidence.Receipt) {
	if c == nil || rec.ToolName != "complete_step" || !rec.Success || len(rec.Args) == 0 {
		return
	}
	var payload struct {
		Evidence []struct {
			Kind        string `json:"kind"`
			CriterionID string `json:"criterion_id"`
		} `json:"evidence"`
	}
	if json.Unmarshal(rec.Args, &payload) != nil {
		return
	}
	for _, e := range payload.Evidence {
		id := strings.TrimSpace(e.CriterionID)
		if id == "" {
			continue
		}
		kind := EvidenceRead
		switch e.Kind {
		case "verification":
			kind = EvidenceVerification
		case "review":
			kind = EvidenceReview
		case "diff", "files":
			kind = EvidenceMutation
		}
		c.Resolve(id, Satisfied, EvidenceRef{
			Kind:          kind,
			MutationEpoch: c.Epoch(),
			Source:        "complete_step",
			Success:       true,
		})
	}
}

func containsInt(in []int, v int) bool {
	return slices.Contains(in, v)
}

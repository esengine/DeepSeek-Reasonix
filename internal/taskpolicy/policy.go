// Package taskpolicy builds the host-side TaskPolicy that freezes planning,
// verification, review, evidence closure, and natural-language constraints for
// one turn before the first model request. It never calls a classification
// model. Reasonix exposes a single adaptive standard execution: the policy is
// derived from task intent, complexity, risk, and user constraints — never
// from a selectable execution mode.
package taskpolicy

import (
	"regexp"
	"strings"

	"reasonix/internal/shellparse"
	"reasonix/internal/taskintent"
)

// PolicyVersion is the diagnostic version stamped on every TaskPolicy and the
// transient execution-policy block. Version 2 removed the preset attribute and
// added the evidence closed-loop level.
const PolicyVersion = 2

// Intent is the host's coarse task intent for the turn.
type Intent = taskintent.Intent

// Risk is the turn risk level; it may only ratchet upward within a turn.
type Risk uint8

const (
	RiskLow Risk = iota
	RiskMedium
	RiskHigh
)

// Route is the planner route chosen for this turn.
type Route uint8

const (
	// RouteDirect executes without an independent planner phase.
	RouteDirect Route = iota
	// RouteLightPlan uses lightweight internal planning.
	RouteLightPlan
	// RouteFullPlan requires a complete plan before execution.
	RouteFullPlan
)

// Verification is the verification level for this turn.
type Verification uint8

const (
	// VerifyNone requires no host verification commands.
	VerifyNone Verification = iota
	// VerifyTargeted runs the cheapest relevant checks and diff review.
	VerifyTargeted
	// VerifyFull requires every acceptance check for the current epoch.
	VerifyFull
)

// Review is the independent review level for this turn.
type Review uint8

const (
	// ReviewNone never starts an independent reviewer.
	ReviewNone Review = iota
	// ReviewConditional starts a reviewer when evidence coverage is incomplete
	// or the change spans modules.
	ReviewConditional
	// ReviewForced always starts an independent reviewer.
	ReviewForced
	// ReviewForcedSecurity always starts reviewer plus security-review.
	ReviewForcedSecurity
)

// Evidence is the evidence closed-loop level for the turn. It replaces every
// historical delivery-profile gate: acceptance criteria before mutations,
// todo/step ownership, opaque-bash restrictions, capability call preferences,
// post-write verification, review, and final completion.
type Evidence uint8

const (
	// EvidenceNone applies to plain conversation: no evidence ledger gates.
	EvidenceNone Evidence = iota
	// EvidenceTargeted requires targeted checks or cited read receipts; a
	// Partial/Unverified ending is allowed when checks are unavailable.
	EvidenceTargeted
	// EvidenceClosedLoop requires full acceptance evidence closure: criteria
	// before state change, verification after the latest mutation, review, and
	// evidence-backed sign-off. Missing evidence can only end Partial,
	// Unverified, or Blocked — never Complete.
	EvidenceClosedLoop
)

// Constraints are natural-language and host-boundary limits for the turn.
type Constraints struct {
	// ForbidMutation blocks every real writer before execution.
	ForbidMutation bool
	// ForbidTests blocks verification commands; gaps become Partial.
	ForbidTests bool
	// AllowedChecks, when non-empty, limits verification to those exact commands.
	AllowedChecks []string
	// ForbidExternal blocks push/publish/deploy-style external actions.
	ForbidExternal bool
	// RequireFullVerification forces VerifyFull and closed-loop evidence.
	RequireFullVerification bool
	// PlanModeReadOnly is the explicit plan-mode read-only boundary.
	PlanModeReadOnly bool
	// Notes records structured reasons for diagnostics (never user-facing).
	Notes []string
}

// Input is the host-trusted material used to derive a TaskPolicy. Quoted and
// fenced regions must already be stripped by StripQuotedConstraints or left
// intact so Derive ignores them.
type Input struct {
	// Raw is the original user text (including quotes/fences). Used only for
	// intent classification when RawForIntent is empty.
	Raw string
	// Instruction is user text with quoted/fenced content removed so constraint
	// phrases inside citations cannot bind the host.
	Instruction string
	// PlanMode is the collaboration plan-mode flag.
	PlanMode bool
	// GoalActive marks a live multi-turn Goal: planning and closure ratchet up.
	GoalActive bool
	// HighRiskHints are host signals (permission/auth/release/security class).
	HighRiskHints bool
	// MediumRiskHints are host signals (cross-module / migration).
	MediumRiskHints bool
	// MultiFile / CrossSurface come from planner-gate style features.
	MultiFile    bool
	CrossSurface bool
	// Anchored means the user named concrete files or targets.
	Anchored bool
	// Structured is true for multi-step structured requests.
	Structured bool
}

// TaskPolicy is the authoritative host policy for one turn.
type TaskPolicy struct {
	Intent Intent
	Risk   Risk
	Route  Route
	// Evidence is the closed-loop level that replaced deliveryProfile gates.
	Evidence     Evidence
	Constraints  Constraints
	Verification Verification
	Review       Review
	// SecurityClass marks auth/permission/release/security work that forces
	// security review.
	SecurityClass bool
	// PolicyVersion is diagnostic only.
	PolicyVersion int
	// RequireAtomicContract forces a zero-extra-model-call Atomic TaskContract
	// on direct single-file modifications.
	RequireAtomicContract bool
	// AllowExploreSubagent permits proactive explore/research sub-agents for
	// mid/high-risk or Goal-active tasks whose scope deterministic rules cannot
	// reliably place.
	AllowExploreSubagent bool
	// SemanticRouterAllowed permits the LLM capability router when deterministic
	// routing cannot place a mid/high-risk request.
	SemanticRouterAllowed bool
}

// Derive builds a TaskPolicy from host-trusted input without model calls. All
// tasks share one standard matrix; only intent, risk, structure, and user
// constraints change the outcome.
func Derive(in Input) TaskPolicy {
	instruction := strings.TrimSpace(in.Instruction)
	if instruction == "" {
		instruction = StripQuotedConstraints(in.Raw)
	}
	rawIntent := strings.TrimSpace(in.Raw)
	if rawIntent == "" {
		rawIntent = instruction
	}
	intent := taskintent.Classify(rawIntent)

	constraints := parseConstraints(instruction)
	if in.PlanMode {
		constraints.PlanModeReadOnly = true
		constraints.ForbidMutation = true
		constraints.Notes = append(constraints.Notes, "plan_mode_read_only")
	}

	securityClass := in.HighRiskHints || isSecurityClass(instruction)
	risk := RiskLow
	if in.MediumRiskHints || in.MultiFile || in.CrossSurface || in.Structured {
		risk = RiskMedium
	}
	if in.HighRiskHints || securityClass || isHighRisk(instruction) {
		risk = RiskHigh
	}
	// Intent-driven floor: external/persist actions start at least medium.
	if intent == taskintent.PersistentAction && risk < RiskMedium {
		risk = RiskMedium
	}

	route := chooseRoute(intent, risk, in, constraints)

	if constraints.ForbidTests {
		constraints.Notes = append(constraints.Notes, "forbid_tests")
	}
	verification := chooseVerification(intent, risk, in, constraints)
	review := chooseReview(risk, securityClass, in, constraints)

	// Evidence closed-loop level.
	closedLoopTriggers := risk >= RiskHigh || securityClass || in.CrossSurface ||
		in.GoalActive || constraints.RequireFullVerification
	evidence := evidenceFor(intent, risk, closedLoopTriggers, in, constraints)

	return TaskPolicy{
		Intent:                intent,
		Risk:                  risk,
		Route:                 route,
		Evidence:              evidence,
		Constraints:           constraints,
		Verification:          verification,
		Review:                review,
		SecurityClass:         securityClass,
		PolicyVersion:         PolicyVersion,
		RequireAtomicContract: intent == taskintent.Mutation && route == RouteDirect && risk == RiskLow && !in.GoalActive,
		AllowExploreSubagent:  risk >= RiskMedium || in.GoalActive,
		SemanticRouterAllowed: risk >= RiskMedium || securityClass || in.GoalActive,
	}
}

// evidenceFor assigns the evidence closed-loop level per the standard matrix:
// conversation needs none; ordinary read-only work and same-surface mutations
// stay targeted so quality gaps can be reported without interrupting the turn.
// Persistent actions, cross-surface work, active Goals, and anything high-risk,
// security-class, or user-required fully verified close the loop.
func evidenceFor(intent Intent, risk Risk, closedLoopTriggers bool, in Input, constraints Constraints) Evidence {
	switch {
	case constraints.ForbidMutation || constraints.PlanModeReadOnly:
		// Read-only work: cite actual reads; no mutation closure ceremony.
		return EvidenceTargeted
	case intent == taskintent.Conversation || intent == taskintent.Advisory:
		if !closedLoopTriggers && risk == RiskLow && !in.Structured && !in.MultiFile {
			return EvidenceNone
		}
		structural := risk >= RiskHigh || in.MultiFile || in.Structured || in.CrossSurface || in.GoalActive
		if closedLoopTriggers && structural {
			return EvidenceClosedLoop
		}
		return EvidenceTargeted
	case intent == taskintent.PersistentAction:
		return EvidenceClosedLoop
	case intent == taskintent.Mutation:
		// Planning complexity and completion enforcement are separate axes.
		// Same-surface structured work may plan and review more while missing
		// quality evidence remains visible. High assurance still hard-stops.
		if closedLoopTriggers {
			return EvidenceClosedLoop
		}
		return EvidenceTargeted
	default:
		if closedLoopTriggers {
			return EvidenceClosedLoop
		}
		return EvidenceTargeted
	}
}

func chooseVerification(intent Intent, risk Risk, in Input, constraints Constraints) Verification {
	verification := VerifyTargeted
	if constraints.RequireFullVerification || risk >= RiskHigh {
		verification = VerifyFull
	} else if risk >= RiskMedium && (in.CrossSurface ||
		(in.MultiFile && (intent == taskintent.Mutation || intent == taskintent.PersistentAction))) {
		verification = VerifyFull
	}
	if (intent == taskintent.Conversation || intent == taskintent.Advisory) &&
		!in.MultiFile && !in.Structured && risk == RiskLow {
		return VerifyNone
	}
	return verification
}

func chooseReview(risk Risk, securityClass bool, in Input, constraints Constraints) Review {
	if constraints.ForbidMutation {
		return ReviewNone
	}
	review := reviewForRisk(risk, securityClass)
	if constraints.RequireFullVerification && review < ReviewForced {
		review = ReviewForced
	}
	if in.GoalActive && review < ReviewForced {
		review = ReviewForced
		if securityClass {
			review = ReviewForcedSecurity
		}
	}
	return review
}

// reviewForRisk returns the standard review floor for a risk level.
func reviewForRisk(risk Risk, securityClass bool) Review {
	if securityClass {
		return ReviewForcedSecurity
	}
	switch risk {
	case RiskHigh:
		return ReviewForced
	case RiskMedium:
		return ReviewConditional
	default:
		return ReviewNone
	}
}

// RaiseRisk ratchets risk upward; never decreases it. Floors for route,
// verification, review, and evidence re-evaluate so later receipts can only
// strengthen the contract.
func (p *TaskPolicy) RaiseRisk(r Risk) {
	if p == nil || r <= p.Risk {
		return
	}
	p.Risk = r
	p.reevaluateFloors()
}

// EscalateConditionalReview promotes a conditional review to forced when the
// evidence ledger observes weak coverage: changes that touch public API,
// schema, persistence, auth, or release surfaces; scope that outgrew the
// initial judgment; weak, ambiguous, or uncovered acceptance criteria; or
// required verification that failed or could not run. It never lowers review.
func (p *TaskPolicy) EscalateConditionalReview(reason string) {
	if p == nil {
		return
	}
	if reason != "" {
		p.Constraints.Notes = append(p.Constraints.Notes, "review_escalation:"+reason)
	}
	if p.Constraints.ForbidMutation {
		return
	}
	if p.Review < ReviewForced {
		p.Review = ReviewForced
	}
	if p.SecurityClass && p.Review < ReviewForcedSecurity {
		p.Review = ReviewForcedSecurity
	}
	if (p.Risk >= RiskHigh || p.SecurityClass || p.Constraints.RequireFullVerification) &&
		p.Evidence < EvidenceClosedLoop {
		p.Evidence = EvidenceClosedLoop
	}
}

// InheritFrom merges a writer parent's floors into a child policy so sub-agent
// execution never runs below the parent task's risk and closure requirements.
// Read-only children do not inherit; their read-only boundary stays absolute.
func (p *TaskPolicy) InheritFrom(parent TaskPolicy) {
	if p == nil {
		return
	}
	if parent.Risk > p.Risk {
		p.RaiseRisk(parent.Risk)
	}
	if parent.Evidence > p.Evidence {
		p.Evidence = parent.Evidence
	}
	if parent.Verification > p.Verification {
		p.Verification = parent.Verification
	}
	if parent.Review > p.Review {
		p.Review = parent.Review
	}
	if parent.SecurityClass {
		p.SecurityClass = true
		if p.Review < ReviewForcedSecurity {
			p.Review = ReviewForcedSecurity
		}
	}
	if parent.RequireAtomicContract {
		p.RequireAtomicContract = true
	}
	if parent.Evidence >= EvidenceClosedLoop && p.Intent == taskintent.Mutation {
		if p.Verification < VerifyTargeted {
			p.Verification = VerifyTargeted
		}
	}
}

// reevaluateFloors recomputes the ratchet-up floors after a risk raise.
func (p *TaskPolicy) reevaluateFloors() {
	if review := reviewForRisk(p.Risk, p.SecurityClass); review > p.Review && !p.Constraints.ForbidMutation {
		p.Review = review
	}
	if p.Risk >= RiskHigh {
		if p.Verification < VerifyFull {
			p.Verification = VerifyFull
		}
		if p.Route < RouteFullPlan {
			p.Route = RouteFullPlan
		}
		if p.Evidence < EvidenceClosedLoop && !p.Constraints.ForbidMutation {
			p.Evidence = EvidenceClosedLoop
		}
		if !p.AllowExploreSubagent {
			p.AllowExploreSubagent = true
		}
		if !p.SemanticRouterAllowed {
			p.SemanticRouterAllowed = true
		}
	}
}

// AllowsMutation reports whether a real writer may proceed under this policy.
func (p TaskPolicy) AllowsMutation() bool {
	return !p.Constraints.ForbidMutation && !p.Constraints.PlanModeReadOnly
}

// AllowsTests reports whether verification commands may run.
func (p TaskPolicy) AllowsTests() bool {
	return !p.Constraints.ForbidTests
}

// AllowsCommand reports whether a specific verification command is permitted.
func (p TaskPolicy) AllowsCommand(command string) bool {
	if !p.AllowsTests() {
		return false
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return true
	}
	if len(p.Constraints.AllowedChecks) == 0 {
		return true
	}
	for _, allowed := range p.Constraints.AllowedChecks {
		if strings.EqualFold(strings.TrimSpace(allowed), command) {
			return true
		}
	}
	// Static argv prefix match: "only run go test" may allow "go test ./...",
	// but shell operators, substitutions, redirects, and extra commands do not
	// inherit that allowance.
	commandFields, malformed := shellparse.StaticFields(command)
	if malformed != "" || len(commandFields) == 0 {
		return false
	}
	for _, allowed := range p.Constraints.AllowedChecks {
		allowedFields, malformed := shellparse.StaticFields(strings.TrimSpace(allowed))
		if malformed == "" && len(allowedFields) > 0 && hasFieldPrefix(commandFields, allowedFields) {
			return true
		}
	}
	return false
}

func hasFieldPrefix(fields, prefix []string) bool {
	if len(prefix) > len(fields) {
		return false
	}
	for i := range prefix {
		if !strings.EqualFold(fields[i], prefix[i]) {
			return false
		}
	}
	return true
}

// AllowsExternal reports whether push/publish-style actions may run.
func (p TaskPolicy) AllowsExternal() bool {
	return !p.Constraints.ForbidExternal
}

// RequiresIndependentReview reports whether a structured reviewer must run.
func (p TaskPolicy) RequiresIndependentReview() bool {
	return p.Review == ReviewForced || p.Review == ReviewForcedSecurity
}

// RequiresSecurityReview reports whether security-review must also run.
func (p TaskPolicy) RequiresSecurityReview() bool {
	return p.Review == ReviewForcedSecurity
}

// ClosedLoop reports whether this turn must close the evidence loop.
func (p TaskPolicy) ClosedLoop() bool {
	return p.Evidence == EvidenceClosedLoop
}

// AllowsPartialWithoutChecks reports whether a turn may end Partial or
// Unverified when checks are user-forbidden or unavailable. Closed-loop turns
// never waive checks silently.
func (p TaskPolicy) AllowsPartialWithoutChecks() bool {
	return p.Constraints.ForbidTests || p.Evidence < EvidenceClosedLoop
}

// chooseRoute applies the standard planning matrix: direct for conversation,
// plain queries, and anchored single-file atomic edits; light planning for
// multi-file same-surface work; full planning for cross-surface, structured,
// Goal-active, and high-risk tasks.
func chooseRoute(intent Intent, risk Risk, in Input, constraints Constraints) Route {
	if intent == taskintent.Conversation || intent == taskintent.Advisory {
		if !in.MultiFile && !in.Structured && risk == RiskLow {
			return RouteDirect
		}
	}
	if risk >= RiskHigh {
		return RouteFullPlan
	}
	if constraints.RequireFullVerification && !constraints.ForbidMutation {
		return RouteFullPlan
	}
	if in.GoalActive && (intent == taskintent.Mutation || in.MultiFile || in.Structured) && !in.Anchored {
		return RouteFullPlan
	}
	if in.CrossSurface || (in.Structured && risk >= RiskMedium) {
		return RouteFullPlan
	}
	if in.MultiFile || in.Structured || (!in.Anchored && intent == taskintent.Mutation) {
		return RouteLightPlan
	}
	return RouteDirect
}

func parseConstraints(instruction string) Constraints {
	var c Constraints
	lower := strings.ToLower(instruction)
	// Analysis-only / no modifications
	if matchesAny(lower, []string{
		"只分析", "只读", "不要修改", "别改", "不要改", "仅分析", "只看不改",
		"analyze only", "analysis only", "don't modify", "do not modify",
		"don't change", "do not change", "no changes", "read only", "read-only",
		"without modifying", "without changes", "don't edit", "do not edit",
	}) {
		c.ForbidMutation = true
		c.Notes = append(c.Notes, "user_forbid_mutation")
	}
	// Reproduce but don't fix
	if matchesAny(lower, []string{
		"复现但不修复", "只复现", "不要修复", "reproduce but don't fix",
		"reproduce only", "don't fix", "do not fix", "no fix",
	}) {
		c.ForbidMutation = true
		c.Notes = append(c.Notes, "user_reproduce_only")
	}
	// No tests
	if matchesAny(lower, []string{
		"不要测试", "别跑测试", "不用测试", "跳过测试", "不要跑测试",
		"don't run tests", "do not run tests", "no tests", "skip tests",
		"without tests", "don't test", "do not test",
	}) {
		c.ForbidTests = true
		c.Notes = append(c.Notes, "user_forbid_tests")
	}
	// Full verification / closed-loop delivery request
	if matchesAny(lower, []string{
		"完整验证", "全面验证", "闭环交付", "完整交付", "交付前检查", "验收闭环",
		"full verification", "complete verification", "verify everything",
		"closed-loop delivery", "deliver with verification",
	}) {
		c.RequireFullVerification = true
		c.Notes = append(c.Notes, "user_require_full_verification")
	}
	// Only run X
	if cmds := parseAllowedChecks(instruction); len(cmds) > 0 {
		c.AllowedChecks = cmds
		c.Notes = append(c.Notes, "user_allowed_checks")
	}
	// No push / no publish
	if matchesAny(lower, []string{
		"不要 push", "不要push", "别 push", "别push", "不要推送", "不要发布",
		"don't push", "do not push", "no push", "don't publish", "do not publish",
		"no publish", "don't deploy", "do not deploy",
	}) {
		c.ForbidExternal = true
		c.Notes = append(c.Notes, "user_forbid_external")
	}
	return c
}

func parseAllowedChecks(instruction string) []string {
	// Patterns: "只跑 X", "only run X", "just run X"
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)只跑\s+([^\n,，;；]+)`),
		regexp.MustCompile(`(?i)只运行\s+([^\n,，;；]+)`),
		regexp.MustCompile(`(?i)only\s+run\s+([^\n,;]+)`),
		regexp.MustCompile(`(?i)just\s+run\s+([^\n,;]+)`),
	}
	var out []string
	for _, re := range patterns {
		m := re.FindStringSubmatch(instruction)
		if len(m) < 2 {
			continue
		}
		cmd := strings.TrimSpace(m[1])
		cmd = strings.Trim(cmd, "\"'`。.")
		if cmd != "" {
			out = append(out, cmd)
		}
	}
	return out
}

func matchesAny(lower string, needles []string) bool {
	for _, n := range needles {
		if n == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

func isSecurityClass(instruction string) bool {
	lower := strings.ToLower(instruction)
	return matchesAny(lower, []string{
		"security", "authentication", "authorization", "permission model",
		"credential", "secret", "oauth", "jwt", "cve", "xss", "sqli",
		"privilege escalation", "release to production", "publish package",
		"deploy to production", "production deploy",
		"安全漏洞", "权限提升", "认证绕过", "鉴权", "凭证泄露", "密钥泄露", "上线发布", "生产环境",
	})
}

func isHighRisk(instruction string) bool {
	lower := strings.ToLower(instruction)
	return matchesAny(lower, []string{
		"migrate", "migration", "drop table", "rm -rf", "force push",
		"destructive", "irreversible", "production data",
		"迁移", "删库", "不可逆", "生产数据",
	}) || isSecurityClass(instruction)
}

// StripQuotedConstraints removes fenced code blocks and quoted spans so
// constraint phrases inside citations do not bind the host.
func StripQuotedConstraints(raw string) string {
	s := raw
	// Fenced code blocks ``` ... ```
	s = stripFences(s)
	// Inline code `...`
	s = stripInlineCode(s)
	// Double-quoted and Chinese quotation spans (non-greedy, single line-ish)
	s = stripQuoted(s, '"', '"')
	s = stripQuoted(s, '“', '”')
	s = stripQuoted(s, '「', '」')
	return strings.TrimSpace(s)
}

func stripFences(s string) string {
	var b strings.Builder
	inFence := false
	for line := range strings.SplitSeq(s, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func stripInlineCode(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '`' {
			in = !in
			continue
		}
		if !in {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripQuoted(s string, open, close rune) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if !in && r == open {
			in = true
			continue
		}
		if in && r == close {
			in = false
			continue
		}
		if !in {
			b.WriteRune(r)
		}
	}
	return b.String()
}

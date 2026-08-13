// Package taskpolicy builds the host-side TaskPolicy that freezes planning,
// verification, review, and natural-language constraints for one turn before
// the first model request. It never calls a classification model.
package taskpolicy

import (
	"regexp"
	"strings"
	"unicode"

	"reasonix/internal/agentpreset"
	"reasonix/internal/shellparse"
	"reasonix/internal/taskintent"
)

// PolicyVersion is the diagnostic version stamped on every TaskPolicy.
const PolicyVersion = agentpreset.PolicyVersion

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
type Route = agentpreset.PlannerRoute

const (
	RouteDirect    = agentpreset.RouteDirect
	RouteLightPlan = agentpreset.RouteLightPlan
	RouteFullPlan  = agentpreset.RouteFullPlan
)

// Verification is the verification level for this turn.
type Verification = agentpreset.VerificationLevel

const (
	VerifyNone     = agentpreset.VerifyNone
	VerifyTargeted = agentpreset.VerifyTargeted
	VerifyFull     = agentpreset.VerifyFull
)

// Review is the independent review level for this turn.
type Review = agentpreset.ReviewLevel

const (
	ReviewNone           = agentpreset.ReviewNone
	ReviewConditional    = agentpreset.ReviewConditional
	ReviewForced         = agentpreset.ReviewForced
	ReviewForcedSecurity = agentpreset.ReviewForcedSecurity
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
	// RequireFullVerification forces VerifyFull regardless of preset.
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
	// Preset is the frozen role setting for this turn.
	Preset agentpreset.AgentPreset
	// PlanMode is the collaboration plan-mode flag.
	PlanMode bool
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
	Preset       agentpreset.AgentPreset
	Intent       Intent
	Risk         Risk
	Route        Route
	Constraints  Constraints
	Verification Verification
	Review       Review
	// SecurityClass marks auth/permission/release/security work that elevates
	// Light's review floor.
	SecurityClass bool
	// PolicyVersion is diagnostic only.
	PolicyVersion int
	// RequireAtomicContract forces an Atomic TaskContract on direct runs.
	RequireAtomicContract bool
	// AllowExploreSubagent mirrors the preset capability for explore workers.
	AllowExploreSubagent bool
	// SemanticRouterAllowed mirrors the preset capability for LLM routing.
	SemanticRouterAllowed bool
}

// Derive builds a TaskPolicy from host-trusted input without model calls.
func Derive(in Input) TaskPolicy {
	preset := agentpreset.Normalize(string(in.Preset))
	policy := agentpreset.PolicyOf(preset)

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
	// Intent-driven floor: mutations start at least low; external/persist medium.
	if intent == taskintent.PersistentAction && risk < RiskMedium {
		risk = RiskMedium
	}
	route := chooseRoute(policy, intent, risk, in)
	verification := policy.VerificationPolicy.Level
	if constraints.RequireFullVerification || !constraints.ForbidTests && risk >= RiskHigh {
		if policy.VerificationPolicy.Level < VerifyFull && (constraints.RequireFullVerification || risk >= RiskHigh) {
			if constraints.RequireFullVerification || securityClass || preset == agentpreset.Delivery {
				verification = VerifyFull
			}
		}
	}
	if constraints.ForbidTests {
		// Host still records the gap as Partial; verification commands stay blocked.
		constraints.Notes = append(constraints.Notes, "forbid_tests")
	}
	if preset == agentpreset.Light && risk >= RiskHigh {
		verification = VerifyFull
	}
	if preset == agentpreset.Delivery && risk >= RiskMedium {
		verification = VerifyFull
	}
	if intent == taskintent.Conversation || intent == taskintent.Advisory {
		if !in.MultiFile && risk == RiskLow {
			verification = VerifyNone
		}
	}

	review := policy.ReviewForRisk(int(risk), securityClass)
	if constraints.ForbidMutation {
		// Read-only turns never need independent review.
		review = ReviewNone
	}

	return TaskPolicy{
		Preset:                preset,
		Intent:                intent,
		Risk:                  risk,
		Route:                 route,
		Constraints:           constraints,
		Verification:          verification,
		Review:                review,
		SecurityClass:         securityClass,
		PolicyVersion:         PolicyVersion,
		RequireAtomicContract: policy.PlannerPolicy.RequireAtomicContract || (preset == agentpreset.Delivery && intent == taskintent.Mutation),
		AllowExploreSubagent:  policy.PlannerPolicy.AllowExploreSubagent && risk > RiskLow,
		SemanticRouterAllowed: policy.CapabilityPolicy.SemanticRouterAllowed && (risk >= RiskMedium || securityClass),
	}
}

// RaiseRisk ratchets risk upward; never decreases it.
func (p *TaskPolicy) RaiseRisk(r Risk) {
	if p == nil {
		return
	}
	if r > p.Risk {
		p.Risk = r
		// Re-evaluate review/verification floors after elevation.
		policy := agentpreset.PolicyOf(p.Preset)
		p.Review = policy.ReviewForRisk(int(p.Risk), p.SecurityClass)
		if p.Preset == agentpreset.Light && p.Risk >= RiskHigh {
			p.Verification = VerifyFull
			p.Route = RouteFullPlan
		}
		if p.Preset == agentpreset.Delivery && p.Risk >= RiskMedium {
			p.Verification = VerifyFull
			if p.Route < RouteFullPlan {
				p.Route = RouteFullPlan
			}
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

func chooseRoute(policy agentpreset.PresetPolicy, intent Intent, risk Risk, in Input) Route {
	if intent == taskintent.Conversation || intent == taskintent.Advisory {
		if !in.MultiFile && !in.Structured && risk == RiskLow {
			return RouteDirect
		}
	}
	if risk >= RiskHigh && policy.PlannerPolicy.FullPlanOnHighRisk {
		return RouteFullPlan
	}
	if risk >= RiskMedium && policy.PlannerPolicy.FullPlanOnMediumRisk {
		return RouteFullPlan
	}
	if in.CrossSurface || (in.Structured && risk >= RiskMedium) {
		if policy.PlannerPolicy.FullPlanOnMediumRisk || risk >= RiskHigh {
			return RouteFullPlan
		}
	}
	if in.MultiFile || in.Structured || (!in.Anchored && intent == taskintent.Mutation) {
		if policy.PlannerPolicy.PreferLightPlan {
			return RouteLightPlan
		}
		if policy.PlannerPolicy.FullPlanOnMediumRisk {
			return RouteFullPlan
		}
	}
	if policy.PlannerPolicy.DirectOK {
		return RouteDirect
	}
	return RouteLightPlan
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

// ExecutionPolicyBlock renders the short provider-visible transient user block
// that freezes the role setting for this turn. Callers persist it in Message
// Content and keep the original user text in RawContent.
func ExecutionPolicyBlock(p TaskPolicy) string {
	var b strings.Builder
	b.WriteString(`<execution-policy preset="`)
	b.WriteString(p.Preset.String())
	b.WriteString(`" version="`)
	b.WriteString(itoa(p.PolicyVersion))
	b.WriteString(`">`)
	b.WriteByte('\n')
	b.WriteString("route=")
	b.WriteString(routeName(p.Route))
	b.WriteString(" risk=")
	b.WriteString(riskName(p.Risk))
	b.WriteString(" verify=")
	b.WriteString(verifyName(p.Verification))
	b.WriteString(" review=")
	b.WriteString(reviewName(p.Review))
	if p.Constraints.ForbidMutation {
		b.WriteString("\nconstraint=no-mutation")
	}
	if p.Constraints.ForbidTests {
		b.WriteString("\nconstraint=no-tests")
	}
	if p.Constraints.ForbidExternal {
		b.WriteString("\nconstraint=no-external")
	}
	if len(p.Constraints.AllowedChecks) > 0 {
		b.WriteString("\nconstraint=only-checks:")
		b.WriteString(strings.Join(p.Constraints.AllowedChecks, ","))
	}
	if p.Constraints.PlanModeReadOnly {
		b.WriteString("\nconstraint=plan-mode-read-only")
	}
	b.WriteString("\n</execution-policy>")
	return b.String()
}

func routeName(r Route) string {
	switch r {
	case RouteLightPlan:
		return "light-plan"
	case RouteFullPlan:
		return "full-plan"
	default:
		return "direct"
	}
}

func riskName(r Risk) string {
	switch r {
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	default:
		return "low"
	}
}

func verifyName(v Verification) string {
	switch v {
	case VerifyTargeted:
		return "targeted"
	case VerifyFull:
		return "full"
	default:
		return "none"
	}
}

func reviewName(r Review) string {
	switch r {
	case ReviewConditional:
		return "conditional"
	case ReviewForced:
		return "forced"
	case ReviewForcedSecurity:
		return "forced-security"
	default:
		return "none"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// HasInstructionalContent reports whether s has non-space runes.
func HasInstructionalContent(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

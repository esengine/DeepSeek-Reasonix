package control

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/agentpreset"
	"reasonix/internal/taskpolicy"
)

const (
	plannerLightResearchRounds = 2
	plannerFullResearchRounds  = 6
)

const (
	plannerReasonExplicitPlanMode    = "explicit_plan_mode"
	plannerReasonSynthetic           = "synthetic"
	plannerReasonSlash               = "slash_command"
	plannerReasonShortReply          = "short_reply"
	plannerReasonConversation        = "conversation"
	plannerReasonUserDirect          = "user_direct"
	plannerReasonUserPlanOnly        = "user_plan_only"
	plannerReasonUserPlanApproval    = "user_plan_for_approval"
	plannerReasonUserPlanAndExecute  = "user_plan_and_execute"
	plannerReasonContextContinuation = "context_continuation"
	plannerReasonLowRiskQuestion     = "low_risk_question"
	plannerReasonHighRisk            = "high_risk"
	plannerReasonCrossSurface        = "cross_surface"
	plannerReasonStructuredRequest   = "structured_request"
	plannerReasonComplexIntent       = "complex_intent"
	plannerReasonAtomicEdit          = "atomic_edit"
	plannerReasonReadOnlyAction      = "read_only_action"
	plannerReasonGuidance            = "complex_guidance"
	plannerReasonGoalActive          = "goal_active"
	plannerReasonAnchoredWork        = "anchored_work"
	plannerReasonAmbiguousWork       = "ambiguous_work"
	plannerReasonWorkRequest         = "work_request"
	plannerReasonTaskPolicy          = "task_policy"
	plannerReasonDefault             = "default_executor"
)

var (
	directOptionReplyRE   = regexp.MustCompile(`(?i)^\s*(?:\d+|[a-z])\s*[.)、。]?\s*$`)
	prefixedOptionReplyRE = regexp.MustCompile(`(?i)^\s*(?:选|选择|就|用|按|走|执行|choose|pick|use|option|choice|方案)\s*(?:第\s*)?(?:方案|选项|option|choice)?\s*(?:\d+|[一二三四五六七八九十]|[a-z])\s*(?:个|号|项|种|条|方案|option|choice)?\s*[.)、。!！?？]?\s*$`)
	plannerListRE         = regexp.MustCompile(`(?m)^\s*(?:[-*]|\d+[.)、])\s+\S`)
	plannerFileRefRE      = regexp.MustCompile(`(?i)(?:^|[\s@` + "`" + `"'(])(?:[\w.-]+[/\\])*[\w.-]+\.(?:go|ts|tsx|js|jsx|py|rs|java|kt|md|json|ya?ml|toml|sql|sh|css|html)(?:$|[\s,;:!?，；：！？)` + "`" + `"'])`)
)

type plannerTurnMetadata struct {
	UserText               string
	Synthetic              bool
	ExplicitPlanMode       bool
	GoalActive             bool
	DeliveryProfile        bool // legacy tests/callers without a frozen TaskPolicy
	HasConversationContext bool
	Policy                 taskpolicy.TaskPolicy
	PolicySet              bool
}

type plannerTurnMetadataKey struct{}

func withPlannerTurnMetadata(ctx context.Context, meta plannerTurnMetadata) context.Context {
	return context.WithValue(ctx, plannerTurnMetadataKey{}, meta)
}

func plannerTurnMetadataFromContext(ctx context.Context) (plannerTurnMetadata, bool) {
	if ctx == nil {
		return plannerTurnMetadata{}, false
	}
	meta, ok := ctx.Value(plannerTurnMetadataKey{}).(plannerTurnMetadata)
	return meta, ok
}

func (c *Controller) withPlannerTurnMetadata(ctx context.Context, userText string, synthetic bool, priorMessages int) context.Context {
	text := strings.TrimSpace(agent.StripTransientUserBlocks(userText))
	features := plannerFeaturesFor(text, normalizePlannerText(text))
	policy := taskpolicy.Derive(taskpolicy.Input{
		Raw:             text,
		Instruction:     taskpolicy.StripQuotedConstraints(text),
		Preset:          agentpreset.Normalize(c.AgentPreset()),
		PlanMode:        c.PlanMode(),
		HighRiskHints:   features.highRisk,
		MediumRiskHints: features.complex,
		MultiFile:       features.multiFile,
		CrossSurface:    features.crossSurface,
		Anchored:        features.anchored,
		Structured:      features.structured,
	})
	ctx = taskpolicy.WithContext(ctx, policy)
	return withPlannerTurnMetadata(ctx, plannerTurnMetadata{
		UserText:               userText,
		Synthetic:              synthetic,
		ExplicitPlanMode:       c.PlanMode(),
		GoalActive:             c.goals.active(),
		DeliveryProfile:        policy.Preset == agentpreset.Delivery,
		HasConversationContext: priorMessages > 1,
		Policy:                 policy,
		PolicySet:              true,
	})
}

// DecidePlannerRoute applies deterministic precedence rules to a pristine user
// turn plus trusted host metadata. It never calls a model and never parses
// controller-injected XML to infer host state.
func DecidePlannerRoute(ctx context.Context, input string) agent.PlannerDecision {
	meta, hasMeta := plannerTurnMetadataFromContext(ctx)
	composedText := strings.TrimSpace(agent.StripTransientUserBlocks(input))
	text := composedText
	if hasMeta && strings.TrimSpace(meta.UserText) != "" {
		text = strings.TrimSpace(meta.UserText)
	}

	if meta.ExplicitPlanMode || strings.HasPrefix(composedText, PlanModeMarker) {
		return plannerExecutorDecision(plannerReasonExplicitPlanMode)
	}
	if meta.Synthetic || IsSyntheticUserMessage(text) {
		return plannerExecutorDecision(plannerReasonSynthetic)
	}
	if text == "" {
		return plannerExecutorDecision(plannerReasonConversation)
	}
	if strings.HasPrefix(text, "/") {
		return plannerExecutorDecision(plannerReasonSlash)
	}
	if isContextDependentShortReply(text) {
		return plannerExecutorDecision(plannerReasonShortReply)
	}
	if isConversationalTurn(text) {
		return plannerExecutorDecision(plannerReasonConversation)
	}

	lower := normalizePlannerText(text)
	if requestsPlanApproval(lower) {
		return plannerPlanDecision(agent.PlannerRoutePlanForApproval, agent.PlannerDepthFull, plannerReasonUserPlanApproval)
	}
	if requestsPlanOnly(lower) {
		return plannerPlanDecision(agent.PlannerRoutePlanOnly, agent.PlannerDepthFull, plannerReasonUserPlanOnly)
	}
	if hasLeadingDirective(lower, planAndExecuteDirectives) || hasLeadingDirective(lower, planFirstDirectives) {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonUserPlanAndExecute)
	}
	if requestsDirectExecution(lower) && (!meta.PolicySet || meta.Policy.Route == taskpolicy.RouteDirect) {
		return plannerExecutorDecision(plannerReasonUserDirect)
	}
	if meta.HasConversationContext && isContextDependentAction(text) {
		return plannerExecutorDecision(plannerReasonContextContinuation)
	}
	if isLowRiskQuestion(lower) {
		return plannerExecutorDecision(plannerReasonLowRiskQuestion)
	}

	features := plannerFeaturesFor(text, lower)
	if meta.PolicySet {
		if meta.GoalActive && features.work && !features.atomic {
			return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonGoalActive)
		}
		switch meta.Policy.Route {
		case taskpolicy.RouteFullPlan:
			return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonTaskPolicy)
		case taskpolicy.RouteLightPlan:
			return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthLight, plannerReasonTaskPolicy)
		default:
			return plannerExecutorDecision(plannerReasonTaskPolicy)
		}
	}
	if features.work && features.highRisk {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonHighRisk)
	}
	if features.multiFile || features.crossSurface {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonCrossSurface)
	}
	if features.structured {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonStructuredRequest)
	}
	if features.complex {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonComplexIntent)
	}
	if features.atomic {
		return plannerExecutorDecision(plannerReasonAtomicEdit)
	}
	if features.guidance {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthLight, plannerReasonGuidance)
	}
	if features.readOnly && !features.ambiguous {
		return plannerExecutorDecision(plannerReasonReadOnlyAction)
	}
	if meta.GoalActive && features.work {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonGoalActive)
	}
	if meta.DeliveryProfile && features.work {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonWorkRequest)
	}
	if features.work && features.ambiguous {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonAmbiguousWork)
	}
	if features.work && features.anchored {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthLight, plannerReasonAnchoredWork)
	}
	if features.work {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthLight, plannerReasonWorkRequest)
	}
	return plannerExecutorDecision(plannerReasonDefault)
}

func plannerExecutorDecision(reason string) agent.PlannerDecision {
	return agent.PlannerDecision{
		Route:  agent.PlannerRouteExecutorOnly,
		Depth:  agent.PlannerDepthNone,
		Reason: reason,
	}
}

func plannerPlanDecision(route agent.PlannerRoute, depth agent.PlannerDepth, reason string) agent.PlannerDecision {
	rounds := plannerLightResearchRounds
	if depth == agent.PlannerDepthFull {
		rounds = plannerFullResearchRounds
	}
	return agent.PlannerDecision{
		Route:             route,
		Depth:             depth,
		Reason:            reason,
		MaxResearchRounds: rounds,
	}
}

type plannerFeatures struct {
	work         bool
	highRisk     bool
	multiFile    bool
	crossSurface bool
	structured   bool
	complex      bool
	atomic       bool
	readOnly     bool
	guidance     bool
	anchored     bool
	ambiguous    bool
}

func plannerFeaturesFor(text, lower string) plannerFeatures {
	fileRefs := plannerFileRefRE.FindAllString(text, -1)
	anchored := len(fileRefs) > 0 || strings.Contains(text, "@") || containsAnyLexical(lower, plannerNamedTargets)
	work := containsAnyLexical(lower, plannerWorkTerms)
	highRisk := containsAnyLexical(lower, plannerHighRiskTerms)
	multiFile := len(fileRefs) >= 2 || strings.Count(text, "@") >= 2
	crossSurface := containsAnyLexical(lower, plannerCrossSurfaceTerms)
	structured := utf8.RuneCountInString(text) >= 240 || plannerListRE.MatchString(text) || strings.Count(text, "\n") >= 2
	complex := containsAnyLexical(lower, complexIntentTerms)
	guidance := isComplexGuidanceQuestion(lower)
	ambiguous := work && containsAnyLexical(lower, plannerAmbiguousScopeTerms)
	readOnly := work && containsAnyLexical(lower, plannerReadOnlyWorkTerms) &&
		!containsAnyLexical(lower, plannerMutationWorkTerms)
	atomic := work && anchored && !highRisk && !multiFile && !crossSurface && !structured && !complex &&
		utf8.RuneCountInString(text) <= 140 && containsAnyLexical(lower, plannerAtomicTerms)
	return plannerFeatures{
		work:         work,
		highRisk:     highRisk,
		multiFile:    multiFile,
		crossSurface: crossSurface,
		structured:   structured,
		complex:      complex,
		atomic:       atomic,
		readOnly:     readOnly,
		guidance:     guidance,
		anchored:     anchored,
		ambiguous:    ambiguous,
	}
}

func normalizePlannerText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "’", "'")
	return text
}

func hasLeadingDirective(lower string, directives []string) bool {
	lower = strings.TrimSpace(lower)
	for _, polite := range []string{"please ", "please, ", "请", "请先", "麻烦", "麻烦先"} {
		if after, ok := strings.CutPrefix(lower, polite); ok {
			lower = strings.TrimSpace(after)
			break
		}
	}
	for _, directive := range directives {
		if strings.HasPrefix(lower, directive) {
			return true
		}
	}
	return false
}

var planAndExecuteDirectives = []string{
	"先规划再执行", "先规划再实现", "先出方案再执行", "先出方案再实现",
	"plan first, then", "plan first then", "plan then implement", "plan and implement",
}

var planFirstDirectives = []string{
	"先规划", "先给方案", "先出方案",
	"plan first", "draft a plan", "give me a plan", "make a plan",
}

var planOnlyDirectives = []string{
	"只规划", "只做规划", "只给方案", "只出方案", "给我方案即可",
	"plan only", "only plan", "just plan", "give me only a plan", "give me a plan only",
}

var planOnlyBoundaryTerms = []string{
	"give me only a plan", "give me a plan only", "only give me the plan",
	"给我方案即可", "只要方案",
}

var plannerNoExecutionTerms = []string{
	"不要执行", "先别执行", "暂不执行", "不要实现", "先别实现", "暂不实现",
	"不要修改", "先别修改", "不要改代码", "先别改代码", "不要动代码",
	"do not execute", "don't execute", "do not implement", "don't implement",
	"do not make changes", "don't make changes", "without executing",
	"without implementation", "no execution", "no implementation",
}

var plannerApprovalTerms = []string{
	"等我确认", "等待我确认", "我确认后", "确认后再",
	"等我批准", "等待我批准", "我批准后", "批准后再",
	"wait for my approval", "wait for approval", "after i approve", "after my approval",
	"until i approve", "until my approval", "let me approve", "let me confirm",
	"after i confirm", "after my confirmation",
}

var directExecutionDirectives = []string{
	"直接改", "直接修改", "直接做", "直接执行", "别规划", "不要规划", "无需规划",
	"just do it", "skip the plan",
}

func requestsPlanOnly(lower string) bool {
	directiveText := plannerDirectiveText(lower)
	if hasLeadingDirective(directiveText, planOnlyDirectives) {
		return true
	}
	if containsAnyLexical(directiveText, planOnlyBoundaryTerms) {
		return true
	}
	if (strings.Contains(directiveText, "只给") || strings.Contains(directiveText, "只要")) &&
		containsAnyLexical(directiveText, plannerIntentTerms) {
		return true
	}
	return containsAnyLexical(directiveText, plannerNoExecutionTerms) &&
		(containsAnyLexical(directiveText, plannerIntentTerms) ||
			containsAnyLexical(directiveText, plannerWorkTerms))
}

func requestsPlanApproval(lower string) bool {
	directiveText := plannerDirectiveText(lower)
	return (containsAnyLexical(directiveText, plannerIntentTerms) ||
		containsAnyLexical(directiveText, plannerWorkTerms)) &&
		containsUnnegatedPlannerApproval(directiveText)
}

func requestsDirectExecution(lower string) bool {
	directiveText := plannerDirectiveText(lower)
	if containsAnyLexical(directiveText, directExecutionDirectives) {
		return true
	}
	for _, term := range []string{"don't plan", "do not plan"} {
		offset := 0
		for offset < len(directiveText) {
			idx := strings.Index(directiveText[offset:], term)
			if idx < 0 {
				break
			}
			idx += offset
			after := strings.TrimSpace(directiveText[idx+len(term):])
			if !strings.HasPrefix(after, "to ") {
				return true
			}
			offset = idx + len(term)
		}
	}
	return false
}

var plannerIntentTerms = []string{
	"plan", "planning", "方案", "规划", "计划",
}

func containsUnnegatedPlannerApproval(text string) bool {
	for _, term := range plannerApprovalTerms {
		offset := 0
		for offset < len(text) {
			idx := strings.Index(text[offset:], term)
			if idx < 0 {
				break
			}
			idx += offset
			if !plannerApprovalNegated(text[:idx]) {
				return true
			}
			offset = idx + len(term)
		}
	}
	return false
}

func plannerApprovalNegated(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	for _, negation := range []string{
		"不要", "不需要", "无需", "无须", "不用", "不必", "别",
		"do not", "don't", "not", "no need to", "do not need to", "don't need to",
		"not necessary to", "without",
	} {
		if strings.HasSuffix(prefix, negation) {
			return true
		}
	}
	return false
}

// plannerDirectiveText removes quoted examples before applying execution
// boundaries. A user explaining "do not execute" or “别规划” is not issuing
// that directive. ASCII apostrophes inside words remain literal, so
// contractions such as don't keep matching the directive tables.
func plannerDirectiveText(text string) string {
	var b strings.Builder
	var closing rune
	escaped := false
	runes := []rune(text)
	for i, r := range runes {
		if closing != 0 {
			if escaped {
				escaped = false
				b.WriteRune(' ')
				continue
			}
			if (closing == '"' || closing == '`') && r == '\\' {
				escaped = true
				b.WriteRune(' ')
				continue
			}
			if r == closing && (closing != '\'' || !plannerInlineApostrophe(runes, i)) {
				closing = 0
			}
			b.WriteRune(' ')
			continue
		}
		switch r {
		case '"':
			closing = '"'
			b.WriteRune(' ')
		case '“':
			closing = '”'
			b.WriteRune(' ')
		case '‘':
			// normalizePlannerText converts the closing ’ to ASCII '.
			closing = '\''
			b.WriteRune(' ')
		case '\'':
			if plannerSingleQuoteStart(runes, i) {
				closing = '\''
				b.WriteRune(' ')
				continue
			}
			b.WriteRune(r)
		case '`':
			closing = '`'
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func plannerSingleQuoteStart(runes []rune, i int) bool {
	if i+1 >= len(runes) || !unicode.IsLetter(runes[i+1]) {
		return false
	}
	return i == 0 || !unicode.IsLetter(runes[i-1]) && !unicode.IsDigit(runes[i-1])
}

func plannerInlineApostrophe(runes []rune, i int) bool {
	return i > 0 && i+1 < len(runes) &&
		(unicode.IsLetter(runes[i-1]) || unicode.IsDigit(runes[i-1])) &&
		(unicode.IsLetter(runes[i+1]) || unicode.IsDigit(runes[i+1]))
}

func isContextDependentAction(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\n\r") || utf8.RuneCountInString(text) > 48 {
		return false
	}
	if plannerFileRefRE.MatchString(text) || strings.Contains(text, "@") {
		return false
	}
	lower := normalizePlannerText(text)
	for _, prefix := range []string{
		"fix it", "fix this", "do it", "apply it", "make that change", "go ahead with it",
		"修一下", "改一下", "按这个改", "照这个做", "执行这个", "就这么改", "修复这个问题",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isConversationalTurn(text string) bool {
	normalized := strings.Trim(strings.ToLower(strings.TrimSpace(text)), " \t\r\n.!?。！？,，;；:：")
	return conversationalTurns[normalized]
}

var conversationalTurns = map[string]bool{
	"hello": true, "hi": true, "hey": true, "thanks": true, "thank you": true,
	"你好": true, "您好": true, "谢谢": true, "辛苦了": true, "收到": true, "明白": true,
}

func isContextDependentShortReply(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\n\r") {
		return false
	}
	if directOptionReplyRE.MatchString(text) || prefixedOptionReplyRE.MatchString(text) {
		return true
	}
	lower := strings.ToLower(text)
	if containsAnyLexical(lower, complexIntentTerms) || containsAnyLexical(lower, plannerWorkTerms) {
		return false
	}
	if shortContextReplies[lower] {
		return true
	}
	if utf8.RuneCountInString(text) > 16 {
		return false
	}
	for _, prefix := range shortContextReplyPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

var shortContextReplies = map[string]bool{
	"ok": true, "okay": true, "yes": true, "y": true, "no": true, "n": true,
	"sure": true, "go ahead": true, "proceed": true, "continue": true, "next": true,
	"sounds good": true, "好": true, "好的": true, "可以": true, "行": true,
	"嗯": true, "对": true, "是": true, "确认": true, "同意": true, "继续": true,
	"继续吧": true, "下一步": true, "开始": true, "开始吧": true, "执行": true,
	"就这样": true, "没问题": true,
}

var shortContextReplyPrefixes = []string{
	"继续", "执行", "开始", "下一步", "go ahead", "proceed", "continue",
}

func isLowRiskQuestion(lower string) bool {
	lower = strings.TrimSpace(lower)
	normalized := strings.ReplaceAll(lower, "'", "")
	if strings.HasPrefix(lower, "what ") || strings.HasPrefix(normalized, "whats ") ||
		strings.HasPrefix(lower, "why ") || strings.HasPrefix(lower, "how ") ||
		strings.HasPrefix(lower, "who ") || strings.HasPrefix(lower, "where ") ||
		strings.HasPrefix(lower, "when ") || strings.HasPrefix(lower, "which ") ||
		strings.HasPrefix(lower, "whose ") || strings.HasPrefix(lower, "whom ") ||
		strings.HasPrefix(lower, "explain ") || strings.HasPrefix(lower, "describe ") ||
		strings.HasPrefix(lower, "tell ") || strings.HasPrefix(lower, "show ") ||
		strings.HasPrefix(lower, "list ") || strings.HasPrefix(lower, "summarize ") ||
		strings.HasPrefix(lower, "summarise ") || strings.HasPrefix(lower, "compare ") ||
		strings.HasPrefix(lower, "difference ") || strings.HasPrefix(lower, "is ") ||
		strings.HasPrefix(lower, "are ") || strings.HasPrefix(lower, "can ") ||
		strings.HasPrefix(lower, "could ") || strings.HasPrefix(lower, "do ") ||
		strings.HasPrefix(lower, "does ") || strings.HasPrefix(lower, "did ") ||
		strings.HasPrefix(lower, "should ") || strings.HasPrefix(lower, "would ") ||
		strings.HasPrefix(lower, "will ") ||
		strings.HasPrefix(lower, "what's") || strings.HasPrefix(normalized, "whats") ||
		strings.HasPrefix(lower, "解释") || strings.HasPrefix(lower, "说明") ||
		strings.HasPrefix(lower, "怎么看") || strings.HasPrefix(lower, "查一下") ||
		strings.HasPrefix(lower, "介绍一下") ||
		strings.HasPrefix(lower, "说一下") || strings.HasPrefix(lower, "帮我看") ||
		strings.HasPrefix(lower, "帮我查") || strings.HasPrefix(lower, "是什么") ||
		strings.HasPrefix(lower, "有没有") || strings.HasPrefix(lower, "能不能") ||
		strings.HasPrefix(lower, "可以吗") || strings.HasPrefix(lower, "对吗") ||
		strings.HasPrefix(lower, "是不是") || strings.HasPrefix(lower, "请问") {
		return !containsAnyLexical(lower, plannerQuestionWorkTerms)
	}
	return false
}

func isComplexGuidanceQuestion(lower string) bool {
	for _, prefix := range []string{
		"how do i ", "how should i ", "how would you ", "what's the best way ",
		"what is the best way ", "explain how to ", "怎么实现", "如何实现", "怎么迁移", "如何迁移",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func containsAnyLexical(s string, terms []string) bool {
	for _, term := range terms {
		if containsLexicalTerm(s, term) {
			return true
		}
	}
	return false
}

func containsLexicalTerm(s, term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	if containsNonASCII(term) || strings.ContainsAny(term, " -_/") {
		return strings.Contains(s, term)
	}
	return slices.Contains(strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}), term)
}

func containsNonASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}

var complexIntentTerms = []string{
	"refactor", "migrate", "migration", "redesign", "end-to-end", "e2e", "wire up",
	"integration", "architecture", "release", "package", "重构", "迁移", "改造",
	"端到端", "联调", "接入", "架构", "发布", "打包",
}

var plannerWorkTerms = []string{
	"fix", "fixing", "update", "updating", "remove", "removing", "delete", "deleting",
	"edit", "editing", "write", "writing", "create", "creating", "add", "adding", "repair",
	"patch", "run", "running", "build", "building", "implement", "implementing", "refactor",
	"refactoring", "migrate", "migrating", "redesign", "review", "reviewing", "audit",
	"inspect", "debug", "test", "tests", "testing", "修改", "修复", "更新", "删除", "移除",
	"编辑", "写入", "创建", "新增", "添加", "运行", "构建", "实现", "重构", "迁移",
	"改造", "评审", "审查", "排查", "调试", "测试", "加个", "加一", "补一个", "补个",
}

var plannerMutationWorkTerms = []string{
	"fix", "fixing", "update", "updating", "remove", "removing", "delete", "deleting",
	"edit", "editing", "write", "writing", "create", "creating", "add", "adding", "repair",
	"patch", "build", "building", "implement", "implementing", "refactor", "refactoring",
	"migrate", "migrating", "redesign", "修改", "修复", "更新", "删除", "移除", "编辑",
	"写入", "创建", "新增", "添加", "构建", "实现", "重构", "迁移", "改造", "加个",
	"加一", "补一个", "补个",
}

var plannerReadOnlyWorkTerms = []string{
	"run", "running", "review", "reviewing", "audit", "inspect", "debug",
	"test", "tests", "testing", "运行", "评审", "审查", "排查", "调试", "测试",
}

var plannerQuestionWorkTerms = []string{
	"fix", "fixing", "update", "updating", "remove", "removing", "delete", "deleting",
	"edit", "editing", "write", "writing", "create", "creating", "add", "adding", "repair",
	"patch", "implement", "implementing", "refactor", "refactoring", "migrate", "migrating",
	"redesign", "修改", "修复", "更新", "删除", "移除", "编辑", "写入", "创建", "新增",
	"添加", "实现", "重构", "迁移", "改造", "加个", "加一", "补一个", "补个",
}

var plannerHighRiskTerms = []string{
	"auth", "authentication", "authorization", "permission", "token", "secret",
	"credential", "payment", "billing", "race", "concurrency", "deadlock", "transaction",
	"encryption", "signature", "sandbox", "privilege", "权限", "鉴权", "认证", "令牌",
	"密钥", "支付", "账单", "并发", "竞态", "竟态", "死锁", "事务", "加密", "签名",
	"沙箱", "提权",
}

var plannerCrossSurfaceTerms = []string{
	"multiple files", "several files", "across", "frontend and backend", "backend and frontend",
	"api and ui", "ui and api", "database and api", "多个文件", "多处", "前后端",
	"整个模块", "整个项目", "全链路", "跨模块",
}

var plannerAtomicTerms = []string{
	"typo", "wording", "copy", "readme", "changelog", "nil check", "null check",
	"log line", "one line", "rename", "文案", "错别字", "拼写", "空指针检查",
	"nil 检查", "一行日志", "改名", "重命名",
}

var plannerNamedTargets = []string{
	"readme", "changelog", "makefile", "dockerfile",
}

var plannerAmbiguousScopeTerms = []string{
	"the bug", "the issue", "the problem", "performance", "everything", "whole module",
	"这个 bug", "这个bug", "这个问题", "性能", "整个模块", "全部问题",
}

// TaskWarrantsPlanner is retained as a small compatibility predicate for
// callers and tests that do not need depth or approval semantics.
func TaskWarrantsPlanner(input string) bool {
	return DecidePlannerRoute(context.Background(), input).Route != agent.PlannerRouteExecutorOnly
}

// NewPlannerPolicy returns the structured deterministic policy used by the
// two-model product path.
func NewPlannerPolicy() agent.PlannerPolicy {
	return DecidePlannerRoute
}

// NewPlannerGate retains the historical bool shape for direct callers.
func NewPlannerGate() func(context.Context, string) bool {
	return func(ctx context.Context, input string) bool {
		return DecidePlannerRoute(ctx, input).Route != agent.PlannerRouteExecutorOnly
	}
}

package control

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/nilutil"
)

const (
	autoPlanOff = "off"
	autoPlanOn  = "on"
)

var numberedListRE = regexp.MustCompile(`(?m)^\s*(?:[-*]|\d+[.)])\s+\S`)
var directOptionReplyRE = regexp.MustCompile(`(?i)^\s*(?:\d+|[a-z])\s*[.)、。]?\s*$`)
var prefixedOptionReplyRE = regexp.MustCompile(`(?i)^\s*(?:选|选择|就|用|按|走|执行|choose|pick|use|option|choice|方案)\s*(?:第\s*)?(?:方案|选项|option|choice)?\s*(?:\d+|[一二三四五六七八九十]|[a-z])\s*(?:个|号|项|种|条|方案|option|choice)?\s*[.)、。!！?？]?\s*$`)

type AutoPlanClassifier interface {
	NeedsPlan(ctx context.Context, input string, score int) (bool, string, error)
}

type autoPlanClassifier = AutoPlanClassifier

func normalizeAutoPlan(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case autoPlanOn, "ask": // "ask" is a legacy synonym for on.
		return autoPlanOn
	case "", autoPlanOff:
		return autoPlanOff
	default:
		return autoPlanOff
	}
}

func (c *Controller) maybeAutoPlan(ctx context.Context, input string) {
	if c.shouldAutoPlan(ctx, input) {
		c.SetPlanMode(true)
		c.noticeDetail("Planning mode enabled for this multi-step task.", "auto plan: task looks multi-step; drafting a plan first")
	}
}

func (c *Controller) shouldAutoPlan(ctx context.Context, input string) bool {
	c.mu.Lock()
	mode := c.autoPlan
	plan := c.planMode
	classifier := c.classifier
	c.mu.Unlock()
	if mode == autoPlanOff || plan || c.goals.active() {
		return false
	}
	score := autoPlanScore(input)
	if score <= 0 {
		return false
	}
	if classifier != nil && score <= 2 {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		needsPlan, reason, err := classifier.NeedsPlan(ctx, input, score)
		if err == nil {
			if needsPlan && reason != "" {
				c.noticeDetail("Plan detection requested a plan.", "auto plan classifier: "+reason)
			}
			return needsPlan
		}
		c.noticeDetail("Plan detection was uncertain; using the fallback planner heuristic.", "auto plan classifier failed; falling back to heuristic: "+err.Error())
	}
	return score >= 2
}

// TaskWarrantsPlanner reports whether a task turn is worth a planner pass in
// two-model mode. Empty input, slash commands, and low-risk informational asks
// (explain / show / what / why / 解释 / 查一下 …) skip straight to the executor.
// Context-dependent short replies ("1", "A", "继续", "好的") also skip the
// planner because the executor session, not the planner session, owns the
// previous assistant answer they refer to.
func TaskWarrantsPlanner(input string) bool {
	text := strings.TrimSpace(agent.StripTransientUserBlocks(input))
	text = stripActiveGoalBlock(text)
	if text == "" || strings.HasPrefix(text, "/") || strings.HasPrefix(text, PlanModeMarker) {
		return false
	}
	if IsSyntheticUserMessage(text) {
		return false
	}
	if isContextDependentShortReply(text) {
		return false
	}
	return !isLowRiskQuestion(strings.ToLower(text))
}

func autoPlanScore(input string) int {
	text := strings.TrimSpace(input)
	text = stripActiveGoalBlock(text)
	if text == "" || strings.HasPrefix(text, "/") || strings.HasPrefix(text, PlanModeMarker) {
		return 0
	}
	if IsSyntheticUserMessage(text) {
		return 0
	}
	if isContextDependentShortReply(text) {
		return 0
	}
	lower := strings.ToLower(text)
	if isLowRiskQuestion(lower) {
		return 0
	}

	score := 0
	if utf8.RuneCountInString(text) >= 160 {
		score++
	}
	if numberedListRE.MatchString(text) {
		score++
	}
	if strings.Count(text, "\n") >= 2 {
		score++
	}
	if containsAny(lower, complexIntentTerms) {
		score++
	}
	if containsAny(lower, multiSurfaceTerms) {
		score++
	}
	if containsAny(lower, docsAndIssueTerms) {
		score++
	}
	if strings.Count(text, "@") >= 2 || strings.Count(lower, ".go")+
		strings.Count(lower, ".ts")+strings.Count(lower, ".tsx")+strings.Count(lower, ".js") >= 2 {
		score++
	}
	return score
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
	if containsAny(lower, complexIntentTerms) || containsAny(lower, lowRiskWorkRequestTerms) {
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
	"ok":          true,
	"okay":        true,
	"yes":         true,
	"y":           true,
	"no":          true,
	"n":           true,
	"sure":        true,
	"go ahead":    true,
	"proceed":     true,
	"continue":    true,
	"next":        true,
	"sounds good": true,
	"好":           true,
	"好的":          true,
	"可以":          true,
	"行":           true,
	"嗯":           true,
	"对":           true,
	"是":           true,
	"确认":          true,
	"同意":          true,
	"继续":          true,
	"继续吧":         true,
	"下一步":         true,
	"开始":          true,
	"开始吧":         true,
	"执行":          true,
	"就这样":         true,
	"没问题":         true,
}

var shortContextReplyPrefixes = []string{
	"继续",
	"执行",
	"开始",
	"下一步",
	"go ahead",
	"proceed",
	"continue",
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
		strings.HasPrefix(lower, "will ") || strings.HasPrefix(lower, "run ") ||
		strings.HasPrefix(lower, "what's") || strings.HasPrefix(normalized, "whats") ||
		strings.HasPrefix(lower, "解释") || strings.HasPrefix(lower, "说明") ||
		strings.HasPrefix(lower, "怎么看") || strings.HasPrefix(lower, "查一下") ||
		strings.HasPrefix(lower, "运行") || strings.HasPrefix(lower, "介绍一下") ||
		strings.HasPrefix(lower, "说一下") || strings.HasPrefix(lower, "帮我看") ||
		strings.HasPrefix(lower, "帮我查") || strings.HasPrefix(lower, "是什么") ||
		strings.HasPrefix(lower, "有没有") || strings.HasPrefix(lower, "能不能") ||
		strings.HasPrefix(lower, "可以吗") || strings.HasPrefix(lower, "对吗") ||
		strings.HasPrefix(lower, "是不是") || strings.HasPrefix(lower, "请问") {
		return !containsAny(lower, complexIntentTerms) && !containsAny(lower, lowRiskWorkRequestTerms)
	}
	return false
}

func stripActiveGoalBlock(text string) string {
	const open = "<active-goal>"
	const close = "</active-goal>"
	if !strings.Contains(text, open) {
		return text
	}
	end := strings.Index(text, close)
	if end < 0 {
		return text
	}
	after := strings.TrimSpace(text[end+len(close):])
	if after == "" {
		return text
	}
	return after
}

func containsAny(s string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

var complexIntentTerms = []string{
	"implement", "add support", "refactor", "migrate", "redesign", "end-to-end",
	"e2e", "wire up", "integration", "fix the issue", "build a",
	"实现", "新增", "支持", "重构", "迁移", "改造", "端到端", "联调", "接入",
	"修复这个问题", "修一下这个问题", "补齐", "设计",
}

var lowRiskWorkRequestTerms = []string{
	"fix", "update", "remove", "delete", "edit", "write", "create", "add ",
	"repair", "patch", "run ", "build", "修改", "修复", "更新", "删除", "移除",
	"编辑", "写入", "创建", "新增", "运行", "构建",
}

var multiSurfaceTerms = []string{
	"multiple files", "several files", "across", "frontend", "backend", "config",
	"tests", "docs", "ui", "api", "database", "schema",
	"多个文件", "多处", "前端", "后端", "配置", "测试", "文档", "接口", "数据库",
}

var docsAndIssueTerms = []string{
	"prd", "issue", "requirements", "spec", "proposal", "roadmap",
	"需求", "产品文档", "接口文档", "方案", "规划",
}

// NewPlannerGate builds the per-turn planner gate for two-model mode. The turn
// context is threaded into the classifier call so a cancelled turn stops the
// borderline check immediately instead of letting it run out its own timeout.
func NewPlannerGate(classifier AutoPlanClassifier) func(context.Context, string) bool {
	if nilutil.IsNil(classifier) {
		return func(_ context.Context, input string) bool {
			return TaskWarrantsPlanner(input)
		}
	}
	return func(ctx context.Context, input string) bool {
		if !TaskWarrantsPlanner(input) {
			return false
		}
		score := autoPlanScore(input)
		if score <= 2 {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			needsPlan, _, err := classifier.NeedsPlan(ctx, input, score)
			if err == nil {
				return needsPlan
			}
		}
		return true
	}
}

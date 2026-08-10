package taskintent

import "strings"

// Goal turn-budget classes. Quotas are fixed; classes never gate permissions.
const (
	BudgetClassSimple   = "simple"
	BudgetClassWrite    = "write"
	BudgetClassResearch = "research"
)

// BudgetTurns returns the default turn quota for a Goal budget class.
func BudgetTurns(class string) int {
	switch class {
	case BudgetClassResearch:
		return 40
	case BudgetClassWrite:
		return 20
	default:
		return 10
	}
}

// ClassifyGoalBudget selects simple/write/research from goal text alone.
// Legacy CLI flags and sidecars apply on/off overrides in the control package.
func ClassifyGoalBudget(goal string) string {
	if needsResearchBudget(goal) {
		return BudgetClassResearch
	}
	if GoalNeedsWriteBudget(goal) {
		return BudgetClassWrite
	}
	return BudgetClassSimple
}

func needsResearchBudget(goal string) bool {
	trimmed := strings.TrimSpace(goal)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, ".reasonix/autoresearch/") {
		return true
	}
	for _, kw := range researchBudgetStrongKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return researchBudgetPhaseCount(lower) >= 4
}

func researchBudgetPhaseCount(lower string) int {
	categories := 0
	for _, group := range researchBudgetPhaseKeywords {
		if containsAnyGoalKeyword(lower, group) {
			categories++
		}
	}
	return categories
}

func containsAnyGoalKeyword(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

var researchBudgetStrongKeywords = []string{
	"持续", "长期", "彻底", "直到根因", "根因明确", "多轮",
	"不要原地打转", "别原地打转", "完整方案", "完整做成方案",
	"跑实验", "反复验证", "长期优化", "系统性研究", "持续研究",
	"持续排查", "持续推进", "长期跑",
	"long-horizon", "long horizon", "long-running", "keep researching",
	"keep working", "root cause", "until the root cause", "do not spin",
	"don't spin", "thoroughly", "systematically",
}

var researchBudgetPhaseKeywords = [][]string{
	{"研究", "调研", "排查", "分析", "定位", "诊断", "research", "investigate", "diagnose", "analyze", "analysis"},
	{"实现", "修复", "改造", "开发", "重构", "implement", "build", "fix", "refactor"},
	{"验证", "测试", "复现", "联调", "benchmark", "verify", "validate", "test", "reproduce"},
	{"优化", "完善", "提升", "收敛", "optimize", "improve", "tune", "polish"},
	{"文档", "方案", "说明", "总结", "document", "docs", "writeup", "plan"},
	{"发布", "上线", "提交", "pull request", "publish", "ship", "deploy"},
}

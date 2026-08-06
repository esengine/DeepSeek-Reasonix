package taskintent

import (
	"strings"
)

// taskFaultSignals are CN/EN failure/fault phrases shared by task recognition
// and Goal budget classification so the two surfaces cannot drift apart.
var taskFaultSignals = []string{
	"not working", "isn't working", "doesn't work", "dont work", "don't work",
	"broken", "error", "bug", "issue", "failed", "failing", "crash", "exception", "panic", "segfault",
	"问题", "不工作", "报错", "错误", "失败", "崩溃", "异常",
	"卡住", "卡住了", "没反应", "不生效", "异常退出", "历史 bug", "历史bug",
}

// taskFaultReadonlySignals mark diagnosis/analysis-only work. A bare fault
// statement without these (and without advisory/no-modify constraints) is
// treated as a Goal write budget.
var taskFaultReadonlySignals = []string{
	"diagnose", "diagnosis", "analyze", "analyse", "inspect", "review", "reproduce",
	"identify the root cause", "root cause", "investigate", "audit", "verify", "check why",
	"诊断", "排查", "定位", "分析", "检查", "复现", "评审", "审查", "审计", "验证",
	"原因", "根因",
}

// GoalNeedsWriteBudget reports whether a Goal objective should start on the
// extended write turn budget. It preserves every explicit mutation classification used by
// ordinary Delivery, then additionally treats bare fault/bug/crash statements as
// write work unless the user asked only for analysis, explanation, or a no-modify
// constraint. Ordinary Delivery evidence/mutation gates keep using
// NeedsMutation / Classify unchanged.
func GoalNeedsWriteBudget(input string) bool {
	if NeedsMutation(input) {
		return true
	}
	return goalBareFaultImpliesWrite(input)
}

func goalBareFaultImpliesWrite(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" || !taskInputHasFaultSignal(normalized) {
		return false
	}
	// Explicit "do not modify/fix" (or equivalent) keeps the goal on simple.
	if goalHasNoModifyConstraint(normalized) {
		return false
	}
	// Questions and explanation intent stay consultative.
	if deliveryTaskIsAdvisory(normalized) || goalHasQuestionIntent(normalized) {
		return false
	}
	// Explicit read-only diagnostic wording stays on the simple budget even when
	// it mentions a bug/crash/failure.
	if goalHasReadonlyDiagnosticIntent(normalized) {
		return false
	}
	return true
}

func taskInputHasFaultSignal(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	for _, phrase := range taskFaultSignals {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func goalHasQuestionIntent(input string) bool {
	if strings.ContainsAny(input, "?？") {
		return true
	}
	// Explanation/consultative stems beyond the shared advisory phrase list.
	for _, phrase := range []string{
		"explain", "what happened", "what's happening", "what is happening",
		"tell me about", "look into why",
		"解释", "说明一下", "怎么回事", "是什么情况", "什么情况",
	} {
		if strings.Contains(input, phrase) {
			return true
		}
	}
	return false
}

func goalHasReadonlyDiagnosticIntent(input string) bool {
	for _, phrase := range taskFaultReadonlySignals {
		if strings.Contains(input, phrase) {
			return true
		}
	}
	return deliveryTaskIsAdvisory(input)
}

func goalHasNoModifyConstraint(input string) bool {
	if _, negated := deliveryTaskMutationIntent(input); negated && !deliveryTaskHasMutationIntent(input) {
		return true
	}
	for _, phrase := range []string{
		"do not fix", "don't fix", "dont fix", "do not change", "don't change", "dont change",
		"do not modify", "don't modify", "dont modify", "do not edit", "don't edit",
		"without fixing", "without changing", "without modifying", "analysis only", "review only",
		"不要修复", "不要修改", "不要改动", "不要改", "别修复", "别修改", "勿修改",
		"只分析", "仅分析", "只检查", "仅检查", "只诊断", "仅诊断", "只定位", "仅定位",
		"只排查", "仅排查", "不要修",
	} {
		if strings.Contains(input, phrase) {
			return true
		}
	}
	// Trailing constraints after punctuation, e.g. "复现并定位问题，但不要修复。"
	for _, clause := range deliveryTaskClauses(input) {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		for _, phrase := range []string{"不要修复", "不要修改", "do not fix", "don't fix", "dont fix"} {
			if strings.Contains(clause, phrase) {
				return true
			}
		}
	}
	return false
}

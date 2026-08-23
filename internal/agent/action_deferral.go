package agent

import (
	"strings"
	"unicode"
)

// looksLikePendingAction recognizes a narrow class of final answers that
// explicitly promise another local action. It is only a control hint; tool
// receipts and structured todos remain the authoritative completion facts.
func looksLikePendingAction(answer string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return false
	}
	for _, clause := range strings.FieldsFunc(answer, pendingActionClauseBoundary) {
		clause = strings.TrimSpace(clause)
		if clause == "" || containsAnySubstring(clause, pendingActionOptionalTerms) {
			continue
		}
		if containsAnySubstring(clause, pendingActionCommitmentTerms) &&
			containsPendingActionVerb(clause) {
			return true
		}
	}
	return false
}

func containsPendingActionVerb(clause string) bool {
	for _, token := range strings.FieldsFunc(clause, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		if pendingActionEnglishVerbs[token] {
			return true
		}
	}
	return containsAnySubstring(clause, pendingActionChineseVerbs)
}

func pendingActionClauseBoundary(r rune) bool {
	switch r {
	case '\n', '\r', '.', '!', '?', ';', ',', '\u3002', '\uff01', '\uff1f', '\uff1b', '\uff0c':
		return true
	default:
		return false
	}
}

var pendingActionCommitmentTerms = []string{
	"i will", "i'll", "i am going to", "i'm going to", "let me",
	"now i", "starting with", "start with",
	"continue by", "接下来", "下一步", "然后", "随后", "我会", "我将", "我先",
	"我接着", "我从", "立即", "马上", "现在开始", "先读", "先修改", "开始执行",
}

var pendingActionEnglishVerbs = map[string]bool{
	"continue": true, "proceed": true, "start": true, "begin": true,
	"read": true, "reading": true, "inspect": true, "inspecting": true, "open": true,
	"edit": true, "editing": true, "modify": true, "modifying": true, "update": true, "updating": true,
	"write": true, "writing": true, "implement": true, "implementing": true, "fix": true, "patch": true,
	"run": true, "running": true, "test": true, "testing": true, "verify": true, "verifying": true,
	"build": true, "building": true, "create": true, "creating": true, "remove": true, "delete": true,
	"read_file": true, "write_file": true, "edit_file": true,
}

var pendingActionChineseVerbs = []string{
	"继续", "开始", "读取", "读", "检查", "打开", "编辑", "修改", "更新", "写", "实现",
	"修复", "执行", "运行", "测试", "验证", "构建", "创建", "删除", "移除",
}

var pendingActionOptionalTerms = []string{
	"no need", "not needed", "do not", "don't", "will not", "won't", "not going to",
	"recommend", "suggest", "optional", "could ", "you can", "the user can", "if needed",
	"if you want", "future work", "无需", "不需要", "不要", "不会", "不再", "建议", "可选",
	"可以考虑", "你可以", "用户可以", "如需", "如果需要", "后续可",
}

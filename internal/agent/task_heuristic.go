package agent

import "strings"

// heuristicInputIsTask reports whether a user input reads as an actionable
// task rather than conversational chat. The delivery evidence gate uses it to
// decide when a turn should be held to acceptance-criteria expectations
// (deliveryTaskNeedsEvidence); greetings and acknowledgements must not arm the
// delivery gates.
func heuristicInputIsTask(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false
	}

	normalized := strings.ToLower(strings.Trim(trimmed, " \t\r\n.!?。！？,，;；:："))

	// Very short greeting/acknowledgement whitelist (1-3 words).
	shortGreetings := []string{
		"hello", "hi", "hey", "你好", "您好", "nihao",
		"thanks", "thank you", "谢谢", "谢了",
		"ok", "okay", "好的", "嗯", "行",
		"got it", "i see", "明白", "了解", "收到", "我知道了", "先不用",
	}

	words := strings.Fields(normalized)
	if len(words) <= 3 {
		for _, greeting := range shortGreetings {
			if normalized == greeting {
				return false
			}
		}
	}

	// Polite acknowledgements can contain action words from the completed
	// task ("thanks for fixing") but should stay conversational.
	chatPhrases := []string{
		"thanks for", "thank you for", "i'll check later", "i will check later",
		"i'll test it later", "i will test it later", "that test was helpful", "the test was helpful",
		"谢谢你", "辛苦了",
	}
	for _, phrase := range chatPhrases {
		if strings.Contains(normalized, phrase) {
			return false
		}
	}

	// File references are a strong task signal.
	if strings.Contains(trimmed, "@") || strings.Contains(trimmed, ".go") ||
		strings.Contains(trimmed, ".js") || strings.Contains(trimmed, ".py") ||
		strings.Contains(trimmed, ".ts") {
		return true
	}

	// Failure/help descriptions are actionable even when phrased without an
	// imperative verb, e.g. "the auth isn't working".
	taskPhrases := []string{
		"not working", "isn't working", "doesn't work", "dont work", "don't work",
		"can you help", "help with", "broken", "error", "bug", "issue", "failed", "failing", "crash", "cannot", "can't",
		"问题", "不工作", "无法", "不能", "报错", "错误", "失败", "崩溃", "异常",
		"卡住", "卡住了", "没反应", "不生效", "异常退出",
	}
	for _, phrase := range taskPhrases {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}

	// Action keyword detection.
	actionNeedles := []string{
		"fix", "debug", "repair", "resolve", "reproduce",
		"create", "add", "write", "edit", "update", "change", "delete", "remove", "rename",
		"review", "inspect", "analyze", "check", "test", "run", "build", "implement", "refactor",
		"continue work", "continue the", "continue this",
		"修复", "调试", "解决", "复现", "创建", "新建", "添加", "编写", "编辑", "修改", "更新",
		"删除", "移除", "重命名", "评审", "检查", "分析", "测试", "运行", "构建", "实现", "重构", "继续处理",
		"看看", "看下", "帮我看", "帮我看下", "处理下", "处理一下", "排查", "定位",
	}

	for _, needle := range actionNeedles {
		if containsTaskNeedle(normalized, needle) {
			return true
		}
	}

	// Default for ambiguous input: a false negative (task treated as chat)
	// disarms the delivery gates, which is worse than a false positive, so
	// short ambiguous inputs read as chat and longer ones as tasks.
	return len(words) > 5
}

func containsTaskNeedle(input, needle string) bool {
	if needle == "" {
		return false
	}
	if containsNonASCII(needle) || strings.Contains(needle, " ") {
		return strings.Contains(input, needle)
	}
	for _, word := range strings.FieldsFunc(input, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_'
	}) {
		if word == needle {
			return true
		}
	}
	return false
}

func containsNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

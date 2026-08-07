package taskintent

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// heuristicInputIsTask reports whether a user input reads as an actionable
// task rather than conversational chat. The delivery evidence gate uses it to
// decide when a turn should be held to acceptance-criteria expectations
// (NeedsEvidence); greetings and acknowledgements must not arm the
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
		if slices.Contains(shortGreetings, normalized) {
			return false
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
		before, after, ok := strings.Cut(normalized, phrase)
		if !ok {
			continue
		}
		// Acknowledgement wording only short-circuits a purely conversational
		// turn. Preserve a real task before it or after an explicit transition,
		// e.g. "thanks for fixing that; now update the tests".
		if prefix := strings.TrimSpace(before); prefix != "" && heuristicInputHasStrongTaskSignal(prefix) {
			return true
		}
		if deliveryTaskHasFollowUpAfterChat(after) {
			return true
		}
		return false
	}
	// Ambiguous prose stays conversational. Delivery evidence gates require a
	// concrete host-observable signal rather than using message length as a
	// proxy; explicit mutations, files, commands, failures, and audit verbs are
	// still classified below.
	return heuristicInputHasStrongTaskSignal(normalized)
}

func heuristicInputHasStrongTaskSignal(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	// File references and concrete commands are strong task signals. Shared
	// parsing keeps email addresses and remote product names from accidentally
	// arming the delivery gate while covering ordinary repository file types.
	if deliveryTaskHasFileReference(normalized) || deliveryTaskHasCommand(normalized) {
		return true
	}
	// Mutation intent has a richer, negation-aware vocabulary than this generic
	// task heuristic. Reuse it so short requests such as "push the branch" do not
	// bypass delivery gates merely because the two keyword lists drift apart.
	if deliveryTaskHasMutationIntent(normalized) || NeedsPersistentAction(normalized) {
		return true
	}

	// Failure/help descriptions are actionable even when phrased without an
	// imperative verb, e.g. "the auth isn't working". Shared fault signals keep
	// task recognition and Goal budget classification from drifting apart.
	if taskInputHasFaultSignal(normalized) {
		return true
	}
	helpPhrases := []string{
		"can you help", "help with", "cannot", "can't",
		"无法", "不能",
	}
	for _, phrase := range helpPhrases {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}

	// Action keyword detection.
	actionNeedles := []string{
		"fix", "debug", "repair", "resolve", "reproduce",
		"create", "add", "write", "edit", "update", "change", "delete", "remove", "rename",
		"review", "inspect", "analyze", "check", "audit", "verify", "test", "run", "build", "implement", "refactor", "modify", "patch", "replace",
		"configure", "upgrade", "downgrade", "enable", "disable", "merge", "make changes", "make a change", "make the changes",
		"make the requested changes", "make the necessary changes", "make these changes", "make those changes", "make code changes",
		"continue work", "continue the", "continue this",
		"修复", "调试", "解决", "复现", "创建", "新建", "添加", "编写", "编辑", "修改", "更新",
		"删除", "移除", "重命名", "评审", "检查", "分析", "审计", "验证", "测试", "运行", "构建", "实现", "重构", "继续处理",
		"调整", "替换", "移动", "升级", "降级", "启用", "禁用", "合并", "改动", "打补丁",
		"看看", "看下", "帮我看", "帮我看下", "处理下", "处理一下", "排查", "定位",
	}

	for _, needle := range actionNeedles {
		if containsTaskNeedle(normalized, needle) {
			return true
		}
	}

	return false
}

func deliveryTaskHasFollowUpAfterChat(input string) bool {
	for index, current := range input {
		switch current {
		case '.', ',', ';', '!', '?', '。', '，', '；', '！', '？':
			candidate := strings.TrimSpace(input[index+utf8.RuneLen(current):])
			if candidate != "" && heuristicInputHasStrongTaskSignal(candidate) {
				return true
			}
		}
	}
	for _, cue := range []string{
		" but ", " however ", " nevertheless ", " now ", " then ", " and ", " please ", " so ", " therefore ",
		"但是", "但请", "不过", "现在", "然后", "所以", "请", "继续", "再",
	} {
		for rest := input; ; {
			index := strings.Index(rest, cue)
			if index < 0 {
				break
			}
			candidate := strings.TrimSpace(rest[index+len(cue):])
			if candidate != "" && heuristicInputHasStrongTaskSignal(candidate) {
				return true
			}
			rest = rest[index+len(cue):]
		}
	}
	return false
}

func containsTaskNeedle(input, needle string) bool {
	if needle == "" {
		return false
	}
	if containsNonASCII(needle) || strings.Contains(needle, " ") {
		return strings.Contains(input, needle)
	}
	return slices.Contains(strings.FieldsFunc(input, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_'
	}), needle)
}

func containsNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

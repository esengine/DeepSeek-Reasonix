package runtimepolicy

import (
	"regexp"
	"strings"

	"reasonix/internal/shellparse"
)

// Constraints are explicit user or host limits. They never encode task
// complexity, security keywords, or file counts.
type Constraints struct {
	ForbidMutation          bool
	ForbidTests             bool
	AllowedChecks           []string
	ForbidExternal          bool
	RequireFullVerification bool
	PlanModeReadOnly        bool
	Notes                   []string
}

// ParseConstraints accepts only explicit forbid/limit phrasing.
func ParseConstraints(instruction string) Constraints {
	var c Constraints
	lower := strings.ToLower(instruction)
	if hasGlobalMutationBan(lower) {
		c.ForbidMutation = true
		c.Notes = append(c.Notes, "user_forbid_mutation")
	}
	if matchesAny(lower, []string{
		"不要测试", "别跑测试", "不用测试", "跳过测试", "不要跑测试",
		"don't run tests", "do not run tests", "no tests", "skip tests",
		"without tests", "don't test", "do not test",
	}) {
		c.ForbidTests = true
		c.Notes = append(c.Notes, "user_forbid_tests")
	}
	if matchesAny(lower, []string{
		"完整验证", "全面验证", "闭环交付", "完整交付", "交付前检查", "验收闭环",
		"full verification", "complete verification", "verify everything",
		"closed-loop delivery", "deliver with verification",
	}) {
		c.RequireFullVerification = true
		c.Notes = append(c.Notes, "user_require_full_verification")
	}
	if cmds := parseAllowedChecks(instruction); len(cmds) > 0 {
		c.AllowedChecks = cmds
		c.Notes = append(c.Notes, "user_allowed_checks")
	}
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

// hasGlobalMutationBan distinguishes a turn-wide read-only instruction from a
// scoped protection such as "do not change any config". The latter still lets
// the requested output or an unrelated implementation target be written.
func hasGlobalMutationBan(instruction string) bool {
	for _, clause := range mutationConstraintClauses(instruction) {
		clause = strings.TrimSpace(strings.TrimLeft(clause, "-*•0123456789. )\t"))
		if clause == "" {
			continue
		}
		if hasExplicitReadOnlyClause(clause) || hasGlobalNegatedMutationClause(clause) {
			return true
		}
	}
	return false
}

func mutationConstraintClauses(instruction string) []string {
	return strings.FieldsFunc(instruction, func(r rune) bool {
		switch r {
		case '\n', '\r', '.', '!', '?', ';', '。', '！', '？', '；':
			return true
		default:
			return false
		}
	})
}

func hasExplicitReadOnlyClause(clause string) bool {
	if hasMutationContinuation(clause) {
		return false
	}
	for _, phrase := range []string{
		"analyze only", "analysis only", "read-only review", "read only review",
		"reproduce only", "reproduce but don't fix", "reproduce but do not fix",
		"只分析", "仅分析", "只看不改", "复现但不修复", "只复现", "仅复现",
	} {
		if strings.Contains(clause, phrase) {
			return true
		}
	}
	trimmed := strings.TrimSpace(clause)
	return trimmed == "read-only" || strings.HasPrefix(trimmed, "read-only ") ||
		trimmed == "read only" || strings.HasPrefix(trimmed, "read only ") ||
		trimmed == "只读" || strings.HasPrefix(trimmed, "只读")
}

func hasMutationContinuation(clause string) bool {
	for _, marker := range []string{" then ", " and then ", " but then ", "然后", "再", "接着"} {
		_, tail, ok := strings.Cut(clause, marker)
		if !ok {
			continue
		}
		if matchesAny(tail, []string{
			"fix", "repair", "implement", "write", "edit", "change", "modify", "create", "commit", "push",
			"修复", "实现", "编写", "写入", "编辑", "修改", "创建", "提交", "推送",
		}) {
			return true
		}
	}
	return false
}

func hasGlobalNegatedMutationClause(clause string) bool {
	if describesReadOnlyActor(clause) {
		return false
	}
	for _, phrase := range []string{
		"don't modify", "do not modify", "don't change", "do not change",
		"don't edit", "do not edit", "without modifying", "without changes",
	} {
		if tail, ok := textAfterPhrase(clause, phrase); ok && globalMutationTail(tail) {
			return true
		}
	}
	for _, phrase := range []string{"don't fix", "do not fix", "no fix"} {
		if tail, ok := textAfterPhrase(clause, phrase); ok && globalFixTail(tail) {
			return true
		}
	}
	if tail, ok := textAfterPhrase(clause, "no changes"); ok && globalNoChangesTail(tail) {
		return true
	}
	if tail, ok := textAfterPhrase(clause, "make no changes"); ok && globalNoChangesTail(tail) {
		return true
	}
	for _, phrase := range []string{"不要修改", "不要改动", "不要改", "别修改", "别改", "勿修改"} {
		if tail, ok := textAfterPhrase(clause, phrase); ok && globalChineseMutationTail(tail) {
			return true
		}
	}
	for _, phrase := range []string{"不要修复", "不要修", "别修复", "别修"} {
		if tail, ok := textAfterPhrase(clause, phrase); ok && globalChineseFixTail(tail) {
			return true
		}
	}
	return false
}

func describesReadOnlyActor(clause string) bool {
	return matchesAny(clause, []string{
		"reviewer", "sub-agent", "subagent", "child agent", "child", "planner",
		"审查者", "评审者", "子代理", "子 agent", "规划器",
	}) && matchesAny(clause, []string{"read-only", "read only", "只读"})
}

func textAfterPhrase(clause, phrase string) (string, bool) {
	_, tail, ok := strings.Cut(clause, phrase)
	return strings.TrimSpace(tail), ok
}

func globalMutationTail(tail string) bool {
	if tail == "" {
		return true
	}
	if strings.HasPrefix(tail, ":") {
		return false
	}
	return hasBroadTarget(tail)
}

func globalFixTail(tail string) bool {
	return tail == "" || startsWithAnyWord(tail, []string{"anything", "anything else", "any issue", "any issues"})
}

func globalNoChangesTail(tail string) bool {
	if tail == "" {
		return true
	}
	return startsWithAnyWord(tail, []string{
		"anywhere", "at all", "to anything", "to the workspace", "to the repository", "to the repo", "to the codebase",
	})
}

func hasBroadTarget(tail string) bool {
	return startsWithAnyWord(tail, []string{
		"anything", "anything else", "the workspace", "this workspace", "workspace",
		"the repository", "this repository", "repository", "the repo", "this repo", "repo",
		"the codebase", "this codebase", "codebase", "any file", "any files", "all files", "the source tree",
	})
}

func startsWithAnyWord(value string, prefixes []string) bool {
	value = strings.TrimSpace(value)
	for _, prefix := range prefixes {
		if value == prefix || strings.HasPrefix(value, prefix+" ") || strings.HasPrefix(value, prefix+",") {
			return true
		}
	}
	return false
}

func globalChineseMutationTail(tail string) bool {
	if tail == "" {
		return true
	}
	if strings.HasPrefix(tail, "：") || strings.HasPrefix(tail, ":") {
		return false
	}
	return startsWithAnyChinese(tail, []string{
		"任何内容", "任何东西", "任何文件", "所有文件", "工作区", "当前工作区",
		"仓库", "当前仓库", "代码库", "当前代码库", "源码树",
	})
}

func globalChineseFixTail(tail string) bool {
	return tail == "" || startsWithAnyChinese(tail, []string{"任何问题", "任何内容", "其他任何问题"})
}

func startsWithAnyChinese(value string, prefixes []string) bool {
	value = strings.TrimSpace(value)
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// StripQuotedConstraints removes fenced and quoted spans so cited phrases
// cannot bind the host.
func StripQuotedConstraints(raw string) string {
	s := stripFences(raw)
	s = stripInlineCode(s)
	s = stripQuoted(s, '"', '"')
	s = stripQuoted(s, '“', '”')
	s = stripQuoted(s, '「', '」')
	return strings.TrimSpace(s)
}

func (c Constraints) AllowsMutation() bool {
	return !c.ForbidMutation && !c.PlanModeReadOnly
}

func (c Constraints) AllowsTests() bool { return !c.ForbidTests }

func (c Constraints) AllowsExternal() bool { return !c.ForbidExternal }

func (c Constraints) AllowsCommand(command string) bool {
	if !c.AllowsTests() {
		return false
	}
	command = strings.TrimSpace(command)
	if command == "" || len(c.AllowedChecks) == 0 {
		return true
	}
	for _, allowed := range c.AllowedChecks {
		if strings.EqualFold(strings.TrimSpace(allowed), command) {
			return true
		}
	}
	commandFields, malformed := shellparse.StaticFields(command)
	if malformed != "" || len(commandFields) == 0 {
		return false
	}
	for _, allowed := range c.AllowedChecks {
		allowedFields, malformed := shellparse.StaticFields(strings.TrimSpace(allowed))
		if malformed == "" && len(allowedFields) > 0 && hasFieldPrefix(commandFields, allowedFields) {
			return true
		}
	}
	return false
}

func parseAllowedChecks(instruction string) []string {
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
		cmd := strings.Trim(strings.TrimSpace(m[1]), "\"'`。.")
		if cmd != "" {
			out = append(out, cmd)
		}
	}
	return out
}

func matchesAny(lower string, needles []string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(lower, strings.ToLower(n)) {
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

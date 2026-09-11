// Goal budget helpers classify natural-language task text (English and
// Chinese) into delivery-intent categories. The delivery evidence gates and
// Goal budget selection consume it as a heuristic; it never gates permissions
// or whether writes are allowed.
package control

import (
	"strings"
	"unicode/utf8"
)

// Intent is the delivery expectation a task text implies.
type Intent uint8

const (
	// Conversation is chat with no host-observable work expected.
	Conversation Intent = iota
	// Advisory asks for explanation or advice rather than observable work.
	Advisory
	// ObservableRead expects host-observable read-only work (inspect, review).
	ObservableRead
	// Mutation expects workspace changes.
	Mutation
	// PersistentAction expects durable state kept across sessions.
	PersistentAction
)

// Classify maps a task text to the delivery intent it implies.
func Classify(input string) Intent {
	switch {
	case deliveryTaskHasMutationIntent(input):
		return Mutation
	case NeedsPersistentAction(input):
		return PersistentAction
	case deliveryTaskIsConversationOnly(input):
		return Conversation
	case !heuristicInputIsTask(input):
		return Conversation
	case deliveryTaskIsAdvisory(input):
		return Advisory
	default:
		return ObservableRead
	}
}

// NeedsEvidence reports whether the intent expects host-observable work.
func (i Intent) NeedsEvidence() bool {
	return i == ObservableRead || i == Mutation || i == PersistentAction
}

// NeedsEvidence reports whether a task text expects host-observable work.
func NeedsEvidence(input string) bool {
	return Classify(input).NeedsEvidence()
}

var deliveryMutationNeedles = []string{
	"fix", "repair", "resolve", "create", "add", "write", "edit", "update", "change", "delete", "remove", "rename",
	"implement", "refactor", "apply", "install", "publish", "commit", "push", "continue work",
	"modify", "patch", "replace", "move", "configure", "upgrade", "downgrade", "bump", "enable", "disable", "merge",
	"make changes", "make a change", "make the changes", "make the requested changes", "make the necessary changes", "make these changes", "make those changes", "make code changes",
	"修复", "解决", "创建", "新建", "添加", "编写", "编辑", "修改", "更新", "删除", "移除", "重命名", "实现", "重构",
	"实施", "落地", "安装", "发布", "提交", "继续处理", "调整", "替换", "移动", "升级", "降级", "启用", "禁用", "合并", "改动", "打补丁",
}

var deliveryAdvisoryPhrases = []string{
	"what's wrong", "what is wrong", "why", "what should i do", "what can i do", "how should i", "how do i", "how can i",
	"can you explain", "could you explain", "give me advice", "any advice", "help me understand",
	"为什么", "怎么回事", "怎么办", "怎么", "怎样", "如何", "是什么问题", "什么原因", "的原因", "给我建议", "有什么建议",
}

func NeedsMutation(input string) bool {
	intent := Classify(input)
	return intent == Mutation || intent == PersistentAction
}

func deliveryTaskHasMutationIntent(input string) bool {
	affirmative, _ := deliveryTaskMutationIntent(input)
	return affirmative
}

func NeedsPersistentAction(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" {
		return false
	}
	actionNeedles := []string{
		"remember", "save", "store", "keep this", "keep that",
		"记住", "记下来", "保存", "存下来", "记录下来",
	}
	durableNeedles := []string{
		"permanently", "durable", "long-term", "long term", "across sessions", "future sessions", "every session", "after restart", "after restarting",
		"永久", "长期", "持久", "跨会话", "以后每次", "未来会话", "重启后", "下次启动",
	}
	for _, clause := range deliveryTaskClauses(normalized) {
		action := false
		for _, needle := range actionNeedles {
			affirmative, _ := deliveryTaskNeedleIntent(clause, needle)
			action = action || affirmative
		}
		durable := false
		for _, needle := range durableNeedles {
			affirmative, _ := deliveryTaskNeedleIntent(clause, needle)
			durable = durable || affirmative
		}
		if action && durable && !deliveryTaskClauseIsAdvisory(clause) {
			return true
		}
	}
	return false
}

func deliveryTaskIsConversationOnly(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" || deliveryTaskHasHostAnchor(normalized) || deliveryTaskHasCommand(normalized) {
		return false
	}
	localCue := containsAnySubstring(normalized, []string{
		"next turn", "next message", "later in this chat", "this conversation", "when i ask again", "when i ask next",
		"下一轮", "下轮", "下一条消息", "稍后再问", "待会再问", "这个对话", "本次对话", "本轮会话",
	})
	conversationAction := containsAnySubstring(normalized, []string{
		"remember", "keep in mind", "keep this", "keep that", "answer", "respond", "reply",
		"记住", "记一下", "回答", "回复", "再告诉我",
	})
	return localCue && conversationAction
}

func deliveryTaskMutationIntent(input string) (affirmative, negated bool) {
	normalized := strings.ToLower(strings.TrimSpace(input))
	for _, clause := range deliveryTaskClauses(normalized) {
		clauseAffirmative := false
		clauseNegated := false
		if deliveryMutationClauseNegated(clause) {
			clauseNegated = true
		}
		for _, needle := range deliveryMutationNeedles {
			hasAffirmative, hasNegated := deliveryTaskNeedleIntent(clause, needle)
			clauseAffirmative = clauseAffirmative || hasAffirmative
			clauseNegated = clauseNegated || hasNegated
		}
		if clauseAffirmative && deliveryTaskClauseIsAdvisory(clause) && !deliveryTaskAdvisoryClauseRequestsMutation(clause) {
			clauseAffirmative = false
			clauseNegated = true
		}
		affirmative = affirmative || clauseAffirmative
		negated = negated || clauseNegated
	}
	return affirmative, negated
}

func deliveryTaskIsAdvisory(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))

	// Concrete targets and commands always remain host-observable, including
	// when the request is phrased as a "why" question.
	if deliveryTaskHasHostAnchor(normalized) || deliveryTaskHasCommand(normalized) {
		return false
	}

	// Question wording is scoped per clause. This keeps remote troubleshooting
	// such as "analyze why WPS won't open" advisory, while a separate imperative
	// clause such as "reproduce the crash" still requires observable work.
	sawAdvisory := false
	for _, clause := range deliveryTaskClauses(normalized) {
		if deliveryTaskClauseIsAdvisory(clause) {
			sawAdvisory = true
			continue
		}
		if deliveryTaskClauseHasObservableWork(clause) {
			return false
		}
	}
	if sawAdvisory {
		return true
	}

	// A standalone refusal, inability, or constraint around a mutation verb is
	// advisory rather than work Reasonix can perform. Affirmative mixed intent is
	// handled by NeedsMutation before this function is consulted.
	_, negatedMutation := deliveryTaskMutationIntent(normalized)
	return negatedMutation
}

func deliveryTaskHasHostAnchor(input string) bool {
	for _, anchor := range []string{
		"this repo", "this repository", "current repository", "codebase", "workspace", "pull request", "this pr", "ci job",
		"/pull/", "actions/runs/",
		"当前仓库", "这个仓库", "当前项目", "这个项目", "代码库", "工作区", "这个 pr", "这个pr", "此 pr", "此pr",
	} {
		if strings.Contains(input, anchor) {
			return true
		}
	}
	return deliveryTaskHasFileReference(input)
}

func deliveryTaskHasFileReference(input string) bool {
	previous := rune(0)
	for index, current := range input {
		if current == '@' && index+1 < len(input) &&
			(index == 0 || strings.ContainsRune(" \t\r\n([{<,:;（【《，。；：", previous)) {
			next, _ := utf8.DecodeRuneInString(input[index+1:])
			if !strings.ContainsRune(" \t\r\n", next) {
				return true
			}
		}
		previous = current
	}

	for _, raw := range strings.FieldsFunc(input, func(r rune) bool {
		switch r {
		case ' ', '\t', '\r', '\n', '`', '\'', '"', '(', ')', '[', ']', '{', '}', '<', '>', ',', '，', ';', '；', '!', '！', '?', '？':
			return true
		default:
			return false
		}
	}) {
		token := strings.ToLower(strings.TrimSpace(raw))
		if token == "" || strings.Contains(token, "://") {
			continue
		}
		if strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") ||
			strings.HasPrefix(token, "/") || strings.Contains(token, `\`) {
			return true
		}
		base := token
		if slash := strings.LastIndexByte(base, '/'); slash >= 0 {
			base = base[slash+1:]
		}
		switch base {
		case "dockerfile", "makefile", "cmakelists.txt", "justfile", "license", "readme", "changelog":
			return true
		}
		dot := strings.LastIndexByte(base, '.')
		if dot < 0 {
			continue
		}
		switch base[dot:] {
		case ".go", ".mod", ".sum", ".js", ".jsx", ".ts", ".tsx", ".py", ".rs", ".java", ".kt", ".swift",
			".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".rb", ".php", ".sh", ".zsh", ".fish", ".ps1",
			".md", ".json", ".yaml", ".yml", ".toml", ".xml", ".sql", ".proto", ".html", ".css", ".scss",
			".vue", ".svelte", ".txt", ".log", ".csv", ".pdf", ".env", ".ini", ".conf", ".lock":
			return true
		}
	}
	return false
}

func deliveryTaskHasCommand(input string) bool {
	tokens := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		asciiWord := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		return !asciiWord && r != '_' && r != '-' && r != '.' && r != '/' && r != '\\' && r != ':'
	})
	for i := range tokens {
		if deliveryCommandStartsAt(tokens, i) {
			return true
		}
	}
	return false
}

func deliveryCommandStartsAt(tokens []string, index int) bool {
	command := strings.TrimSpace(tokens[index])
	if command == "" {
		return false
	}
	if strings.HasPrefix(command, "./") || strings.HasPrefix(command, "../") ||
		strings.HasPrefix(command, "/") || strings.Contains(command, `\`) {
		return true
	}
	next := ""
	if index+1 < len(tokens) {
		next = tokens[index+1]
	}
	if next != "--" && len(next) > 1 && strings.HasPrefix(next, "-") {
		return true
	}
	previous := ""
	if index > 0 {
		previous = tokens[index-1]
	}
	switch command {
	case "go":
		switch next {
		case "build", "clean", "doc", "env", "fmt", "generate", "get", "install", "list", "mod", "run", "test", "tool", "version", "vet", "work":
			return true
		}
	case "git", "npm", "npx", "pnpm", "yarn", "bun", "deno", "cargo", "rustc", "python", "python3",
		"bash", "sh", "zsh", "fish", "powershell", "pwsh", "docker", "docker-compose", "kubectl", "helm", "terraform",
		"gradle", "gradlew", "mvn", "dotnet", "xcodebuild", "gcc", "g++", "clang", "clang++":
		return deliveryCommandHasExplicitCue(previous) || deliveryCommandHasSubcommand(next)
	case "node":
		return deliveryCommandHasExplicitCue(previous) || next == "inspect" || next == "test"
	case "swift":
		return next == "build" || next == "package" || next == "run" || next == "test"
	case "make", "just":
		switch next {
		case "all", "build", "check", "clean", "fail", "failed", "failing", "install", "lint", "test":
			return true
		}
	case "pytest", "cmake", "ninja", "eslint", "tsc", "vitest", "jest":
		return deliveryCommandHasExplicitCue(previous) || next == "fail" || next == "failed" || next == "failing"
	}
	return false
}

func deliveryCommandHasExplicitCue(previous string) bool {
	switch previous {
	case "command", "execute", "executing", "run", "running", "using", "with":
		return true
	default:
		return false
	}
}

func deliveryCommandHasSubcommand(next string) bool {
	switch next {
	case "add", "apply", "branch", "build", "check", "checkout", "clean", "clone", "commit", "config", "container",
		"deploy", "describe", "destroy", "dev", "diff", "down", "env", "exec", "fetch", "fmt", "generate", "get", "image",
		"init", "install", "lint", "list", "log", "logs", "login", "logout", "merge", "mod", "package", "plan", "ps", "publish",
		"pull", "push", "rebase", "remote", "remove", "reset", "restore", "run", "serve", "show", "start", "stash", "status",
		"switch", "tag", "test", "tool", "uninstall", "up", "update", "upgrade", "version", "vet", "work", "worktree":
		return true
	default:
		return false
	}
}

func deliveryTaskClauseHasObservableWork(clause string) bool {
	for _, needle := range []string{
		"review", "inspect", "analyze", "check", "reproduce", "audit", "verify",
		"评审", "审查", "检查", "分析", "复现", "审计", "验证",
	} {
		affirmative, _ := deliveryTaskNeedleIntent(clause, needle)
		if affirmative {
			return true
		}
	}
	return false
}

func deliveryTaskClauseIsAdvisory(clause string) bool {
	for _, phrase := range deliveryAdvisoryPhrases {
		if strings.Contains(clause, phrase) {
			return true
		}
	}
	return false
}

func deliveryTaskAdvisoryClauseRequestsMutation(clause string) bool {
	advisoryIndex := len(clause)
	for _, phrase := range deliveryAdvisoryPhrases {
		if index := strings.Index(clause, phrase); index >= 0 && index < advisoryIndex {
			advisoryIndex = index
		}
	}
	if advisoryIndex == len(clause) {
		return false
	}
	if deliveryTaskStartsWithMutation(clause[:advisoryIndex]) {
		return true
	}

	for _, cue := range []string{" please ", " then ", " so ", " therefore ", "然后", "所以", "而是", "转而"} {
		for rest := clause[advisoryIndex:]; ; {
			index := strings.Index(rest, cue)
			if index < 0 {
				break
			}
			rest = rest[index+len(cue):]
			if deliveryTaskStartsWithMutation(rest) {
				return true
			}
		}
	}
	for rest, offset := clause[advisoryIndex:], advisoryIndex; ; {
		index := strings.Index(rest, "请")
		if index < 0 {
			break
		}
		absolute := offset + index
		after := clause[absolute+len("请"):]
		requestWord := strings.HasSuffix(clause[:absolute], "申") || strings.HasPrefix(after, "求")
		if !requestWord && deliveryTaskStartsWithMutation(after) {
			return true
		}
		offset = absolute + len("请")
		rest = clause[offset:]
	}

	for _, cue := range []string{" and ", "并且", "并"} {
		if index := strings.LastIndex(clause[advisoryIndex:], cue); index >= 0 {
			cueStart := advisoryIndex + index
			tail := clause[cueStart+len(cue):]
			if !deliveryTaskClauseHasNegation(clause[:cueStart]) && deliveryTaskStartsWithMutation(tail) {
				return true
			}
		}
	}
	return false
}

func deliveryTaskStartsWithMutation(input string) bool {
	input = strings.TrimSpace(input)
	for {
		stripped := false
		for _, prefix := range []string{"please ", "can you ", "could you ", "would you ", "you should ", "帮我", "请你", "直接", "继续", "再"} {
			if after, ok := strings.CutPrefix(input, prefix); ok {
				input = strings.TrimSpace(after)
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
	}
	for _, needle := range deliveryMutationNeedles {
		if containsTaskNeedle(input, needle) {
			if goalBudgetContainsNonASCII(needle) {
				return strings.HasPrefix(input, needle)
			}
			tokens := strings.FieldsFunc(input, func(r rune) bool {
				return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '\''
			})
			needleTokens := strings.Fields(needle)
			if len(tokens) >= len(needleTokens) {
				matches := true
				for i := range needleTokens {
					matches = matches && tokens[i] == needleTokens[i]
				}
				if matches {
					return true
				}
			}
		}
	}
	return false
}

func deliveryTaskClauseHasNegation(clause string) bool {
	clause = strings.ReplaceAll(clause, "’", "'")
	for _, phrase := range []string{
		" not ", " never ", " without ", "cannot", "can't", " cant ", "don't", " dont ", "won't", " wont ", "unable",
		"不要", "别", "勿", "不能", "无法", "不想", "不敢", "无需", "不需要", "不可", "没法", "没有", "禁止", "拒绝",
	} {
		if strings.Contains(" "+clause+" ", phrase) {
			return true
		}
	}
	return false
}

func deliveryTaskClauses(input string) []string {
	input = strings.NewReplacer(
		" but ", "\n",
		" however ", "\n",
		" nevertheless ", "\n",
		"但请", "\n请",
		"但是", "\n",
		"不过", "\n",
	).Replace(input)
	return strings.FieldsFunc(input, func(r rune) bool {
		switch r {
		case '\n', '\r', '.', '。', ',', '，', ';', '；', '!', '！', '?', '？':
			return true
		default:
			return false
		}
	})
}

func deliveryMutationClauseNegated(clause string) bool {
	for _, phrase := range []string{
		"without changing", "without modifying", "analysis only", "review only",
		"不要改动", "只分析", "仅分析", "只检查", "仅检查", "只评审", "仅评审",
	} {
		if strings.Contains(clause, phrase) {
			return true
		}
	}
	return false
}

func deliveryTaskNeedleIntent(clause, needle string) (affirmative, negated bool) {
	if goalBudgetContainsNonASCII(needle) {
		for offset := 0; offset < len(clause); {
			relative := strings.Index(clause[offset:], needle)
			if relative < 0 {
				break
			}
			index := offset + relative
			prefix := []rune(clause[:index])
			if deliveryMutationRunesNegated(prefix) {
				negated = true
			} else {
				affirmative = true
			}
			offset = index + len(needle)
		}
		return affirmative, negated
	}

	clause = strings.ReplaceAll(clause, "’", "'")
	tokens := strings.FieldsFunc(clause, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '\''
	})
	needleTokens := strings.Fields(needle)
	for i := 0; i+len(needleTokens) <= len(tokens); i++ {
		matches := true
		for j, token := range needleTokens {
			if tokens[i+j] != token {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if deliveryMutationTokensNegated(tokens[:i]) {
			negated = true
		} else {
			affirmative = true
		}
	}
	return affirmative, negated
}

func deliveryMutationTokensNegated(prefix []string) bool {
	if len(prefix) > 6 {
		prefix = prefix[len(prefix)-6:]
	}
	boundary := -1
	for i, token := range prefix {
		switch token {
		case "but", "however", "nevertheless", "instead", "so", "then", "therefore", "please":
			boundary = i
		}
	}
	if boundary >= 0 {
		prefix = prefix[boundary+1:]
	}
	for i, token := range prefix {
		if token == "not" && i+1 < len(prefix) && prefix[i+1] == "only" {
			continue
		}
		switch token {
		case "not", "never", "without", "cannot", "can't", "cant", "don't", "dont", "won't", "wont", "unable", "avoid", "avoiding", "afraid", "refuse", "refusing", "needn't":
			return true
		case "no":
			if i+1 < len(prefix) && prefix[i+1] == "need" {
				return true
			}
		}
	}
	return false
}

func deliveryMutationRunesNegated(prefix []rune) bool {
	if len(prefix) > 12 {
		prefix = prefix[len(prefix)-12:]
	}
	window := string(prefix)
	scopeStart := 0
	for _, boundary := range []string{"所以", "然后", "而是", "转而", "改为"} {
		if index := strings.LastIndex(window, boundary); index >= 0 {
			end := index + len(boundary)
			if end > scopeStart {
				scopeStart = end
			}
		}
	}
	if index := strings.LastIndex(window, "请"); index >= 0 {
		before, after := window[:index], window[index+len("请"):]
		requestWord := strings.HasSuffix(before, "申") || strings.HasPrefix(after, "求")
		negatedRequest := false
		for _, marker := range []string{"不要", "不能", "无法", "不想", "不敢", "无需", "不需要", "不可", "没法", "禁止", "拒绝"} {
			if strings.HasSuffix(before, marker) || strings.Contains(after, marker) {
				negatedRequest = true
				break
			}
		}
		if !requestWord && !negatedRequest && index+len("请") > scopeStart {
			scopeStart = index + len("请")
		}
	}
	window = window[scopeStart:]
	for _, marker := range []string{"不要", "别", "勿", "不能", "无法", "不想", "不敢", "无需", "不需要", "不可", "没法", "没有", "禁止", "拒绝"} {
		if strings.Contains(window, marker) {
			return true
		}
	}
	return false
}

func containsAnySubstring(s string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

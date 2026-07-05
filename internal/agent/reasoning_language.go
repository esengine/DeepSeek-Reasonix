package agent

import (
	"context"
	"strings"
	"unicode"
)

type reasoningLanguageContextKey struct{}
type responseLanguageContextKey struct{}

// NormalizeReasoningLanguage returns one of auto|zh|en for runtime-only visible
// reasoning preferences. Keep this local to agent so sub-agents can inherit the
// preference without depending on config.
func NormalizeReasoningLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "cn", "chinese", "中文":
		return "zh"
	case "en", "english":
		return "en"
	default:
		return "auto"
	}
}

// NormalizeResponseLanguage returns one of auto|zh|en for final-answer language
// preferences. Auto keeps the stable same-as-user language policy.
func NormalizeResponseLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "cn", "chinese", "中文":
		return "zh"
	case "en", "english":
		return "en"
	default:
		return "auto"
	}
}

// ResponseLanguageBlock is transient user-turn context for final answers. It
// stays out of the stable system prompt so changing the preference between turns
// does not churn the cached prefix.
func ResponseLanguageBlock(lang string) string {
	switch NormalizeResponseLanguage(lang) {
	case "zh":
		return "<response-language>\nFinal answer language preference: use Simplified Chinese for user-facing replies unless the user explicitly asks for another language. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form.\n</response-language>"
	case "en":
		return "<response-language>\nFinal answer language preference: use English for user-facing replies unless the user explicitly asks for another language. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form.\n</response-language>"
	default:
		return ""
	}
}

// ReasoningLanguageBlock is transient user-turn context. It deliberately does
// not belong in the stable system prompt or tool schemas.
func ReasoningLanguageBlock(lang string) string {
	switch NormalizeReasoningLanguage(lang) {
	case "zh":
		return "<reasoning-language>\n可见推理/思考文本偏好：当模型服务暴露可见推理或思考文本时，请使用简体中文。代码、标识符、文件路径、shell 命令和未翻译的技术术语保持原文。此偏好不会覆盖用户对最终回答语言的明确要求。\n</reasoning-language>"
	case "en":
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use English when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	default:
		return ""
	}
}

// ResolveReasoningLanguage returns the concrete visible-reasoning language for
// a turn. Explicit zh/en settings win; auto anchors clear Chinese user prompts
// and otherwise stays provider-default to preserve the historical no-injection
// behaviour for English and ambiguous turns.
func ResolveReasoningLanguage(lang, source string) string {
	mode := NormalizeReasoningLanguage(lang)
	if mode != "auto" {
		return mode
	}
	return InferReasoningLanguageFromText(source)
}

// InferReasoningLanguageFromText conservatively detects Chinese user-authored
// turns for auto reasoning-language mode. It strips Reasonix-injected context
// wrappers first so large @file payloads or transient XML blocks do not drown
// out the user's actual prompt. English and ambiguous turns intentionally return
// auto, preserving the old no-extra-instruction behaviour.
func InferReasoningLanguageFromText(source string) string {
	source = reasoningLanguageSourceText(source)
	if source == "" {
		return "auto"
	}
	han, cjkPunct := reasoningLanguageScriptCounts(source)
	switch {
	case han >= 4:
		return "zh"
	case han >= 2 && (cjkPunct > 0 || hasChineseReasoningCue(source)):
		return "zh"
	default:
		return "auto"
	}
}

func reasoningLanguageSourceText(source string) string {
	s := strings.TrimSpace(StripTransientUserBlocks(source))
	const preamble = "Referenced context:"
	if !strings.HasPrefix(s, preamble) {
		return s
	}
	s = strings.TrimSpace(s[len(preamble):])
	for {
		s = strings.TrimSpace(s)
		if s == "" || !strings.HasPrefix(s, "<") {
			return s
		}
		tagEnd := strings.IndexAny(s, " >\t\r\n")
		if tagEnd <= 1 {
			return s
		}
		tag := s[1:tagEnd]
		switch tag {
		case "file", "dir", "resource", "image":
			closeTag := "</" + tag + ">"
			i := strings.Index(s, closeTag)
			if i < 0 {
				return s
			}
			s = strings.TrimSpace(s[i+len(closeTag):])
		default:
			return s
		}
	}
}

func reasoningLanguageScriptCounts(source string) (han, cjkPunct int) {
	for _, r := range source {
		switch {
		case unicode.In(r, unicode.Han):
			han++
		case isCJKPunctuation(r):
			cjkPunct++
		}
	}
	return han, cjkPunct
}

func isCJKPunctuation(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F:
		return true
	case r >= 0xFF00 && r <= 0xFFEF:
		return true
	default:
		return false
	}
}

func hasChineseReasoningCue(source string) bool {
	for _, cue := range chineseReasoningLanguageCues {
		if strings.Contains(source, cue) {
			return true
		}
	}
	return false
}

var chineseReasoningLanguageCues = []string{
	"你好", "请", "帮我", "帮忙", "看看", "看下", "解释", "说明", "总结", "分析",
	"修复", "实现", "优化", "排查", "处理", "继续", "为什么", "怎么",
	"是否", "能否", "支持", "设置", "中文", "思考", "问题", "报错",
	"代码", "文件", "这个", "那个",
}

func reasoningLanguageBlockForSource(lang, source string) string {
	return ReasoningLanguageBlock(ResolveReasoningLanguage(lang, source))
}

// WithResponseLanguage prefixes content with the transient response-language
// block unless the turn already starts with one.
func WithResponseLanguage(content, lang string) string {
	block := ResponseLanguageBlock(lang)
	if block == "" || hasLeadingInjectedBlock(content, "response-language") {
		return content
	}
	return block + "\n\n" + content
}

// WithReasoningLanguage prefixes content with the transient reasoning-language
// block unless the turn already starts with an injected reasoning-language
// block. User-authored mentions of the tag later in the prompt must not suppress
// the configured preference.
func WithReasoningLanguage(content, lang string) string {
	return WithReasoningLanguageForSource(content, lang, content)
}

// WithReasoningLanguageForSource prefixes content using source as the language
// signal for auto mode. Callers that expand @references should pass the raw
// user prompt as source so referenced English code or logs do not override the
// user's actual conversation language.
func WithReasoningLanguageForSource(content, lang, source string) string {
	block := reasoningLanguageBlockForSource(lang, source)
	if block == "" || hasLeadingInjectedBlock(content, "reasoning-language") {
		return content
	}
	return block + "\n\n" + content
}

func hasLeadingInjectedBlock(content, target string) bool {
	s := strings.TrimLeft(content, " \t\r\n")
	for {
		switch {
		case strings.HasPrefix(s, "<"+target+">"):
			return strings.Contains(s, "</"+target+">")
		case target != "response-language" && strings.HasPrefix(s, "<response-language>"):
			var ok bool
			s, ok = trimLeadingTransientBlock(s, "response-language")
			if !ok {
				return false
			}
		case target != "reasoning-language" && strings.HasPrefix(s, "<reasoning-language>"):
			var ok bool
			s, ok = trimLeadingTransientBlock(s, "reasoning-language")
			if !ok {
				return false
			}
		case strings.HasPrefix(s, "<memory-update>"):
			var ok bool
			s, ok = trimLeadingTransientBlock(s, "memory-update")
			if !ok {
				return false
			}
		case strings.HasPrefix(s, "<background-jobs>"):
			var ok bool
			s, ok = trimLeadingTransientBlock(s, "background-jobs")
			if !ok {
				return false
			}
		case strings.HasPrefix(s, "<hook-context"):
			var ok bool
			s, ok = trimLeadingTransientBlock(s, "hook-context")
			if !ok {
				return false
			}
		default:
			return false
		}
	}
}

func trimLeadingTransientBlock(content, tag string) (string, bool) {
	closeTag := "</" + tag + ">"
	i := strings.Index(content, closeTag)
	if i < 0 {
		return content, false
	}
	return strings.TrimLeft(content[i+len(closeTag):], " \t\r\n"), true
}

// WithResponseLanguagePreference carries the runtime final-answer language
// preference to spawned tools and sub-agents.
func WithResponseLanguagePreference(ctx context.Context, lang string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, responseLanguageContextKey{}, NormalizeResponseLanguage(lang))
}

// ResponseLanguageFromContext returns auto|zh|en.
func ResponseLanguageFromContext(ctx context.Context) string {
	if ctx == nil {
		return "auto"
	}
	if v, ok := ctx.Value(responseLanguageContextKey{}).(string); ok {
		return NormalizeResponseLanguage(v)
	}
	return "auto"
}

// WithReasoningLanguagePreference carries the runtime preference to spawned
// tools, especially sub-agents whose first user turn is created outside the
// parent controller. It stores auto explicitly so live zh/en -> auto changes
// clear stale boot-time preferences in child paths.
func WithReasoningLanguagePreference(ctx context.Context, lang string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, reasoningLanguageContextKey{}, NormalizeReasoningLanguage(lang))
}

// ReasoningLanguageFromContext returns auto|zh|en.
func ReasoningLanguageFromContext(ctx context.Context) string {
	if ctx == nil {
		return "auto"
	}
	if v, ok := ctx.Value(reasoningLanguageContextKey{}).(string); ok {
		return NormalizeReasoningLanguage(v)
	}
	return "auto"
}

package agent

import (
	"context"
	"strings"
)

type reasoningLanguageContextKey struct{}

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

// ReasoningLanguageBlock is transient user-turn context. It deliberately does
// not belong in the stable system prompt or tool schemas.
func ReasoningLanguageBlock(lang string) string {
	switch NormalizeReasoningLanguage(lang) {
	case "zh":
		return "<reasoning-language>\n可见推理/思考文本的语言偏好：当提供方暴露推理文本时，请使用简体中文进行思考和推理。代码、标识符、文件路径、Shell 命令以及未翻译的技术术语保持原样。此偏好不会覆盖用户对最终回答语言的明确要求。\n</reasoning-language>"
	case "en":
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use English when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	default:
		return ""
	}
}

// WithReasoningLanguage prefixes content with the transient reasoning-language
// block unless the turn already starts with an injected reasoning-language
// block. User-authored mentions of the tag later in the prompt must not suppress
// the configured preference.
func WithReasoningLanguage(content, lang string) string {
	block := ReasoningLanguageBlock(lang)
	if block == "" || hasLeadingReasoningLanguageBlock(content) {
		return content
	}
	return block + "\n\n" + content
}

func hasLeadingReasoningLanguageBlock(content string) bool {
	s := strings.TrimLeft(content, " \t\r\n")
	for {
		switch {
		case strings.HasPrefix(s, "<reasoning-language>"):
			return strings.Contains(s, "</reasoning-language>")
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

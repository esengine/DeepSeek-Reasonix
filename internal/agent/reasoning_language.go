package agent

import (
	"context"
	"strings"
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
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use Simplified Chinese when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	case "en":
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use English when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	default:
		return ""
	}
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
	block := ReasoningLanguageBlock(lang)
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

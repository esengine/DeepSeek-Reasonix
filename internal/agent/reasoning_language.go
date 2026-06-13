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
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use Simplified Chinese when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	case "en":
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use English when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	default:
		return ""
	}
}

// WithReasoningLanguage prefixes content with the transient reasoning-language
// block unless it is already present. The broad contains check avoids duplicate
// blocks in controller-composed turns and coordinator handoffs.
func WithReasoningLanguage(content, lang string) string {
	block := ReasoningLanguageBlock(lang)
	if block == "" || strings.Contains(content, "<reasoning-language>") {
		return content
	}
	return block + "\n\n" + content
}

// WithReasoningLanguagePreference carries the runtime preference to spawned
// tools, especially task sub-agents whose first user turn is created outside the
// parent controller.
func WithReasoningLanguagePreference(ctx context.Context, lang string) context.Context {
	mode := NormalizeReasoningLanguage(lang)
	if mode == "auto" {
		return ctx
	}
	return context.WithValue(ctx, reasoningLanguageContextKey{}, mode)
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

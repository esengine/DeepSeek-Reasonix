package config

import (
	"regexp"
	"strings"
)

// modelContextPatterns mirrors qwen-code's tokenLimits.ts PATTERNS: ordered
// regex rules that auto-detect a model's context window from its name. First
// match wins. Used as a fallback when ProviderEntry.ContextWindow is zero.
var modelContextPatterns = []struct {
	re     *regexp.Regexp
	tokens int
}{
	// Google Gemini
	{regexp.MustCompile(`^gemini-3`), 1_000_000},
	{regexp.MustCompile(`^gemini-`), 1_000_000},
	// OpenAI
	{regexp.MustCompile(`^gpt-5`), 272_000},
	{regexp.MustCompile(`^gpt-`), 131_072},
	{regexp.MustCompile(`^o\d`), 200_000},
	// Anthropic
	{regexp.MustCompile(`^claude-`), 200_000},
	// Alibaba / Qwen — commercial API (1M)
	{regexp.MustCompile(`^qwen3-coder-plus`), 1_000_000},
	{regexp.MustCompile(`^qwen3-coder-flash`), 1_000_000},
	{regexp.MustCompile(`^qwen3\.\d`), 1_000_000},
	{regexp.MustCompile(`^qwen-plus-latest$`), 1_000_000},
	{regexp.MustCompile(`^qwen-flash-latest$`), 1_000_000},
	{regexp.MustCompile(`^coder-model$`), 1_000_000},
	// Qwen — 256K tier
	{regexp.MustCompile(`^qwen3-max`), 262_144},
	{regexp.MustCompile(`^qwen3-coder-`), 262_144},
	// Qwen fallback
	{regexp.MustCompile(`^qwen`), 262_144},
	// DeepSeek
	{regexp.MustCompile(`^deepseek-v4`), 1_000_000},
	{regexp.MustCompile(`^deepseek`), 131_072},
	// Zhipu GLM
	{regexp.MustCompile(`^glm-5(\.[01])?(-|$)`), 202_752},
	{regexp.MustCompile(`^glm-(?:[5-9]|\d{2,})`), 1_000_000},
	{regexp.MustCompile(`^glm-`), 202_752},
	// MiniMax
	{regexp.MustCompile(`(?i)^minimax-m3`), 1_000_000},
	{regexp.MustCompile(`(?i)^minimax-m2\.5`), 196_608},
	{regexp.MustCompile(`(?i)^minimax-`), 200_000},
	// Moonshot / Kimi
	{regexp.MustCompile(`^kimi-`), 262_144},
	// ByteDance Seed-OSS
	{regexp.MustCompile(`^seed-oss`), 524_288},
}

// AutoContextWindow returns the inferred context window size for a model name
// using regex pattern matching (ported from qwen-code tokenLimits.ts). Returns
// 0 when no pattern matches, meaning the caller should fall back to its own
// default. The model string is normalized (lowercased, provider prefix stripped)
// before matching.
func AutoContextWindow(model string) int {
	norm := normalizeModelName(model)
	for _, p := range modelContextPatterns {
		if p.re.MatchString(norm) {
			return p.tokens
		}
	}
	return 0
}

// applyAutoContextWindow fills ContextWindow from the resolved model name when
// the instance has no explicit budget. An explicit context_window (config or
// model override) already applied takes precedence; when inference also fails,
// ContextWindow stays 0 — which the agent treats as "compaction disabled", the
// conservative choice for an unknown window.
func (e *ProviderEntry) applyAutoContextWindow() {
	if e == nil || e.ContextWindow != 0 {
		return
	}
	if w := AutoContextWindow(e.Model); w > 0 {
		e.ContextWindow = w
	}
}

// normalizeModelName strips provider prefixes (e.g. "qwen/qwen3.7-plus" →
// "qwen3.7-plus"), lowercases, and trims whitespace. Mirrors qwen-code's
// normalize() for the common cases.
func normalizeModelName(model string) string {
	s := strings.ToLower(strings.TrimSpace(model))
	// Strip provider prefix: "org/model" → "model"
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}
	// Strip pipe/colon suffixes (ollama tags): "model:30b" → "model"
	if idx := strings.LastIndex(s, "|"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

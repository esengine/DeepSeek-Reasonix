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
	// Alibaba / Qwen — verified against help.aliyun.com model docs (2026-08):
	// qwen3.7/3.6/3.5 plus+flash, qwen3-coder-plus/flash, qwen-plus, qwen-flash
	// all advertise 1M context. Only qwen3-max (256k) and its dated snapshots
	// are the 256k tier. Order matters: specific coder/3.x patterns before the
	// bare qwen fallback.
	{regexp.MustCompile(`^qwen3-coder-plus`), 1_000_000},
	{regexp.MustCompile(`^qwen3-coder-flash`), 1_000_000},
	{regexp.MustCompile(`^qwen3\.\d`), 1_000_000}, // qwen3.7-plus/max, qwen3.6-*, qwen3.5-*
	{regexp.MustCompile(`^qwen-plus`), 1_000_000},   // main + dated snapshots
	{regexp.MustCompile(`^qwen-flash`), 1_000_000},  // main + dated snapshots
	{regexp.MustCompile(`^coder-model$`), 1_000_000},
	// Qwen — 256K tier (qwen3-max and snapshots)
	{regexp.MustCompile(`^qwen3-max`), 262_144},
	{regexp.MustCompile(`^qwen3-coder-`), 262_144}, // qwen3-coder-next (unverified tier)
	// Qwen fallback (older open qwen-* models: 128K tier)
	{regexp.MustCompile(`^qwen`), 131_072},
	// DeepSeek
	{regexp.MustCompile(`^deepseek-v4`), 1_000_000},
	{regexp.MustCompile(`^deepseek`), 131_072},
	// Zhipu GLM — verified against docs.bigmodel.cn (2026-08):
	// glm-5.2=1M; glm-5/5.1/4.7=200K; glm-4.5=128K (deprecated).
	// Order matters: 5.x/4.x specific tiers must precede the 5-9/2-digit
	// general rule so glm-5 and glm-5.1 (200K) are not caught by it (1M).
	{regexp.MustCompile(`^glm-5(\.0?[01])?(-|$)`), 204_800},
	{regexp.MustCompile(`^glm-5\.2`), 1_000_000},
	{regexp.MustCompile(`^glm-4\.5`), 131_072},
	{regexp.MustCompile(`^glm-4\.7`), 204_800},
	{regexp.MustCompile(`^glm-(?:[5-9]|\d{2,})`), 1_000_000},
	{regexp.MustCompile(`^glm-`), 204_800},
	// MiniMax — verified against platform.minimaxi.com (2026-08):
	// m3=1M; m2.5/m2.7=204,800 (exact official number).
	{regexp.MustCompile(`(?i)^minimax-m3`), 1_000_000},
	{regexp.MustCompile(`(?i)^minimax-m2\.`), 204_800},
	{regexp.MustCompile(`(?i)^minimax-m1`), 1_000_000}, // legacy M1 tier
	{regexp.MustCompile(`(?i)^minimax-`), 204_800},
	// Moonshot / Kimi — verified against platform.kimi.com (2026-08):
	// kimi-k3=1M; kimi-k2.5/k2.6/k2.7=256K.
	{regexp.MustCompile(`^kimi-k3`), 1_000_000},
	{regexp.MustCompile(`^kimi-k2`), 262_144},
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

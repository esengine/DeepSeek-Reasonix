package responses

import (
	"net/url"
	"strings"
	"time"

	"reasonix/internal/provider"
)

// vendorCapabilities describes how a Responses-compatible endpoint deviates
// from the base OpenAI Responses wire behavior. Vendors are detected from the
// base URL (DetectVendor); unknown endpoints get the zero value, which is the
// standard OpenAI-compatible behavior (stateful, no session-cache header, no
// summary requirement, no tool-call reasoning retention, temperature honored).
//
// This table is the single source of truth for wire-level vendor differences:
// adding a new Responses-compatible vendor means adding one entry here and a
// base-URL case in DetectVendor — not threading more string comparisons
// through responses.go.
type vendorCapabilities struct {
	// stateless marks endpoints that reject previous_response_id and require
	// the full input history on every turn (DeepSeek, MiMo). stateful is the
	// OpenAI default.
	stateless bool

	// sessionCacheHeader marks DashScope, whose session cache must be opted
	// into with the x-dashscope-session-cache header.
	sessionCacheHeader bool

	// toolCallReasoning marks stateless vendors whose documentation requires
	// retaining historical reasoning content in the input on multi-turn tool
	// calls (DeepSeek, MiMo).
	toolCallReasoning bool

	// singleSegmentReasoning marks endpoints whose thinking is one
	// uninterruptible segment per turn: the server emits reasoning and the
	// final answer atomically, and a new reasoning segment only starts on a
	// brand-new turn — never mid-turn after a tool call. MiMo documents this
	// ("reasoning.effort: low/medium/high all enable reasoning, no strength
	// differentiation"; tool-call turns carry one segment). DeepSeek, by
	// contrast, can emit several reasoning segments across a turn's tool
	// loop. Callers must not expect a multi-segment chain-of-thought from
	// single-segment vendors.
	singleSegmentReasoning bool

	// ignoresTemperature marks vendors that force temperature/top_p to their
	// defaults in thinking mode, so sending them is a no-op (MiMo forces
	// 1.0 / 0.95). Keeps the wire request lean for such endpoints.
	ignoresTemperature bool

	// defaultMaxOutputTokens is the max_output_tokens sent when the caller
	// did not request one (req.MaxTokens == 0). Zero means "leave unset and
	// let the server use its own default". MiMo's server default (32768)
	// covers reasoning + visible output, and its thinking mode can spend a
	// large chunk of that budget on reasoning before the visible answer —
	// truncating tool calls mid-JSON on long turns. Raise it to 128000
	// (MiMo-Code's MIMO_OUTPUT_TOKEN_MAX, within the allowed [1, 131072]
	// range) so the answer survives long reasoning.
	defaultMaxOutputTokens int

	// summaryMode, when non-empty, is sent as reasoning.summary in the
	// request body. MiMo-Code's codex config sets model_reasoning_summary
	// = "none" to tell the server NOT to emit reasoning summaries, keeping
	// the output budget for visible content. Empty means "do not send
	// reasoning.summary" (the OpenAI default).
	summaryMode string

	// compactionOutputTokens is the separate budget for native/summary
	// compaction calls. Zero means "no dedicated compaction budget; fall
	// back to ordinary summarize without inheriting a large default".
	compactionOutputTokens int

	// nativeCompaction marks vendors with a dedicated compact endpoint.
	// When false, agents must use ordinary summarize fallback.
	nativeCompaction bool

	// summaryRequired marks vendors whose Responses API requires the
	// `summary` list on input reasoning items (DashScope; without it the
	// server rejects with "Invalid 'summary': summary is required..."). The
	// OpenAI base format only needs `content`. Sending `summary` to vendors
	// that do not define it (MiMo) leaks the reasoning text into an extra
	// field the server may fold back into the model context, doubling the
	// chain-of-thought echoed each turn and inflating reasoning output
	// until truncation. Only send it where the wire demands it.
	summaryRequired bool

	// streamIdleTimeout overrides the default SSE stream idle timeout for
	// this vendor. MiMo's cold-path TTFT can reach ~5 minutes for long
	// reasoning turns; the default 120s would abort prematurely. Zero means
	// use defaultStreamIdleTimeout.
	streamIdleTimeout time.Duration
}

var vendorTable = map[string]vendorCapabilities{
	"dashscope": {
		stateless:              false,
		sessionCacheHeader:     true,
		toolCallReasoning:      false,
		singleSegmentReasoning: false,
		ignoresTemperature:     false,
		// summaryRequired: input reasoning items 需要 summary 列表（回传历史推理时）。
		// summaryMode 不设置：DashScope Responses API 无 reasoning.summary
		// 请求参数。推理摘要始终通过 response.reasoning_summary_text.delta
		// 输出，无法通过参数关闭。控制推理开关用 enable_thinking，不用
		// reasoning.summary。
		summaryRequired: true,
		// No native compact endpoint yet; summarize fallback only.
		compactionOutputTokens: 8192,
	},
	"deepseek": {
		stateless:              true,
		sessionCacheHeader:     false,
		toolCallReasoning:      true,
		singleSegmentReasoning: false,
		ignoresTemperature:     false,
		defaultMaxOutputTokens: provider.DefaultHighOutputTokens,
		summaryMode:            "detailed",
		// Compaction summaries are short briefings; keep the budget separate
		// from ordinary answer output so a summary call cannot inherit 32K.
		compactionOutputTokens: 4096,
	},
	"mimo": {
		stateless:              true,
		sessionCacheHeader:     false,
		toolCallReasoning:      true,
		singleSegmentReasoning: false,
		ignoresTemperature:     true,
		defaultMaxOutputTokens: 128000,
		summaryMode:            "none",
		// MiMo only accepts effort values: none, low, medium, high
		// (case-sensitive lowercase). auto/disabled/off/HIGH are rejected
		// with HTTP 400. NormalizeEffort in effort.go handles the mapping.
		streamIdleTimeout:      8 * time.Minute, // cold-path TTFT ~5min
		compactionOutputTokens: 4096,
	},
	// "" (unknown OpenAI-compatible endpoint) → zero value = default behavior.
	// Unknown gateways deliberately do NOT inherit a large max-output default.
}

// capabilitiesFor returns the wire capabilities for a detected vendor name.
// Unknown vendors fall back to the zero value (standard OpenAI behavior).
func capabilitiesFor(vendor string) vendorCapabilities {
	return vendorTable[vendor]
}

// DetectVendor identifies endpoint behavior that affects the Responses wire.
// Empty means an unknown OpenAI-compatible endpoint with default behavior.
func DetectVendor(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "dashscope.aliyuncs.com", strings.HasSuffix(host, ".dashscope.aliyuncs.com"), strings.HasSuffix(host, ".maas.aliyuncs.com"):
		return "dashscope"
	case host == "api.deepseek.com", strings.HasSuffix(host, ".deepseek.com"):
		return "deepseek"
	case host == "api.xiaomimimo.com", strings.HasSuffix(host, ".xiaomimimo.com"):
		return "mimo"
	default:
		return ""
	}
}

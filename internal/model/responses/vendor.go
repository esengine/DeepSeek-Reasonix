package responses

import (
	"net/url"
	"strings"

	"reasonix/internal/contract/provider"
)

// vendorCapabilities describes how a Responses-compatible endpoint deviates
// from the base OpenAI Responses wire behavior. Vendors are detected from the
// base URL (DetectVendor); unknown endpoints get the zero value, which is the
// standard OpenAI-compatible behavior (stateful, no session-cache header, the
// schema's own summary shape, no tool-call reasoning retention, temperature
// honored). The zero value has to be what the published schema says, because
// it is what every endpoint no one has characterized will be sending.
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
	// truncating tool calls mid-JSON on long turns. Raise it to the next
	// documented tier (65536, within the allowed [1, 131072] range) so the
	// answer survives long reasoning.
	defaultMaxOutputTokens int

	// omitReasoningIdentity marks vendors that fold an input reasoning item
	// into the adjacent assistant message, leaving `id`/`status` nothing to
	// refer to (DeepSeek). OpenAI marks Reasoning.id required — zero sends it.
	omitReasoningIdentity bool

	// summary is the shape the `summary` field takes on an input reasoning
	// item. The zero value is the OpenAI base shape, so an endpoint nobody
	// has characterized still sends what the published schema asks for.
	summary summaryShape
}

// summaryShape is what an input reasoning item's `summary` must look like.
// OpenAI marks the field required, so the zero value carries it and leaves it
// empty: a gateway that validates against the published schema rejects the
// item outright when the field is missing ("missing required field summary"),
// and an endpoint nobody has characterized is exactly the one that must not
// depend on being in a table.
type summaryShape uint8

const (
	// summaryEmpty sends `summary: []` — the required field, carrying
	// nothing, so no reasoning text is repeated into a second place.
	summaryEmpty summaryShape = iota
	// summaryEcho repeats the reasoning into summary for vendors that read
	// it from nowhere else (DashScope, which otherwise rejects with
	// "Invalid 'summary': summary is required and must be a list").
	summaryEcho
	// summaryOmit drops the field for vendors whose reasoning item has no
	// summary in its schema (MiMo): there the extra field is folded back
	// into the model context, doubling the chain-of-thought each turn and
	// inflating reasoning output until it truncates.
	summaryOmit
)

var vendorTable = map[string]vendorCapabilities{
	"dashscope": {
		stateless:              false,
		sessionCacheHeader:     true,
		toolCallReasoning:      false,
		singleSegmentReasoning: false,
		ignoresTemperature:     false,
		summary:                summaryEcho,
	},
	"deepseek": {
		stateless:              true,
		sessionCacheHeader:     false,
		toolCallReasoning:      true,
		singleSegmentReasoning: false,
		ignoresTemperature:     false,
		omitReasoningIdentity:  true,
		// Auto ceiling for ordinary reasoning; high/max is applied via
		// AutoOutputBudget at construction/request time (64K). Never 128K.
		defaultMaxOutputTokens: provider.DefaultReasoningOutputTokens,
	},
	"mimo": {
		stateless:              true,
		sessionCacheHeader:     false,
		toolCallReasoning:      true,
		singleSegmentReasoning: true,
		ignoresTemperature:     true,
		summary:                summaryOmit,
		// Coding-agent default 32K; users may raise explicitly. Not 128K auto.
		defaultMaxOutputTokens: provider.DefaultReasoningOutputTokens,
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

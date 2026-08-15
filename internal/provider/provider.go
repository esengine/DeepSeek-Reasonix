// Package provider defines the model-backend abstraction and a registry mapping
// a provider "kind" to a factory. Concrete implementations live in subpackages
// (e.g. provider/openai) and self-register via init(). The core resolves
// providers by kind from config and never hardcodes a specific model.
package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode"

	"reasonix/internal/nilutil"
)

// Role is the role of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// LocalOnlyToolName/ID make display-only records safe when a newer transcript
// is opened by an older Reasonix binary that does not know Message.LocalOnly.
// Old wire normalization treats this unmatched tool result as an orphan and
// drops it instead of replaying partial content to the model.
const (
	LocalOnlyToolName = "__reasonix_local_only__"
	LocalOnlyToolID   = "__reasonix_local_only__"
)

// Message is a single conversation message.
type Message struct {
	Role Role `json:"role"`
	// Content is the provider-visible conversation content. Keeping this legacy
	// field provider-visible preserves replay for older CLI/Desktop releases.
	Content string `json:"content,omitempty"`
	// RawContent holds the full original when it differs from Content:
	// for user turns, the user-authored text before host-injected context;
	// for tool turns, the complete tool result when first-visible Content was
	// bounded. ModelMessages always clears it so provider serialization, prompt
	// cache hashes, and projection hashes never include it.
	RawContent string `json:"raw_content,omitempty"`
	// ProviderContent is a transitional field written by early Context Engine v2
	// builds. Loaders migrate it into Content/RawContent before normal use.
	ProviderContent  string   `json:"provider_content,omitempty"`
	Images           []string `json:"images,omitempty"`            // data URLs (data:<mime>;base64,…) on user (attachments) and tool (MCP image results) messages; embedded only for vision-capable models
	ReasoningContent string   `json:"reasoning_content,omitempty"` // assistant: thinking-mode chain-of-thought, round-tripped on multi-turn
	// ReasoningID is the provider-issued reasoning-item id (OpenAI Responses:
	// Reasoning.id is required on input items), captured from the streamed
	// output item and round-tripped back into later inputs.
	ReasoningID string `json:"reasoning_id,omitempty"`
	// ReasoningStatus is the final status of the reasoning item
	// ("in_progress" | "completed") as issued by the server's done event,
	// round-tripped back into the input alongside ReasoningID.
	ReasoningStatus string `json:"reasoning_status,omitempty"`
	// ReasoningSignature is an opaque, provider-issued proof that ReasoningContent
	// is genuine model output. Anthropic requires the signed thinking block be
	// replayed on the next turn when a tool call followed thinking; providers
	// without signed reasoning (e.g. the openai-compatible ones) leave it empty.
	// Round-tripped alongside ReasoningContent.
	ReasoningSignature string     `json:"reasoning_signature,omitempty"`
	ToolCalls          []ToolCall `json:"tool_calls,omitempty"` // set by assistant
	// ResponsesItems preserves provider-issued Responses API output items for
	// stateless replay. omitempty keeps old session files byte-compatible.
	ResponsesItems  []json.RawMessage  `json:"responses_items,omitempty"`
	ServerSearch    []ServerSearchCall `json:"server_search,omitempty"`   // cards + Anthropic replay; omitempty
	ToolCallID      string             `json:"tool_call_id,omitempty"`    // links a tool result to its call
	Name            string             `json:"name,omitempty"`            // tool message: tool name
	MemoryCitations []MemoryCitation   `json:"memoryCitations,omitempty"` // local UI metadata; provider requests ignore it
	WorkDurationMs  int64              `json:"workDurationMs,omitempty"`  // local UI metadata; provider requests ignore it
	CreatedAt       int64              `json:"createdAt,omitempty"`       // local UI metadata; unix milliseconds; stripped before provider requests
	Edited          bool               `json:"edited,omitempty"`          // local UI metadata; provider requests ignore it
	Original        string             `json:"original,omitempty"`        // user prompt before inline edit
	// LocalOnly marks durable transcript content that must never be sent to a
	// model provider. Interrupted streaming output uses it so every frontend can
	// replay what the user saw without feeding partial reasoning or tool-call
	// arguments back into the next request.
	LocalOnly       bool             `json:"local_only,omitempty"`
	DecisionReceipt *DecisionReceipt `json:"decision_receipt,omitempty"`
	// DecisionReceipts are local-only metadata attached to a provider-visible
	// message. Keeping them on the existing assistant record preserves the
	// assistant/tool-result adjacency required by current and older readers.
	// ModelMessages strips the field before handing requests to providers.
	DecisionReceipts []*DecisionReceipt       `json:"decision_receipts,omitempty"`
	InterruptedTurn  *InterruptedTurnRecovery `json:"interrupted_turn,omitempty"`
	// FinalReadinessRecovery is durable host state on a LocalOnly sentinel.
	// ModelMessages removes it before provider serialization.
	FinalReadinessRecovery *FinalReadinessRecovery `json:"final_readiness_recovery,omitempty"`
	// ToolExecution is local shell UI metadata on tool-result messages. It is
	// persisted for Desktop/CLI/Serve cards and stripped by ModelMessages before
	// any provider request so tool schemas and prompt-cache prefixes stay stable.
	ToolExecution *ToolExecution `json:"tool_execution,omitempty"`
}

// ToolExecution is host-local shell metadata mirrored from tool.ShellExecution.
// Provider serializers must never emit this object on the wire.
type ToolExecution struct {
	Kind           string `json:"kind,omitempty"`
	Shell          string `json:"shell,omitempty"`
	ShellVersion   string `json:"shellVersion,omitempty"`
	Platform       string `json:"platform,omitempty"`
	SupportsAndAnd bool   `json:"supportsAndAnd"`
	State          string `json:"state,omitempty"`
	FailurePhase   string `json:"failurePhase,omitempty"`
	ExitCode       *int   `json:"exitCode,omitempty"`
	OutputTail     string `json:"outputTail,omitempty"`
	MutationRisk   string `json:"mutationRisk,omitempty"`
	Verification   string `json:"verification,omitempty"`
	DurationMs     int64  `json:"durationMs,omitempty"`
}

// DecisionReceipt is durable, provider-excluded evidence of a user-owned
// approval decision. It intentionally contains only bounded labels and the
// outcome, never free-form guidance or provider-visible content.
type DecisionReceipt struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool,omitempty"`
	Subject string `json:"subject,omitempty"`
	Outcome string `json:"outcome"`
}

// InterruptedTurnRecovery is the durable, provider-excluded handoff for a turn
// that stopped before producing a clean final answer. It contains only bounded
// structural facts; raw partial reasoning remains on the LocalOnly Message for
// display and is never copied into the recovery prompt.
type InterruptedTurnRecovery struct {
	Pending                 bool                     `json:"pending,omitempty"`
	CompletedTools          []InterruptedToolSummary `json:"completed_tools,omitempty"`
	InterruptedTools        []string                 `json:"interrupted_tools,omitempty"`
	DroppedPartialText      bool                     `json:"dropped_partial_text,omitempty"`
	DroppedPartialReasoning bool                     `json:"dropped_partial_reasoning,omitempty"`
}

// InterruptedToolSummary records a completed, fully paired tool call without
// duplicating its arguments or result. The canonical assistant/tool messages
// immediately before the recovery record remain the source of truth.
type InterruptedToolSummary struct {
	ID      string   `json:"id,omitempty"`
	Name    string   `json:"name"`
	Files   []string `json:"files,omitempty"`
	Added   int      `json:"added,omitempty"`
	Removed int      `json:"removed,omitempty"`
}

// MemoryCitation is local display metadata for memories that influenced an
// assistant turn. Provider implementations must not forward it to model APIs.
type MemoryCitation struct {
	ID        string `json:"id,omitempty"`
	Source    string `json:"source"`
	LineStart int    `json:"lineStart,omitempty"`
	LineEnd   int    `json:"lineEnd,omitempty"`
	Note      string `json:"note,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

// ParseImageDataURL splits a `data:<media-type>;base64,<payload>` URL into its
// media type and base64 payload. ok is false for anything that isn't a base64
// data URL — providers that need the split (Anthropic) skip those silently.
func ParseImageDataURL(dataURL string) (mediaType, base64Data string, ok bool) {
	rest, found := strings.CutPrefix(dataURL, "data:")
	if !found {
		return "", "", false
	}
	meta, payload, found := strings.Cut(rest, ",")
	if !found {
		return "", "", false
	}
	mt, found := strings.CutSuffix(meta, ";base64")
	if !found || mt == "" {
		return "", "", false
	}
	return mt, payload, true
}

// ToolCall is a tool invocation requested by the model. Arguments is raw JSON.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	// ThoughtSignature is an opaque Gemini-issued proof attached to a function
	// call. OpenAI-compatible Gemini endpoints require it on message replay.
	ThoughtSignature string `json:"thought_signature,omitempty"`
	Diff             string `json:"diff,omitempty"`
	Added            int    `json:"added,omitempty"`
	Removed          int    `json:"removed,omitempty"`
	// Resolved* fields are Reasonix-local display metadata for stable proxy
	// calls such as use_capability. Provider request builders deliberately
	// serialize only provider-visible fields, so these values never alter the
	// provider-visible conversation or prompt-cache prefix.
	ResolvedName     string `json:"resolved_name,omitempty"`
	CapabilityID     string `json:"capability_id,omitempty"`
	ResolvedReadOnly *bool  `json:"resolved_read_only,omitempty"`
}

// ToolSchema is a tool definition exposed to the model. Parameters is JSON Schema.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Request is a single completion request.
type Request struct {
	Messages    []Message
	Tools       []ToolSchema
	Temperature *float64 // nil = omit; non-nil = send the value, including 0
	MaxTokens   int
	// ResponseFormat, when non-nil, asks the endpoint for structured JSON
	// output (Responses: text.format.type=json_object). Nil omits the field
	// entirely — the common path must stay byte-stable for prompt caching.
	ResponseFormat *ResponseFormat `json:"ResponseFormat,omitempty"`
	EffortOverride string          `json:"EffortOverride,omitempty"` // per-call reasoning-depth override; adapters apply it only when the endpoint's effort vocabulary accepts it
}

// ResponseFormat asks a provider to constrain its output shape.
type ResponseFormat struct {
	// Type is the structured format: "json_object" is the only shape the
	// Responses endpoints currently define (MiMo/DashScope/OpenAI).
	Type string `json:"type"`
}

// Auto ladder for max_output_tokens=0. Bounds completion only; never compact_ratio.
// Official DeepSeek does not use this ladder: Chat/Responses omit the field
// (server 384K ceiling) and Anthropic sends DeepSeekMaxOutputTokens.
const (
	DefaultOrdinaryOutputTokens      = 16 * 1024  // non-reasoning / non-DeepSeek
	DefaultReasoningOutputTokens     = 32 * 1024  // ordinary reasoning / MiMo
	DefaultHighReasoningOutputTokens = 64 * 1024  // high/max effort on non-DeepSeek
	DefaultHighOutputTokens          = 128 * 1024 // explicit only; never auto
	// DeepSeekMaxOutputTokens is the official V4 Flash/Pro completion ceiling.
	// Pricing page: 输出长度最大 384K. K is decimal thousands, matching the
	// documented 1M context = 1,000,000 tokens. Anthropic requires max_tokens.
	DeepSeekMaxOutputTokens = 384_000
)

// AutoOutputBudget maps max_output_tokens=0 to 16K/32K/64K for non-DeepSeek
// vendors. Official DeepSeek omits the field (Chat/Responses) or sends
// DeepSeekMaxOutputTokens (Anthropic).
func AutoOutputBudget(reasoningEnabled bool, effort string) int {
	if !reasoningEnabled {
		return DefaultOrdinaryOutputTokens
	}
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "high", "max":
		return DefaultHighReasoningOutputTokens
	default:
		return DefaultReasoningOutputTokens
	}
}

// TemperaturePtr wraps v in a pointer so callers that explicitly want a
// specific temperature, including 0 for deterministic output, can distinguish
// that intent from "not set, use the provider default".
func TemperaturePtr(v float64) *float64 { return &v }

// OptionalTemperature returns nil when v is zero, matching the historical
// config behavior where 0 meant "not configured", and a pointer otherwise.
func OptionalTemperature(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

// interruptedToolResult stands in for a tool result that never landed — an
// assistant tool_calls turn whose execution was cut short (interrupt, crash) and
// later resumed. Sending such a turn unanswered trips the OpenAI/DeepSeek 400
// "An assistant message with 'tool_calls' must be followed by tool messages
// responding to each 'tool_call_id'".
const interruptedToolResult = "[no result: the previous turn was interrupted before this tool call completed]"

// SanitizeToolPairing is the provider-side alias for NormalizeMessages. It repairs
// a history so it satisfies the tool-call contract the OpenAI-compatible and
// Anthropic APIs enforce (every assistant tool_calls answered, no orphan tool
// messages, truncated args closed) right before sending it to the wire — without
// touching the stored session. Kept as a distinct name so call sites read as
// "defensive wire prep" rather than "session mutation".
func SanitizeToolPairing(msgs []Message) []Message { return NormalizeMessages(msgs) }

// NormalizeMessages repairs a conversation history so it satisfies the tool-call
// contract the OpenAI-compatible and Anthropic APIs enforce: every assistant
// tool_calls entry must be answered by a following tool message for its id, and a
// tool message must follow such a call. It backfills a placeholder result for any
// unanswered call (so the turn stays intact), drops orphan tool messages,
// backfills empty tool-call names from their results (#4727 — old sessions saved
// before adde2d3e can carry an empty name), and closes truncated call-argument
// JSON (DeepSeek 400s on replayed half-streamed args, #3953).
//
// This is the wire-safe entry point for provider requests. Stored session loads
// use NormalizeSessionMessages so they can share the assistant-turn repairs
// without deleting standalone tool messages that must round-trip through
// reasonix --resume.
//
// A well-formed history — no unanswered calls, no orphan results, no empty tool-
// call names, no truncated args — returns the input slice unchanged (same backing
// array, zero allocation). This keeps the prefix-cache key stable for healthy
// sessions and makes repeated normalization cheap.
func NormalizeMessages(msgs []Message) []Message {
	return normalizeMessages(msgs, true)
}

// NormalizeSessionMessages applies only repairs that are safe to persist in a
// saved session. It shares assistant-turn repairs with NormalizeMessages, but
// preserves existing tool messages instead of dropping or reordering them so
// Save/LoadSession remains a byte-for-byte conversation round trip for histories
// that were already on disk.
func NormalizeSessionMessages(msgs []Message) []Message {
	return normalizeMessages(attachStandaloneDecisionReceipts(msgs), false)
}

// attachStandaloneDecisionReceipts migrates the short-lived receipt encoding
// that stored a LocalOnly assistant message between an assistant tool call and
// its result. Folding that metadata into the latest assistant message repairs
// already-written sessions before tool-pair normalization can fabricate a
// placeholder. Healthy histories return the original slice unchanged.
func attachStandaloneDecisionReceipts(msgs []Message) []Message {
	target := -1
	needsMigration := false
	for i, m := range msgs {
		switch {
		case m.Role == RoleUser && !m.LocalOnly:
			target = -1
		case m.Role == RoleAssistant && !m.LocalOnly:
			target = i
		case target >= 0 && m.LocalOnly && m.DecisionReceipt != nil:
			needsMigration = true
		}
		if needsMigration {
			break
		}
	}
	if !needsMigration {
		return msgs
	}

	out := make([]Message, 0, len(msgs))
	target = -1
	for _, m := range msgs {
		switch {
		case m.Role == RoleUser && !m.LocalOnly:
			target = -1
		case m.Role == RoleAssistant && !m.LocalOnly:
			out = append(out, m)
			target = len(out) - 1
			continue
		case target >= 0 && m.LocalOnly && m.DecisionReceipt != nil:
			receipts := append([]*DecisionReceipt(nil), out[target].DecisionReceipts...)
			out[target].DecisionReceipts = append(receipts, m.DecisionReceipt)
			continue
		}
		out = append(out, m)
	}
	return out
}

func normalizeMessages(msgs []Message, dropOrphanTools bool) []Message {
	if normalized, ok := tryNormalizeFastPath(msgs, dropOrphanTools); ok {
		return normalized // well-formed: pass through without allocating
	}
	out := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.LocalOnly {
			if !dropOrphanTools {
				out = append(out, m)
			}
			i++
			continue
		}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == RoleTool && !msgs[j].LocalOnly {
				j++
			}
			// Backfill empty tool-call names from the corresponding tool
			// results so the model sees which tool was invoked (#4727).
			// The wire-format fix (openai.go) ensures empty fields are
			// never omitted, so this backfill is a UX improvement, not a
			// correctness requirement.
			calls := backfillToolCallNames(m.ToolCalls, msgs[i+1:j])
			m.ToolCalls = calls
			out = append(out, repairToolCallArgs(m))
			if dropOrphanTools {
				out = append(out, pairToolResults(calls, msgs[i+1:j])...)
			} else {
				out = append(out, sessionToolResults(calls, msgs[i+1:j])...)
			}
			i = j
			continue
		}
		if m.Role == RoleTool {
			if !dropOrphanTools {
				out = append(out, m)
			}
			// Orphan tool message: provider sends drop it; session loads preserve it.
			i++
			continue
		}
		out = append(out, m)
		i++
	}
	return out
}

// tryNormalizeFastPath reports whether msgs needs no repair and, if so, returns
// it as-is so the caller can skip allocating. Healthy tool-call/tool-result
// turns pass through unchanged; malformed turns take the slow path.
func tryNormalizeFastPath(msgs []Message, dropOrphanTools bool) ([]Message, bool) {
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.LocalOnly {
			if dropOrphanTools {
				return nil, false
			}
			i++
			continue
		}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == RoleTool && !msgs[j].LocalOnly {
				j++
			}
			if !toolTurnWellFormed(m.ToolCalls, msgs[i+1:j]) || needsToolCallArgRepair(m.ToolCalls) {
				return nil, false
			}
			i = j
			continue
		}
		if m.Role == RoleTool && dropOrphanTools {
			return nil, false
		}
		i++
	}
	return msgs, true
}

func toolTurnWellFormed(calls []ToolCall, results []Message) bool {
	if len(calls) != len(results) {
		return false
	}
	for _, tc := range calls {
		if tc.Name == "" {
			return false
		}
	}
	for k, tc := range calls {
		if results[k].ToolCallID != tc.ID {
			return false
		}
		if results[k].Name != tc.Name {
			return false
		}
	}
	return true
}

func needsToolCallArgRepair(calls []ToolCall) bool {
	for _, tc := range calls {
		if tc.Arguments != "" && !json.Valid([]byte(tc.Arguments)) {
			return true
		}
	}
	return false
}

// repairToolCallArgs returns m with any undecodable tool-call Arguments closed
// into valid JSON (copy-on-write; the caller's history is never mutated). Empty
// arguments pass through — some gateways send "" for no-arg tools.
func repairToolCallArgs(m Message) Message {
	broken := false
	for _, tc := range m.ToolCalls {
		if tc.Arguments != "" && !json.Valid([]byte(tc.Arguments)) {
			broken = true
			break
		}
	}
	if !broken {
		return m
	}
	calls := make([]ToolCall, len(m.ToolCalls))
	copy(calls, m.ToolCalls)
	for i := range calls {
		if calls[i].Arguments == "" || json.Valid([]byte(calls[i].Arguments)) {
			continue
		}
		calls[i].Arguments = closeTruncatedJSON(calls[i].Arguments)
	}
	m.ToolCalls = calls
	return m
}

// closeTruncatedJSON best-effort completes a JSON document cut off mid-stream
// (unterminated string, open braces, dangling comma/colon); anything still
// invalid after closing degrades to "{}".
func closeTruncatedJSON(s string) string {
	var stack []byte
	inStr, esc := false, false
	for i := range len(s) {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	out := s
	if esc {
		out = out[:len(out)-1]
	}
	if inStr {
		out += `"`
	}
	trimmed := strings.TrimRight(out, " \t\r\n")
	switch {
	case strings.HasSuffix(trimmed, ","):
		out = trimmed[:len(trimmed)-1]
	case strings.HasSuffix(trimmed, ":"):
		out = trimmed + "null"
	}
	for _, v := range slices.Backward(stack) {
		out += string(v)
	}
	if !json.Valid([]byte(out)) {
		return "{}"
	}
	return out
}

// pairToolResults answers each tool_call with its result, backfilling a
// placeholder for any unanswered one. Distinct non-empty ids pair by id (so
// reordered results re-sort to call order); empty or duplicate ids pair by
// position instead — some gateways stream tool calls by index with no id, and a
// map keyed on id would collapse those results into one (call order is preserved
// because the loop appends results in call order).
func pairToolResults(calls []ToolCall, avail []Message) []Message {
	out := make([]Message, 0, len(calls))
	if idDistinct(calls) {
		byID := make(map[string]Message, len(avail))
		for _, r := range avail {
			byID[r.ToolCallID] = r
		}
		for _, tc := range calls {
			if r, ok := byID[tc.ID]; ok {
				r.Name = tc.Name
				out = append(out, r)
			} else {
				out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
			}
		}
		return out
	}
	for k, tc := range calls {
		if k < len(avail) {
			r := avail[k]
			r.ToolCallID = tc.ID
			r.Name = tc.Name
			out = append(out, r)
		} else {
			out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
		}
	}
	return out
}

// sessionToolResults preserves every stored tool result and appends placeholders
// only for calls that have no recorded answer. Load-time normalization must not
// drop or reorder user history; provider sends can still use pairToolResults for
// strict wire formatting.
func sessionToolResults(calls []ToolCall, avail []Message) []Message {
	out := append([]Message(nil), avail...)
	if idDistinct(calls) {
		answered := make(map[string]struct{}, len(avail))
		for _, r := range avail {
			answered[r.ToolCallID] = struct{}{}
		}
		for _, tc := range calls {
			if _, ok := answered[tc.ID]; !ok {
				out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
			}
		}
		return out
	}
	for k := len(avail); k < len(calls); k++ {
		tc := calls[k]
		out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
	}
	return out
}

// backfillToolCallNames returns calls with any empty Name filled in from the
// matching tool result (by id, then by position). Old sessions (#4727) may have
// saved assistant tool-calls with an empty name; backfilling gives the model
// useful context during replay. The common case (no empty names) returns the
// input unchanged without allocating. Unpaired calls keep their empty name,
// which the wire-format fix (openai.go) handles gracefully.
func backfillToolCallNames(calls []ToolCall, results []Message) []ToolCall {
	missing := false
	for _, c := range calls {
		if c.Name == "" {
			missing = true
			break
		}
	}
	if !missing {
		return calls
	}
	out := make([]ToolCall, len(calls))
	copy(out, calls)
	if idDistinct(calls) {
		byID := make(map[string]string, len(results))
		for _, r := range results {
			if r.Name != "" {
				byID[r.ToolCallID] = r.Name
			}
		}
		for k := range out {
			if out[k].Name == "" {
				if n, ok := byID[out[k].ID]; ok {
					out[k].Name = n
				}
			}
		}
		return out
	}
	// Fallback: positional pairing (same order as pairToolResults).
	for k := range out {
		if out[k].Name == "" && k < len(results) {
			out[k].Name = results[k].Name
		}
	}
	return out
}

// idDistinct reports whether every call carries a non-empty id unique within the
// batch — the condition under which id-keyed pairing is safe.
func idDistinct(calls []ToolCall) bool {
	seen := make(map[string]struct{}, len(calls))
	for _, tc := range calls {
		if tc.ID == "" {
			return false
		}
		if _, dup := seen[tc.ID]; dup {
			return false
		}
		seen[tc.ID] = struct{}{}
	}
	return true
}

// ChunkType identifies the kind of a streamed increment.
type ChunkType int

const (
	ChunkText              ChunkType = iota // text delta
	ChunkReasoning                          // thinking-mode reasoning delta (before the visible answer)
	ChunkToolCallStart                      // a tool call has begun (ToolCall: ID+Name; args still streaming)
	ChunkToolCallArgsDelta                  // progress while a call's arguments stream (ToolCall: ID+Name; ArgChars: cumulative)
	ChunkToolCall                           // one complete tool call
	ChunkUsage                              // token usage for the completion
	ChunkDone                               // completion finished normally
	ChunkError                              // an error occurred
	ChunkResponsesItem                      // a complete provider-issued Responses API output item for stateless replay
	ChunkServerSearch                       // provider-executed web_search; not a client tool call
)

// Usage reports token accounting for a completion. Cache hit/miss come from
// either DeepSeek's top-level prompt_cache_{hit,miss}_tokens or the OpenAI/MiMo
// standard prompt_tokens_details.cached_tokens — the openai provider normalises
// both shapes into these fields. ReasoningTokens is the thinking-mode subset of
// CompletionTokens reported by thinking-capable models. FinishReason carries
// the model's last reported choices[0].finish_reason so the agent can surface
// abnormal terminations ("length", "content_filter", "repetition_truncation").
// Estimated marks counts reconstructed locally because the provider's terminal
// usage record did not arrive; exact provider usage leaves it false.
type Usage struct {
	PromptTokens           int
	CompletionTokens       int
	TotalTokens            int
	CacheHitTokens         int     // prompt tokens served from cache
	CacheMissTokens        int     // prompt tokens not cached, including CacheWriteTokens
	CacheWriteTokens       int     // subset of CacheMissTokens used to create provider cache entries
	CacheWriteBilledTokens float64 // cache-write charge expressed in ordinary input-token equivalents
	ReasoningTokens        int     // subset of CompletionTokens spent on chain-of-thought
	FinishReason           string  // "stop", "tool_calls", "length", "content_filter", "repetition_truncation", …
	Estimated              bool
	// RequestCount is the number of provider requests represented by this
	// aggregate. Zero means one request for backward compatibility. Recovery
	// paths that merge multiple attempts set the exact count.
	RequestCount int
	// Context* fields describe the latest single-request shape for context
	// gauges and rebind telemetry. When zero, consumers fall back to the
	// billable Prompt/Completion/… fields. Multi-attempt sampling recovery
	// sets PromptTokens (etc.) to the billable aggregate and fills Context*
	// from the final attempt only.
	ContextPromptTokens     int
	ContextCompletionTokens int
	ContextReasoningTokens  int
	ContextCacheHitTokens   int
	ContextCacheMissTokens  int
}

// ContextFillTokens returns the latest prompt occupancy used by context gauges.
func (u *Usage) ContextFillTokens() int {
	return u.LatestPromptTokens()
}

// LatestPromptTokens returns the latest-attempt prompt size for context-aware
// runtime decisions. Falls back to PromptTokens for single-attempt legacy usage.
func (u *Usage) LatestPromptTokens() int {
	if u == nil {
		return 0
	}
	if u.ContextPromptTokens > 0 {
		return u.ContextPromptTokens
	}
	return u.PromptTokens
}

// Pricing is a provider's per-1M-token rates, used to estimate spend. Currency
// is a display symbol or ISO-like code (default "¥"). toml tags let config decode it.
type Pricing struct {
	CacheHit float64 `toml:"cache_hit"` // per 1M cached prompt tokens
	Input    float64 `toml:"input"`     // per 1M uncached prompt tokens
	Output   float64 `toml:"output"`    // per 1M completion tokens
	Currency string  `toml:"currency"`
}

// Cost estimates the spend for a usage record. Compatibility adapter only —
// new host code must consume billing.CostQuote instead of aggregating floats.
func (p *Pricing) Cost(u *Usage) float64 {
	if p == nil || u == nil {
		return 0
	}
	// Keep the historical float path byte-stable for tests that assert exact
	// float results without going through the fixed-point quote layer.
	hit := u.CacheHitTokens
	miss := u.CacheMissTokens
	if hit+miss == 0 && u.PromptTokens > 0 {
		miss = u.PromptTokens
	} else if miss == 0 && hit > 0 && u.PromptTokens > hit {
		miss = u.PromptTokens - hit
	}
	// CacheMissTokens intentionally remains the raw prompt-token denominator
	// used by cache hit-rate displays, so cache writes are included there. For
	// cost, split those writes back out and replace them with their provider-
	// supplied input-token equivalent (for example Anthropic's 1.25x 5-minute
	// writes or 2x 1-hour writes). Older providers leave both fields at zero and
	// keep the legacy one-input-rate behavior. A write count without billed
	// units also falls back to 1x for backward compatibility.
	write := min(max(u.CacheWriteTokens, 0), miss)
	billedWrite := 0.0
	if write > 0 {
		billedWrite = u.CacheWriteBilledTokens
		if billedWrite <= 0 {
			billedWrite = float64(write)
		}
	}
	inputTokenUnits := float64(miss-write) + billedWrite
	return (float64(hit)*p.CacheHit +
		inputTokenUnits*p.Input +
		float64(u.CompletionTokens)*p.Output) / 1e6
}

// Symbol returns the currency display symbol, defaulting to "¥".
func (p *Pricing) Symbol() string {
	if p == nil || p.Currency == "" {
		return "¥"
	}
	return currencySymbol(p.Currency)
}

func currencySymbol(currency string) string {
	value := strings.TrimSpace(currency)
	if value == "" {
		return "¥"
	}
	switch strings.ToLower(value) {
	case "cny", "rmb", "yuan", "renminbi", "cnh":
		return "¥"
	case "usd", "dollar", "dollars", "us dollar", "us dollars", "us$":
		return "$"
	case "eur", "euro", "euros":
		return "€"
	case "gbp", "pound", "pounds", "sterling":
		return "£"
	case "jpy", "yen":
		return "¥"
	}
	switch value {
	case "￥", "¥":
		return "¥"
	case "$", "€", "£":
		return value
	}
	// any embedded currency sign → keep as-is (compact symbols like A$, HK$).
	for _, r := range value {
		if unicode.Is(unicode.Sc, r) {
			return value
		}
	}
	if isThreeLetterCurrencyCode(value) {
		return strings.ToUpper(value) + " "
	}
	return "¥"
}

func isThreeLetterCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// Chunk is a single streamed event. Read the field matching Type.
type Chunk struct {
	Type      ChunkType
	Text      string // ChunkText, ChunkReasoning
	Signature string // ChunkReasoning: opaque proof for the reasoning (Anthropic thinking signature), when issued
	// ReasoningID/ReasoningStatus ride the final ChunkReasoning of a turn
	// (empty Text): the provider-issued reasoning item id/status captured
	// from the SSE stream, so the Agent can persist them into the session
	// and the next turn's input reasoning item round-trips them (review
	// #7234 — OpenAI Responses schema marks Reasoning.id required).
	ReasoningID     string            // ChunkReasoning: provider-issued reasoning item id
	ReasoningStatus string            // ChunkReasoning: final reasoning item status ("completed")
	ToolCall        *ToolCall         // ChunkToolCallStart (ID+Name only), ChunkToolCallArgsDelta (ID+Name), ChunkToolCall (complete)
	ArgChars        int               // ChunkToolCallArgsDelta: cumulative argument characters received for this call
	ResponsesItem   json.RawMessage   // ChunkResponsesItem: opaque validated Responses API output item
	ServerSearch    *ServerSearchCall // ChunkServerSearch: display card + replay payload
	Usage           *Usage            // ChunkUsage
	Err             error             // ChunkError
}

// Fixed stream-interrupt reasons for observability. Values are a closed enum
// and must never carry URLs, tool arguments, file paths, or raw error text.
const (
	StreamInterruptConnectionReset = "connection_reset"
	StreamInterruptPrematureEOF    = "premature_eof"
	StreamInterruptIdleTimeout     = "idle_timeout"
)

// StreamInterruptedError marks that the current sampling attempt never reached
// a clean provider terminal event and is therefore uncommitted. The Agent may
// replay the exact same provider request. Providers must not perform body-phase
// request replay themselves — that lives at the Agent layer so retry budgets,
// UI rollback, and tool execution stay single-owner. context.Canceled, auth,
// 4xx/schema errors, and unparseable complete protocol payloads must not use
// this type.
type StreamInterruptedError struct {
	Err    error
	Reason string // one of the StreamInterrupt* constants; may be empty for older callers
}

func (e *StreamInterruptedError) Error() string {
	if e == nil || e.Err == nil {
		return "stream interrupted"
	}
	return e.Err.Error()
}

func (e *StreamInterruptedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// StreamInterrupt wraps err as a StreamInterruptedError with a fixed reason.
func StreamInterrupt(err error, reason string) error {
	if err == nil {
		return nil
	}
	return &StreamInterruptedError{Err: err, Reason: reason}
}

// StreamInterruptReason returns the fixed reason when err is a stream
// interruption, or empty otherwise.
func StreamInterruptReason(err error) string {
	var interrupted *StreamInterruptedError
	if !errors.As(err, &interrupted) || interrupted == nil {
		return ""
	}
	if interrupted.Reason != "" {
		return interrupted.Reason
	}
	return ClassifyStreamInterrupt(interrupted.Err)
}

// ClassifyStreamInterrupt maps a transport error onto a fixed reason enum.
// Prefer attaching Reason at the emit site; this is a best-effort fallback.
func ClassifyStreamInterrupt(err error) string {
	if err == nil {
		return StreamInterruptPrematureEOF
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "stalled") || strings.Contains(msg, "idle timeout") || strings.Contains(msg, "no data for"):
		return StreamInterruptIdleTimeout
	case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || strings.Contains(msg, "before completion") || strings.Contains(msg, "unexpected eof"):
		return StreamInterruptPrematureEOF
	case errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) ||
		strings.Contains(msg, "connection reset") || strings.Contains(msg, "forcibly closed") || strings.Contains(msg, "broken pipe"):
		return StreamInterruptConnectionReset
	default:
		if IsConnReset(err) {
			return StreamInterruptConnectionReset
		}
		return StreamInterruptPrematureEOF
	}
}

func IsStreamInterrupted(err error) bool {
	var interrupted *StreamInterruptedError
	return errors.As(err, &interrupted)
}

// Provider is a chat-capable model backend.
type Provider interface {
	// Name returns the provider instance name, e.g. "deepseek" / "mimo".
	Name() string
	// Stream starts a streaming completion, pushing increments on the channel.
	// Cancelling ctx must abort the underlying request; a closed channel marks
	// the end of the completion.
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// ToolCallReasoningPolicy is optionally implemented by providers whose protocol
// replays the provider-issued reasoning block on assistant tool_calls turns
// (DeepSeek thinking mode). The agent uses it to archive the original reasoning
// text on those turns (a display-translated copy must not round-trip to the
// API) and to warn when a turn arrives with none — the request still succeeds
// because the wire layer always emits the reasoning_content key for such turns,
// but the model loses its chain-of-thought context. Most providers leave this
// unset; callers must treat it as false.
type ToolCallReasoningPolicy interface {
	RequiresToolCallReasoning() bool
}

// RequiresToolCallReasoning reports whether p replays reasoning_content on
// assistant tool_calls turns sent back in history.
func RequiresToolCallReasoning(p Provider) bool {
	if nilutil.IsNil(p) {
		return false
	}
	policy, ok := p.(ToolCallReasoningPolicy)
	return ok && policy.RequiresToolCallReasoning()
}

// ReasoningRoundTripPolicy is optionally implemented by providers that require
// every assistant message to preserve provider-issued reasoning in later
// requests. This is broader than ToolCallReasoningPolicy, which covers only
// assistant tool_calls turns.
type ReasoningRoundTripPolicy interface {
	RequiresReasoningRoundTrip() bool
}

// RequiresReasoningRoundTrip reports whether raw provider reasoning must be
// retained and replayed on all assistant messages.
func RequiresReasoningRoundTrip(p Provider) bool {
	if nilutil.IsNil(p) {
		return false
	}
	policy, ok := p.(ReasoningRoundTripPolicy)
	return ok && policy.RequiresReasoningRoundTrip()
}

// MissingToolCallReasoningWarningPolicy is optionally implemented by providers
// whose replay protocol requires reasoning_content, but whose active model may
// not reliably emit it. The legacy Warning name is retained for source
// compatibility; the agent now uses this policy for silent bounded recovery and
// emits no user-visible protocol notice.
type MissingToolCallReasoningWarningPolicy interface {
	WarnOnMissingToolCallReasoning() bool
}

// MissingToolCallReasoningWarningIdentityPolicy optionally supplies the stable,
// non-credential configuration identity used to rate-limit missing-reasoning
// recovery attempts. The legacy name preserves adapters and persisted state.
// Implementations may include adapter kind, endpoint, model, and thinking
// controls; the raw identity never leaves memory and is hashed before
// persistence.
type MissingToolCallReasoningWarningIdentityPolicy interface {
	MissingToolCallReasoningWarningIdentity() string
}

// WarnOnMissingToolCallReasoning reports whether a tool_calls turn with empty
// reasoning_content should enter silent recovery. Its legacy name is preserved
// for provider implementations compiled against the original diagnostic API.
func WarnOnMissingToolCallReasoning(p Provider) bool {
	if nilutil.IsNil(p) {
		return false
	}
	policy, ok := p.(MissingToolCallReasoningWarningPolicy)
	if ok {
		return policy.WarnOnMissingToolCallReasoning()
	}
	return RequiresToolCallReasoning(p)
}

// MissingToolCallReasoningWarningFingerprint returns an opaque stable key for
// one provider configuration's recovery cooldown. Concrete adapters distinguish
// endpoint/model/protocol changes; providers without the optional policy retain
// a safe type-and-name fallback. The legacy name preserves the on-disk state
// contract. The digest prevents local state from exposing raw endpoints or
// model identifiers.
func MissingToolCallReasoningWarningFingerprint(p Provider) string {
	if nilutil.IsNil(p) {
		return ""
	}
	identity := fmt.Sprintf("%T\x00%s", p, strings.TrimSpace(p.Name()))
	if policy, ok := p.(MissingToolCallReasoningWarningIdentityPolicy); ok {
		if configured := strings.TrimSpace(policy.MissingToolCallReasoningWarningIdentity()); configured != "" {
			identity = configured
		}
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

// Config is a resolved provider instance configuration.
type Config struct {
	Name    string         // instance name, e.g. "deepseek"
	BaseURL string         // OpenAI-compatible endpoint
	Model   string         // model id
	APIKey  string         // resolved from api_key_env
	Extra   map[string]any // kind-specific options
}

// AuthError reports that a provider rejected the API key (HTTP 401/403). Its
// message is already user-facing and actionable — it names the provider and,
// when known, the environment variable the key comes from — and it carries the
// server's own reason as Body, because relay gateways explain *why* the key was
// rejected ("token expired", key not entitled to the model) in the response
// body. Body is deliberately NOT part of Error(): servers echo masked key
// fragments in auth bodies, and the ambient error string flows into logs,
// status lines, and traces where key material must never propagate. Display
// layers that want the reason read Body and extract it themselves. Providers
// should return this (rather than a generic status error) for auth failures.
type AuthError struct {
	Provider  string // the provider instance name, e.g. "deepseek"
	KeyEnv    string // the api_key_env the key is read from, when known
	KeySource string // human-readable source of KeyEnv, when known
	Status    int    // the HTTP status (401 or 403)
	HasKey    bool   // a non-empty key was sent — the server rejected it, vs. no key configured at all
	Body      string // trimmed response-body snippet, the server's verbatim reason when it gave one
}

func (e *AuthError) Error() string {
	key := "the API key"
	if e.KeyEnv != "" {
		key = e.KeyEnv
	}
	if e.KeySource != "" {
		key += " from " + e.KeySource
	}
	return fmt.Sprintf("authentication failed for provider %q (HTTP %d): %s is invalid or expired — update it (in .env or your environment) and retry, or run `reasonix setup`",
		e.Provider, e.Status, key)
}

// Factory builds a Provider from a resolved Config.
type Factory func(cfg Config) (Provider, error)

var registry = map[string]Factory{}

// Register adds a factory under a kind (e.g. "openai"). Intended for init().
// It panics on a duplicate kind, since that is a compile-time wiring mistake.
func Register(kind string, f Factory) {
	if _, dup := registry[kind]; dup {
		panic("provider: duplicate kind " + kind)
	}
	registry[kind] = f
}

// New instantiates the provider of the given kind.
func New(kind string, cfg Config) (Provider, error) {
	f, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("provider: unknown kind %q (registered: %v)", kind, Kinds())
	}
	p, err := f(cfg)
	if err != nil {
		return nil, err
	}
	if nilutil.IsNil(p) {
		return nil, fmt.Errorf("provider: factory %q returned nil provider", kind)
	}
	return p, nil
}

// Kinds returns the registered kinds, sorted.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

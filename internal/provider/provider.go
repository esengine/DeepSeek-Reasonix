// Package provider defines the model-backend abstraction and a registry mapping
// a provider "kind" to a factory. Concrete implementations live in subpackages
// (e.g. provider/openai) and self-register via init(). The core resolves
// providers by kind from config and never hardcodes a specific model.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
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

// Message is a single conversation message.
type Message struct {
	Role             Role     `json:"role"`
	Content          string   `json:"content,omitempty"`
	Images           []string `json:"images,omitempty"`            // data URLs (data:<mime>;base64,…); embedded only for vision-capable models
	ReasoningContent string   `json:"reasoning_content,omitempty"` // assistant: thinking-mode chain-of-thought, round-tripped on multi-turn
	// ReasoningSignature is an opaque, provider-issued proof that ReasoningContent
	// is genuine model output. Anthropic requires the signed thinking block be
	// replayed on the next turn when a tool call followed thinking; providers
	// without signed reasoning (e.g. the openai-compatible ones) leave it empty.
	// Round-tripped alongside ReasoningContent.
	ReasoningSignature string           `json:"reasoning_signature,omitempty"`
	ToolCalls          []ToolCall       `json:"tool_calls,omitempty"`      // set by assistant
	ToolCallID         string           `json:"tool_call_id,omitempty"`    // links a tool result to its call
	Name               string           `json:"name,omitempty"`            // tool message: tool name
	MemoryCitations    []MemoryCitation `json:"memoryCitations,omitempty"` // local UI metadata; provider requests ignore it
	Edited             bool             `json:"edited,omitempty"`          // local UI metadata; provider requests ignore it
	Original           string           `json:"original,omitempty"`        // user prompt before inline edit
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
	Diff      string `json:"diff,omitempty"`
	Added     int    `json:"added,omitempty"`
	Removed   int    `json:"removed,omitempty"`
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
	return normalizeMessages(msgs, false)
}

func normalizeMessages(msgs []Message, dropOrphanTools bool) []Message {
	if normalized, ok := tryNormalizeFastPath(msgs, dropOrphanTools); ok {
		return normalized // well-formed: pass through without allocating
	}
	out := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == RoleTool {
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
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == RoleTool {
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
	for i := 0; i < len(s); i++ {
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
	for i := len(stack) - 1; i >= 0; i-- {
		out += string(stack[i])
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
	ChunkText          ChunkType = iota // text delta
	ChunkReasoning                      // thinking-mode reasoning delta (before the visible answer)
	ChunkToolCallStart                  // a tool call has begun (ToolCall: ID+Name; args still streaming)
	ChunkToolCall                       // one complete tool call
	ChunkUsage                          // token usage for the completion
	ChunkDone                           // completion finished normally
	ChunkError                          // an error occurred
)

// Usage reports token accounting for a completion. Cache hit/miss come from
// either DeepSeek's top-level prompt_cache_{hit,miss}_tokens or the OpenAI/MiMo
// standard prompt_tokens_details.cached_tokens — the openai provider normalises
// both shapes into these fields. ReasoningTokens is the thinking-mode subset of
// CompletionTokens reported by thinking-capable models. FinishReason carries
// the model's last reported choices[0].finish_reason so the agent can surface
// abnormal terminations ("length", "content_filter", "repetition_truncation").
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheHitTokens   int    // prompt tokens served from cache
	CacheMissTokens  int    // prompt tokens not cached
	ReasoningTokens  int    // subset of CompletionTokens spent on chain-of-thought
	FinishReason     string // "stop", "tool_calls", "length", "content_filter", "repetition_truncation", …
}

// Pricing is a provider's per-1M-token rates, used to estimate spend. Currency
// is a display symbol or ISO-like code (default "¥"). toml tags let config decode it.
type Pricing struct {
	CacheHit float64 `toml:"cache_hit"` // per 1M cached prompt tokens
	Input    float64 `toml:"input"`     // per 1M uncached prompt tokens
	Output   float64 `toml:"output"`    // per 1M completion tokens
	Currency string  `toml:"currency"`
}

// Cost estimates the spend for a usage record.
func (p *Pricing) Cost(u *Usage) float64 {
	if p == nil || u == nil {
		return 0
	}
	hit := u.CacheHitTokens
	miss := u.CacheMissTokens
	if hit+miss == 0 && u.PromptTokens > 0 {
		miss = u.PromptTokens
	} else if miss == 0 && hit > 0 && u.PromptTokens > hit {
		miss = u.PromptTokens - hit
	}
	return (float64(hit)*p.CacheHit +
		float64(miss)*p.Input +
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
	Text      string    // ChunkText, ChunkReasoning
	Signature string    // ChunkReasoning: opaque proof for the reasoning (Anthropic thinking signature), when issued
	ToolCall  *ToolCall // ChunkToolCallStart (ID+Name only), ChunkToolCall (complete)
	Usage     *Usage    // ChunkUsage
	Err       error     // ChunkError
}

// StreamInterruptedError marks a recoverable transport cut that happened after
// the caller had already received model output. Providers must not replay these
// requests themselves because doing so could duplicate visible text or tool
// calls; the agent can append a tail recovery prompt instead.
type StreamInterruptedError struct {
	Err error
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
// when known, the environment variable the key comes from — so the CLI can
// surface it verbatim instead of dumping a raw status body. Providers should
// return this (rather than a generic status error) for auth failures.
type AuthError struct {
	Provider  string // the provider instance name, e.g. "deepseek"
	KeyEnv    string // the api_key_env the key is read from, when known
	KeySource string // human-readable source of KeyEnv, when known
	Status    int    // the HTTP status (401 or 403)
	HasKey    bool   // a non-empty key was sent — the server rejected it, vs. no key configured at all
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

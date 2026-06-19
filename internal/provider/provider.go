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
	ReasoningSignature string     `json:"reasoning_signature,omitempty"`
	ToolCalls          []ToolCall `json:"tool_calls,omitempty"`   // set by assistant
	ToolCallID         string     `json:"tool_call_id,omitempty"` // links a tool result to its call
	Name               string     `json:"name,omitempty"`         // tool message: tool name
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
	Temperature float64
	MaxTokens   int
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

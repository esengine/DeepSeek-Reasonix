package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"reasonix/internal/provider"
)

// Provider Broker methods run Host → Desktop for requests and Desktop → Host for
// stream chunks. API keys never leave Desktop; catalog entries are non-secret.

// BrokerCatalogParams is Host → Desktop: list authorized non-secret descriptors.
type BrokerCatalogParams struct {
	// AllowedRefs is optional filter; empty means all refs authorized for this scope.
	AllowedRefs []string `json:"allowedRefs,omitempty"`
}

// BrokerProviderDescriptor is a non-secret catalog entry for remote selection.
// Never includes API keys, base URLs, headers, or env names.
type BrokerProviderDescriptor struct {
	Ref                            string   `json:"ref" validate:"nonempty"`
	DisplayName                    string   `json:"displayName,omitempty"`
	Model                          string   `json:"model,omitempty"`
	ContextWindow                  int      `json:"contextWindow,omitempty" validate:"min=0"`
	PricingCurrency                string   `json:"pricingCurrency,omitempty"`
	CacheHitPerMillion             float64  `json:"cacheHitPerMillion,omitempty" validate:"min=0"`
	InputPerMillion                float64  `json:"inputPerMillion,omitempty" validate:"min=0"`
	OutputPerMillion               float64  `json:"outputPerMillion,omitempty" validate:"min=0"`
	SupportsVision                 bool     `json:"supportsVision,omitempty"`
	SupportedEfforts               []string `json:"supportedEfforts,omitempty"`
	DefaultEffort                  string   `json:"defaultEffort,omitempty"`
	ToolCallReasoning              bool     `json:"toolCallReasoning,omitempty"`
	WarnOnMissingToolCallReasoning bool     `json:"warnOnMissingToolCallReasoning,omitempty"`
}

// BrokerCatalogResult is the authorized catalog snapshot.
type BrokerCatalogResult struct {
	Providers []BrokerProviderDescriptor `json:"providers"`
}

// BrokerProviderRequest is the credential-free provider request transported
// from Host to Desktop. It intentionally names every provider-visible field so
// additions change the generated Remote schema and its compatibility hash.
// Provider credentials, endpoints, headers, and config never belong here.
type BrokerProviderRequest struct {
	Messages    []provider.Message    `json:"messages"`
	Tools       []provider.ToolSchema `json:"tools"`
	Temperature *float64              `json:"temperature,omitempty"`
	MaxTokens   int                   `json:"maxTokens" validate:"min=0"`
}

// BrokerProviderRequestFromProvider copies a provider request into its stable
// Broker wire DTO. Nil collections become empty arrays so the wire shape stays
// deterministic across reconnects and implementations.
func BrokerProviderRequestFromProvider(request provider.Request) BrokerProviderRequest {
	messages := append([]provider.Message(nil), request.Messages...)
	if messages == nil {
		messages = []provider.Message{}
	}
	tools := append([]provider.ToolSchema(nil), request.Tools...)
	if tools == nil {
		tools = []provider.ToolSchema{}
	}
	return BrokerProviderRequest{
		Messages: messages, Tools: tools, Temperature: request.Temperature,
		MaxTokens: request.MaxTokens,
	}
}

// ProviderRequest reconstructs the local Provider input without serializing it
// through an unversioned opaque JSON blob.
func (request BrokerProviderRequest) ProviderRequest() provider.Request {
	return provider.Request{
		Messages:    append([]provider.Message(nil), request.Messages...),
		Tools:       append([]provider.ToolSchema(nil), request.Tools...),
		Temperature: request.Temperature,
		MaxTokens:   request.MaxTokens,
	}
}

func (request BrokerProviderRequest) Validate() error {
	if request.Messages == nil || request.Tools == nil {
		return validationError("messages and tools must be arrays")
	}
	if request.MaxTokens < 0 {
		return validationError("maxTokens must be non-negative")
	}
	for _, tool := range request.Tools {
		parameters := bytes.TrimSpace(tool.Parameters)
		if len(parameters) == 0 || parameters[0] != '{' || !json.Valid(parameters) {
			return validationError("tool parameters must be a JSON object")
		}
	}
	return nil
}

// BrokerStreamOpenParams opens a provider stream on Desktop for one Host turn.
type BrokerStreamOpenParams struct {
	StreamID    string                `json:"streamId" validate:"nonempty"`
	ProviderRef string                `json:"providerRef" validate:"nonempty"`
	Request     BrokerProviderRequest `json:"request"`
	// Effort is optional override string already resolved into Request when set.
	Effort string `json:"effort,omitempty"`
}

func (p BrokerStreamOpenParams) Validate() error {
	if strings.TrimSpace(p.StreamID) == "" || strings.TrimSpace(p.ProviderRef) == "" {
		return validationError("streamId and providerRef are required")
	}
	return p.Request.Validate()
}

// BrokerStreamOpenResult acknowledges the stream; chunks arrive as notifications.
type BrokerStreamOpenResult struct {
	Accepted bool `json:"accepted"`
}

// BrokerStreamCancelParams cancels one in-flight Desktop provider stream.
type BrokerStreamCancelParams struct {
	StreamID string `json:"streamId" validate:"nonempty"`
}

// BrokerStreamCancelResult acknowledges cancel.
type BrokerStreamCancelResult struct {
	Cancelled bool `json:"cancelled"`
}

type BrokerChunkType string

const (
	BrokerChunkText          BrokerChunkType = "text"
	BrokerChunkReasoning     BrokerChunkType = "reasoning"
	BrokerChunkToolCallStart BrokerChunkType = "tool_call_start"
	BrokerChunkToolCallDelta BrokerChunkType = "tool_call_args_delta"
	BrokerChunkToolCall      BrokerChunkType = "tool_call"
	BrokerChunkUsage         BrokerChunkType = "usage"
	BrokerChunkDone          BrokerChunkType = "done"
	BrokerChunkError         BrokerChunkType = "error"
)

type BrokerProviderUsage struct {
	PromptTokens     int    `json:"promptTokens" validate:"min=0"`
	CompletionTokens int    `json:"completionTokens" validate:"min=0"`
	TotalTokens      int    `json:"totalTokens" validate:"min=0"`
	CacheHitTokens   int    `json:"cacheHitTokens" validate:"min=0"`
	CacheMissTokens  int    `json:"cacheMissTokens" validate:"min=0"`
	ReasoningTokens  int    `json:"reasoningTokens" validate:"min=0"`
	FinishReason     string `json:"finishReason,omitempty"`
}

type BrokerProviderErrorCode string

const (
	BrokerProviderFailed      BrokerProviderErrorCode = "provider_failed"
	BrokerProviderInterrupted BrokerProviderErrorCode = "provider_interrupted"
)

// BrokerProviderError is deliberately generic. Raw provider errors can contain
// API keys, authorization headers, endpoints, or response bodies and must never
// cross the Broker boundary.
type BrokerProviderError struct {
	Code    BrokerProviderErrorCode `json:"code"`
	Message string                  `json:"message" validate:"nonempty"`
}

type BrokerProviderChunk struct {
	Type      BrokerChunkType      `json:"type"`
	Text      string               `json:"text,omitempty"`
	Signature string               `json:"signature,omitempty"`
	ToolCall  *provider.ToolCall   `json:"toolCall,omitempty"`
	ArgChars  int                  `json:"argChars,omitempty" validate:"min=0"`
	Usage     *BrokerProviderUsage `json:"usage,omitempty"`
	Error     *BrokerProviderError `json:"error,omitempty"`
}

func (chunk BrokerProviderChunk) Validate() error {
	if chunk.ArgChars < 0 {
		return validationError("argChars must be non-negative")
	}
	if chunk.Type == BrokerChunkError && chunk.Error == nil {
		return validationError("error chunks require error")
	}
	if chunk.Type != BrokerChunkError && chunk.Error != nil {
		return validationError("non-error chunks forbid error")
	}
	if chunk.Type == BrokerChunkUsage && chunk.Usage == nil {
		return validationError("usage chunks require usage")
	}
	return nil
}

func BrokerProviderChunkFromProvider(chunk provider.Chunk) BrokerProviderChunk {
	wired := BrokerProviderChunk{
		Type: brokerChunkTypeFromProvider(chunk.Type), Text: chunk.Text,
		Signature: chunk.Signature, ToolCall: chunk.ToolCall, ArgChars: chunk.ArgChars,
	}
	if chunk.Usage != nil {
		wired.Usage = &BrokerProviderUsage{
			PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens,
			TotalTokens: chunk.Usage.TotalTokens, CacheHitTokens: chunk.Usage.CacheHitTokens,
			CacheMissTokens: chunk.Usage.CacheMissTokens, ReasoningTokens: chunk.Usage.ReasoningTokens,
			FinishReason: chunk.Usage.FinishReason,
		}
	}
	if chunk.Err != nil {
		wired.Type = BrokerChunkError
		code := BrokerProviderFailed
		message := "The local provider stream failed."
		if provider.IsStreamInterrupted(chunk.Err) {
			code = BrokerProviderInterrupted
			message = "The local provider stream was interrupted."
		}
		wired.Error = &BrokerProviderError{Code: code, Message: message}
	} else if wired.Type == BrokerChunkError {
		wired.Error = &BrokerProviderError{Code: BrokerProviderFailed, Message: "The local provider stream failed."}
	}
	return wired
}

func (chunk BrokerProviderChunk) ProviderChunk() provider.Chunk {
	converted := provider.Chunk{
		Type: providerChunkTypeFromBroker(chunk.Type), Text: chunk.Text,
		Signature: chunk.Signature, ToolCall: chunk.ToolCall, ArgChars: chunk.ArgChars,
	}
	if chunk.Usage != nil {
		converted.Usage = &provider.Usage{
			PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens,
			TotalTokens: chunk.Usage.TotalTokens, CacheHitTokens: chunk.Usage.CacheHitTokens,
			CacheMissTokens: chunk.Usage.CacheMissTokens, ReasoningTokens: chunk.Usage.ReasoningTokens,
			FinishReason: chunk.Usage.FinishReason,
		}
	}
	if chunk.Error != nil {
		err := errors.New(chunk.Error.Message)
		if chunk.Error.Code == BrokerProviderInterrupted {
			err = &provider.StreamInterruptedError{Err: err}
		}
		converted.Err = err
	}
	return converted
}

func brokerChunkTypeFromProvider(kind provider.ChunkType) BrokerChunkType {
	switch kind {
	case provider.ChunkText:
		return BrokerChunkText
	case provider.ChunkReasoning:
		return BrokerChunkReasoning
	case provider.ChunkToolCallStart:
		return BrokerChunkToolCallStart
	case provider.ChunkToolCallArgsDelta:
		return BrokerChunkToolCallDelta
	case provider.ChunkToolCall:
		return BrokerChunkToolCall
	case provider.ChunkUsage:
		return BrokerChunkUsage
	case provider.ChunkDone:
		return BrokerChunkDone
	default:
		return BrokerChunkError
	}
}

func providerChunkTypeFromBroker(kind BrokerChunkType) provider.ChunkType {
	switch kind {
	case BrokerChunkText:
		return provider.ChunkText
	case BrokerChunkReasoning:
		return provider.ChunkReasoning
	case BrokerChunkToolCallStart:
		return provider.ChunkToolCallStart
	case BrokerChunkToolCallDelta:
		return provider.ChunkToolCallArgsDelta
	case BrokerChunkToolCall:
		return provider.ChunkToolCall
	case BrokerChunkUsage:
		return provider.ChunkUsage
	case BrokerChunkDone:
		return provider.ChunkDone
	default:
		return provider.ChunkError
	}
}

// BrokerStreamChunkParams is one provider chunk Desktop → Host.
type BrokerStreamChunkParams struct {
	StreamID string              `json:"streamId" validate:"nonempty"`
	Seq      int64               `json:"seq" validate:"min=1"`
	Chunk    BrokerProviderChunk `json:"chunk"`
}

func (p BrokerStreamChunkParams) Validate() error {
	if strings.TrimSpace(p.StreamID) == "" {
		return validationError("streamId is required")
	}
	if p.Seq < 1 {
		return validationError("seq must be >= 1")
	}
	return p.Chunk.Validate()
}

// BrokerStreamEndParams ends a stream (success or error).
type BrokerStreamEndParams struct {
	StreamID string `json:"streamId" validate:"nonempty"`
	// LastSeq freezes the terminal ordering boundary. Receivers must deliver all
	// chunks 1..LastSeq before completing the stream.
	LastSeq int64 `json:"lastSeq" validate:"min=0"`
	// Error is a redacted, non-secret failure message when the stream failed.
	Error string `json:"error,omitempty"`
	// Interrupted is true when the Desktop↔Host connection dropped mid-stream.
	Interrupted bool `json:"interrupted,omitempty"`
}

// BrokerCatalogChangedParams notifies Host that Desktop catalog or authorization changed.
type BrokerCatalogChangedParams struct {
	// Generation is a Desktop-local monotonic counter for drop-stale logic.
	Generation int64 `json:"generation" validate:"min=1"`
}

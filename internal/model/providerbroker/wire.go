package providerbroker

import (
	"context"
	"encoding/json"
	"errors"

	"reasonix/internal/contract/provider"
)

// The wire surface, versioned as one: a Client and a Server that disagree
// about either path disagree about the chunk shape too.
const (
	PathCatalog = "/v1/catalog"
	PathStream  = "/v1/stream"
	// HeaderToken carries the pre-shared token. The remote holds no provider
	// key, so this is the only secret on that side of the tunnel.
	HeaderToken = "X-Reasonix-Broker-Token"
)

type catalogResponse struct {
	Providers []provider.Descriptor `json:"providers"`
}

type streamRequest struct {
	Selection provider.Selection `json:"selection"`
	Request   provider.Request   `json:"request"`
}

// wireChunk is one provider.Chunk on the wire.
type wireChunk struct {
	Type            provider.ChunkType `json:"type"`
	Text            string             `json:"text,omitempty"`
	Signature       string             `json:"signature,omitempty"`
	ReasoningID     string             `json:"reasoningId,omitempty"`
	ReasoningStatus string             `json:"reasoningStatus,omitempty"`
	ToolCall        *provider.ToolCall `json:"toolCall,omitempty"`
	ArgChars        int                `json:"argChars,omitempty"`
	ResponsesItem   json.RawMessage    `json:"responsesItem,omitempty"`
	Usage           *provider.Usage    `json:"usage,omitempty"`
	Err             *wireError         `json:"err,omitempty"`
}

func encodeChunk(c provider.Chunk) wireChunk {
	return wireChunk{
		Type: c.Type, Text: c.Text, Signature: c.Signature,
		ReasoningID: c.ReasoningID, ReasoningStatus: c.ReasoningStatus,
		ToolCall: c.ToolCall, ArgChars: c.ArgChars,
		ResponsesItem: c.ResponsesItem, Usage: c.Usage,
		Err: encodeError(c.Err),
	}
}

func (w wireChunk) decode() provider.Chunk {
	return provider.Chunk{
		Type: w.Type, Text: w.Text, Signature: w.Signature,
		ReasoningID: w.ReasoningID, ReasoningStatus: w.ReasoningStatus,
		ToolCall: w.ToolCall, ArgChars: w.ArgChars,
		ResponsesItem: w.ResponsesItem, Usage: w.Usage,
		Err: w.Err.decode(),
	}
}

// errKind names the concrete error a chunk carried. Callers tell these apart
// by type, never by message, so the kind is what crosses and the message rides
// along only for display.
type errKind string

const (
	errKindAPI         errKind = "api"
	errKindAuth        errKind = "auth"
	errKindPayload     errKind = "stream_payload"
	errKindInterrupted errKind = "stream_interrupted"
	errKindCanceled    errKind = "canceled"
	errKindDeadline    errKind = "deadline"
	errKindOpaque      errKind = "opaque"
	// errKindUnknownModel is a ref the broker's own catalog does not carry.
	errKindUnknownModel errKind = "unknown_model"
)

// wireError is the tagged union of every provider error a caller matches on.
// Wrapped carries StreamInterruptedError's cause, which is itself one of these.
type wireError struct {
	Kind        errKind              `json:"kind"`
	Message     string               `json:"message,omitempty"`
	Provider    string               `json:"provider,omitempty"`
	Status      int                  `json:"status,omitempty"`
	Body        string               `json:"body,omitempty"`
	TraceID     string               `json:"traceId,omitempty"`
	ToolContext string               `json:"toolContext,omitempty"`
	Hint        provider.RequestHint `json:"hint,omitempty"`
	KeyEnv      string               `json:"keyEnv,omitempty"`
	KeySource   string               `json:"keySource,omitempty"`
	HasKey      bool                 `json:"hasKey,omitempty"`
	Reason      string               `json:"reason,omitempty"`
	Ref         string               `json:"ref,omitempty"`
	Wrapped     *wireError           `json:"wrapped,omitempty"`
}

// encodeError projects err onto the wire union. Order is specificity, not
// convenience: StreamInterruptedError wraps one of the others, so matching it
// first is what keeps the cause from being reported as the whole failure.
func encodeError(err error) *wireError {
	if err == nil {
		return nil
	}
	var interrupted *provider.StreamInterruptedError
	if errors.As(err, &interrupted) {
		return &wireError{
			Kind: errKindInterrupted, Message: interrupted.Error(),
			Reason: interrupted.Reason, Wrapped: encodeError(interrupted.Err),
		}
	}
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		return &wireError{
			Kind: errKindAuth, Message: authErr.Error(), Provider: authErr.Provider,
			Status: authErr.Status, Body: authErr.Body, KeyEnv: authErr.KeyEnv,
			KeySource: authErr.KeySource, HasKey: authErr.HasKey,
		}
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return &wireError{
			Kind: errKindAPI, Message: apiErr.Error(), Provider: apiErr.Provider,
			Status: apiErr.Status, Body: apiErr.Body, TraceID: apiErr.TraceID,
			ToolContext: apiErr.ToolContext, Hint: apiErr.Hint,
		}
	}
	var payloadErr *provider.StreamPayloadError
	if errors.As(err, &payloadErr) {
		return &wireError{
			Kind: errKindPayload, Message: payloadErr.Message, Provider: payloadErr.Provider,
		}
	}
	switch {
	case errors.Is(err, provider.ErrUnknownModel):
		return &wireError{Kind: errKindUnknownModel, Message: err.Error()}
	case errors.Is(err, context.Canceled):
		return &wireError{Kind: errKindCanceled, Message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return &wireError{Kind: errKindDeadline, Message: err.Error()}
	}
	return &wireError{Kind: errKindOpaque, Message: err.Error()}
}

// decode rebuilds the error the local provider returned. An unknown kind — a
// newer local kernel naming one this build has never heard of — degrades to
// the message rather than to nil: losing the identity costs a classification,
// losing the error costs the turn its failure.
func (w *wireError) decode() error {
	if w == nil {
		return nil
	}
	switch w.Kind {
	case errKindInterrupted:
		return &provider.StreamInterruptedError{Err: w.Wrapped.decode(), Reason: w.Reason}
	case errKindAuth:
		return &provider.AuthError{
			Provider: w.Provider, KeyEnv: w.KeyEnv, KeySource: w.KeySource,
			Status: w.Status, HasKey: w.HasKey, Body: w.Body,
		}
	case errKindAPI:
		return &provider.APIError{
			Provider: w.Provider, Status: w.Status, Body: w.Body,
			TraceID: w.TraceID, ToolContext: w.ToolContext, Hint: w.Hint,
		}
	case errKindPayload:
		return &provider.StreamPayloadError{Provider: w.Provider, Message: w.Message}
	case errKindCanceled:
		return context.Canceled
	case errKindDeadline:
		return context.DeadlineExceeded
	case errKindUnknownModel:
		return &UnknownModelError{Ref: w.Ref}
	}
	return errors.New(w.Message)
}

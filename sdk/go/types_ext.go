package extension

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// This file holds the handwritten half of the SDK's type layer: behavior the
// generator cannot emit (validators, error constructors, enum helpers). The
// wire DTOs, enums, method names, frozen limits, and the frozen error table
// live in types_generated.go, mirrored from the host's
// internal/extension/protocol package by cmd/extension-protocol-gen.
//
// Stability contract (mirrored from the host): within major version 1 only
// optional fields, new enum values, and new methods may be added. Existing
// required fields, method names, directions, limits, error reasons, and
// semantics never change.

// Enum helpers

// InterceptEvents returns the 17 frozen hook point names, sorted.
func InterceptEvents() []string {
	out := []string{
		string(EventSessionStart), string(EventSessionEnd), string(EventSessionLoad),
		string(EventSessionSave), string(EventSessionRotate), string(EventInputReceive),
		string(EventAgentBeforeStart), string(EventSystemPromptBuild), string(EventContextPrepare),
		string(EventProviderRequest), string(EventProviderResponse), string(EventToolBefore),
		string(EventToolAfter), string(EventPermissionDecision), string(EventCompactionPrepare),
		string(EventCompactionComplete), string(EventFrontendEvent),
	}
	sortStrings(out)
	return out
}

func validInterceptEvent(event InterceptEvent) bool {
	switch event {
	case EventSessionStart, EventSessionEnd, EventSessionLoad, EventSessionSave,
		EventSessionRotate, EventInputReceive, EventAgentBeforeStart,
		EventSystemPromptBuild, EventContextPrepare, EventProviderRequest,
		EventProviderResponse, EventToolBefore, EventToolAfter,
		EventPermissionDecision, EventCompactionPrepare, EventCompactionComplete,
		EventFrontendEvent:
		return true
	}
	return false
}

func validInterceptDecision(decision InterceptDecision) bool {
	switch decision {
	case DecisionContinue, DecisionBlock, DecisionReplace, DecisionAllow, DecisionDeny:
		return true
	}
	return false
}

func validUIFieldKind(kind UIFieldKind) bool {
	switch kind {
	case UIFieldConfirm, UIFieldInput, UIFieldSelect, UIFieldMultiselect:
		return true
	}
	return false
}

func validUISeverity(severity UISeverity) bool {
	switch severity {
	case "", UISeverityInfo, UISeverityWarn, UISeverityError:
		return true
	}
	return false
}

// Wire DTO validators

// Validate enforces the deterministic wire shape.
func (request ProviderRequest) Validate() error {
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

// Validate enforces chunk invariants the tags cannot express.
func (chunk ProviderChunk) Validate() error {
	if chunk.ArgChars < 0 {
		return validationError("argChars must be non-negative")
	}
	if chunk.Type == ChunkError && chunk.Error == nil {
		return validationError("error chunks require error")
	}
	if chunk.Type != ChunkError && chunk.Error != nil {
		return validationError("non-error chunks forbid error")
	}
	if chunk.Type == ChunkUsage && chunk.Usage == nil {
		return validationError("usage chunks require usage")
	}
	return nil
}

// Validate enforces required identifiers plus the request invariants.
func (p StreamOpenParams) Validate() error {
	if strings.TrimSpace(p.StreamID) == "" || strings.TrimSpace(p.ProviderRef) == "" {
		return validationError("streamId and providerRef are required")
	}
	return p.Request.Validate()
}

// Validate enforces stream ordering preconditions and chunk invariants.
func (p StreamChunkParams) Validate() error {
	if strings.TrimSpace(p.StreamID) == "" {
		return validationError("streamId is required")
	}
	if p.Seq < 1 {
		return validationError("seq must be >= 1")
	}
	return p.Chunk.Validate()
}

// Validate checks the data against the frozen error table.
func (d ProtocolErrorData) Validate() error {
	spec, ok := frozenErrorSpecs[d.Reason]
	if !ok {
		return fmt.Errorf("unknown extension error reason %q", d.Reason)
	}
	if d.Retryable != spec.Retryable {
		return fmt.Errorf("retryable must match the frozen error table for %q", d.Reason)
	}
	return nil
}

// ProtocolError

// ProtocolError is the extension protocol's structured error. Handlers may
// return one to choose the wire reason; the SDK answers with the frozen
// JSON-RPC code and structured data. Message should stay generic: it crosses
// the wire verbatim.
type ProtocolError struct {
	Reason  ErrorReason
	Message string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// NewProtocolError builds the frozen error for a reason.
func NewProtocolError(reason ErrorReason) (*ProtocolError, error) {
	spec, ok := frozenErrorSpecs[reason]
	if !ok {
		return nil, fmt.Errorf("extension: unknown error reason %q", reason)
	}
	return &ProtocolError{Reason: reason, Message: spec.Message}, nil
}

// MustProtocolError is NewProtocolError for reasons known to be frozen.
func MustProtocolError(reason ErrorReason) *ProtocolError {
	errValue, err := NewProtocolError(reason)
	if err != nil {
		panic(err)
	}
	return errValue
}

// internal helpers shared with the validator

type validationFailure struct{ message string }

func (e *validationFailure) Error() string { return e.message }

func validationError(message string) error { return &validationFailure{message: message} }

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

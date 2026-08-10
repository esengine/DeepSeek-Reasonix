package protocol

import (
	"fmt"
	"sort"

	"reasonix/internal/extension/rpcwire"
)

// DomainErrorCode is the JSON-RPC code every extension domain error uses on
// the wire. The structured ProtocolErrorData reason distinguishes them.
// Errors that map onto a standard JSON-RPC failure (malformed request,
// unknown method, invalid params, internal) use the standard codes instead.
const DomainErrorCode = -32000

// ErrorReason is the stable wire reason string carried in ProtocolErrorData.
type ErrorReason string

const (
	ErrProtocolError         ErrorReason = "protocol_error"
	ErrUnknownMethod         ErrorReason = "unknown_method"
	ErrInvalidParams         ErrorReason = "invalid_params"
	ErrFrameTooLarge         ErrorReason = "frame_too_large"
	ErrContentRefExpired     ErrorReason = "content_ref_expired"
	ErrUnsupportedVersion    ErrorReason = "unsupported_version"
	ErrCapabilityNotDeclared ErrorReason = "capability_not_declared"
	ErrShutdownTimeout       ErrorReason = "shutdown_timeout"
	ErrStreamGap             ErrorReason = "stream_gap"
	ErrStreamCancelled       ErrorReason = "stream_cancelled"
	ErrProviderFailed        ErrorReason = "provider_failed"
	ErrProviderInterrupted   ErrorReason = "provider_interrupted"
	ErrInterceptTimeout      ErrorReason = "intercept_timeout"
	ErrDependencyUnsatisfied ErrorReason = "dependency_unsatisfied"
	ErrDependencyCycle       ErrorReason = "dependency_cycle"
	ErrSchemaMismatch        ErrorReason = "schema_mismatch"
	ErrActivationFailed      ErrorReason = "activation_failed"
	ErrStaleGeneration       ErrorReason = "stale_generation"
	ErrCleanupFailed         ErrorReason = "cleanup_failed"
	ErrInternal              ErrorReason = "internal"
)

type errorSpec struct {
	Code      int
	Message   string
	Retryable bool
}

// frozenErrorSpecs is the frozen error table. Adding an entry is a conscious
// protocol change: the table is frozen into the generated schema document and
// pinned by tests.
var frozenErrorSpecs = map[ErrorReason]errorSpec{
	ErrProtocolError:         {rpcwire.ErrInvalidRequest, "The extension protocol frame or envelope is invalid.", false},
	ErrUnknownMethod:         {rpcwire.ErrMethodNotFound, "The method is not registered in Extension Protocol v2.", false},
	ErrInvalidParams:         {rpcwire.ErrInvalidParams, "The params do not match the registered method schema.", false},
	ErrFrameTooLarge:         {DomainErrorCode, "The frame exceeds the frozen protocol size limit.", false},
	ErrContentRefExpired:     {DomainErrorCode, "The referenced content has expired.", true},
	ErrUnsupportedVersion:    {DomainErrorCode, "The peer's extension protocol version is not supported.", false},
	ErrCapabilityNotDeclared: {DomainErrorCode, "The extension used a capability its manifest did not declare.", false},
	ErrShutdownTimeout:       {DomainErrorCode, "The extension did not shut down within the requested timeout.", true},
	ErrStreamGap:             {DomainErrorCode, "A provider stream chunk is missing from the ordered sequence.", true},
	ErrStreamCancelled:       {DomainErrorCode, "The provider stream was cancelled.", false},
	ErrProviderFailed:        {DomainErrorCode, "The extension provider stream failed.", true},
	ErrProviderInterrupted:   {DomainErrorCode, "The extension provider stream was interrupted.", true},
	ErrInterceptTimeout:      {DomainErrorCode, "The extension did not answer an intercept within its timeout.", true},
	ErrDependencyUnsatisfied: {DomainErrorCode, "A required dependency is missing or not version-compatible.", false},
	ErrDependencyCycle:       {DomainErrorCode, "The dependency graph contains a required cycle.", false},
	ErrSchemaMismatch:        {DomainErrorCode, "A capability schema hash does not match the expected pin.", false},
	ErrActivationFailed:      {DomainErrorCode, "Component activation failed; the new generation was not published.", false},
	ErrStaleGeneration:       {DomainErrorCode, "The message belongs to a superseded runtime generation.", false},
	ErrCleanupFailed:         {DomainErrorCode, "Component cleanup failed while disposing scoped effects.", true},
	ErrInternal:              {rpcwire.ErrInternal, "An internal extension protocol error occurred.", true},
}

// ProtocolErrorData is the structured JSON-RPC error data for extension
// domain errors.
type ProtocolErrorData struct {
	Reason    ErrorReason `json:"reason"`
	Retryable bool        `json:"retryable"`
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

// ProtocolError is the extension protocol's structured error, the equivalent
// of Remote's RemoteError. Message is the frozen generic text; details that
// could carry sidecar internals never cross the wire.
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

// RPCError converts to the transport error object with the frozen JSON-RPC
// code and structured data.
func (e *ProtocolError) RPCError() *rpcwire.RPCError {
	if e == nil {
		return &rpcwire.RPCError{Code: rpcwire.ErrInternal, Message: "internal error"}
	}
	spec := frozenErrorSpecs[e.Reason]
	return &rpcwire.RPCError{
		Code:    spec.Code,
		Message: e.Message,
		Data:    ProtocolErrorData{Reason: e.Reason, Retryable: spec.Retryable},
	}
}

// NewProtocolError builds the frozen error for a reason.
func NewProtocolError(reason ErrorReason) (*ProtocolError, error) {
	spec, ok := frozenErrorSpecs[reason]
	if !ok {
		return nil, fmt.Errorf("protocol: unknown extension error reason %q", reason)
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

// ErrorContract is the schema- and documentation-visible form of one frozen
// error table entry.
type ErrorContract struct {
	Reason      ErrorReason `json:"reason"`
	JSONRPCCode int         `json:"jsonRpcCode"`
	Message     string      `json:"message"`
	Retryable   bool        `json:"retryable"`
}

// ErrorContracts returns the frozen error table sorted by reason.
func ErrorContracts() []ErrorContract {
	out := make([]ErrorContract, 0, len(frozenErrorSpecs))
	for reason, spec := range frozenErrorSpecs {
		out = append(out, ErrorContract{reason, spec.Code, spec.Message, spec.Retryable})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Reason < out[j].Reason })
	return out
}

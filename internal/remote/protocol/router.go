package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"reasonix/internal/rpcwire"
)

type Handler func(context.Context, any) (any, error)
type HandlerSet map[Method]Handler

type RouterOptions struct {
	OnInternalError func(method Method, err error)
}

type handshakeState uint8

const (
	handshakeNew handshakeState = iota
	handshakeInitializing
	handshakeReady
	handshakeFailed
)

// Router is scoped to exactly one Remote transport. It uses the frozen method
// registry for handler registration, strict param decoding, and result checks.
type Router struct {
	handlers HandlerSet
	options  RouterOptions

	mu    sync.Mutex
	state handshakeState
}

func NewRouter(handlers HandlerSet, options RouterOptions) (*Router, error) {
	if err := ValidateRegistry(); err != nil {
		return nil, err
	}
	for method, handler := range handlers {
		spec, ok := LookupMethod(method)
		if !ok {
			return nil, fmt.Errorf("protocol: handler for unregistered method %q", method)
		}
		if spec.Direction != DirectionClientRequest {
			return nil, fmt.Errorf("protocol: notification %q cannot have a request handler", method)
		}
		if handler == nil {
			return nil, fmt.Errorf("protocol: nil handler for %q", method)
		}
	}
	if handlers[MethodRemoteInitialize] == nil {
		return nil, fmt.Errorf("protocol: remote/initialize handler is required")
	}
	copyHandlers := make(HandlerSet, len(handlers))
	for method, handler := range handlers {
		copyHandlers[method] = handler
	}
	return &Router{handlers: copyHandlers, options: options}, nil
}

func NewCompleteRouter(handlers HandlerSet, options RouterOptions) (*Router, error) {
	if err := ValidateHandlerCoverage(handlers); err != nil {
		return nil, err
	}
	return NewRouter(handlers, options)
}

func ValidateHandlerCoverage(handlers HandlerSet) error {
	for _, spec := range Registry() {
		if spec.Direction == DirectionClientRequest && handlers[spec.Name] == nil {
			return fmt.Errorf("protocol: missing handler for %q", spec.Name)
		}
	}
	return nil
}

// WireOptions freezes Remote's strict JSON-RPC and symmetric 8 MiB framing.
// The BeforeRequest hook runs synchronously in rpcwire's read loop, so the
// initialize-first decision observes wire arrival order rather than goroutine
// scheduling order.
func (r *Router) WireOptions() rpcwire.Options {
	return rpcwire.Options{
		Name: "remote", MaxInboundBytes: FrameBytes, MaxOutboundBytes: FrameBytes,
		StrictJSONRPC: true, MaxConcurrentHandlers: RPCConcurrentHandlers, MaxQueuedNotifications: RPCQueuedNotifications,
		BeforeRequest: r.BeforeRequest, BeforeNotification: r.BeforeNotification,
	}
}

func (r *Router) Bind(conn *rpcwire.Conn) {
	for _, spec := range Registry() {
		if spec.Direction != DirectionClientRequest {
			continue
		}
		if r.handlers[spec.Name] == nil {
			continue
		}
		spec := spec
		conn.Handle(string(spec.Name), func(ctx context.Context, raw json.RawMessage) (any, error) {
			return r.invoke(ctx, spec, raw)
		})
	}
}

func (r *Router) BeforeRequest(method string, _ json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := Method(method)
	switch r.state {
	case handshakeNew:
		if name != MethodRemoteInitialize {
			r.state = handshakeFailed
			return invalidRequest("remote/initialize must be the first request")
		}
		r.state = handshakeInitializing
		return nil
	case handshakeInitializing:
		return invalidRequest("remote initialization is not complete")
	case handshakeReady:
		if name == MethodRemoteInitialize {
			return invalidRequest("remote/initialize has already completed")
		}
		return nil
	default:
		return invalidRequest("remote handshake has failed")
	}
}

// BeforeNotification admits only frozen Desktop-to-Host Broker notifications
// after initialization. Invalid, unknown, or early notifications poison the
// transport because JSON-RPC notifications cannot carry an error response.
func (r *Router) BeforeNotification(method string, raw json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != handshakeReady {
		r.state = handshakeFailed
		return invalidRequest("Remote initialization must complete before notifications")
	}
	if _, err := DecodeBrokerNotificationParams(Method(method), raw); err != nil {
		r.state = handshakeFailed
		return invalidRequest("invalid Broker notification")
	}
	return nil
}

func (r *Router) Ready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == handshakeReady
}

func (r *Router) invoke(ctx context.Context, spec MethodSpec, raw json.RawMessage) (any, error) {
	params, err := decodeAndValidate(raw, spec.ParamsType)
	if err != nil {
		r.finishInitialize(spec.Name, false)
		return nil, invalidParams()
	}
	result, err := r.handlers[spec.Name](ctx, params)
	if err != nil {
		r.finishInitialize(spec.Name, false)
		return nil, r.mapHandlerError(spec.Name, err)
	}
	requireAfterWrite := spec.Name == MethodRemoteInitialize || spec.Name == MethodRemoteDetach || spec.Name == MethodSessionSubscribe
	normalized, err := validateHandlerResult(result, spec.ResultType, requireAfterWrite)
	if err != nil {
		r.finishInitialize(spec.Name, false)
		r.reportInternal(spec.Name, err)
		return nil, internalError()
	}
	r.finishInitialize(spec.Name, true)
	return normalized, nil
}

func (r *Router) finishInitialize(method Method, success bool) {
	if method != MethodRemoteInitialize {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != handshakeInitializing {
		return
	}
	if success {
		r.state = handshakeReady
	} else {
		r.state = handshakeFailed
	}
}

func (r *Router) mapHandlerError(method Method, err error) error {
	var remoteErr *RemoteError
	if errors.As(err, &remoteErr) {
		if validationErr := remoteErr.Data.Validate(); validationErr == nil {
			return remoteErr.RPCError()
		}
	}
	r.reportInternal(method, err)
	return internalError()
}

func (r *Router) reportInternal(method Method, err error) {
	if r.options.OnInternalError != nil {
		r.options.OnInternalError(method, err)
	}
}

func validateHandlerResult(result any, expected reflect.Type, requireAfterWrite bool) (any, error) {
	if result == nil {
		return nil, errors.New("nil handler result")
	}
	var response *rpcwire.HandlerResponse
	switch wrapped := result.(type) {
	case rpcwire.HandlerResponse:
		response = &wrapped
		result = wrapped.Result
	case *rpcwire.HandlerResponse:
		if wrapped == nil {
			return nil, errors.New("nil handler response")
		}
		copyResponse := *wrapped
		response = &copyResponse
		result = wrapped.Result
	}
	if requireAfterWrite && (response == nil || response.AfterWrite == nil) {
		return nil, errors.New("handler must return rpcwire.RespondThen")
	}
	if !requireAfterWrite && response != nil {
		return nil, errors.New("after-write handler response is only valid for response-ordered methods")
	}
	if result == nil {
		return nil, errors.New("nil wrapped handler result")
	}
	value := reflect.ValueOf(result)
	if value.Type() == reflect.PointerTo(expected) {
		if value.IsNil() {
			return nil, errors.New("nil handler result pointer")
		}
		value = value.Elem()
	} else if value.Type() != expected {
		return nil, fmt.Errorf("handler result type %v, want %v", value.Type(), expected)
	}
	normalized := value.Interface()
	if err := validateDecoded(normalized); err != nil {
		return nil, fmt.Errorf("invalid handler result: %w", err)
	}
	if response != nil {
		response.Result = normalized
		return *response, nil
	}
	return normalized, nil
}

func invalidRequest(message string) *rpcwire.RPCError {
	return &rpcwire.RPCError{Code: rpcwire.ErrInvalidRequest, Message: message}
}

func invalidParams() *rpcwire.RPCError {
	return &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "invalid params"}
}

func internalError() *rpcwire.RPCError {
	return &rpcwire.RPCError{Code: rpcwire.ErrInternal, Message: "internal error"}
}

// ValidateNotification verifies that a Host notification is registered and
// that its typed payload matches the frozen schema before it is sent.
func ValidateNotification(method Method, payload any) error {
	spec, ok := LookupMethod(method)
	if !ok || spec.Direction != DirectionHostNotification {
		return fmt.Errorf("protocol: %q is not a registered Host notification", method)
	}
	value := reflect.ValueOf(payload)
	if !value.IsValid() {
		return errors.New("protocol: nil notification payload")
	}
	if value.Type() == reflect.PointerTo(spec.ParamsType) {
		if value.IsNil() {
			return errors.New("protocol: nil notification payload")
		}
		value = value.Elem()
	} else if value.Type() != spec.ParamsType {
		return fmt.Errorf("protocol: notification payload type %v, want %v", value.Type(), spec.ParamsType)
	}
	return validateDecoded(value.Interface())
}

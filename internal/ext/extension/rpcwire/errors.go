// Package rpcwire implements the Extension Protocol's transport-neutral
// JSON-RPC 2.0 over NDJSON wire. It intentionally contains no extension
// lifecycle or capability semantics.
package rpcwire

import (
	"encoding/json"
	"fmt"
	"time"
)

// Standard JSON-RPC 2.0 error codes.
const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
	// ErrServerBusy is a stable transport-local overload response. It is outside
	// the JSON-RPC reserved range and intentionally carries no peer data.
	ErrServerBusy = -32099
)

// ErrorObject is the JSON-RPC error object carried on the wire.
type ErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// RPCError lets a request handler choose the JSON-RPC error response. Data may
// be any JSON-marshalable value. Callers must keep Message and Data safe for the
// target protocol; rpcwire does not apply product-specific redaction.
type RPCError struct {
	Code    int
	Message string
	Data    any
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// ResponseError is returned by Conn.Request when the peer responds with a
// JSON-RPC error. Data remains raw so the protocol adapter can decode its own
// structured error type.
type ResponseError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *ResponseError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// FrameTooLargeError reports a frame that violates the configured NDJSON
// budget. Size and Limit include the trailing newline, matching the bytes sent
// over the transport.
type FrameTooLargeError struct {
	Direction string
	Size      int
	Limit     int
}

func (e *FrameTooLargeError) Error() string {
	return fmt.Sprintf("rpcwire: %s frame is %d bytes; limit is %d", e.Direction, e.Size, e.Limit)
}

// OutboundQueueFullError reports that TryNotify could not enqueue a
// best-effort notification without blocking. The caller may drop the event;
// request/response traffic keeps using the bounded blocking write path.
type OutboundQueueFullError struct {
	Limit int
}

func (e *OutboundQueueFullError) Error() string {
	return fmt.Sprintf("rpcwire: outbound notification queue is full (limit %d)", e.Limit)
}

// WriteStallError reports an outbound write that made no progress within the
// configured MaxWriteStall bound: the peer is alive enough to keep the pipe
// open but has stopped reading, so without a bound the write would block the
// caller forever. It is a transport-fatal error — the connection fails so a
// wedged peer is torn down instead of stalling every later call.
type WriteStallError struct {
	Direction string
	Size      int
	Stall     time.Duration
}

func (e *WriteStallError) Error() string {
	return fmt.Sprintf("rpcwire: %s write of %d bytes made no progress for %s", e.Direction, e.Size, e.Stall)
}

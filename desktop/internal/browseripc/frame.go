// Package browseripc defines the versioned wire protocol between the Reasonix
// desktop host and the Browser Companion child process. Frames are length
// prefixed JSON over the child's stdin/stdout pipes; the canonical schema lives
// in schema.json and every constant below mirrors it (parity is enforced by
// TestSchemaParity).
package browseripc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the protocol version every frame must carry. It is
// negotiated via the hello handshake and is independent of the component and
// Reasonix release versions.
const ProtocolVersion = 1

// Wire limits mirrored from schema.json "limits". The frame budget applies to
// the full length-prefixed payload; per-result budgets (screenshot bytes, page
// text characters) are enforced by the companion before framing.
const (
	FrameMaxBytes         = 16 * 1024 * 1024
	MaxPendingRequests    = 64
	ResponseTimeoutMs     = 30000
	ShutdownGraceMs       = 3000
	MaxTextChars          = 50000
	MaxScreenshotBytes    = 8 * 1024 * 1024
	MaxRequestIDBytes     = 64
	MaxOwnerIDBytes       = 128
	MaxMethodBytes        = 64
	MaxTabIDBytes         = 64
	MaxOriginBytes        = 1024
	MaxURLBytes           = 8192
	DefaultResponseBudget = 4 * 1024 * 1024
)

// ErrorCode is a stable, protocol-visible error classification. New codes are
// additive; codes never change meaning across protocol versions.
type ErrorCode string

const (
	CodeCancelled        ErrorCode = "cancelled"
	CodeComponentMissing ErrorCode = "component_missing"
	CodeCrashed          ErrorCode = "crashed"
	CodeFrameTooLarge    ErrorCode = "frame_too_large"
	CodeInternal         ErrorCode = "internal"
	CodeInvalidParams    ErrorCode = "invalid_params"
	CodeNotReady         ErrorCode = "not_ready"
	CodeOwnerNotFound    ErrorCode = "owner_not_found"
	CodePermissionDenied ErrorCode = "permission_denied"
	CodeProtocolMismatch ErrorCode = "protocol_mismatch"
	CodeStaleRef         ErrorCode = "stale_ref"
	CodeTabBusy          ErrorCode = "tab_busy"
	CodeTabNotFound      ErrorCode = "tab_not_found"
	CodeTimeout          ErrorCode = "timeout"
	CodeUnknownMethod    ErrorCode = "unknown_method"
	CodeUnsupported      ErrorCode = "unsupported"
	CodeUserTakeoverReq  ErrorCode = "user_takeover_required"
	CodeUserTookControl  ErrorCode = "user_took_control"
)

// RPCError is the error member of a response frame.
type RPCError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Request is a host-to-companion call frame. ownerId is bound by the host to a
// Reasonix chat tab ID and may be empty for methods without an owner.
type Request struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestId"`
	OwnerID         string          `json:"ownerId,omitempty"`
	Method          string          `json:"method"`
	Params          json.RawMessage `json:"params"`
}

// Response is a companion-to-host reply. Exactly one of Result/Error is set.
type Response struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestId"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           *RPCError       `json:"error,omitempty"`
}

// Event is an unsolicited companion-to-host notification.
type Event struct {
	ProtocolVersion int       `json:"protocolVersion"`
	Event           EventBody `json:"event"`
}

// EventBody is the structured payload of an Event frame.
type EventBody struct {
	Name    string          `json:"name"`
	OwnerID string          `json:"ownerId,omitempty"`
	Data    json.RawMessage `json:"data"`
}

// ErrFrameTooLarge is returned by ReadFrame when the announced frame length
// exceeds the limit.
var ErrFrameTooLarge = errors.New("browseripc: frame exceeds size limit")

// ErrZeroFrame is returned when a zero-length frame is announced. A zero
// length would decode as a zero-byte JSON document, which is never a valid
// frame, so it is treated as a protocol error rather than EOF.
var ErrZeroFrame = errors.New("browseripc: zero-length frame")

// WriteFrame writes payload as a single length-prefixed frame. The length
// prefix is a big-endian uint32 so both the Go host and the Electron
// companion implement byte-identical framing.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > FrameMaxBytes {
		return fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, len(payload), FrameMaxBytes)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("browseripc: write frame header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("browseripc: write frame payload: %w", err)
	}
	return nil
}

// ReadFrame reads one length-prefixed frame. maxBytes bounds the accepted
// payload size (use FrameMaxBytes for the wire); an announced frame larger
// than maxBytes is rejected without consuming its payload, matching how a
// hostile or corrupted child must fail.
func ReadFrame(r io.Reader, maxBytes int) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint32(header[:]))
	if length == 0 {
		return nil, ErrZeroFrame
	}
	if length > maxBytes {
		return nil, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, length, maxBytes)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("browseripc: read frame payload: %w", err)
	}
	return payload, nil
}

// WriteRequest marshals and frames a request.
func WriteRequest(w io.Writer, req Request) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("browseripc: marshal request: %w", err)
	}
	return WriteFrame(w, payload)
}

// WriteResponse marshals and frames a response.
func WriteResponse(w io.Writer, resp Response) error {
	payload, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("browseripc: marshal response: %w", err)
	}
	return WriteFrame(w, payload)
}

// WriteEvent marshals and frames an event.
func WriteEvent(w io.Writer, ev Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("browseripc: marshal event: %w", err)
	}
	return WriteFrame(w, payload)
}

package plugin

import (
	"context"
	"errors"
	"io"
	"strings"

	mcpjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func isExplicitMCPSessionMissing(err error) bool {
	if errors.Is(err, mcpsdk.ErrSessionMissing) {
		return true
	}
	if !isMCPHTTPNotFound(err) || !hasMCPTransportRejection(err) {
		return false
	}
	found := false
	visitMCPRPCErrors(err, func(rpcErr *mcpjsonrpc.Error) {
		message := strings.ToLower(strings.TrimSpace(rpcErr.Message))
		for _, marker := range []string{
			"session not found",
			"session missing",
			"session expired",
			"invalid session",
			"unknown session",
		} {
			if strings.Contains(message, marker) {
				found = true
			}
		}
	})
	return found
}

// isMCPHTTPNotFound recognizes the status-only error emitted by newer Go MCP
// SDKs for a plain HTTP 404 when no session ID exists. It intentionally does
// not match arbitrary "not found" prose so a tool-level domain error cannot be
// mistaken for an endpoint or protocol mismatch.
func isMCPHTTPNotFound(err error) bool {
	if err == nil {
		return false
	}
	hasRPCError := false
	visitMCPRPCErrors(err, func(*mcpjsonrpc.Error) {
		hasRPCError = true
	})
	if hasRPCError && !hasMCPTransportRejection(err) {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return message == "not found" ||
		strings.HasSuffix(message, ": not found") ||
		strings.Contains(message, "http 404") ||
		strings.Contains(message, "status 404")
}

func hasMCPTransportRejection(err error) bool {
	found := false
	visitMCPRPCErrors(err, func(rpcErr *mcpjsonrpc.Error) {
		if rpcErr.Code == -32005 && strings.EqualFold(strings.TrimSpace(rpcErr.Message), "rejected by transport") {
			found = true
		}
	})
	return found
}

// visitMCPRPCErrors walks every concrete error-tree node because errors.As
// returns only the first matching RPC error and would hide transport evidence.
//
//nolint:errorlint // Direct inspection distinguishes server errors from the SDK transport sentinel.
func visitMCPRPCErrors(err error, visit func(*mcpjsonrpc.Error)) {
	if err == nil {
		return
	}
	if rpcErr, ok := err.(*mcpjsonrpc.Error); ok && rpcErr != nil {
		visit(rpcErr)
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range wrapped.Unwrap() {
			visitMCPRPCErrors(child, visit)
		}
	case interface{ Unwrap() error }:
		visitMCPRPCErrors(wrapped.Unwrap(), visit)
	}
}

func (t *sdkSessionTransport) isStreamableHTTPNotFound(err error) bool {
	return canonicalMCPRuntimeTransport(t.spec.Type) == "streamable-http" && isMCPHTTPNotFound(err)
}

func isTerminalSDKError(err error) bool {
	return errors.Is(err, mcpsdk.ErrConnectionClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func isAmbiguousTransportError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"connection reset", "broken pipe", "connection aborted", "connection refused", "transport is closing"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func classifySessionError(err error) SessionErrorKind {
	if err == nil {
		return SessionErrorNone
	}
	switch {
	case isExplicitMCPSessionMissing(err):
		return SessionErrorSessionMissing
	case errors.Is(err, context.DeadlineExceeded):
		return SessionErrorTimeout
	case isTerminalSDKError(err):
		return SessionErrorStreamClosed
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "unauthorized"), strings.Contains(lower, "forbidden"), strings.Contains(lower, "authorize again"), strings.Contains(lower, "authentication"):
		return SessionErrorAuthRequired
	case strings.Contains(lower, "protocol version"), strings.Contains(lower, "method not found"):
		return SessionErrorProtocol
	default:
		return SessionErrorTransport
	}
}

type sanitizedMCPError struct {
	message string
	cause   error
}

func (e *sanitizedMCPError) Error() string { return e.message }
func (e *sanitizedMCPError) Unwrap() error { return e.cause }

func (t *sdkSessionTransport) sanitizeError(err error, managed *managedMCPSession) error {
	if err == nil {
		return nil
	}
	sessionID := ""
	if managed != nil && managed.session != nil {
		sessionID = managed.session.ID()
	}
	return &sanitizedMCPError{message: t.safeErrorText(err, sessionID), cause: err}
}

func (t *sdkSessionTransport) safeErrorText(err error, sessionID string) string {
	return redactMCPConfigValues(safeMCPErrorText(err, sessionID), t.spec)
}

func redactMCPConfigValues(message string, spec Spec) string {
	values := make([]string, 0, len(spec.Headers)+len(spec.Env)+2)
	values = append(values, spec.WorkspaceRoot, spec.Dir)
	for _, value := range spec.Headers {
		values = append(values, value)
	}
	for _, value := range spec.Env {
		values = append(values, value)
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			message = strings.ReplaceAll(message, value, "[redacted]")
		}
	}
	return message
}

func safeMCPErrorText(err error, sessionID string) string {
	if err == nil {
		return ""
	}
	message := summarizeFailureError(err)
	if sessionID != "" {
		message = strings.ReplaceAll(message, sessionID, "[redacted]")
	}
	if index := strings.Index(strings.ToLower(message), "session id:"); index >= 0 {
		start := index + len("session id:")
		end := strings.IndexByte(message[start:], ')')
		if end >= 0 {
			message = message[:start] + " [redacted]" + message[start+end:]
		}
	}
	return message
}

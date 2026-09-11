package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

func (t *sdkSessionTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	managed, err := t.acquire(ctx)
	if err != nil {
		return nil, t.sanitizeError(err, nil)
	}
	result, err := t.invokeManaged(ctx, managed, method, params)
	if err == nil {
		t.clearRuntimeError(managed)
		return result, nil
	}

	if isExplicitMCPSessionMissing(err) || managed.session.ID() == "" && t.isStreamableHTTPNotFound(err) {
		if managed.session.ID() == "" {
			endpointErr := fmt.Errorf("MCP endpoint returned HTTP 404 without an established session: %w", err)
			t.noteRuntimeError(managed, SessionErrorProtocol, endpointErr)
			return nil, t.sanitizeError(endpointErr, managed)
		}
		t.noteRuntimeError(managed, SessionErrorSessionMissing, err)
		t.invalidate(managed)
		replacement, rebuildErr := t.acquire(ctx)
		if rebuildErr != nil {
			return nil, t.sanitizeError(fmt.Errorf("MCP session expired; rebuild failed: %w", rebuildErr), managed)
		}
		result, err = t.invokeManaged(ctx, replacement, method, params)
		if err == nil {
			t.clearRuntimeError(replacement)
			return result, nil
		}
		return nil, t.sanitizeError(err, replacement)
	}

	if isTerminalSDKError(err) || isAmbiguousTransportError(err) || errors.Is(err, context.DeadlineExceeded) {
		kind := SessionErrorStreamClosed
		if errors.Is(err, context.DeadlineExceeded) {
			kind = SessionErrorTimeout
		} else if !isTerminalSDKError(err) {
			kind = SessionErrorTransport
		}
		t.noteRuntimeError(managed, kind, err)
		t.invalidate(managed)
		if safeToReplayMCPMethod(method) {
			replacement, rebuildErr := t.acquire(ctx)
			if rebuildErr != nil {
				return nil, t.sanitizeError(fmt.Errorf("MCP connection closed; rebuild failed: %w", rebuildErr), managed)
			}
			result, err = t.invokeManaged(ctx, replacement, method, params)
			if err == nil {
				t.clearRuntimeError(replacement)
				return result, nil
			}
			return nil, t.sanitizeError(err, replacement)
		}
		t.startAutoReconnect()
		return nil, t.sanitizeError(fmt.Errorf("MCP tool connection closed after dispatch; execution result is unknown and the call was not retried: %w", err), managed)
	}

	kind := classifySessionError(err)
	t.noteRuntimeError(managed, kind, err)
	return nil, t.sanitizeError(err, managed)
}

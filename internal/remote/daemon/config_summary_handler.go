package daemon

import (
	"context"
	"errors"

	"reasonix/internal/remote/configsummary"
	"reasonix/internal/remote/protocol"
)

// hostConfigSummaryHandlerSet is isolated from newTransport so concurrent
// Phase 6 handler families can be integrated without sharing server.go edits.
// Production must merge this exact entry and pass the daemon-owned provider.
func hostConfigSummaryHandlerSet(t *transport, provider configsummary.Provider) protocol.HandlerSet {
	return protocol.HandlerSet{
		protocol.MethodHostConfigSummary: func(ctx context.Context, value any) (any, error) {
			return t.handleConfigSummary(ctx, value, provider)
		},
	}
}

func (t *transport) handleConfigSummary(ctx context.Context, value any, provider configsummary.Provider) (any, error) {
	params := value.(protocol.HostConfigSummaryParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	if provider == nil {
		err := errors.New("Host config summary provider is unavailable")
		t.server.reportInternal(protocol.MethodHostConfigSummary, err)
		return nil, protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{})
	}
	result, err := provider.Summary(ctx)
	if err != nil {
		// The source error may contain Host-only diagnostics or paths. Report it
		// locally and return only the frozen structured query error to Desktop.
		t.server.reportInternal(protocol.MethodHostConfigSummary, err)
		return nil, protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{})
	}
	return result, nil
}

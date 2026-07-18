package daemon

import (
	"context"
	"errors"

	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
)

// composerHistoryHandlerSet is kept independent of newTransport so the root
// daemon router can merge this completed Phase 6 domain without concurrent
// server.go edits.
func composerHistoryHandlerSet(t *transport) protocol.HandlerSet {
	service, serviceErr := runtimeservice.NewPromptHistoryService(runtimeservice.PromptHistoryOptions{})
	return protocol.HandlerSet{
		protocol.MethodComposerHistory: func(ctx context.Context, value any) (any, error) {
			return t.handleComposerHistory(ctx, value, service, serviceErr)
		},
	}
}

func (t *transport) handleComposerHistory(
	ctx context.Context,
	value any,
	service *runtimeservice.PromptHistoryService,
	serviceErr error,
) (any, error) {
	params := value.(protocol.ComposerHistoryParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	if serviceErr != nil || service == nil {
		if serviceErr == nil {
			serviceErr = errors.New("prompt-history RuntimeService is unavailable")
		}
		t.server.reportInternal(protocol.MethodComposerHistory, serviceErr)
		return nil, protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{})
	}
	mapped, err := t.server.catalog.PromptHistorySessions(ctx, params.ExpectedHostEpoch, params.WorkspaceID)
	if err != nil {
		if _, ok := catalog.ErrorCode(err); ok {
			return nil, t.server.mapCatalogError(protocol.MethodComposerHistory, nil, err)
		}
		t.server.reportInternal(protocol.MethodComposerHistory, err)
		return nil, protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{})
	}
	sources := make([]runtimeservice.PromptHistorySessionSource, len(mapped))
	for index, item := range mapped {
		sources[index] = runtimeservice.PromptHistorySessionSource{
			Session: runtimeapi.SessionRef{
				WorkspaceID: runtimeapi.WorkspaceID(item.Target.WorkspaceID),
				SessionID:   runtimeapi.SessionID(item.Target.SessionID),
			},
			SessionDir: item.SessionDir, SessionPath: item.SessionPath,
		}
	}
	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	page, err := service.History(ctx, runtimeapi.PromptHistoryInput{
		WorkspaceID: runtimeapi.WorkspaceID(params.WorkspaceID),
		Cursor:      runtimeapi.Cursor(params.Cursor),
		Limit:       limit,
	}, sources)
	if err != nil {
		return nil, t.mapComposerHistoryError(err)
	}
	entries := make([]protocol.PromptHistoryEntry, len(page.Entries))
	for index, entry := range page.Entries {
		entries[index] = protocol.PromptHistoryEntry{
			Text: entry.Text, AtMs: entry.AtMillis, Turn: entry.Turn,
			Target: protocol.RuntimeTarget{
				WorkspaceID: protocol.WorkspaceID(entry.Session.WorkspaceID),
				SessionID:   protocol.SessionID(entry.Session.SessionID),
			},
		}
	}
	return protocol.ComposerHistoryResult{
		Entries: entries, HasMore: page.HasMore, NextCursor: protocol.Cursor(page.Next),
	}, nil
}

func (t *transport) mapComposerHistoryError(err error) error {
	switch {
	case errors.Is(err, runtimeservice.ErrInvalidCursor), errors.Is(err, runtimeservice.ErrStaleCursor):
		return protocol.MustRemoteError(protocol.ErrStaleCursor, protocol.ErrorOptions{})
	case errors.Is(err, runtimeservice.ErrInvalidSession), errors.Is(err, runtimeservice.ErrQueryFailed),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{})
	default:
		t.server.reportInternal(protocol.MethodComposerHistory, err)
		return protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{})
	}
}

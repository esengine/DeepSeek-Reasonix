package catalog

import (
	"context"
	"errors"

	"reasonix/internal/remote/protocol"
)

// WorkspaceCatalog resolves the credential-free model/profile choices from
// Host configuration for an open opaque workspace.
func (c *Catalog) WorkspaceCatalog(ctx context.Context, params protocol.WorkspaceCatalogParams) (protocol.WorkspaceCatalogResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		c.mu.Unlock()
		return protocol.WorkspaceCatalogResult{}, err
	}
	workspace, err := c.openWorkspaceLocked(params.WorkspaceID)
	provider, ok := c.profileResolver.(WorkspaceCatalogProvider)
	c.mu.Unlock()
	if err != nil {
		return protocol.WorkspaceCatalogResult{}, err
	}
	if !ok || provider == nil {
		return protocol.WorkspaceCatalogResult{}, catalogError(protocol.ErrQueryFailed, errors.New("Host profile resolver does not expose a workspace catalog"))
	}
	result, err := provider.WorkspaceCatalog(ctx, workspace.CanonicalPath)
	if err != nil {
		return protocol.WorkspaceCatalogResult{}, err
	}
	return result, nil
}

// ValidateLiveTarget is the cold session/close catalog guard. It confirms the
// opaque target still names a live Session without constructing a Controller.
func (c *Catalog) ValidateLiveTarget(ctx context.Context, expected protocol.HostEpoch, target protocol.RuntimeTarget) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(expected); err != nil {
		return err
	}
	workspace, err := c.openWorkspaceLocked(target.WorkspaceID)
	if err != nil {
		return err
	}
	if err := c.syncWorkspaceSessionsLocked(ctx, workspace); err != nil {
		return err
	}
	_, _, err = c.liveRecordLocked(target)
	return err
}

// TopicTargets freezes the current live membership used by topic/trash while
// the daemon Host catalog sequencer excludes concurrent directory mutations.
func (c *Catalog) TopicTargets(ctx context.Context, expected protocol.HostEpoch, workspaceID protocol.WorkspaceID, topicID protocol.TopicID) ([]protocol.RuntimeTarget, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(expected); err != nil {
		return nil, err
	}
	workspace, err := c.openWorkspaceLocked(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := c.syncWorkspaceSessionsLocked(ctx, workspace); err != nil {
		return nil, err
	}
	topic, ok := c.state.Topics[workspaceID][topicID]
	if !ok || topic.Trashed {
		return nil, catalogError(protocol.ErrTopicNotFound, errors.New("unknown Topic identity"))
	}
	records := c.liveTopicSessionsLocked(workspaceID, topicID)
	targets := make([]protocol.RuntimeTarget, 0, len(records))
	for _, record := range records {
		targets = append(targets, protocol.RuntimeTarget{WorkspaceID: workspaceID, SessionID: record.ID})
	}
	return targets, nil
}

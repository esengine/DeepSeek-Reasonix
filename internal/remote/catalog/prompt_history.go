package catalog

import (
	"context"
	"path/filepath"

	"reasonix/internal/remote/protocol"
)

// PromptHistorySession is a Host-only safe mapping from an opaque live target
// to its transcript. It is deliberately not a wire DTO; daemon adapters must
// project only Target through composer/history results.
type PromptHistorySession struct {
	Target      protocol.RuntimeTarget
	SessionDir  string `json:"-"`
	SessionPath string `json:"-"`
}

// PromptHistorySessions freezes the workspace's live Session membership and
// validates every transcript while holding the catalog boundary. Trashed,
// closed-workspace, missing, symlinked, and cross-workspace artifacts never
// reach the shared history scanner.
func (c *Catalog) PromptHistorySessions(
	ctx context.Context,
	expected protocol.HostEpoch,
	workspaceID protocol.WorkspaceID,
) ([]PromptHistorySession, error) {
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
	records := c.liveSessionRecordsLocked(workspaceID)
	result := make([]PromptHistorySession, 0, len(records))
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := c.validateLiveSessionRecordLocked(workspace, record); err != nil {
			return nil, err
		}
		result = append(result, PromptHistorySession{
			Target:     protocol.RuntimeTarget{WorkspaceID: workspaceID, SessionID: record.ID},
			SessionDir: filepath.Dir(record.Path), SessionPath: record.Path,
		})
	}
	return result, nil
}

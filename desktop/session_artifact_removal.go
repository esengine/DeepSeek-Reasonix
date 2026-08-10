package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
)

func removeDesktopSessionArtifactsWithGuard(path string, guard *agent.SessionRemovalGuard) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if guard == nil {
		return fmt.Errorf("session removal guard is required")
	}
	defer guard.Release()
	if err := invalidateTopicDirMarkers(filepath.Dir(path)); err != nil {
		return err
	}
	defer invalidateTopicSessionIndexForPath(path)
	for _, artifact := range sessionOwnedArtifactPaths(path) {
		if strings.TrimSpace(artifact) == "" {
			continue
		}
		if err := os.RemoveAll(artifact); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := guard.RemoveSidecarsAndRelease(); err != nil {
		return err
	}
	if err := removeSessionDisplay(filepath.Dir(path), path); err != nil {
		return err
	}
	if err := removeSessionPlannerDisplay(filepath.Dir(path), path); err != nil {
		return err
	}
	if err := agent.DeleteSubagentsByParent(filepath.Dir(path), agent.BranchID(path)); err != nil {
		return err
	}
	return agent.ClearCleanupPending(path)
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/agent"
)

func reconcileDesktopTrashSessionArtifacts(dir, sessionPath, key string) error {
	// Hold the removal guard across the whole move so no runtime can acquire
	// the session (or save into it) while its artifacts are relocated; the
	// lock sidecars are deleted atomically with the guard release.
	guard, err := acquireSessionRemovalGuard(sessionPath)
	if err != nil {
		return err
	}
	return trashSessionArtifactsWithGuard(dir, sessionPath, key, guard)
}

func trashSessionArtifactsWithGuard(dir, sessionPath, key string, guard *agent.SessionRemovalGuard) error {
	if guard == nil {
		return errSessionBusyElsewhere
	}
	defer guard.Release()
	itemDir := filepath.Join(sessionTrashPath(dir), key)
	if info, err := os.Stat(itemDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("session trash target is not a directory: %s", key)
		}
		trashPath := filepath.Join(itemDir, key)
		if trashInfo, err := os.Stat(trashPath); err == nil && !trashInfo.IsDir() {
			_, liveErr := os.Stat(sessionPath)
			if liveErr != nil && !os.IsNotExist(liveErr) {
				return liveErr
			}
			if liveErr == nil {
				discardable, err := liveSessionContentDiscardable(sessionPath)
				if err != nil {
					return err
				}
				if discardable {
					return removeDesktopSessionArtifactsWithGuard(sessionPath, guard)
				}
				matches, err := trashSessionMatchesLive(sessionPath, trashPath)
				if err != nil {
					return err
				}
				if matches {
					return removeDesktopSessionArtifactsWithGuard(sessionPath, guard)
				}
				itemDir, err = reserveUniqueSessionTrashItemDir(dir, key)
				if err != nil {
					return err
				}
			}
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(itemDir, 0o755); err != nil {
			return err
		}
	} else {
		return err
	}
	defer invalidateTopicSessionIndexForPath(sessionPath)
	if err := invalidateTopicDirMarkers(dir); err != nil {
		return err
	}
	for _, artifact := range sessionTrashArtifacts(sessionPath, key) {
		if err := movePathIfExists(artifact.src, filepath.Join(itemDir, artifact.name)); err != nil {
			return err
		}
	}
	if err := trashSubagentArtifacts(dir, sessionPath, itemDir); err != nil {
		return err
	}
	if err := guard.RemoveSidecarsAndRelease(); err != nil {
		return err
	}
	meta := trashedSessionMeta{Key: key, DeletedAt: time.Now().UnixMilli()}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(itemDir, sessionTrashMetaFile), b, 0o644); err != nil {
		return err
	}
	return agent.ClearCleanupPending(sessionPath)
}

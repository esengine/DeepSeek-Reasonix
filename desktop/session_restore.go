package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func restoreTrashedSessionFile(dir, path string) error {
	_, key, itemDir, err := validateTrashedSessionPath(dir, path)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, key)
	replaceDiscardableTranscript := false
	if _, err := os.Stat(target); err == nil {
		discardable, err := liveSessionDiscardable(target)
		if err != nil {
			return err
		}
		if !discardable {
			return fmt.Errorf("session already exists: %s", key)
		}
		replaceDiscardableTranscript = true
	} else if !os.IsNotExist(err) {
		return err
	}
	// Preflight every owned destination before removing the verified-empty
	// transcript. A newly created inbox or other sidecar may already hold work
	// even while the transcript itself is still discardable.
	if err := checkRestoreSessionArtifactConflicts(target, key, replaceDiscardableTranscript); err != nil {
		return err
	}
	if err := checkRestoreSubagentConflicts(dir, itemDir); err != nil {
		return err
	}
	if replaceDiscardableTranscript {
		if err := removeDesktopSessionArtifacts(target); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, artifact := range sessionTrashArtifacts(target, key) {
		if err := movePathIfExists(filepath.Join(itemDir, artifact.name), artifact.src); err != nil {
			return err
		}
	}
	if err := restoreSubagentArtifacts(dir, itemDir); err != nil {
		return err
	}
	return os.RemoveAll(itemDir)
}

func checkRestoreSessionArtifactConflicts(target, key string, allowDiscardableTranscript bool) error {
	for _, artifact := range sessionTrashArtifacts(target, key) {
		if allowDiscardableTranscript && artifact.src == target {
			continue
		}
		if _, err := os.Lstat(artifact.src); err == nil {
			return fmt.Errorf("session artifact already exists: %s", artifact.name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

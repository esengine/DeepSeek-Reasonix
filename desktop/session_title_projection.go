package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
)

// syncSessionTitleFromBranchMeta projects the current canonical custom title
// while holding the legacy map lock, so a delayed callback observes a newer
// rename instead of publishing the stale title value it originally received.
func syncSessionTitleFromBranchMeta(dir, sessionPath string) error {
	sessionPath, _, err := validateSessionPath(dir, sessionPath)
	if err != nil {
		return err
	}
	key := filepath.Base(sessionPath)
	var loadErr error
	err = updateSessionTitles(dir, func(m map[string]string) bool {
		meta, ok, err := agent.LoadBranchMeta(sessionPath)
		if err != nil {
			loadErr = err
			return false
		}
		title := ""
		if ok {
			title = strings.TrimSpace(meta.CustomTitle)
		}
		if title == "" {
			if _, exists := m[key]; !exists {
				return false
			}
			delete(m, key)
			return true
		}
		if m[key] == title {
			return false
		}
		m[key] = title
		return true
	})
	if err != nil {
		return err
	}
	if loadErr != nil {
		return fmt.Errorf("load canonical session title: %w", loadErr)
	}
	return nil
}

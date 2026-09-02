package agent

import (
	"fmt"
	"strings"
)

// UpdateSessionListingProjectionIfCurrent publishes counts decoded from one
// persisted transcript generation. It rechecks both the transcript digest and
// its sidecar identity while holding the save lock, so an autosave that landed
// after the caller's decode cannot receive the stale projection.
func UpdateSessionListingProjectionIfCurrent(sessionPath, model, preview string, turns int, markActivity bool, expected PersistedState) (bool, error) {
	if strings.TrimSpace(sessionPath) == "" {
		return false, fmt.Errorf("empty session path")
	}
	unlockSave := lockSessionSavePath(sessionPath)
	defer unlockSave()
	unlockFile, err := lockSessionFile(sessionPath)
	if err != nil {
		return false, fmt.Errorf("lock session file: %w", err)
	}
	defer unlockFile()
	_, current, _, err := loadSessionDisplayMessagesUnlocked(sessionPath)
	if err != nil {
		return false, err
	}
	if current.DigestHex != expected.DigestHex || current.RevisionKnown != expected.RevisionKnown ||
		current.RevisionKnown && current.Revision != expected.Revision {
		return false, nil
	}

	unlockMeta, err := LockSessionMetaPath(sessionPath)
	if err != nil {
		return false, err
	}
	defer unlockMeta()
	meta, err := ensureBranchMetaUnlocked(sessionPath)
	if err != nil {
		return false, err
	}
	digest := strings.TrimSpace(meta.ContentDigest)
	if expected.RevisionKnown {
		if meta.Revision != expected.Revision || digest != expected.DigestHex {
			return false, nil
		}
	} else if meta.Revision != 0 || digest != "" {
		return false, nil
	}
	if strings.TrimSpace(model) != "" {
		meta.Model = strings.TrimSpace(model)
	}
	meta.Preview = preview
	meta.Turns = turns
	meta.SchemaVersion = BranchMetaCountsVersion
	stampSessionListingProjection(&meta)
	if err := saveBranchMeta(sessionPath, meta, markActivity); err != nil {
		return false, err
	}
	return true, nil
}

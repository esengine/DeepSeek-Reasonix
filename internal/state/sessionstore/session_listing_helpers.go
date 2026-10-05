package sessionstore

import (
	"os"
	"time"
)

// SessionInfo summarises a saved session for the --resume picker: where it is on
// disk, when it was created/last active, the first user message as a preview, and
// a rough turn count.
type SessionInfo struct {
	Path           string
	CreatedAt      time.Time
	LastActivityAt time.Time
	ModTime        time.Time // compatibility alias for LastActivityAt
	Preview        string
	Turns          int
	CountsKnown    bool
	Scope          string
	WorkspaceRoot  string
	TopicID        string
	TopicTitle     string
	CustomTitle    string
	Archived       bool
	Recovered      bool
	RecoveryReason string
	RecoveryDigest string
	ParentID       string
	RecoveryRootID string
}

// SessionOrderInfo is the lightweight sidecar/mtime ordering record shared by
// session pickers and prompt-history navigation. It intentionally avoids reading
// JSONL content; callers that need previews can layer that on afterwards.
type SessionOrderInfo struct {
	Path           string
	CreatedAt      time.Time
	LastActivityAt time.Time
	ModTime        time.Time // compatibility alias for LastActivityAt
	Scope          string
	WorkspaceRoot  string
	TopicID        string
	TopicTitle     string
	CustomTitle    string
	Archived       bool
	Recovered      bool
	RecoveryReason string
	RecoveryDigest string
	ParentID       string
	RecoveryRootID string
	// Turns and Preview are the cached listing fields from the sidecar; SchemaVersion
	// >= BranchMetaCountsVersion means they were recorded from content and can
	// be trusted (even Turns == 0). ListSessions uses them to skip the whole-file decode.
	Turns         int
	Preview       string
	SchemaVersion int
	// Revision and ContentDigest bind a listing backfill to the transcript
	// generation it decoded. They are sidecar-only compare-and-apply guards and
	// are not exposed through SessionInfo.
	Revision      int64
	ContentDigest string
}

func sessionInfoFromOrder(session SessionOrderInfo, preview string, turns int, countsKnown bool) SessionInfo {
	return SessionInfo{
		Path:           session.Path,
		CreatedAt:      session.CreatedAt,
		LastActivityAt: session.LastActivityAt,
		ModTime:        session.ModTime,
		Preview:        preview,
		Turns:          turns,
		CountsKnown:    countsKnown,
		Scope:          session.Scope,
		WorkspaceRoot:  session.WorkspaceRoot,
		TopicID:        session.TopicID,
		TopicTitle:     session.TopicTitle,
		CustomTitle:    session.CustomTitle,
		Archived:       session.Archived,
		Recovered:      session.Recovered,
		RecoveryReason: session.RecoveryReason,
		RecoveryDigest: session.RecoveryDigest,
		ParentID:       session.ParentID,
		RecoveryRootID: session.RecoveryRootID,
	}
}

func sessionArtifactsHaveContent(path string) bool {
	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Size() > 0 {
		return true
	}
	return sessionEventLogSize(path) > 0
}

func sessionListingCountsNeedRefresh(schemaVersion, turns int) bool {
	if schemaVersion < branchMetaCountsInitialVersion {
		return true
	}
	return turns == 0 && schemaVersion < BranchMetaCountsVersion
}

func updateSessionListingCountsIfCurrent(session SessionOrderInfo, preview string, turns int) (string, int, error) {
	unlock, err := LockSessionMetaPath(session.Path)
	if err != nil {
		return preview, turns, err
	}
	defer unlock()

	current, err := ensureBranchMetaUnlocked(session.Path)
	if err != nil {
		return preview, turns, err
	}
	if current.SchemaVersion != session.SchemaVersion ||
		current.Turns != session.Turns ||
		current.Preview != session.Preview ||
		current.Revision != session.Revision ||
		current.ContentDigest != session.ContentDigest {
		return current.Preview, current.Turns, nil
	}
	if !sessionListingCountsNeedRefresh(current.SchemaVersion, current.Turns) {
		return current.Preview, current.Turns, nil
	}

	current.Preview = preview
	current.Turns = turns
	current.SchemaVersion = BranchMetaCountsVersion
	if err := saveBranchMeta(session.Path, current, false); err != nil {
		return preview, turns, err
	}
	return preview, turns, nil
}

func previewSessionWithError(path string) (string, int, error) {
	msgs, _, _, err := loadSessionMessages(path)
	if err != nil {
		return "", 0, err
	}
	preview, turns := SessionPreviewFromMessages(msgs)
	return preview, turns, nil
}

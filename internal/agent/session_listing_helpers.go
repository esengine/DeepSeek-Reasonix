package agent

import (
	"os"
	"strings"
	"time"
)

// SessionOrderInfo is the lightweight sidecar/mtime ordering record shared by
// session pickers and prompt-history navigation. It intentionally avoids JSONL.
type SessionOrderInfo struct {
	Path              string
	CreatedAt         time.Time
	LastActivityAt    time.Time
	ModTime           time.Time // compatibility alias for LastActivityAt
	Scope             string
	WorkspaceRoot     string
	TopicID           string
	TopicTitle        string
	CustomTitle       string
	Recovered         bool
	RecoveryReason    string
	RecoveryDigest    string
	ParentID          string
	RecoveryPreferred bool
	// Counts are trusted only when their listing identity matches the transcript.
	Turns         int
	Preview       string
	SchemaVersion int
	// The transcript and listing identities also fence async listing backfills.
	Revision             int64
	ContentDigest        string
	ListingRevision      int64
	ListingContentDigest string
}

// ListingProjectionFresh reports whether Turns/Preview describe this record's
// current transcript generation. Legacy sidecars that predate the content
// ledger remain usable until their first revisioned save.
func (s SessionOrderInfo) ListingProjectionFresh() bool {
	return sessionListingProjectionFresh(s.SchemaVersion, s.Turns, s.Revision, s.ListingRevision, s.ContentDigest, s.ListingContentDigest)
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
		Recovered:      session.Recovered,
		RecoveryReason: session.RecoveryReason,
		RecoveryDigest: session.RecoveryDigest,
		ParentID:       session.ParentID,
	}
}

func sessionArtifactsHaveContent(path string) bool {
	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Size() > 0 {
		return true
	}
	return sessionEventLogSize(path) > 0
}

func sessionListingProjectionFresh(schemaVersion, turns int, revision, listingRevision int64, contentDigest, listingContentDigest string) bool {
	if schemaVersion < branchMetaCountsInitialVersion {
		return false
	}
	if turns == 0 && schemaVersion < BranchMetaCountsVersion {
		return false
	}
	contentDigest = strings.TrimSpace(contentDigest)
	listingContentDigest = strings.TrimSpace(listingContentDigest)
	if revision == 0 && listingRevision == 0 && contentDigest == "" && listingContentDigest == "" {
		return true
	}
	return revision > 0 && listingRevision == revision && contentDigest != "" && listingContentDigest == contentDigest
}

func stampSessionListingProjection(meta *BranchMeta) {
	if meta == nil {
		return
	}
	meta.ListingRevision = meta.Revision
	meta.ListingContentDigest = strings.TrimSpace(meta.ContentDigest)
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
	if sessionListingProjectionFresh(current.SchemaVersion, current.Turns, current.Revision, current.ListingRevision, current.ContentDigest, current.ListingContentDigest) {
		return current.Preview, current.Turns, nil
	}

	current.Preview = preview
	current.Turns = turns
	current.SchemaVersion = BranchMetaCountsVersion
	stampSessionListingProjection(&current)
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

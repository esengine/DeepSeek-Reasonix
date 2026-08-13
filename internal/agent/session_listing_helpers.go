package agent

import "os"

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

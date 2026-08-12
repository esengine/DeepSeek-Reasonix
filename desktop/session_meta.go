package main

import "reasonix/internal/agent"

func sessionMetaFromInfo(s agent.SessionInfo, title string, current, open bool, deletedAt int64, parentDir string) SessionMeta {
	turnsState := "unknown"
	if s.CountsKnown {
		turnsState = "valid"
	}
	return SessionMeta{
		Path:           s.Path,
		Preview:        s.Preview,
		Title:          title,
		Turns:          s.Turns,
		TurnsState:     turnsState,
		CreatedAt:      s.CreatedAt.UnixMilli(),
		LastActivityAt: s.LastActivityAt.UnixMilli(),
		ModTime:        s.LastActivityAt.UnixMilli(),
		DeletedAt:      deletedAt,
		Current:        current,
		Open:           open,
		Scope:          s.Scope,
		WorkspaceRoot:  s.WorkspaceRoot,
		TopicID:        s.TopicID,
		TopicTitle:     s.TopicTitle,
		Recovered:      sessionInfoIsAutomaticRecovery(s),
		RecoveryCopy:   sessionInfoIsUnmodifiedRecoveryCopy(s, parentDir),
	}
}

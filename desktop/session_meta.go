package main

import "reasonix/internal/agent"

// SessionMeta summarises one saved session for the history panel.
type SessionMeta struct {
	Path           string `json:"path"`
	Preview        string `json:"preview"`         // first user message
	Title          string `json:"title,omitempty"` // user-chosen name, when set (overrides preview)
	Turns          int    `json:"turns"`
	TurnsState     string `json:"turnsState"`
	CreatedAt      int64  `json:"createdAt"`      // unix milliseconds
	LastActivityAt int64  `json:"lastActivityAt"` // unix milliseconds
	ModTime        int64  `json:"modTime"`        // compatibility alias for lastActivityAt
	DeletedAt      int64  `json:"deletedAt,omitempty"`
	Current        bool   `json:"current"`
	Open           bool   `json:"open"`
	Scope          string `json:"scope,omitempty"`
	WorkspaceRoot  string `json:"workspaceRoot,omitempty"`
	TopicID        string `json:"topicId,omitempty"`
	TopicTitle     string `json:"topicTitle,omitempty"`
	Kind           string `json:"kind,omitempty"` // "channel" for external IM transcripts
	Channel        string `json:"channel,omitempty"`
	ChannelLabel   string `json:"channelLabel,omitempty"`
	RemoteID       string `json:"remoteId,omitempty"`
	ChatType       string `json:"chatType,omitempty"`
	UserID         string `json:"userId,omitempty"`
	ThreadID       string `json:"threadId,omitempty"`
	SessionSource  string `json:"sessionSource,omitempty"`
	Recovered      bool   `json:"recovered,omitempty"`    // created by conflict recovery, including an adopted/continued branch
	RecoveryCopy   bool   `json:"recoveryCopy,omitempty"` // actual branch content is unchanged and covered by its parent
	// Optional v1.24.2 lineage fields; older clients ignore them safely.
	RecoveryGroupID   string `json:"recoveryGroupId,omitempty"`
	RecoveryRole      string `json:"recoveryRole,omitempty"` // normal|covered_copy|adopted|diverged
	RecoveryCanonical bool   `json:"recoveryCanonical,omitempty"`
}

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

package protocol

type ProfilePatch struct {
	Model             *string            `json:"model,omitempty" validate:"nonempty"`
	Effort            *string            `json:"effort,omitempty" validate:"nonempty"`
	CollaborationMode *CollaborationMode `json:"collaborationMode,omitempty"`
	TokenMode         *TokenMode         `json:"tokenMode,omitempty"`
	ToolApprovalMode  *ToolApprovalMode  `json:"toolApprovalMode,omitempty"`
}

type ProfileSelection struct {
	Model             *string            `json:"model,omitempty" validate:"nonempty"`
	Effort            *string            `json:"effort,omitempty" validate:"nonempty"`
	CollaborationMode *CollaborationMode `json:"collaborationMode,omitempty"`
	TokenMode         *TokenMode         `json:"tokenMode,omitempty"`
	ToolApprovalMode  *ToolApprovalMode  `json:"toolApprovalMode,omitempty"`
}

func (p ProfilePatch) Validate() error {
	if p.Model == nil && p.Effort == nil && p.CollaborationMode == nil && p.TokenMode == nil && p.ToolApprovalMode == nil {
		return validationError("profile patch must contain at least one field")
	}
	return nil
}

type TopicSelection struct {
	Kind    TopicSelectionKind `json:"kind"`
	TopicID TopicID            `json:"topicId,omitempty"`
	Title   string             `json:"title,omitempty"`
}

func (t TopicSelection) Validate() error {
	switch t.Kind {
	case TopicExisting:
		if t.TopicID == "" || t.Title != "" {
			return validationError("existing topic requires topicId and forbids title")
		}
	case TopicNew:
		if t.TopicID != "" {
			return validationError("new topic forbids topicId")
		}
	}
	return nil
}

type SessionCreateParams struct {
	HostMutation
	WorkspaceID             WorkspaceID      `json:"workspaceId"`
	AdditionalDirectoryRefs []DirectoryRef   `json:"additionalDirectoryRefs"`
	Topic                   TopicSelection   `json:"topic"`
	Profile                 ProfileSelection `json:"profile"`
}

type SessionCreateResult struct {
	Target          RuntimeTarget   `json:"target"`
	RuntimeEpoch    RuntimeEpoch    `json:"runtimeEpoch"`
	TopicID         TopicID         `json:"topicId"`
	TopicTitle      string          `json:"topicTitle" validate:"nonempty"`
	ResolvedProfile ResolvedProfile `json:"resolvedProfile"`
}

type SessionListParams struct {
	ExpectedHostEpoch HostEpoch   `json:"expectedHostEpoch"`
	WorkspaceID       WorkspaceID `json:"workspaceId"`
	Cursor            Cursor      `json:"cursor,omitempty"`
	Limit             *int        `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type BranchSource struct {
	ParentTarget       RuntimeTarget `json:"parentTarget"`
	ParentCheckpointID CheckpointID  `json:"parentCheckpointId"`
}

type SessionRuntimeSummary struct {
	RuntimeEpoch  RuntimeEpoch `json:"runtimeEpoch"`
	Running       bool         `json:"running"`
	PendingPrompt bool         `json:"pendingPrompt"`
	ActiveJobs    int          `json:"activeJobs" validate:"min=0"`
}

type SessionSummary struct {
	Target              RuntimeTarget          `json:"target"`
	TopicID             TopicID                `json:"topicId"`
	Title               string                 `json:"title" validate:"nonempty"`
	Preview             string                 `json:"preview"`
	Turns               int                    `json:"turns" validate:"min=0"`
	CreatedAtMs         int64                  `json:"createdAtMs" validate:"min=0"`
	LastActivityAtMs    int64                  `json:"lastActivityAtMs" validate:"min=0"`
	BranchSource        *BranchSource          `json:"branchSource,omitempty"`
	RecoveryInterrupted bool                   `json:"recoveryInterrupted"`
	Runtime             *SessionRuntimeSummary `json:"runtime,omitempty"`
}

type SessionListResult struct {
	Items      []SessionSummary `json:"items"`
	HasMore    bool             `json:"hasMore"`
	NextCursor Cursor           `json:"nextCursor,omitempty"`
}

type SessionCloseParams struct {
	SessionMutation
}

type SessionCloseDisposition string

const (
	SessionReleased       SessionCloseDisposition = "released"
	SessionRetainedActive SessionCloseDisposition = "retained_active"
	SessionAlreadyClosed  SessionCloseDisposition = "already_closed"
)

type SessionCloseResult struct {
	Disposition SessionCloseDisposition `json:"disposition"`
}

type TopicListParams struct {
	ExpectedHostEpoch HostEpoch   `json:"expectedHostEpoch"`
	WorkspaceID       WorkspaceID `json:"workspaceId"`
	Cursor            Cursor      `json:"cursor,omitempty"`
	Limit             *int        `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type TopicSummary struct {
	TopicID          TopicID `json:"topicId"`
	Title            string  `json:"title" validate:"nonempty"`
	CreatedAtMs      int64   `json:"createdAtMs" validate:"min=0"`
	SessionCount     int     `json:"sessionCount" validate:"min=0"`
	LastActivityAtMs int64   `json:"lastActivityAtMs" validate:"min=0"`
}

type TopicListResult struct {
	Items      []TopicSummary `json:"items"`
	HasMore    bool           `json:"hasMore"`
	NextCursor Cursor         `json:"nextCursor,omitempty"`
}

type TopicCreateParams struct {
	HostMutation
	WorkspaceID WorkspaceID `json:"workspaceId"`
	Title       string      `json:"title,omitempty"`
}

type TopicCreateResult struct {
	TopicID      TopicID `json:"topicId"`
	Title        string  `json:"title" validate:"nonempty"`
	CreatedAtMs  int64   `json:"createdAtMs" validate:"min=0"`
	SessionCount int     `json:"sessionCount" validate:"min=0"`
}

func (r TopicCreateResult) Validate() error {
	if r.SessionCount != 0 {
		return validationError("new Topic sessionCount must be zero")
	}
	return nil
}

type TopicRenameParams struct {
	HostMutation
	WorkspaceID WorkspaceID `json:"workspaceId"`
	TopicID     TopicID     `json:"topicId"`
	Title       string      `json:"title" validate:"nonempty"`
}

type TopicRenameResult struct {
	Title string `json:"title" validate:"nonempty"`
}

type TopicDeleteParams struct {
	HostMutation
	WorkspaceID WorkspaceID `json:"workspaceId"`
	TopicID     TopicID     `json:"topicId"`
}

type TopicDeleteResult struct {
	Deleted bool `json:"deleted" validate:"true"`
}

type TopicTrashParams struct {
	HostMutation
	WorkspaceID WorkspaceID `json:"workspaceId"`
	TopicID     TopicID     `json:"topicId"`
}

type CleanupDisposition string

const (
	DispositionTrashed        CleanupDisposition = "trashed"
	DispositionCleanupPending CleanupDisposition = "cleanup_pending"
	DispositionAlreadyTrashed CleanupDisposition = "already_trashed"
)

type TopicTrashResult struct {
	Disposition     CleanupDisposition `json:"disposition"`
	TrashedSessions int                `json:"trashedSessions" validate:"min=0"`
}

type SessionRenameParams struct {
	SessionRecordMutation
	Title string `json:"title" validate:"nonempty"`
}

type SessionRenameResult struct {
	Title string `json:"title" validate:"nonempty"`
}

type SessionTrashListParams struct {
	ExpectedHostEpoch HostEpoch   `json:"expectedHostEpoch"`
	WorkspaceID       WorkspaceID `json:"workspaceId"`
	Cursor            Cursor      `json:"cursor,omitempty"`
	Limit             *int        `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type TrashEntry struct {
	Target       RuntimeTarget `json:"target"`
	TopicID      TopicID       `json:"topicId"`
	Title        string        `json:"title" validate:"nonempty"`
	Preview      string        `json:"preview"`
	TrashedAtMs  int64         `json:"trashedAtMs" validate:"min=0"`
	RecoveryCopy bool          `json:"recoveryCopy"`
}

type SessionTrashListResult struct {
	Items      []TrashEntry `json:"items"`
	HasMore    bool         `json:"hasMore"`
	NextCursor Cursor       `json:"nextCursor,omitempty"`
}

type SessionTrashParams struct {
	SessionRecordMutation
	Guard TrashGuard `json:"guard"`
}

type SessionTrashResult struct {
	Disposition CleanupDisposition `json:"disposition"`
}

type SessionRestoreParams struct {
	SessionRecordMutation
}

type SessionRestoreDisposition string

const SessionRestored SessionRestoreDisposition = "restored"

type SessionRestoreResult struct {
	Target      RuntimeTarget             `json:"target"`
	TopicID     TopicID                   `json:"topicId"`
	Disposition SessionRestoreDisposition `json:"disposition"`
}

type SessionPurgeParams struct {
	SessionRecordMutation
	Guard TrashGuard `json:"guard"`
}

type SessionPurgeResult struct {
	Purged bool `json:"purged" validate:"true"`
}

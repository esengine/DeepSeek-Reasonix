package runtimeapi

// HostConfigSummary detail is safe, resolved presentation data. It never
// includes provider credentials or writable Host configuration.
type EffectiveScope struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type ConfigDisplayPath struct {
	Scope       string `json:"scope"`
	DisplayPath string `json:"displayPath"`
}

type FeatureState struct {
	Feature   string `json:"feature"`
	Available bool   `json:"available"`
	Summary   string `json:"summary,omitempty"`
}

type CLIHint struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

type ListWorkspacesInput struct {
	Cursor Cursor `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type WorkspaceListPage struct {
	Items   []Workspace `json:"items"`
	HasMore bool        `json:"hasMore"`
	Next    Cursor      `json:"next,omitempty"`
}

type CloseWorkspaceInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
}

type WorkspaceCloseDisposition string

const (
	WorkspaceClosed        WorkspaceCloseDisposition = "closed"
	WorkspaceAlreadyClosed WorkspaceCloseDisposition = "already_closed"
)

type CloseWorkspaceResult struct {
	Disposition WorkspaceCloseDisposition `json:"disposition"`
}

type WorkspaceCatalogInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
}

type EffortCatalog struct {
	Supported bool     `json:"supported"`
	Default   string   `json:"default,omitempty"`
	Levels    []string `json:"levels"`
}

type ModelCatalogItem struct {
	Ref      ModelRef      `json:"ref"`
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Effort   EffortCatalog `json:"effort"`
}

type WorkspaceCatalog struct {
	Revision           CatalogRevision    `json:"revision"`
	Models             []ModelCatalogItem `json:"models"`
	CollaborationModes []string           `json:"collaborationModes"`
	TokenModes         []string           `json:"tokenModes"`
	ToolApprovalModes  []string           `json:"toolApprovalModes"`
	DefaultProfile     ResolvedProfile    `json:"defaultProfile"`
}

type SessionCatalogInput struct {
	Session SessionRef `json:"session"`
}

type CommandCatalogItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type MCPServerCatalogItem struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	ToolCount int    `json:"toolCount"`
}

type SkillCatalogItem struct {
	ID          SkillID `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Scope       string  `json:"scope"`
}

type PluginCatalogItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type SessionCatalog struct {
	Revision   CatalogRevision        `json:"revision"`
	Commands   []CommandCatalogItem   `json:"commands"`
	MCPServers []MCPServerCatalogItem `json:"mcpServers"`
	Skills     []SkillCatalogItem     `json:"skills"`
	Plugins    []PluginCatalogItem    `json:"plugins"`
}

type CatalogScope string

const (
	CatalogHost      CatalogScope = "host"
	CatalogWorkspace CatalogScope = "workspace"
)

type CatalogKind string

const (
	CatalogTopics           CatalogKind = "topics"
	CatalogSessions         CatalogKind = "sessions"
	CatalogTrash            CatalogKind = "trash"
	CatalogWorkspaceCatalog CatalogKind = "workspaceCatalog"
	CatalogSessionCatalog   CatalogKind = "sessionCatalog"
	CatalogMemory           CatalogKind = "memory"
	CatalogResearch         CatalogKind = "research"
)

// CatalogInvalidation is a cache invalidation only. Callers must re-query the
// corresponding RuntimeAPI domain instead of treating this value as a catalog
// snapshot. Transport generation and Host epoch remain adapter-private.
type CatalogInvalidation struct {
	Revision             CatalogRevision `json:"revision"`
	Scope                CatalogScope    `json:"scope"`
	AffectedWorkspaceIDs []WorkspaceID   `json:"affectedWorkspaceIds,omitempty"`
	Kinds                []CatalogKind   `json:"kinds"`
}

type ListTopicsInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	Cursor      Cursor      `json:"cursor,omitempty"`
	Limit       int         `json:"limit,omitempty"`
}

type TopicSummary struct {
	TopicID            TopicID `json:"topicId"`
	Title              string  `json:"title"`
	CreatedAtMillis    int64   `json:"createdAtMillis"`
	SessionCount       int     `json:"sessionCount"`
	LastActivityMillis int64   `json:"lastActivityMillis"`
}

type TopicPage struct {
	Items   []TopicSummary `json:"items"`
	HasMore bool           `json:"hasMore"`
	Next    Cursor         `json:"next,omitempty"`
}

type CreateTopicInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	Title       string      `json:"title,omitempty"`
}

type CreatedTopic struct {
	TopicID         TopicID `json:"topicId"`
	Title           string  `json:"title"`
	CreatedAtMillis int64   `json:"createdAtMillis"`
	SessionCount    int     `json:"sessionCount"`
}

type RenameTopicInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	TopicID     TopicID     `json:"topicId"`
	Title       string      `json:"title"`
}

type RenameTopicResult struct {
	Title string `json:"title"`
}

type DeleteTopicInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	TopicID     TopicID     `json:"topicId"`
}

type DeleteTopicResult struct {
	Deleted bool `json:"deleted"`
}

type TrashTopicInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	TopicID     TopicID     `json:"topicId"`
}

type CleanupDisposition string

const (
	CleanupTrashed        CleanupDisposition = "trashed"
	CleanupPending        CleanupDisposition = "cleanup_pending"
	CleanupAlreadyTrashed CleanupDisposition = "already_trashed"
)

type TrashTopicResult struct {
	Disposition     CleanupDisposition `json:"disposition"`
	TrashedSessions int                `json:"trashedSessions"`
}

type ListSessionsInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	Cursor      Cursor      `json:"cursor,omitempty"`
	Limit       int         `json:"limit,omitempty"`
}

type BranchSource struct {
	Parent             SessionRef   `json:"parent"`
	ParentCheckpointID CheckpointID `json:"parentCheckpointId"`
}

type SessionRuntimeSummary struct {
	Running       bool `json:"running"`
	PendingPrompt bool `json:"pendingPrompt"`
	ActiveJobs    int  `json:"activeJobs"`
}

type SessionSummary struct {
	Session             SessionRef             `json:"session"`
	TopicID             TopicID                `json:"topicId"`
	Title               string                 `json:"title"`
	Preview             string                 `json:"preview"`
	Turns               int                    `json:"turns"`
	CreatedAtMillis     int64                  `json:"createdAtMillis"`
	LastActivityMillis  int64                  `json:"lastActivityMillis"`
	BranchSource        *BranchSource          `json:"branchSource,omitempty"`
	RecoveryInterrupted bool                   `json:"recoveryInterrupted"`
	Runtime             *SessionRuntimeSummary `json:"runtime,omitempty"`
}

type SessionListPage struct {
	Items   []SessionSummary `json:"items"`
	HasMore bool             `json:"hasMore"`
	Next    Cursor           `json:"next,omitempty"`
}

type CloseSessionInput struct {
	Session SessionRef `json:"session"`
}

type SessionCloseDisposition string

const (
	SessionReleased       SessionCloseDisposition = "released"
	SessionRetainedActive SessionCloseDisposition = "retained_active"
	SessionAlreadyClosed  SessionCloseDisposition = "already_closed"
)

type CloseSessionResult struct {
	Disposition SessionCloseDisposition `json:"disposition"`
}

type RenameSessionInput struct {
	Session SessionRef `json:"session"`
	Title   string     `json:"title"`
}

type RenameSessionResult struct {
	Title string `json:"title"`
}

type ListTrashedSessionsInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	Cursor      Cursor      `json:"cursor,omitempty"`
	Limit       int         `json:"limit,omitempty"`
}

type TrashEntry struct {
	Session         SessionRef `json:"session"`
	TopicID         TopicID    `json:"topicId"`
	Title           string     `json:"title"`
	Preview         string     `json:"preview"`
	TrashedAtMillis int64      `json:"trashedAtMillis"`
	RecoveryCopy    bool       `json:"recoveryCopy"`
}

type TrashPage struct {
	Items   []TrashEntry `json:"items"`
	HasMore bool         `json:"hasMore"`
	Next    Cursor       `json:"next,omitempty"`
}

type TrashGuard string

const (
	TrashNormal                TrashGuard = "normal"
	TrashRedundantRecoveryOnly TrashGuard = "redundant_recovery_only"
)

type TrashSessionInput struct {
	Session SessionRef `json:"session"`
	Guard   TrashGuard `json:"guard"`
}

type TrashSessionResult struct {
	Disposition CleanupDisposition `json:"disposition"`
}

type RestoreSessionInput struct {
	Session SessionRef `json:"session"`
}

type SessionRestoreDisposition string

const SessionRestored SessionRestoreDisposition = "restored"

type RestoreSessionResult struct {
	Session     SessionRef                `json:"session"`
	TopicID     TopicID                   `json:"topicId"`
	Disposition SessionRestoreDisposition `json:"disposition"`
}

type PurgeSessionInput struct {
	Session SessionRef `json:"session"`
	Guard   TrashGuard `json:"guard"`
}

type PurgeSessionResult struct {
	Purged bool `json:"purged"`
}

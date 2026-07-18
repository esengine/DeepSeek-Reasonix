package runtimeapi

import "context"

type HostCapabilitiesAPI interface {
	HostCapabilities(context.Context) (Capabilities, error)
}

type HostConfigAPI interface {
	HostConfigSummary(context.Context) (HostConfigSummary, error)
}

type WorkspaceLifecycleAPI interface {
	ListWorkspaces(context.Context, ListWorkspacesInput) (WorkspaceListPage, error)
	CloseWorkspace(context.Context, CloseWorkspaceInput) (CloseWorkspaceResult, error)
}

type CatalogAPI interface {
	WorkspaceCatalog(context.Context, WorkspaceCatalogInput) (WorkspaceCatalog, error)
	SessionCatalog(context.Context, SessionCatalogInput) (SessionCatalog, error)
	CatalogEvents() <-chan CatalogInvalidation
}

type TopicAPI interface {
	ListTopics(context.Context, ListTopicsInput) (TopicPage, error)
	CreateTopic(context.Context, CreateTopicInput) (CreatedTopic, error)
	RenameTopic(context.Context, RenameTopicInput) (RenameTopicResult, error)
	DeleteTopic(context.Context, DeleteTopicInput) (DeleteTopicResult, error)
	TrashTopic(context.Context, TrashTopicInput) (TrashTopicResult, error)
}

type SessionRecordAPI interface {
	ListSessions(context.Context, ListSessionsInput) (SessionListPage, error)
	CloseSession(context.Context, CloseSessionInput) (CloseSessionResult, error)
	RenameSession(context.Context, RenameSessionInput) (RenameSessionResult, error)
	ListTrashedSessions(context.Context, ListTrashedSessionsInput) (TrashPage, error)
	TrashSession(context.Context, TrashSessionInput) (TrashSessionResult, error)
	RestoreSession(context.Context, RestoreSessionInput) (RestoreSessionResult, error)
	PurgeSession(context.Context, PurgeSessionInput) (PurgeSessionResult, error)
}

type SubscriptionAPI interface {
	UnsubscribeSession(context.Context, UnsubscribeSessionInput) error
}

type HistoryAPI interface {
	SessionHistory(context.Context, HistoryInput) (HistoryPage, error)
}

type ContentAPI interface {
	SessionContent(context.Context, ContentInput) (ContentChunk, error)
}

type ComposerQueryAPI interface {
	ComposerSlashArgs(context.Context, SlashArgsInput) (SlashArgsResult, error)
	ComposerHistory(context.Context, PromptHistoryInput) (PromptHistoryPage, error)
}

type SessionOperationAPI interface {
	NewSession(context.Context, SessionActionInput) (NewSessionResult, error)
	ClearSession(context.Context, SessionActionInput) (ClearSessionResult, error)
	ForkSession(context.Context, ForkSessionInput) (ForkSessionResult, error)
	RewindSession(context.Context, RewindSessionInput) (RewindSessionResult, error)
	CompactSession(context.Context, CompactSessionInput) (OperationStartedResult, error)
	SummarizeSession(context.Context, SummarizeSessionInput) (OperationStartedResult, error)
	CancelOperation(context.Context, CancelOperationInput) (CancelOperationResult, error)
}

type ProfileAPI interface {
	SetProfile(context.Context, SetProfileInput) (SetProfileResult, error)
}

type GoalAPI interface {
	SetGoal(context.Context, SetGoalInput) (SetGoalResult, error)
	ResumeGoal(context.Context, ResumeGoalInput) (ResumeGoalResult, error)
	ClearGoal(context.Context, ClearGoalInput) (ClearGoalResult, error)
}

type ShellAPI interface {
	RunShell(context.Context, RunShellInput) (OperationStartedResult, error)
}

type ContextAPI interface {
	SessionContext(context.Context, SessionContextInput) (ContextView, error)
}

type BalanceAPI interface {
	SessionBalance(context.Context, SessionBalanceInput) (BalanceView, error)
}

type JobsAPI interface {
	ListJobs(context.Context, ListJobsInput) (JobPage, error)
	CancelJob(context.Context, CancelJobInput) (CancelJobResult, error)
}

type MemoryAPI interface {
	Memory(context.Context, MemoryInput) (MemoryView, error)
	MemorySuggestions(context.Context, MemoryInput) (MemorySuggestionsView, error)
	RememberMemory(context.Context, RememberMemoryInput) (RememberMemoryResult, error)
	ForgetMemory(context.Context, ForgetMemoryInput) (ForgetMemoryResult, error)
	SaveMemoryDocument(context.Context, SaveMemoryDocumentInput) (SaveMemoryDocumentResult, error)
	AcceptMemorySuggestion(context.Context, AcceptMemorySuggestionInput) (AcceptMemorySuggestionResult, error)
	AcceptSkillSuggestion(context.Context, AcceptSkillSuggestionInput) (AcceptSkillSuggestionResult, error)
}

type ResearchAPI interface {
	ResearchStatus(context.Context, ResearchInput) (ResearchStatusView, error)
	ListResearch(context.Context, ListResearchInput) (ResearchPage, error)
	ResearchFindings(context.Context, ResearchFindingsInput) (ResearchFindingsPage, error)
	RecordResearchEvidence(context.Context, RecordResearchEvidenceInput) (RecordResearchEvidenceResult, error)
}

type FileQueryAPI interface {
	ListFiles(context.Context, FileListInput) (FileListResult, error)
	SearchFiles(context.Context, FileSearchInput) (FileSearchResult, error)
	PreviewFile(context.Context, FilePreviewInput) (FilePreview, error)
	WorkspaceChanges(context.Context, WorkspaceChangesInput) (WorkspaceChangesPage, error)
}

type GitQueryAPI interface {
	GitHistory(context.Context, GitHistoryInput) (GitHistoryResult, error)
	GitCommitDetail(context.Context, GitCommitDetailInput) (GitCommitDetail, error)
}

// V1RuntimeAPI is the complete frozen target-neutral Remote V1 domain surface.
// Request IDs, epochs, subscription IDs, sequence numbers and transport
// recovery stay inside adapters and never appear in this interface.
type V1RuntimeAPI interface {
	RuntimeAPI
	HostCapabilitiesAPI
	HostConfigAPI
	WorkspaceLifecycleAPI
	CatalogAPI
	TopicAPI
	SessionRecordAPI
	SubscriptionAPI
	HistoryAPI
	ContentAPI
	ComposerQueryAPI
	SessionOperationAPI
	ProfileAPI
	GoalAPI
	ShellAPI
	ContextAPI
	BalanceAPI
	JobsAPI
	MemoryAPI
	ResearchAPI
	FileQueryAPI
	GitQueryAPI
}

package protocol

type Direction string

const (
	// DirectionClientRequest is Desktop → Host request (workbench RuntimeAPI).
	DirectionClientRequest Direction = "client_to_host_request"
	// DirectionHostNotification is Host → Desktop notification (events).
	DirectionHostNotification Direction = "host_to_client_notification"
	// DirectionHostRequest is Host → Desktop request (Provider Broker).
	// Keys stay on Desktop; Host never holds credentials.
	DirectionHostRequest Direction = "host_to_client_request"
	// DirectionClientNotification is Desktop → Host notification (broker stream chunks).
	DirectionClientNotification Direction = "client_to_host_notification"
)

type OperationClass string

const (
	ClassConnection            OperationClass = "connection"
	ClassHostQuery             OperationClass = "host_query"
	ClassHostMutation          OperationClass = "host_mutation"
	ClassSessionQuery          OperationClass = "session_query"
	ClassSessionMutation       OperationClass = "session_mutation"
	ClassSessionRecordMutation OperationClass = "session_record_mutation"
	ClassHostNotification      OperationClass = "host_notification"
	ClassBrokerRequest         OperationClass = "broker_request"
	ClassBrokerNotification    OperationClass = "broker_notification"
)

type RemoteAction string

const (
	ActionNone          RemoteAction = "none"
	ActionRetry         RemoteAction = "retry"
	ActionReconnect     RemoteAction = "reconnect"
	ActionResubscribe   RemoteAction = "resubscribe"
	ActionRestartDaemon RemoteAction = "restart_daemon"
	ActionRunCommand    RemoteAction = "run_command"
)

type InvocationKind string

const (
	InvocationSkill    InvocationKind = "skill"
	InvocationSubagent InvocationKind = "subagent"
)

type SubmitKind string

const (
	SubmitTurn      SubmitKind = "turn"
	SubmitOperation SubmitKind = "operation"
	SubmitCompleted SubmitKind = "completed"
)

type OperationKind string

const (
	OperationShell     OperationKind = "shell"
	OperationCompact   OperationKind = "compact"
	OperationSummarize OperationKind = "summarize"
)

type SubmitEffect string

const (
	EffectNone            SubmitEffect = "none"
	EffectStateChanged    SubmitEffect = "state_changed"
	EffectRuntimeReplaced SubmitEffect = "runtime_replaced"
	EffectSessionReplaced SubmitEffect = "session_replaced"
)

type CancelStatus string

const (
	CancelRequested        CancelStatus = "cancel_requested"
	CancelAlreadyRequested CancelStatus = "already_requested"
)

type PromptDecision string

const (
	DecisionAllowOnce       PromptDecision = "allow_once"
	DecisionAllowSession    PromptDecision = "allow_session"
	DecisionAllowPersistent PromptDecision = "allow_persistent"
	DecisionDeny            PromptDecision = "deny"
)

type PromptKind string

const (
	PromptApproval PromptKind = "approval"
	PromptAsk      PromptKind = "ask"
)

type RewindScope string

const (
	RewindCode         RewindScope = "code"
	RewindConversation RewindScope = "conversation"
	RewindBoth         RewindScope = "both"
)

type SummaryDirection string

const (
	SummaryFrom SummaryDirection = "from"
	SummaryUpTo SummaryDirection = "up_to"
)

type GoalStatus string

const (
	GoalRunning  GoalStatus = "running"
	GoalComplete GoalStatus = "complete"
	GoalBlocked  GoalStatus = "blocked"
	GoalStopped  GoalStatus = "stopped"
)

type CollaborationMode string

const (
	CollaborationNormal CollaborationMode = "normal"
	CollaborationPlan   CollaborationMode = "plan"
	CollaborationGoal   CollaborationMode = "goal"
)

type TokenMode string

const (
	TokenFull     TokenMode = "full"
	TokenEconomy  TokenMode = "economy"
	TokenDelivery TokenMode = "delivery"
)

type ToolApprovalMode string

const (
	ToolApprovalAsk  ToolApprovalMode = "ask"
	ToolApprovalAuto ToolApprovalMode = "auto"
	ToolApprovalYOLO ToolApprovalMode = "yolo"
)

type TopicSelectionKind string

const (
	TopicExisting TopicSelectionKind = "existing"
	TopicNew      TopicSelectionKind = "new"
)

type TrashGuard string

const (
	TrashNormal                TrashGuard = "normal"
	TrashRedundantRecoveryOnly TrashGuard = "redundant_recovery_only"
)

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

type ResyncReason string

const (
	ResyncQueueOverflow   ResyncReason = "queue_overflow"
	ResyncRuntimeReplaced ResyncReason = "runtime_replaced"
	ResyncTargetReplaced  ResyncReason = "target_replaced"
	ResyncStateChanged    ResyncReason = "state_changed"
)

type SessionOutcome string

const (
	OutcomeCompleted   SessionOutcome = "completed"
	OutcomeCancelled   SessionOutcome = "cancelled"
	OutcomeFailed      SessionOutcome = "failed"
	OutcomeInterrupted SessionOutcome = "interrupted"
)

type InterruptionReason string

const (
	InterruptionHostRestarted InterruptionReason = "host_restarted"
)

type FileKind string

const (
	FileText   FileKind = "text"
	FileBinary FileKind = "binary"
	FileImage  FileKind = "image"
	FilePDF    FileKind = "pdf"
)

type SearchTruncationReason string

const (
	SearchResultLimit SearchTruncationReason = "result_limit"
	SearchScanLimit   SearchTruncationReason = "scan_limit"
)

type ByteTruncationReason string

const ByteLimit ByteTruncationReason = "byte_limit"

type GitHistoryTruncationReason string

const GitHistoryLimit GitHistoryTruncationReason = "history_limit"

type GitCommitDetailKind string

const (
	GitDetailFiles GitCommitDetailKind = "files"
	GitDetailPatch GitCommitDetailKind = "patch"
)

type ChangeSource string

const (
	ChangeSession ChangeSource = "session"
	ChangeGit     ChangeSource = "git"
)

type JobKind string

const (
	JobBash JobKind = "bash"
	JobTask JobKind = "task"
)

type JobStatus string

const JobRunning JobStatus = "running"

type JobCancelDisposition string

const (
	JobCancelled  JobCancelDisposition = "cancelled"
	JobNotRunning JobCancelDisposition = "not_running"
)

type ContentEncoding string

const ContentUTF8 ContentEncoding = "utf-8"

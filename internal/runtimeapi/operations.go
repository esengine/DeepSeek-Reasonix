package runtimeapi

type InvocationKind string

const (
	InvocationSkill    InvocationKind = "skill"
	InvocationSubagent InvocationKind = "subagent"
)

type Invocation struct {
	Name string         `json:"name"`
	Kind InvocationKind `json:"kind"`
}

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

type TurnSteerResult struct {
	Accepted bool   `json:"accepted"`
	TurnID   TurnID `json:"turnId"`
}

type TurnCancelResult struct {
	Status CancelStatus `json:"status"`
	TurnID TurnID       `json:"turnId"`
}

type PromptResolvedResult struct {
	Resolved bool     `json:"resolved"`
	PromptID PromptID `json:"promptId"`
}

type SessionActionInput struct {
	Session SessionRef `json:"session"`
}

type NewSessionResult struct {
	Source           SessionRef `json:"source"`
	Session          SessionRef `json:"session"`
	Disposition      string     `json:"disposition"`
	SnapshotRequired bool       `json:"snapshotRequired"`
}

type SessionClearDisposition string

const (
	SessionCleared        SessionClearDisposition = "cleared"
	SessionCleanupPending SessionClearDisposition = "cleanup_pending"
)

type ClearSessionResult struct {
	Previous         SessionRef              `json:"previous"`
	Session          SessionRef              `json:"session"`
	Disposition      SessionClearDisposition `json:"disposition"`
	SnapshotRequired bool                    `json:"snapshotRequired"`
}

type ForkSessionInput struct {
	Session      SessionRef   `json:"session"`
	CheckpointID CheckpointID `json:"checkpointId"`
	Name         string       `json:"name,omitempty"`
}

type ForkSessionResult struct {
	Source SessionRef `json:"source"`
	Child  SessionRef `json:"child"`
}

type RewindScope string

const (
	RewindCode         RewindScope = "code"
	RewindConversation RewindScope = "conversation"
	RewindBoth         RewindScope = "both"
)

type RewindSessionInput struct {
	Session      SessionRef   `json:"session"`
	CheckpointID CheckpointID `json:"checkpointId"`
	Scope        RewindScope  `json:"scope"`
}

type RewindSessionResult struct {
	WorkspaceChanged      bool `json:"workspaceChanged"`
	ConversationRewritten bool `json:"conversationRewritten"`
	SnapshotRequired      bool `json:"snapshotRequired"`
}

type CompactSessionInput struct {
	Session      SessionRef `json:"session"`
	Instructions string     `json:"instructions,omitempty"`
}

type SummaryDirection string

const (
	SummaryFrom SummaryDirection = "from"
	SummaryUpTo SummaryDirection = "up_to"
)

type SummarizeSessionInput struct {
	Session      SessionRef       `json:"session"`
	CheckpointID CheckpointID     `json:"checkpointId"`
	Direction    SummaryDirection `json:"direction"`
}

type OperationStartedResult struct {
	OperationID OperationID `json:"operationId"`
	Disposition string      `json:"disposition"`
}

type CancelOperationInput struct {
	Session     SessionRef  `json:"session"`
	OperationID OperationID `json:"operationId"`
}

type CancelOperationResult struct {
	Status      CancelStatus `json:"status"`
	OperationID OperationID  `json:"operationId"`
}

type ProfilePatch struct {
	Model             *string `json:"model,omitempty"`
	Effort            *string `json:"effort,omitempty"`
	CollaborationMode *string `json:"collaborationMode,omitempty"`
	TokenMode         *string `json:"tokenMode,omitempty"`
	ToolApprovalMode  *string `json:"toolApprovalMode,omitempty"`
}

type SetProfileInput struct {
	Session SessionRef   `json:"session"`
	Patch   ProfilePatch `json:"patch"`
}

type ProfileSetDisposition string

const (
	ProfileUpdated ProfileSetDisposition = "updated"
	ProfileRebuilt ProfileSetDisposition = "rebuilt"
)

type SetProfileResult struct {
	ResolvedProfile       ResolvedProfile       `json:"resolvedProfile"`
	Disposition           ProfileSetDisposition `json:"disposition"`
	AutoResolvedPromptIDs []PromptID            `json:"autoResolvedPromptIds"`
	SnapshotRequired      bool                  `json:"snapshotRequired,omitempty"`
}

type SetGoalInput struct {
	Session SessionRef `json:"session"`
	Goal    string     `json:"goal"`
}

type SetGoalResult struct {
	Goal   string     `json:"goal"`
	Status GoalStatus `json:"status"`
}

type ResumeGoalInput struct {
	Session SessionRef `json:"session"`
}

type ResumeGoalResult struct {
	Resumed bool       `json:"resumed"`
	Goal    string     `json:"goal"`
	Status  GoalStatus `json:"status"`
}

type ClearGoalInput struct {
	Session SessionRef `json:"session"`
}

type ClearGoalResult struct {
	Cleared bool `json:"cleared"`
}

type RunShellInput struct {
	Session SessionRef `json:"session"`
	Command string     `json:"command"`
}

type SessionContextInput struct {
	Session SessionRef `json:"session"`
}

type SessionBalanceInput struct {
	Session SessionRef `json:"session"`
}

type BalanceView struct {
	Available bool   `json:"available"`
	Display   string `json:"display"`
}

type ListJobsInput struct {
	Session SessionRef `json:"session"`
	Cursor  Cursor     `json:"cursor,omitempty"`
	Limit   int        `json:"limit,omitempty"`
}

type JobPage struct {
	Jobs    []Job  `json:"jobs"`
	HasMore bool   `json:"hasMore"`
	Next    Cursor `json:"next,omitempty"`
}

type JobCancelDisposition string

const (
	JobCancelled  JobCancelDisposition = "cancelled"
	JobNotRunning JobCancelDisposition = "not_running"
)

type CancelJobInput struct {
	Session SessionRef `json:"session"`
	JobID   JobID      `json:"jobId"`
}

type CancelJobResult struct {
	Disposition JobCancelDisposition `json:"disposition"`
}

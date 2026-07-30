package protocol

import "reasonix/internal/eventwire"

type ExternalizedField struct {
	JSONPointer      string     `json:"jsonPointer" validate:"jsonPointer"`
	ContentRef       ContentRef `json:"contentRef"`
	TotalBytes       int64      `json:"totalBytes" validate:"min=0"`
	SHA256           string     `json:"sha256" validate:"sha256"`
	Truncated        bool       `json:"truncated"`
	OriginalBytes    *int64     `json:"originalBytes,omitempty" validate:"min=0"`
	TruncationReason string     `json:"truncationReason,omitempty"`
}

type HistoryToolCall struct {
	ID                string  `json:"id" validate:"nonempty"`
	Name              string  `json:"name" validate:"nonempty"`
	Arguments         *string `json:"arguments" externalizable:"true"`
	ResolvedName      string  `json:"resolvedName,omitempty"`
	CapabilityID      string  `json:"capabilityId,omitempty"`
	ResolvedReadOnly  *bool   `json:"resolvedReadOnly,omitempty"`
	Subject           string  `json:"subject,omitempty"`
	Summary           *string `json:"summary,omitempty" externalizable:"true"`
	Diff              *string `json:"diff,omitempty" externalizable:"true"`
	Added             int     `json:"added,omitempty" validate:"min=0"`
	Removed           int     `json:"removed,omitempty" validate:"min=0"`
	ArgumentsArchived bool    `json:"argumentsArchived,omitempty"`
}

type HistoryMessage struct {
	Role               string                     `json:"role" validate:"nonempty"`
	Content            *string                    `json:"content" externalizable:"true"`
	Detail             *string                    `json:"detail,omitempty" externalizable:"true"`
	Code               string                     `json:"code,omitempty"`
	SubmitText         *string                    `json:"submitText,omitempty" externalizable:"true"`
	CheckpointID       CheckpointID               `json:"checkpointId,omitempty"`
	CreatedAtMs        int64                      `json:"createdAtMs,omitempty" validate:"min=0"`
	Reasoning          *string                    `json:"reasoning,omitempty" externalizable:"true"`
	WorkDurationMs     int64                      `json:"workDurationMs,omitempty" validate:"min=0"`
	MemoryCitations    []eventwire.MemoryCitation `json:"memoryCitations,omitempty"`
	Level              string                     `json:"level,omitempty"`
	ToolCalls          []HistoryToolCall          `json:"toolCalls,omitempty"`
	ToolCallID         string                     `json:"toolCallId,omitempty"`
	ToolName           string                     `json:"toolName,omitempty"`
	ToolResultArchived bool                       `json:"toolResultArchived,omitempty"`
	ToolResultError    *string                    `json:"toolResultError,omitempty" externalizable:"true"`
	Pending            bool                       `json:"pending,omitempty"`
	Trigger            string                     `json:"trigger,omitempty"`
	Messages           int                        `json:"messages,omitempty" validate:"min=0"`
	Summary            *string                    `json:"summary,omitempty" externalizable:"true"`
	Archive            *string                    `json:"archive,omitempty" externalizable:"true"`
}

type HistoryPage struct {
	SnapshotID   SnapshotID          `json:"snapshotId"`
	Messages     []HistoryMessage    `json:"messages"`
	StartTurn    int                 `json:"startTurn" validate:"min=0"`
	EndTurn      int                 `json:"endTurn" validate:"min=0"`
	TotalTurns   int                 `json:"totalTurns" validate:"min=0"`
	ActualTurns  int                 `json:"actualTurns" validate:"min=0,max=200"`
	HasOlder     bool                `json:"hasOlder"`
	NextCursor   Cursor              `json:"nextCursor,omitempty"`
	Externalized []ExternalizedField `json:"externalized"`
}

type SessionSubscribeParams struct {
	ExpectedHostEpoch     HostEpoch      `json:"expectedHostEpoch"`
	Target                RuntimeTarget  `json:"target"`
	PageTurns             int            `json:"pageTurns" validate:"min=1,max=200"`
	ReplaceSubscriptionID SubscriptionID `json:"replaceSubscriptionId,omitempty"`
}

type SessionMetaSnapshot struct {
	TopicID         TopicID         `json:"topicId"`
	Title           string          `json:"title" validate:"nonempty"`
	ResolvedProfile ResolvedProfile `json:"resolvedProfile"`
	Goal            *string         `json:"goal" externalizable:"true"`
	GoalStatus      GoalStatus      `json:"goalStatus,omitempty"`
	Capabilities    Capabilities    `json:"capabilities"`
}

type TurnState struct {
	TurnID          TurnID `json:"turnId"`
	CancelRequested bool   `json:"cancelRequested"`
}

type OperationState struct {
	OperationID     OperationID   `json:"operationId"`
	Kind            OperationKind `json:"kind"`
	CancelRequested bool          `json:"cancelRequested"`
}

type RuntimeInterruption struct {
	PreviousTurnInterrupted bool               `json:"previousTurnInterrupted"`
	Reason                  InterruptionReason `json:"reason"`
}

type SessionRuntimeState struct {
	Running          bool                 `json:"running"`
	CurrentTurn      *TurnState           `json:"currentTurn,omitempty"`
	CurrentOperation *OperationState      `json:"currentOperation,omitempty"`
	CancelRequested  bool                 `json:"cancelRequested"`
	LastOutcome      SessionOutcome       `json:"lastOutcome,omitempty"`
	LastError        *string              `json:"lastError,omitempty" externalizable:"true"`
	Interruption     *RuntimeInterruption `json:"interruption,omitempty"`
	LiveEvents       []eventwire.Event    `json:"liveEvents"`
}

type ApprovalPrompt struct {
	PromptID         PromptID         `json:"promptId"`
	Tool             string           `json:"tool" validate:"nonempty"`
	Subject          string           `json:"subject" validate:"nonempty" externalizable:"true"`
	Reason           *string          `json:"reason" externalizable:"true"`
	Fresh            bool             `json:"fresh"`
	AllowedDecisions []PromptDecision `json:"allowedDecisions"`
}

type AskOption struct {
	Label       string  `json:"label" validate:"nonempty"`
	Description *string `json:"description" externalizable:"true"`
}

type AskQuestion struct {
	QuestionID QuestionID  `json:"questionId"`
	Header     string      `json:"header,omitempty"`
	Prompt     *string     `json:"prompt" externalizable:"true"`
	Options    []AskOption `json:"options"`
	Multi      bool        `json:"multi"`
}

type AskPrompt struct {
	PromptID  PromptID      `json:"promptId"`
	Questions []AskQuestion `json:"questions"`
}

type PendingPrompt struct {
	Kind     PromptKind      `json:"kind"`
	Approval *ApprovalPrompt `json:"approval,omitempty"`
	Ask      *AskPrompt      `json:"ask,omitempty"`
}

func (p PendingPrompt) Validate() error {
	if p.Kind == PromptApproval && (p.Approval == nil || p.Ask != nil) {
		return validationError("approval prompt requires only approval payload")
	}
	if p.Kind == PromptAsk && (p.Ask == nil || p.Approval != nil) {
		return validationError("ask prompt requires only ask payload")
	}
	return nil
}

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

type TodoItem struct {
	Content    *string    `json:"content" externalizable:"true"`
	Status     TodoStatus `json:"status"`
	ActiveForm string     `json:"activeForm,omitempty"`
	Level      int        `json:"level,omitempty" validate:"min=0"`
}

type UsageSourceView struct {
	Source           string  `json:"source" validate:"nonempty"`
	PromptTokens     int     `json:"promptTokens" validate:"min=0"`
	CompletionTokens int     `json:"completionTokens" validate:"min=0"`
	TotalTokens      int     `json:"totalTokens" validate:"min=0"`
	ReasoningTokens  int     `json:"reasoningTokens" validate:"min=0"`
	CacheHitTokens   int     `json:"cacheHitTokens" validate:"min=0"`
	CacheMissTokens  int     `json:"cacheMissTokens" validate:"min=0"`
	RequestCount     int     `json:"requestCount" validate:"min=0"`
	SessionCost      float64 `json:"sessionCost,omitempty" validate:"min=0"`
	SessionCurrency  string  `json:"sessionCurrency,omitempty"`
}

type ReadFileRecord struct {
	Path      string `json:"path" validate:"relativePath"`
	Turn      int    `json:"turn" validate:"min=0"`
	TimeMs    int64  `json:"timeMs" validate:"min=0"`
	Offset    *int64 `json:"offset,omitempty" validate:"min=0"`
	Limit     *int64 `json:"limit,omitempty" validate:"min=0"`
	Truncated bool   `json:"truncated"`
}

type ContextView struct {
	UsedTokens              int               `json:"usedTokens" validate:"min=0"`
	WindowTokens            int               `json:"windowTokens" validate:"min=0"`
	PromptTokens            int               `json:"promptTokens" validate:"min=0"`
	CompletionTokens        int               `json:"completionTokens" validate:"min=0"`
	TotalTokens             int               `json:"totalTokens" validate:"min=0"`
	ReasoningTokens         int               `json:"reasoningTokens" validate:"min=0"`
	CacheHitTokens          int               `json:"cacheHitTokens" validate:"min=0"`
	CacheMissTokens         int               `json:"cacheMissTokens" validate:"min=0"`
	SessionCacheHitTokens   int               `json:"sessionCacheHitTokens" validate:"min=0"`
	SessionCacheMissTokens  int               `json:"sessionCacheMissTokens" validate:"min=0"`
	SessionCompletionTokens int               `json:"sessionCompletionTokens" validate:"min=0"`
	RequestCount            int               `json:"requestCount" validate:"min=0"`
	ElapsedMs               int64             `json:"elapsedMs" validate:"min=0"`
	SessionCost             float64           `json:"sessionCost,omitempty" validate:"min=0"`
	SessionCurrency         string            `json:"sessionCurrency,omitempty"`
	Sources                 []UsageSourceView `json:"sources"`
	ReadFiles               []ReadFileRecord  `json:"readFiles"`
}

type CheckpointView struct {
	CheckpointID    CheckpointID `json:"checkpointId"`
	DisplayTurn     int          `json:"displayTurn" validate:"min=0"`
	Prompt          *string      `json:"prompt" externalizable:"true"`
	Files           []string     `json:"files"`
	FileCount       int          `json:"fileCount" validate:"min=0"`
	FilesTruncated  bool         `json:"filesTruncated"`
	CreatedAtMs     int64        `json:"createdAtMs" validate:"min=0"`
	CanCode         bool         `json:"canCode"`
	CanConversation bool         `json:"canConversation"`
}

type JobView struct {
	ID        JobID     `json:"id"`
	Kind      JobKind   `json:"kind"`
	Label     string    `json:"label" validate:"nonempty"`
	Status    JobStatus `json:"status"`
	StartedAt int64     `json:"startedAt" validate:"min=0"`
}

// SessionMirrorSnapshot reserves schema-marked placeholders for Desktop's
// read-only recovery mirror. Concrete values are always replaced by null on
// the wire and transferred through the matching Externalized contentRef.
type SessionMirrorSnapshot struct {
	SessionJSONL *string `json:"session.jsonl" externalizable:"true"`
}

type SessionSnapshot struct {
	SnapshotID    SnapshotID            `json:"snapshotId"`
	HostEpoch     HostEpoch             `json:"hostEpoch"`
	Target        RuntimeTarget         `json:"target"`
	RuntimeEpoch  RuntimeEpoch          `json:"runtimeEpoch"`
	BoundarySeq   uint64                `json:"boundarySeq"`
	Meta          SessionMetaSnapshot   `json:"meta"`
	Runtime       SessionRuntimeState   `json:"runtime"`
	History       HistoryPage           `json:"history"`
	PendingPrompt *PendingPrompt        `json:"pendingPrompt" nullable:"true"`
	Todos         []TodoItem            `json:"todos"`
	Context       ContextView           `json:"context"`
	Jobs          []JobView             `json:"jobs"`
	Checkpoints   []CheckpointView      `json:"checkpoints"`
	Mirror        SessionMirrorSnapshot `json:"mirror"`
	Externalized  []ExternalizedField   `json:"externalized"`
}

type SessionSubscribeResult struct {
	SubscriptionID SubscriptionID  `json:"subscriptionId"`
	Snapshot       SessionSnapshot `json:"snapshot"`
}

type SessionUnsubscribeParams struct {
	SubscriptionID SubscriptionID `json:"subscriptionId"`
}

type SessionUnsubscribeResult struct {
	Unsubscribed bool `json:"unsubscribed" validate:"true"`
}

type SessionHistoryParams struct {
	RuntimeQuery
	SnapshotID SnapshotID `json:"snapshotId"`
	Cursor     Cursor     `json:"cursor,omitempty"`
	PageTurns  int        `json:"pageTurns" validate:"min=1,max=200"`
}

type SessionContentParams struct {
	ContentRef ContentRef `json:"contentRef"`
	Offset     int64      `json:"offset" validate:"min=0"`
}

type SessionContentResult struct {
	ContentRef ContentRef      `json:"contentRef"`
	Offset     int64           `json:"offset" validate:"min=0"`
	DataBase64 string          `json:"dataBase64"`
	NextOffset *int64          `json:"nextOffset,omitempty" validate:"min=0"`
	TotalBytes int64           `json:"totalBytes" validate:"min=0,max=8388608"`
	SHA256     string          `json:"sha256" validate:"sha256"`
	Encoding   ContentEncoding `json:"encoding"`
}

type SessionEvent struct {
	SubscriptionID SubscriptionID      `json:"subscriptionId"`
	HostEpoch      HostEpoch           `json:"hostEpoch"`
	Target         RuntimeTarget       `json:"target"`
	RuntimeEpoch   RuntimeEpoch        `json:"runtimeEpoch"`
	Seq            uint64              `json:"seq"`
	TurnID         TurnID              `json:"turnId,omitempty"`
	OperationID    OperationID         `json:"operationId,omitempty"`
	Event          eventwire.Event     `json:"event"`
	Externalized   []ExternalizedField `json:"externalized"`
}

type SessionResyncRequired struct {
	SubscriptionID          SubscriptionID `json:"subscriptionId"`
	HostEpoch               HostEpoch      `json:"hostEpoch"`
	Target                  RuntimeTarget  `json:"target"`
	RuntimeEpoch            RuntimeEpoch   `json:"runtimeEpoch"`
	LastSeq                 uint64         `json:"lastSeq"`
	Reason                  ResyncReason   `json:"reason"`
	ReplacementTarget       *RuntimeTarget `json:"replacementTarget,omitempty"`
	ReplacementRuntimeEpoch RuntimeEpoch   `json:"replacementRuntimeEpoch,omitempty"`
}

type CatalogChanged struct {
	HostEpoch            HostEpoch       `json:"hostEpoch"`
	Revision             CatalogRevision `json:"revision"`
	Scope                CatalogScope    `json:"scope"`
	AffectedWorkspaceIDs []WorkspaceID   `json:"affectedWorkspaceIds,omitempty"`
	Kinds                []CatalogKind   `json:"kinds"`
}

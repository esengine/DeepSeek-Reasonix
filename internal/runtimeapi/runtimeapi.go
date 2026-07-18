// Package runtimeapi defines the target-neutral contract consumed by the
// Desktop workbench. Implementations may execute in-process or over Reasonix
// Remote, but transport identity and recovery mechanics stay behind this API.
package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/eventwire"
)

// Opaque identities have no path or transport meaning. Callers may compare and
// persist them, but must not parse information out of their value.
type (
	WorkspaceID     string
	SessionID       string
	TopicID         string
	DirectoryRef    string
	Cursor          string
	TurnID          string
	OperationID     string
	PromptID        string
	QuestionID      string
	CheckpointID    string
	JobID           string
	CatalogRevision string
	ModelRef        string
	MemoryID        string
	DocumentID      string
	SuggestionID    string
	SkillID         string
	ResearchTaskID  string
	CriterionID     string
	ContentRef      string
)

// Shared RuntimeService limits. Local and Remote adapters use these values;
// they are domain constraints rather than wire framing limits.
const (
	HistoryMinTurns           = 1
	HistoryMaxTurns           = 200
	DefaultAttachHistoryTurns = 60
	PageDefaultItems          = 200
	PageMaxItems              = 1000
	SearchDefaultItems        = 20
	SearchMaxItems            = 100
	SearchMaxVisitedItems     = 10000
	PreviewBytes              = 256 << 10
	GitHistoryCommits         = 100
	GitPatchBytes             = 1 << 20
)

// SessionRef identifies one Session within its workspace without exposing the
// Session's on-disk representation.
type SessionRef struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	SessionID   SessionID   `json:"sessionId"`
}

func (r SessionRef) Valid() bool {
	return strings.TrimSpace(string(r.WorkspaceID)) != "" && strings.TrimSpace(string(r.SessionID)) != ""
}

// Capability names are stable RuntimeAPI domains, not wire method names.
type Capability string

const (
	CapabilityWorkspaceBrowse Capability = "workspace-browse"
	CapabilityHostConfig      Capability = "host-config-summary"
	CapabilitySessionCreate   Capability = "session-create"
	CapabilitySessionAttach   Capability = "session-attach"
	CapabilityComposerSubmit  Capability = "composer-submit"
	CapabilityTurnSteer       Capability = "turn-steer"
	CapabilityTurnCancel      Capability = "turn-cancel"
	CapabilityPromptApprove   Capability = "prompt-approve"
	CapabilityPromptAnswer    Capability = "prompt-answer"
)

// Capabilities describes only behavior that can actually be invoked through
// this adapter. An adapter must not advertise a capability backed by a stub.
type Capabilities struct {
	HostConfig      bool     `json:"hostConfig"`
	WorkspaceBrowse bool     `json:"workspaceBrowse"`
	SessionCreate   bool     `json:"sessionCreate"`
	SessionAttach   bool     `json:"sessionAttach"`
	ComposerSubmit  bool     `json:"composerSubmit"`
	TurnSteer       bool     `json:"turnSteer"`
	TurnCancel      bool     `json:"turnCancel"`
	PromptApprove   bool     `json:"promptApprove"`
	PromptAnswer    bool     `json:"promptAnswer"`
	Features        Features `json:"features"`
	Limits          Limits   `json:"limits"`
}

// Features and Limits retain the full frozen Host capability view while the
// method-level fields above state what this particular adapter has wired.
type Features struct {
	CoreSession         bool `json:"coreSession"`
	PrimaryFileQueries  bool `json:"primaryFileQueries"`
	UserShell           bool `json:"userShell"`
	JobCancel           bool `json:"jobCancel"`
	Memory              bool `json:"memory"`
	Research            bool `json:"research"`
	MediaPreview        bool `json:"mediaPreview"`
	Attachments         bool `json:"attachments"`
	ClipboardImages     bool `json:"clipboardImages"`
	SFTP                bool `json:"sftp"`
	LocalPathOperations bool `json:"localPathOperations"`
	GitWrite            bool `json:"gitWrite"`
	PTY                 bool `json:"pty"`
	DeliveryWorktree    bool `json:"deliveryWorktree"`
}

type Limits struct {
	FrameBytes             int `json:"frameBytes"`
	SnapshotHistoryBytes   int `json:"snapshotHistoryBytes"`
	ExternalizeFieldBytes  int `json:"externalizeFieldBytes"`
	ContentRefChunkBytes   int `json:"contentRefChunkBytes"`
	ContentRefObjectBytes  int `json:"contentRefObjectBytes"`
	ContentRefIdleMillis   int `json:"contentRefIdleMillis"`
	ContentRefMaxAgeMillis int `json:"contentRefMaxAgeMillis"`
	HistoryMaxTurns        int `json:"historyMaxTurns"`
	PageDefaultItems       int `json:"pageDefaultItems"`
	PageMaxItems           int `json:"pageMaxItems"`
	SearchDefaultItems     int `json:"searchDefaultItems"`
	SearchMaxItems         int `json:"searchMaxItems"`
	SearchMaxVisitedItems  int `json:"searchMaxVisitedItems"`
	PreviewBytes           int `json:"previewBytes"`
	GitHistoryCommits      int `json:"gitHistoryCommits"`
	GitPatchBytes          int `json:"gitPatchBytes"`
}

func (c Capabilities) Supports(capability Capability) bool {
	switch capability {
	case CapabilityHostConfig:
		return c.HostConfig
	case CapabilityWorkspaceBrowse:
		return c.WorkspaceBrowse
	case CapabilitySessionCreate:
		return c.SessionCreate
	case CapabilitySessionAttach:
		return c.SessionAttach
	case CapabilityComposerSubmit:
		return c.ComposerSubmit
	case CapabilityTurnSteer:
		return c.TurnSteer
	case CapabilityTurnCancel:
		return c.TurnCancel
	case CapabilityPromptApprove:
		return c.PromptApprove
	case CapabilityPromptAnswer:
		return c.PromptAnswer
	default:
		return false
	}
}

// ErrUnavailable is returned when the selected target does not implement a
// RuntimeAPI capability. It is intentionally distinct from a successful empty
// result so unfinished work cannot masquerade as a working feature.
var ErrUnavailable = errors.New("runtime capability unavailable")

type UnavailableError struct {
	Capability Capability
	Detail     string
}

func (e *UnavailableError) Error() string {
	if e == nil {
		return ErrUnavailable.Error()
	}
	if detail := strings.TrimSpace(e.Detail); detail != "" {
		return fmt.Sprintf("%s: %s", e.Capability, detail)
	}
	return fmt.Sprintf("%s: %s", e.Capability, ErrUnavailable)
}

func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

func Unavailable(capability Capability, detail string) error {
	return &UnavailableError{Capability: capability, Detail: detail}
}

// ConnectionView is the target-neutral identity displayed by the workbench.
// It deliberately omits SSH, process and protocol state.
type ConnectionView struct {
	Label        string            `json:"label"`
	OS           string            `json:"os,omitempty"`
	Arch         string            `json:"arch,omitempty"`
	ShellKind    string            `json:"shellKind,omitempty"`
	Capabilities Capabilities      `json:"capabilities"`
	Config       HostConfigSummary `json:"config"`
}

// HostConfigSummary contains safe, resolved presentation data. Provider keys,
// MCP credentials and local Desktop configuration never belong here.
type HostConfigSummary struct {
	Available          bool                `json:"available"`
	UnavailableReason  string              `json:"unavailableReason,omitempty"`
	DefaultModel       string              `json:"defaultModel,omitempty"`
	Models             []string            `json:"models"`
	CollaborationModes []string            `json:"collaborationModes"`
	TokenModes         []string            `json:"tokenModes"`
	ToolApprovalModes  []string            `json:"toolApprovalModes"`
	Revision           CatalogRevision     `json:"revision,omitempty"`
	EffectiveScopes    []EffectiveScope    `json:"effectiveScopes,omitempty"`
	DisplayPaths       []ConfigDisplayPath `json:"displayPaths,omitempty"`
	FeatureStates      []FeatureState      `json:"featureStates,omitempty"`
	CLIHints           []CLIHint           `json:"cliHints,omitempty"`
}

func (s HostConfigSummary) RequireAvailable() error {
	if s.Available {
		return nil
	}
	reason := strings.TrimSpace(s.UnavailableReason)
	if reason == "" {
		reason = "the selected target did not provide a Host configuration summary"
	}
	return Unavailable(CapabilityHostConfig, reason)
}

type Workspace struct {
	ID          WorkspaceID `json:"id"`
	Name        string      `json:"name"`
	DisplayPath string      `json:"displayPath"`
}

type Directory struct {
	Ref         DirectoryRef `json:"ref"`
	Name        string       `json:"name"`
	DisplayPath string       `json:"displayPath"`
	ParentRef   DirectoryRef `json:"parentRef,omitempty"`
}

type BrowseWorkspaceInput struct {
	DirectoryRef DirectoryRef `json:"directoryRef,omitempty"`
	TypedPath    string       `json:"typedPath,omitempty"`
	Cursor       Cursor       `json:"cursor,omitempty"`
	Limit        int          `json:"limit,omitempty"`
}

type WorkspacePage struct {
	Directory Directory   `json:"directory"`
	Entries   []Directory `json:"entries"`
	HasMore   bool        `json:"hasMore"`
	Next      Cursor      `json:"next,omitempty"`
}

type OpenWorkspaceInput struct {
	PrimaryDirectory DirectoryRef `json:"primaryDirectory"`
}

type OpenWorkspaceResult struct {
	Workspace   Workspace `json:"workspace"`
	AlreadyOpen bool      `json:"alreadyOpen"`
}

type TopicSelectionKind string

const (
	TopicExisting TopicSelectionKind = "existing"
	TopicNew      TopicSelectionKind = "new"
)

type TopicSelection struct {
	Kind    TopicSelectionKind `json:"kind"`
	TopicID TopicID            `json:"topicId,omitempty"`
	Title   string             `json:"title,omitempty"`
}

type ProfileSelection struct {
	Model             string `json:"model,omitempty"`
	Effort            string `json:"effort,omitempty"`
	CollaborationMode string `json:"collaborationMode,omitempty"`
	TokenMode         string `json:"tokenMode,omitempty"`
	ToolApprovalMode  string `json:"toolApprovalMode,omitempty"`
}

type ResolvedProfile struct {
	Model             string `json:"model"`
	Effort            string `json:"effort"`
	CollaborationMode string `json:"collaborationMode"`
	TokenMode         string `json:"tokenMode"`
	ToolApprovalMode  string `json:"toolApprovalMode"`
}

type CreateSessionInput struct {
	WorkspaceID           WorkspaceID      `json:"workspaceId"`
	AdditionalDirectories []DirectoryRef   `json:"additionalDirectories"`
	Topic                 TopicSelection   `json:"topic"`
	Profile               ProfileSelection `json:"profile"`
}

type CreatedSession struct {
	Session         SessionRef      `json:"session"`
	TopicID         TopicID         `json:"topicId"`
	TopicTitle      string          `json:"topicTitle"`
	ResolvedProfile ResolvedProfile `json:"resolvedProfile"`
}

type HistoryToolCall struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Arguments         *string `json:"arguments,omitempty"`
	Subject           string  `json:"subject,omitempty"`
	Summary           *string `json:"summary,omitempty"`
	Diff              *string `json:"diff,omitempty"`
	Added             int     `json:"added,omitempty"`
	Removed           int     `json:"removed,omitempty"`
	ArgumentsArchived bool    `json:"argumentsArchived,omitempty"`
}

type HistoryMessage struct {
	Role               string                     `json:"role"`
	Content            *string                    `json:"content,omitempty"`
	Detail             *string                    `json:"detail,omitempty"`
	Code               string                     `json:"code,omitempty"`
	SubmitText         *string                    `json:"submitText,omitempty"`
	CheckpointID       CheckpointID               `json:"checkpointId,omitempty"`
	CreatedAtMillis    int64                      `json:"createdAtMillis,omitempty"`
	Reasoning          *string                    `json:"reasoning,omitempty"`
	WorkDurationMillis int64                      `json:"workDurationMillis,omitempty"`
	MemoryCitations    []eventwire.MemoryCitation `json:"memoryCitations,omitempty"`
	Level              string                     `json:"level,omitempty"`
	ToolCalls          []HistoryToolCall          `json:"toolCalls,omitempty"`
	ToolCallID         string                     `json:"toolCallId,omitempty"`
	ToolName           string                     `json:"toolName,omitempty"`
	ToolResultArchived bool                       `json:"toolResultArchived,omitempty"`
	ToolResultError    *string                    `json:"toolResultError,omitempty"`
	Pending            bool                       `json:"pending,omitempty"`
	Trigger            string                     `json:"trigger,omitempty"`
	Messages           int                        `json:"messages,omitempty"`
	Summary            *string                    `json:"summary,omitempty"`
	Archive            *string                    `json:"archive,omitempty"`
}

type HistoryPage struct {
	Messages    []HistoryMessage `json:"messages"`
	StartTurn   int              `json:"startTurn"`
	EndTurn     int              `json:"endTurn"`
	TotalTurns  int              `json:"totalTurns"`
	ActualTurns int              `json:"actualTurns"`
	HasOlder    bool             `json:"hasOlder"`
	Next        Cursor           `json:"next,omitempty"`
}

type TurnState struct {
	ID              TurnID `json:"id"`
	CancelRequested bool   `json:"cancelRequested"`
}

type OperationState struct {
	ID              OperationID `json:"id"`
	Kind            string      `json:"kind"`
	CancelRequested bool        `json:"cancelRequested"`
}

type RuntimeState struct {
	Running          bool                 `json:"running"`
	CurrentTurn      *TurnState           `json:"currentTurn,omitempty"`
	CurrentOperation *OperationState      `json:"currentOperation,omitempty"`
	CancelRequested  bool                 `json:"cancelRequested"`
	LastOutcome      SessionOutcome       `json:"lastOutcome,omitempty"`
	LastError        *string              `json:"lastError,omitempty"`
	Interruption     *RuntimeInterruption `json:"interruption,omitempty"`
	LiveEvents       []eventwire.Event    `json:"liveEvents"`
}

type SessionOutcome string

const (
	OutcomeCompleted   SessionOutcome = "completed"
	OutcomeCancelled   SessionOutcome = "cancelled"
	OutcomeFailed      SessionOutcome = "failed"
	OutcomeInterrupted SessionOutcome = "interrupted"
)

type RuntimeInterruption struct {
	PreviousTurnInterrupted bool   `json:"previousTurnInterrupted"`
	Reason                  string `json:"reason"`
}

type PromptKind string

const (
	PromptApproval PromptKind = "approval"
	PromptAsk      PromptKind = "ask"
)

type PromptDecision string

const (
	DecisionAllowOnce       PromptDecision = "allow_once"
	DecisionAllowSession    PromptDecision = "allow_session"
	DecisionAllowPersistent PromptDecision = "allow_persistent"
	DecisionDeny            PromptDecision = "deny"
)

type ApprovalPrompt struct {
	ID               PromptID         `json:"id"`
	Tool             string           `json:"tool"`
	Subject          string           `json:"subject"`
	Reason           *string          `json:"reason,omitempty"`
	Fresh            bool             `json:"fresh"`
	AllowedDecisions []PromptDecision `json:"allowedDecisions"`
}

type AskOption struct {
	Label       string  `json:"label"`
	Description *string `json:"description,omitempty"`
}

type AskQuestion struct {
	ID      QuestionID  `json:"id"`
	Header  string      `json:"header,omitempty"`
	Prompt  *string     `json:"prompt"`
	Options []AskOption `json:"options"`
	Multi   bool        `json:"multi"`
}

type AskPrompt struct {
	ID        PromptID      `json:"id"`
	Questions []AskQuestion `json:"questions"`
}

type PendingPrompt struct {
	Kind     PromptKind      `json:"kind"`
	Approval *ApprovalPrompt `json:"approval,omitempty"`
	Ask      *AskPrompt      `json:"ask,omitempty"`
}

type GoalStatus string

const (
	GoalRunning  GoalStatus = "running"
	GoalComplete GoalStatus = "complete"
	GoalBlocked  GoalStatus = "blocked"
	GoalStopped  GoalStatus = "stopped"
)

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

type TodoItem struct {
	Content    *string    `json:"content,omitempty"`
	Status     TodoStatus `json:"status"`
	ActiveForm string     `json:"activeForm,omitempty"`
	Level      int        `json:"level,omitempty"`
}

type UsageSource struct {
	Source           string  `json:"source"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	TotalTokens      int     `json:"totalTokens"`
	ReasoningTokens  int     `json:"reasoningTokens"`
	CacheHitTokens   int     `json:"cacheHitTokens"`
	CacheMissTokens  int     `json:"cacheMissTokens"`
	RequestCount     int     `json:"requestCount"`
	SessionCost      float64 `json:"sessionCost,omitempty"`
	SessionCurrency  string  `json:"sessionCurrency,omitempty"`
}

type ReadFileRecord struct {
	Path      string `json:"path"`
	Turn      int    `json:"turn"`
	TimeMs    int64  `json:"timeMs"`
	Offset    *int64 `json:"offset,omitempty"`
	Limit     *int64 `json:"limit,omitempty"`
	Truncated bool   `json:"truncated"`
}

type ContextView struct {
	UsedTokens              int              `json:"usedTokens"`
	WindowTokens            int              `json:"windowTokens"`
	PromptTokens            int              `json:"promptTokens"`
	CompletionTokens        int              `json:"completionTokens"`
	TotalTokens             int              `json:"totalTokens"`
	ReasoningTokens         int              `json:"reasoningTokens"`
	CacheHitTokens          int              `json:"cacheHitTokens"`
	CacheMissTokens         int              `json:"cacheMissTokens"`
	SessionCacheHitTokens   int              `json:"sessionCacheHitTokens"`
	SessionCacheMissTokens  int              `json:"sessionCacheMissTokens"`
	SessionCompletionTokens int              `json:"sessionCompletionTokens"`
	RequestCount            int              `json:"requestCount"`
	ElapsedMillis           int64            `json:"elapsedMillis"`
	SessionCost             float64          `json:"sessionCost,omitempty"`
	SessionCurrency         string           `json:"sessionCurrency,omitempty"`
	Sources                 []UsageSource    `json:"sources"`
	ReadFiles               []ReadFileRecord `json:"readFiles"`
}

type JobKind string

const (
	JobBash JobKind = "bash"
	JobTask JobKind = "task"
)

type JobStatus string

const JobRunning JobStatus = "running"

type Job struct {
	ID              JobID     `json:"id"`
	Kind            JobKind   `json:"kind"`
	Label           string    `json:"label"`
	Status          JobStatus `json:"status"`
	StartedAtMillis int64     `json:"startedAtMillis"`
}

type Checkpoint struct {
	ID              CheckpointID `json:"id"`
	DisplayTurn     int          `json:"displayTurn"`
	Prompt          *string      `json:"prompt,omitempty"`
	Files           []string     `json:"files"`
	FileCount       int          `json:"fileCount"`
	FilesTruncated  bool         `json:"filesTruncated"`
	CreatedAtMillis int64        `json:"createdAtMillis"`
	CanCode         bool         `json:"canCode"`
	CanConversation bool         `json:"canConversation"`
}

type SessionSnapshot struct {
	Session       SessionRef      `json:"session"`
	TopicID       TopicID         `json:"topicId"`
	Title         string          `json:"title"`
	Profile       ResolvedProfile `json:"profile"`
	Goal          *string         `json:"goal,omitempty"`
	GoalStatus    GoalStatus      `json:"goalStatus,omitempty"`
	Capabilities  Capabilities    `json:"capabilities"`
	Runtime       RuntimeState    `json:"runtime"`
	History       HistoryPage     `json:"history"`
	PendingPrompt *PendingPrompt  `json:"pendingPrompt,omitempty"`
	Todos         []TodoItem      `json:"todos"`
	Context       ContextView     `json:"context"`
	Jobs          []Job           `json:"jobs"`
	Checkpoints   []Checkpoint    `json:"checkpoints"`
}

type AttachAndSubscribeInput struct {
	Session      SessionRef `json:"session"`
	HistoryTurns int        `json:"historyTurns"`
}

type SubmitKind string

const (
	SubmitTurn      SubmitKind = "turn"
	SubmitOperation SubmitKind = "operation"
	SubmitCompleted SubmitKind = "completed"
)

type ComposerSubmitInput struct {
	Session          SessionRef   `json:"session"`
	Input            string       `json:"input"`
	DisplayText      string       `json:"displayText,omitempty"`
	EditedOriginal   string       `json:"editedOriginal,omitempty"`
	Invocations      []Invocation `json:"invocations,omitempty"`
	DeliveryRecovery bool         `json:"deliveryRecovery,omitempty"`
}

type ComposerSubmitResult struct {
	Kind             SubmitKind  `json:"kind"`
	TurnID           TurnID      `json:"turnId,omitempty"`
	OperationID      OperationID `json:"operationId,omitempty"`
	Operation        string      `json:"operation,omitempty"`
	Effect           string      `json:"effect,omitempty"`
	Session          SessionRef  `json:"session"`
	SnapshotRequired bool        `json:"snapshotRequired,omitempty"`
}

type SteerInput struct {
	Session SessionRef `json:"session"`
	TurnID  TurnID     `json:"turnId"`
	Text    string     `json:"text"`
}

type CancelTurnInput struct {
	Session SessionRef `json:"session"`
	TurnID  TurnID     `json:"turnId"`
}

type ApproveInput struct {
	Session  SessionRef     `json:"session"`
	PromptID PromptID       `json:"promptId"`
	Decision PromptDecision `json:"decision"`
}

type QuestionAnswer struct {
	QuestionID QuestionID `json:"questionId"`
	Selected   []string   `json:"selected"`
}

type AnswerInput struct {
	Session  SessionRef       `json:"session"`
	PromptID PromptID         `json:"promptId"`
	Answers  []QuestionAnswer `json:"answers"`
}

// SnapshotUpdate is emitted after an adapter has atomically migrated a
// subscription to a replacement runtime or Session. Previous identifies the
// workbench binding to replace; Snapshot is the new Host/Local authority. No
// transport epoch, sequence or subscription identity crosses RuntimeAPI.
type SnapshotUpdate struct {
	Previous SessionRef      `json:"previous"`
	Snapshot SessionSnapshot `json:"snapshot"`
}

// Event is emitted only after an adapter has validated ordering and completed
// any external content hydration. Sequence and subscription mechanics therefore
// remain private to that adapter. Exactly one of Value.Kind or Snapshot is
// populated: ordinary semantic events flow through Value, while an atomic
// subscription migration flows through Snapshot.
type Event struct {
	Session     SessionRef      `json:"session"`
	TurnID      TurnID          `json:"turnId,omitempty"`
	OperationID OperationID     `json:"operationId,omitempty"`
	Value       eventwire.Event `json:"value"`
	Snapshot    *SnapshotUpdate `json:"snapshot,omitempty"`
}

type ConnectionAPI interface {
	Connection(context.Context) (ConnectionView, error)
}

type WorkspaceAPI interface {
	BrowseWorkspace(context.Context, BrowseWorkspaceInput) (WorkspacePage, error)
	OpenWorkspace(context.Context, OpenWorkspaceInput) (OpenWorkspaceResult, error)
}

type SessionAPI interface {
	CreateSession(context.Context, CreateSessionInput) (CreatedSession, error)
	AttachAndSubscribe(context.Context, AttachAndSubscribeInput) (SessionSnapshot, error)
}

type ComposerAPI interface {
	ComposerSubmit(context.Context, ComposerSubmitInput) (ComposerSubmitResult, error)
}

type TurnAPI interface {
	SteerTurn(context.Context, SteerInput) error
	CancelTurn(context.Context, CancelTurnInput) error
}

type PromptAPI interface {
	ApprovePrompt(context.Context, ApproveInput) error
	AnswerPrompt(context.Context, AnswerInput) error
}

type EventAPI interface {
	Events() <-chan Event
}

// RuntimeAPI is the source-compatible target-neutral Phase 5 core. The frozen
// Remote V1 workbench surface is V1RuntimeAPI below; it composes focused
// subinterfaces so adapters can migrate domain-by-domain without exposing
// transport parameters.
type RuntimeAPI interface {
	ConnectionAPI
	WorkspaceAPI
	SessionAPI
	ComposerAPI
	TurnAPI
	PromptAPI
	EventAPI
}

package protocol

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/rpcwire"
)

const DomainErrorCode = -32000

type ReasonixErrorCode string

const (
	ErrRemoteNotInstalled         ReasonixErrorCode = "REMOTE_NOT_INSTALLED"
	ErrHostStopped                ReasonixErrorCode = "HOST_STOPPED"
	ErrVersionMismatch            ReasonixErrorCode = "VERSION_MISMATCH"
	ErrDaemonRestartRequired      ReasonixErrorCode = "DAEMON_RESTART_REQUIRED"
	ErrHostBusy                   ReasonixErrorCode = "HOST_BUSY"
	ErrStaleHostEpoch             ReasonixErrorCode = "STALE_HOST_EPOCH"
	ErrStaleRuntimeEpoch          ReasonixErrorCode = "STALE_RUNTIME_EPOCH"
	ErrRequestIDConflict          ReasonixErrorCode = "REQUEST_ID_CONFLICT"
	ErrLeaseNotHeld               ReasonixErrorCode = "LEASE_NOT_HELD"
	ErrStaleConnection            ReasonixErrorCode = "STALE_CONNECTION"
	ErrStaleDirectoryRef          ReasonixErrorCode = "STALE_DIRECTORY_REF"
	ErrDirectoryNotFound          ReasonixErrorCode = "DIRECTORY_NOT_FOUND"
	ErrNotDirectory               ReasonixErrorCode = "NOT_DIRECTORY"
	ErrPermissionDenied           ReasonixErrorCode = "PERMISSION_DENIED"
	ErrWorkspaceNotFound          ReasonixErrorCode = "WORKSPACE_NOT_FOUND"
	ErrWorkspaceInUse             ReasonixErrorCode = "WORKSPACE_IN_USE"
	ErrSessionNotFound            ReasonixErrorCode = "SESSION_NOT_FOUND"
	ErrWorkspaceSessionMismatch   ReasonixErrorCode = "WORKSPACE_SESSION_MISMATCH"
	ErrRuntimeStartFailed         ReasonixErrorCode = "RUNTIME_START_FAILED"
	ErrSessionPersistFailed       ReasonixErrorCode = "SESSION_PERSIST_FAILED"
	ErrSessionTrashed             ReasonixErrorCode = "SESSION_TRASHED"
	ErrSessionBusy                ReasonixErrorCode = "SESSION_BUSY"
	ErrSessionCleanupPending      ReasonixErrorCode = "SESSION_CLEANUP_PENDING"
	ErrTopicNotFound              ReasonixErrorCode = "TOPIC_NOT_FOUND"
	ErrTopicNotEmpty              ReasonixErrorCode = "TOPIC_NOT_EMPTY"
	ErrTrashEntryNotFound         ReasonixErrorCode = "TRASH_ENTRY_NOT_FOUND"
	ErrRecoveryGuardFailed        ReasonixErrorCode = "RECOVERY_GUARD_FAILED"
	ErrInvalidProfile             ReasonixErrorCode = "INVALID_PROFILE"
	ErrModelNotAvailable          ReasonixErrorCode = "MODEL_NOT_AVAILABLE"
	ErrEffortNotSupported         ReasonixErrorCode = "EFFORT_NOT_SUPPORTED"
	ErrTurnAlreadyRunning         ReasonixErrorCode = "TURN_ALREADY_RUNNING"
	ErrTurnNotActive              ReasonixErrorCode = "TURN_NOT_ACTIVE"
	ErrTurnMismatch               ReasonixErrorCode = "TURN_MISMATCH"
	ErrOperationNotActive         ReasonixErrorCode = "OPERATION_NOT_ACTIVE"
	ErrOperationMismatch          ReasonixErrorCode = "OPERATION_MISMATCH"
	ErrPromptNotPending           ReasonixErrorCode = "PROMPT_NOT_PENDING"
	ErrPromptKindMismatch         ReasonixErrorCode = "PROMPT_KIND_MISMATCH"
	ErrPromptDecisionNotAllowed   ReasonixErrorCode = "PROMPT_DECISION_NOT_ALLOWED"
	ErrSnapshotExpired            ReasonixErrorCode = "SNAPSHOT_EXPIRED"
	ErrSubscriptionNotFound       ReasonixErrorCode = "SUBSCRIPTION_NOT_FOUND"
	ErrContentRefExpired          ReasonixErrorCode = "CONTENT_REF_EXPIRED"
	ErrCheckpointNotFound         ReasonixErrorCode = "CHECKPOINT_NOT_FOUND"
	ErrCheckpointScopeUnavailable ReasonixErrorCode = "CHECKPOINT_SCOPE_UNAVAILABLE"
	ErrRewindPartial              ReasonixErrorCode = "REWIND_PARTIAL"
	ErrStaleCursor                ReasonixErrorCode = "STALE_CURSOR"
	ErrPathNotFound               ReasonixErrorCode = "PATH_NOT_FOUND"
	ErrNotFile                    ReasonixErrorCode = "NOT_FILE"
	ErrGitUnavailable             ReasonixErrorCode = "GIT_UNAVAILABLE"
	ErrGitObjectNotFound          ReasonixErrorCode = "GIT_OBJECT_NOT_FOUND"
	ErrQueryFailed                ReasonixErrorCode = "QUERY_FAILED"
	ErrCapabilityUnavailable      ReasonixErrorCode = "CAPABILITY_UNAVAILABLE"
)

type RemoteErrorData struct {
	ReasonixCode               ReasonixErrorCode `json:"reasonixCode"`
	Retryable                  bool              `json:"retryable"`
	Action                     RemoteAction      `json:"action,omitempty"`
	Target                     *RuntimeTarget    `json:"target,omitempty"`
	Expected                   string            `json:"expected,omitempty"`
	Actual                     string            `json:"actual,omitempty"`
	RetryAfterMs               *int64            `json:"retryAfterMs,omitempty"`
	SuggestedCommand           string            `json:"suggestedCommand,omitempty"`
	WorkspaceMayHaveChanged    *bool             `json:"workspaceMayHaveChanged,omitempty"`
	ConversationMayHaveChanged *bool             `json:"conversationMayHaveChanged,omitempty"`
	SnapshotRequired           *bool             `json:"snapshotRequired,omitempty"`
}

type errorSpec struct {
	Message   string
	Retryable bool
	Action    RemoteAction
	Command   string
}

var frozenErrorSpecs = map[ReasonixErrorCode]errorSpec{
	ErrRemoteNotInstalled:         {"Reasonix Remote is not installed on the Host.", false, ActionRunCommand, "reasonix remote install"},
	ErrHostStopped:                {"Reasonix Remote is not running on the Host.", true, ActionRunCommand, "reasonix remote start"},
	ErrVersionMismatch:            {"Desktop and Host CLI builds do not match.", false, ActionNone, ""},
	ErrDaemonRestartRequired:      {"The running daemon does not match the installed CLI.", false, ActionRestartDaemon, "reasonix remote restart"},
	ErrHostBusy:                   {"Another client currently holds the Host lease.", true, ActionRetry, ""},
	ErrStaleHostEpoch:             {"The Host runtime has restarted.", true, ActionReconnect, ""},
	ErrStaleRuntimeEpoch:          {"The Session runtime has been replaced.", true, ActionResubscribe, ""},
	ErrRequestIDConflict:          {"The request identifier was reused for a different operation.", false, ActionNone, ""},
	ErrLeaseNotHeld:               {"The client no longer holds the Host lease.", true, ActionReconnect, ""},
	ErrStaleConnection:            {"This transport has been replaced by a newer connection.", true, ActionReconnect, ""},
	ErrStaleDirectoryRef:          {"The directory selection has expired.", true, ActionRetry, ""},
	ErrDirectoryNotFound:          {"The selected directory no longer exists.", false, ActionNone, ""},
	ErrNotDirectory:               {"The selected path is not a directory.", false, ActionNone, ""},
	ErrPermissionDenied:           {"The Host user cannot access the requested resource.", false, ActionNone, ""},
	ErrWorkspaceNotFound:          {"The workspace is not available on this Host.", false, ActionNone, ""},
	ErrWorkspaceInUse:             {"The workspace still has active Session work.", true, ActionRetry, ""},
	ErrSessionNotFound:            {"The Session is not available.", false, ActionNone, ""},
	ErrWorkspaceSessionMismatch:   {"The Session does not belong to the requested workspace.", false, ActionNone, ""},
	ErrRuntimeStartFailed:         {"The Session runtime could not be started.", true, ActionRetry, ""},
	ErrSessionPersistFailed:       {"The Session state could not be persisted.", true, ActionRetry, ""},
	ErrSessionTrashed:             {"The Session is in the trash.", false, ActionNone, ""},
	ErrSessionBusy:                {"The Session is busy with conflicting work.", true, ActionRetry, ""},
	ErrSessionCleanupPending:      {"Session cleanup is still in progress.", true, ActionRetry, ""},
	ErrTopicNotFound:              {"The Topic is not available.", false, ActionNone, ""},
	ErrTopicNotEmpty:              {"Only an empty Topic can be deleted.", false, ActionNone, ""},
	ErrTrashEntryNotFound:         {"The trash entry is not available.", false, ActionNone, ""},
	ErrRecoveryGuardFailed:        {"The recovery-only safety check failed.", false, ActionNone, ""},
	ErrInvalidProfile:             {"The requested Session profile is invalid.", false, ActionNone, ""},
	ErrModelNotAvailable:          {"The requested model is not available on the Host.", false, ActionNone, ""},
	ErrEffortNotSupported:         {"The requested effort is not supported by the model.", false, ActionNone, ""},
	ErrTurnAlreadyRunning:         {"A Turn is already running in this Session.", true, ActionRetry, ""},
	ErrTurnNotActive:              {"There is no active Turn.", false, ActionNone, ""},
	ErrTurnMismatch:               {"The active Turn does not match the requested Turn.", false, ActionResubscribe, ""},
	ErrOperationNotActive:         {"There is no active Operation.", false, ActionNone, ""},
	ErrOperationMismatch:          {"The active Operation does not match the requested Operation.", false, ActionResubscribe, ""},
	ErrPromptNotPending:           {"The Prompt is no longer pending.", false, ActionResubscribe, ""},
	ErrPromptKindMismatch:         {"The Prompt type does not match this response.", false, ActionResubscribe, ""},
	ErrPromptDecisionNotAllowed:   {"The requested decision is not allowed for this Prompt.", false, ActionNone, ""},
	ErrSnapshotExpired:            {"The Session snapshot has expired.", true, ActionResubscribe, ""},
	ErrSubscriptionNotFound:       {"The subscription is not available on this transport.", true, ActionResubscribe, ""},
	ErrContentRefExpired:          {"The referenced content has expired.", true, ActionResubscribe, ""},
	ErrCheckpointNotFound:         {"The checkpoint is not available in this runtime.", false, ActionResubscribe, ""},
	ErrCheckpointScopeUnavailable: {"The checkpoint does not support the requested rewind scope.", false, ActionNone, ""},
	ErrRewindPartial:              {"Rewind did not complete and state may have changed.", false, ActionResubscribe, ""},
	ErrStaleCursor:                {"The query cursor has expired.", true, ActionRetry, ""},
	ErrPathNotFound:               {"The requested workspace path does not exist.", false, ActionNone, ""},
	ErrNotFile:                    {"The requested path is not a regular file.", false, ActionNone, ""},
	ErrGitUnavailable:             {"Git information is not available for this workspace.", false, ActionNone, ""},
	ErrGitObjectNotFound:          {"The requested Git object is not available.", false, ActionNone, ""},
	ErrQueryFailed:                {"The Host query could not be completed.", true, ActionRetry, ""},
	ErrCapabilityUnavailable:      {"This capability is unavailable on the Host.", false, ActionNone, ""},
}

type ErrorOptions struct {
	Target                     *RuntimeTarget
	Expected                   string
	Actual                     string
	RetryAfterMs               *int64
	WorkspaceMayHaveChanged    *bool
	ConversationMayHaveChanged *bool
	SnapshotRequired           *bool
}

type RemoteError struct {
	Code    ReasonixErrorCode
	Message string
	Data    RemoteErrorData
}

func (e *RemoteError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *RemoteError) RPCError() *rpcwire.RPCError {
	if e == nil {
		return &rpcwire.RPCError{Code: rpcwire.ErrInternal, Message: "internal error"}
	}
	return &rpcwire.RPCError{Code: DomainErrorCode, Message: e.Message, Data: e.Data}
}

func NewRemoteError(code ReasonixErrorCode, options ErrorOptions) (*RemoteError, error) {
	spec, ok := frozenErrorSpecs[code]
	if !ok {
		return nil, fmt.Errorf("protocol: unknown Reasonix error code %q", code)
	}
	data := RemoteErrorData{
		ReasonixCode: code, Retryable: spec.Retryable, Action: spec.Action,
		Target: options.Target, Expected: strings.TrimSpace(options.Expected),
		Actual: strings.TrimSpace(options.Actual), RetryAfterMs: options.RetryAfterMs,
		SuggestedCommand:           spec.Command,
		WorkspaceMayHaveChanged:    options.WorkspaceMayHaveChanged,
		ConversationMayHaveChanged: options.ConversationMayHaveChanged,
		SnapshotRequired:           options.SnapshotRequired,
	}
	if err := data.Validate(); err != nil {
		return nil, err
	}
	return &RemoteError{Code: code, Message: spec.Message, Data: data}, nil
}

func MustRemoteError(code ReasonixErrorCode, options ErrorOptions) *RemoteError {
	errValue, err := NewRemoteError(code, options)
	if err != nil {
		panic(err)
	}
	return errValue
}

func (d RemoteErrorData) Validate() error {
	spec, ok := frozenErrorSpecs[d.ReasonixCode]
	if !ok {
		return fmt.Errorf("unknown reasonixCode %q", d.ReasonixCode)
	}
	if d.Retryable != spec.Retryable || d.Action != spec.Action || d.SuggestedCommand != spec.Command {
		return errors.New("retryable, action, and suggestedCommand must match the frozen error table")
	}
	if d.Target != nil {
		if err := d.Target.Validate(); err != nil {
			return fmt.Errorf("target: %w", err)
		}
	}
	if (d.Expected == "") != (d.Actual == "") {
		return errors.New("expected and actual must be supplied together")
	}
	if err := validateControlledDiagnosticValue("expected", d.Expected); err != nil {
		return err
	}
	if err := validateControlledDiagnosticValue("actual", d.Actual); err != nil {
		return err
	}
	if d.ReasonixCode == ErrHostBusy {
		if d.RetryAfterMs == nil || *d.RetryAfterMs < 0 {
			return errors.New("HOST_BUSY requires a non-negative retryAfterMs")
		}
	} else if d.RetryAfterMs != nil {
		return errors.New("retryAfterMs is only valid for HOST_BUSY")
	}
	rewindFields := d.WorkspaceMayHaveChanged != nil || d.ConversationMayHaveChanged != nil || d.SnapshotRequired != nil
	if d.ReasonixCode == ErrRewindPartial {
		if d.WorkspaceMayHaveChanged == nil || d.ConversationMayHaveChanged == nil || d.SnapshotRequired == nil {
			return errors.New("REWIND_PARTIAL requires all change and snapshot flags")
		}
		if !*d.SnapshotRequired || (!*d.WorkspaceMayHaveChanged && !*d.ConversationMayHaveChanged) {
			return errors.New("REWIND_PARTIAL requires snapshot refresh and at least one possibly changed range")
		}
	} else if rewindFields {
		return errors.New("change and snapshot flags are only valid for REWIND_PARTIAL")
	}
	return nil
}

func validateControlledDiagnosticValue(name, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 256 || strings.ContainsAny(value, "\x00\r\n/\\") {
		return fmt.Errorf("%s must be a bounded path-free diagnostic token", name)
	}
	return nil
}

type ErrorContract struct {
	ReasonixCode     ReasonixErrorCode `json:"reasonixCode"`
	JSONRPCCode      int               `json:"jsonRpcCode"`
	Message          string            `json:"message"`
	Retryable        bool              `json:"retryable"`
	Action           RemoteAction      `json:"action,omitempty"`
	SuggestedCommand string            `json:"suggestedCommand,omitempty"`
}

func ErrorContracts() []ErrorContract {
	out := make([]ErrorContract, 0, len(frozenErrorSpecs))
	for code, spec := range frozenErrorSpecs {
		out = append(out, ErrorContract{code, DomainErrorCode, spec.Message, spec.Retryable, spec.Action, spec.Command})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReasonixCode < out[j].ReasonixCode })
	return out
}

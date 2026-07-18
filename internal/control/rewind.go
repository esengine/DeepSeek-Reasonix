package control

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/event"
)

// RewindFailure identifies a transport-neutral failure that a runtime host can
// map to its own public error vocabulary without matching human-readable text.
type RewindFailure string

const (
	RewindFailureUnavailable       RewindFailure = "unavailable"
	RewindFailureInvalidScope      RewindFailure = "invalid_scope"
	RewindFailureCheckpointMissing RewindFailure = "checkpoint_missing"
	RewindFailureScopeUnavailable  RewindFailure = "scope_unavailable"
	RewindFailureBusy              RewindFailure = "busy"
	RewindFailureApply             RewindFailure = "apply_failed"
	RewindFailurePartial           RewindFailure = "partial"
)

var (
	ErrRewindUnavailable       = errors.New("rewind unavailable")
	ErrRewindInvalidScope      = errors.New("invalid rewind scope")
	ErrRewindCheckpointMissing = errors.New("rewind checkpoint missing")
	ErrRewindScopeUnavailable  = errors.New("rewind scope unavailable")
	ErrRewindBusy              = errors.New("rewind busy")
	ErrRewindApply             = errors.New("rewind apply failed")
	ErrRewindPartial           = errors.New("rewind partially applied")
)

// RewindResult reports only changes confirmed by a successful write. A partial
// failure carries the conservative "may have changed" ranges in RewindError.
// SnapshotRequired is true for every successful rewind, including a code rewind
// whose restore plan was already reflected in the workspace.
type RewindResult struct {
	WorkspaceChanged      bool
	ConversationRewritten bool
	SnapshotRequired      bool
}

// RewindError is the detailed, protocol-neutral failure returned by
// RewindDetailed. The three impact fields are set only for Failure=partial;
// callers must refresh both the session snapshot and any indicated workspace
// view before allowing another destructive decision.
type RewindError struct {
	Failure                    RewindFailure
	Turn                       int
	Scope                      RewindScope
	WorkspaceMayHaveChanged    bool
	ConversationMayHaveChanged bool
	SnapshotRequired           bool
	Cause                      error
}

func (e *RewindError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return rewindFailureSentinel(e.Failure).Error()
}

func (e *RewindError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is lets hosts map stable failure classes with errors.Is while errors.As still
// exposes partial-impact flags and the diagnostic cause for controlled logging.
func (e *RewindError) Is(target error) bool {
	if e == nil {
		return false
	}
	if target == rewindFailureSentinel(e.Failure) {
		return true
	}
	return errors.Is(e.Cause, target)
}

func rewindFailureSentinel(failure RewindFailure) error {
	switch failure {
	case RewindFailureUnavailable:
		return ErrRewindUnavailable
	case RewindFailureInvalidScope:
		return ErrRewindInvalidScope
	case RewindFailureCheckpointMissing:
		return ErrRewindCheckpointMissing
	case RewindFailureScopeUnavailable:
		return ErrRewindScopeUnavailable
	case RewindFailureBusy:
		return ErrRewindBusy
	case RewindFailureApply:
		return ErrRewindApply
	case RewindFailurePartial:
		return ErrRewindPartial
	default:
		return ErrRewindApply
	}
}

func newRewindError(failure RewindFailure, turn int, scope RewindScope, cause error) *RewindError {
	if cause == nil {
		cause = rewindFailureSentinel(failure)
	}
	return &RewindError{Failure: failure, Turn: turn, Scope: scope, Cause: cause}
}

func newPartialRewindError(turn int, scope RewindScope, workspace, conversation bool, cause error) *RewindError {
	if cause == nil {
		cause = ErrRewindPartial
	}
	return &RewindError{
		Failure:                    RewindFailurePartial,
		Turn:                       turn,
		Scope:                      scope,
		WorkspaceMayHaveChanged:    workspace,
		ConversationMayHaveChanged: conversation,
		SnapshotRequired:           true,
		Cause:                      cause,
	}
}

type rewindPlan struct {
	boundary int
	messages int
	code     bool
}

// RewindDetailed implements the frozen rewind transaction boundary used by
// both local frontends and long-lived runtime hosts. For scope=both it freezes
// and validates the complete code + conversation plan before RestoreCode can
// touch the workspace. Writes are deliberately ordered code then conversation.
//
// A runtime that exposes opaque checkpoint identities must resolve that ID
// against the current CheckpointSnapshot immediately before this call and must
// discard the mapping after a conversation rewrite. This method independently
// rejects a turn that is no longer present, so a removed boundary cannot yield
// a false success; the opaque-ID non-reuse rule remains the runtime's concern.
func (c *Controller) RewindDetailed(turn int, scope RewindScope) (RewindResult, error) {
	return c.rewindDetailed(turn, scope, false)
}

func (c *Controller) rewindDetailed(turn int, scope RewindScope, allowEmptyCode bool) (result RewindResult, err error) {
	if scope != RewindCode && scope != RewindConversation && scope != RewindBoth {
		return RewindResult{}, newRewindError(RewindFailureInvalidScope, turn, scope,
			fmt.Errorf("%w: %d", ErrRewindInvalidScope, scope))
	}
	if !c.checkpoints.enabled() || c.executor == nil {
		return RewindResult{}, newRewindError(RewindFailureUnavailable, turn, scope,
			fmt.Errorf("%w: checkpoints unavailable", ErrRewindUnavailable))
	}
	if rotationErr := c.beginRotation(); rotationErr != nil {
		if errors.Is(rotationErr, errTurnRunningRotation) {
			rotationErr = fmt.Errorf("%w: cannot rewind while a turn is running: %w", ErrRewindBusy, rotationErr)
		} else {
			rotationErr = fmt.Errorf("%w: %w", ErrRewindBusy, rotationErr)
		}
		return RewindResult{}, newRewindError(RewindFailureBusy, turn, scope, rotationErr)
	}
	defer c.endRotation()

	workspaceWriteStarted := false
	conversationWriteStarted := false
	defer func() {
		if recovered := recover(); recovered != nil {
			cause := fmt.Errorf("rewind failed internally: %v", recovered)
			if workspaceWriteStarted || conversationWriteStarted {
				err = newPartialRewindError(turn, scope, workspaceWriteStarted, conversationWriteStarted, cause)
				result.SnapshotRequired = true
				return
			}
			err = newRewindError(RewindFailureApply, turn, scope, cause)
			result = RewindResult{}
		}
	}()

	plan, planErr := c.preflightRewind(turn, scope, allowEmptyCode)
	if planErr != nil {
		return RewindResult{}, planErr
	}
	result.SnapshotRequired = true

	if scope == RewindCode || scope == RewindBoth {
		workspaceWriteStarted = plan.code
		written, deleted, restoreErr := c.checkpoints.restoreCode(turn)
		result.WorkspaceChanged = len(written) > 0 || len(deleted) > 0
		if restoreErr != nil {
			return result, newPartialRewindError(turn, scope, true, false,
				fmt.Errorf("rewind code: %w", restoreErr))
		}
		if result.WorkspaceChanged {
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
				Text: fmt.Sprintf("rewound code to turn %d — %d file(s) restored, %d removed", turn, len(written), len(deleted))})
		}
	}

	if scope == RewindConversation || scope == RewindBoth {
		session := c.executor.Session()
		messages := session.Snapshot()
		// Rotation excludes turns and conflicting operations. The length check is
		// retained as a defensive invariant in case a future non-turn writer is
		// introduced after preflight.
		if len(messages) != plan.messages || plan.boundary < 0 || plan.boundary > len(messages) {
			if result.WorkspaceChanged {
				return result, newPartialRewindError(turn, scope, true, false,
					fmt.Errorf("conversation changed after rewind preflight"))
			}
			return RewindResult{}, newRewindError(RewindFailureApply, turn, scope,
				fmt.Errorf("%w: conversation changed after rewind preflight", ErrRewindApply))
		}
		conversationWriteStarted = true
		session.Replace(messages[:plan.boundary])
		result.ConversationRewritten = true
		c.checkpoints.truncateFrom(turn)
		if snapshotErr := c.SnapshotRewrite(); snapshotErr != nil {
			return result, newPartialRewindError(turn, scope, result.WorkspaceChanged, true,
				fmt.Errorf("persist rewound conversation: %w", snapshotErr))
		}
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("rewound conversation to turn %d", turn)})
	}

	return result, nil
}

func (c *Controller) preflightRewind(turn int, scope RewindScope, allowEmptyCode bool) (rewindPlan, *RewindError) {
	snapshot := c.checkpoints.capture()
	checkpointIndex := -1
	for index, meta := range snapshot.Metas {
		if meta.Turn == turn {
			checkpointIndex = index
			break
		}
	}
	if checkpointIndex < 0 {
		return rewindPlan{}, newRewindError(RewindFailureCheckpointMissing, turn, scope,
			fmt.Errorf("%w: turn %d", ErrRewindCheckpointMissing, turn))
	}

	plan := rewindPlan{}
	if scope == RewindCode || scope == RewindBoth {
		for _, meta := range snapshot.Metas[checkpointIndex:] {
			for _, path := range meta.Paths {
				if strings.TrimSpace(path) == "" || !rewindPathWithinRoot(c.workspaceRoot, path) {
					return rewindPlan{}, newRewindError(RewindFailureScopeUnavailable, turn, scope,
						fmt.Errorf("%w: code checkpoint contains an unsafe path", ErrRewindScopeUnavailable))
				}
				plan.code = true
			}
		}
		if !plan.code && !allowEmptyCode {
			return rewindPlan{}, newRewindError(RewindFailureScopeUnavailable, turn, scope,
				fmt.Errorf("%w: code restore is unavailable for turn %d", ErrRewindScopeUnavailable, turn))
		}
	}

	if scope == RewindConversation || scope == RewindBoth {
		boundary, hasBoundary := c.checkpoints.boundary(turn)
		if !snapshot.ConversationAvailable[turn] || !hasBoundary {
			return rewindPlan{}, newRewindError(RewindFailureScopeUnavailable, turn, scope,
				fmt.Errorf("%w: conversation rewind unavailable for turn %d (resumed session)", ErrRewindScopeUnavailable, turn))
		}
		messages := c.executor.Session().Snapshot()
		if boundary < 0 || boundary > len(messages) {
			return rewindPlan{}, newRewindError(RewindFailureScopeUnavailable, turn, scope,
				fmt.Errorf("%w: conversation rewind unavailable for turn %d: the conversation was compacted past this point", ErrRewindScopeUnavailable, turn))
		}
		plan.boundary = boundary
		plan.messages = len(messages)
	}
	return plan, nil
}

func rewindPathWithinRoot(root, path string) bool {
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(root, path)
	}
	absolute = filepath.Clean(absolute)
	if root == "" {
		return true
	}
	relative, err := filepath.Rel(filepath.Clean(root), absolute)
	return err == nil && filepath.IsLocal(relative)
}

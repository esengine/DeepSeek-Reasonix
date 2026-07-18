package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/proc"
	"reasonix/internal/sandbox"
)

// OperationKind identifies one non-Turn foreground action. These values are
// transport-neutral: a runtime may bind an admitted handle to its own opaque
// identity without leaking that identity into Controller business rules.
type OperationKind string

const (
	OperationShell     OperationKind = "shell"
	OperationCompact   OperationKind = "compact"
	OperationSummarize OperationKind = "summarize"
)

// SummarizeDirection selects which side of a checkpoint boundary is folded.
type SummarizeDirection string

const (
	SummarizeFrom SummarizeDirection = "from"
	SummarizeUpTo SummarizeDirection = "up_to"
)

// OperationSpec is the immutable input captured at synchronous admission.
// Shell uses Command, Compact uses Instructions, and Summarize uses Turn plus
// Direction. A Controller never derives or stores a transport operation ID.
type OperationSpec struct {
	Kind         OperationKind
	Command      string
	Instructions string
	Turn         int
	Direction    SummarizeDirection
}

// ForegroundBusyState identifies exactly which gate rejected an Operation.
// Callers can use errors.Is(err, ErrSessionBusy) for protocol mapping and
// errors.As for diagnostics without string matching.
type ForegroundBusyState string

const (
	ForegroundTurn      ForegroundBusyState = "turn"
	ForegroundFinishing ForegroundBusyState = "finishing"
	ForegroundRotation  ForegroundBusyState = "rotation"
	ForegroundOperation ForegroundBusyState = "operation"
)

var (
	ErrSessionBusy          = errors.New("session foreground busy")
	ErrControllerClosed     = errors.New("controller closed")
	ErrInvalidOperation     = errors.New("invalid operation")
	ErrOperationUnavailable = errors.New("operation unavailable")
)

// OperationAdmissionError is returned synchronously when another foreground
// owner holds the Session. No worker has been started when this error returns.
type OperationAdmissionError struct {
	State ForegroundBusyState
}

func (e *OperationAdmissionError) Error() string {
	return fmt.Sprintf("%s: %s", ErrSessionBusy, e.State)
}

func (e *OperationAdmissionError) Unwrap() error { return ErrSessionBusy }

// OperationResult is delivered exactly once on an admitted handle's Done
// channel. Cancellation is reported as context.Canceled/DeadlineExceeded;
// ordinary command/provider failures remain distinguishable from cancellation.
type OperationResult struct {
	Err error
}

// OperationCancelAttempt is the strict outcome of cancelling this exact handle.
// A completed or superseded handle can never affect later foreground work.
type OperationCancelAttempt string

const (
	OperationCancelNotActive        OperationCancelAttempt = "not_active"
	OperationCancelRequestedNow     OperationCancelAttempt = "cancel_requested"
	OperationCancelAlreadyRequested OperationCancelAttempt = "already_requested"
)

// OperationHandle is unique to one admitted worker. Its lifecycle flags are
// guarded by Controller.mu; its specification and channels are immutable.
type OperationHandle struct {
	controller      *Controller
	spec            OperationSpec
	done            chan OperationResult
	cancel          context.CancelFunc
	cancelRequested bool
	commitStarted   bool
	holdsRotation   bool
}

// Spec returns the immutable admitted specification.
func (h *OperationHandle) Spec() OperationSpec {
	if h == nil {
		return OperationSpec{}
	}
	return h.spec
}

// Done resolves exactly once when the worker and Controller admission state
// have both completed. The channel is then closed.
func (h *OperationHandle) Done() <-chan OperationResult {
	if h == nil {
		return nil
	}
	return h.done
}

// Cancel requests cancellation of this exact admitted Operation.
func (h *OperationHandle) Cancel() OperationCancelAttempt {
	if h == nil || h.controller == nil {
		return OperationCancelNotActive
	}
	return h.controller.cancelOperation(h)
}

// StartOperation performs strict synchronous admission. Unlike runGuarded it
// never parks: busy/finishing/rotation/closed returns before a goroutine or any
// operation event can be emitted. The worker context belongs to Controller and
// deliberately does not inherit an attaching RPC request context.
func (c *Controller) StartOperation(spec OperationSpec) (*OperationHandle, error) {
	normalized, err := validateOperationSpec(spec)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrControllerClosed
	}
	if c.operation != nil {
		c.mu.Unlock()
		return nil, &OperationAdmissionError{State: ForegroundOperation}
	}
	if c.rotating {
		c.mu.Unlock()
		return nil, &OperationAdmissionError{State: ForegroundRotation}
	}
	if c.running {
		c.mu.Unlock()
		return nil, &OperationAdmissionError{State: ForegroundTurn}
	}
	if c.finishing {
		c.mu.Unlock()
		return nil, &OperationAdmissionError{State: ForegroundFinishing}
	}
	if (normalized.Kind == OperationCompact || normalized.Kind == OperationSummarize) && c.executor == nil {
		c.mu.Unlock()
		return nil, ErrOperationUnavailable
	}

	ctx, cancel := context.WithCancel(context.Background())
	h := &OperationHandle{
		controller:    c,
		spec:          normalized,
		done:          make(chan OperationResult, 1),
		cancel:        cancel,
		holdsRotation: normalized.Kind == OperationCompact || normalized.Kind == OperationSummarize,
	}
	c.operation = h
	if h.holdsRotation {
		// Admission and the rotation claim are one atomic decision. The worker
		// therefore never performs a second beginRotation that could contradict
		// an already-returned accepted handle.
		c.rotating = true
	}
	c.operationWG.Add(1)
	c.mu.Unlock()

	go c.runOperation(ctx, h)
	return h, nil
}

func validateOperationSpec(spec OperationSpec) (OperationSpec, error) {
	spec.Command = strings.TrimSpace(spec.Command)
	spec.Instructions = strings.TrimSpace(spec.Instructions)
	switch spec.Kind {
	case OperationShell:
		if spec.Command == "" || spec.Instructions != "" || spec.Direction != "" || spec.Turn != 0 {
			return OperationSpec{}, fmt.Errorf("%w: shell requires only command", ErrInvalidOperation)
		}
	case OperationCompact:
		if spec.Command != "" || spec.Direction != "" || spec.Turn != 0 {
			return OperationSpec{}, fmt.Errorf("%w: compact accepts only instructions", ErrInvalidOperation)
		}
	case OperationSummarize:
		if spec.Command != "" || spec.Instructions != "" || spec.Turn < 0 ||
			(spec.Direction != SummarizeFrom && spec.Direction != SummarizeUpTo) {
			return OperationSpec{}, fmt.Errorf("%w: summarize requires turn and direction", ErrInvalidOperation)
		}
	default:
		return OperationSpec{}, fmt.Errorf("%w: unknown kind %q", ErrInvalidOperation, spec.Kind)
	}
	return spec, nil
}

func (c *Controller) runOperation(ctx context.Context, h *OperationHandle) {
	defer c.operationWG.Done()
	defer h.cancel()

	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("internal operation error: %v", recovered)
			}
		}()
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
			return
		}
		switch h.spec.Kind {
		case OperationShell:
			err = c.runShellBlocking(ctx, h.spec.Command)
		case OperationCompact:
			err = c.compactOperationUnderRotation(ctx, h)
		case OperationSummarize:
			err = c.summarizeOperationAtUnderRotation(ctx, h)
		default:
			err = fmt.Errorf("%w: unknown kind %q", ErrInvalidOperation, h.spec.Kind)
		}
	}()

	// Open foreground admission before publishing Done. A consumer reacting to
	// completion can immediately submit the next Turn/Operation without racing
	// the stale handle.
	c.mu.Lock()
	if c.operation == h {
		c.operation = nil
		if h.holdsRotation {
			c.rotating = false
		}
	}
	c.mu.Unlock()

	c.sink.Emit(event.Event{Kind: event.OperationDone, Err: err})
	h.done <- OperationResult{Err: err}
	close(h.done)
}

func (c *Controller) cancelOperation(h *OperationHandle) OperationCancelAttempt {
	c.mu.Lock()
	if c.operation != h {
		c.mu.Unlock()
		return OperationCancelNotActive
	}
	if h.commitStarted {
		c.mu.Unlock()
		return OperationCancelNotActive
	}
	if h.cancelRequested {
		c.mu.Unlock()
		return OperationCancelAlreadyRequested
	}
	h.cancelRequested = true
	cancel := h.cancel
	c.mu.Unlock()
	cancel()
	return OperationCancelRequestedNow
}

// commitOperation is the cancellation/completion linearization point for
// transcript-rewriting Operations. Once it succeeds, Cancel reports not-active
// and the already-computed rewrite is allowed to commit. If cancellation or
// shutdown won first, the Agent returns without replacing the Session.
func (c *Controller) commitOperation(ctx context.Context, h *OperationHandle) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.operation != h || h.cancelRequested {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.commitStarted = true
	return nil
}

// runShellBlocking executes one explicit user command without sandboxing or a
// PTY. The caller owns foreground admission and decides how to surface the
// returned error; Tool events are common to Local RunShell and Remote Operation.
func (c *Controller) runShellBlocking(ctx context.Context, command string) error {
	sh := c.shell
	if sh.Path == "" {
		sh = sandbox.ResolveShell("", "", nil)
	}
	argv, _ := sandbox.Command(sandbox.Spec{}, sh, command)

	preview := []rune(command)
	if len(preview) > 32 {
		preview = preview[:32]
	}
	id := "shell-" + string(preview)
	diagnosticPreview := shellCommandPreview(command)

	c.sink.Emit(event.Event{
		Kind: event.ToolDispatch,
		Tool: event.Tool{
			ID:   id,
			Name: "bash",
			Args: fmt.Sprintf(`{"command":%q}`, command),
		},
	})

	runCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.WaitDelay = shellWaitDelay
	cmd.Dir = c.workspaceRoot
	var buf bytes.Buffer
	w := io.MultiWriter(&buf, &shellWriter{emit: func(chunk string) {
		c.sink.Emit(event.Event{
			Kind: event.ToolProgress,
			Tool: event.Tool{ID: id, Output: chunk},
		})
	}})
	cmd.Stdout = w
	cmd.Stderr = w
	start := time.Now()
	_, err := proc.RunCommand(runCtx, cmd, proc.RunOptions{
		Track:           true,
		CancelWaitGrace: shellWaitDelay + time.Second,
		Source:          "user_shell",
		ShellKind:       sh.Kind.String(),
		ShellPath:       sh.Path,
		CommandPreview:  diagnosticPreview,
	})
	durationMs := time.Since(start).Milliseconds()
	out := buf.String()

	if runCtx.Err() == context.Canceled {
		c.sink.Emit(event.Event{
			Kind: event.ToolResult,
			Tool: event.Tool{ID: id, Name: "bash", Output: out, Err: i18n.M.TurnCancelled, DurationMs: durationMs},
		})
		return context.Canceled
	}
	if runCtx.Err() == context.DeadlineExceeded {
		c.sink.Emit(event.Event{
			Kind: event.ToolResult,
			Tool: event.Tool{ID: id, Name: "bash", Output: out, Err: fmt.Sprintf(i18n.M.ShellExecTimeoutFmt, shellTimeout), DurationMs: durationMs},
		})
		return context.DeadlineExceeded
	}
	if err != nil {
		c.sink.Emit(event.Event{
			Kind: event.ToolResult,
			Tool: event.Tool{ID: id, Name: "bash", Output: out, Err: fmt.Sprintf(i18n.M.ShellExecFailedFmt, err), DurationMs: durationMs},
		})
		return err
	}
	c.sink.Emit(event.Event{
		Kind: event.ToolResult,
		Tool: event.Tool{ID: id, Name: "bash", Output: out, DurationMs: durationMs},
	})
	return nil
}

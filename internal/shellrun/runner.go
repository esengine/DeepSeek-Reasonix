// Package shellrun provides a shared foreground shell runner used by the model
// bash tool and the user !command path. It classifies exits, collects a bounded
// output tail, and keeps combined stdout/stderr model-visible output intact.
package shellrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/tool"
)

// DefaultWaitDelay mirrors the bash tool's child-process wait grace.
const DefaultWaitDelay = 5 * time.Second

var errForegroundTimeout = errors.New("shell foreground timeout")

// Request describes one foreground shell launch. Argv must already include the
// interpreter and any sandbox wrapping; Command is only for diagnostics.
type Request struct {
	Argv              []string
	Dir               string
	Env               []string
	Timeout           time.Duration
	WaitDelay         time.Duration
	CommandPreview    string
	ShellKind         string
	ShellPath         string
	Source            string
	Track             bool
	PreserveWaitDelay bool
	// Progress receives live combined output chunks (optional).
	Progress func(chunk string)
	// Run is optional; tests inject a process runner. When nil, proc.RunCommand.
	Run func(ctx context.Context, cmd *exec.Cmd, opts proc.RunOptions) (*proc.TrackedCommand, error)
}

// Result is the structured outcome of a foreground run.
type Result struct {
	Combined string
	// OutputTail is the bounded tail of combined output, populated only when the
	// run did not complete successfully. Stdout and stderr share one pipe so the
	// model-visible ordering is preserved, which makes a stderr-only tail
	// impossible; in practice the last bytes before a failure are the diagnosis.
	OutputTail   string
	ExitCode     *int
	Started      bool
	State        string
	FailurePhase string
	Err          error
	Tracked      *proc.TrackedCommand
	Cmd          *exec.Cmd
}

// RunForeground starts the process, captures combined stdout/stderr with a
// lock-safe collector, and classifies timeout / cancel / launch / execution
// failures. Combined output is always returned so callers can feed the model.
func RunForeground(ctx context.Context, req Request) Result {
	if len(req.Argv) == 0 {
		return Result{
			State:        tool.ShellStateFailed,
			FailurePhase: tool.ShellPhaseLaunch,
			Err:          fmt.Errorf("empty argv"),
		}
	}
	waitDelay := req.WaitDelay
	if waitDelay <= 0 {
		waitDelay = DefaultWaitDelay
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		runCtx, cancel = context.WithTimeoutCause(ctx, req.Timeout, errForegroundTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, req.Argv[0], req.Argv[1:]...)
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	cmd.WaitDelay = waitDelay

	collector := newOutputCollector(tool.OutputTailMaxBytes)
	var writers []io.Writer
	writers = append(writers, collector.combined, collector.tail)
	if req.Progress != nil {
		writers = append(writers, &progressWriter{emit: req.Progress})
	}
	// Stdout and Stderr must stay the *same* writer value: os/exec then hands the
	// child a single pipe, so the two streams interleave in the order the child
	// wrote them and only one copy goroutine calls Progress. Two MultiWriters
	// would mean two pipes, and combined output would be reordered per stream.
	// The bounded tail therefore covers combined output rather than stderr only;
	// failing commands routinely report on stdout, so the tail stays useful.
	w := io.MultiWriter(writers...)
	cmd.Stdout = w
	cmd.Stderr = w

	run := req.Run
	if run == nil {
		run = proc.RunCommand
	}
	source := req.Source
	if source == "" {
		source = "shellrun"
	}
	tracked, err := run(runCtx, cmd, proc.RunOptions{
		Track:           req.Track,
		CancelWaitGrace: waitDelay + time.Second,
		Source:          source,
		ShellKind:       req.ShellKind,
		ShellPath:       req.ShellPath,
		CommandPreview:  req.CommandPreview,
	})

	out := Result{
		Combined:   collector.combined.String(),
		OutputTail: collector.tailString(),
		Started:    processStarted(cmd, err),
		Tracked:    tracked,
		Cmd:        cmd,
	}

	if req.PreserveWaitDelay && runCtx.Err() == nil && errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}

	// Timeout takes precedence when the tool-local deadline fired.
	if errors.Is(context.Cause(runCtx), errForegroundTimeout) {
		out.State = tool.ShellStateTimedOut
		out.FailurePhase = tool.ShellPhaseTimeout
		out.ExitCode = exitCodeFromErr(err)
		out.Err = fmt.Errorf("command timed out (> %s)", req.Timeout)
		return out
	}
	// Parent cancellation (user stop / session cancel).
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(runCtx.Err(), context.Canceled) || isCanceledWait(err)) {
		out.State = tool.ShellStateCancelled
		out.FailurePhase = tool.ShellPhaseCancellation
		out.ExitCode = exitCodeFromErr(err)
		if cause := context.Cause(runCtx); cause != nil {
			out.Err = cause
		} else {
			out.Err = err
		}
		return out
	}
	if err == nil {
		code := 0
		out.ExitCode = &code
		out.State = tool.ShellStateCompleted
		// The tail exists to explain a failure. Dropping it on success keeps
		// successful runs from persisting up to 16 KiB of ordinary stdout into
		// every session record and tool card.
		out.OutputTail = ""
		return out
	}
	if code := exitCodeFromErr(err); code != nil {
		out.ExitCode = code
		out.Started = true
		out.State = tool.ShellStateFailed
		out.FailurePhase = tool.ShellPhaseExecution
		out.Err = fmt.Errorf("command exited: %w", err)
		return out
	}
	// Process never produced an exit status — launch / dependency style failure.
	out.State = tool.ShellStateFailed
	if out.Started {
		out.FailurePhase = tool.ShellPhaseExecution
	} else {
		out.FailurePhase = tool.ShellPhaseLaunch
	}
	out.Err = err
	return out
}

func processStarted(cmd *exec.Cmd, err error) bool {
	if cmd != nil && cmd.Process != nil {
		return true
	}
	// ExitError means the process ran.
	var ee *exec.ExitError
	return errors.As(err, &ee)
}

func exitCodeFromErr(err error) *int {
	if err == nil {
		code := 0
		return &code
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		return &code
	}
	return nil
}

func isCanceledWait(err error) bool {
	var c proc.CanceledWaitError
	return errors.As(err, &c)
}

// outputCollector owns the combined buffer and a bounded tail ring. Writes stay
// serialized behind one mutex so a caller that does wire two pipes cannot race
// on the Buffer.
type outputCollector struct {
	mu       sync.Mutex
	combined *lockedBuffer
	tail     *tailWriter
}

func newOutputCollector(tailLimit int) *outputCollector {
	c := &outputCollector{}
	c.combined = &lockedBuffer{mu: &c.mu}
	c.tail = &tailWriter{mu: &c.mu, limit: tailLimit}
	return c
}

func (c *outputCollector) tailString() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.tail.buf)
}

// lockedBuffer is a bytes.Buffer guarded by an external mutex so MultiWriter
// concurrent writes from stdout and stderr stay race-free.
type lockedBuffer struct {
	mu  *sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type tailWriter struct {
	mu    *sync.Mutex
	limit int
	buf   []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if w.limit > 0 && len(w.buf) > w.limit {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-w.limit:]...)
	}
	return len(p), nil
}

type progressWriter struct{ emit func(string) }

func (w *progressWriter) Write(p []byte) (int, error) {
	if w.emit != nil && len(p) > 0 {
		w.emit(string(p))
	}
	return len(p), nil
}

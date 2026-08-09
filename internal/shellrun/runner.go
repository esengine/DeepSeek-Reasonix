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
	"strings"
	"sync"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/tool"
)

// DefaultWaitDelay mirrors the bash tool's child-process wait grace.
const DefaultWaitDelay = 5 * time.Second

const (
	// combinedOutputMaxBytes bounds the foreground output retained in memory.
	// Tool-result truncation happens only after the process exits, so it cannot
	// protect the host from a command that prints forever (#6473, #6528).
	combinedOutputMaxBytes = 10 << 20
	// Keep the final diagnostics as well as the command's opening context after
	// the cap is crossed. Build and test failures are commonly printed last.
	combinedOutputTailBytes = 64 << 10
	combinedOutputTruncated = "\n\n...[shell output truncated at 10 MiB; showing the final 64 KiB]...\n\n"
	// Live progress crosses async UI queues and append-only reducers before the
	// final bounded result replaces it. Keep that transient path small too, or a
	// never-ending command can still exhaust memory while Combined stays bounded.
	progressOutputMaxBytes  = 64 << 10
	progressOutputTruncated = "\n\n...[live shell output capped at 64 KiB; final diagnostics will appear when the command exits]...\n\n"
)

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

	collector := newOutputCollector(combinedOutputMaxBytes, tool.OutputTailMaxBytes)
	var writers []io.Writer
	writers = append(writers, collector.combined, collector.tail)
	if req.Progress != nil {
		writers = append(writers, newProgressWriter(req.Progress, progressOutputMaxBytes, progressOutputTruncated))
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
	combined *boundedBuffer
	tail     *tailWriter
}

func newOutputCollector(combinedLimit, tailLimit int) *outputCollector {
	c := &outputCollector{}
	c.combined = &boundedBuffer{
		mu:        &c.mu,
		limit:     combinedLimit,
		tailLimit: combinedOutputTailBytes,
		marker:    combinedOutputTruncated,
	}
	c.tail = &tailWriter{mu: &c.mu, limit: tailLimit}
	return c
}

func (c *outputCollector) tailString() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.tail.buf)
}

// boundedBuffer keeps complete output up to limit. Once output crosses the
// limit it retains a head plus a rolling tail separated by marker. Write always
// reports the full input consumed so a safety cap never changes child-process
// behavior into an artificial short-write failure.
type boundedBuffer struct {
	mu        *sync.Mutex
	buf       bytes.Buffer
	tail      []byte
	limit     int
	tailLimit int
	marker    string
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if !b.truncated && (b.limit <= 0 || b.buf.Len()+len(p) <= b.limit) {
		_, err := b.buf.Write(p)
		return len(p), err
	}
	if !b.truncated {
		b.truncated = true
		headLimit := max(0, b.limit-b.tailLimit-len(b.marker))
		previous := b.buf.Bytes()
		b.tail = appendBoundedTail(b.tail, previous, b.tailLimit)
		if b.buf.Len() > headLimit {
			b.buf.Truncate(headLimit)
		}
	}
	b.tail = appendBoundedTail(b.tail, p, b.tailLimit)
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.truncated {
		return b.buf.String()
	}
	var out strings.Builder
	out.Grow(b.buf.Len() + len(b.marker) + len(b.tail))
	out.Write(b.buf.Bytes())
	out.WriteString(b.marker)
	out.Write(b.tail)
	return out.String()
}

func appendBoundedTail(dst, p []byte, limit int) []byte {
	if limit <= 0 || len(p) >= limit {
		if limit <= 0 {
			return nil
		}
		return append(dst[:0], p[len(p)-limit:]...)
	}
	if overflow := len(dst) + len(p) - limit; overflow > 0 {
		copy(dst, dst[overflow:])
		dst = dst[:len(dst)-overflow]
	}
	return append(dst, p...)
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

type progressWriter struct {
	mu        sync.Mutex
	emit      func(string)
	limit     int
	forwarded int
	marker    string
	truncated bool
}

func newProgressWriter(emit func(string), limit int, marker string) *progressWriter {
	return &progressWriter{emit: emit, limit: max(0, limit), marker: marker}
}

func (w *progressWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.emit == nil || w.truncated {
		return len(p), nil
	}
	remaining := max(0, w.limit-w.forwarded)
	forward := min(len(p), remaining)
	if forward > 0 {
		w.emit(string(p[:forward]))
		w.forwarded += forward
	}
	if forward < len(p) {
		w.truncated = true
		if w.marker != "" {
			w.emit(w.marker)
		}
	}
	return len(p), nil
}

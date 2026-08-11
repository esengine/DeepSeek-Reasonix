package proc

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

const (
	defaultCancelRetryInterval = 500 * time.Millisecond
	defaultCancelRetryFor      = 5 * time.Second
)

// RunOptions controls process-tree tracking for a foreground command.
type RunOptions struct {
	Track               bool
	CancelWaitGrace     time.Duration
	CancelRetryInterval time.Duration
	CancelRetryFor      time.Duration
	Source              string
	ShellKind           string
	ShellPath           string
	CommandPreview      string
}

// RunDiagnostics is local-only state for debugging stuck cancellation.
type RunDiagnostics struct {
	Source                  string
	ShellKind               string
	ShellPath               string
	CommandPreview          string
	RootPID                 int
	Tracked                 bool
	JobObjectCreated        bool
	TreeTrackerStarted      bool
	KillCalls               int
	TreeKillAttempts        int
	RetryKillCalls          int
	CancelWaitGraceExpired  bool
	CancelRetryWindowMillis int64
}

// TrackedCommand owns the cancellation state for one started command.
type TrackedCommand struct {
	cmd *exec.Cmd

	mu     sync.Mutex
	job    uintptr
	tree   *TreeTracker
	killed bool
	diag   RunDiagnostics
}

// RunCommand starts cmd and waits for it. When tracking is enabled, cancellation
// kills the tracked process tree and returns after a bounded wait even if the
// platform wait path remains wedged.
func RunCommand(ctx context.Context, cmd *exec.Cmd, opts RunOptions) (*TrackedCommand, error) {
	opts = normalizeRunOptions(opts)
	if !opts.Track {
		SetCancelKillsTree(cmd)
		return nil, cmd.Run()
	}

	tracked := &TrackedCommand{cmd: cmd}
	tracked.setMetadata(opts)
	HideWindow(cmd)
	cmd.Cancel = func() error {
		tracked.Kill()
		return context.Canceled
	}
	job, err := StartTracked(cmd)
	if err != nil {
		return tracked, err
	}
	tracked.setStarted(job)
	tracked.setTree(TrackTree(cmd))
	return tracked, waitForTrackedCommand(ctx, tracked, cmd.Wait, opts.CancelWaitGrace, opts.CancelRetryInterval, opts.CancelRetryFor)
}

// SetCancelKillsTree configures cmd so context cancellation kills the entire
// process tree instead of only the direct child.
func SetCancelKillsTree(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	HideWindow(cmd)
	SetProcessGroupKill(cmd)
	cmd.Cancel = func() error {
		KillTree(cmd)
		return context.Canceled
	}
}

// CanceledWaitError preserves the underlying wait error while presenting the
// context cancellation as the primary command result.
type CanceledWaitError struct {
	Cause   error
	WaitErr error
}

func (e CanceledWaitError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	if e.WaitErr != nil {
		return e.WaitErr.Error()
	}
	return "command wait canceled"
}

func (e CanceledWaitError) Unwrap() []error {
	if e.Cause != nil && e.WaitErr != nil {
		return []error{e.Cause, e.WaitErr}
	}
	if e.Cause != nil {
		return []error{e.Cause}
	}
	if e.WaitErr != nil {
		return []error{e.WaitErr}
	}
	return nil
}

func normalizeRunOptions(opts RunOptions) RunOptions {
	if opts.CancelWaitGrace <= 0 {
		opts.CancelWaitGrace = time.Second
	}
	if opts.CancelRetryInterval <= 0 {
		opts.CancelRetryInterval = defaultCancelRetryInterval
	}
	if opts.CancelRetryFor <= 0 {
		opts.CancelRetryFor = defaultCancelRetryFor
	}
	return opts
}

func waitForTrackedCommand(ctx context.Context, tracked *TrackedCommand, wait func() error, grace, retryEvery, retryFor time.Duration) error {
	waitCh := make(chan error, 1)
	go func() { waitCh <- wait() }()

	select {
	case err := <-waitCh:
		tracked.StopTracking()
		return err
	case <-ctx.Done():
	}

	tracked.Kill()
	select {
	case err := <-waitCh:
		tracked.StopTracking()
		return CanceledWaitError{Cause: context.Cause(ctx), WaitErr: err}
	case <-time.After(grace):
		tracked.markGraceExpired(retryFor)
		diag := tracked.Diagnostics()
		slog.Warn("proc: command wait still blocked after cancellation",
			"source", diag.Source,
			"shell_kind", diag.ShellKind,
			"shell_path", diag.ShellPath,
			"command_preview", diag.CommandPreview,
			"root_pid", diag.RootPID,
			"tracked", diag.Tracked,
			"job_object_created", diag.JobObjectCreated,
			"tree_tracker_started", diag.TreeTrackerStarted,
			"retry_window_ms", diag.CancelRetryWindowMillis)
		go tracked.retryKillUntilWait(waitCh, retryEvery, retryFor)
		return context.Cause(ctx)
	}
}

// Kill terminates the tracked command tree.
func (p *TrackedCommand) Kill() {
	if p == nil {
		return
	}
	p.mu.Lock()
	firstKill := !p.killed
	p.killed = true
	p.diag.KillCalls++
	job := p.job
	p.job = 0
	tree := p.tree
	p.mu.Unlock()
	if !firstKill {
		job = 0
	}
	KillTracked(p.cmd, job)
	if tree != nil {
		treeKills := tree.Kill()
		tree.Stop()
		p.mu.Lock()
		p.diag.TreeKillAttempts += treeKills
		p.mu.Unlock()
	}
}

// StopTracking stops background tree observation without killing the command.
func (p *TrackedCommand) StopTracking() {
	if p == nil {
		return
	}
	p.mu.Lock()
	tree := p.tree
	p.tree = nil
	p.mu.Unlock()
	if tree != nil {
		tree.Stop()
	}
}

// Diagnostics returns a snapshot of local cancellation state.
func (p *TrackedCommand) Diagnostics() RunDiagnostics {
	if p == nil {
		return RunDiagnostics{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.diag
}

func (p *TrackedCommand) setStarted(job uintptr) {
	if p == nil {
		return
	}
	rootPID := 0
	if p.cmd != nil && p.cmd.Process != nil {
		rootPID = p.cmd.Process.Pid
	}
	p.mu.Lock()
	killed := p.killed
	if !killed {
		p.job = job
	}
	p.diag.RootPID = rootPID
	p.diag.Tracked = true
	p.diag.JobObjectCreated = job != 0
	p.mu.Unlock()
	if killed && job != 0 {
		KillTracked(p.cmd, job)
	}
}

func (p *TrackedCommand) setTree(tree *TreeTracker) {
	if p == nil || tree == nil {
		return
	}
	p.mu.Lock()
	killed := p.killed
	if !killed {
		p.tree = tree
		p.diag.TreeTrackerStarted = true
	}
	p.mu.Unlock()
	if killed {
		tree.Kill()
		tree.Stop()
	}
}

func (p *TrackedCommand) markGraceExpired(retryFor time.Duration) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.diag.CancelWaitGraceExpired = true
	p.diag.CancelRetryWindowMillis = retryFor.Milliseconds()
	p.mu.Unlock()
}

func (p *TrackedCommand) retryKillUntilWait(waitCh <-chan error, interval, max time.Duration) {
	defer p.StopTracking()
	deadline := time.NewTimer(max)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-waitCh:
			return
		case <-ticker.C:
			p.mu.Lock()
			p.diag.RetryKillCalls++
			p.mu.Unlock()
			p.Kill()
		case <-deadline.C:
			diag := p.Diagnostics()
			if diag.CancelWaitGraceExpired {
				slog.Warn("proc: command cleanup retry window ended",
					"source", diag.Source,
					"shell_kind", diag.ShellKind,
					"shell_path", diag.ShellPath,
					"command_preview", diag.CommandPreview,
					"root_pid", diag.RootPID,
					"kill_calls", diag.KillCalls,
					"retry_kill_calls", diag.RetryKillCalls)
			}
			return
		}
	}
}

func (p *TrackedCommand) setMetadata(opts RunOptions) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.diag.Source = opts.Source
	p.diag.ShellKind = opts.ShellKind
	p.diag.ShellPath = opts.ShellPath
	p.diag.CommandPreview = opts.CommandPreview
	p.mu.Unlock()
}

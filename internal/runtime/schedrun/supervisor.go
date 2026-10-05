package schedrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/base/proc"
	"reasonix/internal/contract/observe"
	"reasonix/internal/state/schedule"
)

const (
	// DefaultWallGrace is how long past the run's wall clock the child may take
	// to land on its own before the supervisor kills it.
	DefaultWallGrace = time.Minute
	// tokenHardNum/tokenHardDen make the kill line 1.25 times the run's cap: the
	// child stops itself at the cap, and the kill only meets one that does not.
	tokenHardNum, tokenHardDen = 5, 4

	settleTimeout = 30 * time.Second
	killWait      = 10 * time.Second
	stderrTail    = 4 << 10
)

// Supervisor runs claimed runs. Store and Policy are required.
type Supervisor struct {
	Store  *schedule.Store
	Policy schedule.Policy
	// Command builds the child. Nil runs this binary as `schedule exec <id>`.
	Command func(triggerID string) (*exec.Cmd, error)
	// WallGrace overrides DefaultWallGrace.
	WallGrace time.Duration
	// StartWait overrides StartTimeout, how long the child has to take its lease.
	StartWait time.Duration
}

// Report is how a supervised run ended. State and Code are always set; Killed
// is set when the supervisor, not the child, ended the run.
type Report struct {
	TriggerID  string
	State      schedule.RunState
	Code       string
	Observed   int64
	Clean      bool
	Killed     bool
	StderrTail string
}

// Run executes the claimed run triggerID in a child and settles it. The error is
// nil when the child ran to a result of its own, whatever that result says; it
// wraps a sentinel when the supervisor had to end the run or could not run it.
// schedule.ErrRunSettled means the run was no longer claimed (another executor
// has it, or the reaper took it) and nothing was left running.
func (s *Supervisor) Run(ctx context.Context, triggerID string) (Report, error) {
	rep := Report{TriggerID: triggerID}
	m, _, err := s.Store.Snapshot(ctx)
	if err != nil {
		rep.Code = CodeStore
		return rep, err
	}
	var run *schedule.Run
	for i := range m.Runs {
		if m.Runs[i].TriggerID == triggerID {
			run = &m.Runs[i]
		}
	}
	if run == nil {
		rep.Code = CodeNotFound
		return rep, schedule.ErrRunNotFound
	}
	if run.State != schedule.RunClaimed {
		rep.Code = CodeRunSettled
		return rep, schedule.ErrRunSettled
	}
	var sc *schedule.Schedule
	for i := range m.Schedules {
		if m.Schedules[i].ID == run.ScheduleID {
			sc = &m.Schedules[i]
		}
	}
	if sc == nil {
		rep.Code, rep.State, rep.Clean = CodeNotFound, schedule.RunFailed, true
		return rep, errors.Join(schedule.ErrNotFound, s.settle(triggerID, schedule.Outcome{State: schedule.RunFailed, Clean: true}, nil))
	}

	release, err := s.Store.HoldSupervision(triggerID)
	if err != nil {
		rep.Code = CodeRunHeld
		return rep, err
	}
	defer release()

	perRunCap := run.PerRunCap
	if s.Policy.PerRunTokens > 0 {
		perRunCap = min(perRunCap, s.Policy.PerRunTokens)
	}
	c, err := s.child(triggerID, perRunCap+perRunCap/tokenHardDen*(tokenHardNum-tokenHardDen))
	if err != nil {
		rep.Code, rep.State, rep.Clean = CodeSpawn, schedule.RunFailed, true
		return rep, errors.Join(fmt.Errorf("%w: %w", ErrSpawn, err), s.settle(triggerID, schedule.Outcome{State: schedule.RunFailed, Clean: true}, nil))
	}

	if err := s.awaitReady(ctx, c); err != nil {
		rep.Code = CodeRunHeld
		if errors.Is(err, ErrCancelled) {
			rep.Code = CodeCancelled
		}
		return rep, err
	}
	token, err := s.Store.MarkRunning(ctx, triggerID)
	if err != nil {
		c.kill()
		c.reap(killWait)
		rep.Code = CodeStore
		if errors.Is(err, schedule.ErrRunSettled) {
			rep.Code = CodeRunSettled
		}
		return rep, err
	}
	if _, err := io.WriteString(c.stdin, GoLine+" "+token+"\n"); err != nil {
		c.kill()
		c.reap(killWait)
		rep = s.end(triggerID, rep, endInfo{killed: true, code: CodeSpawn, state: schedule.RunFailed}, c)
		return rep, fmt.Errorf("%w: release the child: %w", ErrSpawn, err)
	}

	wallSec := sc.Budget.PerRunWallSec
	if s.Policy.PerRunWallSeconds > 0 {
		wallSec = min(wallSec, s.Policy.PerRunWallSeconds)
	}
	cause := s.watch(ctx, c, wallSec)
	rep = s.end(triggerID, rep, cause, c)
	return rep, cause.err
}

// awaitReady waits for the child to say it holds the run's lease. A child that
// leaves without saying so was refused the run by another executor; the run is
// left claimed, unsettled, for that executor's owner or the reaper.
func (s *Supervisor) awaitReady(ctx context.Context, c *child) error {
	wait := s.StartWait
	if wait <= 0 {
		wait = StartTimeout
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-c.ready:
		return nil
	case <-c.done:
		c.job.Finish()
		return ErrExecutorRefused
	case <-timer.C:
		c.kill()
		c.reap(killWait)
		c.job.Finish()
		return ErrStartTimeout
	case <-ctx.Done():
		c.kill()
		c.reap(killWait)
		c.job.Finish()
		return ErrCancelled
	}
}

// watch waits for the run to end by itself or be ended, and kills the child when
// it is to be ended.
func (s *Supervisor) watch(ctx context.Context, c *child, wallSec int64) endInfo {
	grace := s.WallGrace
	if grace <= 0 {
		grace = DefaultWallGrace
	}
	deadline := time.NewTimer(time.Duration(wallSec)*time.Second + grace)
	defer deadline.Stop()
	select {
	case <-c.done:
		return endInfo{}
	case <-c.overTokens:
		c.kill()
		return endInfo{killed: true, err: ErrTokenLimit, code: CodeTokenLimit, state: schedule.RunBudgetStopped}
	case <-c.protocol:
		c.kill()
		return endInfo{killed: true, err: ErrProtocol, code: CodeProtocol, state: schedule.RunFailed}
	case <-deadline.C:
		c.kill()
		return endInfo{killed: true, err: ErrWallLimit, code: CodeWallLimit, state: schedule.RunBudgetStopped}
	case <-ctx.Done():
		c.kill()
		return endInfo{killed: true, err: ErrCancelled, code: CodeCancelled, state: schedule.RunInterrupted}
	}
}

type endInfo struct {
	killed bool
	err    error
	code   string
	state  schedule.RunState
}

// end collects the child, works out how it ended and settles the run. Whatever
// the supervisor cannot vouch for is charged at the whole cap by the store.
func (s *Supervisor) end(triggerID string, rep Report, cause endInfo, c *child) Report {
	exited := c.reap(killWait)
	rep.Observed = c.tokens.Load()
	rep.StderrTail = observe.Sanitize(c.stderr.String())
	c.job.Finish()
	rep.Killed = cause.killed
	done := c.result()
	var out schedule.Outcome
	switch {
	case cause.killed:
		rep.State, rep.Code = cause.state, cause.code
		out = schedule.Outcome{State: cause.state, Observed: rep.Observed}
	case done != nil && done.Unmetered:
		rep.State, rep.Code = done.State, CodeUnmetered
		out = schedule.Outcome{State: done.State, Observed: rep.Observed}
	case done != nil && exited && c.exitOK() && c.usages.Load() == done.Usages && rep.Observed == done.Tokens:
		rep.State, rep.Code, rep.Clean = done.State, done.Code, true
		out = schedule.Outcome{State: done.State, Observed: rep.Observed, Clean: true}
	default:
		rep.State, rep.Code = schedule.RunFailed, CodeCrashed
		out = schedule.Outcome{State: schedule.RunFailed, Observed: rep.Observed}
	}
	var res *schedule.Result
	if done != nil && rep.State != schedule.RunInterrupted {
		res = &schedule.Result{Report: done.Report, Pending: done.Pending, Posture: done.Posture, SessionPath: done.SessionPath}
	}
	if err := s.settle(triggerID, out, res); err != nil {
		rep.Code = CodeStore
		if errors.Is(err, schedule.ErrRunSettled) {
			rep.Code = CodeRunSettled
		}
	}
	return rep
}

func (s *Supervisor) settle(triggerID string, o schedule.Outcome, res *schedule.Result) error {
	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()
	var putErr error
	if res != nil {
		putErr = s.Store.PutResult(ctx, triggerID, *res)
		if errors.Is(putErr, schedule.ErrRunSettled) {
			return putErr
		}
	}
	return errors.Join(putErr, s.Store.Finish(ctx, s.Policy, triggerID, o))
}

// Reap settles runs whose executor is gone; the ticker calls it before it
// claims anything.
func (s *Supervisor) Reap(ctx context.Context) ([]string, error) {
	return s.Store.ReapDead(ctx, s.Policy)
}

func (s *Supervisor) child(triggerID string, hardTokens int64) (*child, error) {
	build := s.Command
	if build == nil {
		build = selfCommand
	}
	cmd, err := build(triggerID)
	if err != nil {
		return nil, err
	}
	c := &child{cmd: cmd, hardTokens: hardTokens, done: make(chan struct{}), ready: make(chan struct{}), overTokens: make(chan struct{}, 1), protocol: make(chan struct{}, 1), stderr: &tail{limit: stderrTail}}
	cmd.Stderr = c.stderr
	if c.stdin, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c.stdout = out
	if c.job, err = proc.StartTrackedRequired(cmd); err != nil {
		return nil, err
	}
	go c.pump()
	return c, nil
}

func selfCommand(triggerID string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "schedule", "exec", triggerID)
	cmd.Dir = os.TempDir()
	return cmd, nil
}

// child is one running child process and what the supervisor has heard from it.
type child struct {
	cmd    *exec.Cmd
	job    *proc.TrackedJob
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *tail

	hardTokens int64
	tokens     atomic.Int64
	usages     atomic.Int64

	mu        sync.Mutex
	last      *Done
	waitErr   error
	waited    bool
	done      chan struct{}
	overOnce  sync.Once
	readyOnce sync.Once
	ready     chan struct{}

	overTokens chan struct{}
	protocol   chan struct{}
}

// pump reads the child's stream until it ends, then collects the exit.
func (c *child) pump() {
	err := readLines(c.stdout, func(l Line) {
		switch l.Kind {
		case KindReady:
			c.readyOnce.Do(func() { close(c.ready) })
		case KindUsage:
			c.usages.Add(1)
			total := saturatingAdd(&c.tokens, max(l.Tokens, 0))
			if c.hardTokens > 0 && total > c.hardTokens {
				c.overOnce.Do(func() { c.overTokens <- struct{}{} })
			}
		case KindResult:
			c.mu.Lock()
			c.last = l.Result
			c.mu.Unlock()
		}
	})
	if errors.Is(err, ErrProtocol) {
		select {
		case c.protocol <- struct{}{}:
		default:
		}
		_, _ = io.Copy(io.Discard, c.stdout)
	}
	err = c.cmd.Wait()
	c.mu.Lock()
	c.waitErr, c.waited = err, true
	c.mu.Unlock()
	close(c.done)
}

func saturatingAdd(v *atomic.Int64, n int64) int64 {
	for {
		cur := v.Load()
		next := cur + n
		if next < cur {
			next = 1<<63 - 1
		}
		if v.CompareAndSwap(cur, next) {
			return next
		}
	}
}

func (c *child) kill() { c.job.Kill(c.cmd) }

// reap waits for the child to be gone, a bounded time after a kill, and reports
// whether it was.
func (c *child) reap(limit time.Duration) bool {
	_ = c.stdin.Close()
	select {
	case <-c.done:
		return true
	case <-time.After(limit):
		_ = c.stdout.Close()
		select {
		case <-c.done:
			return true
		case <-time.After(limit):
			return false
		}
	}
}

func (c *child) result() *Done {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *child) exitOK() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waited && c.waitErr == nil
}

// tail keeps the last bytes written to it.
type tail struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		t.buf = t.buf[len(t.buf)-t.limit:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// Package schedulerun is the child half of one supervised scheduled run: the
// supervisor starts a binary as `schedule exec <id>`, and this builds the
// read-only session, drives it to a result and reports it back over the pipe.
// It is a frontend of its own so the Studio host can run a scheduled child
// without carrying the command line.
package schedulerun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"reasonix/internal/frontend/hostenv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/observe"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
	"reasonix/internal/contract/tool"
	"reasonix/internal/frontend/clisession"
	"reasonix/internal/runtime/schedrun"
	"reasonix/internal/runtime/scheduler"
	"reasonix/internal/session/control"
	"reasonix/internal/state/schedule"
)

// Exit statuses of the child. A run that reached a result exits 0 whatever the
// result says; the supervisor reads the state from the result line.
const (
	ExitUsage      = 2
	ExitParentGone = 73
	ExitRunHeld    = 75
	ExitSetup      = 1
)

// Exec runs one claimed run named by args[0] and returns the process exit status.
func Exec(args []string) int {
	// The run's usage is recorded asynchronously; a process that ends after one
	// run drains it before it goes. The command line does the same around Run.
	defer clisession.CloseUsageCatalogs()
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix schedule exec <trigger-id>")
		return ExitUsage
	}
	if !schedrun.SupervisorPipe(os.Stdin) {
		fmt.Fprintln(os.Stderr, schedrun.ErrNoSupervisor)
		return ExitUsage
	}
	return (&scheduleRun{id: args[0], out: schedrun.NewWriter(os.Stdout)}).exec()
}

// scheduleRun is the child half of one supervised run. It reads the claimed run
// from the store, never from its arguments, and takes its policy from the
// user's configuration alone: no file of the workspace is read before the run is
// built, and the build itself reads none.
type scheduleRun struct {
	id     string
	out    *schedrun.Writer
	store  *schedule.Store
	policy schedule.Policy
	sc     schedule.Schedule
	claim  schedule.Run
	gov    *schedrun.Governor
	sink   *reportSink
}

func (r *scheduleRun) exec() int {
	roots := config.RootsForHome("")
	dir := roots.ScheduleDir()
	if dir == "" {
		fmt.Fprintln(os.Stderr, "schedule: the state directory is unknown")
		return ExitSetup
	}
	store, err := schedule.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "schedule:", err)
		return ExitSetup
	}
	r.store = store
	release, err := store.HoldRun(r.id)
	if err != nil {
		fmt.Fprintln(os.Stderr, schedrun.CodeRunHeld, err)
		return ExitRunHeld
	}
	defer release()

	if err := r.out.Ready(); err != nil {
		return ExitParentGone
	}
	token, gone, err := schedrun.AwaitGo(os.Stdin, schedrun.StartTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, schedrun.CodeParentGone, err)
		return ExitParentGone
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	orphan := WatchSupervisor(gone, cancel, schedrun.ForceExitGrace, os.Exit)

	done := r.execute(ctx, roots, token)
	orphaned := orphan.Load()
	if orphaned {
		r.settleOrphan()
		return ExitParentGone
	}
	if err := r.out.Result(done); err != nil {
		fmt.Fprintln(os.Stderr, "schedule: the result could not be delivered:", err)
		return ExitSetup
	}
	return 0
}

// WatchSupervisor cancels the run when the supervisor's stream ends and, if the
// process is still there after grace, ends it: a tool that ignores cancellation
// must not keep an orphan alive. The flag says the supervisor left.
func WatchSupervisor(gone <-chan struct{}, cancel func(), grace time.Duration, exit func(int)) *atomic.Bool {
	var orphan atomic.Bool
	schedrun.ExitWhenGone(gone, grace, ExitParentGone, exit)
	go func() {
		<-gone
		orphan.Store(true)
		cancel()
	}()
	return &orphan
}

// settleOrphan records what a run cost when its supervisor vanished, at the
// whole cap: the extent was not seen by anyone who could stop it.
func (r *scheduleRun) settleOrphan() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	policy := r.policy
	if policy.MaxConsecutiveFails <= 0 {
		policy = schedule.DefaultPolicy()
	}
	var seen int64
	if r.gov != nil {
		seen = r.gov.Tokens()
	}
	_ = r.store.Finish(ctx, policy, r.id, schedule.Outcome{State: schedule.RunInterrupted, Observed: seen})
}

func failed(code string) schedrun.Done {
	return schedrun.Done{State: schedule.RunFailed, Code: code}
}

// load reads the run and its schedule and refuses one that is not exactly what a
// person confirmed and the supervisor released.
func (r *scheduleRun) load(ctx context.Context) (code string) {
	m, _, err := r.store.Snapshot(ctx)
	if err != nil {
		return schedrun.CodeStore
	}
	var run *schedule.Run
	for i := range m.Runs {
		if m.Runs[i].TriggerID == r.id {
			run = &m.Runs[i]
		}
	}
	if run == nil {
		return schedrun.CodeNotFound
	}
	r.claim = *run
	found := false
	for _, sc := range m.Schedules {
		if sc.ID == run.ScheduleID {
			r.sc, found = sc, true
		}
	}
	if !found {
		return schedrun.CodeNotFound
	}
	if r.sc.Confirmed.By != schedule.ConfirmedByHuman || r.sc.Confirmed.Digest != schedule.Digest(r.sc) {
		return schedrun.CodeDigest
	}
	// Trust is the person's to withdraw: a project no longer on the trusted list
	// is not read by a schedule that was confirmed while it was.
	if _, ok := scheduler.Match(scheduler.Folders(""), r.sc.Target.Workspace); !ok {
		return schedrun.CodeWorkspaceUntrust
	}
	return ""
}

func (r *scheduleRun) execute(ctx context.Context, roots config.Roots, token string) schedrun.Done {
	if code := r.load(ctx); code != "" {
		return failed(code)
	}
	if err := r.store.Start(ctx, r.id, token); err != nil {
		switch {
		case errors.Is(err, schedule.ErrRunStarted):
			return failed(schedrun.CodeRunStarted)
		case errors.Is(err, schedule.ErrRunToken):
			return failed(schedrun.CodeRunToken)
		case errors.Is(err, schedule.ErrRunSettled):
			return failed(schedrun.CodeRunSettled)
		}
		fmt.Fprintln(os.Stderr, "schedule:", err)
		return failed(schedrun.CodeStore)
	}
	user, err := schedule.LoadOverrides(roots.UserConfigLoadPath())
	if err == nil {
		r.policy, _, err = schedule.Resolve(user, schedule.Overrides{})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "schedule:", err)
		return failed(schedrun.CodePolicy)
	}
	ws, code := r.workspace()
	if code != "" {
		return failed(code)
	}
	ref := r.sc.Model.Provider + "/" + r.sc.Model.Model
	cfg, err := roots.LoadUserScopeForRoot(ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, "schedule:", err)
		return failed(schedrun.CodeBuild)
	}
	if _, ok := cfg.ResolveModel(ref); !ok {
		return failed(schedrun.CodeModelUnavailable)
	}

	ceiling := min(r.sc.Budget.PerRunTokens, r.policy.PerRunTokens)
	wall := time.Duration(min(r.sc.Budget.PerRunWallSec, r.policy.PerRunWallSeconds)) * time.Second
	steps := min(r.sc.Budget.PerRunSteps, r.policy.PerRunSteps)
	r.gov = schedrun.NewGovernor(ceiling, nil)
	r.sink = &reportSink{gov: r.gov, out: r.out}
	ledger := observe.NewLedger(nil)
	var effort *string
	if e := strings.TrimSpace(r.sc.Model.Effort); e != "" {
		effort = &e
	}
	if err := os.Chdir(ws); err != nil {
		return failed(tool.CodeRootMissing)
	}
	ctrl, err := boot.Build(ctx, boot.Options{
		VerificationClient: hostenv.VerificationClient(),
		Version:            "",
		Model:              ref,
		MaxSteps:           int(steps),
		MaxStepsKey:        "schedule per-run steps",
		RequireKey:         true,
		Sink:               r.sink,
		AgentPreset:        boot.NormalizeAgentPreset(""),
		SessionDir:         clisession.DirFor(ws),
		WorkspaceRoot:      ws,
		EffortOverride:     effort,
		StatsSource:        surface.CLI,
		Stderr:             os.Stderr,
		Observe: &boot.ObserveOptions{
			Pending: r.gov.Sink(ledger, r.policy.MaxRepeatParks),
			Run:     observe.RunContext{ScheduleID: r.sc.ID, TriggerID: r.id, Slot: r.claim.SlotAt, RemainingTokens: ceiling},
			Tools:   r.sc.Grant.Tools,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "schedule:", err)
		return failed(schedrun.CodeBuild)
	}
	defer ctrl.Close()
	r.gov.SetStop(ctrl.Cancel)

	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := clisession.BindRun(ctrl, leases, nil, ""); err != nil {
		fmt.Fprintln(os.Stderr, "schedule:", control.SessionInUseMessage(err))
		return failed(schedrun.CodeBuild)
	}

	runCtx, stopWall := context.WithTimeout(ctx, wall)
	defer stopWall()
	runErr := ctrl.Run(runCtx, r.sc.Prompt)

	posture, _ := ctrl.ObservePosture()
	parked := ledger.List()
	state, why := r.outcome(ctx, runCtx, runErr, ctrl, parked)
	report, _ := schedule.ClipReport(r.sink.report())
	return schedrun.Done{
		State: state, Code: why, Tokens: r.gov.Tokens(), Usages: r.gov.Usages(), Unmetered: r.gov.Unmetered(),
		Report: report, Pending: parked, Posture: posture, SessionPath: ctrl.SessionPath(),
	}
}

// workspace resolves the directory the schedule was confirmed for.
func (r *scheduleRun) workspace() (string, string) {
	ws := filepath.Clean(r.sc.Target.Workspace)
	info, err := os.Stat(ws)
	if err != nil || !info.IsDir() {
		return "", tool.CodeRootMissing
	}
	if r.sc.Grant.ReadOnlyBash {
		return "", schedrun.CodeBuild
	}
	real, err := filepath.EvalSymlinks(ws)
	if err != nil {
		return "", tool.CodeRootMissing
	}
	for _, root := range r.sc.Grant.ReadRoots {
		if rr, err := filepath.EvalSymlinks(root); err != nil || !sameDir(rr, real) {
			return "", schedrun.CodeBuild
		}
	}
	return ws, ""
}

func sameDir(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ai, bi)
}

// outcome names how the run ended from what the host saw, never from what the
// model said: the governor's flags, the context, the controller's own stop and
// the typed error of the turn.
func (r *scheduleRun) outcome(ctx, runCtx context.Context, runErr error, ctrl *control.Controller, parked []observe.Pending) (schedule.RunState, string) {
	switch {
	case r.gov.OverBudget():
		return schedule.RunBudgetStopped, schedrun.CodeTokenLimit
	case r.gov.Unmetered():
		return schedule.RunBudgetStopped, schedrun.CodeUnmetered
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return schedule.RunBudgetStopped, schedrun.CodeWallLimit
	case ctx.Err() != nil:
		return schedule.RunInterrupted, schedrun.CodeCancelled
	case r.gov.RepeatParked():
		return schedule.RunBlocked, schedrun.CodeRepeatParked
	case errors.Is(ctrl.ObserveStopped(), observe.ErrParkLimit):
		return schedule.RunBlocked, schedrun.CodeParkLimit
	case runErr != nil:
		return schedule.RunFailed, classifyRunError(runErr)
	}
	for _, p := range parked {
		if p.Kind == observe.KindAsk {
			return schedule.RunBlocked, ""
		}
	}
	return schedule.RunSucceeded, ""
}

// classifyRunError reads the identity of a provider failure from its type.
func classifyRunError(err error) string {
	var auth *provider.AuthError
	var api *provider.APIError
	switch {
	case errors.As(err, &auth):
		return schedrun.CodeProviderAuth
	case errors.As(err, &api) && (api.Status == 402 || api.Status == 429):
		return schedrun.CodeProviderQuota
	}
	return schedrun.CodeRunError
}

// reportSink keeps the run's final answer and reports each usage to the
// supervisor as it happens, which is what lets the supervisor stop a child that
// will not stop itself.
type reportSink struct {
	gov *schedrun.Governor
	out *schedrun.Writer

	mu    sync.Mutex
	final string
	tail  strings.Builder
}

func (s *reportSink) Emit(e event.Event) {
	switch e.Kind {
	case event.Usage:
		if e.Usage == nil {
			return
		}
		n := int64(e.Usage.PromptTokens) + int64(e.Usage.CompletionTokens)
		s.gov.AddUsage(n, (e.UsageSource == "" || e.UsageSource == "executor") && !e.Usage.Estimated)
		_ = s.out.Usage(max(n, 0))
	case event.StreamAttempt:
		if e.StreamAttempt.Action == event.StreamAttemptCommit {
			s.gov.Committed()
		}
	case event.Text:
		s.mu.Lock()
		s.tail.WriteString(e.Text)
		s.mu.Unlock()
	case event.Message:
		s.mu.Lock()
		if strings.TrimSpace(e.Text) != "" {
			s.final = e.Text
		}
		s.mu.Unlock()
	case event.ToolDispatch, event.TurnStarted:
		s.mu.Lock()
		s.tail.Reset()
		s.mu.Unlock()
	}
}

func (s *reportSink) report() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.final) != "" {
		return s.final
	}
	return s.tail.String()
}

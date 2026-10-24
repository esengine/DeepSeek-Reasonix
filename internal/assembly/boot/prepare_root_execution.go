package boot

import (
	"context"
	"errors"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/executionpresence"
	"reasonix/internal/state/sessionstore"
)

// PreparedRootExecution stages a root replacement without admitting input.
// The host binds session authority and publishes its controller after Adopt.
type PreparedRootExecution struct {
	Result      *BuildResult
	Controller  *control.Controller
	old         *control.Controller
	migration   RuntimeMigration
	presence    *executionpresence.Reservation
	environment *rootenv.Environment
	bound       bool
	adopted     bool
	published   bool
	closed      bool
}

// PrepareRootExecution runs under the source controller's replacement gate.
// Call Close after admission or failure to release the preparation reservation.
func PrepareRootExecution(ctx context.Context, old *control.Controller, opts Options) (*PreparedRootExecution, error) {
	p := &PreparedRootExecution{Controller: old, old: old, migration: CaptureRuntimeMigration(old)}
	mode := old.RootEnvironmentStrategy()
	if old.RootEnvironment() != nil || (mode != "auto" && mode != "isolated") {
		return p, nil
	}
	path := old.SessionPath()
	if path == "" {
		return nil, sessionstore.ErrSessionWriteAuthorityMissing
	}
	_, bound, err := sessionstore.LoadExecutionEnvironment(path)
	if err != nil {
		return nil, err
	}
	p.bound = bound
	if !bound && mode == "auto" && !old.RootEnvironmentSelectionPending() {
		return p, nil
	}
	workingCopy := old.WorkspaceRepo().WorkTree
	if workingCopy == "" {
		workingCopy = old.WorkspaceRoot()
	}
	registry, err := executionpresence.New(workingCopy, config.WorkspaceLeaseDir())
	if err != nil {
		return nil, err
	}
	strategy := executionpresence.Strategy(mode)
	if bound {
		strategy = executionpresence.Isolated
	}
	_, readOnly := old.ObservePosture()
	p.presence, err = registry.Reserve(ctx, executionpresence.Request{Strategy: strategy, Writer: !readOnly || bound, SessionID: sessionstore.BranchID(path)})
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			p.Close()
		}
	}()
	if p.presence.Record().Selected == executionpresence.Shared {
		if err := p.presence.Activate(ctx, workingCopy); err != nil {
			return nil, err
		}
		ok = true
		return p, nil
	}
	if !bound {
		environment, cfg, err := old.PrepareRootEnvironment(ctx)
		p.environment = environment
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || mode == "isolated" {
				return nil, err
			}
			if fallbackErr := p.presence.FallbackShared(ctx, err.Error()); fallbackErr != nil {
				return nil, errors.Join(err, fallbackErr)
			}
			if err := p.presence.Activate(ctx, workingCopy); err != nil {
				return nil, err
			}
			if opts.Sink != nil {
				report(opts.Sink, event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Automatic root isolation could not prepare this execution; shared transactions remain active.", Detail: err.Error()})
			}
			ok = true
			return p, nil
		}
		opts.RootEnvironment, opts.frozenConfig = environment, cfg
		opts.SessionEnvironmentPath = ""
	} else {
		opts.SessionEnvironmentPath = path
	}
	opts.SessionDir = old.SessionDir()
	opts.Jobs, opts.WorkspaceLease = nil, nil
	opts.RuntimeReload = RuntimeReload{Owner: old.RuntimeOwner(), ForceFullRebuild: true}
	opts.deferPublish = true
	p.Result, err = BuildRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	p.Controller = p.Result.Controller
	if err := p.presence.Activate(ctx, p.Controller.WorkspaceRepo().WorkTree); err != nil {
		return nil, err
	}
	ok = true
	return p, nil
}

func (p *PreparedRootExecution) Adopt(ctx context.Context) error {
	if p.closed {
		return rootenv.ErrStale
	}
	if p.Result == nil {
		return nil
	}
	if p.adopted {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.bound {
		if err := p.old.BindPreparedRootEnvironment(ctx, p.environment); err != nil {
			return err
		}
	}
	if err := ApplyRuntimeMigration(p.Controller, p.old, p.migration); err != nil {
		return err
	}
	p.adopted = true
	return nil
}

func (p *PreparedRootExecution) Publish(commit func(*BuildResult) error) error {
	if p.closed {
		return rootenv.ErrStale
	}
	if p.Result == nil {
		return nil
	}
	if !p.adopted || commit == nil {
		return errors.New("root environment must adopt its binding before publication")
	}
	if !p.published {
		if err := commit(p.Result); err != nil {
			return err
		}
		publishBuildResult(p.Result)
		p.published = true
	}
	return nil
}

func (p *PreparedRootExecution) Close() {
	if p.closed {
		return
	}
	p.closed = true
	if p.Result != nil && !p.published {
		p.Result.Controller.ReleaseResources()
	}
	if p.environment != nil {
		_ = p.environment.Close()
	}
	if p.presence != nil {
		_ = p.presence.Close()
	}
}

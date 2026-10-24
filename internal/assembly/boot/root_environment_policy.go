package boot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func (b *builder) rootEnvironmentPolicy() *control.RootEnvironmentPolicy {
	if b.rootEnvironment != nil {
		return &control.RootEnvironmentPolicy{Generation: b.ext.generation, Publish: b.publishRootEnvironment, Recheck: b.recheckRootEnvironment,
			Retry: b.retryRootEnvironment, Results: b.rootEnvironmentResults, Recover: b.recoverRootEnvironment}
	}
	if b.session.mutations == nil {
		return nil
	}
	return &control.RootEnvironmentPolicy{Generation: b.ext.generation, Block: func() error {
		if mode, _ := b.cfg.CollaborationMode(); mode == "auto" {
			if err := automaticIsolationAvailability(b.opts, b.cfg); err != nil {
				return err
			}
		}
		if b.opts.FileOverlay != nil || b.opts.TerminalRunner != nil {
			return errors.New("client file or terminal services cannot move automatically")
		}
		if b.ext.mgr != nil && len(b.ext.mgr.Clients()) > 0 {
			return errors.New("extension processes already belong to this runtime")
		}
		if b.tools.host != nil && len(b.tools.host.Servers()) > 0 {
			return errors.New("configured MCP runtimes cannot move automatically")
		}
		if b.tools.taskTool != nil && b.tools.taskTool.Scheduler().ActiveWriters() > 0 {
			return errors.New("delegated writers are already active")
		}
		return nil
	}, Prepare: func(ctx context.Context, path string) (*rootenv.Environment, *config.Config, error) {
		cfg, err := b.cfg.CloneResolved()
		if err != nil {
			return nil, nil, err
		}
		store, err := rootenv.New(b.roots.DeliveryWorktreeDir(), b.root, b.repo, b.session.mutations)
		if err != nil {
			return nil, nil, err
		}
		store.WithVerificationChecks(b.prompt.projectChecks)
		retained, err := store.ReclaimAbandoned(ctx)
		reportRetainedRootEnvironments(b.sink, retained)
		if err != nil {
			return nil, nil, err
		}
		if b.tools.taskTool != nil {
			store.WithPreparationCapacities(b.tools.taskTool.Scheduler().PhaseCapacity().Preparation)
		}
		denied := appendUniquePaths(RuntimeForbidReadRoots(cfg, b.root), b.opts.inheritedReadDenies...)
		environment, err := store.Prepare(ctx, sessionstore.CanonicalSessionPath(path), cfg.Agent.IsolationCopyPaths, denied)
		return environment, cfg, err
	}}
}

func reportRetainedRootEnvironments(sink event.Sink, retained []rootenv.Retained) {
	if len(retained) == 0 {
		return
	}
	lines := make([]string, 0, len(retained))
	for _, r := range retained {
		lines = append(lines, fmt.Sprintf("%s (%s) at %s: %v", r.ID, r.State, r.WorktreeRoot, r.Err))
	}
	report(sink, event.Event{Level: event.LevelWarn, Text: "Kept abandoned root environments that may still hold work.", Detail: strings.Join(lines, "\n")})
}

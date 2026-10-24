package boot

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func (b *builder) publishRootEnvironment(ctx context.Context, controller *control.Controller) error {
	return b.withRootPublisher(controller, func(p *rootPublisher) error { return p.publish(ctx, controller) })
}

func (b *builder) withRootPublisher(controller *control.Controller, run func(*rootPublisher) error) error {
	if b.session.mutations == nil || controller.RootEnvironment() == nil {
		return nil
	}
	environment, err := controller.RootEnvironment().Retain()
	if err != nil {
		return err
	}
	defer environment.Close()
	return run(&rootPublisher{builder: b, environment: environment})
}

type rootPublisher struct {
	*builder
	environment *rootenv.Environment
	manual      bool
}

func (b *rootPublisher) publish(ctx context.Context, controller *control.Controller) error {
	path := controller.SessionPath()
	ctx = agent.WithParentSession(ctx, sessionstore.BranchID(path))
	if b.tools.taskTool != nil {
		ctx = isolation.WithPreparationCapacities(ctx, b.tools.taskTool.Scheduler().PhaseCapacity().Preparation)
	}
	p, err := b.environment.BeginPublication(ctx, path, b.tools.env.forbidReadRoots)
	if err != nil {
		return fmt.Errorf("root scene retained; export could not be frozen: %w", err)
	}
	if p == nil {
		return nil
	}
	store, parent, spec, err := b.rootPublicationRuntime(ctx, controller)
	if err != nil {
		return err
	}
	if b.manual {
		value := true
		parent.frozenConfig.Agent.AutoIntegrate = &value
	}
	if integrated, err := store.ImportedResultIntegrated(ctx, p.Import()); err == nil && integrated {
		return b.environment.AcknowledgePublication(ctx, path, p.ID, store)
	}
	_, err = store.ImportSnapshotResult(ctx, p.Import())
	if err != nil {
		return fmt.Errorf("root export %s retained: %w", p.ResultID, err)
	}
	if len(b.prompt.projectChecks) == 0 {
		b.rootPublicationNotice(p, "Root export retained without automatic integration: no host-selected verification checks are available.")
		return nil
	}
	valid := false
	entry, err := store.RecheckImportedResult(ctx, p.Import(), func(ctx context.Context, e *isolation.Entry) ([]isolation.CheckEvidence, error) {
		checks, checked, err := b.checkRootExport(ctx, store, parent, spec, e, controller.ToolApprovalMode())
		valid = checked
		return checks, err
	})
	if err != nil {
		return fmt.Errorf("root export %s retained after checks: %w", p.ResultID, err)
	}
	if !valid {
		b.rootPublicationNotice(p, "Root export retained: verification inputs are incomplete or checks did not pass.")
		return nil
	}
	posture := &isolationPosture{}
	posture.bind(controller)
	detail, integrationErr := verifyAndIntegrate(ctx, parent, store, posture, b.environment.Record().SourceWorkspace,
		spec, entry, b.sink, b.rootPublicationAuthorizer(controller, p), b.prompt.projectChecks)
	if detail != "" {
		b.rootPublicationNotice(p, detail)
	}
	if integrated, err := store.ImportedResultIntegrated(ctx, p.Import()); err == nil && integrated {
		integrationErr = errors.Join(integrationErr, b.environment.AcknowledgePublication(ctx, path, p.ID, store))
	}
	return integrationErr
}

func (b *rootPublisher) rootPublicationRuntime(ctx context.Context, controller *control.Controller) (*isolation.Store, Options, delegation.ProfileExecSpec, error) {
	r := b.environment.Record()
	parent, spec := b.opts, delegation.ProfileExecSpec{}
	if b.tools.taskTool == nil {
		return nil, parent, spec, fmt.Errorf("root export requires the existing host check runtime; files retained")
	}
	cfg, err := b.cfg.CloneResolved()
	if err != nil {
		return nil, parent, spec, err
	}
	current, err := b.roots.LoadForRootReadOnly(r.SourceWorkspace)
	if err != nil {
		return nil, parent, spec, err
	}
	cfg.Sandbox.ForbidRead, err = rebindReadPolicy(b.tools.env.forbidReadRoots, b.root, r.SourceWorkspace)
	if err != nil {
		return nil, parent, spec, err
	}
	cfg.Sandbox.ForbidRead = appendUniquePaths(cfg.Sandbox.ForbidRead, RuntimeForbidReadRoots(current, r.SourceWorkspace)...)
	cfg.Tools.Disabled = append(cfg.Tools.Disabled, current.Tools.Disabled...)
	cfg.Permissions.Deny = append(cfg.Permissions.Deny, current.Permissions.Deny...)
	cfg.Sandbox.WorkspaceRoot = r.SourceWorkspace
	coordinator, err := buildMutationCoordinator(ctx, cfg, r.SourceWorkspace, b.sink, r.Source)
	if err != nil {
		return nil, parent, spec, err
	}
	store := isolation.NewStore(filepath.Join(b.roots.DeliveryWorktreeDir(), "isolated"), r.SourceWorkspace, r.Source).
		WithMutationCoordinator(coordinator).WithEnvironmentCopies(cfg.Agent.IsolationCopyPaths, cfg.Sandbox.ForbidRead...)
	parent.frozenConfig, parent.DelegationScheduler, parent.SessionDir = cfg, b.tools.taskTool.Scheduler(), b.session.dir
	parent.WorkspaceRoot, parent.WorkspaceRepo, parent.readPolicyRoot = r.SourceWorkspace, r.Source, r.SourceWorkspace
	parent.inheritedReadDenies = cfg.Sandbox.ForbidRead
	spec.Worker = delegation.WorkerSpec{Kind: "task", Name: "root_publication", Model: b.model.name}
	spec.Grant.WritePaths, err = writeclaim.WholeWorkspaceWriteClaim(r.SourceWorkspace)
	return store, parent, spec, err
}

func (b *builder) rootPublicationNotice(p *rootenv.Publication, text string) {
	report(b.sink, event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text,
		Detail: fmt.Sprintf("root environment %s; result %s; source %s", p.EnvironmentID, p.ResultID, p.From.RepoRoot)})
}

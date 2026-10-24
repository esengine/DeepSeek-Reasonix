package boot

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

func newIsolatedEnvironmentFactory(parent Options, store *isolation.Store, posture *isolationPosture, source string, sink event.Sink, authority func(context.Context, writeclaim.WritePathSet) isolation.SourceAuthorizer, checks ...instruction.VerifyCheck) delegation.EnvironmentFactory {
	frozenChecks := slices.Clone(checks)
	return func(ctx context.Context, spec delegation.ProfileExecSpec, origin *delegation.EnvironmentSource) (prepared *delegation.ExecutionEnvironment, resultErr error) {
		resumeID := ""
		if origin != nil {
			resumeID = origin.ID
		}
		if err := checkAutomaticTaskIsolation(parent, spec.Sched.Environment, resumeID); err != nil {
			return nil, err
		}
		sourceSpec := spec
		var authorize isolation.SourceAuthorizer
		if authority != nil {
			authorize = authority(ctx, sourceSpec.Grant.WritePaths)
		}
		if resumeID == "" && (spec.Context.ContinueFrom != "" || spec.Context.ForkFrom != "") {
			return nil, tool.Refusal{Code: "isolation.unsupported_arg", Message: "isolated continuation requires a retained environment identity"}
		}
		opts, err := unattendedOptions(parent, isolatedUsage{sink}, posture.mode(), source, spec.Worker.Model)
		if err != nil {
			return nil, err
		}
		entry, err := prepareIsolatedEntry(ctx, parent.isolatedParent, store, source, origin, agent.ParentSession(ctx))
		if err != nil {
			return nil, err
		}
		defer func() {
			if p := recover(); p != nil {
				resultErr = fmt.Errorf("prepare isolated environment: %v", p)
			}
			if resultErr != nil {
				_, finishErr := store.Complete(context.WithoutCancel(ctx), entry, isolation.Outcome{}, resultErr)
				resultErr = errors.Join(resultErr, finishErr)
			}
		}()
		releasePreparation, err := store.AdmitRuntimePreparation(ctx, entry.ID)
		if err != nil {
			return nil, err
		}
		defer releasePreparation()
		opts.isolatedParent = &isolatedParent{store: store, entry: entry, source: source, authorize: authorize}
		task, mapped, closeRuntime, err := buildIsolatedCandidate(ctx, parent, opts, source, entry.Cand, spec)
		if err != nil {
			return nil, err
		}
		ledger := evidence.NewLedger()
		verification := &isolatedVerification{store: store, entry: entry, task: task, spec: mapped,
			checks: frozenChecks, ledger: ledger, sink: sink, close: closeRuntime}
		if authorize != nil {
			verification.integrate = func(ctx context.Context) (string, error) {
				return verifyAndIntegrate(ctx, parent, store, posture, source, sourceSpec, entry, sink, authorize, frozenChecks)
			}
		}
		return &delegation.ExecutionEnvironment{ID: entry.ID, Generation: entry.RunGeneration, Task: task, Spec: mapped, Evidence: ledger, Finish: verification.finish}, nil
	}
}

func buildIsolatedCandidate(ctx context.Context, parent, opts Options, source string, candidate worktree.Candidate, spec delegation.ProfileExecSpec) (*delegation.TaskTool, delegation.ProfileExecSpec, func(), error) {
	cfg, err := parent.frozenConfig.CloneResolved()
	if err != nil {
		return nil, spec, nil, err
	}
	cfg.Sandbox.ForbidRead, err = isolatedReadPolicy(cfg, source, candidate.WorkspaceRoot)
	if err != nil {
		return nil, spec, nil, err
	}
	opts.inheritedReadDenies, err = rebindReadPolicy(parent.inheritedReadDenies, source, candidate.WorkspaceRoot)
	if err != nil {
		return nil, spec, nil, err
	}
	opts.readPolicyRoot = candidate.WorkspaceRoot
	cfg.Sandbox.WorkspaceRoot = candidate.WorkspaceRoot
	opts.frozenConfig = cfg
	opts.DelegationScheduler = parent.DelegationScheduler
	opts.WorkspaceRoot, opts.WorkspaceRepo = candidate.WorkspaceRoot, candidate.Repo
	opts.WorkspaceOnly, opts.SandboxBashOverride = true, "enforce"
	opts.protectedMutationRoots = appendUniquePaths(parent.protectedMutationRoots, source, parent.WorkspaceRepo.WorkTree)
	opts.SessionDir = parent.SessionDir
	opts.MaxSteps = spec.Sched.MaxSteps
	opts.EffortOverride = &spec.Worker.Effort
	spec.Grant.WritePaths, err = mapIsolatedWritePaths(spec.Grant.WritePaths, source, candidate.WorkspaceRoot)
	if err != nil {
		return nil, spec, nil, err
	}
	child := newBuilder()
	built, err := child.build(ctx, opts)
	if err != nil {
		return nil, spec, nil, err
	}
	built.Controller.ApplyHeadlessApprovalMode(opts.HeadlessApprovalMode)
	if child.tools.taskTool == nil {
		built.Controller.Close()
		return nil, spec, nil, tool.Refusal{Code: isolation.CodeUnavailable, Message: "isolated runtime cannot execute delegated tasks"}
	}
	return child.tools.taskTool, spec, built.Controller.Close, nil
}

func mapIsolatedWritePaths(grant writeclaim.WritePathSet, source, target string) (writeclaim.WritePathSet, error) {
	if grant.Empty() {
		return grant, nil
	}
	if grant.WholeWorkspace {
		mapped, err := writeclaim.WholeWorkspaceWriteClaim(target)
		mapped.Optimistic = grant.Optimistic
		return mapped, err
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return writeclaim.WritePathSet{}, err
	}
	var paths []string
	for _, path := range grant.Paths {
		rel, err := filepath.Rel(resolved, path)
		if err != nil || !filepath.IsLocal(rel) {
			return writeclaim.WritePathSet{}, tool.Refusal{Code: "isolation.unsupported_arg", Message: "write_paths cannot be mapped into the isolated workspace"}
		}
		paths = append(paths, rel)
	}
	return writeclaim.NormalizeWritePaths(target, paths)
}

package boot

import (
	"fmt"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

func inheritRootEnvironment(old *control.Controller, opts Options) Options {
	if opts.RootEnvironment == nil {
		opts.RootEnvironment = old.RootEnvironment()
	}
	return opts
}

func (b *builder) bindRootVerificationChecks() {
	if b.rootEnvironment == nil {
		return
	}
	b.prompt.projectChecks = nil
	if contract := b.rootEnvironment.Record().CheckContract; contract != nil {
		b.prompt.projectChecks = contract.Checks
	}
}

func (b *builder) bindRootEnvironmentPolicy(cfg *config.Config) error {
	if b.rootEnvironment == nil {
		return nil
	}
	source := b.rootEnvironment.Record().SourceWorkspace
	denied, err := isolatedReadPolicy(cfg, source, b.root)
	if err != nil {
		return err
	}
	inherited, err := rebindReadPolicy(b.opts.inheritedReadDenies, source, b.root)
	if err != nil {
		return err
	}
	b.opts.inheritedReadDenies = appendUniquePaths(denied, inherited...)
	b.opts.readPolicyRoot = b.root
	return nil
}

func (b *builder) restoreRootEnvironment() error {
	var environment *rootenv.Environment
	if borrowed := b.opts.RootEnvironment; borrowed != nil {
		retained, err := borrowed.Retain()
		if err != nil {
			return err
		}
		environment = retained
	} else if path := b.opts.SessionEnvironmentPath; path != "" {
		binding, exists, err := sessionstore.LoadExecutionEnvironment(path)
		if err != nil || !exists {
			return err
		}
		repo, err := gitcmd.Open(b.ctx, binding.SourceWorkspace)
		if err != nil {
			return fmt.Errorf("restore bound root environment: %w", err)
		}
		coordinator, err := mutation.New(binding.SourceWorkspace, config.WorkspaceLeaseDir(), nil)
		if err != nil {
			return err
		}
		store, err := rootenv.New(b.opts.roots().DeliveryWorktreeDir(), binding.SourceWorkspace, repo, coordinator)
		if err != nil {
			return err
		}
		if _, err := store.Resolve(b.ctx, binding); err != nil {
			return err
		}
		environment, err = store.Open(b.ctx, binding.EnvironmentID, binding.SessionPath)
		if err != nil {
			return err
		}
	}
	if environment == nil {
		return nil
	}
	b.rootEnvironment = environment
	if err := environment.Validate(b.ctx); err != nil {
		return err
	}
	r := environment.Record()
	b.opts.WorkspaceRoot = r.Candidate.WorkspaceRoot
	b.opts.WorkspaceRepo = r.Candidate.Repo
	b.opts.WorkspaceRepo.Dir = r.Candidate.WorkspaceRoot
	b.opts.WorkspaceOnly, b.opts.SandboxBashOverride = true, "enforce"
	b.opts.protectedMutationRoots = appendUniquePaths(b.opts.protectedMutationRoots, r.SourceWorkspace, r.Source.WorkTree)
	return nil
}

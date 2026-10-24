package boot

import (
	"context"
	"path/filepath"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

func (b *rootPublisher) rootPublicationAuthorizer(controller *control.Controller, p *rootenv.Publication) isolation.SourceAuthorizer {
	path := controller.SessionPath()
	validate := controller.MutationExecutionValidator(sessionstore.BranchID(path))
	return func(ctx context.Context, paths []string) error {
		if err := validate(ctx); err != nil {
			return err
		}
		if err := b.environment.CheckSession(ctx, path); err != nil {
			return err
		}
		r := b.environment.Record()
		if r.Publication == nil || r.Publication.ID != p.ID || r.Publication.State != "pending" {
			return tool.Refusal{Code: isolation.CodeStaleExecution, Message: "root publication was replaced or acknowledged"}
		}
		if err := r.Source.VerifyDiscovery(ctx); err != nil {
			return err
		}
		cfg, err := b.roots.LoadForRootReadOnly(r.SourceWorkspace)
		if err != nil {
			return err
		}
		currentGate := control.NewSharedHeadlessGate(permission.New(cfg.Permissions.Mode, cfg.Permissions.Allow, cfg.Permissions.Ask, cfg.Permissions.Deny), controller.ToolApprovalMode())
		denied, err := rebindReadPolicy(b.tools.env.forbidReadRoots, b.root, r.SourceWorkspace)
		if err != nil {
			return err
		}
		access := (builtin.Workspace{WriteRoots: []string{r.SourceWorkspace}, ForbidReadRoots: appendUniquePaths(denied, RuntimeForbidReadRoots(cfg, r.SourceWorkspace)...),
			SessionGuard: b.tools.env.sessionGuard, ManagedConfig: b.tools.env.managedConfig}).TargetAccessCheck()
		for _, target := range paths {
			relative, err := filepath.Rel(r.SourceWorkspace, target)
			if err != nil || !filepath.IsLocal(relative) {
				return tool.Refusal{Code: isolation.CodeOwnerMismatch, Message: "root publication exceeds its original workspace grant"}
			}
			if integrationPathDenied(b.tools.gate, b.root, filepath.Join(b.root, relative)) {
				return tool.Refusal{Code: isolation.CodeOwnerMismatch, Message: "current root policy denies publication"}
			}
		}
		if err := checkSourceMutationPaths(ctx, r.SourceWorkspace, b.tools.gate, access, paths); err != nil {
			return err
		}
		return checkSourceMutationPaths(ctx, r.SourceWorkspace, currentGate, access, paths)
	}
}

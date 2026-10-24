package boot

import (
	"context"

	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/session/control"
)

func (b *builder) recheckRootEnvironment(ctx context.Context, controller *control.Controller) error {
	return b.withRootPublisher(controller, func(p *rootPublisher) error { p.manual = true; return p.publish(ctx, controller) })
}

func (b *builder) retryRootEnvironment(ctx context.Context, controller *control.Controller, id string) error {
	return b.withRootPublisher(controller, func(p *rootPublisher) error {
		store, _, _, err := p.rootPublicationRuntime(ctx, controller)
		if err != nil {
			return err
		}
		return p.environment.RetryPublication(ctx, controller.SessionPath(), id, store)
	})
}

func (b *builder) rootEnvironmentResults(ctx context.Context, controller *control.Controller) (out isolation.Catalog, err error) {
	out = isolation.Catalog{Results: []isolation.ResultSummary{}, Diagnostics: []isolation.CatalogDiagnostic{}}
	err = b.withRootPublisher(controller, func(p *rootPublisher) error {
		store, _, _, err := p.rootPublicationRuntime(ctx, controller)
		if err != nil {
			return err
		}
		catalog, err := store.Catalog(ctx)
		if err != nil {
			return err
		}
		out.RecoveryRequired, out.RecoveryReason = catalog.RecoveryRequired, catalog.RecoveryReason
		preview, err := store.PreviewRecovery(ctx)
		if err != nil {
			return err
		}
		if preview != nil {
			out.Recovery = &isolation.RecoveryTarget{Workspace: p.environment.Record().SourceWorkspace, Revision: preview.Revision, Paths: preview.Paths}
		}
		publication := p.environment.Record().Publication
		if publication == nil {
			return nil
		}
		for _, row := range catalog.Results {
			if row.ID != publication.ResultID && row.ResultID != publication.ResultID {
				continue
			}
			row.Actions = []string{}
			row.Kind = "root_" + row.Kind
			out.Results = append(out.Results, row)
		}
		return nil
	})
	return out, err
}

func (b *builder) recoverRootEnvironment(ctx context.Context, controller *control.Controller, revision string) error {
	return b.withRootPublisher(controller, func(p *rootPublisher) error {
		r := p.environment.Record()
		if r.Publication == nil {
			return rootenv.ErrStale
		}
		store, _, _, err := p.rootPublicationRuntime(ctx, controller)
		if err != nil {
			return err
		}
		return store.RecoverExpected(ctx, revision, p.rootPublicationAuthorizer(controller, r.Publication))
	})
}

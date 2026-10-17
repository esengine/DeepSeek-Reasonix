package serve

import (
	"context"
	"errors"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/session/control"
)

type compositionAssembly struct {
	build   func(context.Context, boot.Options) (*boot.BuildResult, error)
	persist func(*control.Controller) error
}

func (s *Server) buildComposition(ctx context.Context, opts boot.Options) (*boot.BuildResult, error) {
	if s.compositionAssembly != nil && s.compositionAssembly.build != nil {
		return s.compositionAssembly.build(ctx, opts)
	}
	return boot.BuildRuntime(ctx, opts)
}

func (s *Server) persistComposition(next *control.Controller) error {
	if s.compositionAssembly != nil && s.compositionAssembly.persist != nil {
		return s.compositionAssembly.persist(next)
	}
	return next.SnapshotRewrite()
}

func (s *Server) bindCompositionSelector(ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	ctrl.ConfigureCompositions(ctrl.PresetStore().Home, func(ctx context.Context, id string) error { return s.switchComposition(ctx, ctrl, id) })
	ctrl.ConfigureCompositionNew(func(ctx context.Context) error { return s.newCompositionSession(ctx, ctrl) })
}

func (s *Server) switchComposition(ctx context.Context, cur *control.Controller, id string) error {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	v, err := cur.PresetStore().Get(id)
	if err != nil {
		return err
	}
	if b := cur.CompositionBinding(); b != nil && b.Version.Revision == v.Revision {
		return nil
	}
	opts := s.rebuildOptions(cur, currentModelRef(cur))
	opts.AgentComposition, opts.Composition = id, nil
	opts.RuntimeReload = boot.RuntimeReload{}
	migration := boot.CaptureRuntimeMigration(cur)
	res, err := s.buildComposition(ctx, opts)
	if err != nil {
		return err
	}
	next := res.Controller
	if err := boot.ApplyRuntimeMigration(next, cur, migration); err != nil {
		next.Close()
		return err
	}
	next.EnableInteractiveApproval()
	next.EnsureSessionPath()
	if err := s.rebindSessionLeaseFor(next.SessionPath(), next); err != nil {
		next.Close()
		return err
	}
	if err := s.persistComposition(next); err != nil {
		return errors.Join(err, s.rollbackComposition(cur, next, true))
	}
	if err := s.publishController(cur, next); err != nil {
		return errors.Join(err, s.rollbackComposition(cur, next, true))
	}
	s.lastBuild = res
	s.bindCompositionSelector(next)
	s.resetSessionView()
	cur.Close()
	return nil
}

func (s *Server) rollbackComposition(cur, next *control.Controller, restore bool) error {
	err := s.rebindSessionLeaseFor(cur.SessionPath(), cur)
	if err == nil && restore && cur.SessionPath() != "" && cur.SessionPath() == next.SessionPath() {
		err = cur.SnapshotRewrite()
	}
	next.ReleaseResources()
	return err
}

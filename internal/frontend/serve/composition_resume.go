package serve

import (
	"context"
	"errors"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func buildCompositionOrHistory(ctx context.Context, opts boot.Options) (*boot.BuildResult, error) {
	result, err := boot.BuildRuntime(ctx, opts)
	if err != nil && opts.Composition != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return boot.BuildCompositionHistory(opts, err), nil
	}
	return result, err
}

func (s *Server) resumeCompositionInto(loaded *sessionstore.Session, path string) error {
	if err := s.prepareResumeComposition(loaded, path); err != nil {
		return err
	}
	return s.ctl().Resume(loaded, path)
}

func (s *Server) prepareResumeComposition(loaded *sessionstore.Session, path string) error {
	cur, ok := s.ctl().(*control.Controller)
	if !ok {
		return nil
	}
	old, target := cur.CompositionBinding(), loaded.Composition()
	if old == nil && target == nil || old != nil && target != nil && old.Digest == target.Digest {
		return nil
	}
	release, err := cur.BeginRuntimeReplacement()
	if err != nil {
		return err
	}
	defer release()
	opts := s.rebuildOptions(cur, currentModelRef(cur))
	opts.Composition, opts.AgentComposition = target, ""
	opts.RuntimeReload = boot.RuntimeReload{}
	result, err := buildCompositionOrHistory(context.Background(), opts)
	if err != nil {
		return err
	}
	next := result.Controller
	if err := next.Resume(loaded, path); err != nil {
		next.Close()
		return err
	}
	next.EnableInteractiveApproval()
	if err := s.rebindSessionLeaseFor(path, next); err != nil {
		next.Close()
		return err
	}
	if err := s.publishController(cur, next); err != nil {
		_ = s.rebindSessionLeaseFor(cur.SessionPath(), cur)
		next.Close()
		return err
	}
	s.lastBuild = result
	cur.Close()
	return nil
}

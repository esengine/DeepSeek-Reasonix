package serve

import (
	"context"
	"errors"
	"net/http"
	"reasonix/internal/assembly/boot"
	"reasonix/internal/session/control"
)

func (s *Server) newSession(w http.ResponseWriter, r *http.Request) {
	if s.ctl().CompositionBinding() == nil {
		s.newLegacySession(w, r)
		return
	}
	if err := s.ctl().NewSession(); err != nil {
		compositionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) newCompositionSession(ctx context.Context, cur *control.Controller) error {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if err := cur.Snapshot(); err != nil {
		return err
	}
	opts := s.rebuildOptions(cur, currentModelRef(cur))
	opts.Composition, opts.AgentComposition = nil, "default"
	opts.SessionTemp, opts.BrowserSession = nil, nil
	opts.RuntimeReload = boot.RuntimeReload{}
	result, err := s.buildComposition(ctx, opts)
	if err != nil {
		return err
	}
	next := result.Controller
	next.EnableInteractiveApproval()
	next.SetToolApprovalMode(cur.ToolApprovalMode())
	next.EnsureSessionPath()
	if err := s.rebindSessionLeaseFor(next.SessionPath(), next); err != nil {
		next.Close()
		return err
	}
	if err := s.persistComposition(next); err != nil {
		return errors.Join(err, s.rollbackComposition(cur, next, false))
	}
	if err := s.publishController(cur, next); err != nil {
		return errors.Join(err, s.rollbackComposition(cur, next, false))
	}
	s.lastBuild = result
	s.resetSessionView()
	cur.Close()
	return nil
}

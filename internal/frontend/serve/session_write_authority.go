package serve

import (
	"errors"
	"log/slog"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/session/control"
)

// SetSessionLeases hands the server the session-lease keeper that guards its
// active session file. Call it before serving; a nil keeper leaves gating off.
func (s *Server) SetSessionLeases(k *control.SessionLeaseKeeper) error {
	s.leases = k
	if ctrl, ok := s.ctl().(*control.Controller); ok {
		ctrl.SetOnSessionRecovered(sessionLeaseRecoveryHandler(k))
		ctrl.SetOnSessionPathChanged(s.followSessionLease)
		if k != nil {
			return k.BindControllerAuthority(ctrl)
		}
	}
	return nil
}

// followSessionLease takes the lease on a path the controller just moved to —
// a no-op where an endpoint already rebound (/resume, /new, /fork). What it
// covers is the path no endpoint minted: the one a controller creates for itself
// on the first turn, which used to stay unowned for the window's life. A refusal
// stands, and a save needing authority is then refused rather than forked.
func (s *Server) followSessionLease(path string) {
	if s == nil || s.leases == nil || strings.TrimSpace(path) == "" {
		return
	}
	if err := s.rebindSessionLease(path); err != nil {
		slog.Warn("serve: session is written by someone else; this pane holds no authority for it",
			"session", filepath.Base(path), "err", err)
	}
}

func sessionLeaseRecoveryHandler(k *control.SessionLeaseKeeper) func(control.SessionRecoveryInfo) error {
	if k == nil {
		return nil
	}
	return k.HandleSessionRecovered
}

// rebindSessionLease moves the server's session lease to path and rebinds the
// write authority generation. A nil keeper gates nothing (tests, embedded use).
func (s *Server) rebindSessionLease(path string) error {
	ctrl, _ := s.ctl().(*control.Controller)
	return s.rebindSessionLeaseFor(path, ctrl)
}

func (s *Server) rebindSessionLeaseFor(path string, ctrl *control.Controller) error {
	if s.leases == nil {
		return nil
	}
	if err := s.leases.Rebind(path); err != nil {
		return err
	}
	if ctrl != nil {
		return s.leases.BindControllerAuthority(ctrl)
	}
	return nil
}

// promoteSessionLease repairs a read-only attachment whose previous holder has
// gone away. A pane can legitimately attach while another in-process pane is
// still leaving the same transcript, and without a later promotion it stays
// read-only with no writer left. Mutating endpoints call this before admission;
// the hub's runtime reconciliation calls it to make the repair visible.
func (s *Server) promoteSessionLease() error {
	if s == nil || s.leases == nil {
		return nil
	}
	ctrl, ok := s.ctl().(*control.Controller)
	if !ok || ctrl == nil {
		return nil
	}
	path := strings.TrimSpace(ctrl.SessionPath())
	if path == "" {
		return nil
	}
	writable, err := s.leases.Attach(path)
	if err != nil {
		return err
	}
	if !writable {
		return sessionstore.ErrSessionLeaseHeld
	}
	return s.leases.BindControllerAuthority(ctrl)
}

func sessionLeaseUnavailable(err error) bool {
	return errors.Is(err, sessionstore.ErrSessionLeaseHeld)
}

// sessionInUseError renders a lease refusal for HTTP clients in the shared CLI
// wording, without the session file path.
func sessionInUseError(err error) string {
	return control.SessionInUseMessage(err) + "; " + control.SessionLeaseCloseHint
}

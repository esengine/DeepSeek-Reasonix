package serve

import "reasonix/internal/control"

// SetSessionLeases hands the server the session-lease keeper that guards its
// active session file. Call it before serving; a nil keeper leaves gating off.
func (s *Server) SetSessionLeases(k *control.SessionLeaseKeeper) error {
	s.leases = k
	if ctrl, ok := s.ctl().(*control.Controller); ok {
		ctrl.SetOnSessionRecovered(sessionLeaseRecoveryHandler(k))
		if k != nil {
			return k.BindControllerAuthority(ctrl)
		}
	}
	return nil
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

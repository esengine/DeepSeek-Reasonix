package serve

import (
	"fmt"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

// SetSessionLeases hands the server the session-lease keeper that guards its
// active session file. Call it before serving; a nil keeper leaves gating off.
func (s *Server) SetSessionLeases(k *control.SessionLeaseKeeper) error {
	s.leases = k
	if k != nil {
		k.SetControllerOwnershipBinder(func(ctrl *control.Controller, owner *control.SessionLeaseKeeper) {
			s.setControllerLeaseOwner(ctrl, owner)
			ctrl.SetOnSessionRecovered(s.sessionRecoveryHandler(ctrl, owner))
			ctrl.SetOnSessionTransition(s.sessionTransitionHandler(ctrl, owner))
		})
	}
	if ctrl, ok := s.ctl().(*control.Controller); ok {
		if k != nil {
			return k.BindControllerAuthority(ctrl)
		}
	}
	return nil
}

func (s *Server) sessionTransitionHandler(ctrl *control.Controller, k *control.SessionLeaseKeeper) func(control.SessionTransitionInfo) error {
	if k == nil && s.tagFor(ctrl) == nil {
		return nil
	}
	return func(info control.SessionTransitionInfo) error {
		if k != nil {
			if err := k.HandleSessionTransition(info); err != nil {
				return err
			}
		}
		path := agent.CanonicalSessionPath(info.TargetPath)
		info.OnCommit(func() {
			if tag := s.tagFor(ctrl); tag != nil {
				tag.PrimePath(path)
			}
			if s.publishControllerPathIfCurrent(ctrl, path) && branchTransitionNeedsRouteEvent(info.Reason) {
				s.announceSessionChanged(path, false)
			}
		})
		return nil
	}
}

func branchTransitionNeedsRouteEvent(reason string) bool {
	switch reason {
	case "fork", "branch", "switch":
		return true
	default:
		return false
	}
}

func (s *Server) sessionRecoveryHandler(ctrl *control.Controller, k *control.SessionLeaseKeeper) func(control.SessionRecoveryInfo) error {
	if k == nil && s.tagFor(ctrl) == nil {
		return nil
	}
	return func(info control.SessionRecoveryInfo) error {
		if k != nil || s.controllerLeaseOwner(ctrl) != nil {
			if err := s.handleControllerSessionRecovered(ctrl, k, info); err != nil {
				return err
			}
		}
		if err := s.moveDetachedRecovery(ctrl, info.RecoveryPath); err != nil {
			return err
		}
		info.OnCommit(func() { s.publishRecoveredControllerRoute(ctrl, info.RecoveryPath) })
		return nil
	}
}

func (s *Server) publishRecoveredControllerRoute(ctrl *control.Controller, path string) {
	if tag := s.tagFor(ctrl); tag != nil {
		tag.PrimePath(path)
	}
	if s.publishControllerPathIfCurrent(ctrl, path) {
		// Recovery changes foreground identity outside the ordinary transition
		// hook. Publish the same must-deliver route barrier so a saturated
		// all-session subscriber cannot keep routing later frames to the old path.
		s.announceSessionChanged(path, false)
	}
}

func (s *Server) setControllerLeaseOwner(ctrl *control.Controller, owner *control.SessionLeaseKeeper) {
	if ctrl == nil {
		return
	}
	s.leaseOwnersMu.Lock()
	if owner == nil {
		delete(s.leaseOwners, ctrl)
	} else {
		if s.leaseOwners == nil {
			s.leaseOwners = map[*control.Controller]*control.SessionLeaseKeeper{}
		}
		s.leaseOwners[ctrl] = owner
	}
	s.leaseOwnersMu.Unlock()
}

func (s *Server) controllerLeaseOwner(ctrl *control.Controller) *control.SessionLeaseKeeper {
	if ctrl == nil {
		return nil
	}
	s.leaseOwnersMu.Lock()
	defer s.leaseOwnersMu.Unlock()
	return s.leaseOwners[ctrl]
}

// handleControllerSessionRecovered resolves ownership at invocation time. A
// controller may have captured its callback immediately before a busy switch
// transfers it to a detached keeper; the controller/keeper identity check
// prevents that stale callback from mutating the now-reused foreground keeper.
func (s *Server) handleControllerSessionRecovered(ctrl *control.Controller, fallback *control.SessionLeaseKeeper, info control.SessionRecoveryInfo) error {
	owner := s.controllerLeaseOwner(ctrl)
	if owner == nil {
		owner = fallback
	}
	for range 3 {
		if owner == nil {
			return nil
		}
		handled, err := owner.HandleSessionRecoveredFor(ctrl, info)
		if handled {
			return err
		}
		next := s.controllerLeaseOwner(ctrl)
		if next == nil || next == owner {
			return fmt.Errorf("bind recovery session: controller ownership changed during handoff")
		}
		owner = next
	}
	return fmt.Errorf("bind recovery session: controller ownership remained unstable")
}

// publishControllerPathIfCurrent keeps the controller identity check and its
// broadcaster route update in the same publication critical section. A
// recovery/transition callback from a just-demoted controller therefore
// cannot overwrite the newly published foreground route.
func (s *Server) publishControllerPathIfCurrent(ctrl *control.Controller, path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctrl != control.SessionAPI(ctrl) {
		return false
	}
	s.bc.SetCurrentSession(path)
	return true
}

// moveDetachedRecovery keeps the registry key aligned when a background
// controller forks to a recovery transcript after an autosave conflict.
func (s *Server) moveDetachedRecovery(ctrl *control.Controller, recoveryPath string) error {
	if ctrl == nil {
		return nil
	}
	recoveryPath = agent.CanonicalSessionPath(recoveryPath)
	if recoveryPath == "" {
		return nil
	}
	s.detachedMu.Lock()
	defer s.detachedMu.Unlock()
	for oldPath, detached := range s.detached {
		if detached.ctrl != control.SessionAPI(ctrl) {
			continue
		}
		if existing := s.detached[recoveryPath]; existing != nil && existing != detached {
			return fmt.Errorf("recovery session is already running in the background")
		}
		delete(s.detached, oldPath)
		detached.path = recoveryPath
		s.detached[recoveryPath] = detached
		return nil
	}
	return nil
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

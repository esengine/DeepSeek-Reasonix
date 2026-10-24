package serve

import (
	"context"
	"errors"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/session/control"
)

func (s *Server) pinSubmissionSession() error {
	ctrl := s.ctl()
	if ensurer, ok := any(ctrl).(interface{ EnsureSessionPath() }); ok {
		before := ctrl.SessionPath()
		ensurer.EnsureSessionPath()
		if after := ctrl.SessionPath(); after != before {
			if err := s.rebindSessionLease(after); err != nil {
				return err
			}
			keepUsedWorkspace(ctrl.WorkspaceRoot())
		}
	}
	return nil
}

// prepareRootExecutionLocked stages and publishes before input captures a runtime.
// The caller holds bindMu through subsequent synchronous input admission.
func (s *Server) prepareRootExecutionLocked(ctx context.Context) (cleanup func(), resultErr error) {
	old, ok := s.ctl().(*control.Controller)
	if !ok || old.RootEnvironment() != nil || old.Running() {
		return func() {}, nil
	}
	mode := old.RootEnvironmentStrategy()
	if mode != "auto" && mode != "isolated" {
		return func() {}, nil
	}
	if mode == "auto" && !old.RootEnvironmentSelectionPending() {
		return func() {}, nil
	}
	release, err := old.BeginRuntimeReplacement()
	if err != nil {
		if mode == "auto" {
			return func() {}, nil
		}
		return nil, err
	}
	defer release()
	prepared, err := boot.PrepareRootExecution(ctx, old, s.rebuildOptions(old, currentModelRef(old)))
	if err != nil {
		return nil, err
	}
	ok = false
	defer func() {
		if !ok {
			prepared.Close()
		}
	}()
	if prepared.Result != nil {
		next := prepared.Controller
		if err := prepared.Adopt(ctx); err != nil {
			return nil, err
		}
		next.EnableInteractiveApproval()
		next.SetOnSessionRecovered(sessionLeaseRecoveryHandler(s.leases))
		next.SetOnSessionPathChanged(s.followSessionLease)
		if err := s.rebindSessionLeaseFor(next.SessionPath(), next); err != nil {
			return nil, err
		}
		defer func() {
			if !ok {
				resultErr = errors.Join(resultErr, s.rebindSessionLeaseFor(old.SessionPath(), old))
			}
		}()
		if err := prepared.Publish(func(result *boot.BuildResult) error {
			if err := s.publishController(old, next); err != nil {
				return err
			}
			s.lastBuild = result
			return nil
		}); err != nil {
			return nil, err
		}
		s.nameWorkspaceHolder(next)
		old.Close()
	}
	ok = true
	return prepared.Close, nil
}

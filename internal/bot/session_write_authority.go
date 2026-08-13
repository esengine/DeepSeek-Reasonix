package bot

import (
	"reasonix/internal/control"
)

func bindBotSessionWriteAuthority(state *sessionState) error {
	if state == nil || state.leases == nil {
		return nil
	}
	ctrl, ok := state.ctrl.(*control.Controller)
	if !ok || ctrl == nil {
		return nil
	}
	return state.leases.BindControllerAuthority(ctrl)
}

func rebindBotSessionWriteAuthority(state *sessionState, path string) error {
	if state == nil || state.leases == nil {
		return nil
	}
	if err := state.leases.Rebind(path); err != nil {
		return err
	}
	return bindBotSessionWriteAuthority(state)
}

func rebindBotControllerWriteAuthority(leases *control.SessionLeaseKeeper, ctrl *control.Controller) error {
	if leases == nil || ctrl == nil {
		return nil
	}
	if err := leases.Rebind(ctrl.SessionPath()); err != nil {
		return err
	}
	return leases.BindControllerAuthority(ctrl)
}

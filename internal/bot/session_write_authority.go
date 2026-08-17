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
	if err := state.leases.BindControllerAuthority(ctrl); err != nil {
		return err
	}
	if state.onSessionTransition != nil {
		ctrl.SetOnSessionTransition(state.onSessionTransition)
	}
	return nil
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

func (gw *BotGateway) botSessionTransitionHandler(key string, msg InboundMessage, state *sessionState) func(control.SessionTransitionInfo) error {
	return func(info control.SessionTransitionInfo) error {
		if state == nil || state.leases == nil {
			return nil
		}
		state.lifecycleMu.Lock()
		defer state.lifecycleMu.Unlock()
		if state.retired {
			return errBotSessionRetired
		}
		if err := state.leases.HandleSessionTransition(info); err != nil {
			return err
		}
		targetPath := canonicalBotPath(info.TargetPath)
		live := false
		gw.mu.Lock()
		if gw.controllers[key] == state {
			live = true
			state.sessionPath = targetPath
			if override, ok := gw.sessionOverrides[key]; ok {
				override.sessionPath = targetPath
				gw.sessionOverrides[key] = override
			}
		}
		gw.mu.Unlock()
		if live {
			gw.rememberSessionPath(msg, targetPath)
		}
		return nil
	}
}

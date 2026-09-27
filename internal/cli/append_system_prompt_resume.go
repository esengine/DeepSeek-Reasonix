package cli

import (
	"errors"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func commitStartupResumeWithStandingInstructions(binding *cliTakeoverBinding, manager *cliTakeoverManager, ctrl *control.Controller,
	resumed *agent.Session, target cliResumeTarget, approve func(error) bool, refresh bool) error {
	if err := commitStartupResume(binding, manager, ctrl, resumed, target, approve); err != nil {
		return err
	}
	if !refresh || target.empty() {
		return nil
	}
	// Resume owns the persisted conversation; this process owns its freshly
	// assembled standing guidance, including any final extension replacement.
	prompt := ctrl.SystemPrompt()
	history := ctrl.History()
	fresh := agent.NewSession(prompt).Snapshot()
	if len(fresh) == 0 {
		return errors.New("cannot restore process-scoped system instructions")
	}
	if len(history) > 0 && history[0].Role == provider.RoleSystem {
		history[0] = fresh[0]
	} else {
		history = append(fresh, history...)
	}
	if ctrl.UsesExclusiveSession() {
		if err := ctrl.AdoptRebuiltModelContext(history); err != nil {
			return err
		}
	} else {
		ctrl.AdoptHistory(history, ctrl.SessionPath())
	}
	if current := ctrl.History(); len(current) == 0 || current[0].Content != prompt {
		return errors.New("cannot restore process-scoped system instructions")
	}
	return ctrl.Snapshot()
}

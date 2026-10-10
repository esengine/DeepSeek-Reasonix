package control

import (
	"strings"

	"reasonix/internal/contract/config"
)

// WebSearchModelSetting separates what the user saved from what search will
// use: Effective is the resolved provider/model (empty when nothing serves) and
// Overridden says the project configuration, not the saved value, decided it.
type WebSearchModelSetting struct {
	Stored     string
	Effective  string
	Reason     config.WebSearchReason
	Overridden bool
}

func (c *Controller) WebSearchModel() (WebSearchModelSetting, error) {
	user, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil {
		return WebSearchModelSetting{}, err
	}
	merged, err := config.LoadForRoot(c.WorkspaceRoot())
	if err != nil {
		return WebSearchModelSetting{}, err
	}
	out := merged.WebSearchOutcome()
	return WebSearchModelSetting{
		Stored:     strings.TrimSpace(user.Agent.WebSearchModel),
		Effective:  out.Ref,
		Reason:     out.Reason,
		Overridden: strings.TrimSpace(merged.Agent.WebSearchModel) != strings.TrimSpace(user.Agent.WebSearchModel),
	}, nil
}

func (c *Controller) SaveWebSearchModel(ref string) error {
	if c.Running() {
		return ErrTurnRunning
	}
	return config.EditConfigFile(config.UserConfigPath(), func(edit *config.Config) error {
		return edit.SetWebSearchModel(ref)
	})
}

package control

import (
	"strings"

	"reasonix/internal/contract/config"
)

type WebSearchModelSetting struct {
	Stored    string
	Effective string
}

func (c *Controller) WebSearchModel() (WebSearchModelSetting, error) {
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil {
		return WebSearchModelSetting{}, err
	}
	effective, err := config.LoadForRoot(c.WorkspaceRoot())
	if err != nil {
		return WebSearchModelSetting{}, err
	}
	return WebSearchModelSetting{Stored: strings.TrimSpace(cfg.Agent.WebSearchModel), Effective: strings.TrimSpace(effective.Agent.WebSearchModel)}, nil
}

func (c *Controller) SaveWebSearchModel(ref string) error {
	if c.Running() {
		return ErrTurnRunning
	}
	cfg, err := config.LoadForRoot(c.WorkspaceRoot())
	if err != nil {
		return err
	}
	if err := cfg.SetWebSearchModel(ref); err != nil {
		return err
	}
	return config.EditConfigFile(config.UserConfigPath(), func(edit *config.Config) error {
		edit.Agent.WebSearchModel = cfg.Agent.WebSearchModel
		return nil
	})
}

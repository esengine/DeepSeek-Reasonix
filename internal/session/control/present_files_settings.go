package control

import "reasonix/internal/contract/config"

type PresentFilesSettings struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}

func (c *Controller) PresentFilesSettings() PresentFilesSettings {
	path := config.UserConfigPath()
	return PresentFilesSettings{Enabled: config.LoadForEdit(path).Tools.PresentFiles, Path: path}
}

// SavePresentFilesSettings saves the user choice without changing the live tool prefix.
func (c *Controller) SavePresentFilesSettings(enabled bool) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	cfg.Tools.PresentFiles = enabled
	return cfg.SaveTo(path)
}

package control

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/settings"
)

// ChangedSettings names the user-file keys that differ from the shipped
// defaults, so a person can see what they have changed from one place.
type ChangedSettings struct {
	Keys []string `json:"keys"`
	Path string   `json:"path"`
}

func (c *Controller) ChangedSettings() ChangedSettings {
	path := config.UserConfigPath()
	keys := settings.Changed(config.LoadForEdit(path), config.Default())
	if keys == nil {
		keys = []string{}
	}
	return ChangedSettings{Keys: keys, Path: path}
}

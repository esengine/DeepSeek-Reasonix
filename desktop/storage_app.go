package main

import (
	"os"

	"reasonix/internal/config"
	"reasonix/internal/pluginpkg"
)

type StorageSettingsView struct {
	DefaultWorkspace string `json:"defaultWorkspace"`
	StatePath        string `json:"statePath"`
	CachePath        string `json:"cachePath"`
	ExtensionsPath   string `json:"extensionsPath"`
}

func (a *App) StorageSettings() StorageSettingsView {
	return StorageSettingsView{
		DefaultWorkspace: storageDefaultWorkspace(),
		StatePath:        config.MemoryUserDir(),
		CachePath:        config.CacheDir(),
		ExtensionsPath:   pluginpkg.PluginsDir(config.ReasonixHomeDir()),
	}
}

func storageDefaultWorkspace() string {
	if workspace := loadWorkspace(); workspace != "" {
		if info, err := os.Stat(workspace); err == nil && info.IsDir() {
			return workspace
		}
	}
	workspace, _ := os.Getwd()
	return workspace
}

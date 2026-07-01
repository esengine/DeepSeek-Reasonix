package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"reasonix/internal/config"
)

// DesktopLayoutState captures UI layout preferences persisted across launches.
type DesktopLayoutState struct {
	WorkspacePanelOpen bool `json:"workspacePanelOpen"`
}

func layoutStatePath() string {
	return filepath.Join(config.MemoryUserDir(), "desktop-layout.json")
}

func loadLayoutState() DesktopLayoutState {
	path := layoutStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return DesktopLayoutState{}
	}
	var s DesktopLayoutState
	if err := json.Unmarshal(data, &s); err != nil {
		return DesktopLayoutState{}
	}
	return s
}

// SaveLayoutState persists layout preferences so the next launch restores them.
func (a *App) SaveLayoutState(state DesktopLayoutState) error {
	path := layoutStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadLayoutState returns the saved layout preferences. Never fails — returns
// zero values when no saved state exists.
func (a *App) LoadLayoutState() DesktopLayoutState {
	return loadLayoutState()
}

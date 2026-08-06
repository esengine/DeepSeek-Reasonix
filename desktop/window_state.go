package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

// DesktopWindowState captures the window geometry to restore across launches.
type DesktopWindowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Maximised bool `json:"maximised"`
}

const (
	// Minimum geometry accepted from the frontend (mirrors Wails MinWidth/MinHeight
	// floor with a slightly looser lower bound so older saved states still restore).
	minWindowWidth  = 400
	minWindowHeight = 300
	// maxWindowDimension rejects corrupt or absurd sizes without relying on live
	// monitor queries during save/shutdown.
	maxWindowDimension = 100_000
	// Windows frameless/bordered windows often report a small negative origin
	// (commonly -8,-8) when docked to the primary display edge. Treat those as
	// legitimate positions rather than "off-screen" corruption.
	minWindowOrigin = -100
	// When a monitor is unplugged the saved origin may sit well outside the
	// remaining virtual desktop. Positions beyond this soft bound are rejected
	// at restore time so the window is re-centered.
	maxWindowOriginAbs = 100_000
)

var (
	windowStateMu        sync.Mutex
	windowStatePersistMu sync.Mutex
	lastKnownWindow      DesktopWindowState
	lastKnownWindowOK    bool
)

func windowStatePath() string {
	return filepath.Join(config.MemoryUserDir(), "desktop-window.json")
}

// loadWindowState reads the saved window geometry. The second return value is
// false when no saved state exists (first launch, missing file, corrupt JSON,
// or out-of-range dimensions). Callers must not restore position when ok is
// false — zero values are not a valid window origin.
func loadWindowState() (DesktopWindowState, bool) {
	path := windowStatePath()
	data, err := readFileUTF8(path)
	if err != nil {
		return DesktopWindowState{}, false
	}
	state, err := parseWindowStateJSON(data)
	if err != nil {
		return DesktopWindowState{}, false
	}
	// Seed the process-local last-known-good so background-hide and shutdown
	// can persist without querying the native window (which can panic when DPI
	// reports 0 during Wails teardown).
	rememberWindowState(state)
	return state, true
}

// parseWindowStateJSON validates a desktop-window.json payload.
func parseWindowStateJSON(data []byte) (DesktopWindowState, error) {
	var s DesktopWindowState
	if err := json.Unmarshal(data, &s); err != nil {
		return DesktopWindowState{}, fmt.Errorf("decode window state: %w", err)
	}
	if err := validateWindowState(s); err != nil {
		return DesktopWindowState{}, err
	}
	return s, nil
}

// validateWindowState rejects sizes/positions that must never be written back.
// x=-8,y=-8 is intentionally valid: Windows border metrics can land there.
func validateWindowState(s DesktopWindowState) error {
	if s.Width < minWindowWidth || s.Width > maxWindowDimension {
		return fmt.Errorf("window width %d out of range [%d, %d]", s.Width, minWindowWidth, maxWindowDimension)
	}
	if s.Height < minWindowHeight || s.Height > maxWindowDimension {
		return fmt.Errorf("window height %d out of range [%d, %d]", s.Height, minWindowHeight, maxWindowDimension)
	}
	if s.X < minWindowOrigin || s.X > maxWindowOriginAbs {
		return fmt.Errorf("window x %d out of range [%d, %d]", s.X, minWindowOrigin, maxWindowOriginAbs)
	}
	if s.Y < minWindowOrigin || s.Y > maxWindowOriginAbs {
		return fmt.Errorf("window y %d out of range [%d, %d]", s.Y, minWindowOrigin, maxWindowOriginAbs)
	}
	return nil
}

// windowPositionRestorable reports whether a saved origin is safe to apply.
// Slightly negative coordinates (Windows border insets) are accepted; large
// off-screen positions force a center fallback.
func windowPositionRestorable(s DesktopWindowState, maxScreenW, maxScreenH int) bool {
	if s.X < minWindowOrigin || s.Y < minWindowOrigin {
		return false
	}
	if maxScreenW > 0 && s.X > maxScreenW*2 {
		return false
	}
	if maxScreenH > 0 && s.Y > maxScreenH*2 {
		return false
	}
	return true
}

func rememberWindowState(s DesktopWindowState) {
	windowStateMu.Lock()
	defer windowStateMu.Unlock()
	lastKnownWindow = s
	lastKnownWindowOK = true
}

func lastKnownWindowState() (DesktopWindowState, bool) {
	windowStateMu.Lock()
	defer windowStateMu.Unlock()
	if !lastKnownWindowOK {
		return DesktopWindowState{}, false
	}
	return lastKnownWindow, true
}

// resetLastKnownWindowStateForTest clears the process-local cache. Tests only.
func resetLastKnownWindowStateForTest() {
	windowStateMu.Lock()
	defer windowStateMu.Unlock()
	lastKnownWindow = DesktopWindowState{}
	lastKnownWindowOK = false
}

// SaveWindowState is the bound method the frontend calls to persist the current
// window geometry before quit and periodically during use. Go never queries the
// native window for geometry; only frontend-reported values are accepted.
func (a *App) SaveWindowState(state DesktopWindowState) error {
	if err := validateWindowState(state); err != nil {
		return err
	}
	windowStatePersistMu.Lock()
	defer windowStatePersistMu.Unlock()
	rememberWindowState(state)
	return writeWindowState(state)
}

func writeWindowState(state DesktopWindowState) error {
	path := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o644)
}

// saveWindowStateSync re-persists the last frontend-reported geometry. It must
// never call WindowGetSize / WindowGetPosition / WindowIsMaximised: during
// Wails shutdown those paths can hit ScaleToDefaultDPI with DPI=0 and panic.
// If no frontend report has landed yet, this is a no-op (first-launch quit).
func (a *App) saveWindowStateSync() {
	windowStatePersistMu.Lock()
	defer windowStatePersistMu.Unlock()
	state, ok := lastKnownWindowState()
	if !ok {
		return
	}
	if err := writeWindowState(state); err != nil {
		// Best-effort: the frontend already wrote this state earlier.
		_ = err
	}
}

// lastKnownMaximised returns the last frontend-reported maximised flag for
// background-hide restore. Falls back to false when nothing was reported.
func (a *App) lastKnownMaximised() bool {
	state, ok := lastKnownWindowState()
	if !ok {
		return false
	}
	return state.Maximised
}

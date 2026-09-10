package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

// DesktopZoomFactor persists the user's zoom factor preference across
// restarts. The frontend writes it; the host RPC Hello handshake reads it to
// set the shell window's zoom factor.
type DesktopZoomFactor struct {
	ZoomFactor float64 `json:"zoomFactor"`
}

func zoomFactorPath() string {
	return filepath.Join(config.MemoryUserDir(), "desktop-zoom.json")
}

func initialDesktopZoomFactor() float64 {
	if zf, ok := loadZoomFactor(); ok && zf > 0 {
		return zf
	}
	return 1.0
}

// loadZoomFactor reads the saved zoom factor. The bool is false when no saved
// value exists (first launch, missing file, corrupt JSON). Callers should fall
// back to 1.0 (no zoom) in that case.
func loadZoomFactor() (float64, bool) {
	path := zoomFactorPath()
	data, err := readFileUTF8(path)
	if err != nil {
		return 0, false
	}
	var zf DesktopZoomFactor
	if err := json.Unmarshal(data, &zf); err != nil {
		return 0, false
	}
	if zf.ZoomFactor < 0.5 || zf.ZoomFactor > 2.0 {
		return 0, false
	}
	return zf.ZoomFactor, true
}

// GetDesktopZoomFactor returns the currently persisted restart zoom factor,
// or 1.0 if none is saved.
func (a *App) GetDesktopZoomFactor() float64 {
	zf, ok := loadZoomFactor()
	if !ok {
		return 1.0
	}
	return zf
}

// SetDesktopZoomFactor persists a zoom factor for the next launch. The value
// is clamped to [0.5, 2.0] (50% – 200%) for safety.
func (a *App) SetDesktopZoomFactor(factor float64) error {
	if factor < 0.5 {
		factor = 0.5
	}
	if factor > 2.0 {
		factor = 2.0
	}
	path := zoomFactorPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(DesktopZoomFactor{ZoomFactor: factor})
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o644)
}

// RestartApplication restarts the whole application so a newly saved zoom
// factor takes effect in the shell window.
func (a *App) RestartApplication() error {
	a.relaunchDesktop(true)
	return nil
}

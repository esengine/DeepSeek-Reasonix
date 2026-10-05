package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
)

// desktopPostureReleasedFile records, beside the user config, that the shipped
// "auto" was released once. It is not config_version: 7 through 12 belong to
// the 1.x line's own upgrades, and a marker it reads would make it skip them.
const desktopPostureReleasedFile = "desktop-posture-released.json"

// releaseShippedDesktopPosture hands a written "auto" back to the derived
// default, once per config. Every save used to write the shipped "auto" out, so
// a file holding it cannot show that anyone chose it; a choice made after this
// ran is kept. Ask, YOLO and values this build does not know stay as written.
func releaseShippedDesktopPosture(path string) (bool, error) {
	if desktopPostureReleased(path) {
		return false, nil
	}
	cfg := LoadForEdit(path)
	changed := false
	if strings.EqualFold(strings.TrimSpace(cfg.Desktop.DefaultToolApprovalMode), "auto") {
		cfg.Desktop.DefaultToolApprovalMode = ""
		if err := cfg.SaveTo(path); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, markDesktopPostureReleased(path)
}

func desktopPostureReleased(path string) bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(path), desktopPostureReleasedFile))
	return err == nil
}

func markDesktopPostureReleased(path string) error {
	dir := filepath.Dir(path)
	if strings.TrimSpace(path) == "" || dir == "" {
		return errors.New("no config directory to record the posture release in")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(map[string]time.Time{"released_at": time.Now().UTC()})
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(dir, desktopPostureReleasedFile), append(data, '\n'), 0o600)
}

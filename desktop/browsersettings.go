// Built-in browser settings persistence. The only user-owned choice today is
// the default open mode for chat links (built-in window vs system browser);
// everything else follows the protocol defaults. Stored beside the other
// desktop JSON prefs so it survives app restarts independently of config.toml.
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

const (
	browserDefaultOpenModeBuiltin = "builtin"
	browserDefaultOpenModeSystem  = "system"

	browserSettingsFileName  = "browser-settings-v1.json"
	browserSettingsFormatV1  = "reasonix.browser.settings.v1"
	browserSettingsVersionV1 = 1
)

var browserSettingsMu sync.Mutex

// browserSettingsFile is the on-disk shape. v1 is frozen: new fields require a
// new format/version. Missing fields fall back to defaults, and a document
// from a newer format version is never overwritten.
type browserSettingsFile struct {
	Format          string `json:"format,omitempty"`
	Version         int    `json:"version,omitempty"`
	DefaultOpenMode string `json:"defaultOpenMode,omitempty"`
	// Future marks a file written by a newer format version. Never
	// serialized; saves refuse to overwrite it.
	Future bool `json:"-"`
}

// browserSettingsPath is a variable so tests can isolate the file location.
var browserSettingsPath = func() string {
	return filepath.Join(config.ReasonixHomeDir(), browserSettingsFileName)
}

// loadBrowserSettings reads the persisted settings. Missing or corrupt files
// yield defaults (built-in browser, per the product default), and a corrupt
// file is repaired by the next save. A well-formed document from a newer
// format version loads as Future, which suppresses every save.
func loadBrowserSettings() browserSettingsFile {
	var s browserSettingsFile
	data, err := os.ReadFile(browserSettingsPath())
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return browserSettingsFile{}
	}
	if s.Format != browserSettingsFormatV1 || s.Version != browserSettingsVersionV1 {
		return browserSettingsFile{Future: true}
	}
	return s
}

func saveBrowserSettings(s browserSettingsFile) error {
	browserSettingsMu.Lock()
	defer browserSettingsMu.Unlock()
	// A document written by a newer format version must never be overwritten
	// by an older version's save.
	if loadBrowserSettings().Future {
		return fmt.Errorf("browser settings were written by a newer format; refusing to overwrite")
	}
	if s.Format == "" {
		s.Format = browserSettingsFormatV1
	}
	if s.Version == 0 {
		s.Version = browserSettingsVersionV1
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(browserSettingsPath()), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(browserSettingsPath(), data, 0o600)
}

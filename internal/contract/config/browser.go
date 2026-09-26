package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// BrowserConfig is the browser the agent drives. Executable, when set, is the
// only browser used; otherwise an installed Chrome, Edge or Chromium is found.
// Whether the tools exist at all is [tools] browser_tools, not this section.
type BrowserConfig struct {
	// SharedEnabled is the 1.x line's own switch in the shared user file; it is
	// carried through a save exactly as read and decides nothing here.
	SharedEnabled *bool  `toml:"enabled"`
	Executable    string `toml:"executable"`
	Headless      bool   `toml:"headless"`
}

// BrowserProfilesDir holds one browser profile per workspace — the logins and
// cookies the agent's browser keeps, apart from the person's own browser.
func BrowserProfilesDir() string {
	home := processRoots().Home()
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, "browser")
}

func renderBrowserConfig(b *strings.Builder, cfg BrowserConfig) {
	b.WriteString("[browser]\n")
	if cfg.SharedEnabled != nil {
		fmt.Fprintf(b, "enabled = %v   # read by the 1.x command line only; see [tools] browser_tools\n", *cfg.SharedEnabled)
	}
	if cfg.Executable != "" {
		fmt.Fprintf(b, "executable = %q   # empty = find Chrome, Edge or Chromium\n", cfg.Executable)
	} else {
		b.WriteString("# executable = \"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome\"\n")
	}
	fmt.Fprintf(b, "headless = %v   # true = no window\n\n", cfg.Headless)
}

// renderChangedBrowserConfig writes [browser] only where it differs from base.
func renderChangedBrowserConfig(b *strings.Builder, cfg, base BrowserConfig) {
	if !sameBrowserConfig(cfg, base) {
		renderBrowserConfig(b, cfg)
	}
}

func sameBrowserConfig(a, b BrowserConfig) bool {
	sameShared := (a.SharedEnabled == nil) == (b.SharedEnabled == nil) &&
		(a.SharedEnabled == nil || *a.SharedEnabled == *b.SharedEnabled)
	return sameShared && a.Executable == b.Executable && a.Headless == b.Headless
}

// BrowserToolsEnabled reports whether the built-in browser tools are bound.
// Unset means on: the browser itself only starts when a tool first uses it.
func (t ToolsConfig) BrowserToolsEnabled() bool {
	return t.BrowserTools == nil || *t.BrowserTools
}

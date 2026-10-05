// desktop_config.go — the desktop-only preference block.
package config

import (
	"fmt"
	"strings"
)

// DesktopClosesToBackground answers the behavioural question, and answers no
// for a config that never said. normalizeCloseBehavior has always defaulted to
// "background" and nothing has ever read it, so honouring that default now
// would hide the window of every user who never asked for it.
func (c *Config) DesktopClosesToBackground() bool {
	for _, set := range []string{c.Desktop.CloseBehavior, c.UI.CloseBehavior} {
		if strings.TrimSpace(set) != "" {
			return normalizeCloseBehavior(set) == "background"
		}
	}
	return false
}

// DesktopEditor is the editor a person named, taken as given. Empty leaves the
// search to whoever does it: naming one here is how a machine with several says
// which, and second-guessing it would make the setting advisory.
func (c *Config) DesktopEditor() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Desktop.Editor)
}

// DesktopTray normalizes the status-icon preference: auto shows one where the
// platform has one, off keeps the taskbar entry as the only surface.
func (c *Config) DesktopTray() string {
	if strings.EqualFold(strings.TrimSpace(c.Desktop.Tray), "off") {
		return "off"
	}
	return "auto"
}

// SetDesktopTray sets whether the desktop shell shows a status icon. UI-only.
func (c *Config) SetDesktopTray(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto", "on":
		c.Desktop.Tray = "auto"
	case "off", "hidden":
		c.Desktop.Tray = "off"
	default:
		return fmt.Errorf("tray %q: must be auto|off", mode)
	}
	return nil
}

// DesktopDefaultToolApprovalMode is the Ask/Auto/YOLO posture the person named
// for new desktop sessions, or "" when they never named one and the session
// follows the sandbox claim and folder trust instead.
func (c *Config) DesktopDefaultToolApprovalMode() string {
	if c == nil || strings.TrimSpace(c.Desktop.DefaultToolApprovalMode) == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(c.Desktop.DefaultToolApprovalMode)) {
	case "workspace-write":
		return ""
	case "danger-full-access":
		return "yolo"
	}
	return NormalizeToolApprovalMode(c.Desktop.DefaultToolApprovalMode)
}

// UnrecognizedDesktopToolApprovalMode is the file's default_tool_approval_mode
// when it names no posture this build knows, so it loads as ask; else "".
func (c *Config) UnrecognizedDesktopToolApprovalMode() string {
	raw := ""
	if c != nil {
		raw = strings.TrimSpace(c.Desktop.DefaultToolApprovalMode)
	}
	switch strings.ToLower(raw) {
	case "", "ask", "workspace-write", "danger-full-access", "read-only":
		return ""
	}
	if NormalizeToolApprovalMode(raw) != "ask" {
		return ""
	}
	return raw
}

// DesktopConfig controls desktop-only UI preferences. It is intentionally
// separate from top-level language and [ui] so desktop choices do not affect CLI
// language, terminal colours, or provider-visible prompt/request data.
type DesktopConfig struct {
	Language   string `toml:"language"`    // auto|en|zh; empty/auto = browser/OS auto-detect
	Currency   string `toml:"currency"`    // legacy display currency; migrated to [billing].display_currency
	Theme      string `toml:"theme"`       // auto|dark|light; empty resolves to auto
	ThemeStyle string `toml:"theme_style"` // graphite|aurora|slate|carbon|nocturne|amber and legacy aliases
	ThemePack  string `toml:"theme_pack"`  // installed pack id; empty is the default appearance
	// Welcomed records that the opening sequence has played. It lives in the
	// config rather than browser storage because clearing a cache is not the
	// same as meeting the app for the first time.
	Welcomed bool `toml:"welcomed"`
	// SurfaceSlots is where the user put an extension's surface, keyed
	// "<pluginId>:<surfaceId>". It outranks the extension's own suggestion.
	SurfaceSlots   map[string]string `toml:"surface_slots"`
	TerminalTheme  string            `toml:"terminal_theme"`  // auto|dark|light; auto follows the desktop app theme
	ExternalOpener string            `toml:"external_opener"` // preferred installed app used by the desktop Open control
	Editor         string            `toml:"editor"`          // command or path to open the workspace in; empty searches the editors this machine has
	CloseBehavior  string            `toml:"close_behavior"`  // quit|background; desktop window close behavior
	// Tray is auto|off. It is not a second close_behavior: with no status
	// icon there is no way back to a hidden window, so off also means the
	// close button quits whatever close_behavior says.
	Tray                    string           `toml:"tray"`
	DisplayMode             string           `toml:"display_mode"`               // standard|compact (legacy "minimal" maps to compact); transcript display mode
	StatusBarStyle          string           `toml:"status_bar_style"`           // icon|text; desktop status bar metric labels
	StatusBarItems          []string         `toml:"status_bar_items"`           // ordered visible desktop status bar items
	DefaultToolApprovalMode string           `toml:"default_tool_approval_mode"` // ask|auto|yolo; empty follows the sandbox claim and folder trust
	CheckUpdates            *bool            `toml:"check_updates"`              // startup update checks; nil keeps the default enabled
	UpdateChannel           string           `toml:"update_channel"`             // legacy: read for compatibility, never written back
	Telemetry               *bool            `toml:"telemetry"`                  // anonymous launch ping, scrubbed native crash diagnostics, per-package market install counts (anonymous, no content); nil = enabled
	Metrics                 *bool            `toml:"metrics"`                    // aggregate desktop metrics (anonymous signal/bucket counts, lifecycle health) and per-package market install counts (anonymous, no content); nil = enabled
	ProviderAccess          []string         `toml:"provider_access"`            // desktop-only list of provider entries shown in Settings > Model > Access
	ExpandThinking          bool             `toml:"expand_thinking"`            // deprecated compatibility alias: true maps to auto
	ReasoningDisplayMode    string           `toml:"reasoning_display_mode"`
	ConversationWidth       string           `toml:"conversation_width"` // standard|full; how far prose runs; empty = standard
	PinnedVersion           string           `toml:"pinned_version"`     // release pinned by a rollback; empty follows the channel
	Appearance              AppearanceConfig `toml:"appearance"`
}

// AppearanceConfig is what the user set for themselves, on top of whichever
// theme pack is active: how large it all is, what it is set in, and their own
// picture. A pack ships a palette; these are the reader's own eyes and desk.
// Zero means "unset" throughout, so an untouched config resolves to the
// stylesheet's own defaults rather than to a number written here.
type AppearanceConfig struct {
	Zoom      float64         `toml:"zoom"`      // whole-interface scale, 0.8..1.6; 0 = 1.0
	ReadSize  float64         `toml:"read_size"` // transcript body size in px; 0 = the stylesheet's
	FontUI    string          `toml:"font_ui"`   // CSS font-family list for the interface
	FontMono  string          `toml:"font_mono"` // CSS font-family list for code and output
	Wallpaper WallpaperConfig `toml:"wallpaper"`
}

// WallpaperConfig is the user's own background image. File is a name inside
// the appearance directory, not a path: the bytes live where the app can find
// them after the picture the user picked has moved or been deleted.
type WallpaperConfig struct {
	File    string  `toml:"file"`
	Opacity float64 `toml:"opacity"` // at rest, 0..1
	Dim     float64 `toml:"dim"`     // scrim of page colour over it, 0..1
	FocusX  float64 `toml:"focus_x"` // 0..1, the point that survives cropping
	FocusY  float64 `toml:"focus_y"` // 0..1
}

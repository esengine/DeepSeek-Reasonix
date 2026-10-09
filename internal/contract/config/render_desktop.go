// render_desktop.go — the [desktop] table.
package config

import (
	"fmt"
	"strings"
)

// renderDesktopSection writes the [desktop] table. It is split out because
// the appearance and behaviour preferences here are the longest run in the
// file and none of them interact with the rest of the render.
func renderDesktopSection(b *strings.Builder, c *Config) {
	b.WriteString("[desktop]\n")
	if lang := c.DesktopLanguage(); lang != "" {
		fmt.Fprintf(b, "language = %q   # desktop UI language; empty/auto = browser/OS auto-detect\n", lang)
	} else {
		b.WriteString("# language = \"zh\"   # desktop UI language; empty/auto = browser/OS auto-detect\n")
	}
	// Legacy desktop.currency is still emitted when set so older binaries keep
	// reading the preference; new writers also own [billing].display_currency.
	if currency := c.DesktopCurrency(); currency != "" {
		fmt.Fprintf(b, "currency = %q   # legacy display currency; prefer [billing].display_currency\n", currency)
	}
	fmt.Fprintf(b, "theme = %q   # desktop only: auto|dark|light\n", c.DesktopTheme())
	fmt.Fprintf(b, "terminal_theme = %q   # integrated terminal: auto|dark|light; auto follows the desktop app\n", c.DesktopTerminalTheme())
	if style := c.DesktopThemeStyle(); style != "" {
		fmt.Fprintf(b, "theme_style = %q   # desktop accent palette\n", style)
	} else {
		b.WriteString("# theme_style = \"graphite\"   # graphite|aurora|slate|carbon|nocturne|amber and legacy aliases\n")
	}
	if len(c.Desktop.SurfaceSlots) > 0 {
		fmt.Fprintf(b, "surface_slots = %s   # where the user put each extension surface; overrides what the extension asked for\n", renderStringMap(c.Desktop.SurfaceSlots))
	}
	if pack := strings.TrimSpace(c.Desktop.ThemePack); pack != "" {
		fmt.Fprintf(b, "theme_pack = %q   # installed theme pack id; empty is the default appearance\n", pack)
	} else {
		b.WriteString("# theme_pack = \"my-theme\"   # a pack under <memory root>/themes; empty is the default appearance\n")
	}
	// Only written once true: an absent key and "not yet welcomed" are the same
	// state, and a fresh config should not carry a line about a sequence that
	// has not happened.
	if c.Desktop.Welcomed {
		b.WriteString("welcomed = true   # the opening sequence has played on this machine\n")
	}
	if opener := c.DesktopExternalOpener(); opener != "" {
		fmt.Fprintf(b, "external_opener = %q   # desktop Open control: installed application id\n", opener)
	} else {
		b.WriteString("# external_opener = \"vscode\"   # desktop Open control: installed application id\n")
	}
	if editor := c.DesktopEditor(); editor != "" {
		fmt.Fprintf(b, "editor = %q   # command or path the workspace opens in; empty searches this machine\n", editor)
	} else {
		b.WriteString("# editor = \"code\"   # command or path the workspace opens in; empty searches this machine\n")
	}
	fmt.Fprintf(b, "close_behavior = %q   # desktop: quit|background when the window close button is clicked\n", c.DesktopCloseBehavior())
	fmt.Fprintf(b, "tray = %q   # desktop: auto|off status icon; with no icon the close button quits whatever the line above says\n", c.DesktopTray())
	fmt.Fprintf(b, "status_bar_style = %q   # desktop: icon|text metric labels in the bottom status bar\n", c.DesktopStatusBarStyle())
	fmt.Fprintf(b, "status_bar_items = %s   # desktop: ordered visible bottom status bar items\n", renderStringArray(c.DesktopStatusBarItems()))
	if mode := c.DesktopDefaultToolApprovalMode(); mode != "" {
		fmt.Fprintf(b, "default_tool_approval_mode = %q   # desktop: Ask/Auto/YOLO default for newly-created sessions\n", mode)
	} else {
		b.WriteString("# default_tool_approval_mode = \"ask\"   # desktop: unset opens in Auto only where the OS sandbox confines writes and the folder is trusted\n")
	}
	fmt.Fprintf(b, "check_updates = %v   # desktop: check for new versions on startup\n", c.DesktopCheckUpdates())
	// Only written while held; an absent key follows the catalog. It has to
	// outlive the install that set it, or the next launch updates the user
	// back off the build they chose.
	if pinned := c.DesktopPinnedVersion(); pinned != "" {
		fmt.Fprintf(b, "pinned_version = %q   # desktop: hold this machine on a release; empty follows the catalog\n", pinned)
	}
	fmt.Fprintf(b, "telemetry = %v   # desktop: anonymous daily ping + scrubbed next-launch native crash diagnostics + per-package market install counts (anonymous, no content); never content\n", c.DesktopTelemetry())
	fmt.Fprintf(b, "metrics = %v   # desktop: aggregate quality/lifecycle metrics (anonymous signal/bucket counts) + per-package market install counts (anonymous, no content); never content\n", c.DesktopMetrics())
	// A non-nil empty slice is intentional: provider_access = [] means the
	// user removed every desktop access entry. Omitting it would make the next
	// load treat the config as legacy and infer access again.
	if c.Desktop.ProviderAccess != nil {
		fmt.Fprintf(b, "provider_access = %s   # desktop settings: providers shown on Settings > Model > Access\n", renderStringArray(c.Desktop.ProviderAccess))
	}
	renderDesktopReasoningDisplayMode(b, c)
	fmt.Fprintf(b, "display_mode = %q   # desktop: standard|compact transcript display mode\n", c.DesktopDisplayMode())
	if width := c.DesktopConversationWidth(); width == "full" {
		fmt.Fprintf(b, "conversation_width = %q   # desktop: standard|full transcript width; empty = standard\n", width)
	}
	renderAppearanceSection(b, c.Desktop.Appearance)
	b.WriteString("\n")

	b.WriteString("[billing]\n")
	if pref := c.DisplayCurrencyPref(); pref != "" {
		fmt.Fprintf(b, "display_currency = %q   # auto|CNY|USD; display only — does not rewrite provider list prices\n", pref)
	} else {
		b.WriteString("# display_currency = \"auto\"   # auto|CNY|USD; display only — does not rewrite provider list prices\n")
	}
	b.WriteString("\n")
}

// renderAppearanceSection writes [desktop.appearance]. Every writer that adds a
// field to the struct has to add it here too: this renderer is hand-written, so
// anything it does not know about is dropped on the next save — the wallpaper
// landed on disk and vanished from the config the moment anything else was
// written.
func renderAppearanceSection(b *strings.Builder, a AppearanceConfig) {
	paper := strings.TrimSpace(a.Wallpaper.File)
	if a.Zoom == 0 && a.ReadSize == 0 && a.FontUI == "" && a.FontMono == "" && paper == "" {
		return
	}
	// The child table implies the parent, so an empty [desktop.appearance] is
	// noise in a file people read.
	if a.Zoom != 0 || a.ReadSize != 0 || a.FontUI != "" || a.FontMono != "" {
		b.WriteString("\n[desktop.appearance]\n")
	}
	if a.Zoom != 0 {
		fmt.Fprintf(b, "zoom = %g   # whole-interface scale, 0.8..1.6\n", a.Zoom)
	}
	if a.ReadSize != 0 {
		fmt.Fprintf(b, "read_size = %g   # transcript body size in px\n", a.ReadSize)
	}
	if a.FontUI != "" {
		fmt.Fprintf(b, "font_ui = %q   # interface font family\n", a.FontUI)
	}
	if a.FontMono != "" {
		fmt.Fprintf(b, "font_mono = %q   # code and output font family\n", a.FontMono)
	}
	if paper == "" {
		return
	}
	b.WriteString("\n[desktop.appearance.wallpaper]\n")
	fmt.Fprintf(b, "file = %q   # a name inside the appearance directory, never a path\n", paper)
	fmt.Fprintf(b, "opacity = %g   # at rest, 0..1\n", a.Wallpaper.Opacity)
	fmt.Fprintf(b, "dim = %g   # scrim of page colour over it, 0..1\n", a.Wallpaper.Dim)
	fmt.Fprintf(b, "focus_x = %g   # 0..1, the point that survives cropping\n", a.Wallpaper.FocusX)
	fmt.Fprintf(b, "focus_y = %g\n", a.Wallpaper.FocusY)
}

package config

import (
	"fmt"
	"strings"
)

func renderDesktop(b *strings.Builder, c *Config) {
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
	fmt.Fprintf(b, "layout_style = %q   # desktop layout: classic|workbench|creation\n", c.DesktopLayoutStyle())
	fmt.Fprintf(b, "theme = %q   # desktop only: auto|dark|light\n", c.DesktopTheme())
	fmt.Fprintf(b, "terminal_theme = %q   # integrated terminal: auto|dark|light; auto follows the desktop app\n", c.DesktopTerminalTheme())
	if style := c.DesktopThemeStyle(); style != "" {
		fmt.Fprintf(b, "theme_style = %q   # desktop accent palette\n", style)
	} else {
		b.WriteString("# theme_style = \"graphite\"   # graphite|aurora|slate|carbon|nocturne|amber and legacy aliases\n")
	}
	if opener := c.DesktopExternalOpener(); opener != "" {
		fmt.Fprintf(b, "external_opener = %q   # desktop Open control: installed application id\n", opener)
	} else {
		b.WriteString("# external_opener = \"vscode\"   # desktop Open control: installed application id\n")
	}
	fmt.Fprintf(b, "close_behavior = %q   # desktop: quit|background when the window close button is clicked\n", c.DesktopCloseBehavior())
	fmt.Fprintf(b, "status_bar_style = %q   # desktop: icon|text metric labels in the bottom status bar\n", c.DesktopStatusBarStyle())
	fmt.Fprintf(b, "status_bar_items = %s   # desktop: ordered visible bottom status bar items\n", renderStringArray(c.DesktopStatusBarItems()))
	fmt.Fprintf(b, "hide_amounts = %v   # desktop: mask balance and costs in status and context displays\n", c.Desktop.HideAmounts)
	fmt.Fprintf(b, "default_tool_approval_mode = %q   # desktop: Ask/Auto/YOLO default for newly-created sessions\n", c.DesktopDefaultToolApprovalMode())
	fmt.Fprintf(b, "check_updates = %v   # desktop: check for new versions on startup\n", c.DesktopCheckUpdates())
	fmt.Fprintf(b, "telemetry = %v   # desktop: anonymous launch ping + scrubbed next-launch native crash diagnostics; never content\n", c.DesktopTelemetry())
	fmt.Fprintf(b, "metrics = %v   # desktop: aggregate quality/lifecycle metrics (anonymous signal/bucket counts); never content\n", c.DesktopMetrics())
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
	b.WriteString("\n")
}

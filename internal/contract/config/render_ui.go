package config

import (
	"fmt"
	"strings"
)

// renderUISection writes the [ui] table, the CLI's presentation-only settings.
func renderUISection(b *strings.Builder, c *Config, scope RenderScope) {
	b.WriteString("[ui]\n")
	fmt.Fprintf(b, "theme = %q   # auto|dark|light; CLI colors only; REASONIX_THEME can override per run\n", c.UITheme())
	if style := c.UIThemeStyle(); style != "" {
		fmt.Fprintf(b, "theme_style = %q   # CLI accent palette; REASONIX_THEME_STYLE can override per run\n", style)
	} else {
		b.WriteString("# theme_style = \"graphite\"   # graphite|aurora|slate|carbon|nocturne|amber and legacy aliases\n")
	}
	if layout := c.UIShortcutLayout(); layout != "classic" {
		fmt.Fprintf(b, "shortcut_layout = %q   # classic|desktop; compatibility setting; Shift+Tab toggles Plan, Ctrl+Y toggles YOLO\n", layout)
	} else {
		b.WriteString("# shortcut_layout = \"desktop\"   # classic|desktop; compatibility setting; Shift+Tab toggles Plan, Ctrl+Y toggles YOLO\n")
	}
	if strings.TrimSpace(c.UI.CursorShape) != "" {
		fmt.Fprintf(b, "cursor_shape = %q   # block|underline|bar; text input cursor shape\n", c.UICursorShape())
	} else {
		b.WriteString("# cursor_shape = \"bar\"   # block|underline|bar; text input cursor shape\n")
	}
	if strings.TrimSpace(c.UI.CloseBehavior) != "" && scope == RenderScopeProject {
		fmt.Fprintf(b, "close_behavior = %q   # legacy desktop close behavior; prefer [desktop].close_behavior in user config\n", c.DesktopCloseBehavior())
	}
	if c.UI.ShowReasoning {
		b.WriteString("show_reasoning = true   # CLI: show thinking text by default; false = collapsed (toggle with Ctrl+O)\n")
	} else {
		b.WriteString("# show_reasoning = true   # CLI: show thinking text by default; false = collapsed (toggle with Ctrl+O)\n")
	}
	fmt.Fprintf(b, "show_turn_usage = %v   # CLI/TUI: show per-request token and cost receipts in the transcript\n", c.UI.ShowTurnUsage)
	b.WriteString("\n")
}

package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/extension/uihub"
	"reasonix/internal/i18n"
)

// Extension structured-UI rendering (Extension Protocol v2). The
// sidecar's publish calls arrive as ExtensionSurface / ExtensionStatus events
// carrying an already-redacted ExtensionSurfacePayload; these helpers map the
// payload onto the transcript idioms the rest of the TUI uses — notice-style
// severity lines for status/notification, and a compaction-style gutter card
// for card/form. Blocking prompts never reach here: the hub routes those
// through the ordinary Ask machinery, which the chooser already renders.

// extensionStatusLine renders one extension status contribution as a
// severity-aware notice line: "[plugin] label: detail". Empty payload → "".
func extensionStatusLine(p *event.ExtensionSurfacePayload) string {
	if p == nil || p.Status == nil {
		return ""
	}
	st := p.Status
	text := "[" + p.PluginID + "] " + st.Label
	if st.Detail != "" {
		text += ": " + st.Detail
	}
	if st.Progress != nil {
		text += fmt.Sprintf(" (%.0f%%)", *st.Progress*100)
	}
	return extensionSeverityLine(st.Severity, text)
}

// extensionNotificationLine renders one extension notification as a
// severity-aware notice line: "[plugin] title — body".
func extensionNotificationLine(p *event.ExtensionSurfacePayload) string {
	if p == nil || p.Notification == nil {
		return ""
	}
	n := p.Notification
	text := "[" + p.PluginID + "] " + n.Title
	if n.Body != "" {
		text += " — " + n.Body
	}
	return extensionSeverityLine(n.Severity, text)
}

// extensionSeverityLine paints a notice line with the glyph and colour of its
// severity, mirroring the event.Notice glyphs (· info, ! warn) plus ✗ error.
func extensionSeverityLine(severity, text string) string {
	switch severity {
	case "warn":
		return themeStyle(activeCLITheme.warn).Render("  ! " + text)
	case "error":
		return themeStyle(activeCLITheme.danger).Render("  ✗ " + text)
	default:
		return dim("  · " + text)
	}
}

// extensionSurfaceLines renders a published card/form surface as a transcript
// card. Unknown kinds and mismatched payloads yield nothing (the event stays
// silent rather than drawing a broken card).
func extensionSurfaceLines(p *event.ExtensionSurfacePayload, width int) []string {
	if p == nil {
		return nil
	}
	switch {
	case p.Card != nil:
		return extensionCardLines(p.PluginID, p.Card, width)
	case p.Form != nil:
		return extensionFormLines(p.PluginID, p.Form)
	}
	return nil
}

// extensionCardLines renders a card surface like the compaction card: an
// accent ◆ header, the body under a dim "  │ " gutter (markdown through the
// same renderer assistant answers use), then key/value rows, progress, and
// action hints. The TUI has no button primitive, so actions render as their
// /<plugin>:<action> slash invocation.
func extensionCardLines(pluginID string, c *event.ExtensionCardView, width int) []string {
	title := c.Title
	if title == "" {
		title = pluginID
	}
	lines := []string{accent("◆ " + title)}
	body := c.Text
	if c.Markdown != "" {
		bodyWidth := max(width-visibleWidth("  │ "), 1)
		if rendered := newMarkdownRenderer(bodyWidth).Render(c.Markdown); rendered != "" {
			body = rendered
		} else {
			body = c.Markdown
		}
	}
	for ln := range strings.SplitSeq(strings.TrimRight(body, "\n"), "\n") {
		if ln == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  │ "+ln)
	}
	for _, f := range c.Fields {
		lines = append(lines, dim("  │ "+f.Key+": "+f.Value))
	}
	if c.Progress != nil {
		lines = append(lines, dim(fmt.Sprintf("  │ %.0f%%", *c.Progress*100)))
	}
	for _, a := range c.Actions {
		hint := fmt.Sprintf(i18n.M.ExtRunActionFmt, uihub.SlashName(pluginID, a.ActionID))
		if a.Label != "" {
			hint = a.Label + " — " + hint
		}
		lines = append(lines, dim("  │ → "+hint))
	}
	return lines
}

// extensionFormLines renders a published form surface as a descriptive card.
// The field values themselves arrive through the Ask machinery (the hub turns
// the form into ordinary question prompts), so the card only announces the
// form — no dialog side is needed here.
func extensionFormLines(pluginID string, f *event.ExtensionFormView) []string {
	title := f.Title
	if title == "" {
		title = pluginID
	}
	lines := []string{accent("◆ " + title)}
	for ln := range strings.SplitSeq(strings.TrimRight(f.Message, "\n"), "\n") {
		if ln == "" {
			continue
		}
		lines = append(lines, dim("  │ "+ln))
	}
	lines = append(lines, dim("  │ "+i18n.M.ExtFormFieldsHint))
	return lines
}

// parseExtensionActionArgs maps the trailing fields of a "/<plugin>:<action>
// args…" line onto the action's string map; the convention lives in the
// control package so every frontend parses identically.
func parseExtensionActionArgs(fields []string) map[string]string {
	return control.ParseExtensionActionArgs(fields)
}

// matchExtensionAction resolves a typed "/<plugin>:<action>" command against
// the declared extension actions, returning the matching view. Anything that
// does not parse as a well-formed action name, or parses but is undeclared,
// is not an extension action — the slash dispatch falls through to
// SlashUnknown exactly as before.
func matchExtensionAction(ctrl control.SessionAPI, typedCmd string) (control.ExtensionActionView, bool) {
	if ctrl == nil {
		return control.ExtensionActionView{}, false
	}
	pluginID, actionID, ok := uihub.ParseSlashName(typedCmd)
	if !ok {
		return control.ExtensionActionView{}, false
	}
	for _, action := range ctrl.ExtensionActions() {
		if action.PluginID == pluginID && action.ActionID == actionID {
			return action, true
		}
	}
	return control.ExtensionActionView{}, false
}

// extensionActionHint marks an extension action in the slash menu the way
// plugin commands are marked: "plugin <id> · <label>".
func extensionActionHint(a control.ExtensionActionView) string {
	source := "plugin " + a.PluginID
	if a.Label == "" {
		return source
	}
	return source + " · " + a.Label
}

package acp

import (
	"strings"

	"reasonix/internal/contract/event"
)

// extensionSurfaceText flattens one extension surface payload to plain text
// for clients without structured-surface support: status →
// "[plugin] label: detail", card → title + body + fields, form → title +
// message, notification → title + body.
func extensionSurfaceText(p *event.ExtensionSurfacePayload) string {
	var b strings.Builder
	write := func(s string) {
		if s == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	switch {
	case p.Status != nil:
		line := "[" + p.PluginID + "] " + p.Status.Label
		if p.Status.Detail != "" {
			line += ": " + p.Status.Detail
		}
		write(line)
	case p.Card != nil:
		write(p.Card.Title)
		body := p.Card.Text
		if p.Card.Markdown != "" {
			body = p.Card.Markdown
		}
		write(body)
		for _, f := range p.Card.Fields {
			write(f.Key + ": " + f.Value)
		}
	case p.Form != nil:
		write(p.Form.Title)
		write(p.Form.Message)
	case p.Notification != nil:
		write(p.Notification.Title)
		write(p.Notification.Body)
	case p.Frame != nil:
		write("[" + p.PluginID + "] " + p.Frame.Title)
		write(p.Frame.Fallback)
	}
	return b.String()
}

// extensionSeverityWarns reports whether the payload carries a warn/error
// severity, which earns the same "[warning] " prefix as event.Notice.
func extensionSeverityWarns(p *event.ExtensionSurfacePayload) bool {
	severity := ""
	if p.Status != nil {
		severity = p.Status.Severity
	}
	if p.Notification != nil {
		severity = p.Notification.Severity
	}
	return severity == "warn" || severity == "error"
}

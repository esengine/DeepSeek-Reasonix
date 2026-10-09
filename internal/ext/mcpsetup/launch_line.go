package mcpsetup

import (
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/base/textutil"
	"reasonix/internal/contract/config"
)

// LaunchLine is what starting e runs or contacts, for a person deciding on it:
// redacted, without env values, and stripped of anything that could repaint a
// terminal or reorder the line.
func LaunchLine(e config.PluginEntry) string {
	clean := textutil.SanitizeLaunch
	line := RedactURL(clean(e.URL))
	if strings.TrimSpace(e.Command) != "" {
		args := make([]string, len(e.Args))
		for i, a := range e.Args {
			args[i] = clean(a)
		}
		parts := append([]string{secrets.RedactConfigValue("", clean(e.Command))}, secrets.RedactArgs(args)...)
		line = strings.Join(parts, " ")
	}
	return clean(line)
}

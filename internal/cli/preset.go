package cli

import (
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agentpreset"
	"reasonix/internal/i18n"
)

// Deprecated /preset, /work-mode, and /profile stay parseable for one version.
// They print a single deprecation notice and never change runtime policy.

var presetDeprecationOnce sync.Once

// noticePresetDeprecated prints the standard-execution notice once per process.
func (m *chatTUI) noticePresetDeprecated() {
	presetDeprecationOnce.Do(func() {
		m.notice(i18n.M.WorkModeDeprecatedNotice)
	})
}

// parseAgentPreset validates a deprecated mode label. The result is only used
// to decide whether the old argument was well-formed; it never changes
// behavior.
func parseAgentPreset(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "light", "economy", "eco", "lite":
		return agentpreset.Light.String(), true
	case "balanced", "full":
		return agentpreset.Balanced.String(), true
	case "delivery":
		return agentpreset.Delivery.String(), true
	default:
		return "", false
	}
}

// runPresetCommand is the no-op compatibility handler for /preset, /work-mode,
// and /profile. Every invocation explains the adaptive standard execution; it
// never switches, rebuilds, or persists anything.
func (m *chatTUI) runPresetCommand(input string) tea.Cmd {
	args := tokenizeArgs(input)
	m.noticePresetDeprecated()
	if len(args) == 2 {
		if _, ok := parseAgentPreset(args[1]); !ok {
			m.notice(i18n.M.WorkModeUsage)
			return nil
		}
	}
	return nil
}

// runWorkModeCommand keeps the legacy dispatch name compiling; /work-mode and
// /profile delegate to the same compatibility handler.
func (m *chatTUI) runWorkModeCommand(input string) tea.Cmd {
	return m.runPresetCommand(input)
}

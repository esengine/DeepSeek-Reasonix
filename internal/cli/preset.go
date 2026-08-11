package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/boot"
	"reasonix/internal/i18n"
)

type presetOption struct {
	name string
	desc string
}

func agentPresetName(preset string) string {
	switch boot.NormalizeAgentPreset(preset) {
	case boot.AgentPresetLight:
		return "light"
	case boot.AgentPresetDelivery:
		return "delivery"
	default:
		return "balanced"
	}
}

func agentPresetDisplay(preset string) string {
	switch boot.NormalizeAgentPreset(preset) {
	case boot.AgentPresetLight:
		return i18n.M.WorkModeEconomyLabel
	case boot.AgentPresetDelivery:
		return i18n.M.WorkModeDeliveryLabel
	default:
		return i18n.M.WorkModeBalancedLabel
	}
}

func parseAgentPreset(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "light", "economy", "eco", "lite":
		return boot.AgentPresetLight, true
	case "balanced", "full":
		return boot.AgentPresetBalanced, true
	case "delivery":
		return boot.AgentPresetDelivery, true
	default:
		return "", false
	}
}

func agentPresetOptions() []presetOption {
	return []presetOption{
		{name: "light", desc: i18n.M.WorkModeEconomyDesc},
		{name: "balanced", desc: i18n.M.WorkModeBalancedDesc},
		{name: "delivery", desc: i18n.M.WorkModeDeliveryDesc},
	}
}

func renderAgentPresets(width int, current string) string {
	currentName := agentPresetName(current)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader(i18n.M.WorkModeListHeaderFmt, agentPresetDisplay(current)))
	for _, option := range agentPresetOptions() {
		status := ""
		if option.name == currentName {
			status = "  " + viewStatus(i18n.M.ArgModelCurrent)
		}
		nameWidth := viewPadWidth(option.name, 10)
		desc := viewCompactText(option.desc, viewBudget(width, 2+nameWidth+1+visibleWidth(status)))
		fmt.Fprintf(&b, "  %-*s %s%s\n", nameWidth, option.name, viewMeta(desc), status)
	}
	b.WriteString(viewHint(i18n.M.WorkModeListHint))
	return strings.TrimRight(b.String(), "\n")
}

// runPresetCommand changes the role setting for the current TUI session without
// rebuilding the controller. Old /work-mode and /profile commands delegate here.
func (m *chatTUI) runPresetCommand(input string) tea.Cmd {
	args := tokenizeArgs(input)
	// Deprecation notice once for legacy command names.
	cmdName := ""
	if len(args) > 0 {
		cmdName = args[0]
	}
	if cmdName == "/work-mode" || cmdName == "/profile" {
		m.notice(i18n.M.WorkModeDeprecatedNotice)
	}
	if len(args) == 1 {
		m.commitLine(renderAgentPresets(m.width, m.runtimeProfile))
		return nil
	}
	if len(args) != 2 {
		m.notice(i18n.M.WorkModeUsage)
		return nil
	}
	target, ok := parseAgentPreset(args[1])
	if !ok {
		m.notice(i18n.M.WorkModeUsage)
		return nil
	}
	if m.ctrl == nil {
		m.notice(i18n.M.WorkModeSwitchUnavailable)
		return nil
	}
	if m.modelSwitchPending {
		m.notice(i18n.M.RuntimeSwitchPending)
		return nil
	}
	if m.runtimeSwitchBusy() {
		m.notice(i18n.M.WorkModeSwitchBusy)
		return nil
	}
	if boot.NormalizeAgentPreset(m.runtimeProfile) == target {
		m.notice(fmt.Sprintf(i18n.M.WorkModeAlreadyOnFmt, agentPresetDisplay(target)))
		return nil
	}
	// In-place switch: no Controller rebuild, no history rewrite.
	m.ctrl.SetAgentPreset(target)
	m.runtimeProfile = boot.TokenModeFromAgentPreset(target)
	m.notice(fmt.Sprintf(i18n.M.WorkModeSwitchedFmt, agentPresetDisplay(target)))
	return nil
}

// Compatibility wrappers keep existing work-mode call sites compiling.
func runtimeProfileDisplay(profile string) string { return agentPresetDisplay(profile) }
func parseWorkMode(value string) (string, bool) {
	preset, ok := parseAgentPreset(value)
	if !ok {
		return "", false
	}
	return boot.TokenModeFromAgentPreset(preset), true
}
func renderWorkModes(width int, current string) string { return renderAgentPresets(width, current) }
func (m *chatTUI) runWorkModeCommand(input string) tea.Cmd {
	return m.runPresetCommand(input)
}

func (m *chatTUI) workModeArgItems(val string) ([]compItem, int, bool) {
	cmdEnd := strings.IndexAny(val, " \t")
	if cmdEnd < 0 {
		return nil, 0, false
	}
	cmd := val[:cmdEnd]
	if cmd != "/work-mode" && cmd != "/profile" && cmd != "/preset" {
		return nil, 0, false
	}
	from := strings.LastIndexAny(val, " \t") + 1
	if len(strings.Fields(val[:from])) != 1 {
		return nil, from, true
	}
	query := strings.ToLower(val[from:])
	current := agentPresetName(m.runtimeProfile)
	var out []compItem
	for _, option := range agentPresetOptions() {
		if query != "" && !strings.HasPrefix(option.name, query) {
			continue
		}
		hint := option.desc
		if option.name == current {
			hint = i18n.M.ArgModelCurrent + " · " + hint
		}
		out = append(out, compItem{label: option.name, insert: option.name, hint: hint})
	}
	return out, from, true
}

package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/boot"
	"reasonix/internal/i18n"
)

type workModeOption struct {
	name string
	desc string
}

func runtimeProfileName(profile string) string {
	switch boot.NormalizeTokenMode(profile) {
	case boot.TokenModeEconomy:
		return "economy"
	case boot.TokenModeDelivery:
		return "delivery"
	default:
		return "balanced"
	}
}

func runtimeProfileDisplay(profile string) string {
	switch boot.NormalizeTokenMode(profile) {
	case boot.TokenModeEconomy:
		return i18n.M.WorkModeEconomyLabel
	case boot.TokenModeDelivery:
		return i18n.M.WorkModeDeliveryLabel
	default:
		return i18n.M.WorkModeBalancedLabel
	}
}

func parseWorkMode(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "economy":
		return boot.TokenModeEconomy, true
	case "balanced", boot.TokenModeFull:
		return boot.TokenModeFull, true
	case "delivery":
		return boot.TokenModeDelivery, true
	default:
		return "", false
	}
}

func workModeOptions() []workModeOption {
	return []workModeOption{
		{name: "economy", desc: i18n.M.WorkModeEconomyDesc},
		{name: "balanced", desc: i18n.M.WorkModeBalancedDesc},
		{name: "delivery", desc: i18n.M.WorkModeDeliveryDesc},
	}
}

func renderWorkModes(width int, current string) string {
	currentName := runtimeProfileName(current)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader(i18n.M.WorkModeListHeaderFmt, runtimeProfileDisplay(current)))
	for _, option := range workModeOptions() {
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

// runWorkModeCommand changes the runtime profile for the current TUI session.
// Rebuilding is asynchronous and failure-atomic: the old controller and
// profile remain active until a fully initialized replacement is ready.
func (m *chatTUI) runWorkModeCommand(input string) tea.Cmd {
	args := tokenizeArgs(input)
	if len(args) == 1 {
		m.commitLine(renderWorkModes(m.width, m.runtimeProfile))
		return nil
	}
	if len(args) != 2 {
		m.notice(i18n.M.WorkModeUsage)
		return nil
	}
	target, ok := parseWorkMode(args[1])
	if !ok {
		m.notice(i18n.M.WorkModeUsage)
		return nil
	}
	if m.buildController == nil || m.ctrl == nil {
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
	if boot.NormalizeTokenMode(m.runtimeProfile) == target {
		m.notice(fmt.Sprintf(i18n.M.WorkModeAlreadyOnFmt, runtimeProfileDisplay(target)))
		return nil
	}

	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("work-mode: snapshot failed: " + err.Error())
	}
	carried := m.ctrl.History()
	resumePath := m.ctrl.SessionPath()
	if err := m.rebindSessionLease(resumePath); err != nil {
		m.notice("work-mode: " + sessionLeaseHeldNotice(err))
		return nil
	}

	display := runtimeProfileDisplay(target)
	m.notice(fmt.Sprintf(i18n.M.WorkModeSwitchingFmt, display))
	oldCtrl := m.ctrl
	build := m.buildController
	ref := m.modelRef
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		c, err := build(controllerBuildSpec{
			ModelRef:         ref,
			RuntimeProfile:   target,
			ToolApprovalMode: oldCtrl.ToolApprovalMode(),
			PlanMode:         oldCtrl.PlanMode(),
		}, carried, resumePath, oldCtrl)
		if err != nil {
			return modelSwitchMsg{
				ref:           ref,
				profile:       target,
				failurePrefix: "work-mode",
				err:           err,
			}
		}
		return modelSwitchMsg{
			ref:           ref,
			profile:       target,
			ctrl:          c,
			oldCtrl:       oldCtrl,
			label:         c.Label(),
			commands:      c.Commands(),
			skills:        c.SlashSkills(),
			host:          c.Host(),
			successNotice: fmt.Sprintf(i18n.M.WorkModeSwitchedFmt, display),
		}
	}
	return m.pendingModelSwitch
}

func (m *chatTUI) workModeArgItems(val string) ([]compItem, int, bool) {
	cmdEnd := strings.IndexAny(val, " \t")
	if cmdEnd < 0 {
		return nil, 0, false
	}
	cmd := val[:cmdEnd]
	if cmd != "/work-mode" && cmd != "/profile" {
		return nil, 0, false
	}
	from := strings.LastIndexAny(val, " \t") + 1
	if len(strings.Fields(val[:from])) != 1 {
		return nil, from, true
	}
	query := strings.ToLower(val[from:])
	current := runtimeProfileName(m.runtimeProfile)
	var out []compItem
	for _, option := range workModeOptions() {
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

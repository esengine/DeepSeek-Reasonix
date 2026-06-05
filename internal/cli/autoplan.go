package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

func (m *chatTUI) runAutoPlanCommand(input string) {
	args := tokenizeArgs(input)
	if len(args) < 2 {
		cfg, err := config.Load()
		if err != nil {
			m.notice("auto-plan: " + err.Error())
			return
		}
		m.notice(fmt.Sprintf("auto-plan: %s (usage: /auto-plan off|on)", cliAutoPlanMode(cfg.Agent.AutoPlan)))
		return
	}
	if len(args) > 2 {
		m.notice("usage: /auto-plan off|on")
		return
	}
	if m.ctrl != nil && m.ctrl.Running() {
		m.notice("finish or cancel the current turn before changing auto-plan")
		return
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice("auto-plan: cannot resolve config path")
		return
	}
	edit := config.LoadForEdit(path)
	if err := edit.SetAutoPlan(args[1]); err != nil {
		m.notice("auto-plan: " + err.Error())
		return
	}
	if err := edit.SaveTo(path); err != nil {
		m.notice("auto-plan: " + err.Error())
		return
	}

	mode := edit.Agent.AutoPlan
	if m.ctrl != nil {
		m.ctrl.SetAutoPlan(mode)
	}
	m.notice(fmt.Sprintf("auto-plan set to %s", mode))
}

func cliAutoPlanMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on", "ask":
		return "on"
	default:
		return "off"
	}
}

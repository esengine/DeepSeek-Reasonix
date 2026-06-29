package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

func (m *chatTUI) runMemoryV5Command(input string) {
	args := tokenizeArgs(input)
	if len(args) > 2 {
		m.notice("usage: /memory-v5 off|on|status")
		return
	}
	if len(args) < 2 || strings.EqualFold(args[1], "status") {
		cfg, err := config.Load()
		if err != nil {
			m.notice("memory-v5: " + err.Error())
			return
		}
		m.notice(fmt.Sprintf("memory-v5: %s (usage: /memory-v5 off|on|status)", cliMemoryV5Mode(cfg.MemoryCompilerEnabled())))
		return
	}
	if m.ctrl != nil && m.ctrl.Running() {
		m.notice("finish or cancel the current turn before changing memory-v5")
		return
	}
	enabled, err := parseCLIMemoryV5Mode(args[1])
	if err != nil {
		m.notice("memory-v5: " + err.Error())
		return
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice("memory-v5: cannot resolve config path")
		return
	}
	edit := config.LoadForEdit(path)
	if err := edit.SetMemoryCompilerEnabled(enabled); err != nil {
		m.notice("memory-v5: " + err.Error())
		return
	}
	if err := edit.SaveTo(path); err != nil {
		m.notice("memory-v5: " + err.Error())
		return
	}

	if m.ctrl != nil {
		m.ctrl.SetMemoryCompilerEnabled(enabled)
	}
	m.notice(fmt.Sprintf("memory-v5 set to %s", cliMemoryV5Mode(enabled)))
}

func parseCLIMemoryV5Mode(mode string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, fmt.Errorf("memory-v5 %q: must be off|on|status", mode)
	}
}

func cliMemoryV5Mode(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

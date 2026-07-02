package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

func (m *chatTUI) runMemoryV5Command(input string) {
	args := tokenizeArgs(input)
	if len(args) > 2 {
		m.notice("usage: /memory-v5 off|observe|compact|on|status")
		return
	}
	if len(args) < 2 || strings.EqualFold(args[1], "status") {
		cfg, err := config.Load()
		if err != nil {
			m.notice("memory-v5: " + err.Error())
			return
		}
		m.notice(fmt.Sprintf("memory-v5: %s (usage: /memory-v5 off|observe|compact|on|status)", cliMemoryV5Mode(cfg.MemoryCompilerEnabled(), cfg.MemoryCompilerVerbosity())))
		return
	}
	if m.ctrl != nil && m.ctrl.Running() {
		m.notice("finish or cancel the current turn before changing memory-v5")
		return
	}
	setting, err := parseCLIMemoryV5Setting(args[1])
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
	if err := edit.SetMemoryCompilerEnabled(setting.enabled); err != nil {
		m.notice("memory-v5: " + err.Error())
		return
	}
	if setting.setVerbosity {
		if err := edit.SetMemoryCompilerVerbosity(setting.verbosity); err != nil {
			m.notice("memory-v5: " + err.Error())
			return
		}
	}
	if err := edit.SaveTo(path); err != nil {
		m.notice("memory-v5: " + err.Error())
		return
	}

	if m.ctrl != nil {
		m.ctrl.SetMemoryCompilerEnabled(setting.enabled)
		if setting.setVerbosity {
			m.ctrl.SetMemoryCompilerVerbosity(setting.verbosity)
		}
	}
	m.notice(fmt.Sprintf("memory-v5 set to %s", cliMemoryV5Mode(edit.MemoryCompilerEnabled(), edit.MemoryCompilerVerbosity())))
}

type cliMemoryV5Setting struct {
	enabled      bool
	verbosity    string
	setVerbosity bool
}

func parseCLIMemoryV5Setting(mode string) (cliMemoryV5Setting, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off":
		return cliMemoryV5Setting{enabled: false}, nil
	case "observe", "silent", "minimal":
		return cliMemoryV5Setting{enabled: true, verbosity: config.MemoryCompilerVerbosityObserve, setVerbosity: true}, nil
	case "on", "compact", "inject", "contract":
		return cliMemoryV5Setting{enabled: true, verbosity: config.MemoryCompilerVerbosityCompact, setVerbosity: true}, nil
	default:
		return cliMemoryV5Setting{}, fmt.Errorf("memory-v5 %q: must be off|observe|compact|on|status", mode)
	}
}

func cliMemoryV5Mode(enabled bool, verbosity string) string {
	if !enabled {
		return "off"
	}
	return config.NormalizeMemoryCompilerVerbosity(verbosity)
}

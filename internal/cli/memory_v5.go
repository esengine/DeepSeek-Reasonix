package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/memorycompiler"
)

func (m *chatTUI) runMemoryV5Command(input string) {
	args := tokenizeArgs(input)
	if len(args) > 2 {
		m.notice("usage: /memory-v5 off|observe|compact|on|status|learnings")
		return
	}
	if len(args) == 2 && strings.EqualFold(args[1], "learnings") {
		m.notice(memoryV5LearningsText(m.ctrl))
		return
	}
	if len(args) < 2 || strings.EqualFold(args[1], "status") {
		cfg, err := config.Load()
		if err != nil {
			m.notice("memory-v5: " + err.Error())
			return
		}
		m.notice(fmt.Sprintf("memory-v5: %s (usage: /memory-v5 off|observe|compact|on|status|learnings)", cliMemoryV5Mode(cfg.MemoryCompilerEnabled(), cfg.MemoryCompilerVerbosity())))
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
	// Lock only the load-modify-save cycle; the controller updates below run
	// off-lock.
	edit, err := func() (*config.Config, error) {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		edit := config.LoadForEdit(path)
		if err := edit.SetMemoryCompilerEnabled(setting.enabled); err != nil {
			return nil, err
		}
		if setting.setVerbosity {
			if err := edit.SetMemoryCompilerVerbosity(setting.verbosity); err != nil {
				return nil, err
			}
		}
		if err := edit.SaveTo(path); err != nil {
			return nil, err
		}
		return edit, nil
	}()
	if err != nil {
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
		return cliMemoryV5Setting{}, fmt.Errorf("memory-v5 %q: must be off|observe|compact|on|status|learnings", mode)
	}
}

// memoryV5LearningsText renders the project's learned Memory v5 state. It is a
// read-only local view; nothing here reaches a provider.
func memoryV5LearningsText(ctrl control.SessionAPI) string {
	root := ""
	if ctrl != nil {
		root = ctrl.WorkspaceRoot()
	}
	rt := memorycompiler.New(config.MemoryCompilerDir(root))
	if rt == nil {
		return "memory-v5: no project state directory"
	}
	rep, ok := rt.LearningsReport(0)
	if !ok {
		return "memory-v5: no learned state yet"
	}
	return memorycompiler.FormatLearningsReport(rep)
}

func cliMemoryV5Mode(enabled bool, verbosity string) string {
	if !enabled {
		return "off"
	}
	return config.NormalizeMemoryCompilerVerbosity(verbosity)
}

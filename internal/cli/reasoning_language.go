package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

func (m *chatTUI) runReasoningLanguageCommand(input string) {
	args := tokenizeArgs(input)
	if len(args) < 2 {
		cfg, err := config.Load()
		if err != nil {
			m.notice("reasoning-language: " + err.Error())
			return
		}
		m.notice(fmt.Sprintf("reasoning-language: %s (usage: /reasoning-language auto|zh|en)", cliReasoningLanguageMode(cfg.ReasoningLanguage())))
		return
	}
	if len(args) > 2 {
		m.notice("usage: /reasoning-language auto|zh|en")
		return
	}
	if m.ctrl != nil && m.ctrl.Running() {
		m.notice("finish or cancel the current turn before changing reasoning-language")
		return
	}
	mode, err := parseCLIReasoningLanguage(args[1])
	if err != nil {
		m.notice("reasoning-language: " + err.Error())
		return
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice("reasoning-language: cannot resolve config path")
		return
	}
	edit := config.LoadForEdit(path)
	if err := edit.SetReasoningLanguage(mode); err != nil {
		m.notice("reasoning-language: " + err.Error())
		return
	}
	if err := edit.SaveTo(path); err != nil {
		m.notice("reasoning-language: " + err.Error())
		return
	}

	mode = edit.ReasoningLanguage()
	if m.ctrl != nil {
		m.ctrl.SetReasoningLanguage(mode)
	}
	m.notice(fmt.Sprintf("reasoning-language set to %s", mode))
}

func parseCLIReasoningLanguage(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		return "auto", nil
	case "zh":
		return "zh", nil
	case "en":
		return "en", nil
	default:
		return "", fmt.Errorf("reasoning_language %q: must be auto|zh|en", mode)
	}
}

func cliReasoningLanguageMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "zh":
		return "zh"
	case "en":
		return "en"
	default:
		return "auto"
	}
}

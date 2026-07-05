package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

func (m *chatTUI) runLanguageSubcommand(input string) {
	args := tokenizeArgs(input)
	if len(args) < 2 {
		cfg, err := config.Load()
		if err != nil {
			m.notice("language: " + err.Error())
			return
		}
		saved := languageDisplay(cfg.Language)
		resolved := i18n.DetectLanguage(cfg.Language)
		m.notice(i18n.M.LanguageHeader + "\n" + describeLanguages(saved, resolved) + "\n" + i18n.M.LanguageHint)
		return
	}
	if len(args) > 2 {
		m.notice(i18n.M.LanguageHint)
		return
	}

	lang, err := normalizeLanguageArg(args[1])
	if err != nil {
		m.notice(err.Error())
		return
	}
	path := config.SourcePath()
	if path == "" {
		path = config.UserConfigPath()
	}
	if path == "" {
		m.notice("language: cannot resolve config path")
		return
	}
	// Lock the whole load-modify-save cycle, including the user-scope override
	// cleanup — its own read-modify-write on the user config. path may be a
	// project config here; holding the user-config lock for that case is
	// harmless. The controller update below runs off-lock.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		edit := config.LoadForEdit(path)
		if err := edit.SetLanguage(lang); err != nil {
			return err
		}
		if err := edit.SaveTo(path); err != nil {
			return err
		}
		if lang == "" {
			return clearUserLanguageOverride(path)
		}
		return nil
	}(); err != nil {
		m.notice("language: " + err.Error())
		return
	}

	resolved := i18n.DetectLanguage(lang)
	if m.ctrl != nil {
		m.ctrl.SetResponseLanguage(lang)
	}
	m.notice(fmt.Sprintf(i18n.M.LanguageChangedFmt, languageDisplay(lang), resolved))
}

// clearUserLanguageOverride drops a stale user-level language override after a
// project-level write. Callers must hold LockUserConfigEdits: this is its own
// load-modify-save on the user config and must not take the lock itself.
func clearUserLanguageOverride(primaryPath string) error {
	userPath := config.UserConfigPath()
	if userPath == "" || sameConfigPath(primaryPath, userPath) {
		return nil
	}
	if _, err := os.Stat(userPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	edit := config.LoadForEdit(userPath)
	if strings.TrimSpace(edit.Language) == "" {
		return nil
	}
	if err := edit.SetLanguage(""); err != nil {
		return err
	}
	return edit.SaveTo(userPath)
}

func sameConfigPath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA == nil {
		a = aa
	}
	if errB == nil {
		b = bb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func normalizeLanguageArg(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto", "detect", "default":
		return "", nil
	case "en", "english":
		return "en", nil
	case "zh", "cn", "chinese", "中文":
		return "zh", nil
	default:
		return "", fmt.Errorf("usage: /language auto|en|zh")
	}
}

func languageDisplay(lang string) string {
	if strings.TrimSpace(lang) == "" {
		return "auto"
	}
	return lang
}

func describeLanguages(current, resolved string) string {
	items := []struct {
		tag  string
		hint string
	}{
		{"auto", i18n.M.ArgLanguageAuto},
		{"en", i18n.M.ArgLanguageEn},
		{"zh", i18n.M.ArgLanguageZh},
	}
	var b strings.Builder
	for _, it := range items {
		marker := "  "
		if it.tag == current {
			marker = "• "
		}
		hint := it.hint
		if it.tag == current {
			hint += " · " + i18n.M.ArgThemeCurrent
		}
		if it.tag == "auto" && current == "auto" {
			hint += " · " + resolved
		}
		fmt.Fprintf(&b, "%s%-6s %s\n", marker, it.tag, hint)
	}
	return strings.TrimRight(b.String(), "\n")
}

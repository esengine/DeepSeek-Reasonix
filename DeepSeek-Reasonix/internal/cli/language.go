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
	edit := config.LoadForEdit(path)
	if err := edit.SetLanguage(lang); err != nil {
		m.notice("language: " + err.Error())
		return
	}
	if err := edit.SaveTo(path); err != nil {
		m.notice("language: " + err.Error())
		return
	}
	if lang == "" {
		if err := clearUserLanguageOverride(path); err != nil {
			m.notice("language: " + err.Error())
			return
		}
	}

	resolved := i18n.DetectLanguage(lang)
	m.notice(fmt.Sprintf(i18n.M.LanguageChangedFmt, languageDisplay(lang), resolved))
}

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

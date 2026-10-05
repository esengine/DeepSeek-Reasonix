package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/termrender"
)

// miscSlash answers the commands that only change how this terminal draws:
// the kernel holds no state for them, so Submit would call them unknown.
func (m *model) miscSlash(display string) (tea.Cmd, bool) {
	name, arg, _ := strings.Cut(display, " ")
	switch name {
	case "/cls", "/todo", "/verbose", "/diff-fold", "/theme":
	default:
		return nil, false
	}
	m.composer.Reset()
	m.tr.AddEcho(display)
	switch name {
	case "/cls":
		return m.clearDisplay(), true
	case "/todo":
		m.todos = nil
		m.tr.AddNotice("info", i18n.M.SlashTodoCleared)
	case "/verbose":
		m.toggleVerbose()
	case "/diff-fold":
		m.toggleDiffFold()
	case "/theme":
		m.setTheme(strings.TrimSpace(arg))
	}
	return m.commit(), true
}

// clearDisplay redraws the screen under a fresh banner without touching the
// conversation, as /cls does in 1.x. The to-do list is state the kernel owns,
// so it stays up.
func (m *model) clearDisplay() tea.Cmd {
	if m.tr.Running {
		m.tr.AddNotice("warn", "wait for the current turn to finish before /cls")
		return m.commit()
	}
	todos := m.todos
	m.resetScreen()
	m.todos = todos
	m.tr.AddNotice("info", i18n.M.SlashClsDone)
	return tea.Sequence(tea.ClearScreen, m.greet(), m.commit())
}

func (m *model) toggleVerbose() {
	m.verbose = !m.verbose
	suffix := ""
	if err := storeVerbose(m.verbose); err != nil {
		suffix = "\npreference was not saved: " + err.Error()
	}
	if m.verbose {
		m.tr.AddNotice("info", "verbose on — thinking text will be shown"+suffix)
	} else {
		m.tr.AddNotice("info", "verbose off — thinking text will stay collapsed"+suffix)
	}
}

func (m *model) toggleDiffFold() {
	if diffFoldLines == 0 {
		diffFoldLines = diffPreviewLines
		m.tr.AddNotice("info", fmt.Sprintf(i18n.M.DiffFoldEnabledFmt, diffPreviewLines))
		return
	}
	diffFoldLines = 0
	m.tr.AddNotice("info", i18n.M.DiffFoldDisabled)
}

func (m *model) setTheme(name string) {
	if name == "" {
		m.tr.AddNotice("info", i18n.M.ThemeHeader+"\n"+termrender.DescribeThemes()+"\n"+i18n.M.ThemeHint)
		return
	}
	name = strings.ToLower(name)
	var theme termrender.Palette
	if termrender.IsThemeMode(name) {
		theme = termrender.SetThemeMode(name)
	} else {
		var ok bool
		if theme, ok = termrender.SetThemeStyle(name); !ok {
			m.tr.AddNotice("info", fmt.Sprintf(i18n.M.ThemeUnknownFmt, name)+"\n"+termrender.DescribeThemes())
			return
		}
	}
	termrender.ApplyTextareaTheme(&m.composer)
	m.tr.AddNotice("info", fmt.Sprintf(i18n.M.ThemeChangedFmt, theme.Name, theme.Style))
	if err := storeTheme(name, theme); err != nil {
		m.tr.AddNotice("warn", "theme: preference was not saved: "+err.Error())
	}
}

// prefsPath is the user config: theme and verbosity are personal, and a project
// reasonix.toml is committed and shared.
func prefsPath() string { return config.UserConfigPath() }

func storedVerbose() bool {
	cfg, err := config.Load()
	return err == nil && cfg.UI.ShowReasoning
}

func storeVerbose(on bool) error {
	return editPrefs(func(cfg *config.Config) error { return cfg.SetShowReasoning(on) })
}

func storeTheme(name string, theme termrender.Palette) error {
	return editPrefs(func(cfg *config.Config) error {
		if termrender.IsThemeMode(name) {
			cfg.UI.Theme, cfg.UI.ThemeStyle = name, theme.Style
		} else {
			cfg.UI.Theme, cfg.UI.ThemeStyle = theme.Name, name
		}
		return nil
	})
}

func editPrefs(edit func(*config.Config) error) error {
	path := prefsPath()
	if path == "" {
		return fmt.Errorf("cannot resolve config path")
	}
	return config.EditConfigFile(path, edit)
}

// verboseFold is how a settling answer carries its thinking under /verbose:
// open and shuttable full screen, open for good in the scrollback.
func (m *model) verboseFold() outputFold {
	if m.scr != nil {
		return foldOpen
	}
	return foldPinned
}

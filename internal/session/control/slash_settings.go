package control

import (
	"fmt"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/outputstyle"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessionstore"
)

// settingsNotice answers the settings verbs a terminal user types as bare
// slash commands. Like managementNotice it reports whether it handled one.
func (c *Controller) settingsNotice(fields []string, trimmed string) bool {
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
	switch fields[0] {
	case "/effort":
		if len(fields) != 1 {
			return false
		}
		c.notice(c.effortStatusText())
	case "/forget":
		c.forgetNotice(rest)
	case "/rename":
		c.renameNotice(rest)
	case "/sandbox":
		c.notice(c.sandboxStatusText())
	case "/output-style", "/output-styles":
		c.notice(c.outputStylesText())
	case "/reasoning-language":
		c.reasoningLanguageNotice(fields)
	case "/language":
		c.languageNotice(fields)
	case "/currency":
		c.currencyNotice(fields)
	default:
		return false
	}
	return true
}

func (c *Controller) effortStatusText() string {
	cfg, err := config.LoadForRootReadOnly(c.WorkspaceRoot())
	if err != nil {
		return "effort: " + err.Error()
	}
	entry, ok := cfg.ResolveModel(c.ModelRef())
	if !ok {
		return fmt.Sprintf("effort: unknown model %q", c.ModelRef())
	}
	capability := config.EffortCapabilityForEntry(entry)
	if !capability.Supported {
		return fmt.Sprintf("effort is not configurable for %s", entry.Name)
	}
	return fmt.Sprintf("effort for %s: %s (default: %s; options: %s)",
		entry.Name, config.EffortDisplay(entry), capability.Default, strings.Join(capability.Levels, "|"))
}

func (c *Controller) forgetNotice(name string) {
	if name == "" {
		c.notice(i18n.M.ForgetUsage)
		return
	}
	if err := c.ForgetMemory(name); err != nil {
		c.notice(fmt.Sprintf("forget: %v", err))
		return
	}
	c.notice(fmt.Sprintf(i18n.M.ForgetDoneFmt, name))
}

func (c *Controller) renameNotice(title string) {
	if title == "" {
		c.notice(i18n.M.RenameUsage)
		return
	}
	path := c.SessionPath()
	if path == "" {
		c.notice(i18n.M.RenameNoSession)
		return
	}
	if err := sessionstore.RenameSession(path, title); err != nil {
		c.notice("rename: " + err.Error())
		return
	}
	c.notice(fmt.Sprintf(i18n.M.RenameDoneFmt, title))
}

func (c *Controller) sandboxStatusText() string {
	cfg, err := config.LoadForRootReadOnly(c.WorkspaceRoot())
	if err != nil {
		return "sandbox: config not loaded"
	}
	bash := cfg.BashMode()
	var b strings.Builder
	b.WriteString("sandbox\n")
	b.WriteString("  phase 0  file-writer confinement\n")
	if roots := cfg.WriteRoots(); len(roots) > 0 {
		fmt.Fprintf(&b, "    write_roots  %s\n", strings.Join(roots, ", "))
	}
	if cfg.Sandbox.WorkspaceRoot != "" {
		fmt.Fprintf(&b, "    workspace_root  %s\n", cfg.Sandbox.WorkspaceRoot)
	}
	if len(cfg.Sandbox.AllowWrite) > 0 {
		fmt.Fprintf(&b, "    allow_write  %s\n", strings.Join(cfg.Sandbox.AllowWrite, ", "))
	}
	b.WriteString("  phase 1  OS bash sandbox\n")
	fmt.Fprintf(&b, "    bash        %s", bash)
	if bash == "enforce" && !sandbox.Available() {
		b.WriteString(" (unavailable: no OS sandbox on this host; bash execution is refused. " + sandbox.UnavailableRemediation() + ")")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "    network     %v", cfg.Sandbox.Network)
	return b.String()
}

func (c *Controller) outputStylesText() string {
	styles := outputstyle.List(outputstyle.Dirs())
	if len(styles) == 0 {
		return i18n.M.OutputStyleNone
	}
	active := ""
	if cfg, err := config.LoadForRootReadOnly(c.WorkspaceRoot()); err == nil {
		active = cfg.Agent.OutputStyle
	}
	var b strings.Builder
	b.WriteString("output styles\n")
	for _, st := range styles {
		scope := "builtin"
		if !st.Builtin {
			scope = "custom"
		}
		mark := ""
		if strings.EqualFold(st.Name, active) {
			mark = "  active"
		}
		fmt.Fprintf(&b, "  %-16s (%s)  %s%s\n", st.Name, scope, st.Description, mark)
	}
	b.WriteString("set agent.output_style in reasonix.toml to apply one (takes effect next session)")
	return b.String()
}

func (c *Controller) reasoningLanguageNotice(fields []string) {
	const usage = "usage: /reasoning-language auto|zh|en"
	if len(fields) < 2 {
		cfg, err := config.LoadForRootReadOnly(c.WorkspaceRoot())
		if err != nil {
			c.notice("reasoning-language: " + err.Error())
			return
		}
		c.notice(fmt.Sprintf("reasoning-language: %s (usage: /reasoning-language auto|zh|en)", cfg.ReasoningLanguage()))
		return
	}
	if len(fields) > 2 {
		c.notice(usage)
		return
	}
	if c.Running() {
		c.notice("finish or cancel the current turn before changing reasoning-language")
		return
	}
	mode := strings.ToLower(fields[1])
	if mode != "auto" && mode != "zh" && mode != "en" {
		c.notice(fmt.Sprintf("reasoning-language: reasoning_language %q: must be auto|zh|en", fields[1]))
		return
	}
	path := config.UserConfigPath()
	if path == "" {
		c.notice("reasoning-language: cannot resolve config path")
		return
	}
	var saved string
	err := config.EditConfigFile(path, func(cfg *config.Config) error {
		if err := cfg.SetReasoningLanguage(mode); err != nil {
			return err
		}
		saved = cfg.ReasoningLanguage()
		return nil
	})
	if err != nil {
		c.notice("reasoning-language: " + err.Error())
		return
	}
	c.SetReasoningLanguage(saved)
	c.notice(fmt.Sprintf("reasoning-language set to %s", saved))
}

func (c *Controller) languageNotice(fields []string) {
	if len(fields) < 2 {
		cfg, err := config.LoadForRootReadOnly(c.WorkspaceRoot())
		if err != nil {
			c.notice("language: " + err.Error())
			return
		}
		c.notice(i18n.M.LanguageHeader + "\n" + describeLanguages(languageDisplay(cfg.Language), i18n.DetectLanguage(cfg.Language)) + "\n" + i18n.M.LanguageHint)
		return
	}
	if len(fields) > 2 {
		c.notice(i18n.M.LanguageHint)
		return
	}
	lang, ok := normalizeLanguageArg(fields[1])
	if !ok {
		c.notice("usage: /language auto|en|zh")
		return
	}
	if err := saveLanguage(lang); err != nil {
		c.notice("language: " + err.Error())
		return
	}
	c.SetResponseLanguage(lang)
	c.notice(fmt.Sprintf(i18n.M.LanguageChangedFmt, languageDisplay(lang), i18n.DetectLanguage(lang)))
	if cfg, err := config.LoadForRootReadOnly(c.WorkspaceRoot()); err == nil && cfg.Language != lang {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeLanguageOverridden,
			Text: "this project's config sets language = " + languageDisplay(cfg.Language) + ", which overrides your choice in new sessions", Detail: cfg.Language})
	}
}

// saveLanguage stores the choice in the user config: a language is a personal
// preference and must not land in a project file that is committed and shared.
func saveLanguage(lang string) error {
	path := config.UserConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve config path")
	}
	return config.EditConfigFile(path, func(cfg *config.Config) error { return cfg.SetLanguage(lang) })
}

func normalizeLanguageArg(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto", "detect", "default":
		return "", true
	case "en", "english":
		return "en", true
	case "zh", "cn", "chinese", "中文":
		return "zh", true
	}
	return "", false
}

func languageDisplay(lang string) string {
	if strings.TrimSpace(lang) == "" {
		return "auto"
	}
	return lang
}

func describeLanguages(current, resolved string) string {
	items := []struct{ tag, hint string }{
		{"auto", i18n.M.ArgLanguageAuto},
		{"en", i18n.M.ArgLanguageEn},
		{"zh", i18n.M.ArgLanguageZh},
	}
	var b strings.Builder
	for _, it := range items {
		marker, hint := "  ", it.hint
		if it.tag == current {
			marker = "• "
			hint += " · " + i18n.M.ArgThemeCurrent
			if it.tag == "auto" {
				hint += " · " + resolved
			}
		}
		fmt.Fprintf(&b, "%s%-6s %s\n", marker, it.tag, hint)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ParseDisplayCurrency maps a /currency argument to the stored preference:
// "" for auto, otherwise CNY or USD.
func ParseDisplayCurrency(arg string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(arg)) {
	case "", "AUTO":
		return "", nil
	case "CNY":
		return "CNY", nil
	case "USD":
		return "USD", nil
	}
	return "", fmt.Errorf("pricing currency %q: must be auto|CNY|USD", arg)
}

func currencyDisplay(pref string) string {
	if strings.TrimSpace(pref) == "" {
		return "auto"
	}
	return strings.ToUpper(strings.TrimSpace(pref))
}

func describeCurrencies(current, resolved string) string {
	var b strings.Builder
	for _, item := range []string{"auto", "CNY", "USD"} {
		marker, hint := "  ", ""
		if item == current {
			marker = "• "
		}
		if item == "auto" {
			hint = " (" + resolved + ")"
		}
		fmt.Fprintf(&b, "%s%s%s\n", marker, item, hint)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *Controller) currencyNotice(fields []string) {
	if len(fields) < 2 {
		cfg, err := config.LoadForRootReadOnly(c.WorkspaceRoot())
		if err != nil {
			c.notice("currency: " + err.Error())
			return
		}
		c.notice(i18n.M.CurrencyHeader + "\n" + describeCurrencies(currencyDisplay(cfg.DisplayCurrencyPref()), cfg.ResolveDisplayCurrency()) + "\n" + i18n.M.CurrencyHint)
		return
	}
	if len(fields) > 2 {
		c.notice(i18n.M.CurrencyHint)
		return
	}
	mode, err := ParseDisplayCurrency(fields[1])
	if err != nil {
		c.notice(err.Error())
		return
	}
	path := config.UserConfigPath()
	if path == "" {
		c.notice("currency: cannot resolve user config path")
		return
	}
	var resolved string
	err = config.EditConfigFile(path, func(cfg *config.Config) error {
		if err := cfg.SetDisplayCurrency(mode); err != nil {
			return err
		}
		resolved = cfg.ResolveDisplayCurrency()
		return nil
	})
	if err != nil {
		c.notice("currency: " + err.Error())
		return
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeDisplayCurrency,
		Text: fmt.Sprintf(i18n.M.CurrencyChangedFmt, currencyDisplay(mode), resolved), Detail: mode})
}

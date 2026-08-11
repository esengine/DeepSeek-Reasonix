package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

func (m *chatTUI) runCurrencySubcommand(input string) tea.Cmd {
	args := tokenizeArgs(input)
	if len(args) < 2 {
		cfg, err := config.Load()
		if err != nil {
			m.notice("currency: " + err.Error())
			return nil
		}
		m.notice(i18n.M.CurrencyHeader + "\n" + describePricingCurrencies(pricingCurrencyDisplay(cfg.DisplayCurrencyPref()), cfg.ResolveDisplayCurrency()) + "\n" + i18n.M.CurrencyHint)
		return nil
	}
	if len(args) > 2 {
		m.notice(i18n.M.CurrencyHint)
		return nil
	}
	mode, err := parseCLIPricingCurrency(args[1])
	if err != nil {
		m.notice(err.Error())
		return nil
	}
	if !m.runtimeSettingChangeReady() {
		return nil
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice("currency: cannot resolve user config path")
		return nil
	}
	var resolved string
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		edit := config.LoadForEdit(path)
		if err := edit.SetDisplayCurrency(mode); err != nil {
			return err
		}
		resolved = edit.ResolveDisplayCurrency()
		return edit.SaveTo(path)
	}(); err != nil {
		m.notice("currency: " + err.Error())
		return nil
	}

	success := fmt.Sprintf(i18n.M.CurrencyChangedFmt, pricingCurrencyDisplay(mode), resolved)
	if m.ctrl == nil {
		m.notice(success)
		return nil
	}
	return m.scheduleCurrentControllerRebuild("currency", success)
}

func parseCLIPricingCurrency(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "AUTO":
		return "", nil
	case "CNY":
		return "CNY", nil
	case "USD":
		return "USD", nil
	default:
		return "", fmt.Errorf("pricing currency %q: must be auto|CNY|USD", value)
	}
}

func pricingCurrencyDisplay(currency string) string {
	if strings.TrimSpace(currency) == "" {
		return "auto"
	}
	return strings.ToUpper(strings.TrimSpace(currency))
}

func describePricingCurrencies(current, resolved string) string {
	items := []string{"auto", "CNY", "USD"}
	var b strings.Builder
	for _, item := range items {
		marker := "  "
		if item == current {
			marker = "• "
		}
		hint := ""
		if item == "auto" {
			hint = " (" + resolved + ")"
		}
		fmt.Fprintf(&b, "%s%s%s\n", marker, item, hint)
	}
	return strings.TrimRight(b.String(), "\n")
}

package control

import (
	"errors"
	"fmt"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

// ErrDisplayCurrencyInvalid is a mode that is none of auto, CNY or USD.
var ErrDisplayCurrencyInvalid = errors.New("display currency")

// DisplayCurrencySettings is the user's stored display-currency preference:
// "auto" follows the wallet, otherwise the pinned currency code.
type DisplayCurrencySettings struct {
	Mode string `json:"mode"`
	Path string `json:"path"`
}

func (c *Controller) DisplayCurrencySettings() DisplayCurrencySettings {
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	return DisplayCurrencySettings{Mode: currencyDisplay(cfg.DisplayCurrencyPref()), Path: path}
}

// SaveDisplayCurrency stores the preference and announces it, which is what
// rebinds the quote sink and every frontend ledger without a rebuild.
func (c *Controller) SaveDisplayCurrency(mode string) error {
	pref, err := ParseDisplayCurrency(mode)
	if err != nil {
		return err
	}
	path := config.UserConfigPath()
	if path == "" {
		return errors.New("cannot resolve user config path")
	}
	var resolved string
	err = config.EditConfigFile(path, func(cfg *config.Config) error {
		if err := cfg.SetDisplayCurrency(pref); err != nil {
			return err
		}
		resolved = cfg.ResolveDisplayCurrency()
		return nil
	})
	if err != nil {
		return err
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeDisplayCurrency,
		Text: fmt.Sprintf(i18n.M.CurrencyChangedFmt, currencyDisplay(pref), resolved), Detail: pref})
	return nil
}

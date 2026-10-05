package serve

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/model/billing"
)

// walletBody is the wallet as a frontend lays it out. FetchedAt dates the
// number so a value standing in past its freshness can say so rather than look
// current.
type walletBody struct {
	Display   string       `json:"display"`
	Available bool         `json:"available"`
	Stale     bool         `json:"stale"`
	FetchedAt time.Time    `json:"fetchedAt"`
	Lines     []walletLine `json:"lines,omitempty"`
}

// walletLine is one currency, already rendered. Two currencies are two lines
// and never a sum — combining them would mean inventing an exchange rate — so
// the only thing a frontend does with these is stack them.
type walletLine struct {
	Currency string `json:"currency"`
	Total    string `json:"total"`
	Granted  string `json:"granted,omitempty"`
}

// balance answers the active provider's wallet. It is its own route because
// /status is polled four times a second while a turn runs, and a network round
// trip on that clock is what the reader ends up waiting on. 204 is "this
// provider has no wallet" — an absence to render as nothing, not as a failure.
func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	reading := s.ctl().Balance(r.Context())
	s.adoptWalletCurrency(reading)
	if !reading.Configured {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if reading.Balance == nil {
		slog.Warn("serve: wallet unreadable", "err", reading.Err)
		refuse(w, http.StatusBadGateway, walletCode(reading.Err), reading.Err.Error(), nil)
		return
	}
	writeJSON(w, walletBody{
		Display:   reading.Balance.DisplayForCurrency(s.bc.DisplayCurrency()),
		Available: reading.Balance.Available,
		Stale:     reading.Stale(),
		FetchedAt: reading.At,
		Lines:     walletLines(reading.Balance.Infos),
	})
}

func walletLines(infos []billing.Info) []walletLine {
	lines := make([]walletLine, 0, len(infos))
	for _, i := range infos {
		lines = append(lines, walletLine{Currency: i.Currency, Total: i.Render(), Granted: i.RenderGranted()})
	}
	return lines
}

// walletCode names which failure this was. "Your key was rejected" and "the
// endpoint is down" are different things to do next, and a single sentence for
// both leaves the reader matching words to tell them apart.
func walletCode(err error) string {
	switch {
	case errors.Is(err, billing.ErrUnauthorized):
		return "wallet.unauthorized"
	case errors.Is(err, billing.ErrUnreadable):
		return "wallet.unreadable"
	default:
		return "wallet.unreachable"
	}
}

// adoptWalletCurrency lets a single-currency wallet select an existing
// valuation. Runtime-only: a hint is never persisted as configuration or
// history, and a user's own preference always wins.
func (s *Server) adoptWalletCurrency(reading billing.Reading) {
	if reading.Balance == nil {
		return
	}
	if cfg, err := config.Load(); err == nil && cfg.DisplayCurrencyPref() == "" {
		s.bc.SetDisplayCurrency(reading.Balance.PrimaryCurrency())
	}
}

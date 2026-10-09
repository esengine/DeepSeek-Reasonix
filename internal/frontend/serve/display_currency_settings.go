package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/session/control"
)

func (s *Server) registerDisplayCurrencyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /display-currency", s.displayCurrencySettings)
	mux.HandleFunc("POST /display-currency", s.saveDisplayCurrency)
}

func (s *Server) displayCurrencySettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().DisplayCurrencySettings())
}

// saveDisplayCurrency needs no rebuild: the controller announces the change on
// the event stream, which rebinds the quote sink and this server's ledger.
func (s *Server) saveDisplayCurrency(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SaveDisplayCurrency(body.Mode); err != nil {
		if errors.Is(err, control.ErrDisplayCurrencyInvalid) {
			saveFailed(w, http.StatusBadRequest, "display_currency.invalid", err)
			return
		}
		saveFailed(w, http.StatusInternalServerError, "display_currency.save_failed", err)
		return
	}
	writeJSON(w, s.ctl().DisplayCurrencySettings())
}

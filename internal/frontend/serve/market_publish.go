// market_publish.go — submitting a package to the community registry under the
// signed-in account, and reading back where each submission stands in review.
package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/market"
	"reasonix/internal/platform/account"
)

// marketPublisher is swapped by tests; production always talks to the fixed host.
var marketPublisher = func(hc *http.Client) market.Publisher { return market.NewClient(hc) }

func (s *Server) registerMarketPublishRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /market/publish", s.marketPublish)
	mux.HandleFunc("GET /market/mine", s.marketMine)
	mux.HandleFunc("POST /market/mine/plan", s.marketOwnPlan)
	mux.HandleFunc("POST /market/mine/install", s.marketOwnInstall)
	mux.HandleFunc("POST /market/mine/{handle}/{name}/submit", s.marketOwnSubmit)
}

// marketSession is the gate both routes share. The token is the account's
// session, so it is read only where the host lets this window sign in.
func (s *Server) marketSession(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.accountAuthAllowed(w, r) {
		return "", false
	}
	token := account.Token()
	if token == "" {
		refuse(w, http.StatusUnauthorized, "market.signed_out", "sign in to publish to the community market", nil)
		return "", false
	}
	return token, true
}

func (s *Server) marketPublish(w http.ResponseWriter, r *http.Request) {
	token, ok := s.marketSession(w, r)
	if !ok {
		return
	}
	var sub market.Submission
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&sub); err != nil {
		badBody(w)
		return
	}
	out, err := marketPublisher(marketHTTP()).Publish(r.Context(), token, sub)
	if err != nil {
		refusePublish(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) marketMine(w http.ResponseWriter, r *http.Request) {
	token, ok := s.marketSession(w, r)
	if !ok {
		return
	}
	rows, err := marketPublisher(marketHTTP()).Mine(r.Context(), token)
	if err != nil {
		refusePublish(w, err)
		return
	}
	held := market.InstalledRecords(config.ReasonixHomeDir())
	out := make([]marketEntry, 0, len(rows))
	for _, p := range rows {
		out = append(out, marketEntry{Package: p, Installed: installedView(held, p.Slug)})
	}
	writeJSON(w, map[string]any{"packages": out})
}

// refusePublish gives each reason a submission did not land its own code; the
// registry's causes are projected from its codes, never re-derived here.
func refusePublish(w http.ResponseWriter, err error) {
	detail := map[string]any{"detail": err.Error()}
	var rejected *market.RejectedError
	switch {
	case errors.Is(err, market.ErrSignedOut):
		refuse(w, http.StatusUnauthorized, "market.signed_out", "sign in to publish to the community market", detail)
	case errors.Is(err, market.ErrEmailUnverified):
		refuse(w, http.StatusForbidden, "market.email_unverified", "verify the account's email before publishing", detail)
	case errors.Is(err, market.ErrNotOwner):
		refuse(w, http.StatusConflict, "market.not_owner", "that name belongs to another publisher", detail)
	case errors.Is(err, market.ErrVersionExists):
		refuse(w, http.StatusConflict, "market.version_exists", "that version is already published", detail)
	case errors.Is(err, market.ErrRateLimited):
		refuse(w, http.StatusTooManyRequests, "market.rate_limited", "too many submissions; wait a minute", detail)
	case errors.Is(err, market.ErrUnpublishable):
		refuse(w, http.StatusUnprocessableEntity, "market.unpublishable", "the market could not install this source once approved", detail)
	case errors.As(err, &rejected):
		refuse(w, http.StatusUnprocessableEntity, "market.rejected", "the registry refused the submission", map[string]any{"detail": rejected.Message})
	default:
		refuseMarket(w, err)
	}
}

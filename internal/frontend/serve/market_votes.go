// market_votes.go — what the signed-in person says about a listed package, and
// the anonymous install report. The account token never leaves the kernel: it
// goes from the credential store to the one registry host and nowhere else.
package serve

import (
	"encoding/json"
	"net/http"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/market"
	"reasonix/internal/platform/account"
	"reasonix/internal/platform/telemetry"
)

// Swapped by tests; production always talks to the fixed registry host.
var (
	marketVoting   = func(hc *http.Client) market.Voting { return market.NewClient(hc) }
	marketReporter = func(hc *http.Client) market.InstallReporter { return market.NewClient(hc) }
)

// AllowMarketInstallReport lets an install from the market be reported to the
// registry. The host grants it only for a release build; the person's anonymous
// statistics switches still decide per install.
func (s *Server) AllowMarketInstallReport() { s.grants.installReport = true }

func (s *Server) registerMarketVoteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /market/packages/{handle}/{name}/vote", s.marketMyVote)
	mux.HandleFunc("POST /market/packages/{handle}/{name}/vote", s.marketVote)
}

func (s *Server) marketMyVote(w http.ResponseWriter, r *http.Request) {
	if !s.accountAuthAllowed(w, r) {
		return
	}
	token := account.Token()
	if token == "" {
		writeJSON(w, map[string]any{"signedIn": false, "value": 0})
		return
	}
	v, err := marketVoting(marketHTTP()).MyVote(r.Context(), token, r.PathValue("handle")+"/"+r.PathValue("name"))
	if err != nil {
		refuseMarket(w, err)
		return
	}
	writeJSON(w, voteJSON(v))
}

func (s *Server) marketVote(w http.ResponseWriter, r *http.Request) {
	if !s.accountAuthAllowed(w, r) {
		return
	}
	var req struct {
		Value *int `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	if req.Value == nil {
		missingField(w, "value")
		return
	}
	token := account.Token()
	if token == "" {
		refuseMarket(w, market.ErrSignedOut)
		return
	}
	v, err := marketVoting(marketHTTP()).Vote(r.Context(), token, r.PathValue("handle")+"/"+r.PathValue("name"), *req.Value)
	if err != nil {
		refuseMarket(w, err)
		return
	}
	v.CanVote = true
	v.EmailVerified = true
	writeJSON(w, voteJSON(v))
}

func voteJSON(v market.Vote) map[string]any {
	return map[string]any{
		"signedIn": true, "value": v.Value, "upCount": v.UpCount, "downCount": v.DownCount,
		"approvalRate": v.ApprovalRate, "canVote": v.CanVote, "own": v.Own, "emailVerified": v.EmailVerified,
	}
}

// marketInstallKey is the market's own anonymous install id, or "" when
// none may be sent: the host has not allowed it (or r came from a paired
// device), either desktop statistics switch is off, or the environment opts out.
func (s *Server) marketInstallKey(r *http.Request) string {
	if !s.grants.at(r).installReport || telemetry.OptedOut() {
		return ""
	}
	cfg, err := config.Load()
	if err != nil || cfg == nil || !cfg.DesktopTelemetry() || !cfg.DesktopMetrics() {
		return ""
	}
	id, err := market.InstallID(config.ReasonixHomeDir())
	if err != nil {
		return ""
	}
	return id
}

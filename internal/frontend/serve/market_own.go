// market_own.go — a publisher installing their own package before, or without,
// review, and sending a private one to review. Which package that is and where
// it comes from is the registry's answer for the signed-in account, never the
// request's; the install is the same plan-then-apply as any other source.
package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/market"
)

func (s *Server) marketOwnPlan(w http.ResponseWriter, r *http.Request) {
	s.marketOwnRun(w, r, false)
}

func (s *Server) marketOwnInstall(w http.ResponseWriter, r *http.Request) {
	s.marketOwnRun(w, r, true)
}

func (s *Server) marketOwnRun(w http.ResponseWriter, r *http.Request, apply bool) {
	token, ok := s.marketSession(w, r)
	if !ok {
		return
	}
	var req market.Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		badBody(w)
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	if req.Slug == "" {
		missingField(w, "slug")
		return
	}
	if apply && strings.TrimSpace(req.Version) == "" {
		missingField(w, "version")
		return
	}
	hc := marketHTTP()
	svc := &market.Service{
		Owner:        marketPublisher(hc),
		Home:         config.ReasonixHomeDir(),
		NewInstaller: market.NewInstallSource(s.ctl().WorkspaceRoot(), hc, s.ctl().DisconnectMCPServer),
	}
	run := svc.PlanOwn
	if apply {
		run = svc.InstallOwn
	}
	out, err := run(r.Context(), token, req)
	if err != nil {
		refuseOwn(w, err)
		return
	}
	s.writeMarketOutcome(w, r, out, req.Slug, apply)
}

func (s *Server) marketOwnSubmit(w http.ResponseWriter, r *http.Request) {
	token, ok := s.marketSession(w, r)
	if !ok {
		return
	}
	pkg, err := marketPublisher(marketHTTP()).Submit(r.Context(), token, r.PathValue("handle")+"/"+r.PathValue("name"))
	if err != nil {
		refuseOwn(w, err)
		return
	}
	writeJSON(w, map[string]any{"package": pkg})
}

// refuseOwn names what only the owner's routes can meet before deferring to
// the publish and market refusals.
func refuseOwn(w http.ResponseWriter, err error) {
	detail := map[string]any{"detail": err.Error()}
	switch {
	case errors.Is(err, market.ErrNotFound):
		refuse(w, http.StatusNotFound, "market.not_yours", "none of the account's packages has that name", detail)
	case errors.Is(err, market.ErrNotPrivate):
		refuse(w, http.StatusConflict, "market.not_private", "only a private package can be submitted for review", detail)
	default:
		refusePublish(w, err)
	}
}

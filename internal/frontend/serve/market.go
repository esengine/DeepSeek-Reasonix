// market.go — the community registry as a source: browse it, read one entry,
// and install its approved version through the same plan-then-apply as a
// pasted address. What the registry says is shown; what gets installed is
// decided by the pinned digest and the plan the person confirmed.
package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/market"
)

// marketRegistry is swapped by tests; production always reads the fixed host.
var marketRegistry = func(hc *http.Client) market.Registry { return market.NewClient(hc) }

func (s *Server) registerMarketRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /market/packages", s.marketList)
	mux.HandleFunc("GET /market/packages/{handle}/{name}", s.marketDetail)
	mux.HandleFunc("POST /market/plan", s.marketPlan)
	mux.HandleFunc("POST /market/install", s.marketInstall)
	s.registerMarketVoteRoutes(mux)
}

// marketEntry is a listed package plus what this machine holds of it.
type marketEntry struct {
	market.Package
	Installed *marketInstalled `json:"installed,omitempty"`
}

type marketInstalled struct {
	Version     string `json:"version"`
	ContentHash string `json:"contentHash"`
}

type marketDetailView struct {
	Package   market.Package   `json:"package"`
	Approved  *market.Version  `json:"approved,omitempty"`
	Pinned    bool             `json:"pinned"`
	Installed *marketInstalled `json:"installed,omitempty"`
}

// marketHTTP is the user's proxy-aware client: a machine that needs a proxy to
// reach the registry needs it to fetch what the registry points at too.
func marketHTTP() *http.Client {
	spec := netclient.ProxySpec{Mode: netclient.ModeAuto}
	if cfg, err := config.Load(); err == nil && cfg != nil {
		spec = cfg.NetworkProxySpec()
	}
	hc, err := netclient.NewHTTPClient(spec, netclient.TransportOptions{})
	if err != nil {
		return &http.Client{}
	}
	return hc
}

func (s *Server) marketClient() market.Registry { return marketRegistry(marketHTTP()) }

func (s *Server) marketList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	page, err := s.marketClient().List(r.Context(), market.Query{
		Kind: q.Get("kind"), Q: q.Get("q"), Sort: q.Get("sort"), Offset: offset, Pinned: q.Get("pinned") == "1",
	})
	if err != nil {
		refuseMarket(w, err)
		return
	}
	held := market.InstalledRecords(config.ReasonixHomeDir())
	out := make([]marketEntry, 0, len(page.Packages))
	for _, p := range page.Packages {
		out = append(out, marketEntry{Package: p, Installed: installedView(held, p.Slug)})
	}
	writeJSON(w, map[string]any{"packages": out, "limit": page.Limit, "offset": page.Offset})
}

func (s *Server) marketDetail(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("handle") + "/" + r.PathValue("name")
	d, err := s.marketClient().Detail(r.Context(), slug)
	if err != nil {
		refuseMarket(w, err)
		return
	}
	writeJSON(w, marketDetailView{
		Package:   d.Package,
		Approved:  d.Approved,
		Pinned:    d.Approved != nil && installsource.IsContentDigest(d.Approved.ContentHash),
		Installed: installedView(market.InstalledRecords(config.ReasonixHomeDir()), d.Package.Slug),
	})
}

func installedView(held map[string]market.Record, slug string) *marketInstalled {
	rec, ok := held[slug]
	if !ok {
		return nil
	}
	return &marketInstalled{Version: rec.Version, ContentHash: rec.ContentHash}
}

func (s *Server) marketPlan(w http.ResponseWriter, r *http.Request) {
	s.marketRun(w, r, false)
}

func (s *Server) marketInstall(w http.ResponseWriter, r *http.Request) {
	s.marketRun(w, r, true)
}

func (s *Server) marketRun(w http.ResponseWriter, r *http.Request, apply bool) {
	var req market.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	svc := &market.Service{
		Registry:     s.marketClient(),
		Home:         config.ReasonixHomeDir(),
		NewInstaller: market.NewInstallSource(s.ctl().WorkspaceRoot(), marketHTTP(), s.ctl().DisconnectMCPServer),
	}
	if apply {
		if key := s.marketInstallKey(r); key != "" {
			svc.Report, svc.InstallKey = marketReporter(marketHTTP()), key
		}
	}
	run := svc.Plan
	if apply {
		run = svc.Install
	}
	out, err := run(r.Context(), req)
	if err != nil {
		refuseMarket(w, err)
		return
	}
	s.writeMarketOutcome(w, r, out, req.Slug, apply)
}

func (s *Server) writeMarketOutcome(w http.ResponseWriter, r *http.Request, out market.Outcome, slug string, apply bool) {
	out.Fields["slug"], _ = json.Marshal(slug)
	out.Fields["unreviewed"], _ = json.Marshal(out.Unreviewed)
	out.Fields["version"], _ = json.Marshal(out.Version.Version)
	if apply && jsonTrue(out.Fields["applied"]) {
		if err := s.reloadExtensions(r.Context()); err != nil {
			out.Fields["reloadError"], _ = json.Marshal(err.Error())
		}
	}
	writeJSON(w, out.Fields)
}

// refuseMarket gives each way a market request fails its own code: the
// registry being down, the registry answering nonsense, an entry that cannot
// be installed as reviewed, and content that moved since review are four
// different next steps.
func refuseMarket(w http.ResponseWriter, err error) {
	detail := map[string]any{"detail": err.Error()}
	switch {
	case errors.Is(err, market.ErrBadSlug):
		refuse(w, http.StatusBadRequest, "market.bad_slug", "not a package slug", detail)
	case errors.Is(err, market.ErrNotFound):
		refuse(w, http.StatusNotFound, "market.not_found", "no approved package has that name", detail)
	case errors.Is(err, market.ErrFilterUnsupported):
		refuse(w, http.StatusBadGateway, "market.filter_unsupported", "the community registry cannot list installable packages only", detail)
	case errors.Is(err, market.ErrUnreachable):
		refuse(w, http.StatusBadGateway, "market.unreachable", "the community registry could not be reached", detail)
	case errors.Is(err, market.ErrBadResponse):
		refuse(w, http.StatusBadGateway, "market.bad_response", "the community registry answered unexpectedly", detail)
	case errors.Is(err, market.ErrUnpinned):
		refuse(w, http.StatusConflict, "market.unpinned", "the approved version is not pinned to reviewed content", detail)
	case errors.Is(err, market.ErrUnpreviewed):
		refuse(w, http.StatusBadRequest, "market.unpreviewed", "an unreviewed install needs the digest of the preview it confirms", detail)
	case errors.Is(err, market.ErrBadSource):
		refuse(w, http.StatusConflict, "market.bad_source", "the approved version names a source that is not installed from here", detail)
	case errors.Is(err, market.ErrNotTheme):
		refuse(w, http.StatusConflict, "market.not_theme", "the package listed as a theme would install more than themes", detail)
	case errors.Is(err, market.ErrVersionChanged):
		refuse(w, http.StatusConflict, "market.version_changed", "a different version has been approved since this one was shown", detail)
	case errors.Is(err, installsource.ErrDigestMismatch):
		refuse(w, http.StatusConflict, "market.content_changed", "the source no longer holds the reviewed content", detail)
	case errors.Is(err, installsource.ErrNotPinnable):
		refuse(w, http.StatusConflict, "market.not_pinnable", "the source resolves to content that cannot be pinned", detail)
	case errors.Is(err, market.ErrSignedOut):
		refuse(w, http.StatusUnauthorized, "market.signed_out", "sign in to the account first", detail)
	case errors.Is(err, market.ErrEmailUnverified):
		refuse(w, http.StatusForbidden, "market.email_unverified", "verify the account email first", detail)
	case errors.Is(err, market.ErrOwnPackage):
		refuse(w, http.StatusForbidden, "market.own_package", "publishers cannot vote on their own packages", detail)
	case errors.Is(err, market.ErrBadVote):
		refuse(w, http.StatusBadRequest, "market.bad_vote", "a vote is +1, -1 or 0", detail)
	case errors.Is(err, market.ErrRateLimited):
		refuse(w, http.StatusTooManyRequests, "market.rate_limited", "the community registry is throttling requests", detail)
	case errors.Is(err, installsource.ErrApprovalDenied):
		refuse(w, http.StatusConflict, "market.plan_changed", "what the source resolves to changed since the plan was shown", detail)
	default:
		saveFailed(w, http.StatusUnprocessableEntity, "install.failed", err)
	}
}

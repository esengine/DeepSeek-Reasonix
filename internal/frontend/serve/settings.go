package serve

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"reasonix/internal/contract/agentpreset"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
	"reasonix/internal/session/control"
)

func (s *Server) preset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Preset string `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if !agentpreset.IsValid(body.Preset) {
		refuse(w, http.StatusBadRequest, "settings.unknown_preset", "no such preset", nil)
		return
	}
	s.ctl().SetAgentPreset(body.Preset)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) registerModelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /model", s.model)
	mux.HandleFunc("POST /default-model", s.setDefaultModel)
}

func (s *Server) model(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Ref) == "" {
		missingField(w, "ref")
		return
	}
	if err := s.switchModelRequested(r.Context(), strings.TrimSpace(body.Ref)); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setDefaultModel records the model new sessions start on in this machine's user
// config and leaves the session as it is. A pane resolving through a broker
// takes its default from the home machine, so a write here would land in a file
// that pane never reads.
func (s *Server) setDefaultModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Ref) == "" {
		missingField(w, "ref")
		return
	}
	if s.resolver != nil {
		refuse(w, http.StatusConflict, "settings.default_model_brokered", "this pane's default model belongs to the machine its models come from", nil)
		return
	}
	ref := strings.TrimSpace(body.Ref)
	catalog := s.ctl().ProviderCatalog()
	edit := config.LoadForEdit(config.UserConfigPath())
	if !edit.ModelRefSelectable(ref, catalog) {
		refuse(w, http.StatusBadRequest, "settings.unknown_model", "no configured model matches that reference", map[string]any{"model": ref})
		return
	}
	if err := edit.RequireAnswers(ref, config.AnswersChat); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	if err := persistDefaultModel(ref, catalog); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// persistDefaultModel records the choice in the user config.
func persistDefaultModel(ref string, catalog []provider.Descriptor) error {
	path := config.UserConfigPath()
	if path == "" {
		return errNoUserConfig
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	edit := config.LoadForEdit(path)
	if err := edit.SetDefaultModel(ref, catalog); err != nil {
		return err
	}
	return edit.SaveTo(path)
}

var errNoUserConfig = errors.New("no user configuration path")

// autoApproveTools toggles YOLO/full-access tool auto-approval.
func (s *Server) autoApproveTools(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	s.ctl().SetAutoApproveTools(body.On)
	w.WriteHeader(http.StatusNoContent)
}

// toolApprovalMode selects ask, auto, or yolo approval behavior for interactive
// frontends. Plan remains a separate workflow governed by the selected mode.
func (s *Server) toolApprovalMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	mode, ok := control.ParseToolApprovalMode(body.Mode)
	if !ok {
		badValue(w, "mode", "readOnly", "ask", "auto", "dontAsk", "yolo")
		return
	}
	s.applyApprovalMode(mode)
	w.WriteHeader(http.StatusNoContent)
}

// bypass is the legacy HTTP endpoint for YOLO/full-access tool auto-approval.
func (s *Server) bypass(w http.ResponseWriter, r *http.Request) {
	s.autoApproveTools(w, r)
}

// applyApprovalMode switches the live posture and records it in both places it
// has to outlive this pane: the hub, so the next conversation opens in it, and
// the config, so the next launch does. A switch that only lands on the running
// controller reads, either way round, as a choice that was never made.
func (s *Server) applyApprovalMode(mode string) {
	s.ctl().SetToolApprovalMode(mode)
	s.stance.set(mode)
	// A terminal states its posture on its own command line; recording its
	// switch would make a terminal's YOLO the window's next default.
	if s.statsSurface() != surface.CLI {
		persistDesktopApprovalMode(mode)
	}
}

// persistDesktopApprovalMode records the posture chosen on the composer. Same
// reasoning as the model above: without it the next launch reads as the choice
// never having been made. The refusal path is a log, not a failed request — the
// live switch already took effect.
func persistDesktopApprovalMode(mode string) {
	path := config.UserConfigPath()
	if path == "" {
		return
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	edit := config.LoadForEdit(path)
	if err := edit.SetDesktopDefaultToolApprovalMode(mode); err != nil {
		slog.Warn("serve: persist approval mode", "mode", mode, "err", err)
		return
	}
	if err := edit.SaveTo(path); err != nil {
		slog.Warn("serve: save approval mode", "mode", mode, "path", path, "err", err)
	}
}

func (s *Server) effort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Effort string `json:"effort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Effort) == "" {
		missingField(w, "effort")
		return
	}
	if err := s.switchEffort(r.Context(), strings.TrimSpace(body.Effort)); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// status returns a combined status snapshot.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	used, window := s.ctl().ContextSnapshot()
	hit, miss := s.ctl().SessionCache()
	sess := map[string]any{
		"label":            s.ctl().Label(),
		"running":          s.ctl().Running(),
		"plan":             s.ctl().PlanMode(),
		"autoApproveTools": s.ctl().AutoApproveTools(),
		"bypass":           s.ctl().AutoApproveTools(),
		"toolApprovalMode": s.ctl().ToolApprovalMode(),
		"approvalDefault":  s.ctl().Posture(),
		"preset":           s.ctl().AgentPreset(),
		"goal":             s.ctl().Goal(),
		"goalStatus":       s.ctl().GoalStatus(),
		"cwd":              s.ctl().SessionDir(),
		"workspaceRoot":    s.ctl().WorkspaceRoot(),
		"sessionPath":      s.ctl().SessionPath(),
		"used":             used,
		"window":           window,
		"cacheHit":         hit,
		"cacheMiss":        miss,
	}
	// Additive: planPhase absent means outside the workflow, so a client that
	// reads it can say "executing an approved plan" while one that reads only
	// `plan` keeps the behaviour it always had.
	if v, ok := s.ctl().(decisionViewer); ok {
		sess["decisions"] = v.Decisions()
		if phase := v.PlanPhase(); phase != planmode.Inactive {
			sess["planPhase"] = phase.String()
		}
	}
	if u := s.ctl().LastUsage(); u != nil {
		sess["lastUsage"] = u
	}
	if face, ok := s.ctl().ModelFace(); ok {
		sess["effort"] = face.Effort
		sess["modelRef"] = face.Ref
		if label := providerLabel(face.Ref); label != "" {
			sess["providerDisplayName"] = label
		}
		// Whether this model reads images at all. A composer that cannot ask
		// lets the user paste a screenshot into a text-only model and watch
		// nothing happen.
		sess["vision"] = face.Vision
		// And whether that false is an answer or a silence: a relay forwards
		// models nothing here has a label for, and telling its user the model
		// cannot read images states a limitation that was never established.
		sess["visionDeclared"] = face.VisionDeclared
	}
	// Only a model that declares modes lists any, which is what keeps the
	// switch off every other model's effort menu.
	if modes := s.ctl().ModelModes(); len(modes) > 0 {
		sess["modes"] = modes
	}
	sess["sessionCostQuote"] = s.bc.SessionCostQuote()
	if j := s.ctl().Jobs(); len(j) > 0 {
		sess["jobs"] = j
	}
	writeJSON(w, sess)
}

type modelEntry struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider"`
	// DisplayName labels Provider on screen; Ref and Provider stay the identity.
	DisplayName string `json:"displayName,omitempty"`
	Model       string `json:"model"`
	Kind        string `json:"kind,omitempty"`
	// What this model produces, projected from the protocol table so a chooser
	// filters on a declaration rather than on a kind's spelling.
	Answers string `json:"answers,omitempty"`
	Active  bool   `json:"active,omitempty"`
	Default bool   `json:"default,omitempty"`
	// Vendor is the endpoint host. Entries sharing it are one service reached
	// under different protocols, which is what lets a picker fold the routes.
	Vendor string `json:"vendor,omitempty"`
	// KeyEnv is the credential this route spends. It pairs with Vendor to
	// identify the account: one host can hold more than one.
	KeyEnv string `json:"keyEnv,omitempty"`
	// Preset marks a name we shipped rather than one the user chose, so a picker
	// knows whose name Provider is before putting it on screen.
	Preset bool `json:"preset,omitempty"`
	// The capability face; see describeModel. Omitted fields mean "nothing
	// declares this", never "no".
	Vision        bool        `json:"vision,omitempty"`
	Efforts       []string    `json:"efforts,omitempty"`
	Effort        string      `json:"effort,omitempty"`
	ContextWindow int         `json:"contextWindow,omitempty"`
	Price         *modelPrice `json:"price,omitempty"`
	// ForcesThinking tells frontends that even the lowest effort still reasons.
	ForcesThinking bool `json:"forcesThinking,omitempty"`
}

type modelRoute struct {
	key  string
	solo bool
}

// modelRouteKey names where a model is reached: endpoint plus credential slot.
// Two accounts at one endpoint are two routes.
func modelRouteKey(p *config.ProviderEntry, model string) string {
	return strings.ToLower(strings.TrimRight(p.BaseURL, "/")) + "\x00" + strings.TrimSpace(p.APIKeyEnv) + "\x00" + model
}

// collapseModelRoutes drops entries naming the same model at the same endpoint.
// A multi-model provider block and a single-model block pinning one of them (to
// attach its own price) are two config rows, not two models. The survivor is the
// active one, so the current selection stays selectable, then the default, then
// the single-model block, whose price table is the exact one.
func collapseModelRoutes(entries []modelEntry, routes []modelRoute) []modelEntry {
	if len(routes) == 0 {
		return entries
	}
	best := make(map[string]int, len(routes))
	for i := range routes {
		j, seen := best[routes[i].key]
		if !seen || betterModelRoute(entries[i], routes[i], entries[j], routes[j]) {
			best[routes[i].key] = i
		}
	}
	kept := entries[:0:0]
	for i := range entries {
		if i < len(routes) && best[routes[i].key] != i {
			continue
		}
		kept = append(kept, entries[i])
	}
	return kept
}

func betterModelRoute(a modelEntry, ar modelRoute, b modelEntry, br modelRoute) bool {
	if a.Active != b.Active {
		return a.Active
	}
	if a.Default != b.Default {
		return a.Default
	}
	return ar.solo && !br.solo
}

// decisionViewer is the projection surface: what waits on the user, and where
// the run sits in the plan lifecycle. Reading it changes nothing. It is an
// optional capability rather than a port method because only this frontend
// renders it today.
type decisionViewer interface {
	PlanPhase() planmode.Phase
	Decisions() []control.Decision
}

// providerLabel reads the label live from the user's file: a rename rebuilds
// nothing, so the build's snapshot of the entry cannot carry it.
func providerLabel(ref string) string {
	name, _, _ := strings.Cut(ref, "/")
	if p, ok := config.LoadForEdit(config.UserConfigPath()).Provider(name); ok {
		return strings.TrimSpace(p.DisplayName)
	}
	return ""
}

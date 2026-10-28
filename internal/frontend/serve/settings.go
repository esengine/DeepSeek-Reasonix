package serve

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/contract/agentpreset"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/jobs"
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

// sessionStatus is GET /status as a value: every key it sends, named. It was a
// map literal, whose shape no guard can read — which is how `busy` came to be
// sent without the desktop declaring it. Fields are alphabetical by JSON name,
// the order a map's keys marshal in; reordering them changes the bytes of an
// identical snapshot, and TestStatusPayloadSendsTheBytesTheMapSent pins that.
type sessionStatus struct {
	AgentComposition       *compositionSummary   `json:"agentComposition,omitempty"`
	AgentCompositionLocked bool                  `json:"agentCompositionLocked"`
	ApprovalDefault        control.PostureReport `json:"approvalDefault"`
	AutoApproveTools       bool                  `json:"autoApproveTools"`
	// Busy is the admission gate; Running is the turn. They differ for as long
	// as the previous turn's TurnDone is still being delivered, and that gap is
	// the one state a window cannot describe from Running alone.
	Busy      bool   `json:"busy"`
	Bypass    bool   `json:"bypass"`
	CacheHit  int    `json:"cacheHit"`
	CacheMiss int    `json:"cacheMiss"`
	Cwd       string `json:"cwd"`
	// A pointer to the slice: the key is absent from a controller projecting no
	// decisions and [] from one projecting an empty set, which omitempty on a
	// bare slice would collapse into one silence.
	Decisions *[]control.Decision `json:"decisions,omitempty"`
	// The model face arrives or does not as a whole; once it does, its four keys
	// keep their zero values. Pointers, because a bare string or bool under
	// omitempty would turn a stated zero into an absent key.
	Effort              *string                  `json:"effort,omitempty"`
	Goal                string                   `json:"goal"`
	GoalRuntime         *control.GoalRuntimeView `json:"goalRuntime,omitempty"`
	GoalStatus          string                   `json:"goalStatus"`
	Jobs                []jobs.View              `json:"jobs,omitempty"`
	Label               string                   `json:"label"`
	LastUsage           *provider.Usage          `json:"lastUsage,omitempty"`
	ModelRef            *string                  `json:"modelRef,omitempty"`
	Modes               []control.ModelModeView  `json:"modes,omitempty"`
	Plan                bool                     `json:"plan"`
	PlanPhase           string                   `json:"planPhase,omitempty"`
	Preset              string                   `json:"preset"`
	ProviderDisplayName string                   `json:"providerDisplayName,omitempty"`
	Running             bool                     `json:"running"`
	SessionCostQuote    pricing.CostQuote        `json:"sessionCostQuote"`
	SessionPath         string                   `json:"sessionPath"`
	SessionUsage        SessionUsage             `json:"sessionUsage"`
	Tariff              map[string]any           `json:"tariff,omitempty"`
	ToolApprovalMode    string                   `json:"toolApprovalMode"`
	Used                int                      `json:"used"`
	Vision              *bool                    `json:"vision,omitempty"`
	VisionDeclared      *bool                    `json:"visionDeclared,omitempty"`
	Window              int                      `json:"window"`
	WorkspaceRoot       string                   `json:"workspaceRoot"`
}

// statusFacts is what GET /status reads, gathered once. Shaping it is a
// separate, pure step so the golden test can build both this build's payload
// and the map it replaced from one set of inputs: a key dropped on either side
// is then a diff, not a coincidence.
type statusFacts struct {
	AgentCompositionLocked bool
	Label                  string
	Running                bool
	Busy                   bool
	Plan                   bool
	AutoApproveTools       bool
	ToolApprovalMode       string
	Posture                control.PostureReport
	Preset                 string
	Goal                   string
	GoalStatus             string
	Cwd                    string
	WorkspaceRoot          string
	SessionPath            string
	Used                   int
	Window                 int
	CacheHit               int
	CacheMiss              int
	Composition            *compositionSummary
	GoalRuntime            *control.GoalRuntimeView
	Decisions              *[]control.Decision
	PlanPhase              planmode.Phase
	LastUsage              *provider.Usage
	Face                   *control.ModelFace
	ProviderDisplayName    string
	Modes                  []control.ModelModeView
	SessionCostQuote       pricing.CostQuote
	SessionUsage           SessionUsage
	Jobs                   []jobs.View
	Tariff                 *pricing.Tariff
}

// statusFacts reads the session once for GET /status.
func (s *Server) statusFacts() statusFacts {
	ctl := s.ctl()
	used, window := ctl.ContextSnapshot()
	hit, miss := ctl.SessionCache()
	facts := statusFacts{
		AgentCompositionLocked: compositionLocked(ctl),
		Label:                  ctl.Label(),
		Running:                ctl.TurnInFlight(),
		Busy:                   ctl.Running(),
		Plan:                   ctl.PlanMode(),
		AutoApproveTools:       ctl.AutoApproveTools(),
		ToolApprovalMode:       ctl.ToolApprovalMode(),
		Posture:                ctl.Posture(),
		Preset:                 ctl.AgentPreset(),
		Goal:                   ctl.Goal(),
		GoalStatus:             ctl.GoalStatus(),
		Cwd:                    ctl.SessionDir(),
		WorkspaceRoot:          ctl.WorkspaceRoot(),
		SessionPath:            ctl.SessionPath(),
		Used:                   used,
		Window:                 window,
		CacheHit:               hit,
		CacheMiss:              miss,
		Composition:            compositionControllerView(ctl),
		GoalRuntime:            s.goalRuntime(),
	}
	// Additive: planPhase absent means outside the workflow, so a client that
	// reads it can say "executing an approved plan" while one that reads only
	// `plan` keeps the behaviour it always had.
	if v, ok := ctl.(decisionViewer); ok {
		decisions := v.Decisions()
		facts.Decisions = &decisions
		facts.PlanPhase = v.PlanPhase()
	}
	facts.LastUsage = ctl.LastUsage()
	if face, ok := ctl.ModelFace(); ok {
		facts.Face = &face
		facts.ProviderDisplayName = providerLabel(face.Ref)
	}
	facts.Modes = ctl.ModelModes()
	facts.SessionCostQuote = s.bc.SessionCostQuote()
	facts.SessionUsage = s.bc.SessionUsage()
	facts.Jobs = ctl.Jobs()
	// Additive: present only where the vendor's own schedule bills the model's
	// rate, so a custom price or a relay shows no window it does not charge.
	if tariff, ok := ctl.Tariff(); ok {
		facts.Tariff = &tariff
	}
	return facts
}

// view is the wire shape: one field per key, in the order the JSON encoder
// writes them. The conditionals are the same ones the map literal applied, so
// an absent key still means exactly what it meant.
func (f statusFacts) view() sessionStatus {
	v := sessionStatus{
		AgentCompositionLocked: f.AgentCompositionLocked,
		ApprovalDefault:        f.Posture,
		AutoApproveTools:       f.AutoApproveTools,
		Busy:                   f.Busy,
		// A second key for the same fact, kept because a client may read
		// either name; it is not a second opinion.
		Bypass:           f.AutoApproveTools,
		CacheHit:         f.CacheHit,
		CacheMiss:        f.CacheMiss,
		Cwd:              f.Cwd,
		Goal:             f.Goal,
		GoalStatus:       f.GoalStatus,
		Label:            f.Label,
		Plan:             f.Plan,
		Preset:           f.Preset,
		Running:          f.Running,
		SessionCostQuote: f.SessionCostQuote,
		SessionPath:      f.SessionPath,
		SessionUsage:     f.SessionUsage,
		ToolApprovalMode: f.ToolApprovalMode,
		Used:             f.Used,
		Window:           f.Window,
		WorkspaceRoot:    f.WorkspaceRoot,
		AgentComposition: f.Composition,
		GoalRuntime:      f.GoalRuntime,
		LastUsage:        f.LastUsage,
	}
	if f.Decisions != nil {
		v.Decisions = f.Decisions
		if f.PlanPhase != planmode.Inactive {
			v.PlanPhase = f.PlanPhase.String()
		}
	}
	if f.Face != nil {
		effort, ref := f.Face.Effort, f.Face.Ref
		vision, declared := f.Face.Vision, f.Face.VisionDeclared
		v.Effort, v.ModelRef = &effort, &ref
		// Whether this model reads images at all, and whether that answer was
		// ever given: a relay forwards models nothing here has a label for, and
		// an undeclared one is not the same claim as a text-only one.
		v.Vision, v.VisionDeclared = &vision, &declared
		if f.ProviderDisplayName != "" {
			v.ProviderDisplayName = f.ProviderDisplayName
		}
	}
	// Only a model that declares modes lists any, which is what keeps the switch
	// off every other model's effort menu.
	if len(f.Modes) > 0 {
		v.Modes = f.Modes
	}
	if len(f.Jobs) > 0 {
		v.Jobs = f.Jobs
	}
	if f.Tariff != nil {
		v.Tariff = tariffBody(*f.Tariff)
	}
	return v
}

// status returns a combined status snapshot.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.statusFacts().view())
}

type modelEntry struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider"`
	// DisplayName labels Provider on screen; Ref and Provider stay the identity.
	DisplayName string `json:"displayName,omitempty"`
	Model       string `json:"model"`
	ModelAlias  string `json:"modelAlias,omitempty"`
	// ModelShort is the model drawn with its entry's shared prefix left out, when
	// that was asked for; empty draws the model as it is.
	ModelShort string `json:"modelShort,omitempty"`
	Kind       string `json:"kind,omitempty"`
	// What this model produces, projected from the protocol table so a chooser
	// filters on a declaration rather than on a kind's spelling.
	Answers string `json:"answers,omitempty"`
	Active  bool   `json:"active,omitempty"`
	Default bool   `json:"default,omitempty"`
	// NotAdded marks a connection outside the desktop's access list: it is
	// configured, but not one the person added, so a role cannot pick it.
	NotAdded bool `json:"notAdded,omitempty"`
	// Vendor is the endpoint host. Entries sharing it are one service reached
	// under different protocols, which is what lets a picker fold the routes.
	Vendor string `json:"vendor,omitempty"`
	// Mark is the vendor the curated preset this provider was created from
	// declares, for the client to draw a mark. Empty for a provider the person
	// wrote by hand: nothing declares its vendor, so the client does not guess.
	Mark string `json:"mark,omitempty"`
	// ServiceMark is the same vendor for the service heading. It differs from
	// Mark only on a relay, whose models are made by someone else.
	ServiceMark string `json:"serviceMark,omitempty"`
	// KeyEnv is the credential this route spends. It pairs with Vendor to
	// identify the account: one host can hold more than one.
	KeyEnv string `json:"keyEnv,omitempty"`
	// Preset marks a name we shipped rather than one the user chose, so a picker
	// knows whose name Provider is before putting it on screen.
	Preset bool `json:"preset,omitempty"`
	// The capability face; see describeModel. Omitted fields mean "nothing
	// declares this", never "no".
	Vision        bool        `json:"vision,omitempty"`
	WebSearch     bool        `json:"webSearch,omitempty"`
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

// goalRuntime is a goal's usage while there is a goal to account for; a session
// without one reports none, so a window draws nothing for it.
func (s *Server) goalRuntime() *control.GoalRuntimeView {
	if s.ctl().Goal() == "" {
		return nil
	}
	view := s.ctl().GoalRuntime()
	return &view
}

// tariffBody is the vendor's billing window as a client draws it. ChangesAt is
// absent when the schedule has no flip ahead, so a client says nothing about one.
func tariffBody(t pricing.Tariff) map[string]any {
	body := map[string]any{"peak": t.Peak}
	if t.Changes {
		body["changesAt"] = t.ChangesAt.UTC().Format(time.RFC3339)
	}
	return body
}

// presetVendor is the declared vendor of the curated preset a provider came
// from, read through the provenance the entry already records, and whether the
// preset is a relay of other makers' models.
func presetVendor(p *config.ProviderEntry) (vendor string, relay bool) {
	if p == nil || p.PresetID == "" {
		return "", false
	}
	if preset, ok := config.CuratedProviderPreset(p.PresetID); ok {
		return preset.Vendor, preset.Relay
	}
	return "", false
}

func providerMark(p *config.ProviderEntry) string {
	vendor, _ := presetVendor(p)
	return vendor
}

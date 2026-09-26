// providers.go — adding, listing and removing model providers.
package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reasonix/internal/model/catalog"
	"regexp"
	"slices"
	"strings"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
)

// hostGrants are the surfaces a host opens on behalf of its one local client.
// They travel together because they are one decision — "this server is a window,
// not a network service" — and three independent bools spell states no host
// means: signing in but not switching folders, editing providers but not either.
type hostGrants struct {
	workspaceSwitch bool // POST /workspace; see AllowWorkspaceSwitch
	accountAuth     bool // /account routes; see AllowAccountAuth
	providerEdit    bool // /providers writes; see AllowProviderEdit
	localDesktop    bool // POST /workspace/editor, GET /workspace/locate; see AllowLocalDesktop
}

// AllowProviderEdit grants the /providers routes. Off until a host asks:
// adding one writes an API key into the credential store of the machine
// running the kernel, so a server reachable over the network must not let a
// client do it. The desktop shell asks because its only client is its window.
func (s *Server) AllowProviderEdit() { s.grants.providerEdit = true }

func (s *Server) registerProviderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /providers", s.providers)
	mux.HandleFunc("GET /providers/protocols", s.providerProtocols)
	mux.HandleFunc("POST /providers", s.saveProvider)
	mux.HandleFunc("POST /providers/probe", s.probeProvider)
	mux.HandleFunc("POST /providers/remove", s.removeProvider)
	mux.HandleFunc("POST /providers/edit", s.editProvider)
	mux.HandleFunc("POST /providers/websearch", s.setProviderWebSearch)
	mux.HandleFunc("POST /providers/thinking", s.setProviderThinking)
	mux.HandleFunc("POST /providers/continuation", s.setProviderContinuation)
	s.registerProviderCheckRoutes(mux)
}

const providerProbeTimeout = 20 * time.Second

// providerNameRE is what may become a config table name and part of a model
// ref ("<provider>/<model>"), so a slash or whitespace cannot be allowed in.
var providerNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// providerView is one configured provider as the panel lists it.
type providerView struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	BaseURL string   `json:"baseUrl"`
	Models  []string `json:"models"`
	// VisionModels is which of them read images, so the form shows the current
	// answer instead of asking the user to remember it.
	VisionModels []string `json:"visionModels"`
	// CanSetVision is false where the kernel refuses image input for every model
	// on this endpoint whatever the config says — an editor offering the toggle
	// there is offering a switch that does nothing.
	CanSetVision bool `json:"canSetVision"`
	// VisionSettable narrows that to the models the refusal does not cover. One
	// endpoint now serves text-only and image-taking models side by side, so a
	// single boolean per connection answers for models it was never asked about.
	VisionSettable []string `json:"visionSettable"`
	// WebSearch is the endpoint-executed search tool: CanWebSearch says this
	// door offers one at all, WebSearch whether it is on. The OpenAI chat wire
	// has no format for it, so the answer differs per protocol on one account.
	CanWebSearch bool `json:"canWebSearch"`
	WebSearch    bool `json:"webSearch"`
	// SendsThinking is whether thinking/reasoning_effort may go on the wire.
	// CanSetThinking is false where the protocol never carries them, so the UI
	// offers the switch only where a gateway can actually reject the request.
	CanSetThinking bool `json:"canSetThinking"`
	SendsThinking  bool `json:"sendsThinking"`
	// Continuation is how this endpoint carries context between turns, and
	// CanSetContinuation whether its protocol has the choice at all. Empty is
	// vendor detection, which is what an uncharacterised endpoint must keep.
	CanSetContinuation bool   `json:"canSetContinuation"`
	Continuation       string `json:"continuation"`
	Default            string `json:"default"`
	HasKey             bool   `json:"hasKey"`
	// KeyEnv names the credential slot. Two entries at one host holding
	// different keys are two accounts and must not be shown as one.
	KeyEnv string `json:"keyEnv,omitempty"`
	// InUse marks the provider the running conversation is on. Removing it
	// would leave the session pointing at a model that no longer exists.
	InUse bool `json:"inUse"`
	// Preset marks an entry that came from the curated catalog rather than
	// from this panel, so the UI can say where it came from.
	Preset bool `json:"preset"`
	// The three no probe can answer: a wrong window moves compaction to the
	// wrong moment, and a relay without its headers refuses every request.
	ContextWindow   int               `json:"contextWindow,omitempty"`
	MaxOutputTokens int               `json:"maxOutputTokens,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	ExtraBody       map[string]any    `json:"extraBody,omitempty"`
	// Which request shape controls thinking here, as declared. Empty is "not
	// declared", which is a different answer from "none" and the reason the
	// effort ladder can come out empty on a relay.
	ReasoningProtocol string `json:"reasoningProtocol,omitempty"`
	// The declared effort vocabulary and its default, as stored; empty is none.
	SupportedEfforts []string `json:"supportedEfforts,omitempty"`
	DefaultEffort    string   `json:"defaultEffort,omitempty"`
}

func (s *Server) providers(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	current, _, _ := strings.Cut(currentModelRef(s.ctl()), "/")
	out := make([]providerView, 0, len(cfg.Providers))
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		settable := visionSettableOf(cfg, p)
		out = append(out, providerView{
			Name:               p.Name,
			Kind:               strings.ToLower(strings.TrimSpace(p.Kind)),
			BaseURL:            p.BaseURL,
			Models:             nonNilStrings(p.ChatModelList()),
			VisionModels:       nonNilStrings(visionModelsOf(cfg, p)),
			CanSetVision:       len(settable) > 0 || config.CanConfigureVision(p),
			VisionSettable:     nonNilStrings(settable),
			CanWebSearch:       config.HasServerWebSearchCapability(p),
			WebSearch:          config.EffectiveWebSearch(p),
			CanSetThinking:     config.CanConfigureThinkingParams(p),
			SendsThinking:      config.SendsThinkingParams(p),
			CanSetContinuation: config.CanConfigureContinuation(p),
			Continuation:       string(config.ContinuationOf(p)),
			Default:            p.DefaultModel(),
			HasKey:             p.APIKey() != "",
			KeyEnv:             p.APIKeyEnv,
			InUse:              p.Name == current,
			Preset:             strings.TrimSpace(p.PresetID) != "",
			ReasoningProtocol:  strings.ToLower(strings.TrimSpace(p.ReasoningProtocol)),
			SupportedEfforts:   config.StoredEffortLevels(p.SupportedEfforts),
			DefaultEffort:      strings.ToLower(strings.TrimSpace(p.DefaultEffort)),
			ContextWindow:      p.ContextWindow,
			MaxOutputTokens:    p.MaxOutputTokens,
			Headers:            p.Headers,
			ExtraBody:          p.ExtraBody,
		})
	}
	writeJSON(w, out)
}

// protocolView is one wire format a source may be saved as. No label rides
// along: the list is the kernel's, the words for it are each frontend's.
type protocolView struct {
	Kind            string `json:"kind"`
	Answers         string `json:"answers"`
	Discovery       string `json:"discovery"`
	ServerWebSearch bool   `json:"serverWebSearch"`
	ReasoningParams bool   `json:"reasoningParams"`
}

// providerProtocols lists what a chooser may offer, so a wire added to the
// kernel reaches every panel without a frontend release.
func (s *Server) providerProtocols(w http.ResponseWriter, _ *http.Request) {
	catalog := config.Protocols()
	out := make([]protocolView, 0, len(catalog))
	for _, p := range catalog {
		out = append(out, protocolView{
			Kind:            p.Kind,
			Answers:         string(p.Answers),
			Discovery:       p.Discovery,
			ServerWebSearch: p.ServerWebSearch,
			ReasoningParams: p.ReasoningParams,
		})
	}
	writeJSON(w, out)
}

// probeProvider reports what an endpoint turns out to be. It writes nothing:
// the user sees the guesses and confirms them before anything is saved.
func (s *Server) probeProvider(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		BaseURL string `json:"baseUrl"`
		APIKey  string `json:"apiKey"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), providerProbeTimeout)
	defer cancel()
	proxied, direct := probeClients()
	got, err := catalog.ProbeEndpoint(ctx, catalog.ProbeOptions{
		BaseURL: body.BaseURL,
		APIKey:  body.APIKey,
		Client:  proxied,
		Direct:  direct,
	})
	if err != nil {
		writeProbeFailure(w, err)
		return
	}
	writeJSON(w, struct {
		Kind       string   `json:"kind"`
		Kinds      []string `json:"kinds"`
		AuthHeader bool     `json:"authHeader"`
		Models     []string `json:"models"`
		Default    string   `json:"default"`
		Efforts    []string `json:"efforts"`
		Effort     string   `json:"effort"`
		Vision     []string `json:"vision"`
		Ambiguous  bool     `json:"ambiguous"`
		NoProxy    bool     `json:"noProxy"`
	}{
		Kind:       got.Kind,
		Kinds:      nonNilStrings(got.Kinds),
		AuthHeader: got.AuthHeader,
		Models:     nonNilStrings(got.Models),
		Default:    got.Default,
		Efforts:    nonNilStrings(got.Efforts),
		Effort:     got.Effort,
		Vision:     nonNilStrings(got.Vision),
		Ambiguous:  got.Ambiguous,
		NoProxy:    got.NoProxy,
	})
}

// saveProvider writes one provider and its key. The key goes to the credential
// store, never into the config file — a config is something a user pastes into
// an issue.
func (s *Server) saveProvider(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Name              string            `json:"name"`
		Kind              string            `json:"kind"`
		BaseURL           string            `json:"baseUrl"`
		APIKey            string            `json:"apiKey"`
		Models            []string          `json:"models"`
		Default           string            `json:"default"`
		AuthHeader        bool              `json:"authHeader"`
		NoProxy           bool              `json:"noProxy"`
		Effort            string            `json:"effort"`
		Vision            []string          `json:"vision"`
		ContextWindow     int               `json:"contextWindow"`
		MaxOutputTokens   int               `json:"maxOutputTokens"`
		ReasoningProtocol *string           `json:"reasoningProtocol"`
		Headers           map[string]string `json:"headers"`
		ExtraBody         map[string]any    `json:"extraBody"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	entry, err := providerEntryFrom(body.Name, body.Kind, body.BaseURL, body.Default, body.Effort, body.Models, body.Vision, body.AuthHeader, body.NoProxy)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.ContextWindow < 0 || body.MaxOutputTokens < 0 {
		refuse(w, http.StatusBadRequest, "provider.bad_token_limit", "token limits cannot be negative", nil)
		return
	}
	entry.ContextWindow = body.ContextWindow
	entry.MaxOutputTokens = body.MaxOutputTokens
	if body.ReasoningProtocol != nil {
		stored, ok := config.StoredReasoningProtocol(*body.ReasoningProtocol)
		if !ok {
			refuse(w, http.StatusBadRequest, "provider.bad_reasoning_protocol", "unsupported reasoning protocol", map[string]any{"protocol": *body.ReasoningProtocol})
			return
		}
		entry.ReasoningProtocol = stored
	}
	entry.Headers = trimmedHeaders(body.Headers)
	if path, ok := firstNullPath(body.ExtraBody, ""); ok {
		refuse(w, http.StatusBadRequest, "provider.extra_body_null", fmt.Sprintf("extra body field %q cannot be null", path), map[string]any{"path": path})
		return
	}
	entry.ExtraBody = body.ExtraBody
	entry.APIKeyEnv = keyEnvForNewSource(entry.Name, entry.BaseURL, body.APIKey)
	if key := strings.TrimSpace(body.APIKey); key != "" {
		if _, err := config.SetCredential(entry.APIKeyEnv, key); err != nil {
			// The slot is derived from the name, so this is the person's to
			// fix and the name is the field to point them at — not the key,
			// which is what an unclassified failure here reads as.
			if errors.Is(err, config.ErrInvalidCredentialKey) {
				refuse(w, http.StatusBadRequest, "provider.bad_key_slot",
					"the name does not make a usable credential slot",
					map[string]any{"name": entry.Name, "slot": entry.APIKeyEnv})
				return
			}
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.UpsertProvider(entry); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// No rebuild: the model list and every switch read config from disk, so the
	// new provider is selectable on the next call without disturbing the
	// conversation that is running.
	writeJSON(w, map[string]any{"name": entry.Name, "models": nonNilStrings(entry.ModelList())})
}

func (s *Server) removeProvider(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		refuse(w, http.StatusBadRequest, "provider.name_required", "a provider name is required", nil)
		return
	}
	// The conversation on this provider moves to what remains once it is gone.
	// Only work in progress stops that; an open, idle pane does not.
	current, _, _ := strings.Cut(currentModelRef(s.ctl()), "/")
	inUse := current == name
	if inUse && controllerHasActiveRuntimeWork(s.ctl()) {
		busy(w, "provider.running", "the conversation on this model is running; stop it first", nil)
		return
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.RemoveProvider(name); err != nil {
		switch {
		case errors.Is(err, config.ErrProviderNotFound):
			// Already gone is the state the caller asked for: a second click, or
			// another window that got there first.
			w.WriteHeader(http.StatusNoContent)
		default:
			writeErr(w, http.StatusBadRequest, err)
		}
		return
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// With nothing left there is no model to move to; the window finds the
	// empty config and asks for a connection, as it does on a first launch.
	if inUse && cfg.DefaultModel != "" {
		if err := s.switchModel(r.Context(), cfg.DefaultModel); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// providerEntryFrom validates what the panel sent and builds the config entry.
// Only the fields the panel can honestly fill are set; everything else keeps
// its zero value so a hand-edited config is not silently overwritten.
func providerEntryFrom(name, kind, baseURL, def, effort string, models, vision []string, authHeader, noProxy bool) (config.ProviderEntry, error) {
	name = strings.TrimSpace(name)
	if !providerNameRE.MatchString(name) {
		return config.ProviderEntry{}, refusal(http.StatusUnprocessableEntity, "provider.name_invalid", errors.New("provider name must be letters, digits, dot, dash or underscore"), nil)
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !config.SupportedProviderKind(kind) {
		return config.ProviderEntry{}, refusal(http.StatusUnprocessableEntity, "provider.kind_unsupported", fmt.Errorf("unsupported provider kind %q", kind), map[string]any{"kind": kind})
	}
	if strings.TrimSpace(baseURL) == "" {
		return config.ProviderEntry{}, refusal(http.StatusUnprocessableEntity, "provider.endpoint_required", errors.New("an endpoint address is required"), nil)
	}
	chat := make([]string, 0, len(models))
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" {
			chat = append(chat, m)
		}
	}
	if len(chat) == 0 {
		return config.ProviderEntry{}, refusal(http.StatusUnprocessableEntity, "provider.no_models_picked", errors.New("pick at least one model"), nil)
	}
	def = strings.TrimSpace(def)
	if def != "" && !slices.Contains(chat, def) {
		return config.ProviderEntry{}, refusal(http.StatusUnprocessableEntity, "provider.default_not_selected", fmt.Errorf("default model %q is not one of the selected models", def), map[string]any{"model": def})
	}
	return config.ProviderEntry{
		Name:         name,
		Kind:         kind,
		BaseURL:      strings.TrimSpace(baseURL),
		Models:       chat,
		Default:      def,
		APIKeyEnv:    providerKeyEnv(name),
		AuthHeader:   authHeader,
		NoProxy:      noProxy,
		Effort:       strings.TrimSpace(effort),
		VisionModels: vision,
	}, nil
}

// providerKeyEnv is where this provider's key is stored. config owns the rule,
// because the store that refuses an invalid slot is the same package: this held
// its own copy and produced "129_API_KEY" for a relay called "129".
func providerKeyEnv(name string) string { return config.APIKeyEnvFor(name) }

// probeClients builds the two routes a probe tries: the user's configured proxy
// first, then a direct one for endpoints that only answer without it.
func probeClients() (proxied, direct *http.Client) {
	spec := netclient.ProxySpec{Mode: netclient.ModeAuto}
	if cfg, err := config.Load(); err == nil && cfg != nil {
		spec = cfg.NetworkProxySpec()
	}
	proxied, _ = netclient.NewHTTPClient(spec, netclient.TransportOptions{})
	direct, _ = netclient.NewHTTPClient(netclient.ProxySpec{Mode: netclient.ModeOff}, netclient.TransportOptions{})
	return proxied, direct
}

func decodeProviderBody(w http.ResponseWriter, r *http.Request, into any) bool {
	return decodeBody(w, r, into)
}

// nonNilStrings keeps an empty list an empty list. A nil slice marshals to
// null, and a client that maps over it takes its whole render down.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Probing fails in ways that each send the user somewhere different: a refused
// key to the console, a 404 to the address bar, an embedding-only gateway to a
// different service entirely. The identity travels; the wording does not.
const (
	codeProbeAddressMissing  = "provider.probe.address_missing"
	codeProbeUnauthorized    = "provider.probe.unauthorized"
	codeProbePaymentRequired = "provider.probe.payment_required"
	codeProbeRateLimited     = "provider.probe.rate_limited"
	codeProbePathNotFound    = "provider.probe.path_not_found"
	codeProbeNoChatModels    = "provider.probe.no_chat_models"
	codeProbeUpstreamError   = "provider.probe.upstream_error"
	codeProbeUnreachable     = "provider.probe.unreachable"
	codeProbeNotCompatible   = "provider.probe.not_compatible"
)

// writeProbeFailure sends the diagnosis out under its own code. Spelled as a
// switch rather than by building the code from the reason: the parity guard
// reads these call sites, and a concatenated code is one it cannot check.
func writeProbeFailure(w http.ResponseWriter, err error) {
	var probe *catalog.ProbeError
	if !errors.As(err, &probe) {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	msg, params := probe.Error(), probe.Params
	switch probe.Reason {
	case catalog.ProbeAddressMissing:
		refuse(w, http.StatusBadRequest, codeProbeAddressMissing, msg, params)
	case catalog.ProbeUnauthorized:
		refuse(w, http.StatusUnauthorized, codeProbeUnauthorized, msg, params)
	case catalog.ProbePaymentRequired:
		refuse(w, http.StatusPaymentRequired, codeProbePaymentRequired, msg, params)
	case catalog.ProbeRateLimited:
		refuse(w, http.StatusTooManyRequests, codeProbeRateLimited, msg, params)
	case catalog.ProbePathNotFound:
		refuse(w, http.StatusNotFound, codeProbePathNotFound, msg, params)
	case catalog.ProbeNoChatModels:
		refuse(w, http.StatusUnprocessableEntity, codeProbeNoChatModels, msg, params)
	case catalog.ProbeUpstreamError:
		refuse(w, http.StatusBadGateway, codeProbeUpstreamError, msg, params)
	case catalog.ProbeUnreachable:
		refuse(w, http.StatusBadGateway, codeProbeUnreachable, msg, params)
	default:
		refuse(w, http.StatusBadGateway, codeProbeNotCompatible, msg, params)
	}
}

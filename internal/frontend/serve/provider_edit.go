// provider_edit.go — changing a source without flattening what it already knows.
package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
	"reasonix/internal/model/billing"
)

// editProvider changes only the fields this panel owns and leaves the rest of
// the entry alone. UpsertProvider replaces an entry wholesale, so building one
// from the form would drop every field the form cannot show — per-model prices,
// effort vocabularies, context windows, headers, the preset it came from.
func (s *Server) editProvider(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body providerEditBody
	if !decodeProviderBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider(name)
	if !ok {
		notFound(w, "provider", name)
		return
	}
	assembled, wasListed := assemblyShape(entry), s.runsListedModelOf(entry)
	models := trimmedNonEmpty(body.Models)
	if len(models) == 0 {
		refuse(w, http.StatusBadRequest, "provider.no_models_picked", "pick at least one model", nil)
		return
	}
	def := strings.TrimSpace(body.Default)
	if def != "" && !slices.Contains(models, def) {
		refuse(w, http.StatusBadRequest, "provider.default_not_selected", "the default is not one of the selected models", map[string]any{"model": def})
		return
	}
	if base := strings.TrimSpace(body.BaseURL); base != "" {
		entry.BaseURL = base
	}
	if !applyKind(w, entry, body.Kind) {
		return
	}
	entry.Models = models
	entry.Model = ""
	entry.Default = def
	if bad := applyModelAliases(entry, body.ModelAliases); bad != nil {
		refuse(w, http.StatusBadRequest, bad.code, bad.message, bad.detail)
		return
	}
	if body.HideModelPrefix != nil {
		entry.HideModelPrefix = *body.HideModelPrefix
	}
	applyVisionSelection(entry, trimmedNonEmpty(body.Vision))

	if !applyNumericFields(w, entry, body.ContextWindow, body.MaxOutputTokens, body.IdleTimeoutSeconds) {
		return
	}
	if bad := applyModelDeclarations(entry, models, body.ModelLimits, body.ModelPrices); bad != nil {
		refuse(w, http.StatusBadRequest, bad.code, bad.message, bad.detail)
		return
	}
	if body.Headers != nil {
		entry.Headers = trimmedHeaders(*body.Headers)
	}
	if bad := applyReasoningFields(entry, models, body.ReasoningProtocol, body.SupportedEfforts, body.DefaultEffort, body.ModelEfforts); bad != nil {
		refuse(w, http.StatusBadRequest, bad.code, bad.message, bad.detail)
		return
	}
	if !applyEndpointSwitches(w, entry, body.WebSearch, body.Continuation) {
		return
	}
	if !applyBillingMode(w, entry, body.BillingMode) {
		return
	}
	if !applyBalanceFields(w, entry, body.BalanceURL, body.BalanceFormat) {
		return
	}
	if body.ExtraBody != nil {
		// A null cannot be written to TOML, so it would be dropped on save and
		// the field would silently never reach the wire.
		if path, ok := firstNullPath(*body.ExtraBody, ""); ok {
			refuse(w, http.StatusBadRequest, "provider.extra_body_null",
				fmt.Sprintf("extra body field %q cannot be null", path), map[string]any{"path": path})
			return
		}
		entry.ExtraBody = *body.ExtraBody
	}

	// Storing the key is the whole update: providers ask for it per request, so
	// a session running on an exhausted key picks up its replacement on the next
	// one without a rebuild that would interrupt the conversation.
	if key := strings.TrimSpace(body.APIKey); key != "" {
		if _, err := config.SetCredential(entry.APIKeyEnv, key); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := config.NoteProviderKeys(config.UserConfigPath(), entry); err != nil {
		slog.Warn("serve: could not record the keys the 1.x line drops", "provider", entry.Name, "err", err)
	}
	s.applyEditedProvider(w, r, entry, assemblyShape(entry) != assembled, wasListed)
}

// providerEditBody carries the compatibility fields as pointers, so that "not
// sent" and "sent empty" stay different answers: a client that does not show
// them must not silently clear the headers a gateway needs.
type providerEditBody struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	// The wire format. A different one is the same account spoken to
	// another way, so it is a change to this entry, not a new entry.
	Kind            *string  `json:"kind"`
	APIKey          string   `json:"apiKey"`
	Models          []string `json:"models"`
	Default         string   `json:"default"`
	Vision          []string `json:"vision"`
	ContextWindow   *int     `json:"contextWindow"`
	MaxOutputTokens *int     `json:"maxOutputTokens"`
	// Seconds the endpoint may stay silent, before its response headers or
	// between stream events, before the call is read as dropped. Zero is
	// the built-in default.
	IdleTimeoutSeconds *int               `json:"idleTimeoutSeconds"`
	Headers            *map[string]string `json:"headers"`
	ExtraBody          *map[string]any    `json:"extraBody"`
	// Which request shape this endpoint controls thinking with. No probe
	// answers it — a relay forwards a vendor's models under its own name —
	// so the declaration has to come from whoever knows what is behind it.
	ReasoningProtocol *string `json:"reasoningProtocol"`
	// The endpoint's effort vocabulary, for a relay whose protocol ladder is
	// not the one its backend accepts. An empty list clears the declaration.
	SupportedEfforts *[]string `json:"supportedEfforts"`
	DefaultEffort    *string   `json:"defaultEffort"`
	// Per-model vocabularies for a gateway serving several vendors' models;
	// a listed model sent without one inherits the connection's.
	ModelEfforts *map[string]modelEffortView `json:"modelEfforts"`
	// Per-model window and output cap, with the same listed-models-are-the-
	// whole-answer rule; a model sent empty inherits the connection's.
	ModelLimits  *map[string]modelLimitsView `json:"modelLimits"`
	ModelPrices  *map[string]*modelPriceView `json:"modelPrices"`
	ModelAliases *map[string]string          `json:"modelAliases"`
	// HideModelPrefix leaves the prefix every model shares out of how they are drawn.
	HideModelPrefix *bool `json:"hideModelPrefix"`
	// Declarations a panel used to write one click at a time. Pointers, so
	// an unsent one leaves the stored choice alone. Whether thinking
	// parameters are sent is reasoningProtocol "none", not a field of its own.
	WebSearch    *bool   `json:"webSearch"`
	Continuation *string `json:"continuation"`
	// The wallet endpoint and the format it answers in. Sent empty, the URL
	// removes the readout and takes its format with it.
	BalanceURL    *string `json:"balanceUrl"`
	BalanceFormat *string `json:"balanceFormat"`
	// How the connection is billed: payg, or subscription_equivalent where the
	// figures shown are an API-priced estimate of a plan. Unsent leaves it alone.
	BillingMode *string `json:"billingMode"`
}

// applyEditedProvider answers a saved edit. What the agent binds at assembly
// reaches the conversation running on this source only through a rebuild; the
// key is read per request and needs none. The edit that unticks the running
// model is itself a plain save; after it, nothing on the entry can carry a
// later change to that conversation until it switches models.
func (s *Server) applyEditedProvider(w http.ResponseWriter, r *http.Request, entry *config.ProviderEntry, changed, wasListed bool) {
	running, _, _ := strings.Cut(currentModelRef(s.ctl()), "/")
	if !changed || running != entry.Name {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.runsListedModelOf(entry) {
		if !wasListed {
			refuse(w, http.StatusConflict, "provider.saved_model_unlisted", "the source was saved; this conversation runs on a model the source no longer lists, so switch models to apply it", nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		if isSwitchBusy(err) {
			busy(w, "provider.saved_while_running", "the source was saved; the conversation has work in progress and keeps its current settings until it is rebuilt", nil)
			return
		}
		rebuildFailed(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runsListedModelOf reports whether the current conversation runs on one of
// the models entry lists.
func (s *Server) runsListedModelOf(entry *config.ProviderEntry) bool {
	running, model, _ := strings.Cut(currentModelRef(s.ctl()), "/")
	return running == entry.Name && slices.Contains(entry.ModelList(), model)
}

// priceRates is what quotes read of a price. Where it was read and when are not
// part of it, so noting a source does not rebuild a running conversation.
func priceRates(prices map[string]*provider.Pricing) map[string][4]any {
	out := make(map[string][4]any, len(prices))
	for model, p := range prices {
		if p != nil {
			out[model] = [4]any{p.Input, p.Output, p.CacheHit, p.Currency}
		}
	}
	return out
}

// assemblyShape is what of an entry the agent binds when it is built. Empty and
// absent collections are one answer, because the form sends {} for a field it
// leaves blank and the file drops it.
func assemblyShape(e *config.ProviderEntry) string {
	orNil := func(n int, v any) any {
		if n == 0 {
			return nil
		}
		return v
	}
	shape := []any{
		e.BalanceURL, e.BalanceFormat, e.Kind, e.BaseURL, orNil(len(e.Models), e.Models), e.Vision, orNil(len(e.VisionModels), e.VisionModels),
		orNil(len(e.ModelOverrides), e.ModelOverrides), e.ContextWindow, e.MaxOutputTokens,
		orNil(len(e.Headers), e.Headers), orNil(len(e.ExtraBody), e.ExtraBody), e.ReasoningProtocol,
		orNil(len(e.SupportedEfforts), e.SupportedEfforts), e.DefaultEffort, e.IdleTimeoutSeconds,
		orNil(len(e.Prices), priceRates(e.Prices)), e.ProviderBillingMode(), orNil(len(e.VideoModels), e.VideoModels), e.VideoProtocol,
	}
	b, err := json.Marshal(shape)
	if err != nil {
		// Unencodable extra body: %#v still differs wherever the contents do,
		// and a pointer it prints can only make an unchanged entry rebuild.
		return fmt.Sprintf("%#v", shape)
	}
	return string(b)
}

// applyKind moves an entry to another wire of the same role and answers the
// refusal itself. An alias is stored as its canonical kind.
func applyKind(w http.ResponseWriter, entry *config.ProviderEntry, requested *string) bool {
	if requested == nil {
		return true
	}
	kind := strings.ToLower(strings.TrimSpace(*requested))
	wire, known := config.ProtocolFor(kind)
	if !known || config.AnswersFor(kind) != config.AnswersFor(entry.Kind) {
		refuse(w, http.StatusUnprocessableEntity, "provider.kind_unsupported",
			fmt.Sprintf("unsupported provider kind %q", kind), map[string]any{"kind": kind})
		return false
	}
	if wire.Kind == entry.Kind {
		return true
	}
	// Every wire reads request_url, and auth_header shapes one wire's
	// credentials, so the pinned values would be sent to the new protocol.
	if entry.RequestURL != "" || entry.ChatURL != "" || entry.AuthHeader {
		refuse(w, http.StatusConflict, "provider.kind_pinned_endpoint",
			"this source pins a request URL or auth header written for its current protocol", map[string]any{"kind": wire.Kind})
		return false
	}
	entry.Kind = wire.Kind
	return true
}

// applyReasoningFields stores the protocol, the connection's effort vocabulary
// and each listed model's own, refusing the first one that cannot be applied.
func applyReasoningFields(entry *config.ProviderEntry, models []string, protocol *string,
	levels *[]string, def *string, perModel *map[string]modelEffortView) *editRefusal {
	if protocol != nil {
		stored, ok := config.StoredReasoningProtocol(*protocol)
		if !ok {
			return &editRefusal{"provider.bad_reasoning_protocol",
				fmt.Sprintf("%q is not a reasoning protocol", *protocol), map[string]any{"protocol": *protocol}}
		}
		entry.ReasoningProtocol = stored
	}
	if level, ok := applyEffortDeclaration(entry, levels, def); !ok {
		return &editRefusal{"provider.default_effort_not_listed",
			fmt.Sprintf("default effort %q is not one of the declared levels", level), map[string]any{"level": level}}
	}
	return applyModelEfforts(entry, models, perModel)
}

// applyEffortDeclaration stores a declared effort vocabulary and its default.
// Only a request that touched them answers for their consistency: a mismatch
// already in the file is one EffectiveEffort falls back past. A default left
// with no levels to name is dropped; one outside the levels is refused.
func applyEffortDeclaration(entry *config.ProviderEntry, levels *[]string, def *string) (string, bool) {
	if levels == nil && def == nil {
		return "", true
	}
	if levels != nil {
		entry.SupportedEfforts = config.StoredEffortLevels(*levels)
	}
	if def != nil {
		entry.DefaultEffort = strings.ToLower(strings.TrimSpace(*def))
	}
	if entry.DefaultEffort == "" || slices.Contains(entry.SupportedEfforts, entry.DefaultEffort) {
		return "", true
	}
	if len(entry.SupportedEfforts) > 0 {
		return entry.DefaultEffort, false
	}
	entry.DefaultEffort = ""
	return "", true
}

// applyVisionSelection makes the panel's list the whole answer. The provider-wide
// flag answers for every model a whitelist omits, and a per-model override beats
// both, so a toggle that only wrote the list would leave the user flipping a
// switch that changes nothing.
func applyVisionSelection(entry *config.ProviderEntry, vision []string) {
	entry.VisionModels = vision
	entry.Vision = false
	for _, model := range entry.Models {
		override, ok := entry.ModelOverrides[model]
		if !ok || override.Vision == nil {
			continue
		}
		reads := slices.Contains(vision, model)
		override.Vision = &reads
		entry.ModelOverrides[model] = override
	}
}

// trimmedHeaders drops blank names and values: a header with an empty name is
// not a header, and one with an empty value is a line the user was still typing.
func trimmedHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstNullPath reports the first null anywhere in the object, named by its
// path so the message points at the line to fix rather than at the whole field.
func firstNullPath(v any, path string) (string, bool) {
	switch t := v.(type) {
	case nil:
		return path, true
	case map[string]any:
		for k, inner := range t {
			at := k
			if path != "" {
				at = path + "." + k
			}
			if found, ok := firstNullPath(inner, at); ok {
				return found, true
			}
		}
	case []any:
		for i, inner := range t {
			if found, ok := firstNullPath(inner, fmt.Sprintf("%s[%d]", path, i)); ok {
				return found, true
			}
		}
	}
	return "", false
}

func trimmedNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// visionModelsOf answers per model rather than reading the whitelist, so a
// provider-wide flag or a per-model override shows up as the checkbox it is.
func visionModelsOf(cfg *config.Config, p *config.ProviderEntry) []string {
	out := make([]string, 0, len(p.Models))
	for _, model := range p.ChatModelList() {
		if entry, ok := cfg.ResolveModel(p.Name + "/" + model); ok && config.EffectiveVision(entry) {
			out = append(out, model)
		}
	}
	return out
}

// visionSettableOf is which models on this connection the kernel would honour a
// vision flag for. Resolved per model rather than per endpoint: DeepSeek serves
// an image-taking model beside text-only ones, and a connection-wide answer
// would speak for models it was never asked about.
func visionSettableOf(cfg *config.Config, p *config.ProviderEntry) []string {
	out := make([]string, 0, len(p.Models))
	for _, model := range p.ChatModelList() {
		if entry, ok := cfg.ResolveModel(p.Name + "/" + model); ok && config.CanConfigureVision(entry) {
			out = append(out, model)
		}
	}
	return out
}

// applyBillingMode sets the billing mode from the closed set the quotes know.
func applyBillingMode(w http.ResponseWriter, entry *config.ProviderEntry, mode *string) bool {
	if mode == nil {
		return true
	}
	switch m := strings.TrimSpace(*mode); m {
	case pricing.BillingModePAYG, pricing.BillingModeSubscriptionEquivalent:
		entry.BillingMode = m
		return true
	}
	refuse(w, http.StatusBadRequest, "provider.billing_mode_unknown", "the billing mode must be payg or subscription_equivalent",
		map[string]any{"modes": []string{pricing.BillingModePAYG, pricing.BillingModeSubscriptionEquivalent}})
	return false
}

// applyBalanceFields sets the wallet endpoint and its declared format, the
// format only from the closed set.
func applyBalanceFields(w http.ResponseWriter, entry *config.ProviderEntry, rawURL, format *string) bool {
	if rawURL != nil {
		trimmed := strings.TrimSpace(*rawURL)
		if trimmed != "" {
			u, err := url.Parse(trimmed)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
				refuse(w, http.StatusBadRequest, "provider.balance_url_invalid", "the balance address must be an http or https URL", nil)
				return false
			}
		}
		entry.BalanceURL = trimmed
		if trimmed == "" {
			entry.BalanceFormat = ""
		}
	}
	if format != nil {
		f := strings.TrimSpace(*format)
		if f != "" && !billing.Format(f).Valid() {
			refuse(w, http.StatusBadRequest, "provider.balance_format_unknown", "that balance format is not one of the declared formats", map[string]any{"format": f, "formats": balanceFormatNames()})
			return false
		}
		entry.BalanceFormat = f
	}
	return true
}

// applyEndpointSwitches stores the two per-endpoint declarations and answers
// the refusal itself. Each exists only where the protocol catalog says the wire
// has the field; a value for an entry without it is a caller to correct. No
// probe tells a relay that stores no state from one that does, so it is declared.
func applyEndpointSwitches(w http.ResponseWriter, entry *config.ProviderEntry, webSearch *bool, continuation *string) bool {
	if webSearch != nil {
		if !config.SupportsServerWebSearch(entry) {
			refuse(w, http.StatusBadRequest, "provider.no_websearch_wire", "this protocol has no wire format for a provider-executed web search", nil)
			return false
		}
		on := *webSearch
		entry.WebSearch = &on
	}
	if continuation != nil {
		mode, ok := config.ParseContinuation(*continuation)
		if !ok {
			badValue(w, "continuation", string(config.ContinuationAuto), string(config.ContinuationStateful), string(config.ContinuationStateless))
			return false
		}
		if !config.CanConfigureContinuation(entry) {
			refuse(w, http.StatusBadRequest, "provider.no_continuation", "this protocol carries no stored state between turns", nil)
			return false
		}
		config.SetContinuation(entry, mode)
	}
	return true
}

// applyNumericFields stores the three numbers a form sends as pointers, so
// unsent leaves the stored value alone, and answers the refusal itself. Zero is
// a real answer for the window — it turns automatic compaction off — and for the
// idle timeout it is the built-in default, so only negatives and out-of-range
// idle values are mistakes.
func applyNumericFields(w http.ResponseWriter, entry *config.ProviderEntry, window, maxOut, idle *int) bool {
	if window != nil {
		if *window < 0 {
			refuse(w, http.StatusBadRequest, "provider.bad_context_window", "context window cannot be negative", nil)
			return false
		}
		entry.ContextWindow = *window
	}
	if maxOut != nil {
		if *maxOut < 0 {
			refuse(w, http.StatusBadRequest, "provider.bad_max_output_tokens", "maximum output tokens cannot be negative", nil)
			return false
		}
		entry.MaxOutputTokens = *maxOut
	}
	if idle != nil {
		if *idle != 0 && (*idle < provider.MinIdleTimeoutSeconds || *idle > provider.MaxIdleTimeoutSeconds) {
			refuse(w, http.StatusBadRequest, "provider.bad_idle_timeout", "idle timeout is outside the allowed range",
				map[string]any{"min": provider.MinIdleTimeoutSeconds, "max": provider.MaxIdleTimeoutSeconds})
			return false
		}
		entry.IdleTimeoutSeconds = *idle
	}
	return true
}

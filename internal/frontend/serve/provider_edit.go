// provider_edit.go — changing a source without flattening what it already knows.
package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
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
	// The three compatibility fields are pointers so that "not sent" and "sent
	// empty" stay different answers: a client that does not show them must not
	// silently clear the headers a gateway needs.
	var body struct {
		Name            string             `json:"name"`
		BaseURL         string             `json:"baseUrl"`
		APIKey          string             `json:"apiKey"`
		Models          []string           `json:"models"`
		Default         string             `json:"default"`
		Vision          []string           `json:"vision"`
		ContextWindow   *int               `json:"contextWindow"`
		MaxOutputTokens *int               `json:"maxOutputTokens"`
		Headers         *map[string]string `json:"headers"`
		ExtraBody       *map[string]any    `json:"extraBody"`
		// Which request shape this endpoint controls thinking with. No probe
		// answers it — a relay forwards a vendor's models under its own name —
		// so the declaration has to come from whoever knows what is behind it.
		ReasoningProtocol *string `json:"reasoningProtocol"`
		// The endpoint's effort vocabulary, for a relay whose protocol ladder is
		// not the one its backend accepts. An empty list clears the declaration.
		SupportedEfforts *[]string `json:"supportedEfforts"`
		DefaultEffort    *string   `json:"defaultEffort"`
	}
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
	entry.Models = models
	entry.Model = ""
	entry.Default = def
	applyVisionSelection(entry, trimmedNonEmpty(body.Vision))

	if body.ContextWindow != nil {
		// Zero is a real answer here — it turns automatic compaction off for
		// this source — so only a negative one is a mistake.
		if *body.ContextWindow < 0 {
			refuse(w, http.StatusBadRequest, "provider.bad_context_window", "context window cannot be negative", nil)
			return
		}
		entry.ContextWindow = *body.ContextWindow
	}
	if body.MaxOutputTokens != nil {
		if *body.MaxOutputTokens < 0 {
			refuse(w, http.StatusBadRequest, "provider.bad_max_output_tokens", "maximum output tokens cannot be negative", nil)
			return
		}
		entry.MaxOutputTokens = *body.MaxOutputTokens
	}
	if body.Headers != nil {
		entry.Headers = trimmedHeaders(*body.Headers)
	}
	if body.ReasoningProtocol != nil {
		stored, ok := config.StoredReasoningProtocol(*body.ReasoningProtocol)
		if !ok {
			refuse(w, http.StatusBadRequest, "provider.bad_reasoning_protocol",
				fmt.Sprintf("%q is not a reasoning protocol", *body.ReasoningProtocol),
				map[string]any{"protocol": *body.ReasoningProtocol})
			return
		}
		entry.ReasoningProtocol = stored
	}
	if level, ok := applyEffortDeclaration(entry, body.SupportedEfforts, body.DefaultEffort); !ok {
		refuse(w, http.StatusBadRequest, "provider.default_effort_not_listed",
			fmt.Sprintf("default effort %q is not one of the declared levels", level),
			map[string]any{"level": level})
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
	s.applyEditedProvider(w, r, entry, assemblyShape(entry) != assembled, wasListed)
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
		if codedRefusal(err) == codeSwitchModel {
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
		e.BaseURL, orNil(len(e.Models), e.Models), e.Vision, orNil(len(e.VisionModels), e.VisionModels),
		orNil(len(e.ModelOverrides), e.ModelOverrides), e.ContextWindow, e.MaxOutputTokens,
		orNil(len(e.Headers), e.Headers), orNil(len(e.ExtraBody), e.ExtraBody), e.ReasoningProtocol,
		orNil(len(e.SupportedEfforts), e.SupportedEfforts), e.DefaultEffort,
	}
	b, err := json.Marshal(shape)
	if err != nil {
		// Unencodable extra body: %#v still differs wherever the contents do,
		// and a pointer it prints can only make an unchanged entry rebuild.
		return fmt.Sprintf("%#v", shape)
	}
	return string(b)
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

// setProviderThinking pins or releases the plain-chat request shape. Relays
// that reject an unknown thinking/reasoning_effort field fail every request
// until this is off, and the endpoint's own error rarely names the field.
func (s *Server) setProviderThinking(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
		On   bool   `json:"on"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider(strings.TrimSpace(body.Name))
	if !ok {
		notFound(w, "provider", body.Name)
		return
	}
	if !config.CanConfigureThinkingParams(entry) {
		refuse(w, http.StatusBadRequest, "provider.no_thinking_param", "this protocol never sends thinking parameters", nil)
		return
	}
	config.SetThinkingParams(entry, body.On)
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setProviderContinuation pins how this endpoint carries context between turns.
// A relay may answer the Responses protocol without storing any, and it rejects
// the reference rather than ignoring it — so every turn after the first fails
// until this is stateless. Declared rather than probed: a retry that happens to
// succeed says nothing about why the first attempt did not.
func (s *Server) setProviderContinuation(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
		Mode string `json:"mode"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	mode, ok := config.ParseContinuation(body.Mode)
	if !ok {
		badValue(w, "mode", string(config.ContinuationAuto), string(config.ContinuationStateful), string(config.ContinuationStateless))
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, found := edit.Provider(strings.TrimSpace(body.Name))
	if !found {
		notFound(w, "provider", body.Name)
		return
	}
	if !config.CanConfigureContinuation(entry) {
		refuse(w, http.StatusBadRequest, "provider.no_continuation", "this protocol carries no stored state between turns", nil)
		return
	}
	config.SetContinuation(entry, mode)
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setProviderWebSearch records the tri-state for the endpoint-executed search
// tool. It is a real per-entry choice, unlike the protocol: the wire format is
// what makes it available, and this only says whether to use it.
func (s *Server) setProviderWebSearch(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
		On   bool   `json:"on"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider(strings.TrimSpace(body.Name))
	if !ok {
		notFound(w, "provider", body.Name)
		return
	}
	if !config.SupportsServerWebSearch(entry) {
		refuse(w, http.StatusBadRequest, "provider.no_websearch_wire", "this protocol has no wire format for a provider-executed web search", nil)
		return
	}
	on := body.On
	entry.WebSearch = &on
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

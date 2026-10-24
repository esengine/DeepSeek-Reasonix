// provider_check.go — asking a configured endpoint what it still is.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"reasonix/internal/model/catalog"
	"regexp"
	"strings"
	"time"
	"unicode"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/netclient"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/redirectguard"
	"reasonix/internal/safety/typesafe"
)

// providerCheck is what re-probing a saved provider found. Kind is the protocol
// the endpoint answered to, which is not always the one the entry claims: a
// gateway that changed hands, or a guess made when both protocols replied.
type providerCheck struct {
	OK   bool   `json:"ok"`
	Kind string `json:"kind,omitempty"`
	// BaseURL is set only when the saved address is not the one chat needs; it
	// is reported, never written, because a check must not edit what it tests.
	BaseURL string `json:"baseUrl,omitempty"`
	// Matches is whether that answer is consistent with the kind the entry
	// declares. Protocols sharing a listing shape are consistent with each
	// other, so a Responses source answering the OpenAI listing is not a change.
	Matches   bool     `json:"matches"`
	Models    []string `json:"models,omitempty"`
	Vision    []string `json:"vision,omitempty"`
	Ambiguous bool     `json:"ambiguous,omitempty"`
	NoProxy   bool     `json:"noProxy,omitempty"`
	// Code is why the check failed, as the dotted identity the add flow's
	// refusals use. Params carry only the numbers its sentence needs.
	Code string `json:"code,omitempty"`
	// Target is the host a refused redirect pointed at, for the sentence.
	Target string         `json:"target,omitempty"`
	Params map[string]int `json:"params,omitempty"`
	// HTTPStatus and Detail are what the endpoint answered with, for the user
	// to read; neither is an input to Code.
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// No protocol switch rides along with this. A probe only lists models, and it
// tries both auth shapes against the same listing URLs, so "both answered" says
// nothing about whether both chat contracts live at this base_url — DeepSeek
// serves OpenAI chat at /chat/completions and Anthropic at /anthropic/v1/messages.
// Flipping kind alone would aim one contract at the other's address.
const codeKeyOriginChanged = "provider.key_origin_changed"

// refuseKeyOriginChange stops a draft address on another origin from borrowing
// the saved credential: with no key typed, the probe would resolve the saved
// one and send it to wherever the form now points.
func refuseKeyOriginChange(w http.ResponseWriter, saved *config.ProviderEntry, draftBaseURL, draftKey string) bool {
	draftBaseURL = strings.TrimSpace(draftBaseURL)
	if saved == nil || draftBaseURL == "" || strings.TrimSpace(draftKey) != "" {
		return false
	}
	if strings.TrimSpace(saved.APIKeyEnv) == "" && saved.APIKey() == "" {
		return false
	}
	if redirectguard.SameOrigin(saved.BaseURL, draftBaseURL) {
		return false
	}
	refuse(w, http.StatusConflict, codeKeyOriginChanged, "the address is on another origin than the saved key; type the key for it", nil)
	return true
}

func (s *Server) registerProviderCheckRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /providers/check", s.checkProvider)
	mux.HandleFunc("POST /providers/check/model", s.checkProviderModel)
}

// checkProvider re-runs the add-a-source probe against what is already saved, so
// "is this key still good, and is it still the protocol we recorded" is one
// button rather than a delete and a re-add.
func (s *Server) checkProvider(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
		// The rest is the form's unsaved state. Each is optional: blank means
		// what is saved, so a plain re-check still sends only the name.
		BaseURL string `json:"baseUrl"`
		APIKey  string `json:"apiKey"`
		Kind    string `json:"kind"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	saved, ok := cfg.Provider(strings.TrimSpace(body.Name))
	if !ok {
		notFound(w, "provider", body.Name)
		return
	}
	if refuseKeyOriginChange(w, saved, body.BaseURL, body.APIKey) {
		return
	}
	// Only a check of what is saved says anything about what is saved.
	plain := strings.TrimSpace(body.BaseURL) == "" && strings.TrimSpace(body.APIKey) == "" && strings.TrimSpace(body.Kind) == ""
	entry := *saved
	if value := strings.TrimSpace(body.BaseURL); value != "" {
		entry.BaseURL = value
	}
	if value := strings.ToLower(strings.TrimSpace(body.Kind)); value != "" {
		if !config.SupportedProviderKind(value) {
			refuse(w, http.StatusBadRequest, "provider.kind_unsupported", "unsupported provider kind", map[string]any{"kind": value})
			return
		}
		entry.Kind = value
	}
	if key := strings.TrimSpace(body.APIKey); key != "" {
		entry = entry.WithAPIKeyForProbe(key)
	}
	ctx, cancel := context.WithTimeout(r.Context(), providerProbeTimeout)
	defer cancel()
	if config.AnswersFor(entry.Kind) == config.AnswersDecision {
		got := checkDecisionProvider(ctx, cfg, &entry)
		if plain {
			noteLastCheck(saved, got)
		}
		writeJSON(w, got)
		return
	}
	proxied, direct := probeClients()
	got, probeErr := catalog.ProbeEndpoint(ctx, catalog.ProbeOptions{
		BaseURL: entry.BaseURL,
		APIKey:  entry.APIKey(),
		Client:  proxied,
		Direct:  direct,
	})
	// A refusal is a finding, not a request failure: the row wants to say what
	// went wrong, and a bare status code would leave it with nothing to show.
	if probeErr != nil {
		found := probeFinding(probeErr, entry.APIKey)
		if plain {
			noteLastCheck(saved, found)
		}
		writeJSON(w, found)
		return
	}
	completed := ""
	if got.BaseURL != strings.TrimSpace(entry.BaseURL) {
		completed = got.BaseURL
	}
	result := providerCheck{
		OK:        true,
		Kind:      got.Kind,
		BaseURL:   completed,
		Matches:   config.ProtocolAnswerMatches(entry.Kind, got.Kind),
		Models:    nonNilStrings(got.Models),
		Vision:    nonNilStrings(got.Vision),
		Ambiguous: got.Ambiguous,
		NoProxy:   got.NoProxy,
	}
	if plain {
		noteLastCheck(saved, result)
	}
	writeJSON(w, result)
}

// probeFinding reads a failed probe as the typed finding a client renders. An
// error that is not a probe identity is the kernel's own fault, not the
// endpoint's or the user's, and says so.
func probeFinding(err error, apiKey func() string) providerCheck {
	var probe *catalog.ProbeError
	if !errors.As(err, &probe) {
		return providerCheck{Code: codeProbeFailed}
	}
	_, code := probeReasonRefusal(probe.Reason)
	found := providerCheck{Code: code, HTTPStatus: probe.Status, Detail: endpointDetail(probe.Body, apiKey)}
	var left *redirectguard.OriginLeft
	if errors.As(err, &left) {
		found.Detail, found.Target = "", left.To
	}
	for name, value := range probe.Params {
		if n, ok := value.(int); ok {
			if found.Params == nil {
				found.Params = map[string]int{}
			}
			found.Params[name] = n
		}
	}
	return found
}

type providerModelCheckRequest struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	BaseURL    string `json:"baseUrl"`
	APIKey     string `json:"apiKey"`
	Kind       string `json:"kind"`
	AuthHeader *bool  `json:"authHeader"`
	NoProxy    *bool  `json:"noProxy"`
}

type providerModelCheck struct {
	Model      string `json:"model"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	// Detail is the endpoint's own error text, for the user to read. It is
	// never an input to Status or Reason.
	Detail string `json:"detail,omitempty"`
}

const (
	modelCheckDetailRunes = 300
	modelCheckDetailScan  = 64 << 10
	modelCheckMinKeyLen   = 8
)

var ansiSequence = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)?")

// checkProviderModel spends one bounded completion only after the settings UI
// asks for it. A model need not appear in the saved or remote catalog: the
// exact caller-supplied ID is put on the wire and nothing is persisted.
func (s *Server) checkProviderModel(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body providerModelCheckRequest
	if !decodeProviderBody(w, r, &body) {
		return
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		refuse(w, http.StatusBadRequest, "provider.no_models_picked", "a model ID is required", nil)
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	entry, found := cfg.Provider(strings.TrimSpace(body.Name))
	if !found && strings.TrimSpace(body.BaseURL) == "" {
		notFound(w, "provider", body.Name)
		return
	}
	if !found {
		entry = &config.ProviderEntry{Name: "probe", Kind: "openai"}
	}
	if found && refuseKeyOriginChange(w, entry, body.BaseURL, body.APIKey) {
		return
	}
	candidate := *entry
	if value := strings.TrimSpace(body.BaseURL); value != "" {
		candidate.BaseURL = value
	}
	if value := strings.ToLower(strings.TrimSpace(body.Kind)); value != "" {
		if !config.SupportedProviderKind(value) {
			refuse(w, http.StatusBadRequest, "provider.kind_unsupported", "unsupported provider kind", map[string]any{"kind": value})
			return
		}
		candidate.Kind = value
	}
	if body.AuthHeader != nil {
		candidate.AuthHeader = *body.AuthHeader
	}
	if body.NoProxy != nil {
		candidate.NoProxy = *body.NoProxy
	}
	candidate.Model = model
	if key := strings.TrimSpace(body.APIKey); key != "" {
		candidate = candidate.WithAPIKeyForProbe(key)
	}

	proxy := cfg.NetworkProxySpec()
	if candidate.NoProxy && proxy.Mode != netclient.ModeCustom {
		proxy = netclient.ProxySpec{Mode: netclient.ModeOff}
	}
	ctx, cancel := context.WithTimeout(provider.WithRetryLimit(r.Context(), 0), modelCheckTimeout(candidate.BaseURL))
	defer cancel()
	if config.AnswersFor(candidate.Kind) == config.AnswersDecision {
		if candidate.APIKey() == "" {
			candidate.ResolveAPIKeyFromProcessEnvForProbe()
		}
		err := probeDecision(ctx, cfg, &candidate, model)
		status, reason, httpStatus := classifyProviderModelCheck(err)
		writeJSON(w, providerModelCheck{Model: model, Status: status, Reason: localTimeout(reason, candidate.BaseURL), HTTPStatus: httpStatus, Detail: modelCheckDetail(err, candidate.APIKey)})
		return
	}
	modelProvider, err := boot.NewProviderWithProxy(&candidate, proxy)
	if err != nil {
		writeJSON(w, providerModelCheck{Model: model, Status: "unknown", Reason: "rejected"})
		return
	}
	err = runProviderModelCheck(ctx, modelProvider)
	status, reason, httpStatus := classifyProviderModelCheck(err)
	writeJSON(w, providerModelCheck{Model: model, Status: status, Reason: localTimeout(reason, candidate.BaseURL), HTTPStatus: httpStatus, Detail: modelCheckDetail(err, candidate.APIKey)})
}

// A local engine that has not loaded the model yet takes its first request for
// as long as the weights take to reach memory, so a local address gets a longer
// wait than a remote one.
var (
	modelCheckRemoteTimeout = providerProbeTimeout
	modelCheckLocalTimeout  = 3 * providerProbeTimeout
)

const reasonLocalTimeout = "local_timeout"

func modelCheckTimeout(baseURL string) time.Duration {
	if loopbackEndpoint(baseURL) {
		return modelCheckLocalTimeout
	}
	return modelCheckRemoteTimeout
}

// localTimeout names a timeout against the machine's own address as such: the
// host knows where it asked, and the cause to look at there is the engine, not
// the network.
func localTimeout(reason, baseURL string) string {
	if reason == "timeout" && loopbackEndpoint(baseURL) {
		return reasonLocalTimeout
	}
	return reason
}

func loopbackEndpoint(baseURL string) bool {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// modelCheckDetail returns what the endpoint said, as one printable line. The
// order matters: control characters and whitespace fold first so they cannot
// split a credential or shift a cut, the probe key is replaced on the whole
// text, and the pattern scrub sees at most modelCheckDetailScan bytes ended on
// a word boundary, so no cut leaves part of a key visible.
func modelCheckDetail(err error, apiKey func() string) string {
	var text string
	var typeSafeErr *typesafe.HTTPError
	var auth *provider.AuthError
	var apiErr *provider.APIError
	var streamErr *provider.StreamPayloadError
	var left *redirectguard.OriginLeft
	switch {
	case errors.As(err, &left):
		text = left.Error()
	case errors.As(err, &typeSafeErr):
		text = typeSafeErr.Body
	case errors.As(err, &auth):
		text = auth.Body
	case errors.As(err, &apiErr):
		text = apiErr.Body
	case errors.As(err, &streamErr):
		text = streamErr.Message
	}
	return endpointDetail(text, apiKey)
}

// endpointDetail folds what an endpoint said into one printable, bounded line
// with the probe key and credential-shaped text removed.
func endpointDetail(text string, apiKey func() string) string {
	text = ansiSequence.ReplaceAllString(text, " ")
	text = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)), " ")
	if key := strings.TrimSpace(apiKey()); len(key) >= modelCheckMinKeyLen {
		escaped, _ := json.Marshal(key)
		for _, form := range []string{key, url.QueryEscape(key), url.PathEscape(key), strings.Trim(string(escaped), `"`)} {
			text = strings.ReplaceAll(text, form, "***")
		}
	}
	if len(text) > modelCheckDetailScan {
		text = text[:modelCheckDetailScan]
		if i := strings.LastIndexByte(text, ' '); i >= 0 {
			text = text[:i]
		}
	}
	text = secrets.RedactCredentials(text)
	if runes := []rune(text); len(runes) > modelCheckDetailRunes {
		text = string(runes[:modelCheckDetailRunes]) + "…"
	}
	return text
}

// errToolsUnsupported is the endpoint answering chat but refusing a tools
// array. It is established by observation — the same request succeeds once the
// array is removed — never by reading words out of the refusal.
var errToolsUnsupported = errors.New("provider model check: endpoint refuses a tools array")

// probeTool is the smallest tool an endpoint can be offered. The check carries
// one because Reasonix is an agent: a model that answers chat and refuses tools
// cannot run a turn here, and a check that never sent a tools array would
// report that endpoint available and leave the failure to the first real turn.
var probeTool = provider.ToolSchema{
	Name:        "reasonix_probe",
	Description: "Connectivity probe. Do not call it.",
	Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
}

func runProviderModelCheck(ctx context.Context, modelProvider provider.Provider) error {
	err := probeProviderModel(ctx, modelProvider, []provider.ToolSchema{probeTool})
	if err == nil || !worthAskingWithoutTools(err) {
		return err
	}
	// The body was rejected. Ask again without the tools array: if that answers,
	// the array was the whole objection, which is a different fact from "this
	// model does not work" and one the refusal's own wording cannot settle.
	if ctx.Err() == nil && probeProviderModel(ctx, modelProvider, nil) == nil {
		return errToolsUnsupported
	}
	return err
}

// worthAskingWithoutTools reports whether a second attempt could answer
// anything the first did not. A credential or a rate limit refuses the retry
// for the same reason it refused the first, so only a rejection of the request
// body earns the extra call.
func worthAskingWithoutTools(err error) bool {
	_, reason, _ := classifyProviderModelCheck(err)
	return reason == "rejected"
}

func probeProviderModel(ctx context.Context, modelProvider provider.Provider, tools []provider.ToolSchema) error {
	stream, err := provider.Stream(ctx, modelProvider, provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "OK"}},
		Tools:     tools,
		MaxTokens: 1,
	})
	if err != nil {
		return err
	}
	proved := false
	for chunk := range stream {
		if chunk.Type == provider.ChunkError {
			return chunk.Err
		}
		if chunk.Type == provider.ChunkDone || chunk.Type == provider.ChunkUsage ||
			(chunk.Type == provider.ChunkText && chunk.Text != "") ||
			(chunk.Type == provider.ChunkReasoning && chunk.Text != "") || chunk.ToolCall != nil {
			proved = true
		}
	}
	if proved {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("provider model check ended without a protocol result")
}

func classifyProviderModelCheck(err error) (status, reason string, httpStatus int) {
	if err == nil {
		return "available", "", 0
	}
	if errors.Is(err, errToolsUnsupported) {
		return "unavailable", "tools", 0
	}
	var typeSafeErr *typesafe.HTTPError
	if errors.As(err, &typeSafeErr) {
		if typeSafeErr.Status == http.StatusUnauthorized || typeSafeErr.Status == http.StatusForbidden {
			return "unknown", "auth", typeSafeErr.Status
		}
		return classifyProviderModelCheck(&provider.APIError{Status: typeSafeErr.Status, Body: typeSafeErr.Body})
	}
	var auth *provider.AuthError
	if errors.As(err, &auth) {
		return "unknown", "auth", auth.Status
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == http.StatusTooManyRequests:
			return "unknown", "rate_limited", apiErr.Status
		case apiErr.Status == http.StatusRequestTimeout || apiErr.Status == http.StatusGatewayTimeout:
			return "unknown", "timeout", apiErr.Status
		case modelErrorIdentity(apiErr.Body) == "not_found":
			return "unavailable", "not_found", apiErr.Status
		case modelErrorIdentity(apiErr.Body) == "rejected":
			return "unavailable", "rejected", apiErr.Status
		case apiErr.Status >= 500:
			return "unknown", "network", apiErr.Status
		default:
			return "unknown", "rejected", apiErr.Status
		}
	}
	var streamErr *provider.StreamPayloadError
	if errors.As(err, &streamErr) {
		switch modelIdentity(streamErr.Code, streamErr.Type, streamErr.Param) {
		case "not_found":
			return "unavailable", "not_found", 0
		case "rejected":
			return "unavailable", "rejected", 0
		}
		if rateLimitIdentity(streamErr.Code, streamErr.Type) {
			return "unknown", "rate_limited", 0
		}
		return "unknown", "rejected", 0
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "unknown", "timeout", 0
	}
	if errors.Is(err, redirectguard.ErrRefused) {
		return "unknown", "redirect_refused", 0
	}
	var netErr net.Error
	if errors.As(err, &netErr) || provider.IsStreamInterrupted(err) {
		return "unknown", "network", 0
	}
	return "unknown", "rejected", 0
}

func modelErrorIdentity(body string) string {
	var envelope struct {
		Code  string `json:"code"`
		Type  string `json:"type"`
		Param string `json:"param"`
		Error struct {
			Code  string `json:"code"`
			Type  string `json:"type"`
			Param string `json:"param"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &envelope) != nil {
		return ""
	}
	code, typ, param := envelope.Error.Code, envelope.Error.Type, envelope.Error.Param
	if code == "" && typ == "" && param == "" {
		code, typ, param = envelope.Code, envelope.Type, envelope.Param
	}
	return modelIdentity(code, typ, param)
}

func modelIdentity(code, typ, param string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	typ = strings.ToLower(strings.TrimSpace(typ))
	param = strings.ToLower(strings.TrimSpace(param))
	if code == "model_not_found" || code == "model_not_found_error" || code == "unknown_model" || typ == "model_not_found" {
		return "not_found"
	}
	if param == "model" {
		return "rejected"
	}
	if typ == "not_found_error" {
		return "not_found"
	}
	return ""
}

func rateLimitIdentity(code, typ string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	typ = strings.ToLower(strings.TrimSpace(typ))
	return code == "rate_limit_exceeded" || code == "rate_limit_error" || typ == "rate_limit_error"
}

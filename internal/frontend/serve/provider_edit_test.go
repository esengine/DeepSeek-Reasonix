package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// A source the panel cannot fully describe: per-model prices and effort
// vocabularies, a context window, a wallet endpoint. Editing its model list must
// not cost the user any of it.
const richProviderConfig = `default_model = "rich/alpha"

[[providers]]
name = "rich"
kind = "openai"
base_url = "https://gateway.invalid/v1"
models = ["alpha", "beta"]
default = "alpha"
api_key_env = "RICH_API_KEY"
balance_url = "https://gateway.invalid/balance"
context_window = 131072
thinking = "enabled"

[providers.prices.alpha]
input = 1.0
output = 2.0
currency = "CNY"

[providers.model_overrides.beta]
supported_efforts = ["low", "high"]
default_effort = "high"
`

func newRichProviderServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return c })
}

// newRichProviderServerAs is the same server with the controller wrapped, for
// the tests that need it to answer differently about what it is doing.
func newRichProviderServerAs(t *testing.T, wrap func(control.SessionAPI) control.SessionAPI) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	if _, err := config.SetCredential("RICH_API_KEY", "sk-rich"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(richProviderConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{
		Sink: bc, Label: "alpha", ModelRef: "rich/alpha", SessionDir: testenv.TempDir(t),
	})
	s := New(wrap(ctrl), bc, config.ServeConfig{})
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestEditProviderKeepsWhatTheFormCannotShow(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","baseUrl":"https://gateway.invalid/v1",
		"models":["alpha","beta"],"default":"beta","vision":["beta"]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/edit = %d: %s", resp.StatusCode, b)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Provider("rich")
	if !ok {
		t.Fatal("the provider disappeared")
	}
	if entry.ContextWindow != 131072 || entry.Thinking != "enabled" || entry.BalanceURL == "" {
		t.Fatalf("the edit flattened provider-wide fields: %+v", entry)
	}
	if entry.Prices["alpha"] == nil || entry.Prices["alpha"].Input != 1 {
		t.Fatalf("the per-model price did not survive: %+v", entry.Prices)
	}
	if got := entry.ModelOverrides["beta"].SupportedEfforts; len(got) != 2 {
		t.Fatalf("the per-model effort list did not survive: %v", got)
	}
	if entry.Default != "beta" {
		t.Fatalf("default = %q, want the edited beta", entry.Default)
	}
}

// The toggle has to be the whole answer. A provider-wide flag answers for every
// model the whitelist omits, so writing only the list would leave the switch
// looking broken.
func TestEditProviderVisionSelectionIsAuthoritative(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":["beta"]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/edit = %d", resp.StatusCode)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	beta, ok := cfg.ResolveModel("rich/beta")
	if !ok || !config.EffectiveVision(beta) {
		t.Fatal("the model the user ticked does not read images")
	}
	alpha, ok := cfg.ResolveModel("rich/alpha")
	if !ok || config.EffectiveVision(alpha) {
		t.Fatal("a model the user left unticked still claims image input")
	}
}

// The list has to show the current answer, or the form asks the user to
// remember which models read images.
func TestProvidersListReportsWhichModelsReadImages(t *testing.T) {
	srv := newRichProviderServer(t)
	postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":["beta"]
	}`).Body.Close()

	resp, err := http.Get(srv.URL + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []providerView
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Name != "rich" {
			continue
		}
		if len(p.VisionModels) != 1 || p.VisionModels[0] != "beta" {
			t.Fatalf("visionModels = %v, want just beta", p.VisionModels)
		}
		return
	}
	t.Fatal("the provider is missing from the list")
}

func TestEditProviderRefusesWhatItCannotApply(t *testing.T) {
	srv := newRichProviderServer(t)

	for name, body := range map[string]string{
		"a provider that is gone":          `{"name":"nobody","models":["alpha"]}`,
		"no models":                        `{"name":"rich","models":[]}`,
		"a default outside the model list": `{"name":"rich","models":["alpha"],"default":"beta"}`,
	} {
		resp := postProvider(t, srv.URL, "/providers/edit", body)
		if resp.StatusCode == http.StatusNoContent {
			t.Errorf("%s: the edit was accepted", name)
		}
		resp.Body.Close()
	}
}

// The OpenAI chat wire has no format for a provider-executed search, so the
// same account answers differently through each of its doors. Recording that
// against a door that cannot run one would be a setting with no effect.
func TestWebSearchIsPerDoorNotPerAccount(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	if _, err := config.SetCredential("DEEPSEEK_API_KEY", "sk-ds"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "ds/deepseek-v4-pro"

[[providers]]
name = "ds"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-pro"]
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name = "ds-anthropic"
kind = "anthropic"
base_url = "https://api.deepseek.com/anthropic"
models = ["deepseek-v4-pro"]
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	openaiDoor, _ := cfg.Provider("ds")
	anthropicDoor, _ := cfg.Provider("ds-anthropic")
	if config.HasServerWebSearchCapability(openaiDoor) {
		t.Fatal("the OpenAI chat wire has no web search to offer")
	}
	if !config.HasServerWebSearchCapability(anthropicDoor) {
		t.Fatal("the official DeepSeek Anthropic endpoint does offer web search")
	}
	if !config.EffectiveWebSearch(anthropicDoor) {
		t.Fatal("an official DeepSeek Anthropic endpoint defaults its search on")
	}
}

// The three fields a probe cannot answer have to survive a save and come back
// on the next read — that round trip is the whole reason they are in the panel.
func TestEditProviderKeepsCompatibilityFields(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"name":"existing","models":["model-a"],"default":"model-a","contextWindow":128000,` +
		`"headers":{"HTTP-Referer":"https://example.com"," ":"x"},"extraBody":{"enable_thinking":true}}`
	resp := postProvider(t, srv.URL, "/providers/edit", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/edit = %d", resp.StatusCode)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Provider("existing")
	if !ok {
		t.Fatal("provider went missing")
	}
	if entry.ContextWindow != 128000 {
		t.Fatalf("context window = %d", entry.ContextWindow)
	}
	if len(entry.Headers) != 1 || entry.Headers["HTTP-Referer"] != "https://example.com" {
		t.Fatalf("headers = %v, want the blank name dropped", entry.Headers)
	}
	if entry.ExtraBody["enable_thinking"] != true {
		t.Fatalf("extra body = %v", entry.ExtraBody)
	}
}

// Omitting them means "leave them alone": a client that does not show the
// fields must not wipe the headers a gateway needs.
func TestEditProviderLeavesCompatibilityFieldsAloneWhenUnsent(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	first := postProvider(t, srv.URL, "/providers/edit",
		`{"name":"existing","models":["model-a"],"default":"model-a","contextWindow":64000,"headers":{"X-Title":"Reasonix"}}`)
	first.Body.Close()
	second := postProvider(t, srv.URL, "/providers/edit", `{"name":"existing","models":["model-a"],"default":"model-a"}`)
	second.Body.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cfg.Provider("existing")
	if entry.ContextWindow != 64000 || entry.Headers["X-Title"] != "Reasonix" {
		t.Fatalf("an unsent field was cleared: window=%d headers=%v", entry.ContextWindow, entry.Headers)
	}
}

// A null cannot be written to TOML, so accepting one would store a field that
// silently never reaches the wire.
func TestEditProviderRejectsNullInExtraBody(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers/edit",
		`{"name":"existing","models":["model-a"],"default":"model-a","extraBody":{"outer":{"inner":null}}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST with a null = %d, want 400", resp.StatusCode)
	}
	got, _ := readAllString(resp)
	if !strings.Contains(got, "outer.inner") {
		t.Fatalf("refusal does not name the offending path: %s", got)
	}
}

// A relay forwards someone else's models under its own name, so nothing about
// it can be probed for a reasoning vocabulary — and with none, every effort
// level but auto is refused and the composer shows no ladder at all. Declaring
// the shape is the only way in, and the panel is where a user does it.
func TestDeclaringAReasoningProtocolGivesTheSourceAnEffortLadder(t *testing.T) {
	srv := newRichProviderServer(t)

	before, ok := loadEntry(t, "rich/alpha")
	if !ok {
		t.Fatal("seed provider did not resolve")
	}
	if config.EffortCapabilityForEntry(before).Supported {
		t.Fatal("an undeclared OpenAI-compatible gateway already had a ladder; this test proves nothing")
	}

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"reasoningProtocol":"openai"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("edit status = %d, want 204", resp.StatusCode)
	}

	after, ok := loadEntry(t, "rich/alpha")
	if !ok {
		t.Fatal("provider stopped resolving after the edit")
	}
	got := config.EffortCapabilityForEntry(after)
	if !got.Supported {
		t.Fatal("declaring the protocol left the source without an effort ladder")
	}
	if !slices.Equal(got.Levels, []string{"auto", "low", "medium", "high"}) {
		t.Fatalf("levels = %v, want the OpenAI ladder", got.Levels)
	}
}

// A value outside the vocabulary is refused rather than normalized to auto: a
// typo that silently means "no declaration" is a control that did nothing and
// said it worked.
func TestEditProviderRefusesAnUnknownReasoningProtocol(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha"],"default":"alpha","vision":[],
		"reasoningProtocol":"gpt5-thinking"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var reason Reason
	if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
		t.Fatalf("decode reason: %v", err)
	}
	if reason.Code != "provider.bad_reasoning_protocol" {
		t.Fatalf("code = %q, want provider.bad_reasoning_protocol", reason.Code)
	}
}

// A relay whose backend takes levels its protocol's ladder does not name needs
// the vocabulary itself declared; the panel stores it as the effort layer reads
// it, and the listing hands it back so the form opens on the current answer.
func TestDeclaringEffortLevelsReplacesTheLadder(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"supportedEfforts":[" Low","medium","auto","xhigh","low"],"defaultEffort":"Medium"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := readAllString(resp)
		t.Fatalf("edit status = %d, want 204: %s", resp.StatusCode, body)
	}

	after, ok := loadEntry(t, "rich/alpha")
	if !ok {
		t.Fatal("provider stopped resolving after the edit")
	}
	got := config.EffortCapabilityForEntry(after)
	if !slices.Equal(got.Levels, []string{"auto", "low", "medium", "xhigh"}) || got.Default != "medium" {
		t.Fatalf("capability = %+v, want the declared ladder defaulting to medium", got)
	}

	listed, err := http.Get(srv.URL + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer listed.Body.Close()
	var list []providerView
	if err := json.NewDecoder(listed.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	var rich *providerView
	for i := range list {
		if list[i].Name == "rich" {
			rich = &list[i]
		}
	}
	if rich == nil || !slices.Equal(rich.SupportedEfforts, []string{"low", "medium", "xhigh"}) || rich.DefaultEffort != "medium" {
		t.Fatalf("listing = %+v, want the stored vocabulary", rich)
	}

	reset := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"supportedEfforts":[],"defaultEffort":""
	}`)
	defer reset.Body.Close()
	if reset.StatusCode != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204", reset.StatusCode)
	}
	cleared, _ := loadEntry(t, "rich/alpha")
	if config.EffortCapabilityForEntry(cleared).Supported {
		t.Fatal("an emptied vocabulary still produced a ladder")
	}
}

func TestEditProviderRefusesADefaultEffortOutsideTheLevels(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha"],"default":"alpha","vision":[],
		"supportedEfforts":["low","high"],"defaultEffort":"max"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var reason Reason
	if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
		t.Fatalf("decode reason: %v", err)
	}
	if reason.Code != "provider.default_effort_not_listed" {
		t.Fatalf("code = %q, want provider.default_effort_not_listed", reason.Code)
	}
}

func loadEntry(t *testing.T, ref string) (*config.ProviderEntry, bool) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.ResolveModel(ref)
}

func gaugeWindow(t *testing.T, base string) int {
	t.Helper()
	resp, err := http.Get(base + "/context")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Window int `json:"window"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got.Window
}

// The window is bound into the agent at assembly, so an edit to the source the
// open conversation runs on has to reach that conversation too — otherwise the
// gauge and the fold point keep counting against the number the form replaced.
func TestEditProviderReachesTheConversationRunningOnIt(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","contextWindow":1000000
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/edit = %d: %s", resp.StatusCode, b)
	}
	if got := gaugeWindow(t, srv.URL); got != 1000000 {
		t.Fatalf("the open conversation's window = %d, want the saved 1000000", got)
	}
}

// Mid-turn the write lands and only the rebuild waits, which is a different
// answer from a failed save and from a refused one.
func TestEditProviderMidTurnSaysTheConversationKeepsItsSettings(t *testing.T) {
	srv := newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return midTurn{c} })

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","contextWindow":1000000
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/edit mid-turn = %d, want 409: %s", resp.StatusCode, b)
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "provider.saved_while_running" {
		t.Fatalf("code = %q, want provider.saved_while_running", got.Code)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cfg.Provider("rich")
	if entry.ContextWindow != 1000000 {
		t.Fatalf("window on disk = %d — the refusal said it was saved, so it has to be", entry.ContextWindow)
	}
}

// Unticking the model the conversation runs on is a saved edit, not a failed
// one: there is nothing on the entry left to rebuild that conversation on.
func TestEditProviderDroppingTheRunningModelIsStillSaved(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["beta"],"default":"beta"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/edit without the running model = %d, want 204: %s", resp.StatusCode, b)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cfg.Provider("rich")
	if !slices.Equal(entry.Models, []string{"beta"}) {
		t.Fatalf("models on disk = %v, want [beta]", entry.Models)
	}
}

// The form sends every field, so a key rotation arrives looking like a full
// edit. The key is read per request; mid-turn it must not be answered as a
// change the running conversation will miss.
func TestEditProviderKeyOnlyMidTurnNeedsNoRebuild(t *testing.T) {
	srv := newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return midTurn{c} })

	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","baseUrl":"https://gateway.invalid/v1","apiKey":"sk-rotated",
		"models":["alpha","beta"],"default":"alpha","vision":[],
		"contextWindow":131072,"maxOutputTokens":0,"reasoningProtocol":"",
		"supportedEfforts":[],"defaultEffort":"","headers":{},"extraBody":{}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("key-only edit mid-turn = %d, want 204: %s", resp.StatusCode, b)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cfg.Provider("rich")
	if got := entry.APIKey(); got != "sk-rotated" {
		t.Fatalf("stored key = %q, want the rotated one", got)
	}
}

// Once the running model is off the entry, a later edit the agent binds at
// assembly cannot reach the conversation; the answer has to say why rather
// than report a change that never applies.
func TestEditProviderAfterDroppingTheRunningModelSaysItIsUnlisted(t *testing.T) {
	srv := newRichProviderServer(t)

	drop := postProvider(t, srv.URL, "/providers/edit", `{"name":"rich","models":["beta"],"default":"beta"}`)
	drop.Body.Close()
	if drop.StatusCode != http.StatusNoContent {
		t.Fatalf("dropping the running model = %d, want 204", drop.StatusCode)
	}
	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["beta"],"default":"beta","contextWindow":1000000
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		b, _ := readAllString(resp)
		t.Fatalf("editing the window after the drop = %d, want 409: %s", resp.StatusCode, b)
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "provider.saved_model_unlisted" {
		t.Fatalf("code = %q, want provider.saved_model_unlisted", got.Code)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cfg.Provider("rich")
	if entry.ContextWindow != 1000000 {
		t.Fatalf("window on disk = %d — the refusal said it was saved, so it has to be", entry.ContextWindow)
	}
}

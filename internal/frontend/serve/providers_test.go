package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
	"reasonix/internal/state/history"
	"reasonix/internal/state/stats"
)

// newProviderEditServer is a server with one configured provider, its config
// and credential store redirected into the test's own home.
func newProviderEditServer(t *testing.T, workspaceRoot ...string) *Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	configPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "existing/model-a"

[[providers]]
name = "existing"
kind = "openai"
base_url = "https://example.invalid/v1"
models = ["model-a"]
default = "model-a"
api_key_env = "EXISTING_API_KEY"
`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	root := ""
	if len(workspaceRoot) > 0 {
		root = workspaceRoot[0]
	}
	ctrl := control.New(control.Options{
		Sink: bc, Label: "model-a", ModelRef: "existing/model-a", SessionDir: testenv.TempDir(t), WorkspaceRoot: root,
	})
	t.Cleanup(ctrl.Close)
	closeSharedCatalogsOnCleanup(t)
	return New(ctrl, bc, config.ServeConfig{})
}

// closeSharedCatalogsOnCleanup releases the usage and history catalogs before the
// test's home is removed. They are process-wide, cached per home rather than owned
// by a controller or hub, so no Close reaches them — right in production, wrong
// here: Windows will not unlink an open file, failing a test that already passed.
// Any test pointing REASONIX_HOME at a TempDir needs this.
func closeSharedCatalogsOnCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = stats.CloseUsageCatalogs(ctx)
		_ = history.CloseSharedCatalog(ctx)
	})
}

func postProvider(t *testing.T, base, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(base+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// The grant is the whole security story: a key lands in the credential store of
// the machine running the kernel, so a reachable server must refuse until its
// host says its only client is a local window.
func TestProviderEditIsRefusedUntilTheHostGrantsIt(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	for _, path := range []string{"/providers", "/providers/probe", "/providers/check/model", "/providers/remove"} {
		resp := postProvider(t, srv.URL, path, `{}`)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("POST %s = %d, want 403 before the host grants provider editing", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
	// Listing is not a write and stays readable, so the panel can show what is
	// configured even where adding is refused.
	resp, err := http.Get(srv.URL + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /providers = %d, want the listing to stay readable", resp.StatusCode)
	}
}

func TestSaveProviderWritesConfigAndKeepsTheKeyOutOfIt(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"kimi","kind":"openai","baseUrl":"https://api.moonshot.cn/v1",
		"apiKey":"sk-secret-value","models":["kimi-k2","kimi-k2-turbo"],
		"default":"kimi-k2","authHeader":false,"noProxy":false,"effort":"","vision":[],
		"contextWindow":262144,"maxOutputTokens":32768,
		"reasoningProtocol":"none","headers":{"X-Title":"Reasonix"},
		"extraBody":{"temperature":0.7}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers = %d: %s", resp.StatusCode, b)
	}

	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	written := string(raw)
	if !strings.Contains(written, `name        = "kimi"`) && !strings.Contains(written, `name = "kimi"`) {
		t.Fatalf("provider not written to config:\n%s", written)
	}
	// A config is something a user pastes into an issue. The secret belongs in
	// the credential store, and only its env name belongs here.
	if strings.Contains(written, "sk-secret-value") {
		t.Fatalf("the API key was written into the config file:\n%s", written)
	}
	if !config.CredentialStored("KIMI_API_KEY") {
		t.Fatal("the API key did not reach the credential store")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == "kimi" {
			found = true
			if got := cfg.Providers[i].DefaultModel(); got != "kimi-k2" {
				t.Fatalf("default model = %q", got)
			}
			if cfg.Providers[i].ContextWindow != 262144 || cfg.Providers[i].MaxOutputTokens != 32768 {
				t.Fatalf("token limits = (%d, %d), want (262144, 32768)", cfg.Providers[i].ContextWindow, cfg.Providers[i].MaxOutputTokens)
			}
			if cfg.Providers[i].ReasoningProtocol != "none" || cfg.Providers[i].Headers["X-Title"] != "Reasonix" {
				t.Fatalf("advanced compatibility fields were not persisted: %#v", cfg.Providers[i])
			}
			if got, ok := cfg.Providers[i].ExtraBody["temperature"].(float64); !ok || got != 0.7 {
				t.Fatalf("extra body temperature = %#v, want 0.7", cfg.Providers[i].ExtraBody["temperature"])
			}
		}
	}
	if !found {
		t.Fatal("the saved provider is not visible to a fresh config load")
	}
}

// A whitelist that also raises the provider-wide flag defeats itself: the flag
// answers for every model the list omits, so the text-only half of a mixed
// provider claims image input and the next attached screenshot reaches an
// endpoint that rejects it.
func TestSaveProviderNarrowsVisionToTheListedModels(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"mixed","kind":"openai","baseUrl":"https://gateway.invalid/v1",
		"apiKey":"sk-x","models":["text-only","vision-pro"],
		"default":"text-only","authHeader":false,"noProxy":false,
		"effort":"","vision":["vision-pro"]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers = %d: %s", resp.StatusCode, b)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	textOnly, ok := cfg.ResolveModel("mixed/text-only")
	if !ok {
		t.Fatal("ResolveModel did not find mixed/text-only")
	}
	if config.EffectiveVision(textOnly) {
		t.Fatal("a model outside the saved vision list must stay text-only")
	}
	vision, ok := cfg.ResolveModel("mixed/vision-pro")
	if !ok {
		t.Fatal("ResolveModel did not find mixed/vision-pro")
	}
	if !config.EffectiveVision(vision) {
		t.Fatal("a model in the saved vision list must accept images")
	}
}

func TestSaveProviderRejectsWhatItCannotStore(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	// The status the producer chose now reaches the wire, so this asserts what a
	// client actually branches on: the code. Before, the handler discarded both
	// the code and the 422 and wrote a bare 400.
	for name, tc := range map[string]struct{ body, code string }{
		"a name that would break the model ref": {`{"name":"a/b","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`, "provider.name_invalid"},
		"an empty name":                         {`{"name":"","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`, "provider.name_invalid"},
		"a whitespace-only name":                {`{"name":"   ","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`, "provider.name_invalid"},
		"a Chinese name":                        {`{"name":"公司中转站","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`, "provider.name_invalid"},
		"a Unicode-normalizable name":           {`{"name":"ｒｅｌａｙ","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`, "provider.name_invalid"},
		"a combining-mark name":                 {`{"name":"relay\u0301","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`, "provider.name_invalid"},
		"a name longer than 64 characters":      {`{"name":"` + strings.Repeat("a", 65) + `","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`, "provider.name_invalid"},
		"an unsupported protocol":               {`{"name":"x","kind":"grpc","baseUrl":"https://x.invalid","models":["m"]}`, "provider.kind_unsupported"},
		"no endpoint":                           {`{"name":"x","kind":"openai","baseUrl":"","models":["m"]}`, "provider.endpoint_required"},
		"no models":                             {`{"name":"x","kind":"openai","baseUrl":"https://x.invalid","models":[]}`, "provider.no_models_picked"},
		"a default outside the model list":      {`{"name":"x","kind":"openai","baseUrl":"https://x.invalid","models":["m"],"default":"other"}`, "provider.default_not_selected"},
	} {
		resp := postProvider(t, srv.URL, "/providers", tc.body)
		if resp.StatusCode < 400 || resp.StatusCode >= 500 {
			t.Errorf("%s: status = %d, want a client refusal", name, resp.StatusCode)
		}
		var got Reason
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Errorf("%s: refusal was not a Reason: %v", name, err)
		} else if got.Code != tc.code {
			t.Errorf("%s: code = %q, want %q", name, got.Code, tc.code)
		}
		resp.Body.Close()
	}
}

// The provider an idle conversation is on goes when asked to. With nothing
// else configured the default is cleared, which is what sends the window back
// to connecting a model rather than leaving it on one that no longer resolves.
func TestRemoveProviderInUseGoesWhenIdle(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers/remove", `{"name":"existing"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("removing the idle in-use provider = %d: %s", resp.StatusCode, b)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Provider("existing"); ok || cfg.DefaultModel != "" {
		t.Fatalf("provider kept or default left pointing at it: default=%q", cfg.DefaultModel)
	}
}

// With another source configured, the conversation moves onto it.
func TestRemoveProviderInUseMovesTheConversation(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	add := postProvider(t, srv.URL, "/providers", `{"name":"spare","kind":"openai","baseUrl":"https://x.invalid","apiKey":"sk-spare","models":["m"]}`)
	add.Body.Close()
	resp := postProvider(t, srv.URL, "/providers/remove", `{"name":"existing"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("remove = %d: %s", resp.StatusCode, b)
	}
	if got, _, _ := strings.Cut(currentModelRef(s.ctl()), "/"); got != "spare" {
		t.Fatalf("conversation is on %q, want it moved to spare", currentModelRef(s.ctl()))
	}
}

// A saved default nothing serves is not where the conversation goes: it moves
// to what remains, as a window opening on that config would.
func TestRemoveProviderInUseSkipsAStaleDefault(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	add := postProvider(t, srv.URL, "/providers", `{"name":"spare","kind":"openai","baseUrl":"https://x.invalid","apiKey":"sk-spare","models":["m"]}`)
	add.Body.Close()
	path := config.UserConfigPath()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(body), `default_model = "existing/model-a"`, `default_model = "deepseek-v4-flash"`, 1)
	if stale == string(body) {
		t.Fatalf("fixture has no default_model line to make stale:\n%s", body)
	}
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := postProvider(t, srv.URL, "/providers/remove", `{"name":"existing"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("remove = %d: %s", resp.StatusCode, b)
	}
	if got, _, _ := strings.Cut(currentModelRef(s.ctl()), "/"); got != "spare" {
		t.Fatalf("conversation is on %q, want it moved to spare", currentModelRef(s.ctl()))
	}
}

func TestRemoveProviderDropsAnUnusedOne(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	add := postProvider(t, srv.URL, "/providers", `{"name":"spare","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`)
	add.Body.Close()
	resp := postProvider(t, srv.URL, "/providers/remove", `{"name":"spare"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("remove = %d: %s", resp.StatusCode, b)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == "spare" {
			t.Fatal("the removed provider is still in the config")
		}
	}
}

func TestProvidersListMarksTheOneInUse(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []providerView
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no providers listed")
	}
	for _, p := range got {
		if p.Name == "existing" && !p.InUse {
			t.Fatalf("the running provider is not marked in use: %+v", p)
		}
		if p.Models == nil {
			t.Fatalf("%s has a nil model list, which marshals to null and breaks the client", p.Name)
		}
	}
}

func TestProviderKeyEnvIsDerivedSafely(t *testing.T) {
	for name, want := range map[string]string{
		"kimi":       "KIMI_API_KEY",
		"my-gateway": "MY_GATEWAY_API_KEY",
		"a.b_c":      "A_B_C_API_KEY",
	} {
		if got := providerKeyEnv(name); got != want {
			t.Errorf("providerKeyEnv(%q) = %q, want %q", name, got, want)
		}
	}
}

func readAllString(resp *http.Response) (string, error) {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String(), nil
		}
	}
}

// The panel renders its protocol chooser from this, so a wire the kernel can
// build has to reach it without a frontend release.
func TestProviderProtocolsListsEveryWireASourceMayBeSavedAs(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/providers/protocols")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []struct {
		Kind            string `json:"kind"`
		Discovery       string `json:"discovery"`
		ServerWebSearch bool   `json:"serverWebSearch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	byKind := map[string]bool{}
	for _, p := range got {
		byKind[p.Kind] = p.ServerWebSearch
		if p.Kind == "responses" && p.Discovery != "openai" {
			t.Errorf("responses discovered as %q, want the OpenAI model listing", p.Discovery)
		}
	}
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		if _, ok := byKind[kind]; !ok {
			t.Errorf("protocol catalog is missing %q: %#v", kind, got)
		}
	}
	if !byKind["responses"] {
		t.Error("the Responses wire runs web search on the provider and must say so")
	}
}

func TestSaveProviderAcceptsEveryCatalogedWire(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"deepseek-responses","kind":"responses","baseUrl":"https://api.deepseek.com",
		"apiKey":"sk-value","models":["deepseek-v4-flash"],"default":"deepseek-v4-flash",
		"authHeader":false,"noProxy":false,"effort":"","vision":[]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers with kind=responses = %d: %s", resp.StatusCode, b)
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"responses"`) {
		t.Fatalf("the Responses source did not reach the config:\n%s", raw)
	}
}

func TestSaveProviderTrimsASCIIName(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"  company-relay.1_x  ","kind":"openai","baseUrl":"https://relay.example/v1",
		"models":["m1"]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers with surrounding spaces = %d: %s", resp.StatusCode, b)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Provider("company-relay.1_x"); !ok {
		t.Fatalf("trimmed provider name was not stored: %#v", cfg.Providers)
	}
}

// The panel mirrors this pattern in vendors.ts (sourceNameUsable); the same
// cases are asserted there so the two copies cannot drift apart silently.
func TestProviderNameRE_Boundaries(t *testing.T) {
	cases := map[string]bool{
		"": false, "   ": false,
		"a": true, strings.Repeat("a", 64): true, strings.Repeat("a", 65): false,
		".a": false, "-a": false, "_a": false, "a.": true, "a b": false, "a/b": false,
		"公司中转站": false, "ｒｅｌａｙ": false, "relay\u0301": false,
		" company-relay.1_x ": false, "Relay_1.x-y": true,
	}
	for name, want := range cases {
		if got := providerNameRE.MatchString(name); got != want {
			t.Errorf("providerNameRE(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestRemoveProviderLeavesNoRoleNamingIt(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	add := postProvider(t, srv.URL, "/providers", `{"name":"spare","kind":"openai","baseUrl":"https://x.invalid","models":["m"]}`)
	add.Body.Close()
	path := config.UserConfigPath()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	roles := "[agent]\nguardian_model = \"spare/m\"\nvision_model = \"spare\"\nadvisor_model = \"spare/m\"\n"
	if err := os.WriteFile(path, append(body, []byte("\n"+roles)...), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := postProvider(t, srv.URL, "/providers/remove", `{"name":"spare"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("remove = %d: %s", resp.StatusCode, b)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "spare") {
		t.Fatalf("saved config still names the removed provider:\n%s", saved)
	}
}

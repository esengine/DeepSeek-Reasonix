package serve

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/session/control"

	"reasonix/internal/contract/config"
)

// Three provider blocks on one endpoint (a two-model block plus a single-model
// block per model, each pinning its own price) offered the same two models
// four times.
func TestCollapseModelRoutesKeepsOneEntryPerEndpointModel(t *testing.T) {
	const ep = "https://api.deepseek.com"
	entries := []modelEntry{
		{Ref: "deepseek-flash/deepseek-v4-flash", Provider: "deepseek-flash", Model: "deepseek-v4-flash"},
		{Ref: "deepseek-pro/deepseek-v4-pro", Provider: "deepseek-pro", Model: "deepseek-v4-pro"},
		{Ref: "deepseek/deepseek-v4-flash", Provider: "deepseek", Model: "deepseek-v4-flash", Active: true},
		{Ref: "deepseek/deepseek-v4-pro", Provider: "deepseek", Model: "deepseek-v4-pro"},
	}
	routes := []modelRoute{
		{key: ep + "\x00deepseek-v4-flash", solo: true},
		{key: ep + "\x00deepseek-v4-pro", solo: true},
		{key: ep + "\x00deepseek-v4-flash"},
		{key: ep + "\x00deepseek-v4-pro"},
	}
	got := collapseModelRoutes(entries, routes)
	if len(got) != 2 {
		t.Fatalf("kept %d entries, want 2: %+v", len(got), got)
	}
	// Active wins so the current selection stays selectable; the other model
	// falls to the single-model block, whose price table is the exact one.
	if got[0].Ref != "deepseek-pro/deepseek-v4-pro" || got[1].Ref != "deepseek/deepseek-v4-flash" {
		t.Fatalf("kept %q and %q", got[0].Ref, got[1].Ref)
	}
}

// Same model name at two different vendors is two real choices.
func TestCollapseModelRoutesKeepsDistinctEndpoints(t *testing.T) {
	entries := []modelEntry{
		{Ref: "direct/gpt-x", Model: "gpt-x"},
		{Ref: "proxy/gpt-x", Model: "gpt-x"},
	}
	routes := []modelRoute{{key: "https://a\x00gpt-x"}, {key: "https://b\x00gpt-x"}}
	if got := collapseModelRoutes(entries, routes); len(got) != 2 {
		t.Fatalf("kept %d entries, want both vendors", len(got))
	}
}

func TestModelRouteKeySeparatesAccountsAtOneEndpoint(t *testing.T) {
	a := &config.ProviderEntry{Name: "opencode", BaseURL: "https://opencode.ai/zen/v1", APIKeyEnv: "OPENCODE_API_KEY"}
	b := &config.ProviderEntry{Name: "opencode-free", BaseURL: "https://opencode.ai/zen/v1/", APIKeyEnv: "OPENCODE_FREE_API_KEY"}
	if modelRouteKey(a, "kimi-k3") == modelRouteKey(b, "kimi-k3") {
		t.Fatal("different keys at one endpoint collapsed into one route")
	}
	c := &config.ProviderEntry{Name: "solo", BaseURL: "https://opencode.ai/zen/v1", APIKeyEnv: "OPENCODE_API_KEY"}
	if modelRouteKey(a, "kimi-k3") != modelRouteKey(c, "kimi-k3") {
		t.Fatal("same endpoint and key must still collapse")
	}
}

func newModelListServer(t *testing.T, providers string) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(providers), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".env"), []byte("ACCT_A_KEY=a\nACCT_B_KEY=b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, Label: "m1", SessionDir: testenv.TempDir(t)})
	t.Cleanup(ctrl.Close)
	closeSharedCatalogsOnCleanup(t)
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func TestModelsListsBothAccountsAtOneEndpoint(t *testing.T) {
	srv := newModelListServer(t, `
[[providers]]
name = "acct-a"
kind = "openai"
base_url = "https://example.invalid/v1"
models = ["m1", "m2"]
api_key_env = "ACCT_A_KEY"

[[providers]]
name = "acct-b"
kind = "openai"
base_url = "https://example.invalid/v1"
models = ["m1", "m2"]
api_key_env = "ACCT_B_KEY"
`)
	got := offeredModelRefs(t, srv.URL)
	for _, want := range []string{"acct-a/m1", "acct-a/m2", "acct-b/m1", "acct-b/m2"} {
		if !slices.Contains(got, want) {
			t.Fatalf("offered %v, missing %s", got, want)
		}
	}
}

func TestModelsStillCollapsesSameEndpointAndKey(t *testing.T) {
	srv := newModelListServer(t, `
[[providers]]
name = "multi"
kind = "openai"
base_url = "https://example.invalid/v1"
models = ["m1", "m2"]
api_key_env = "ACCT_A_KEY"

[[providers]]
name = "solo"
kind = "openai"
base_url = "https://example.invalid/v1"
model = "m1"
api_key_env = "ACCT_A_KEY"
`)
	got := offeredModelRefs(t, srv.URL)
	if len(got) != 2 {
		t.Fatalf("offered %v, want one entry per model", got)
	}
}

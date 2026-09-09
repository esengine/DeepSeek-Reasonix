package config

import (
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func searchAssignmentConfig() *Config {
	return &Config{Providers: []ProviderEntry{
		{Name: "first", Kind: "responses", BaseURL: "http://localhost:8000", Models: []string{"m", "org/fast"}, Default: "m", WebSearch: boolPointer(true)},
		{Name: "second", Kind: "anthropic", BaseURL: "http://localhost:8001", Model: "m", WebSearch: boolPointer(true)},
	}}
}

func TestWebSearchModelAssignment(t *testing.T) {
	c := searchAssignmentConfig()
	current := c.Providers[0]
	current.WebSearch = boolPointer(false)
	for _, automatic := range []string{"", "auto", "AUTO"} {
		c.Agent.WebSearchModel = automatic
		if got := c.ResolveWebSearch(&current); got.Status != "disabled" {
			t.Fatalf("automatic disable: %+v", got)
		}
	}
	if err := c.SetWebSearchModel(" first/org/fast "); err != nil {
		t.Fatal(err)
	}
	got := c.ResolveWebSearch(&current)
	if got.Entry == nil || got.Entry.Name != "first" || got.Entry.Model != "org/fast" {
		t.Fatalf("explicit assignment: %+v", got)
	}
	c.Agent.WebSearchModel = "second/m"
	got = c.ResolveWebSearch(&current)
	if got.Entry == nil || got.Entry.Name != "second" {
		t.Fatal("main disable incorrectly overrides explicit account")
	}
	got.Entry.BaseURL = "changed"
	if c.Providers[1].BaseURL == "changed" {
		t.Fatal("route not detached")
	}
	c.Providers[1].WebSearch = boolPointer(false)
	if got = c.ResolveWebSearch(nil); got.Status != "invalid" || got.Entry != nil {
		t.Fatal("disabled assignment fell back")
	}
	c.Providers = c.Providers[:1]
	if got = c.ResolveWebSearch(nil); got.Status != "invalid" {
		t.Fatal("removed assignment fell back")
	}
	if c.Agent.WebSearchModel != "second/m" {
		t.Fatal("lost invalid reference")
	}
	for _, ref := range []string{"first/missing", "missing/m", "first"} {
		if err := c.SetWebSearchModel(ref); err == nil {
			t.Fatalf("accepted %q", ref)
		}
	}
	c.Agent.WebSearchModel = "first/m"
	c.Desktop.ProviderAccess = []string{}
	if c.ResolveWebSearch(nil).Status != "invalid" {
		t.Fatal("access restriction bypassed")
	}
	c.Desktop.ProviderAccess = nil
	c.Environment.Offline = true
	if c.ResolveWebSearch(nil).Status != "disabled" {
		t.Fatal("offline bypassed")
	}
	c.Environment.Offline = false
	c.Tools.Enabled = []string{"read_file"}
	if c.ResolveWebSearch(nil).Status != "disabled" {
		t.Fatal("allowlist bypassed")
	}
}

func TestWebSearchModelCredentialsAndProtocol(t *testing.T) {
	c := searchAssignmentConfig()
	c.Agent.WebSearchModel = "first/m"
	c.Providers[0].BaseURL = "https://search.example"
	c.Providers[0].APIKeyEnv = "REASONIX_SEARCH_TEST_MISSING_KEY"
	t.Setenv("REASONIX_SEARCH_TEST_MISSING_KEY", "")
	if c.ResolveWebSearch(nil).Status != "invalid" {
		t.Fatal("missing credentials accepted")
	}
	c.Providers[0].resolvedAPIKey = "test"
	if c.ResolveWebSearch(nil).Status != "ready" {
		t.Fatal("configured provider rejected")
	}
	c.Providers[0].Kind = "openai"
	if c.ResolveWebSearch(nil).Status != "invalid" {
		t.Fatal("unsupported protocol accepted")
	}
	c.Providers[0].BaseURL = "https://api.deepseek.com"
	got := c.ResolveWebSearch(nil)
	if got.Entry == nil || got.Entry.Kind != "anthropic" || got.Entry.APIKey() != "test" {
		t.Fatal("official conversion lost credentials")
	}
}

func TestWebSearchModelRoundTripAndPreservation(t *testing.T) {
	c := Default()
	c.Agent.WebSearchModel = "search/org/fast"
	var decoded Config
	if _, err := toml.Decode(RenderTOML(c), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Agent.WebSearchModel != c.Agent.WebSearchModel {
		t.Fatal("full render lost assignment")
	}
	if !strings.Contains(RenderTOMLProjectDelta(c), `web_search_model = "search/org/fast"`) {
		t.Fatal("project delta lost assignment")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "# user comment\nfuture_option = true\n[agent]\n# preserved\nweb_search_model = \"auto\"\nfuture_agent_option = 42\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	unlock := LockUserConfigEdits()
	defer unlock()
	if err := c.SaveWebSearchModelTo(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# user comment", "# preserved", "future_option = true", "future_agent_option = 42", `web_search_model = "search/org/fast"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("lost %s: %s", want, raw)
		}
	}
	// Previous schema ignores the added optional field, rather than rejecting TOML.
	var previous struct {
		Agent struct {
			VisionModel string `toml:"vision_model"`
		}
	}
	meta, err := toml.Decode(string(raw), &previous)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, key := range meta.Undecoded() {
		if key.String() == "agent.web_search_model" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected previous reader to ignore assignment")
	}
}

package config

import (
	"errors"
	"testing"
)

func TestWebSearchExplicitAssignmentNeverFallsBack(t *testing.T) {
	on, off := true, false
	c := &Config{Providers: []ProviderEntry{
		{Name: "first", Kind: "anthropic", Model: "one", Models: []string{"one", "two"}, BaseURL: "https://search.invalid", WebSearch: &on},
		{Name: "other", Kind: "responses", Model: "fallback", WebSearch: &on},
	}}
	for _, ref := range []string{"lost/one", "first/missing", "one"} {
		c.Agent.WebSearchModel = ref
		got := c.ResolveWebSearch(nil)
		if got.Entry != nil || !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
			t.Fatalf("%s = %+v", ref, got)
		}
	}
	c.Agent.WebSearchModel = "first/two"
	got := c.ResolveWebSearch(nil)
	if got.Err != nil || got.Entry == nil || got.Entry.Model != "two" || got.Entry.Name != "first" {
		t.Fatalf("explicit = %+v", got)
	}
	c.Providers[0].WebSearch = &off
	if got := c.ResolveWebSearch(nil); got.Entry != nil || !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
		t.Fatalf("disabled explicit = %+v", got)
	}
	c.Agent.WebSearchModel = "auto"
	if got := c.ResolveWebSearch(&c.Providers[0]); got.Entry != nil {
		t.Fatalf("current disable lost: %+v", got)
	}
	c.Agent.WebSearchModel = ""
	if got := c.ResolveWebSearch(nil); got.Entry == nil || got.Entry.Name != "other" {
		t.Fatalf("automatic = %+v", got)
	}
}

func TestWebSearchOfficialChatRouteUsesMessagesWithoutChangingAccount(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{Name: "account", Kind: "openai", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}}}
	c.Providers[0] = c.Providers[0].WithAPIKeyForProbe("fixture-key")
	c.Agent.WebSearchModel = "account/deepseek-v4-flash"
	got := c.ResolveWebSearch(nil)
	if got.Err != nil || got.Entry == nil {
		t.Fatalf("selection = %+v", got)
	}
	if got.Entry.Name != "account" || got.Entry.Kind != "anthropic" || got.Entry.BaseURL != "https://api.deepseek.com/anthropic" {
		t.Fatalf("route = %+v", got.Entry)
	}
	if c.Providers[0].Kind != "openai" {
		t.Fatal("selection mutated source")
	}
	c.Providers[0].RequestURL = "https://custom.invalid/chat"
	if got := c.ResolveWebSearch(nil); !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
		t.Fatalf("custom route redirected: %+v", got)
	}
}

func TestWebSearchModelSurvivesProviderRemovalAsUnavailable(t *testing.T) {
	on := true
	c := &Config{Providers: []ProviderEntry{{Name: "selected", Kind: "anthropic", Model: "search", WebSearch: &on}, {Name: "other", Kind: "responses", Model: "fallback", WebSearch: &on}}}
	c.Agent.WebSearchModel = "selected/search"
	if err := c.RemoveProvider("selected"); err != nil {
		t.Fatal(err)
	}
	if c.Agent.WebSearchModel != "selected/search" || !errors.Is(c.ResolveWebSearch(nil).Err, ErrWebSearchModelUnavailable) {
		t.Fatal("provider removal silently changed the search assignment")
	}
}

func TestWebSearchHonorsAccessCredentialsOfflineAndToolSelection(t *testing.T) {
	on := true
	e := ProviderEntry{Name: "search", Kind: "anthropic", Model: "fixture", BaseURL: "https://fixture.invalid", WebSearch: &on, APIKeyEnv: "FIXTURE_SEARCH_KEY"}
	c := &Config{Providers: []ProviderEntry{e}}
	c.Agent.WebSearchModel = "search/fixture"
	if got := c.ResolveWebSearch(nil); !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
		t.Fatalf("missing credentials = %+v", got)
	}
	c.Providers[0] = e.WithAPIKeyForProbe("fixture")
	c.Desktop.ProviderAccess = []string{}
	if got := c.ResolveWebSearch(nil); !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
		t.Fatalf("removed connection = %+v", got)
	}
	c.Desktop.ProviderAccess = nil
	c.Environment.Offline = true
	if got := c.ResolveWebSearch(nil); got.Entry != nil || got.Err != nil {
		t.Fatalf("offline = %+v", got)
	}
	c.Environment.Offline = false
	c.Tools.Enabled = []string{"read_file"}
	if got := c.ResolveWebSearch(nil); got.Entry != nil || got.Err != nil {
		t.Fatalf("tool disabled = %+v", got)
	}
}

func TestWebSearchAutomaticOpenCodeSelectionStaysOnTheAccount(t *testing.T) {
	on := true
	current := ProviderEntry{Name: "chat", Kind: "openai", Model: "deepseek-v4-pro", BaseURL: "https://opencode.ai/zen/go/v1", APIKeyEnv: "ACCOUNT_A_KEY"}
	other := ProviderEntry{Name: "other", Kind: "anthropic", Model: "deepseek-v4-flash", BaseURL: "https://opencode.ai/zen/go", APIKeyEnv: "ACCOUNT_B_KEY", WebSearch: &on}
	other = other.WithAPIKeyForProbe("fixture")
	c := &Config{Providers: []ProviderEntry{other}}
	if got := c.ResolveWebSearch(&current); got.Entry != nil {
		t.Fatal("automatic search changed OpenCode accounts")
	}
	c.Providers[0].APIKeyEnv = current.APIKeyEnv
	if got := c.ResolveWebSearch(&current); got.Entry == nil || got.Entry.Name != "other" {
		t.Fatalf("matching account route = %+v", got)
	}
}

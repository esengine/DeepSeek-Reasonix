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
		got := c.ResolveWebSearch()
		if got.Entry != nil || !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
			t.Fatalf("%s = %+v", ref, got)
		}
	}
	c.Agent.WebSearchModel = "first/two"
	got := c.ResolveWebSearch()
	if got.Err != nil || got.Entry == nil || got.Entry.Model != "two" || got.Entry.Name != "first" {
		t.Fatalf("explicit = %+v", got)
	}
	c.Providers[0].WebSearch = &off
	if got := c.ResolveWebSearch(); got.Entry != nil || !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
		t.Fatalf("disabled explicit = %+v", got)
	}
	for _, ref := range []string{"", "auto"} {
		c.Agent.WebSearchModel = ref
		if got := c.ResolveWebSearch(); got.Entry != nil || got.Err != nil {
			t.Fatalf("%q must leave search to the conversation provider, got %+v", ref, got)
		}
	}
}

func TestWebSearchOfficialChatRouteUsesMessagesWithoutChangingAccount(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{Name: "account", Kind: "openai", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}}}
	c.Providers[0] = c.Providers[0].WithAPIKeyForProbe("fixture-key")
	c.Agent.WebSearchModel = "account/deepseek-v4-flash"
	got := c.ResolveWebSearch()
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
	if got := c.ResolveWebSearch(); !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
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
	if c.Agent.WebSearchModel != "selected/search" || !errors.Is(c.ResolveWebSearch().Err, ErrWebSearchModelUnavailable) {
		t.Fatal("provider removal silently changed the search assignment")
	}
}

func TestWebSearchHonorsAccessCredentialsOfflineAndToolSelection(t *testing.T) {
	on := true
	e := ProviderEntry{Name: "search", Kind: "anthropic", Model: "fixture", BaseURL: "https://fixture.invalid", WebSearch: &on, APIKeyEnv: "FIXTURE_SEARCH_KEY"}
	c := &Config{Providers: []ProviderEntry{e}}
	c.Agent.WebSearchModel = "search/fixture"
	if got := c.ResolveWebSearch(); !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
		t.Fatalf("missing credentials = %+v", got)
	}
	c.Providers[0] = e.WithAPIKeyForProbe("fixture")
	c.Desktop.ProviderAccess = []string{}
	if got := c.ResolveWebSearch(); !errors.Is(got.Err, ErrWebSearchModelUnavailable) {
		t.Fatalf("removed connection = %+v", got)
	}
	c.Desktop.ProviderAccess = nil
	c.Environment.Offline = true
	if got := c.ResolveWebSearch(); got.Entry != nil || got.Err != nil {
		t.Fatalf("offline = %+v", got)
	}
	c.Environment.Offline = false
	c.Tools.Enabled = []string{"read_file"}
	if got := c.ResolveWebSearch(); got.Entry != nil || got.Err != nil {
		t.Fatalf("tool disabled = %+v", got)
	}
}

func TestWebSearchUnavailableCarriesItsReason(t *testing.T) {
	on, off := true, false
	e := ProviderEntry{Name: "s", Kind: "anthropic", Model: "m", BaseURL: "https://fixture.invalid", WebSearch: &on, APIKeyEnv: "FIXTURE_REASON_KEY"}
	c := &Config{Providers: []ProviderEntry{e}}
	cases := []struct {
		ref    string
		mutate func()
		want   WebSearchReason
	}{
		{"nonsense", func() {}, WebSearchBadRef},
		{"s/m", func() { c.Desktop.ProviderAccess = []string{} }, WebSearchNotAdded},
		{"s/gone", func() { c.Desktop.ProviderAccess = nil }, WebSearchModelRemoved},
		{"s/m", func() { c.Providers[0].WebSearch = &off }, WebSearchUnsupported},
		{"s/m", func() { c.Providers[0].WebSearch = &on }, WebSearchNoCredentials},
	}
	for _, tc := range cases {
		tc.mutate()
		c.Agent.WebSearchModel = tc.ref
		_, err := c.ResolveWebSearchModel(tc.ref)
		got, ok := WebSearchReasonOf(err)
		if !ok || got != tc.want || !errors.Is(err, ErrWebSearchModelUnavailable) {
			t.Fatalf("%s: reason = %q ok=%v err=%v, want %q", tc.ref, got, ok, err, tc.want)
		}
		if out := c.WebSearchOutcome(); out.Reason != tc.want || out.Ref != "" {
			t.Fatalf("%s: outcome = %+v", tc.ref, out)
		}
	}
}

func TestWebSearchOutcomeNamesWhoActuallySearches(t *testing.T) {
	on, off := true, false
	chat := ProviderEntry{Name: "chat", Kind: "anthropic", Model: "main", BaseURL: "https://chat.invalid", WebSearch: &on}
	other := ProviderEntry{Name: "other", Kind: "anthropic", Model: "side", BaseURL: "https://other.invalid", WebSearch: &on}
	c := &Config{DefaultModel: "chat/main", Providers: []ProviderEntry{chat, other}}
	if out := c.WebSearchOutcome(); !out.Auto || out.Ref != "chat/main" {
		t.Fatalf("auto = %+v", out)
	}
	c.Providers[0].WebSearch = &off
	if out := c.WebSearchOutcome(); !out.Auto || out.Ref != "" {
		t.Fatalf("auto must not borrow another account: %+v", out)
	}
}

func TestWebSearchAssignmentSilencesTheConversationsOwnTool(t *testing.T) {
	on := true
	c := &Config{DefaultModel: "chat/main", Providers: []ProviderEntry{
		{Name: "chat", Kind: "anthropic", Model: "main", BaseURL: "https://chat.invalid", WebSearch: &on},
		{Name: "search", Kind: "anthropic", Model: "s", BaseURL: "https://search.invalid", WebSearch: &on},
	}}
	e, _ := c.ResolveModel("chat/main")
	if !EffectiveWebSearch(e) {
		t.Fatal("unset assignment must keep the conversation's built-in search")
	}
	c.Agent.WebSearchModel = "search/s"
	e, _ = c.ResolveModel("chat/main")
	if EffectiveWebSearch(e) {
		t.Fatal("an assigned search model must be the only search route")
	}
	if !EffectiveIndependentWebSearch(c.Providers[1].forModel("s")) {
		t.Fatal("the assignment must not disable the model it names")
	}
}

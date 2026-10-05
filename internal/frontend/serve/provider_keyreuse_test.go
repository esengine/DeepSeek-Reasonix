package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
)

func savedProvider(t *testing.T, name string) *config.ProviderEntry {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Provider(name)
	if !ok {
		t.Fatalf("provider %q was not saved", name)
	}
	return entry
}

// Adding the same endpoint under the other protocol is another door onto one
// account. Without a shared credential slot the second entry has no key, and
// the pair reads as two accounts in a list that groups on host and key.
func TestAddingASecondDoorReusesTheHostsKey(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	first := postProvider(t, srv.URL, "/providers", `{
		"name":"acme","kind":"openai","baseUrl":"https://api.acme.test/v1",
		"apiKey":"sk-acme","models":["m1"],"default":"m1"
	}`)
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first save = %d", first.StatusCode)
	}

	// No key: the user is adding the other protocol for the account already here.
	second := postProvider(t, srv.URL, "/providers", `{
		"name":"acme-anthropic","kind":"anthropic","baseUrl":"https://api.acme.test/anthropic",
		"apiKey":"","models":["m1"],"default":"m1"
	}`)
	second.Body.Close()
	if second.StatusCode != http.StatusOK {
		t.Fatalf("second save = %d", second.StatusCode)
	}

	one, two := savedProvider(t, "acme"), savedProvider(t, "acme-anthropic")
	if one.APIKeyEnv != two.APIKeyEnv {
		t.Fatalf("key slots differ: %q vs %q — the doors would read as two accounts", one.APIKeyEnv, two.APIKeyEnv)
	}
	if two.APIKey() == "" {
		t.Fatal("the second door has no credential, so nothing it lists is selectable")
	}
}

// A key of its own means a different account — two tenants of one relay. Reusing
// the slot there would overwrite the first tenant's credential.
func TestAddingASecondAccountAtOneHostKeepsItsOwnKey(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	postProvider(t, srv.URL, "/providers", `{
		"name":"relay","kind":"openai","baseUrl":"https://relay.test/v1",
		"apiKey":"sk-one","models":["m1"],"default":"m1"
	}`).Body.Close()
	postProvider(t, srv.URL, "/providers", `{
		"name":"relay-work","kind":"openai","baseUrl":"https://relay.test/v1",
		"apiKey":"sk-two","models":["m1"],"default":"m1"
	}`).Body.Close()

	one, two := savedProvider(t, "relay"), savedProvider(t, "relay-work")
	if one.APIKeyEnv == two.APIKeyEnv {
		t.Fatalf("both tenants share slot %q, so saving the second overwrote the first", one.APIKeyEnv)
	}
	if one.APIKey() != "sk-one" || two.APIKey() != "sk-two" {
		t.Fatalf("credentials crossed: %q and %q", one.APIKey(), two.APIKey())
	}
}

// A first source at an unseen host has nothing to inherit.
func TestFirstSourceAtAHostGetsItsOwnKeySlot(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	postProvider(t, srv.URL, "/providers", `{
		"name":"fresh","kind":"openai","baseUrl":"https://fresh.test/v1",
		"apiKey":"sk-fresh","models":["m1"],"default":"m1"
	}`).Body.Close()

	if got := savedProvider(t, "fresh").APIKeyEnv; got != "FRESH_API_KEY" {
		t.Fatalf("key slot = %q, want the one derived from its own name", got)
	}
}

// A slot is derived from a name, and two names can fold to one slot: "Existing"
// and "existing" both make EXISTING_API_KEY. Writing a new account's key there
// replaces the credential the other provider is using, with nothing said.
func TestANewSourceNeverWritesAnotherProvidersKeySlot(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	if _, err := config.SetCredential("EXISTING_API_KEY", "sk-existing"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"Existing","kind":"openai","baseUrl":"https://relay.test/v1",
		"apiKey":"sk-relay","models":["m1"],"default":"m1"
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", resp.StatusCode)
	}

	old, added := savedProvider(t, "existing"), savedProvider(t, "Existing")
	if old.APIKey() != "sk-existing" {
		t.Fatalf("the existing provider's key became %q: adding a source overwrote it", old.APIKey())
	}
	if added.APIKeyEnv == old.APIKeyEnv || added.APIKey() != "sk-relay" {
		t.Fatalf("new source slot %q holds %q, want its own slot holding sk-relay", added.APIKeyEnv, added.APIKey())
	}
}

// Without a key and at another host, taking the folded slot would hand this
// endpoint the other provider's credential.
func TestAKeylessSourceElsewhereDoesNotBorrowAFoldedSlot(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	if _, err := config.SetCredential("EXISTING_API_KEY", "sk-existing"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"Existing","kind":"openai","baseUrl":"https://relay.test/v1",
		"apiKey":"","models":["m1"],"default":"m1"
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", resp.StatusCode)
	}
	if added := savedProvider(t, "Existing"); added.APIKey() != "" {
		t.Fatalf("a keyless source at another host resolved %q from slot %q", added.APIKey(), added.APIKeyEnv)
	}
}

func reasonCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var got Reason
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("status %d carried no Reason: %v", resp.StatusCode, err)
	}
	return got.Code
}

// Adding is not editing. Upserting over a same-named entry kept that entry's
// slot, so a keyless source at a relay resolved the old key and sent it there.
func TestAddingUnderAnExistingNameIsRefused(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	if _, err := config.SetCredential("EXISTING_API_KEY", "sk-existing"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"existing","kind":"openai","baseUrl":"https://relay.test/v1",
		"apiKey":"","models":["m1"],"default":"m1"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || reasonCode(t, resp) != "provider.name_taken" {
		t.Fatalf("adding under a taken name = %d, want 409 provider.name_taken", resp.StatusCode)
	}
	if got := savedProvider(t, "existing"); got.BaseURL != "https://example.invalid/v1" || got.APIKey() != "sk-existing" {
		t.Fatalf("existing entry became %s holding %q", got.BaseURL, got.APIKey())
	}
}

// Removing a provider leaves its credential stored, and a slot is not free just
// because no config entry names it any more.
func TestAKeylessSourceDoesNotInheritARemovedProvidersKey(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	postProvider(t, srv.URL, "/providers", `{
		"name":"Relay","kind":"openai","baseUrl":"https://relay-a.test/v1",
		"apiKey":"sk-a","models":["m1"],"default":"m1"
	}`).Body.Close()
	postProvider(t, srv.URL, "/providers/remove", `{"name":"Relay"}`).Body.Close()
	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"relay","kind":"openai","baseUrl":"https://relay-b.test/v1",
		"apiKey":"","models":["m1"],"default":"m1"
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", resp.StatusCode)
	}
	if got := savedProvider(t, "relay"); got.APIKey() != "" {
		t.Fatalf("relay-b resolved %q from slot %q, the removed relay-a's key", got.APIKey(), got.APIKeyEnv)
	}
}

// Onboarding fills in the entry a fresh install already names, so it replaces
// on purpose. At that entry's host it keeps the slot; elsewhere it gets its own.
func TestReplacingAnEntryKeepsItsSlotOnlyAtItsHost(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers", `{
		"name":"existing","kind":"openai","baseUrl":"https://example.invalid/v1","replace":true,
		"apiKey":"sk-same","models":["m1"],"default":"m1"
	}`)
	resp.Body.Close()
	if got := savedProvider(t, "existing"); resp.StatusCode != http.StatusOK || got.APIKeyEnv != "EXISTING_API_KEY" || got.APIKey() != "sk-same" {
		t.Fatalf("replace at the same host = %d, slot %q holding %q", resp.StatusCode, got.APIKeyEnv, got.APIKey())
	}

	resp = postProvider(t, srv.URL, "/providers", `{
		"name":"existing","kind":"openai","baseUrl":"https://relay.test/v1","replace":true,
		"apiKey":"sk-relay","models":["m1"],"default":"m1"
	}`)
	resp.Body.Close()
	got := savedProvider(t, "existing")
	if resp.StatusCode != http.StatusOK || got.APIKeyEnv == "EXISTING_API_KEY" || got.APIKey() != "sk-relay" {
		t.Fatalf("replace at another host = %d, slot %q holding %q", resp.StatusCode, got.APIKeyEnv, got.APIKey())
	}
}

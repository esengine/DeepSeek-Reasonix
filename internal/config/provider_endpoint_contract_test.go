package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeepSeekEndpointContract(t *testing.T) {
	preset, ok := CuratedProviderPreset("deepseek-anthropic")
	if !ok {
		t.Fatal("hidden DeepSeek preset missing")
	}
	catalog := CatalogForProviderPreset(preset)
	want := map[string]string{
		"openai":    "https://api.deepseek.com/v1/chat/completions",
		"responses": "https://api.deepseek.com/responses",
		"anthropic": "https://api.deepseek.com/anthropic/v1/messages",
	}
	for kind, requestURL := range want {
		route, exists := catalog.Protocols[kind]
		if !exists {
			t.Fatalf("DeepSeek %s route missing", kind)
		}
		if got := ProviderRequestURL(kind, route.BaseURL); got != requestURL {
			t.Fatalf("DeepSeek %s request URL = %q, want %q", kind, got, requestURL)
		}
	}
}

func TestProviderEndpointMismatchIsConservative(t *testing.T) {
	cases := []struct {
		name     string
		entry    ProviderEntry
		mismatch bool
	}{
		{"foreign standard suffix", ProviderEntry{Kind: "openai", RequestURL: "https://gateway.test/v1/messages"}, true},
		{"deepseek hybrid path", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/anthropic/v1/chat/completions"}, true},
		{"deepseek official chat", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/v1/chat/completions"}, false},
		{"mimo shared openai and responses root", ProviderEntry{Name: "mimo-api", Kind: "openai", BaseURL: "https://api.xiaomimimo.com/v1"}, false},
		{"deepseek accepted root alias", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/chat/completions"}, false},
		{"custom host", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://gateway.test/anthropic/v1/chat/completions"}, false},
		{"query override", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/anthropic/v1/chat/completions?token=x"}, false},
		{"unknown path", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/custom/route"}, false},
		{"custom path in foreign namespace", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/anthropic/custom/chat/completions"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProviderEndpointMismatchForEntry(&tc.entry)
			if (got != nil) != tc.mismatch {
				t.Fatalf("mismatch = %+v, want %v", got, tc.mismatch)
			}
			if got != nil && tc.entry.Name == "deepseek-anthropic" && got.Recommended != "https://api.deepseek.com/v1/chat/completions" {
				t.Fatalf("recommendation = %q", got.Recommended)
			}
		})
	}
}

func TestCatalogForProviderEntryUsesHiddenLegacyPreset(t *testing.T) {
	id, catalog, ok := CatalogForProviderEntry(&ProviderEntry{Name: "deepseek-anthropic"})
	if !ok || id != "deepseek-anthropic" || catalog.BrandID != "deepseek" || catalog.Region != "global" || catalog.Product != "api" {
		t.Fatalf("catalog identity = %q %+v %v", id, catalog, ok)
	}
}

func TestRepairProviderEndpointContractUsesExactCatalogRoute(t *testing.T) {
	legacyStateful := true
	entry := ProviderEntry{
		Name: "deepseek-anthropic", DisplayName: "Deepseek2", PresetID: "deepseek-anthropic",
		Kind: "responses", BaseURL: "https://api.deepseek.com", RequestURL: "https://api.deepseek.com/anthropic/v1/messages",
		ChatURL: "https://stale.example/chat/completions", Models: []string{"deepseek-v4-flash"}, Default: "deepseek-v4-flash",
		APIKeyEnv: "MY_DEEPSEEK_KEY", Headers: map[string]string{"X-Test": "keep"}, ExtraBody: map[string]any{"future": "keep"},
		ContextWindow: 1_000_000, ResponsesMode: "stateful", ResponsesStateful: &legacyStateful,
	}
	repair, changed := RepairProviderEndpointContract(&entry)
	if !changed || repair == nil {
		t.Fatal("expected exact DeepSeek route to be repaired")
	}
	if repair.ProviderName != "Deepseek2" || repair.FromProtocol != "responses" || repair.ToProtocol != "anthropic" ||
		repair.RequestURL != "https://api.deepseek.com/anthropic/v1/messages" {
		t.Fatalf("repair = %+v", repair)
	}
	if entry.Kind != "anthropic" || entry.BaseURL != "https://api.deepseek.com/anthropic" ||
		entry.RequestURL != "" || entry.ChatURL != "" || entry.ResponsesMode != "" || entry.ResponsesStateful != nil {
		t.Fatalf("repaired entry = %+v", entry)
	}
	if got := ProviderEffectiveRequestURL(&entry); got != "https://api.deepseek.com/anthropic/v1/messages" {
		t.Fatalf("effective request URL = %q", got)
	}
	if entry.Name != "deepseek-anthropic" || entry.PresetID != "deepseek-anthropic" ||
		entry.APIKeyEnv != "MY_DEEPSEEK_KEY" || entry.ContextWindow != 1_000_000 ||
		!reflect.DeepEqual(entry.Models, []string{"deepseek-v4-flash"}) ||
		!reflect.DeepEqual(entry.Headers, map[string]string{"X-Test": "keep"}) ||
		!reflect.DeepEqual(entry.ExtraBody, map[string]any{"future": "keep"}) {
		t.Fatalf("repair discarded user-owned fields: %+v", entry)
	}
	if second, changed := RepairProviderEndpointContract(&entry); changed || second != nil {
		t.Fatalf("repair is not idempotent: %+v changed=%v", second, changed)
	}
}

func TestRepairProviderEndpointContractAppliesTargetRouteOptions(t *testing.T) {
	tests := []struct {
		name        string
		entry       ProviderEntry
		wantKind    string
		wantBaseURL string
		wantMode    string
		wantAuth    bool
		wantRequest string
	}{
		{
			name: "responses",
			entry: ProviderEntry{Name: "deepseek-anthropic", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
				RequestURL: "https://api.deepseek.com/responses", AuthHeader: true},
			wantKind: "responses", wantBaseURL: "https://api.deepseek.com", wantMode: "stateless",
			wantRequest: "https://api.deepseek.com/responses",
		},
		{
			name: "openai",
			entry: ProviderEntry{Name: "deepseek-anthropic", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
				RequestURL: "https://api.deepseek.com/v1/chat/completions", AuthHeader: true, ResponsesMode: "stateful"},
			wantKind: "openai", wantBaseURL: "https://api.deepseek.com/v1",
			wantRequest: "https://api.deepseek.com/v1/chat/completions",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, changed := RepairProviderEndpointContract(&tc.entry); !changed {
				t.Fatal("expected repair")
			}
			if tc.entry.Kind != tc.wantKind || tc.entry.BaseURL != tc.wantBaseURL ||
				tc.entry.ResponsesMode != tc.wantMode || tc.entry.AuthHeader != tc.wantAuth ||
				ProviderEffectiveRequestURL(&tc.entry) != tc.wantRequest {
				t.Fatalf("entry = %+v, request = %q", tc.entry, ProviderEffectiveRequestURL(&tc.entry))
			}
		})
	}
}

func TestRepairProviderEndpointContractPreservesUnprovenRoutes(t *testing.T) {
	tests := []ProviderEntry{
		{Name: "deepseek-anthropic", Kind: "responses", RequestURL: "https://gateway.test/v1/messages"},
		{Name: "deepseek-anthropic", Kind: "responses", RequestURL: "https://api.deepseek.com/anthropic/v1/messages?route=custom"},
		{Name: "deepseek-anthropic", Kind: "responses", RequestURL: "https://user@api.deepseek.com/anthropic/v1/messages"},
		{Name: "deepseek-anthropic", Kind: "responses", RequestURL: "https://api.deepseek.com/custom/messages"},
		{Name: "custom", Kind: "responses", RequestURL: "https://api.deepseek.com/anthropic/v1/messages"},
	}
	for _, entry := range tests {
		before := entry
		if repair, changed := RepairProviderEndpointContract(&entry); changed || repair != nil {
			t.Fatalf("unexpected repair for %+v: %+v", before, repair)
		}
		if !reflect.DeepEqual(entry, before) {
			t.Fatalf("unproven route changed: before=%+v after=%+v", before, entry)
		}
	}
}

func TestProjectProviderEndpointRepairIsRuntimeOnly(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "reasonix.toml")
	raw := `[[providers]]
name = "deepseek-anthropic"
preset_id = "deepseek-anthropic"
kind = "responses"
base_url = "https://api.deepseek.com"
request_url = "https://api.deepseek.com/anthropic/v1/messages"
models = ["deepseek-v4-flash"]
default = "deepseek-v4-flash"
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, root)
	cfg, err := LoadForRootWithoutCredentialsReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("deepseek-anthropic")
	if !ok || p.Kind != "anthropic" || p.BaseURL != "https://api.deepseek.com/anthropic" ||
		ProviderEffectiveRequestURL(p) != "https://api.deepseek.com/anthropic/v1/messages" {
		t.Fatalf("runtime provider = %+v found=%v", p, ok)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != raw {
		t.Fatalf("project config was rewritten:\n%s", after)
	}
}

func TestRewriteProviderEndpointContractsPreservesInlineUnknownFields(t *testing.T) {
	raw := `config_version = 10 # keep
providers = [{name="deepseek-anthropic", preset_id="deepseek-anthropic", kind="anthropic", base_url="https://api.deepseek.com/anthropic", request_url="https://api.deepseek.com/responses", responses_stateful=true, api_key_env="MY_KEY", models=["deepseek-v4-flash"], future={value="keep"}}] # keep inline
`
	next, repairs, err := rewriteProviderEndpointContracts(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(repairs) != 1 || repairs[0].FromProtocol != "anthropic" || repairs[0].ToProtocol != "responses" {
		t.Fatalf("repairs = %+v", repairs)
	}
	for _, want := range []string{
		"config_version = 10 # keep",
		`kind="responses"`,
		`base_url="https://api.deepseek.com"`,
		`request_url=""`,
		`responses_mode = "stateless"`,
		`api_key_env="MY_KEY"`,
		`future={value="keep"}`,
		"# keep inline",
	} {
		if !strings.Contains(next, want) {
			t.Fatalf("rewritten inline config missing %q:\n%s", want, next)
		}
	}
	if strings.Contains(next, "responses_stateful") {
		t.Fatalf("legacy responses state survived:\n%s", next)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := LoadForEdit(path)
	p, ok := loaded.Provider("deepseek-anthropic")
	if !ok || p.Kind != "responses" || p.ResponsesMode != "stateless" ||
		ProviderEffectiveRequestURL(p) != "https://api.deepseek.com/responses" {
		t.Fatalf("reloaded inline provider = %+v found=%v", p, ok)
	}
}

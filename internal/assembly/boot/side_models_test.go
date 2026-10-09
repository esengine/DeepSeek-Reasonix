package boot

import (
	"errors"
	"slices"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

func recordSideEfforts(t *testing.T) *[]string {
	t.Helper()
	var efforts []string
	orig := newSideProvider
	newSideProvider = func(kind string, cfg provider.Config) (provider.Provider, error) {
		efforts = append(efforts, cfg.Extra["effort"].(string))
		return orig(kind, cfg)
	}
	t.Cleanup(func() { newSideProvider = orig })
	return &efforts
}

func sideEntry(t *testing.T, e config.ProviderEntry) *config.ProviderEntry {
	t.Helper()
	t.Setenv("SIDE_MODEL_TEST_KEY", "sk-test")
	e.APIKeyEnv = "SIDE_MODEL_TEST_KEY"
	e.ResolveAPIKeyFromProcessEnvForProbe()
	return &e
}

func TestSideProviderEffortsUsed(t *testing.T) {
	cases := []struct {
		name  string
		entry config.ProviderEntry
		want  []string
	}{
		{"generic without vocabulary retries on automatic depth", config.ProviderEntry{Name: "luna", Kind: "openai", BaseURL: "https://relay.example.com/v1", Model: "gpt-6-luna", Effort: "high"}, []string{"disabled", ""}},
		{"generic with vocabulary retries on its lowest level", config.ProviderEntry{Name: "luna", Kind: "openai", BaseURL: "https://relay.example.com/v1", Model: "gpt-6-luna", Effort: "high", SupportedEfforts: []string{"high", "medium", "low"}}, []string{"disabled", "low"}},
		{"deepseek accepts disabled", config.ProviderEntry{Name: "ds", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Effort: "high"}, []string{"disabled"}},
		{"minimax accepts disabled", config.ProviderEntry{Name: "mm", Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}, []string{"disabled"}},
		{"zhipu accepts disabled", config.ProviderEntry{Name: "glm", Kind: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.2"}, []string{"disabled"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := recordSideEfforts(t)
			e := sideEntry(t, c.entry)
			if promptRefiner(e, netclient.ProxySpec{}, nil) == nil {
				t.Fatal("promptRefiner is nil")
			}
			if !slices.Equal(*got, c.want) {
				t.Fatalf("efforts tried = %q, want %q", *got, c.want)
			}
			*got = nil
			if commitMessenger(e, netclient.ProxySpec{}, nil) == nil {
				t.Fatal("commitMessenger is nil")
			}
			if !slices.Equal(*got, c.want) {
				t.Fatalf("messenger efforts tried = %q, want %q", *got, c.want)
			}
		})
	}
}

func TestSideProviderRetriesOnlyAnEffortRefusal(t *testing.T) {
	got := recordSideEfforts(t)
	if _, err := sideProvider(nil, netclient.ProxySpec{}); !errors.Is(err, errSideModelUnconfigured) {
		t.Fatalf("nil entry: want errSideModelUnconfigured, got %v", err)
	}
	if len(*got) != 0 {
		t.Fatalf("an unconfigured entry built a provider: %q", *got)
	}
	for name, e := range map[string]config.ProviderEntry{
		"unknown kind":     {Name: "x", Kind: "no-such-kind", BaseURL: "https://relay.example.com/v1", Model: "m"},
		"missing base url": {Name: "x", Kind: "openai", Model: "m"},
	} {
		*got = nil
		_, err := sideProvider(sideEntry(t, e), netclient.ProxySpec{})
		if err == nil || errors.Is(err, errSideModelUnconfigured) || errors.Is(err, provider.ErrEffortRefused) {
			t.Fatalf("%s: want a plain construction error, got %v", name, err)
		}
		if len(*got) != 1 {
			t.Fatalf("%s: built %d times, want 1", name, len(*got))
		}
	}
}

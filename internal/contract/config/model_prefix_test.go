package config

import (
	"os"
	"strings"
	"testing"
)

func labelOf(e ProviderEntry, model string) string { return e.ModelShort(model) }

func TestHideModelPrefixDropsTheSharedGroupFromTheDisplayOnly(t *testing.T) {
	e := ProviderEntry{Models: []string{"group/deepseek-v4.1-flash", "group/glm-5-flash"}, HideModelPrefix: true}
	if got := labelOf(e, "group/deepseek-v4.1-flash"); got != "deepseek-v4.1-flash" {
		t.Fatalf("short = %q", got)
	}
	if got := labelOf(e, "group/glm-5-flash"); got != "glm-5-flash" {
		t.Fatalf("short = %q", got)
	}
	if e.Models[0] != "group/deepseek-v4.1-flash" {
		t.Fatal("the model id the wire carries was changed")
	}
}

func TestHideModelPrefixIsOffUnlessAskedAndNeedsAPrefixEveryModelShares(t *testing.T) {
	off := ProviderEntry{Models: []string{"group/a", "group/b"}}
	if got := labelOf(off, "group/a"); got != "" {
		t.Fatalf("off by default, short = %q", got)
	}
	mixed := ProviderEntry{Models: []string{"group/a", "other/b", "plain"}, HideModelPrefix: true}
	for _, m := range mixed.Models {
		if got := labelOf(mixed, m); got != "" {
			t.Fatalf("no prefix is shared, yet %q reads %q", m, got)
		}
	}
	nested := ProviderEntry{Models: []string{"team/group/a", "team/group/b"}, HideModelPrefix: true}
	if got := labelOf(nested, "team/group/a"); got != "a" {
		t.Fatalf("nested short = %q, want the whole shared path dropped", got)
	}
}

func TestHideModelPrefixKeepsTheModelsThatWouldBeIndistinguishable(t *testing.T) {
	e := ProviderEntry{Models: []string{"group/x", "group/y"}, ModelAliases: map[string]string{"group/y": "x"}, HideModelPrefix: true}
	if got := labelOf(e, "group/x"); got != "" {
		t.Fatalf("group/x would read the same as the alias of group/y, short = %q", got)
	}
}

func TestHideModelPrefixRoundTripsThroughTheConfigFile(t *testing.T) {
	cfg := &Config{Providers: []ProviderEntry{{Name: "mag", Kind: "openai", BaseURL: "https://m.example/v1", Models: []string{"group/a"}, HideModelPrefix: true}}}
	out := RenderTOMLForScope(cfg, RenderScopeUser)
	if !strings.Contains(out, "hide_model_prefix = true") {
		t.Fatalf("not rendered:\n%s", out)
	}
	path := t.TempDir() + "/config.toml"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := LoadForEditReadOnlyStrict(path)
	if err != nil || len(back.Providers) != 1 || !back.Providers[0].HideModelPrefix {
		t.Fatalf("reload err=%v providers=%+v", err, back.Providers)
	}
}

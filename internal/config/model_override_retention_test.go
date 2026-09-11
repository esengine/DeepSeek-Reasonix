package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadSingleProvider(t *testing.T, providerBody string) *ProviderEntry {
	t.Helper()
	dir := t.TempDir()
	body := "default_model = \"relay\"\n\n[[providers]]\nname = \"relay\"\nkind = \"openai\"\nmodels = [\"a\", \"b\"]\ndefault = \"a\"\n" + providerBody + "\n"
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	c, err := LoadForRootReadOnly(dir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	e, ok := c.ResolveModel("relay/a")
	if !ok {
		t.Fatal("ResolveModel did not resolve relay/a")
	}
	return e
}

// normalizedModelOverrides drops overrides it judges empty. Its emptiness test
// omitted MaxOutputTokens, so an override carrying only max_output_tokens was
// discarded at load and the documented key silently did nothing.
func TestMaxOutputTokensOnlyOverrideSurvivesLoad(t *testing.T) {
	e := loadSingleProvider(t, `model_overrides = { "a" = { max_output_tokens = 32768 } }`)
	if e.MaxOutputTokens != 32768 {
		t.Fatalf("MaxOutputTokens = %d, want 32768 from the model override", e.MaxOutputTokens)
	}
}

// A negative value is the documented way to force-omit optional wire limits, so
// it must survive the same pass rather than reading as an unset field.
func TestNegativeMaxOutputTokensOnlyOverrideSurvivesLoad(t *testing.T) {
	e := loadSingleProvider(t, `model_overrides = { "a" = { max_output_tokens = -1 } }`)
	if e.MaxOutputTokens != -1 {
		t.Fatalf("MaxOutputTokens = %d, want -1 from the model override", e.MaxOutputTokens)
	}
}

// An override with nothing set is still dropped, and an override aimed at a
// different model must not leak onto the resolved one.
func TestEmptyAndForeignModelOverridesStayInert(t *testing.T) {
	e := loadSingleProvider(t, `model_overrides = { "a" = { }, "b" = { max_output_tokens = 4096 } }`)
	if len(e.ModelOverrides) != 1 {
		t.Fatalf("ModelOverrides = %+v, want only the non-empty entry for b", e.ModelOverrides)
	}
	if e.MaxOutputTokens == 4096 {
		t.Fatal("an override for model b was applied to model a")
	}
}

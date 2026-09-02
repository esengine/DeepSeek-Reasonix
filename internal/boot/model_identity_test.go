package boot

import (
	"strings"
	"testing"

	"reasonix/internal/config"
)

func identityEntry(vision bool) *config.ProviderEntry {
	e := &config.ProviderEntry{
		Name:          "deepseek",
		Kind:          "openai",
		BaseURL:       "https://api.deepseek.com",
		Model:         "deepseek-v4-flash",
		ContextWindow: 131072,
		Vision:        vision,
	}
	if vision {
		// The official vision SKU is detected by name on the DeepSeek base URL
		// regardless of the provider-wide flag.
		e.Model = "deepseek-v4-flash-vision-exp"
	}
	return e
}

// A vision-capable SKU must learn its own identity and its direct-input
// behavior from the prompt: models cannot introspect this, which is why the
// vision SKU answered "I am a text-only model" and routed screenshots through
// MCP bridges / OCR instead of reading them (#9288).
func TestModelIdentityTellsVisionModelsToReadImagesDirectly(t *testing.T) {
	got := appendModelIdentity("BASE", identityEntry(true))

	if !strings.Contains(got, "`deepseek/deepseek-v4-flash-vision-exp`") {
		t.Fatalf("identity section must name the provider-qualified ref:\n%s", got)
	}
	if !strings.Contains(got, "131072-token context window") {
		t.Fatalf("identity section must state the context window:\n%s", got)
	}
	for _, phrase := range []string{
		"You accept images as direct input",
		"read the image itself first",
		"opt-in fallbacks",
		"never state that you cannot see images",
	} {
		if !strings.Contains(got, phrase) {
			t.Fatalf("vision identity missing %q:\n%s", phrase, got)
		}
	}
	if !strings.HasPrefix(got, "BASE\n\n## Model") {
		t.Fatalf("identity must append after the base prompt, got prefix %q", got[:min(len(got), 40)])
	}
}

// A text-only model gets the honest inverse: no claimed vision, an explicit
// ask-for-description path, and no silent pretending.
func TestModelIdentityTellsTextModelsTheyCannotSeeImages(t *testing.T) {
	got := appendModelIdentity("BASE", identityEntry(false))

	if !strings.Contains(got, "You are a text-only model") || !strings.Contains(got, "cannot see image content") {
		t.Fatalf("text identity must state the modality limit:\n%s", got)
	}
	if strings.Contains(got, "You accept images as direct input") {
		t.Fatalf("text identity must not claim vision:\n%s", got)
	}
}

// The section sits inside the provider-cached prefix: it must be a pure
// function of the resolved entry, byte-identical across assemblies.
func TestModelIdentityIsByteStableForSameEntry(t *testing.T) {
	e := identityEntry(true)
	if appendModelIdentity("BASE", e) != appendModelIdentity("BASE", e) {
		t.Fatal("identity section must be deterministic for the same entry")
	}
}

func TestModelIdentityOmitsEmptyModels(t *testing.T) {
	if got := appendModelIdentity("BASE", &config.ProviderEntry{Name: "x"}); got != "BASE" {
		t.Fatalf("entry without a model must not inject identity, got %q", got)
	}
	if got := appendModelIdentity("BASE", nil); got != "BASE" {
		t.Fatalf("nil entry must not inject identity, got %q", got)
	}
}

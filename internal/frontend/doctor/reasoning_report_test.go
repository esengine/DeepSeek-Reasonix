package doctor

import (
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

// A relay's protocol is declared or it is nothing, and an endpoint that refuses
// a request for reasoning this host never sent looks identical either way in a
// bundle that omits the declaration. Report the knobs so the answer is in the
// artifact rather than in a round of questions.
func TestReportCarriesTheKnobsThatDecideThinking(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{
		Name:              "relay",
		Kind:              "openai",
		BaseURL:           "https://relay.example.com/v1",
		Model:             "deepseek-v3.2",
		APIKeyEnv:         "RELAY_API_KEY",
		ReasoningProtocol: "deepseek",
		Effort:            "high",
		Thinking:          "enabled",
	}, {
		Name:      "undeclared",
		Kind:      "openai",
		BaseURL:   "https://relay.example.com/v1",
		Model:     "deepseek-v3.2",
		APIKeyEnv: "RELAY_API_KEY",
	}}

	report := Collect(Options{Version: "test", Config: cfg})
	if len(report.Providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(report.Providers))
	}
	if got := report.Providers[0]; got.ReasoningProtocol != "deepseek" || got.Effort != "high" || got.Thinking != "enabled" {
		t.Errorf("declared provider = %+v, want its three thinking knobs", got)
	}
	if got := report.Providers[1]; got.ReasoningProtocol != "" {
		t.Errorf("undeclared provider reports protocol %q, want none", got.ReasoningProtocol)
	}

	text := RenderText(report)
	if !strings.Contains(text, "think:deepseek/high/enabled") {
		t.Errorf("rendered report hides the declared knobs:\n%s", text)
	}
	if !strings.Contains(text, "think:auto") {
		t.Errorf("rendered report hides that a provider declares nothing:\n%s", text)
	}
}

package cli

import (
	"testing"

	"reasonix/internal/contract/config"
)

// An ACP client lists models under their description, so a renamed source is
// described by its label while the id it sends back stays the config ref.
func TestACPModelOptionsDescribeARenamedSourceByItsLabel(t *testing.T) {
	cfg := &config.Config{Providers: []config.ProviderEntry{
		{Name: "relay", DisplayName: "Work gateway", Kind: "openai", BaseURL: "http://127.0.0.1:1/v1", Models: []string{"m"}},
		{Name: "plain", Kind: "openai", BaseURL: "http://127.0.0.1:2/v1", Models: []string{"n"}},
	}}
	options, models := acpModelOptions(cfg)
	if len(options) != 2 || len(models) != 2 {
		t.Fatalf("options=%d models=%d, want 2 each", len(options), len(models))
	}
	if options[0].Value != "relay/m" || options[0].Description != "Work gateway" || models[0].ModelID != "relay/m" || models[0].Description != "Work gateway" {
		t.Fatalf("renamed source = %+v / %+v", options[0], models[0])
	}
	if options[1].Description != "plain" || models[1].Description != "plain" {
		t.Fatalf("unlabelled source = %+v / %+v", options[1], models[1])
	}
}

package acp

import (
	"context"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestEnrichStateWithExtensionModels(t *testing.T) {
	base := SessionConfigState{
		Model: "openai/x",
		Models: &SessionModelState{
			CurrentModelID:  "openai/x",
			AvailableModels: []ModelInfo{{ModelID: "openai/x", Name: "openai/x"}},
		},
		ConfigOptions: []SessionConfigOption{{
			ID: "model", Name: "Model", Category: "model", Type: "select",
			CurrentValue: "openai/x",
			Options:      []SessionConfigSelectOption{{Value: "openai/x", Name: "openai/x"}},
		}},
	}
	catalog := []provider.Descriptor{
		{Ref: "plugin/demo/fake/echo", DisplayName: "Demo Echo"},
		{Ref: "openai/x"},              // config-owned: skipped (not plugin-namespaced)
		{Ref: "plugin/demo/fake/echo"}, // duplicate: deduped
		{Ref: "plugin//fake/echo"},     // malformed owner: skipped
	}

	out := enrichStateWithExtensionModels(base, catalog)

	var infos []string
	for _, m := range out.Models.AvailableModels {
		infos = append(infos, m.ModelID)
	}
	if len(infos) != 2 || infos[0] != "openai/x" || infos[1] != "plugin/demo/fake/echo" {
		t.Fatalf("AvailableModels = %v", infos)
	}
	found := false
	for _, m := range out.Models.AvailableModels {
		if m.ModelID == "plugin/demo/fake/echo" && (m.Name != "Demo Echo" || m.Description != "Demo Echo") {
			t.Fatalf("display name not propagated: %+v", m)
		}
		if m.ModelID == "plugin/demo/fake/echo" {
			found = true
		}
	}
	if !found {
		t.Fatal("plugin model missing from legacy model list")
	}
	opts := out.ConfigOptions[0].Options
	if len(opts) != 2 || opts[1].Value != "plugin/demo/fake/echo" || opts[1].Name != "Demo Echo" {
		t.Fatalf("model select options = %+v", opts)
	}

	// Idempotent: enriching twice must not duplicate.
	twice := enrichStateWithExtensionModels(out, catalog)
	if len(twice.Models.AvailableModels) != 2 || len(twice.ConfigOptions[0].Options) != 2 {
		t.Fatalf("enrichment is not idempotent: %+v", twice.Models.AvailableModels)
	}

	// Nil catalog / nil Models are no-ops.
	empty := enrichStateWithExtensionModels(SessionConfigState{Model: "openai/x"}, nil)
	if empty.Models != nil {
		t.Fatal("nil catalog mutated the state")
	}
}

// A session whose current model is a builtin must still SEE installed
// extension models on a config-state read (the reviewer P1 case).
func TestConfigStateForSessionIncludesExtensionModels(t *testing.T) {
	ctrl := control.New(control.Options{
		Sink: event.Discard,
		ProviderResolver: &provider.StaticResolver{
			Descriptors: []provider.Descriptor{
				{Ref: "openai/x", DisplayName: "openai"},
				{Ref: "plugin/demo/fake/echo", DisplayName: "Demo Echo"},
			},
		},
	})
	defer ctrl.Close()

	svc := &service{}
	sess := &acpSession{id: "sess-1", cwd: "/tmp", ctrl: ctrl}
	state, err := svc.configStateForSession(context.Background(), sess)
	if err != nil {
		t.Fatalf("configStateForSession: %v", err)
	}
	if state.Models == nil {
		t.Fatal("state.Models is nil")
	}
	var hasPlugin bool
	for _, m := range state.Models.AvailableModels {
		if m.ModelID == "plugin/demo/fake/echo" {
			hasPlugin = true
		}
	}
	if !hasPlugin {
		t.Fatalf("extension model not discoverable on config-state read: %+v", state.Models.AvailableModels)
	}
}

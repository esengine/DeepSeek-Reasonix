package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// extensionCatalogCtrl stubs only what the model discovery/switch paths read
// from a tab controller; everything else falls through the embedded nil
// SessionAPI and must not be called.
type extensionCatalogCtrl struct {
	stubSessionAPI
	catalog []provider.Descriptor
}

func (c *extensionCatalogCtrl) ProviderCatalog() []provider.Descriptor { return c.catalog }

// TestModelsForTabIncludesExtensionProviders: the desktop model switcher
// merges the tab controller's extension catalog into the config-backed list —
// plugin refs arrive fully namespaced, duplicates collapse, and the active
// plugin ref is marked current.
func TestModelsForTabIncludesExtensionProviders(t *testing.T) {
	reloadRuntimeFixture(t) // one configured provider "old" with model old-model

	app := NewApp()
	tab := &WorkspaceTab{
		ID:    "tab_a",
		Scope: "global",
		Ready: true,
		model: "plugin/demo/fake/x",
		Ctrl: &extensionCatalogCtrl{catalog: []provider.Descriptor{
			{Ref: "plugin/demo/fake/x", Model: "x"},
			{Ref: "plugin/demo/fake/y", Model: "y"},
			{Ref: "old/old-model"}, // claim-replaced duplicate must not repeat
			// ProviderCatalog is merged, so it can also contain unnamespaced
			// config-backed descriptors. They are not extension models and must
			// never leak into a synthetic PLUGIN group in the desktop picker.
			{Ref: "deepseek-responses", DisplayName: "deepseek-responses"},
			{Ref: "siliconflow-cn", DisplayName: "siliconflow-cn"},
		}},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	models := app.ModelsForTab(tab.ID)
	var pluginX, pluginY, oldCount *ModelInfo
	oldSeen := 0
	for i := range models {
		switch models[i].Ref {
		case "plugin/demo/fake/x":
			pluginX = &models[i]
		case "plugin/demo/fake/y":
			pluginY = &models[i]
		case "old/old-model":
			oldSeen++
			oldCount = &models[i]
		}
	}
	if pluginX == nil || pluginY == nil {
		t.Fatalf("ModelsForTab = %+v, want both plugin refs", models)
	}
	if pluginX.Provider != "plugin/demo" || pluginX.Model != "fake/x" {
		t.Fatalf("plugin ref display = %+v, want provider plugin/demo model fake/x", pluginX)
	}
	if !pluginX.Current {
		t.Fatalf("active plugin ref not marked current: %+v", pluginX)
	}
	if oldSeen != 1 || oldCount == nil {
		t.Fatalf("config ref repeated %d times, want exactly one", oldSeen)
	}
	for _, info := range models {
		if info.Ref == "deepseek-responses" || info.Ref == "siliconflow-cn" || info.Provider == "plugin" {
			t.Fatalf("unnamespaced provider descriptor leaked into plugin models: %+v", info)
		}
	}

	// A tab whose controller has no extension catalog (nil → no sidecar
	// declared providers) enumerates config only, as before.
	tab.Ctrl = &extensionCatalogCtrl{}
	for _, info := range app.ModelsForTab(tab.ID) {
		if strings.HasPrefix(info.Ref, "plugin/") {
			t.Fatalf("empty catalog produced plugin ref %+v", info)
		}
	}
}

// TestSetModelForTabPluginRefReachesBoot: with a controller whose merged
// catalog declares the plugin ref, SetModelForTab's config gate accepts the
// ref and hands it to boot — which is the resolver of record (here it fails
// only because no sidecar is installed in this test's REASONIX_HOME). A
// non-plugin unknown ref must still fail at the desktop gate, before boot.
func TestSetModelForTabPluginRefReachesBoot(t *testing.T) {
	dir := reloadRuntimeFixture(t)

	oldExec := agent.New(nil, nil, agent.NewSession("old system prompt"), agent.Options{}, event.Discard)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	oldCtrl := control.New(control.Options{
		Executor:   oldExec,
		SessionDir: dir,
		Label:      "old",
		Sink:       event.Discard,
		// The merged catalog this controller would expose with the demo
		// sidecar live: plugin/demo/fake/x is a valid switch target.
		ProviderResolver: &provider.StaticResolver{Descriptors: []provider.Descriptor{
			{Ref: "plugin/demo/fake/x", Model: "x"},
		}},
	})
	reloadRuntimeTab(t, app, dir, oldCtrl)

	err := app.SetModelForTab("tab_a", "plugin/demo/fake/x")
	if err == nil {
		t.Fatal("SetModelForTab succeeded without an installed sidecar; expected boot to gate the plugin ref")
	}
	if !errors.Is(err, boot.ErrUnknownModel) {
		t.Fatalf("SetModelForTab error = %v, want boot.ErrUnknownModel (the desktop gate must pass plugin refs through to boot)", err)
	}

	// The desktop config gate still rejects unknown NON-plugin refs itself.
	err = app.SetModelForTab("tab_a", "ghost/never-existed")
	if err == nil || !strings.Contains(err.Error(), `unknown model "ghost/never-existed"`) {
		t.Fatalf("SetModelForTab non-plugin error = %v, want the desktop unknown-model gate", err)
	}
	if errors.Is(err, boot.ErrUnknownModel) {
		t.Fatalf("non-plugin unknown ref reached boot: %v", err)
	}
}

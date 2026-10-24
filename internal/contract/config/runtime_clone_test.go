package config

import (
	"reflect"
	"testing"
	"time"
)

func TestCloneResolvedRetainsAuthorityAndSeparatesCollections(t *testing.T) {
	c := Default()
	vision := false
	c.Providers = []ProviderEntry{{Name: "fixture", resolvedAPIKey: "fixture-key", visionOverride: &vision, Headers: map[string]string{"header": "original"}, Models: []string{"model"}, roots: RootsForHome("/fixture-home")}}
	c.Plugins = []PluginEntry{{Name: "fixture", Source: MCPConfigSource("user"), Env: map[string]string{"KEY": "original"}, expansionEnv: map[string]string{"EXPAND": "original"}}}
	c.expansionEnv = map[string]string{"GLOBAL": "original"}
	c.Bot = map[string]any{"integer": int64(7), "date": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	c.projectScope.admitted = []ProjectProgram{{Files: []string{"authorized-file"}}}
	cloned, err := c.CloneResolved()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, cloned) {
		t.Fatal("resolved runtime metadata or value types changed")
	}
	cloned.Providers[0].Headers["header"] = "changed"
	cloned.Providers[0].Models[0] = "changed"
	*cloned.Providers[0].visionOverride = true
	cloned.Plugins[0].Env["KEY"] = "changed"
	cloned.Plugins[0].expansionEnv["EXPAND"] = "changed"
	cloned.expansionEnv["GLOBAL"] = "changed"
	cloned.projectScope.admitted[0].Files[0] = "changed"
	if c.Providers[0].Headers["header"] != "original" || c.Providers[0].Models[0] != "model" || vision || c.Plugins[0].Env["KEY"] != "original" || c.Plugins[0].expansionEnv["EXPAND"] != "original" || c.expansionEnv["GLOBAL"] != "original" || c.projectScope.admitted[0].Files[0] != "authorized-file" {
		t.Fatal("clone mutated its parent's state")
	}
}

func TestCloneResolvedRefusesRuntimeOwners(t *testing.T) {
	c := Default()
	c.Bot = map[string]any{"callback": func() {}}
	if _, err := c.CloneResolved(); err == nil {
		t.Fatal("runtime owner was silently copied")
	}
}

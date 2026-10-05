package boot

import (
	"encoding/json"
	"testing"
)

// A relay that re-serializes a schema turns an absent "required" into null,
// which upstream rejects; every tool the provider is shown must carry an array.
func TestEffectEveryProviderToolDeclaresRequiredArray(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	approveWorkspace(t, dir)

	req, _ := captureTokenProfileSurface(t, "")
	if len(req.Tools) == 0 {
		t.Fatal("provider request carries no tools")
	}
	for _, s := range req.Tools {
		var root map[string]json.RawMessage
		if err := json.Unmarshal(s.Parameters, &root); err != nil {
			t.Fatalf("%s: parameters are not a JSON object: %v", s.Name, err)
		}
		var required []string
		if raw, ok := root["required"]; !ok || json.Unmarshal(raw, &required) != nil || required == nil {
			t.Fatalf("%s: required = %s, want a JSON array", s.Name, root["required"])
		}
	}
}

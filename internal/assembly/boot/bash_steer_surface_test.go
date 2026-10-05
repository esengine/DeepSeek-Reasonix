package boot

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/tools/builtin"
)

// A steered name missing from the provider schema cannot be called, so the whole
// recommendation reads as unreliable and the model falls back to shell reads.
// Asserted against the real boot surface, not the declared allowlist, so a tool
// dropped during registration fails here too.
func TestBashSteerNamesReachTheProviderSurface(t *testing.T) {
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
	visible := toolSchemaNames(req.Tools)

	steered := builtin.BashSteerTools()
	if len(steered) == 0 {
		t.Fatal("bash steer recommends no tools; the shell recipes in the description go uncountered")
	}
	for _, name := range steered {
		if !slices.Contains(visible, name) {
			t.Fatalf("bash description recommends %q but it is not provider-visible\nvisible=%v", name, visible)
		}
	}

	var bashDesc string
	for _, s := range req.Tools {
		if s.Name == "bash" {
			bashDesc = s.Description
			break
		}
	}
	if bashDesc == "" {
		t.Fatalf("bash missing from provider surface; visible=%v", visible)
	}
	for _, name := range steered {
		if !strings.Contains(bashDesc, name) {
			t.Fatalf("bash description does not mention steered tool %q\ndescription=%q", name, bashDesc)
		}
	}
}

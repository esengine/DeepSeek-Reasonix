package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

var _ tool.SandboxCapabilityTool = bash{}

func TestBashSchemaAdvertisesBoundedSandboxCapabilities(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Type                 string                     `json:"type"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal((bash{}).Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	capability, ok := schema.Properties["sandbox_capabilities"]
	if !ok || capability.Type != "object" || capability.AdditionalProperties == nil || *capability.AdditionalProperties {
		t.Fatalf("sandbox_capabilities schema = %#v", capability)
	}
	for _, name := range []string{"network", "read_paths", "write_paths", "argv_prefix", "justification"} {
		if _, ok := capability.Properties[name]; !ok {
			t.Fatalf("sandbox_capabilities schema missing %q", name)
		}
	}
	for name, want := range map[string]int{"read_paths": 4, "write_paths": 4, "argv_prefix": 8} {
		var property struct {
			MaxItems int `json:"maxItems"`
		}
		if err := json.Unmarshal(capability.Properties[name], &property); err != nil {
			t.Fatal(err)
		}
		if property.MaxItems != want {
			t.Fatalf("%s.maxItems = %d, want %d", name, property.MaxItems, want)
		}
	}
}

func TestBashCapabilityOmissionPreservesOutput(t *testing.T) {
	sh := sandbox.ResolveShell("bash", "", nil)
	out, err := (bash{shell: sh, workDir: t.TempDir()}).Execute(context.Background(), argsJSON(t, map[string]any{
		"command": "printf unchanged",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "unchanged" {
		t.Fatalf("output = %q, want byte-identical legacy output", out)
	}
}

func TestBashInvalidCapabilitySoftDeniesAndRunsBaseCommand(t *testing.T) {
	sh := sandbox.ResolveShell("bash", "", nil)
	out, err := (bash{shell: sh, workDir: t.TempDir(), sb: sandbox.Spec{Mode: "off"}}).Execute(context.Background(), argsJSON(t, map[string]any{
		"command": "printf base-ran",
		"sandbox_capabilities": map[string]any{
			"network": true,
			"unknown": true,
		},
	}))
	if err != nil {
		t.Fatalf("base command failed after soft denial: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "base-ran") || !strings.Contains(out, "sandbox capability request was not applied") {
		t.Fatalf("output = %q, want command output plus soft-denial diagnostic", out)
	}
}

func TestPreparedBashInvocationReviewIsDefensiveAndSingleUse(t *testing.T) {
	workspace := t.TempDir()
	invocation, err := (bash{workDir: workspace, sb: sandbox.Spec{Mode: "off"}}).PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": "printf once",
		"sandbox_capabilities": map[string]any{
			"argv_prefix": []string{"printf"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	review := invocation.Review()
	review.ArgvPrefix[0] = "mutated"
	if got := invocation.Review().ArgvPrefix[0]; got != "printf" {
		t.Fatalf("review mutation leaked into invocation: %q", got)
	}
	if _, err := invocation.Execute(context.Background(), sandbox.BaseOnly); err != nil {
		t.Fatal(err)
	}
	if _, err := invocation.Execute(context.Background(), sandbox.BaseOnly); err == nil {
		t.Fatal("prepared invocation executed twice")
	}
}

func TestPreparedBashInvocationAppliesAuthorizedAtomicWriteDelta(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("OS sandbox unavailable")
	}
	workspace := t.TempDir()
	external := t.TempDir()
	output := filepath.Join(external, "created.txt")
	b := bash{
		workDir: workspace,
		sb: sandbox.Spec{
			Mode:          "enforce",
			WriteRoots:    []string{workspace},
			Network:       true,
			MinimalWrites: true,
		},
	}
	args := argsJSON(t, map[string]any{
		"command": "printf granted > " + shellSingleQuote(output),
		"sandbox_capabilities": map[string]any{
			"write_paths": []any{map[string]string{
				"identity": string(sandbox.CanonicalAbsolute),
				"path":     external,
			}},
		},
	})
	invocation, err := b.PrepareSandboxInvocation(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if review := invocation.Review(); review.State != sandbox.CapabilityReady {
		t.Skipf("path capabilities unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	out, err := invocation.Execute(context.Background(), sandbox.AuthorizedDelta)
	if err != nil {
		t.Fatalf("authorized command failed: %v (out=%q)", err, out)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "granted" {
		t.Fatalf("output file = %q", got)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

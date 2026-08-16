package boot

import (
	"testing"

	"reasonix/internal/config"
)

func TestLSPSpecsExplicitCommandDoesNotInheritDefaultFallbacks(t *testing.T) {
	spec := LSPSpecs(config.LSPConfig{Servers: map[string]config.LSPServer{
		"kotlin": {
			Command: "company-kotlin-server",
			Args:    []string{"--company-mode"},
		},
	}})["kotlin"]

	if spec.Command != "company-kotlin-server" {
		t.Fatalf("Command = %q, want explicit user command", spec.Command)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "--company-mode" {
		t.Fatalf("Args = %v, want explicit user arguments", spec.Args)
	}
	if len(spec.Fallbacks) != 0 {
		t.Fatalf("Fallbacks = %v, want none for an explicit user command", spec.Fallbacks)
	}
}

func TestLSPSpecsNonCommandOverrideKeepsDefaultFallbacks(t *testing.T) {
	spec := LSPSpecs(config.LSPConfig{Servers: map[string]config.LSPServer{
		"kotlin": {InstallHint: "custom install hint"},
	}})["kotlin"]

	if len(spec.Fallbacks) != 1 || spec.Fallbacks[0] != "intellij-server" {
		t.Fatalf("Fallbacks = %v, want the built-in intellij-server fallback", spec.Fallbacks)
	}
}

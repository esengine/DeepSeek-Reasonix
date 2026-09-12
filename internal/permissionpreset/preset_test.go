package permissionpreset

import "testing"

func TestLegacyMigrationIsConservative(t *testing.T) {
	tests := map[string]Preset{
		"ask": ReadOnly, "auto": WorkspaceWrite, "yolo": WorkspaceWrite,
		"read-only": ReadOnly, "workspace-write": WorkspaceWrite,
		"danger-full-access": DangerFullAccess,
		"dontAsk":            ReadOnly, "unknown": ReadOnly,
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
	if got := NormalizeDefault(""); got != WorkspaceWrite {
		t.Fatalf("NormalizeDefault(empty) = %q", got)
	}
}

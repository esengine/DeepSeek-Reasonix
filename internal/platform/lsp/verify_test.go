package lsp

import (
	"errors"
	"testing"
)

// A server whose approval no longer holds is not started, and the caller gets
// the verifier's own error rather than a missing-binary hint.
func TestSpawnRefusesAServerThatFailsVerification(t *testing.T) {
	refused := errors.New("changed since approved")
	m := NewManager(t.TempDir(), map[string]ServerSpec{"x": {Command: "definitely-not-installed", Verify: func() error { return refused }}})
	if _, err := m.spawn("x", m.specs["x"]); !errors.Is(err, refused) {
		t.Fatalf("spawn = %v, want the verifier's refusal", err)
	}
}

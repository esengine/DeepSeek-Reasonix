package serve

import (
	"context"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// Choosing the depth the provider already defaults to changes nothing that is
// sent, but it is still the person's answer: the status the picker reads has to
// say it, and say "auto" again when they go back.
func TestTheStatusEffortFollowsTheChoiceEvenWhenTheRequestDoesNotChange(t *testing.T) {
	writeServeDefaultEffortSelectionConfig(t)
	bc := NewBroadcaster()
	identity := serveProviderIdentity(t, "alternate/shared-chat")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.ResolveModel("alternate/shared-chat")
	if !ok {
		t.Fatal("resolve alternate/shared-chat")
	}
	ctrl := control.New(control.Options{
		Sink:                bc,
		ModelRef:            "alternate/shared-chat",
		ModelEntry:          entry,
		Effort:              identity.Effort,
		ProviderFingerprint: identity.Fingerprint,
		SessionDir:          testenv.TempDir(t),
	})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	server.buildController = func(context.Context, string) (*control.Controller, error) {
		t.Fatal("a depth that sends the same request must not rebuild")
		return nil, nil
	}

	shown := func() string {
		face, ok := ctrl.ModelFace()
		if !ok {
			t.Fatal("controller has no model face")
		}
		return face.Effort
	}
	if got := shown(); got != "" {
		t.Fatalf("effort shown before any choice = %q, want auto (empty)", got)
	}
	if err := server.switchEffort(context.Background(), "high"); err != nil {
		t.Fatal(err)
	}
	if got := shown(); got != "high" {
		t.Fatalf("effort shown after choosing high = %q, want high", got)
	}
	if err := server.switchEffort(context.Background(), "auto"); err != nil {
		t.Fatal(err)
	}
	if got := shown(); got != "" {
		t.Fatalf("effort shown after choosing auto = %q, want auto (empty)", got)
	}
}

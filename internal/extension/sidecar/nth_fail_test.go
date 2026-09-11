package sidecar

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/pluginpkg"
)

// TestNthRequiredFailureClosesEarlierSuccesses: Nth required fail → nil manager.
func TestNthRequiredFailureClosesEarlierSuccesses(t *testing.T) {
	home := t.TempDir()
	installFakePlugin(t, home, "p1", func(rt *pluginpkg.RuntimeSpec) { rt.Required = true })
	installFakePlugin(t, home, "p2", func(rt *pluginpkg.RuntimeSpec) { rt.Required = true })
	installFakePlugin(t, home, "p3-bad", func(rt *pluginpkg.RuntimeSpec) {
		rt.Required = true
		rt.Env[fakeEnvInitResult] = `{"protocolVersion":"1","name":"bad","version":"1","stateSchemaVersion":0}`
	})
	mgr, _, err := StartPackages(context.Background(), home, testSessionContext(), nil)
	if err == nil {
		t.Fatal("expected required failure")
	}
	var req *RequiredStartError
	if !errors.As(err, &req) {
		t.Fatalf("err = %T %v", err, err)
	}
	if mgr != nil {
		t.Fatal("manager must be nil after required failure (all closed)")
	}
}

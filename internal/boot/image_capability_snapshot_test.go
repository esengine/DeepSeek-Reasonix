package boot

import (
	"context"
	"reasonix/internal/event"
	"testing"
)

func TestBuildImageCapabilityFrozenUntilRebuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	base := `default_model = "relay/x"
[agent]
system_prompt = "stable prefix"
[[providers]]
name = "relay"
kind = "openai"
base_url = "http://localhost:1"
model = "x"
`
	writeFile(t, dir, "reasonix.toml", base)
	old, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if old.ImageInputEnabled() || old.ImageCapabilityChanged() {
		t.Fatal("fresh unknown snapshot should be disabled and current")
	}
	writeFile(t, dir, "reasonix.toml", base+"[providers.model_overrides.x]\nvision = true\n")
	if old.ImageInputEnabled() || !old.ImageCapabilityChanged() {
		t.Fatal("saved setting must invalidate, not mutate old runtime")
	}
	next, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if !next.ImageInputEnabled() || next.ImageCapabilityChanged() {
		t.Fatal("rebuilt snapshot should be enabled and current")
	}
	writeFile(t, dir, "reasonix.toml", base+"[providers.model_overrides.x]\nvision = false\n")
	if !next.ImageInputEnabled() || !next.ImageCapabilityChanged() {
		t.Fatal("running snapshot switched before rebuild")
	}
}

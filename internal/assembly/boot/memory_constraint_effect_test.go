package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/memory"
)

func TestEffectConstraintOfATouchedGalaxyReachesTheProviderWithoutMovingThePrefix(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &effectRecordingProvider{}
	provider.Register("boot-effect-memory-constraint", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-effect-memory-constraint"
model = "x"
`)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	run := func(text string) {
		t.Helper()
		if err := ctrl.Run(context.Background(), text); err != nil {
			t.Fatalf("Run(%q): %v", text, err)
		}
	}
	const query = "how often may the studio line cut a release per day"
	run("hello")
	run(query)
	before := agentRequests(rec.requests())
	if got := lastUserTurn(t, before); strings.Contains(got, "Never cut a release on a Friday") {
		t.Fatalf("the constraint is not saved yet, so the probe proves nothing:\n%s", got)
	}

	if _, err := ctrl.SaveMemory(memory.Memory{
		Name: "no-friday-release", Description: "never cut a release on a friday",
		Type: memory.TypeConstraint, Galaxy: "release process", Body: "Never cut a release on a Friday.",
	}); err != nil {
		t.Fatalf("SaveMemory constraint: %v", err)
	}
	if _, err := ctrl.SaveMemory(memory.Memory{
		Name: "release-cadence", Description: "versions go out at most once per day",
		Type: memory.TypeProject, Galaxy: "release process", Body: "At most one release per calendar day for the studio line.",
	}); err != nil {
		t.Fatalf("SaveMemory fact: %v", err)
	}
	run("hello again")
	run(query)
	after := agentRequests(rec.requests())
	got := lastUserTurn(t, after)
	if !strings.Contains(got, "Never cut a release on a Friday") {
		t.Fatalf("the constraint of the touched galaxy never reached the provider:\n%s", got)
	}
	if !strings.Contains(got, "At most one release per calendar day") {
		t.Fatalf("the matching fact never reached the provider:\n%s", got)
	}
	first, _ := json.Marshal(after[0].Tools)
	last, _ := json.Marshal(after[len(after)-1].Tools)
	if !bytes.Equal(first, last) || systemOf(after[0]) != systemOf(after[len(after)-1]) {
		t.Fatal("saving memory moved the stable prefix")
	}
}

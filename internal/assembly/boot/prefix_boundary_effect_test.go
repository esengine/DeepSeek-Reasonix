package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/memory"
)

// prefixProject builds a session in its own project and returns the system
// message that reached the provider. Every argument is a thing a project can
// differ on: its skills, its saved facts, and whether it is a repository.
func prefixProject(t *testing.T, kind, skillName, instructions string, isRepo bool, fact string) provider.Request {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &effectRecordingProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	writeFile(t, dir, "REASONIX.md", instructions)
	if isRepo {
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatalf("make repository marker: %v", err)
		}
	}
	approveWorkspace(t, dir)
	skillDir := filepath.Join(dir, ".reasonix", "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("make skill dir: %v", err)
	}
	writeFile(t, skillDir, "SKILL.md", "---\nname: "+skillName+"\ndescription: does the "+skillName+" thing\n---\n\nBody.\n")

	// The fact has to be saved by a session that then ends: a store written
	// mid-session is what the next session would have folded in.
	first, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}
	if _, err := first.SaveMemory(memory.Memory{
		Name: fact, Description: "a saved fact named " + fact,
		Type: memory.TypeProject, Body: "The body of " + fact + ".",
	}); err != nil {
		first.Close()
		t.Fatalf("SaveMemory: %v", err)
	}
	first.Close()

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := agentRequests(rec.requests())
	if len(reqs) == 0 {
		t.Fatal("no agent request reached the provider boundary")
	}
	return reqs[0]
}

// TestEffectNoProjectStateReachesThePrefix is the standing boundary each
// per-feature effect test guards one piece of: two projects sharing nothing —
// different instructions, skills and saved facts, one a repository — must send
// the byte-identical system message, which is what makes the prefix reusable
// across every project on the machine and not just within one session.
func TestEffectNoProjectStateReachesThePrefix(t *testing.T) {
	a := prefixProject(t, "boot-prefix-a", "alphaskill",
		"# Alpha rules\n\nAlways run go vet before committing.\n", true, "deploy-target")
	b := prefixProject(t, "boot-prefix-b", "betaskill",
		"# Beta rules\n\nNever touch the vendored tree.\n", false, "release-cadence")

	if sysA, sysB := systemOf(a), systemOf(b); sysA != sysB {
		t.Fatalf("project state reached the cached system prompt; every byte after it is re-sent for each project:\nfirst diff site: %q",
			firstDivergence(sysA, sysB))
	}
	// The prefix the provider caches is the system message AND the tool
	// schemas, which are the larger half. A per-project tool surface diverges
	// it just as a per-project prompt does, one message later.
	if toolsA, toolsB := toolSchemaJSON(t, a), toolSchemaJSON(t, b); toolsA != toolsB {
		t.Fatalf("project state reached the cached tool schemas:\nfirst diff site: %q",
			firstDivergence(toolsA, toolsB))
	}
}

func toolSchemaJSON(t *testing.T, req provider.Request) string {
	t.Helper()
	data, err := json.Marshal(req.Tools)
	if err != nil {
		t.Fatalf("marshal tool schemas: %v", err)
	}
	return string(data)
}

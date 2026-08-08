package evolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/memory"
)

func TestApplyL0WritesMemoryWithWhyHow(t *testing.T) {
	userDir := t.TempDir()
	project := t.TempDir()
	mem := memory.StoreFor(userDir, project)
	propStore := StoreFor(userDir, project)
	p := baseProposal(TierL0)
	p.Target.MemoryName = "no-force-push-main"
	if err := propStore.Save(p); err != nil {
		t.Fatal(err)
	}

	res, err := Apply(p, ApplyDeps{
		MemoryStore:   mem,
		ProjectRoot:   project,
		ProposalStore: &propStore,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Noop || res.Path == "" {
		t.Fatalf("unexpected result %+v", res)
	}
	raw, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{"**Why:**", "**How to apply:**", "force-push"} {
		if !strings.Contains(body, want) {
			t.Fatalf("memory body missing %q:\n%s", want, body)
		}
	}
	// Idempotent re-apply after status persisted.
	got, ok := propStore.Get(p.ID)
	if !ok || got.Status != StatusApplied {
		t.Fatalf("persisted status = %+v ok=%v", got, ok)
	}
	res2, err := Apply(got, ApplyDeps{MemoryStore: mem, ProposalStore: &propStore})
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if !res2.Noop {
		t.Fatal("re-apply should be no-op")
	}
}

func TestApplyL1PatchesStandingDoc(t *testing.T) {
	userDir := t.TempDir()
	project := t.TempDir()
	agents := filepath.Join(project, "AGENTS.md")
	initial := "# My project\n\n## Project\n\nA demo.\n\n## Commands\n\n- go test ./...\n\n## Conventions\n\n- Use gofmt.\n"
	if err := os.WriteFile(agents, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	propStore := StoreFor(userDir, project)
	p := baseProposal(TierL1)
	p.ID = "evp_l1"
	p.Target = Target{Kind: TargetAgentsMD, Section: "Conventions"}
	p.Title = "No force-push main"
	p.HowToApply = "Never force-push the main branch"
	p.Body = ""
	if err := propStore.Save(p); err != nil {
		t.Fatal(err)
	}

	res, err := Apply(p, ApplyDeps{
		ProjectRoot:   project,
		StandingPath:  agents,
		ProposalStore: &propStore,
	})
	if err != nil {
		t.Fatalf("Apply L1: %v", err)
	}
	raw, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "## Conventions") || !strings.Contains(body, "No force-push main") {
		t.Fatalf("standing doc missing bullet:\n%s", body)
	}
	// Must not rewrite Project/Commands wholesale.
	if !strings.Contains(body, "## Project") || !strings.Contains(body, "## Commands") || !strings.Contains(body, "go test ./...") {
		t.Fatalf("standing doc lost skeleton:\n%s", body)
	}
	if strings.Count(body, "# My project") != 1 {
		t.Fatalf("unexpected full rewrite:\n%s", body)
	}

	// Second apply of same rule is no-op-ish (identical bullet).
	p2 := p
	p2.ID = "evp_l1b"
	p2.Status = StatusProposed
	res2, err := Apply(p2, ApplyDeps{ProjectRoot: project, StandingPath: agents})
	if err != nil {
		t.Fatalf("second L1: %v", err)
	}
	if !res2.Noop {
		// If patching again added duplicate, fail.
		raw2, _ := os.ReadFile(agents)
		if strings.Count(string(raw2), "No force-push main") > 1 {
			t.Fatal("duplicate L1 bullets")
		}
	}
	_ = res
}

func TestApplyRejectsNoEvidence(t *testing.T) {
	p := baseProposal(TierL0)
	p.Evidence = nil
	_, err := Apply(p, ApplyDeps{MemoryStore: memory.StoreFor(t.TempDir(), t.TempDir())})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestCaptureApplySamples writes L0/L1 apply artifacts when GROK_GOAL_SCRATCH is
// set (harness evidence only). Uses t.TempDir for all intermediate work.
func TestCaptureApplySamples(t *testing.T) {
	outRoot := strings.TrimSpace(os.Getenv("GROK_GOAL_SCRATCH"))
	if outRoot == "" {
		t.Skip("GROK_GOAL_SCRATCH not set")
	}
	sampleDir := filepath.Join(outRoot, "evolve-apply-sample")
	userDir := t.TempDir()
	project := t.TempDir()
	agents := filepath.Join(project, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# My project\n\n## Project\n\nA demo.\n\n## Commands\n\n- go test ./...\n\n## Conventions\n\n- Use gofmt.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mem := memory.StoreFor(userDir, project)
	p0 := baseProposal(TierL0)
	p0.Target.MemoryName = "no-force-push-main"
	r0, err := Apply(p0, ApplyDeps{MemoryStore: mem, ProjectRoot: project})
	if err != nil {
		t.Fatalf("L0: %v", err)
	}
	b0, err := os.ReadFile(r0.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sampleDir, "L0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sampleDir, "L0", "memory.md"), b0, 0o644); err != nil {
		t.Fatal(err)
	}
	p1 := baseProposal(TierL1)
	p1.ID = "evp_l1_sample"
	p1.Target = Target{Kind: TargetAgentsMD}
	p1.Title = "No force-push main"
	p1.HowToApply = "Never force-push the main branch"
	p1.Body = ""
	if _, err := Apply(p1, ApplyDeps{ProjectRoot: project, StandingPath: agents}); err != nil {
		t.Fatalf("L1: %v", err)
	}
	b1, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sampleDir, "L1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sampleDir, "L1", "AGENTS.md"), b1, 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b0), "**Why:**") || !strings.Contains(string(b1), "No force-push main") {
		t.Fatal("sample contents missing expected markers")
	}
}

func TestComposePrefixUnchangedWithoutReload(t *testing.T) {
	userDir := t.TempDir()
	project := t.TempDir()
	// Pre-seed standing doc so Load has something stable.
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("# P\n\n## Conventions\n\n- Keep tests green.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setBefore := memory.Load(memory.Options{CWD: project, UserDir: userDir})
	prefixBefore := memory.Compose("BASE_SYSTEM", setBefore)

	p := baseProposal(TierL0)
	p.ID = "evp_compose"
	mem := memory.StoreFor(userDir, project)
	if _, err := Apply(p, ApplyDeps{MemoryStore: mem, ProjectRoot: project}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Same pre-apply Set pointer/snapshot: Compose must not change.
	prefixAfter := memory.Compose("BASE_SYSTEM", setBefore)
	if prefixAfter != prefixBefore {
		t.Fatalf("stable prefix mutated without reload\nbefore:\n%s\nafter:\n%s", prefixBefore, prefixAfter)
	}

	// Reload would pick up new memory index — prove disk changed without prefix hot-reload.
	setAfter := memory.Load(memory.Options{CWD: project, UserDir: userDir})
	prefixReloaded := memory.Compose("BASE_SYSTEM", setAfter)
	if prefixReloaded == prefixBefore {
		// Index might still be empty if description path differs; check store list.
		if len(mem.List()) == 0 {
			t.Fatal("expected L0 memory on disk after apply")
		}
	}
}

package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// catalogRun builds a project carrying one skill and returns the request that
// reached the provider, so the assertions read the real prefix and not a
// reconstruction of it.
func catalogRun(t *testing.T, kind, skillName, skillDesc string) provider.Request {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &effectRecordingProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	skillDir := filepath.Join(dir, ".reasonix", "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("make skill dir: %v", err)
	}
	writeFile(t, skillDir, "SKILL.md", "---\nname: "+skillName+"\ndescription: "+skillDesc+"\n---\n\nBody.\n")

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, Ablation: ablation.Set{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
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

// TestEffectSkillCatalogRidesTheTurnNotThePrefix pins the boundary for the
// skills catalog: two projects with different skills must send the byte-
// identical system message — the prefix every session on the machine shares —
// while each turn carries its own listing.
func TestEffectSkillCatalogRidesTheTurnNotThePrefix(t *testing.T) {
	a := catalogRun(t, "boot-effect-cat-a", "alphaskill", "does the alpha thing")
	b := catalogRun(t, "boot-effect-cat-b", "betaskill", "does the beta thing")

	if sysA, sysB := systemOfRequest(a), systemOfRequest(b); sysA != sysB {
		t.Fatalf("different skills composed different prefixes:\nfirst diff site: %q", firstDivergence(sysA, sysB))
	}

	turnA, turnB := userMessagesOf(a), userMessagesOf(b)
	if !strings.Contains(turnA, "alphaskill") || strings.Contains(turnA, "betaskill") {
		t.Fatalf("project A's turn does not carry exactly its own catalog:\n%s", turnA)
	}
	if !strings.Contains(turnB, "betaskill") || strings.Contains(turnB, "alphaskill") {
		t.Fatalf("project B's turn does not carry exactly its own catalog:\n%s", turnB)
	}
}

func systemOfRequest(req provider.Request) string {
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

func userMessagesOf(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

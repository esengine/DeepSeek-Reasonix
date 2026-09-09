package control

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontext"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestTurnContextUsesLiveSkillCatalogAcrossAddEditDelete(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	writeControlSkill(t, project, ".reasonix/skills/alpha/SKILL.md", "---\ndescription: alpha one\n---\nbody")
	store := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, DisableBuiltins: true})
	sess := agent.NewSession("stable system")
	executor := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{
		Executor: executor, SkillStore: store, Skills: store.List(),
		SessionContextStatic: sessioncontext.Sections{Workspace: "workspace"},
	})

	appendCurrent := func() sessioncontext.Snapshot {
		t.Helper()
		if !executor.AppendTurnContext(c.withTurnContext(context.Background(), true)) {
			t.Fatal("expected a replacement context")
		}
		for i := range slices.Backward(sess.Messages) {
			if snapshot, ok := sessioncontext.Parse(sess.Messages[i].Content); ok {
				return snapshot
			}
		}
		t.Fatal("no context found")
		return sessioncontext.Snapshot{}
	}

	first := appendCurrent()
	if !strings.Contains(first.Sections.SkillsCatalog, "alpha one") {
		t.Fatalf("first catalog = %q", first.Sections.SkillsCatalog)
	}
	betaPath := filepath.Join(project, ".reasonix", "skills", "beta", "SKILL.md")
	writeControlSkill(t, project, ".reasonix/skills/beta/SKILL.md", "---\ndescription: beta one\n---\nbody")
	second := appendCurrent()
	if second.Digest == first.Digest || !strings.Contains(second.Sections.SkillsCatalog, "beta one") {
		t.Fatalf("added-skill snapshot = %+v", second)
	}
	writeControlSkill(t, project, ".reasonix/skills/beta/SKILL.md", "---\ndescription: beta edited\n---\nbody")
	third := appendCurrent()
	if third.Digest == second.Digest || !strings.Contains(third.Sections.SkillsCatalog, "beta edited") {
		t.Fatalf("edited-skill snapshot = %+v", third)
	}
	if err := os.Remove(betaPath); err != nil {
		t.Fatal(err)
	}
	fourth := appendCurrent()
	if fourth.Digest == third.Digest || strings.Contains(fourth.Sections.SkillsCatalog, "beta") {
		t.Fatalf("deleted-skill snapshot = %+v", fourth)
	}
	if got := sessionContextCount(sess.Messages); got != 4 {
		t.Fatalf("context count = %d, want one full snapshot per change", got)
	}
	if executor.AppendTurnContext(c.withTurnContext(context.Background(), true)) {
		t.Fatal("unchanged live catalog should deduplicate")
	}
}

func TestTurnContextPublishesBackgroundMemoryReplacementWithoutLegacyUpdate(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "user")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mem := memory.Load(memory.Options{CWD: project, UserDir: userDir})
	sess := agent.NewSession("stable system")
	executor := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: executor, Memory: mem, SessionContextStatic: sessioncontext.Sections{Workspace: "workspace"}})
	if !executor.AppendTurnContext(c.withTurnContext(context.Background(), true)) {
		t.Fatal("initial runtime snapshot was not published")
	}
	if _, err := c.SaveMemory(memory.Memory{
		Name: "currency", Description: "balance currency", Scope: memory.FactScopeGlobal,
		Type: memory.TypeUser, Body: "Use RMB.",
	}); err != nil {
		t.Fatal(err)
	}
	if composed := c.Compose("hello"); strings.Contains(composed, "<memory-update>") {
		t.Fatalf("background save generated legacy update: %q", composed)
	}
	if !executor.AppendTurnContext(c.withTurnContext(context.Background(), true)) {
		t.Fatal("memory change did not publish a context")
	}
	snapshot, ok := latestControlSessionContext(sess.Messages)
	if !ok || !strings.Contains(snapshot.Sections.BackgroundMemory, "currency") || !strings.Contains(snapshot.Sections.BackgroundMemory, "Use RMB.") {
		t.Fatalf("memory snapshot = %+v", snapshot)
	}
	if err := c.ForgetMemory("currency"); err != nil {
		t.Fatal(err)
	}
	if !executor.AppendTurnContext(c.withTurnContext(context.Background(), true)) {
		t.Fatal("forget did not publish a replacement context")
	}
	latest, ok := latestControlSessionContext(sess.Messages)
	if !ok || strings.Contains(latest.Sections.BackgroundMemory, "currency") {
		t.Fatalf("forgotten fact remained in latest snapshot: %+v", latest)
	}
}

func sessionContextCount(messages []provider.Message) int {
	count := 0
	for _, message := range messages {
		if _, ok := sessioncontext.Parse(message.Content); ok && message.Origin == provider.MessageOriginHost {
			count++
		}
	}
	return count
}

func latestControlSessionContext(messages []provider.Message) (sessioncontext.Snapshot, bool) {
	for i := range slices.Backward(messages) {
		if snapshot, ok := sessioncontext.Parse(messages[i].Content); ok && messages[i].Origin == provider.MessageOriginHost {
			return snapshot, true
		}
	}
	return sessioncontext.Snapshot{}, false
}

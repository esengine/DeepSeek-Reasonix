package boot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
)

// skillSearchProvider plays one scripted use_capability call per model round,
// then answers, so every step reads what the previous one put in the context.
type skillSearchProvider struct {
	mu    sync.Mutex
	calls []string
	reqs  []provider.Request
}

func (p *skillSearchProvider) Name() string { return "boot-skill-scale" }

func (p *skillSearchProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	step := len(effectToolResults(req))
	ch := make(chan provider.Chunk, 2)
	if step < len(p.calls) {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: fmt.Sprintf("cap-%d", step), Name: "use_capability", Arguments: p.calls[step]}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *skillSearchProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func writeProjectSkill(t *testing.T, dir, name, frontmatter, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, ".reasonix", "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\n" + frontmatter + "---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A catalog too large to describe keeps every skill nameable on the turn and
// every description reachable through the capability search: the model finds
// a skill by its description, by a trigger its author declared, and runs it.
func TestEffectOversizedSkillCatalogStaysReachable(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	const fillers = 80
	var names []string
	for i := range fillers {
		name := fmt.Sprintf("chore-%02d", i)
		names = append(names, name)
		writeProjectSkill(t, dir, name,
			fmt.Sprintf("description: FILLER-%02d housekeeping playbook padding the fixture catalog well past what one listing can describe.\n", i),
			"filler body")
	}
	writeProjectSkill(t, dir, "ledger-reconcile",
		"description: Reconcile quarterly ledger balances against bank statements and flag mismatches.\n",
		"RECONCILE-BODY-MARKER")
	writeProjectSkill(t, dir, "month-close",
		"description: Month-end procedure.\ntriggers: 季度对账\n",
		"CLOSE-BODY-MARKER")
	names = append(names, "ledger-reconcile", "month-close")

	rec := &skillSearchProvider{calls: []string{
		`{"action":"search","query":"reconcile ledger balances"}`,
		`{"action":"search","query":"季度对账"}`,
		`{"action":"call","capability_id":"skill:ledger-reconcile","arguments":{"arguments":"Q3"}}`,
	}}
	provider.Register("skill-scale", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "skill-scale"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "turn-alpha"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := rec.requests()
	if len(reqs) < 4 {
		t.Fatalf("the script needs four model rounds, saw %d", len(reqs))
	}

	listing := blockOf(projectionOf(t, reqs[0], "turn-alpha"), "available-skills")
	if listing == "" {
		t.Fatal("no skill listing reached the boundary, so this test measures nothing")
	}
	for _, name := range names {
		if !strings.Contains(listing, "\n- "+name+"\n") {
			t.Errorf("%s is not nameable from the listing the model was sent", name)
		}
	}
	if strings.Contains(listing, "FILLER-") || strings.Contains(listing, "truncated") {
		t.Errorf("the oversized listing carries clipped descriptions instead of names:\n%s", listing)
	}
	if !strings.Contains(listing, `use_capability({ action: "search"`) {
		t.Errorf("the listing does not say where the descriptions went:\n%s", listing)
	}
	t.Logf("listing: %d bytes for %d project skills", len(listing), len(names))

	byDescription := effectToolResults(reqs[1])[0]
	if !strings.Contains(byDescription, `"id": "skill:ledger-reconcile"`) || !strings.Contains(byDescription, "bank statements") {
		t.Errorf("searching by what the skill does did not return it with its description:\n%s", byDescription)
	}
	byTrigger := effectToolResults(reqs[2])[1]
	if !strings.Contains(byTrigger, `"id": "skill:month-close"`) {
		t.Errorf("searching by a declared trigger did not return the skill:\n%s", byTrigger)
	}
	ran := effectToolResults(reqs[3])[2]
	if !strings.Contains(ran, "RECONCILE-BODY-MARKER") {
		t.Errorf("calling the found skill did not run it:\n%s", ran)
	}
}

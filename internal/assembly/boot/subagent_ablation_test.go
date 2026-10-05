package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
)

// The arm is only a control if it removes delegation whichever tool reaches it.
// A measured no-subagent run spent a child on `explore` before this gate.
func TestSubagentArmRefusesProfileSkillDelegation(t *testing.T) {
	called := false
	run := skill.SubagentRunner(func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
		called = true
		return "ran", nil
	})
	sk := skill.Skill{Name: "explore", RunAs: skill.RunSubagent}

	gated := gateSubagentArm(ablation.New(ablation.Subagent), run)
	if _, err := gated(context.Background(), sk, "task", skill.SubagentRunOptions{}); err == nil ||
		!strings.Contains(err.Error(), "subagent ablation arm") {
		t.Fatalf("err = %v, want a refusal naming the arm", err)
	}
	if called {
		t.Fatal("the gate must refuse before spawning a child")
	}

	// Without the arm the runner is untouched, so the normal path keeps its
	// exact behaviour rather than routing through a wrapper.
	if _, err := gateSubagentArm(ablation.Set{}, run)(context.Background(), sk, "task", skill.SubagentRunOptions{}); err != nil {
		t.Fatalf("ungated run: %v", err)
	}
	if !called {
		t.Fatal("the ungated runner must still run")
	}
}

// Under the arm the control run must not even carry the wrapper schemas.
func TestSubagentArmDropsBuiltinWrapperTools(t *testing.T) {
	run := skill.SubagentRunner(func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
		return "", nil
	})
	if got := builtinSubagentTools(ablation.New(ablation.Subagent), skill.New(skill.Options{}), run); len(got) != 0 {
		t.Fatalf("arm still registered %d wrapper tools", len(got))
	}
	if got := builtinSubagentTools(ablation.Set{}, skill.New(skill.Options{}), run); len(got) == 0 {
		t.Fatal("the ordinary run must keep explore/research/review")
	}
}

// The evidence arm has to reach the schema the provider is shown, not only the
// readiness gate. Ablating the gate alone left both arms paying for the same
// contract, so the comparison could only ever answer for the gate.
func TestEvidenceArmRemovesTheEvidenceToolSchema(t *testing.T) {
	surface := func(arm ablation.Set) map[string]bool {
		reg := tool.NewRegistry()
		for _, x := range tool.Builtins() {
			reg.Add(x)
		}
		applyUnifiedProviderToolSurface(reg, false, arm, nil)
		names := map[string]bool{}
		for _, s := range reg.Schemas() {
			names[s.Name] = true
		}
		return names
	}

	control := surface(ablation.Set{})
	for _, name := range EvidenceToolNames() {
		if !control[name] {
			t.Fatalf("control run is missing %q, so the arm would measure nothing", name)
		}
	}

	arm := surface(ablation.New(ablation.Evidence))
	for _, name := range EvidenceToolNames() {
		if arm[name] {
			t.Fatalf("%q still in the schema under the evidence arm", name)
		}
	}
	// The arm is evidence, not task state: a task list is not an evidence claim.
	if !arm["todo_write"] {
		t.Fatal("todo_write must survive the evidence arm")
	}
}

// A child gets its body as its whole prefix, so a fact the parent was told and
// it was not is a fact it will spend a round rediscovering — or guess wrong.
func TestSkillSubagentPromptCarriesTheWorkspaceVCS(t *testing.T) {
	root := robustTempDir(t)
	r := &skillSubagents{root: root}
	sk := skill.Skill{Name: "review", Body: "REVIEW BODY"}

	plain := r.systemPrompt(sk)
	if !strings.HasPrefix(plain, "REVIEW BODY") {
		t.Fatalf("prompt must lead with the body, got %q", plain)
	}
	if !strings.Contains(plain, "Version control: none (not a repository)") {
		t.Fatalf("prompt = %q, want the no-repository fact stated", plain)
	}

	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := r.systemPrompt(sk); !strings.Contains(got, "Version control: git") {
		t.Fatalf("prompt = %q, want the detected version control named", got)
	}
}

// recall keeps its read half under the search arm, so the check is on the
// schema's shape rather than the tool's presence: an arm shown a query
// parameter its runtime refuses is being asked a question it cannot answer.
func TestRecallSearchArmRemovesTheQueryParameter(t *testing.T) {
	schemaOf := func(arm ablation.Set) string {
		reg := tool.NewRegistry()
		for _, x := range tool.Builtins() {
			reg.Add(x)
		}
		applyUnifiedProviderToolSurface(reg, false, arm, nil)
		for _, s := range reg.Schemas() {
			if s.Name == "recall" {
				b, err := json.Marshal(s)
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
		}
		t.Fatal("recall is not provider-visible; the arm would measure nothing")
		return ""
	}

	control := schemaOf(ablation.Set{})
	if !strings.Contains(control, `"query"`) {
		t.Fatalf("control recall schema has no query parameter:\n%s", control)
	}
	arm := schemaOf(ablation.New(ablation.RecallSearch))
	if strings.Contains(arm, `"query"`) {
		t.Errorf("the no-recall-search arm is still shown a query parameter:\n%s", arm)
	}
	if !strings.Contains(arm, `"positions"`) {
		t.Errorf("the arm lost the read half too:\n%s", arm)
	}
	if strings.Contains(arm, "search") {
		t.Errorf("the arm's description still offers search:\n%s", arm)
	}
}

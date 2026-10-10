package boot

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
)

// A tool an assembly registers only under some configuration, or only in a
// sub-registry, must be declared with tool.RegisterConditional: anything absent
// from the default session and from the built-in catalog would otherwise have
// its permission rules refused as naming no tool.
func TestEveryAssembledToolIsKnownToTheRuleVocabulary(t *testing.T) {
	known := map[string]bool{}
	for _, tl := range tool.Builtins() {
		known[tl.Name()] = true
	}
	for _, n := range tool.ConditionalNames() {
		known[n] = true
	}
	configs := map[string]string{
		"default":   "",
		"advisor":   "[agent]\nadvisor_model = \"test-model\"\n",
		"bestofn":   "[agent]\nbest_of_n = true\n",
		"isolation": "[agent]\nworktree_isolation = true\n",
		"enabled":   "[tools]\nenabled = [\"read_file\"]\n",
	}
	assembled := map[string][]string{}
	baseline := func() {
		for _, n := range assembled["default"] {
			known[n] = true
		}
	}
	for _, label := range []string{"default", "advisor", "bestofn", "isolation", "enabled"} {
		extra := configs[label]
		isolateConfigHome(t)
		dir := robustTempDir(t)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		t.Chdir(dir)
		writeUserConfig(t, userModel+"\n"+extra)
		registerBootTokenProfileTestProvider()
		setBootTokenProfileTestProvider(t, testutil.NewMock("tools"))
		ctrl, err := Build(context.Background(), Options{})
		if err != nil {
			t.Fatalf("%s: Build: %v", label, err)
		}
		for _, e := range ctrl.AllToolContractEntries() {
			assembled[label] = append(assembled[label], e.Name)
		}
		ctrl.Close()
		if label == "default" {
			baseline()
		}
	}
	parent := tool.NewRegistry()
	for _, tl := range tool.Builtins() {
		parent.Add(tl)
	}
	review := tool.NewRegistry()
	agent.AttachReviewReportTool(review, agent.ReviewReportGrant{})
	assembled["planner"] = agent.PlannerToolRegistry(parent).Names()
	assembled["review"] = review.Names()
	assembled["subagent"] = agent.SubagentToolRegistryForDepth(parent, nil, 1, 3).Names()
	assembled["readonly"] = agent.ReadOnlySubagentToolRegistryForDepth(parent, nil, 1, 3).Names()
	for label, names := range assembled {
		for _, n := range names {
			if !known[n] && !tool.IsConnectionName(n) {
				t.Errorf("%s registers %q, which is neither a built-in nor declared with tool.RegisterConditional", label, n)
			}
		}
	}
	if !slices.Contains(assembled["advisor"], "advise") {
		t.Errorf("advisor config did not assemble advise: %s", strings.Join(assembled["advisor"], ","))
	}
}

// Declaring a conditional tool must not put it in a session that was not
// offered it: the request's tool list is the cache-stable prefix.
func TestEffectConditionalDeclarationsDoNotReachTheProviderPrefix(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, userModel)
	req := firstTokenProfileRequest(t, "")
	sent := map[string]bool{}
	for _, schema := range req.Tools {
		sent[schema.Name] = true
	}
	for _, n := range []string{"advise", "best_of_n", "apply_isolated", "discard_isolated", "complete_subtask", "system_one", "submit_plan", "conclude_no_changes", "review_report"} {
		if sent[n] {
			t.Errorf("%s is declared conditional but reached the default request tools", n)
		}
	}
}

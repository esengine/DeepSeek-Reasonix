package boot

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestSkillDiscoveryAndEmbeddedReferencesThroughBoot(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	approveWorkspace(t, dir)
	for i := range 100 {
		name := fmt.Sprintf("a-%03d-%s", i, strings.Repeat("x", 45))
		writeFile(t, dir, ".agents/skills/"+name+"/SKILL.md",
			"---\nname: "+name+"\ndescription: Common fixture operation\n---\nFixture body")
	}
	const target = "zzz-rare-diagnostic"
	writeFile(t, dir, ".agents/skills/"+target+"/SKILL.md",
		"---\nname: "+target+"\ndescription: Diagnose rare service faults\n---\nRARE_BODY_ON_DEMAND")
	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("skill-disclosure",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "find", Name: "use_capability",
			Arguments: `{"action":"search","query":"zzz-rare-diagnostic"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "load", Name: "use_capability",
			Arguments: `{"action":"call","capability_id":"skill:zzz-rare-diagnostic","arguments":{}}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "guide", Name: "use_capability",
			Arguments: `{"action":"call","capability_id":"tool:read_skill","arguments":{"name":"reasonix-guide"}}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "page", Name: "use_capability",
			Arguments: `{"action":"call","capability_id":"tool:read_skill","arguments":{"name":"reasonix-guide","reference":"references/hooks.md"}}`}}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "Find the rare diagnostic and consult hook matching guidance."); err != nil {
		t.Fatal(err)
	}
	reqs := mainConversationRequests(prov.Requests())
	if len(reqs) < 5 {
		t.Fatalf("request count = %d, want full discovery/read sequence", len(reqs))
	}
	initial := sessionContextMessage(reqs[0].Messages)
	if strings.Contains(initial, target) || !strings.Contains(initial, "more skills") {
		t.Fatalf("fixture did not exercise an omitted catalog entry: %s", initial)
	}
	for _, req := range reqs {
		if systemMessage(req.Messages) != systemMessage(reqs[0].Messages) ||
			!reflect.DeepEqual(req.Tools, reqs[0].Tools) {
			t.Fatal("skill discovery/read changed the provider prefix or schemas")
		}
	}
	var outputs []string
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool {
			outputs = append(outputs, msg.Content)
		}
	}
	if len(outputs) != 4 {
		t.Fatalf("tool outputs = %d, want 4", len(outputs))
	}
	if !strings.Contains(outputs[0], "skill:"+target) ||
		!strings.Contains(outputs[1], "RARE_BODY_ON_DEMAND") {
		t.Fatalf("omitted skill not discoverable/invocable: %v", outputs[:2])
	}
	if strings.Contains(outputs[2], "hook.invalid_matcher") ||
		!strings.Contains(outputs[3], "hook.invalid_matcher") {
		t.Fatalf("reference page not loaded only on demand: %v", outputs[2:])
	}
}

package boot

import (
	"context"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestSkillDiagnosticsFollowRuntimeBindingScope(t *testing.T) {
	isolateConfigHome(t)
	reg := tool.NewRegistry()
	host := plugin.NewHost()
	defer host.Close()
	for _, spec := range []plugin.Spec{{Name: "a", Package: "owner-a"}, {Name: "b", Package: "owner-b"}} {
		for _, item := range plugin.LazyToolset(spec, &plugin.CachedSchema{Tools: []plugin.CachedTool{{Name: "search"}}}, host, reg, context.Background(), false) {
			reg.Add(item)
		}
	}
	store := skill.New(skill.Options{DisableDiscovery: true})
	store.ConfigureToolBindings(func(sk skill.Skill) []tool.MCPBinding { return skillMCPBindings(sk, reg, nil, nil, nil) })
	opts := skill.ToolReferenceOptions{Known: tool.KnownToolNames(), Bindings: reg.MCPBindings()}
	owned := skill.Skill{Name: "owned", Plugin: "owner-a", AllowedTools: []string{"search"}}
	prepared := store.Prepare(owned)
	if len(prepared.AllowedTools) != 2 || prepared.AllowedTools[0] != "mcp__a__search" {
		t.Fatal(prepared.AllowedTools)
	}
	if d := skill.CheckToolReferences([]skill.Skill{owned}, opts); len(d) != 0 {
		t.Fatal(d)
	}
	local := skill.Skill{Name: "local", AllowedTools: []string{"search"}}
	child := agent.SubagentToolRegistry(reg, store.Prepare(local).AllowedTools)
	for _, name := range []string{"search", "mcp__a__search", "mcp__b__search", "use_capability"} {
		if _, ok := child.Get(name); ok {
			t.Fatal("unexpected runtime tool", name)
		}
	}
	d := skill.CheckToolReferences([]skill.Skill{local}, opts)
	if len(d) != 1 || d[0].Code != "skill.tool_reference_unknown" {
		t.Fatalf("invalid local alias accepted: %+v", d)
	}
}

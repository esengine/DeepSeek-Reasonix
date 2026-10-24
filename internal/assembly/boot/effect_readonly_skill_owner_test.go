package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type readonlySkillEffectProvider struct {
	rootCalls int
	children  []provider.Request
}

func (*readonlySkillEffectProvider) Name() string { return "readonly-skill-effect" }
func (p *readonlySkillEffectProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if strings.Contains(firstUser(req), "INSPECT_SKILL_FIXTURE") {
		p.children = append(p.children, req)
		return (&skillOwnerProvider{}).Stream(ctx, req)
	}
	ch := make(chan provider.Chunk, 2)
	if p.rootCalls%2 == 0 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("readonly-skill-%d", p.rootCalls), Name: "read_only_skill",
			Arguments: `{"name":"readonly-probe","arguments":"INSPECT_SKILL_FIXTURE"}`}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "finished"}
	}
	p.rootCalls++
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectSharedReadOnlySkillKeepsEphemeralBoundsAndStablePrefix(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	p := &readonlySkillEffectProvider{}
	provider.Register("readonly-skill-effect", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `default_model="test-model"
[agent]
system_prompt="BASE"
collaboration="shared"
[codegraph]
enabled=false
[[providers]]
name="test-model"
kind="readonly-skill-effect"
model="x"
`)
	writeFile(t, root, ".reasonix/skills/readonly-probe/SKILL.md", "---\nname: readonly-probe\ndescription: Inspect a fixture\nrunAs: subagent\nallowed-tools: read_file\n---\nInspect the requested fixture and answer.\n")
	mirrorToUserConfigAt(t, home, root)
	approveWorkspaceAt(t, home, root)
	c, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetToolApprovalMode(control.ToolApprovalYolo)
	for range 2 {
		if err := c.RunTurn(t.Context(), "Run the read-only skill"); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.children) != 2 {
		t.Fatalf("read-only skill did not run twice: %d", len(p.children))
	}
	for _, req := range p.children {
		read, complete := false, false
		for _, schema := range req.Tools {
			switch schema.Name {
			case "read_file":
				read = true
			case "complete_subtask":
				complete = true
			default:
				t.Fatalf("expanded read-only surface: %s", schema.Name)
			}
		}
		if !read || !complete {
			t.Fatal("missing shared execution protocol or research capability")
		}
	}
	a, _ := json.Marshal(p.children[0].Tools)
	b, _ := json.Marshal(p.children[1].Tools)
	if string(a) != string(b) || p.children[0].Messages[0].Content != p.children[1].Messages[0].Content {
		t.Fatal("child prefix changed between identical invocations")
	}
	children, err := sessionstore.ListSubagentsByParent(c.SessionDir(), sessionstore.BranchID(c.SessionPath()))
	if err != nil || len(children) != 0 {
		t.Fatalf("ephemeral skill persisted child state: %v %v", children, err)
	}
}

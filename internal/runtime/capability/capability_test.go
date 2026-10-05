package capability

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
)

// A skill is routed by the triggers its author declared, never by the host
// recognizing a topic. "帮我看看这段代码有没有问题" reads like a review request
// and still buys nothing until some skill says that phrase is its trigger.
func TestRouteMatchesAuthoredTriggersNotTopicWords(t *testing.T) {
	untriggered := SkillEntries([]skill.Skill{{
		Name:        "review",
		Description: "review code for bugs",
		Scope:       skill.ScopeBuiltin,
	}}, []tool.ContractEntry{{Name: "run_skill"}})
	if decision := Route("帮我看看这段代码有没有问题", untriggered); len(decision.Candidates) != 0 {
		t.Fatalf("topic words routed a skill nobody triggered: %+v", decision.Candidates)
	}

	triggered := SkillEntries([]skill.Skill{{
		Name:        "review",
		Description: "review code for bugs",
		Scope:       skill.ScopeBuiltin,
		Triggers:    []string{"有没有问题"},
	}}, []tool.ContractEntry{{Name: "run_skill"}})
	decision := Route("帮我看看这段代码有没有问题", triggered)
	if len(decision.Candidates) == 0 {
		t.Fatal("authored trigger did not match")
	}
	if got := decision.Candidates[0]; got.Entry.ID != "skill:review" || got.Policy != AutoUsePrefer {
		t.Fatalf("candidate = %+v, want review/prefer", got)
	}
}

func TestRouteDoesNotTreatTextMentionAsInvocation(t *testing.T) {
	entries := SkillEntries([]skill.Skill{{
		Name:        "audit",
		Description: "audit something",
		Scope:       skill.ScopeProject,
	}}, []tool.ContractEntry{{Name: "run_skill"}})

	decision := Route("/audit 检查一下", entries)
	if len(decision.Candidates) != 0 {
		t.Fatalf("text mention must not create a structural skill invocation: %+v", decision.Candidates)
	}

	// Prose naming also hands off to the semantic router.
	for _, prose := range []string{"请使用 audit skill 检查一下", "use the audit skill", "don't use the audit skill"} {
		if d := Route(prose, entries); len(d.Candidates) != 0 {
			t.Errorf("%q routed deterministically: %+v", prose, d.Candidates)
		}
	}
}

func TestRouteRespectsSkillAutoUseMetadata(t *testing.T) {
	entries := SkillEntries([]skill.Skill{
		{
			Name:        "quiet",
			Description: "quiet skill",
			Scope:       skill.ScopeProject,
			Triggers:    []string{"inspect"},
			AutoUse:     "off",
		},
		{
			Name:        "gentle",
			Description: "gentle skill",
			Scope:       skill.ScopeProject,
			Triggers:    []string{"inspect"},
			AutoUse:     "suggest",
		},
	}, []tool.ContractEntry{{Name: "run_skill"}})

	decision := Route("please inspect this", entries)
	if len(decision.Candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly the suggest skill", decision.Candidates)
	}
	if got := decision.Candidates[0]; got.Entry.ID != "skill:gentle" || got.Policy != AutoUseSuggest {
		t.Fatalf("candidate = %+v, want gentle/suggest", got)
	}
}

func TestRouteKeepsAllStrongCandidatesBeforeSuggestBudget(t *testing.T) {
	entries := make([]Entry, 0, 8)
	for i := range 6 {
		entries = append(entries, Entry{ID: fmt.Sprintf("skill:required-%d", i), Kind: KindSkill, Name: fmt.Sprintf("required-%d", i), AutoUse: AutoUsePrefer, Triggers: []string{"ship"}})
	}
	entries = append(entries,
		Entry{ID: "skill:suggest-a", Kind: KindSkill, Name: "suggest-a", AutoUse: AutoUseSuggest, Triggers: []string{"ship"}},
		Entry{ID: "skill:suggest-b", Kind: KindSkill, Name: "suggest-b", AutoUse: AutoUseSuggest, Triggers: []string{"ship"}},
	)

	decision := Route("ship this", entries)
	if len(decision.Candidates) != 6 {
		t.Fatalf("candidates = %d, want all 6 strong candidates", len(decision.Candidates))
	}
	for _, candidate := range decision.Candidates {
		if candidate.Policy != AutoUsePrefer {
			t.Fatalf("suggest candidate displaced a strong candidate: %+v", candidate)
		}
	}
}

func TestRouteDeliveryPromotesMatchedBuiltinSkills(t *testing.T) {
	entries := []Entry{
		{ID: "skill:explore", Kind: KindSkill, Name: "explore", Source: string(skill.ScopeBuiltin), AutoUse: AutoUseSuggest, Triggers: []string{"调用链"}},
		{ID: "skill:custom", Kind: KindSkill, Name: "custom", Source: string(skill.ScopeProject), AutoUse: AutoUseSuggest, Triggers: []string{"调用链"}},
	}
	decision := RouteDelivery("分析调用链", entries)
	if !decision.Delivery || len(decision.Candidates) != 2 {
		t.Fatalf("delivery decision = %+v", decision)
	}
	if decision.Candidates[0].Entry.ID != "skill:explore" || decision.Candidates[0].Policy != AutoUsePrefer {
		t.Fatalf("built-in candidate was not promoted: %+v", decision.Candidates)
	}
	if decision.Candidates[1].Entry.ID != "skill:custom" || decision.Candidates[1].Policy != AutoUseSuggest {
		t.Fatalf("custom authored policy changed: %+v", decision.Candidates)
	}
}

// An MCP tool is routed when the user names its server. Mentioning the vendor
// in passing is not naming it: "查一下 GitHub issue" used to buy a prefer for
// every github tool, which spent the strong slots on a guess.
func TestRouteMatchesNamedMCPServerNotVendorMentions(t *testing.T) {
	entries := ToolEntries([]tool.ContractEntry{{
		Name:        "mcp__github__search_issues",
		Description: "search GitHub issues",
		ReadOnly:    true,
	}})

	if decision := Route("查一下 GitHub issue 里有没有相关反馈", entries); len(decision.Candidates) != 0 {
		t.Fatalf("a vendor mention routed an MCP tool: %+v", decision.Candidates)
	}

	// Naming the tool is naming the identifier the host minted for it.
	decision := Route("用 mcp__github__search_issues 查一下相关反馈", entries)
	if len(decision.Candidates) == 0 {
		t.Fatal("named MCP tool did not route")
	}
	got := decision.Candidates[0]
	if got.Entry.ID != "mcp-tool:github/search_issues" || got.Policy != AutoUsePrefer {
		t.Fatalf("candidate = %+v, want github mcp/prefer", got)
	}

	// "<server> mcp" matched any sentence containing it, so declining the
	// server preferred it. Both of these used to route.
	for _, refusal := range []string{"don't use the github mcp server", "别用 github mcp"} {
		if d := Route(refusal, entries); len(d.Candidates) != 0 {
			t.Errorf("%q routed: %+v", refusal, d.Candidates)
		}
	}
}

func TestRouteDoesNotPreferFailedCachedMCPTool(t *testing.T) {
	entries := []Entry{{
		ID:            "mcp-tool:github/search_issues",
		Kind:          KindMCPTool,
		Name:          "github/search_issues",
		Source:        "github",
		Status:        StatusFailed,
		ConnectSource: "mcp",
		ConnectName:   "github",
	}}

	decision := Route("用 github mcp 查一下相关反馈", entries)
	if len(decision.Candidates) != 0 {
		t.Fatalf("failed cached MCP tool was still routed: %+v", decision.Candidates)
	}
}

func TestRenderTransientBlockMentionsConnectSource(t *testing.T) {
	decision := RouteDecision{Candidates: []RouteCandidate{{
		Entry: Entry{
			ID:            "skill:review",
			Kind:          KindSkill,
			Name:          "review",
			Status:        StatusConfigured,
			ConnectSource: "skills",
		},
		Policy: AutoUsePrefer,
		Reason: "matched",
	}}}

	block := RenderTransientBlock(decision)
	for _, want := range []string{`<capability-route version="1">`, `source:skills`, `connect_tool_source`} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
}

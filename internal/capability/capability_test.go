package capability

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestRoutePrefersReviewSkillForReviewRequest(t *testing.T) {
	entries := SkillEntries([]skill.Skill{{
		Name:        "review",
		Description: "review code for bugs",
		Scope:       skill.ScopeBuiltin,
	}}, []tool.ContractEntry{{Name: "run_skill"}})

	decision := Route("帮我看看这段代码有没有问题", entries)
	if len(decision.Candidates) == 0 {
		t.Fatal("Route returned no candidates")
	}
	got := decision.Candidates[0]
	if got.Entry.ID != "skill:review" || got.Policy != AutoUsePrefer {
		t.Fatalf("candidate = %+v, want review/prefer", got)
	}
}

func TestRouteRequiresExplicitSkill(t *testing.T) {
	entries := SkillEntries([]skill.Skill{{
		Name:        "audit",
		Description: "audit something",
		Scope:       skill.ScopeProject,
	}}, []tool.ContractEntry{{Name: "run_skill"}})

	decision := Route("请使用 audit skill 检查一下", entries)
	if len(decision.Candidates) == 0 {
		t.Fatal("Route returned no candidates")
	}
	if got := decision.Candidates[0].Policy; got != AutoUseRequire {
		t.Fatalf("policy = %s, want require", got)
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
	for i := 0; i < 6; i++ {
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

func TestRoutePrefersGitHubMCPForIssueLookup(t *testing.T) {
	entries := ToolEntries([]tool.ContractEntry{{
		Name:        "mcp__github__search_issues",
		Description: "search GitHub issues",
		ReadOnly:    true,
	}})

	decision := Route("查一下 GitHub issue 里有没有相关反馈", entries)
	if len(decision.Candidates) == 0 {
		t.Fatal("Route returned no candidates")
	}
	got := decision.Candidates[0]
	if got.Entry.ID != "mcp-tool:github/search_issues" || got.Policy != AutoUsePrefer {
		t.Fatalf("candidate = %+v, want github mcp/prefer", got)
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

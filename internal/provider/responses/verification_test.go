package responses

import (
	"strings"
	"testing"
)

func TestExtractNumericFacts(t *testing.T) {
	facts := ExtractNumericFacts("2025年集装箱吞吐量达4,466万TEU，增长8.6%；燃料销量5,677万吨")
	if len(facts) < 4 {
		t.Fatalf("must extract numeric facts, got %v", facts)
	}
	found := false
	for _, f := range facts {
		if strings.Contains(f, "4,466") {
			found = true
		}
	}
	if !found {
		t.Fatalf("must find 4,466: %v", facts)
	}
}

func TestCrossCheckVerified(t *testing.T) {
	// 同一事实（4466万TEU）出现在官方 + 行业两个独立源 → verified
	e1 := &KnowledgeEntry{
		AnswerSummary: "新加坡2025年集装箱吞吐量4,466万TEU创纪录",
		Sources:       []Source{{Domain: "mpa.gov.sg", Title: "新加坡海事港务局"}},
	}
	e2 := &KnowledgeEntry{
		AnswerSummary: "MPA数据：2025年新加坡处理4466万标准箱",
		Sources:       []Source{{Domain: "reuters.com", Title: "路透"}},
	}
	facts := []string{"2025年集装箱吞吐量 4466万TEU"}
	rep := CrossCheck(facts, []*KnowledgeEntry{e1, e2})
	if rep.VerifiedN != 1 {
		t.Fatalf("must verify cross-source fact: %+v", rep)
	}
	if !rep.Coverage["official"] || !rep.Coverage["industry"] {
		t.Fatalf("coverage must include official+industry: %v", rep.Coverage)
	}
	// 旁观验证结论提及第三方
	if !strings.Contains(rep.Conclusion, "旁观验证") {
		t.Fatalf("conclusion must mention third-party: %q", rep.Conclusion)
	}
}

func TestCrossCheckSingleSource(t *testing.T) {
	// 仅官方单一来源 → single_src（无可比对的第二独立源）
	e := &KnowledgeEntry{
		AnswerSummary: "某数据 12345",
		Sources:       []Source{{Domain: "mpa.gov.sg", Title: "新加坡海事港务局"}},
	}
	facts := []string{"某数据 12345"}
	rep := CrossCheck(facts, []*KnowledgeEntry{e})
	if rep.VerifiedN != 0 || rep.SingleN != 1 {
		t.Fatalf("single source must be flagged: %+v", rep)
	}
	// 不可分类来源（个人博客）→ unverified（无法验证）
	e2 := &KnowledgeEntry{AnswerSummary: "其他 999", Sources: []Source{{Domain: "blog.example.com"}}}
	rep2 := CrossCheck([]string{"其他 999"}, []*KnowledgeEntry{e2})
	if rep2.VerifiedN != 0 || rep2.SingleN != 0 {
		t.Fatalf("uncategorized source must be unverified: %+v", rep2)
	}
}

func TestCrossCheckEmpty(t *testing.T) {
	rep := CrossCheck(nil, nil)
	if rep.Conclusion == "" {
		t.Fatal("empty must still produce conclusion")
	}
	if rep.Render() == "" {
		t.Fatal("empty render must produce header")
	}
}

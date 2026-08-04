package responses

import "testing"

func TestClassifyTier(t *testing.T) {
	cases := []struct {
		query string
		want  RetrievalTier
	}{
		{"今天北京天气怎么样", TierSimple},
		{"北京人口多少", TierSimple},
		{"什么是量子计算", TierSimple},
		{"2026年8月3日北京天气怎么样", TierSimple},
		{"总结2026年AI领域的主要进展", TierGeneral},
		{"列出top10开源大模型", TierGeneral},
		{"最新进展是什么", TierGeneral},
		{"对比ChatGPT和DeepSeek的优缺点", TierComplex},
		{"分析AI对就业的影响", TierComplex},
		{"这个政策争议有哪些不同观点", TierComplex},
		{"撰写一份关于大模型安全的研究报告", TierDeep},
		{"深度调研RAG技术的最新论文", TierDeep},
		{"详细分析2026年全球AI市场格局并给出投资建议，覆盖技术路线、商业模式、监管政策、主要玩家竞争态势、未来五年发展趋势预测，最后总结风险与机遇", TierDeep},
		{"", TierSimple},
	}
	for _, c := range cases {
		got, rounds := ClassifyTier(c.query)
		if got != c.want {
			t.Errorf("%q => %s, want %s (rounds=%d)", c.query, got, c.want, rounds)
			continue
		}
		if rounds < 1 || rounds > 12 {
			t.Errorf("%q rounds=%d out of range", c.query, rounds)
		}
	}
}

func TestTierMaxRounds(t *testing.T) {
	if TierSimple.maxRounds() != 1 {
		t.Errorf("simple rounds=%d want 1", TierSimple.maxRounds())
	}
	if TierGeneral.maxRounds() != 3 {
		t.Errorf("general rounds=%d want 3", TierGeneral.maxRounds())
	}
	if TierComplex.maxRounds() != 5 {
		t.Errorf("complex rounds=%d want 5", TierComplex.maxRounds())
	}
	if TierDeep.maxRounds() != 12 {
		t.Errorf("deep rounds=%d want 12", TierDeep.maxRounds())
	}
}

// TestTierDomainValid：本地注入知识域是合法 Tier，且不触发 web 刷新。
func TestTierDomainValid(t *testing.T) {
	if TierDomain != "domain" {
		t.Fatalf("TierDomain must be \"domain\", got %q", TierDomain)
	}
	// tierOf 接受 domain 并原样返回
	e := &KnowledgeEntry{Tier: string(TierDomain)}
	if got := tierOf(e); got != TierDomain {
		t.Fatalf("tierOf must preserve domain, got %q", got)
	}
}

package responses

import (
	"strings"
	"testing"
)

func TestPlanResearchDepthCoverage(t *testing.T) {
	// L1: 仅事实
	p1 := PlanResearch("量子计算", DepthL1)
	if len(p1.Queries) != 1 || p1.Queries[0].Aspect != AspectFact {
		t.Fatalf("L1 must be single fact query, got %+v", p1.Queries)
	}
	if !p1.Coverage[AspectFact] {
		t.Fatal("L1 must cover fact")
	}

	// L2: 事实 + 原因 + 官方 + 媒体
	p2 := PlanResearch("AI 监管", DepthL2)
	if len(p2.Queries) != 4 {
		t.Fatalf("L2 want 4 queries, got %d: %+v", len(p2.Queries), p2.Queries)
	}
	for _, want := range []QueryAspect{AspectFact, AspectCause, AspectOfficial, AspectMedia} {
		if !p2.Coverage[want] {
			t.Fatalf("L2 must cover %s", want)
		}
	}

	// L3: 全维 9 查询
	p3 := PlanResearch("新能源产业", DepthL3)
	if len(p3.Queries) != 9 {
		t.Fatalf("L3 want 9 queries, got %d: %+v", len(p3.Queries), p3.Queries)
	}
	for _, want := range []QueryAspect{
		AspectFact, AspectCause, AspectFuture, AspectRisk,
		AspectOfficial, AspectMedia, AspectData,
		AspectRecent, AspectHistory,
	} {
		if !p3.Coverage[want] {
			t.Fatalf("L3 must cover %s", want)
		}
	}
	// 事实第一优先
	if p3.Queries[0].Aspect != AspectFact {
		t.Fatalf("fact must be first, got %s", p3.Queries[0].Aspect)
	}
}

func TestPlanToTier(t *testing.T) {
	if PlanToTier(DepthL1) != TierSimple {
		t.Fatal("L1 -> simple")
	}
	if PlanToTier(DepthL2) != TierComplex {
		t.Fatal("L2 -> complex")
	}
	if PlanToTier(DepthL3) != TierDeep {
		t.Fatal("L3 -> deep")
	}
}

func TestNeedClarification(t *testing.T) {
	if !NeedClarification("随便聊聊", DepthL3) {
		t.Fatal("L3 always needs clarification")
	}
	if !NeedClarification("这个", DepthL1) {
		t.Fatal("vague short topic needs clarification")
	}
	if NeedClarification("2026年北京新能源产业政策", DepthL1) {
		t.Fatal("specific topic should not need clarification at L1")
	}
	if !strings.Contains(ClarificationQuestion("x", DepthL3), "时间窗口") {
		t.Fatal("L3 question must mention scope dimensions")
	}
}

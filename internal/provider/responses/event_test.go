package responses

import (
	"testing"
	"time"
)

func TestAdvanceEventConfidenceRisesAndConflictPenalizes(t *testing.T) {
	now := time.Now()
	e := &KnowledgeEntry{TimeSensitive: true, FreshUntil: now.Add(-time.Hour)}

	// 第一次增量更新（无冲突）：置信度上升
	advanceEvent(e, now, []string{"美军航母已抵达波斯湾"}, nil)
	if e.UpdateCount != 1 {
		t.Fatalf("update count=%d want 1", e.UpdateCount)
	}
	if e.Confidence < baseConfidence || e.Confidence > baseConfidence+confidenceStep {
		t.Fatalf("confidence=%.2f want ~%.2f", e.Confidence, baseConfidence+confidenceStep)
	}
	if e.ConflictDetected {
		t.Fatal("no conflict should be detected")
	}
	// 时效刷新
	if e.FreshUntil.Before(now) {
		t.Fatal("FreshUntil should be refreshed after update")
	}

	// 第二次更新带冲突信号（"否认"）
	advanceEvent(e, now, []string{"伊朗否认美军航母抵达波斯湾"}, nil)
	if !e.ConflictDetected {
		t.Fatal("conflict should be detected")
	}
	if e.Confidence > baseConfidence {
		t.Fatalf("conflict must drop confidence, got %.2f", e.Confidence)
	}
}

func TestAdvanceEventClampsConfidence(t *testing.T) {
	now := time.Now()
	e := &KnowledgeEntry{}
	for i := 0; i < 10; i++ {
		advanceEvent(e, now, []string{"持续更新"}, nil)
	}
	if e.Confidence > maxConfidence {
		t.Fatalf("confidence %.2f exceeds cap %.2f", e.Confidence, maxConfidence)
	}
	if e.UpdateCount != 10 {
		t.Fatalf("update count=%d want 10", e.UpdateCount)
	}
}

func TestHasConflictHints(t *testing.T) {
	if !hasConflictHints([]string{"官方否认该事件"}) {
		t.Fatal("否认 must be a conflict hint")
	}
	if !hasConflictHints([]string{"The government denies the report"}) {
		t.Fatal("denies must be a conflict hint")
	}
	if hasConflictHints([]string{"天气晴朗，适合出行"}) {
		t.Fatal("normal text must not trigger conflict")
	}
}

func TestLinkRelatedEvent(t *testing.T) {
	a := &KnowledgeEntry{Query: "美军航母抵达波斯湾"}
	b := &KnowledgeEntry{Query: "伊朗反应"}
	LinkRelatedEvent(a, b)
	if len(a.EventChain) != 1 || a.EventChain[0] != "伊朗反应" {
		t.Fatalf("a chain=%v", a.EventChain)
	}
	if len(b.EventChain) != 1 || b.EventChain[0] != "美军航母抵达波斯湾" {
		t.Fatalf("b chain=%v", b.EventChain)
	}
	// 幂等
	LinkRelatedEvent(a, b)
	if len(a.EventChain) != 1 {
		t.Fatalf("chain must dedupe, got %v", a.EventChain)
	}
}

func TestSummaryState(t *testing.T) {
	e := &KnowledgeEntry{UpdateCount: 3, Confidence: 0.8, ConflictDetected: true}
	u, c, f := e.SummaryState()
	if u != 3 || c != 0.8 || !f {
		t.Fatalf("state=(%d,%.2f,%v)", u, c, f)
	}
	// nil receiver is safe and reports zero state.
	var n *KnowledgeEntry
	if u, c, f := n.SummaryState(); u != 0 || c != 0 || f {
		t.Fatalf("nil state=(%d,%.2f,%v)", u, c, f)
	}
}

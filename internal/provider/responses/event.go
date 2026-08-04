package responses

import (
	"strings"
	"time"
)

// P4 信息流模型：把一次性的 web_search 结果升级为"动态事件追踪"。
// 对应设计文档 5.3 的情报模式：初始事件 → 增量更新 → 冲突信号 → 关联链接 → 置信度演化。

// Event confidence tuning.
const (
	// baseConfidence is a fresh event's starting confidence.
	baseConfidence = 0.6
	// confidenceStep is added per successful incremental update (cap 0.95).
	confidenceStep = 0.1
	// conflictPenalty is subtracted when a contradictory update lands.
	conflictPenalty = 0.3
	// maxConfidence caps accumulated confidence.
	maxConfidence = 0.95
)

// negationHints are phrases in a fresh update that contradict a prior claim.
// A lightweight heuristic for conflict detection — the intelligence-community
// analogue is "conflicting source reporting". Deliberately excludes generic
// negations ("不是/并未/并非" appear in innocuous sentences like "今天不是
// 周末") to avoid false positives; only explicit contradiction markers remain.
var negationHints = []string{
	"否认", "辟谣", "没有发生", "撤回",
	"denies", "denied", "contradicts", "refutes", "retracts", "no evidence",
}

// AdvanceEvent is the exported form of advanceEvent for external callers
// (e.g. cmd/websearch-smoke event-stream testing).
func AdvanceEvent(e *KnowledgeEntry, now time.Time, newFacts []string, conflictHints []string) {
	advanceEvent(e, now, newFacts, conflictHints)
}

// advanceEvent applies one incremental update: bumps the counter, detects
// conflict between the fresh facts and the stored ones, adjusts confidence,
// and refreshes the timestamp. newFacts carries the latest update's key
// facts; conflictHints may be precomputed by the caller, otherwise the
// heuristic scan over newFacts decides.
func advanceEvent(e *KnowledgeEntry, now time.Time, newFacts []string, conflictHints []string) {
	if e == nil {
		return
	}
	e.UpdateCount++
	if e.Confidence == 0 {
		e.Confidence = baseConfidence
	}
	conflict := len(conflictHints) > 0
	if !conflict {
		conflict = hasConflictHints(newFacts)
	}
	if conflict {
		e.ConflictDetected = true
		e.Confidence -= conflictPenalty
	} else {
		e.Confidence += confidenceStep
	}
	if e.Confidence > maxConfidence {
		e.Confidence = maxConfidence
	}
	if e.Confidence < 0 {
		e.Confidence = 0
	}
	e.LastUpdatedAt = now
	// A refresh extends freshness so the cycle can continue.
	if !e.ExpiresAt.IsZero() {
		e.ExpiresAt = now.Add(DefaultKnowledgeTTL)
	}
	if e.TimeSensitive && !e.FreshUntil.IsZero() {
		e.FreshUntil = now.Add(DefaultKnowledgeTTL / 2)
	}
}

// hasConflictHints scans text for contradiction markers.
func hasConflictHints(texts []string) bool {
	for _, t := range texts {
		lower := strings.ToLower(t)
		for _, h := range negationHints {
			if strings.Contains(lower, h) {
				return true
			}
		}
	}
	return false
}

// LinkRelatedEvent cross-links two entries so the event chain records the
// relationship (初始事件 → 关联事件). Deduplicates.
func LinkRelatedEvent(a, b *KnowledgeEntry) {
	if a == nil || b == nil {
		return
	}
	a.EventChain = appendUnique(a.EventChain, b.Query)
	b.EventChain = appendUnique(b.EventChain, a.Query)
}

func appendUnique(list []string, s string) []string {
	if s == "" {
		return list
	}
	for _, existing := range list {
		if existing == s {
			return list
		}
	}
	return append(list, s)
}

// SummaryState renders the P4 tracking state for logging/UI.
func (e *KnowledgeEntry) SummaryState() (updates int, confidence float64, conflict bool) {
	if e == nil {
		return 0, 0, false
	}
	return e.UpdateCount, e.Confidence, e.ConflictDetected
}

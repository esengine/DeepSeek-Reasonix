package responses

import "strings"

// RetrievalTier classifies how hard a query is to answer, routing it to the
// right executor (retrieval tier design, gate 2). Classification is a cheap
// heuristic on query shape (length + intent keywords) so it costs zero API
// tokens; an LLM pass can be layered on top later if precision needs to
// improve.
type RetrievalTier string

const (
	// TierSimple is a single factual query: weather, population, date,
	// definition. One web_search round suffices.
	TierSimple RetrievalTier = "simple"
	// TierGeneral combines several facts: comparisons, summaries, "list
	// the top N". A few web_search rounds with follow-ups.
	TierGeneral RetrievalTier = "general"
	// TierComplex needs multi-source cross-checking: policy impact, event
	// analysis, disputed claims. Orchestrated (architect-research), up to
	// five rounds.
	TierComplex RetrievalTier = "complex"
	// TierDeep is a research-level subject: full report, literature
	// review. Delegated to the deep-research subagent (L1/L2/L3), up to
	// twelve rounds (loop-engineer cap).
	TierDeep RetrievalTier = "deep"
	// TierDomain is locally-injected knowledge (a project/library distilled
	// into the cache, not a live web_search result). Tier encodes the
	// knowledge SOURCE: domain entries are pre-verified by the owner, so
	// they skip web freshness entirely and never trigger refresh.
	TierDomain RetrievalTier = "domain"
)

// maxRounds bounds web_search turns per tier. The agent layer owns the loop;
// these are the ceiling the executor may use.
func (t RetrievalTier) maxRounds() int {
	switch t {
	case TierGeneral:
		return 3
	case TierComplex:
		return 5
	case TierDeep:
		return 12
	default:
		return 1
	}
}

// MaxRounds is the exported form of maxRounds for external callers
// (e.g. cmd/websearch-smoke query entry).
func (t RetrievalTier) MaxRounds() int { return t.maxRounds() }

// deepHints are phrases that mark a research-level request even when short.
var deepHints = []string{
	"深度研究", "深度调研", "全面调研", "撰写报告", "研究报告", "文献综述",
	"详细分析", "系统梳理", "definitive guide", "deep research", "literature review",
}

// complexHints mark multi-source cross-checking intent.
var complexHints = []string{
	"对比", "比较", "影响", "原因分析", "争议", "不同观点", "交叉验证",
	"事件分析", "政策影响", "tradeoff", "compare", "impact", "controversy",
}

// generalHints mark multi-fact aggregation.
var generalHints = []string{
	"总结", "概述", "几个", "哪些", "清单", "列表", "最新进展", "排名",
	"top ", "top1", "top3", "top5", "top10", "top 10",
	"list ", "summary", "overview", "latest",
}

// ClassifyTier heuristically grades query difficulty. Long queries are
// treated as deep; intent keywords escalate general -> complex -> deep.
// Short, single-fact questions stay simple.
func ClassifyTier(query string) (RetrievalTier, int) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return TierSimple, 1
	}
	// Length is the strongest signal: a long question implies a broad
	// subject that one search round cannot cover.
	if len([]rune(q)) > 120 {
		return TierDeep, TierDeep.maxRounds()
	}
	has := func(hints []string) bool {
		for _, h := range hints {
			if strings.Contains(q, strings.ToLower(h)) {
				return true
			}
		}
		return false
	}
	if has(deepHints) {
		return TierDeep, TierDeep.maxRounds()
	}
	if has(complexHints) {
		return TierComplex, TierComplex.maxRounds()
	}
	if has(generalHints) {
		return TierGeneral, TierGeneral.maxRounds()
	}
	return TierSimple, TierSimple.maxRounds()
}

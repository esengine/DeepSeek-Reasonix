package responses

import (
	"fmt"
	"strings"
)

// 四维检索框架（deep-research Phase 3 代码化）：按 时间/来源/角度/深度
// 四维生成检索查询计划，深度越高级覆盖维度越多。L1 只查事实；L2 加
// 来源维度；L3 全四维。供 agent/检索层迭代执行。

// ResearchDepth mirrors deep-research 的 L1/L2/L3 深度分级。
type ResearchDepth string

const (
	DepthL1 ResearchDepth = "L1" // 快速事实查询
	DepthL2 ResearchDepth = "L2" // 多源交叉
	DepthL3 ResearchDepth = "L3" // 全维深度研究
)

// QueryAspect labels which dimension a planned query covers.
type QueryAspect string

const (
	AspectFact     QueryAspect = "fact"     // 深度维：发生了什么
	AspectCause    QueryAspect = "cause"    // 深度维：为什么
	AspectFuture   QueryAspect = "future"   // 深度维：会怎样
	AspectRisk     QueryAspect = "risk"     // 深度维：风险
	AspectOfficial QueryAspect = "official" // 来源维：官方公告
	AspectMedia    QueryAspect = "media"    // 来源维：媒体报道
	AspectData     QueryAspect = "data"     // 来源维：数据报告
	AspectRecent   QueryAspect = "recent"   // 时间维：最新动态
	AspectHistory  QueryAspect = "history"  // 时间维：历史背景
)

// PlannedQuery is one concrete search from the four-dimension plan.
type PlannedQuery struct {
	Query  string
	Aspect QueryAspect
	// Priority 0 (highest) sorts execution order: facts first, then
	// context/verification.
	Priority int
}

// RetrievalPlan is the full four-dimension search plan for a topic.
type RetrievalPlan struct {
	Topic    string
	Depth    ResearchDepth
	Queries  []PlannedQuery
	Coverage map[QueryAspect]bool // which dimensions are covered
}

// timeTemplates / sourceTemplates / angleTemplates / depthTemplates encode
// the four-dimension search templates from deep-research Phase 3.
var (
	depthTemplates = map[QueryAspect]string{
		AspectFact:   "%s",          // 发生了什么
		AspectCause:  "%s 原因 为什么",   // 为什么发生
		AspectFuture: "%s 未来 趋势 预测", // 会怎样发展
		AspectRisk:   "%s 风险 问题 挑战", // 潜在风险
	}
	sourceTemplates = map[QueryAspect]string{
		AspectOfficial: "%s 官方 公告",
		AspectMedia:    "%s 媒体 报道",
		AspectData:     "%s 数据 报告 统计",
	}
	timeTemplates = map[QueryAspect]string{
		AspectRecent:  "%s 最新 动态",
		AspectHistory: "%s 历史 背景 起源",
	}
)

// PlanResearch generates the four-dimension retrieval plan for a topic.
// 维度覆盖：
//
//	L1: 事实（发生了什么）
//	L2: 事实 + 原因 + 官方/媒体来源
//	L3: 全维度（时间 2 + 来源 3 + 深度 4 = 9 个查询）
func PlanResearch(topic string, depth ResearchDepth) RetrievalPlan {
	plan := RetrievalPlan{
		Topic:    topic,
		Depth:    depth,
		Coverage: map[QueryAspect]bool{},
	}
	add := func(aspect QueryAspect, tmpl string, priority int) {
		plan.Queries = append(plan.Queries, PlannedQuery{
			Query:    fmt.Sprintf(tmpl, topic),
			Aspect:   aspect,
			Priority: priority,
		})
		plan.Coverage[aspect] = true
	}

	// 深度维——事实永远第一优先
	add(AspectFact, depthTemplates[AspectFact], 0)
	switch depth {
	case DepthL1:
		// 只查事实
	case DepthL2:
		add(AspectCause, depthTemplates[AspectCause], 1)
		add(AspectOfficial, sourceTemplates[AspectOfficial], 1)
		add(AspectMedia, sourceTemplates[AspectMedia], 2)
	case DepthL3:
		add(AspectCause, depthTemplates[AspectCause], 1)
		add(AspectFuture, depthTemplates[AspectFuture], 2)
		add(AspectRisk, depthTemplates[AspectRisk], 2)
		add(AspectOfficial, sourceTemplates[AspectOfficial], 1)
		add(AspectMedia, sourceTemplates[AspectMedia], 2)
		add(AspectData, sourceTemplates[AspectData], 2)
		add(AspectRecent, timeTemplates[AspectRecent], 3)
		add(AspectHistory, timeTemplates[AspectHistory], 3)
	}
	return plan
}

// PlanToTier maps a research depth to the retrieval difficulty tier used by
// ClassifyTier (deep research delegates to the complex/deep executor).
func PlanToTier(d ResearchDepth) RetrievalTier {
	switch d {
	case DepthL3:
		return TierDeep
	case DepthL2:
		return TierComplex
	default:
		return TierSimple
	}
}

// NeedClarification reports whether a topic needs range-alignment before deep
// research (deep-research Phase 1 反问确认代码化): L3 topics and ambiguous
// questions should be confirmed with the user (scope, time window, focus)
// before spending rounds on a wrong direction.
func NeedClarification(topic string, depth ResearchDepth) bool {
	if depth == DepthL3 {
		return true
	}
	// 宽泛话题（无明确对象/时间范围）需要澄清
	t := strings.TrimSpace(topic)
	if len([]rune(t)) < 4 {
		return true
	}
	for _, vague := range []string{"这个", "那个", "相关", "一些", "某个", "问题"} {
		if strings.Contains(t, vague) {
			return true
		}
	}
	return false
}

// ClarificationQuestion builds the ask-tool question for range alignment.
func ClarificationQuestion(topic string, depth ResearchDepth) string {
	switch depth {
	case DepthL3:
		return "这是一个 L3 深度研究课题。请确认研究范围：1) 时间窗口（最新/近一年/历史全貌）？2) 重点关注维度（事实/原因/趋势/风险）？3) 是否有地域或领域限定？"
	default:
		return "研究范围较模糊，请补充：具体想了解什么？时间范围？关注点？"
	}
}

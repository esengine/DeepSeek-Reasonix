package responses

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// orchestrator.go：信息事件模型五步编排（2026-08-03 用户要求的完整流程）：
//
//	① 大模型语义拆解命题（DecomposeProposition）
//	② 多因果线分析（每子命题 → CausalChain）
//	③ 按子命题分配信息检索任务（BuildFleetRetrievalTasks）
//	④ 信息流（P4 event 增量追踪）
//	⑤ 信息帧拼图（MergeFrames）

// InfoEventModel is the assembled result of the five-step pipeline.
type InfoEventModel struct {
	Topic        string
	Propositions []Proposition   // ① 拆解出的子命题
	Chains       []CausalChain   // ② 每子命题一条因果线
	FleetTasks   []FleetTaskSpec // ③ 并行检索任务
	EventMain    *KnowledgeEntry // ④ 信息流主事件
	View         FrameView       // ⑤ 信息帧拼图
}

// ModelExecutor wires the pipeline pieces. Callers inject real fetch/decompose.
type ModelExecutor struct {
	// Decompose splits the topic into sub-propositions.
	Decompose DecomposeFunc
	// Fetch performs one web retrieval for a query.
	Fetch FetchFunc
	// Policy gates web access (nil = local-only).
	Policy *RetrievalPolicy
}

// Execute runs the five-step information event model.
func (ex *ModelExecutor) Execute(topic string) (*InfoEventModel, error) {
	m := &InfoEventModel{Topic: topic}

	// ① 语义拆解命题
	props, err := DecomposeProposition(topic, ex.Decompose)
	if err != nil {
		// 拆解失败退化为单命题（原命题整体检索）
		props = []Proposition{{Title: topic, Query: topic}}
	}
	m.Propositions = props

	// ② 多因果线 + ③ 检索任务分配 + ④ 信息流
	now := time.Now()
	mainEntry := NewKnowledgeEntry(topic) // 信息流主事件
	for _, p := range props {
		// ② 每子命题一条因果线（骨架：信号占位，检索后补事实）
		chain := NewCausalChain(p.Title)
		chain.Add(CausalEvent{Stage: StageSignal, Time: now.Format("2006-01-02"), Detail: "子命题: " + p.Title})
		m.Chains = append(m.Chains, *chain)

		// ③ 按子命题分配检索任务
		m.FleetTasks = append(m.FleetTasks, ex.fleetTaskFor(p))

		// ④ 信息流：检索子命题 → 增量合入主事件
		if ex.Fetch != nil {
			entry, err := ex.Fetch(context.Background(), p.Query, TierComplex)
			if err == nil && entry != nil {
				entry.Query = p.Query
				LinkRelatedEvent(mainEntry, entry)
				advanceEvent(mainEntry, now, entry.KeyFacts, nil)
			}
		}
	}
	m.EventMain = mainEntry

	// ⑤ 信息帧拼图（用主事件事实 + 因果线渲染）
	mainFacts := mainEntry.KeyFacts
	if len(mainFacts) == 0 {
		mainFacts = []string{"命题已拆解为 " + fmt.Sprint(len(props)) + " 个子命题，分别检索"}
	}
	frame := NewInfoFrame(DomainGeneral, "", "zh", topic)
	frame.Facts = mainFacts
	m.View = MergeFrames(topic, []*InfoFrame{frame})
	return m, nil
}

func (ex *ModelExecutor) fleetTaskFor(p Proposition) FleetTaskSpec {
	aspects := strings.Join(p.Aspects, "、")
	if aspects == "" {
		aspects = "信号/影响/后果"
	}
	// AI 驱动：场景提示词 + 语言后缀由 LLM 拆解时判定（非程序固定）
	scopedQuery := SceneQuery(p.Query, p.Scene, p.Language)
	lang := p.Language
	if lang == "" {
		lang = "zh"
	}
	return FleetTaskSpec{
		Prompt: retrievalPromptBase() + fmt.Sprintf(
			"本次任务：检索子命题「%s」：%s。\n关注维度: %s。\n"+
				"用 web_search 检索后返回 InfoFrame JSON："+
				`{"domain":"%s","language":"%s","topic":"%s","facts":["..."],"sources":[{"title":"","url":""}],"confidence":0.0}`,
			p.Title, scopedQuery, aspects, p.Scene, lang, p.Title),
		Description: "检索子命题: " + p.Title + " (" + string(p.Scene) + "/" + lang + ")",
		ReadOnly:    true,
		Tools:       []string{"web_search", "retrieve_info"},
		MaxSteps:    5,
	}
}

// NewKnowledgeEntry builds a bare event-tracked entry.
func NewKnowledgeEntry(topic string) *KnowledgeEntry {
	return &KnowledgeEntry{Query: topic, TimeSensitive: true}
}

// Report renders the full model as markdown (拆解→因果线→任务→帧拼图).
func (m *InfoEventModel) Report() string {
	var b strings.Builder
	b.WriteString("# " + m.Topic + " — 信息事件模型\n\n")

	fmt.Fprintf(&b, "## ① 命题拆解（%d 子命题）\n\n", len(m.Propositions))
	for i, p := range m.Propositions {
		fmt.Fprintf(&b, "%d. **%s** — %s\n", i+1, p.Title, p.Query)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## ② 多因果线（%d 条）\n\n", len(m.Chains))
	for _, c := range m.Chains {
		b.WriteString("- " + c.Topic + "\n")
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## ③ 并行检索任务（%d 个）\n\n", len(m.FleetTasks))
	for i, t := range m.FleetTasks {
		fmt.Fprintf(&b, "%d. %s\n", i+1, t.Description)
	}
	b.WriteString("\n")

	if m.EventMain != nil {
		fmt.Fprintf(&b, "## ④ 信息流（更新 %d 次，置信度 %.2f）\n\n",
			m.EventMain.UpdateCount, m.EventMain.Confidence)
	}
	b.WriteString("\n## ⑤ 信息帧拼图\n\n")
	b.WriteString(m.View.Render())
	return b.String()
}

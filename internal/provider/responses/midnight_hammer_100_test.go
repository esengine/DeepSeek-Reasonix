package responses

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ========================================================================
// 案例一：2025年6月"午夜之锤"（Operation Midnight Hammer）
// 航班追踪史上最完整案例 —— 100 轮检索可靠性测试
//
// 背景：美伊冲突情报追踪。通过开源航班追踪数据（ADS-B / FlightRadar24），
// 观测到 KC-46 加油机大规模东调 → E-4B "末日飞机"出动 → B-2 隐形轰炸机
// 在印度洋 Diego Garcia 集结 → 6月22日夜间打击。信号→行动预警窗口 7 天。
//
// 本测试覆盖 10 个维度 × 10 轮，合计 100 轮，验证检索系统在情报级场景
// 下的可靠性、一致性、故障恢复和性能边界。
// ========================================================================

// =========================================================================
// 维度 1：因果链构造与阶段排序（10 轮）
// =========================================================================
func TestMidnightHammer_Dim1_CausalChain(t *testing.T) {
	t.Log("=== 维度1：因果链构造（信号→推断→行动→后果）===")

	// 第 1 轮：基础四阶段链
	t.Run("R01_基础链_午夜之锤", func(t *testing.T) {
		c := NewCausalChain("午夜之锤")
		c.Add(CausalEvent{Stage: StageSignal, Time: "6月15日", Detail: "KC-46加油机20架东调", Signal: "KC-46"})
		c.Add(CausalEvent{Stage: StageInference, Time: "6月17日", Detail: "E-4B出动→推断战备升级"})
		c.Add(CausalEvent{Stage: StageAction, Time: "6月22日", Detail: "B-2隐形轰炸机打击"})
		c.Add(CausalEvent{Stage: StageConsequence, Time: "6月22日 02:10", Detail: "成功突袭地下设施"})
		if len(c.Events) != 4 {
			t.Fatalf("事件数=%d want 4", len(c.Events))
		}
		stages := []CausalStage{}
		for _, e := range c.Events {
			stages = append(stages, e.Stage)
		}
		want := []CausalStage{StageSignal, StageInference, StageAction, StageConsequence}
		for i := range want {
			if stages[i] != want[i] {
				t.Fatalf("阶段[%d]=%s want %s", i, stages[i], want[i])
			}
		}
	})

	// 第 2 轮：预警窗口计算
	t.Run("R02_预警窗口_7天", func(t *testing.T) {
		c := NewCausalChain("午夜之锤")
		c.Add(CausalEvent{Stage: StageSignal, Time: "6月15日", Detail: "加油机东调"})
		c.Add(CausalEvent{Stage: StageAction, Time: "6月22日", Detail: "B-2打击"})
		win := c.ComputeWarningWindow()
		if win != 7*24*time.Hour {
			t.Fatalf("预警窗口=%v want 168h", win)
		}
	})

	// 第 3 轮：乱序插入自动排序
	t.Run("R03_乱序插入自动排序", func(t *testing.T) {
		c := NewCausalChain("乱序测试")
		c.Add(CausalEvent{Stage: StageConsequence, Time: "6月22日", Detail: "后果"})
		c.Add(CausalEvent{Stage: StageSignal, Time: "6月15日", Detail: "信号"})
		c.Add(CausalEvent{Stage: StageAction, Time: "6月22日", Detail: "行动"})
		c.Add(CausalEvent{Stage: StageInference, Time: "6月17日", Detail: "推断"})
		want := []CausalStage{StageSignal, StageInference, StageAction, StageConsequence}
		for i, e := range c.Events {
			if e.Stage != want[i] {
				t.Fatalf("乱序后[%d]=%s want %s", i, e.Stage, want[i])
			}
		}
	})

	// 第 4 轮：渲染输出含关键字段
	t.Run("R04_渲染输出完整性", func(t *testing.T) {
		c := NewCausalChain("午夜之锤")
		c.Add(CausalEvent{Stage: StageSignal, Time: "6月15日", Detail: "KC-46加油机20架东调"})
		c.Add(CausalEvent{Stage: StageAction, Time: "6月22日", Detail: "B-2打击"})
		c.Confidence = 0.85
		c.Sources = []string{"https://twz.com/report", "https://adsbexchange.com/data"}
		md := c.Render()
		for _, want := range []string{"午夜之锤", "信号", "行动", "KC-46", "B-2", "置信度", "来源", "twz.com"} {
			if !strings.Contains(md, want) {
				t.Fatalf("渲染必须包含 %q", want)
			}
		}
	})

	// 第 5 轮：多信号源聚合
	t.Run("R05_多信号源聚合", func(t *testing.T) {
		c := NewCausalChain("多信号追踪")
		c.Add(CausalEvent{Stage: StageSignal, Time: "6月14日", Detail: "RC-135侦察机增加架次", Signal: "RC-135"})
		c.Add(CausalEvent{Stage: StageSignal, Time: "6月15日", Detail: "KC-46加油机20架东调", Signal: "KC-46"})
		c.Add(CausalEvent{Stage: StageSignal, Time: "6月16日", Detail: "C-17运输机频繁起降", Signal: "C-17"})
		c.Add(CausalEvent{Stage: StageAction, Time: "6月22日", Detail: "B-2打击"})
		sigCount := 0
		for _, e := range c.Events {
			if e.Stage == StageSignal {
				sigCount++
			}
		}
		if sigCount != 3 {
			t.Fatalf("信号数=%d want 3", sigCount)
		}
		win := c.ComputeWarningWindow()
		if win < 6*24*time.Hour {
			t.Fatalf("多信号预警窗口=%v 太短", win)
		}
	})

	// 第 6 轮：空链处理
	t.Run("R06_空链安全处理", func(t *testing.T) {
		c := NewCausalChain("空")
		if len(c.Events) != 0 {
			t.Fatal("空链应无事件")
		}
		if c.ComputeWarningWindow() != 0 {
			t.Fatal("空链预警窗口=0")
		}
		md := c.Render()
		if !strings.Contains(md, "空") {
			t.Fatal("空链渲染至少含主题")
		}
	})

	// 第 7 轮：nil 链安全
	t.Run("R07_nil链安全", func(t *testing.T) {
		var c *CausalChain
		if c.Render() != "" {
			t.Fatal("nil 渲染必须为空")
		}
		if c.ComputeWarningWindow() != 0 {
			t.Fatal("nil 窗口必须为 0")
		}
		c.Add(CausalEvent{Stage: StageSignal, Time: "test", Detail: "x"})
	})

	// 第 8 轮：时间解析（中文格式）
	t.Run("R08_中文日期解析", func(t *testing.T) {
		tests := []struct{ in, want string }{
			{"6月14日", "06-14"},
			{"12月31日", "12-31"},
			{"2025-06-17", "06-17"},
			{"2025/06/17", "06-17"},
		}
		for _, tc := range tests {
			got, err := parseChainTime(tc.in)
			if err != nil {
				t.Fatalf("解析 %q 失败: %v", tc.in, err)
			}
			if got.Format("01-02") != tc.want {
				t.Fatalf("%q → %s, want %s", tc.in, got.Format("01-02"), tc.want)
			}
		}
	})

	// 第 9 轮：时间解析（非法格式）
	t.Run("R09_非法日期拒绝", func(t *testing.T) {
		for _, bad := range []string{"", "abc", "13月1日", "0月0日", "完全不是时间"} {
			if _, err := parseChainTime(bad); err == nil {
				t.Fatalf("%q 应解析失败", bad)
			}
		}
	})

	// 第 10 轮：FromEventStream 构造
	t.Run("R10_FromEventStream构造", func(t *testing.T) {
		e := &KnowledgeEntry{
			KeyFacts:   []string{"KC-46东调", "E-4B出动", "B-2集结"},
			Confidence: 0.75,
			Sources:    []Source{{URL: "https://adsbexchange.com"}},
			EventChain: []string{"美伊冲突升级", "联合国安理会紧急会议"},
			CreatedAt:  time.Date(2025, 6, 15, 0, 0, 0, 0, time.Local),
		}
		c := FromEventStream("午夜之锤", e)
		if c == nil {
			t.Fatal("FromEventStream 不应返回 nil")
		}
		if c.Confidence != 0.75 {
			t.Fatalf("置信度=%.2f want 0.75", c.Confidence)
		}
		sigCount := 0
		for _, ev := range c.Events {
			if ev.Stage == StageSignal {
				sigCount++
			}
		}
		if sigCount != 3 {
			t.Fatalf("信号事件数=%d want 3", sigCount)
		}
	})
}

// =========================================================================
// 维度 2：L1 精确缓存命中（10 轮）
// =========================================================================
func TestMidnightHammer_Dim2_L1CacheHit(t *testing.T) {
	cleanKnowledgeCacheMH(t)
	t.Log("=== 维度2：L1精确缓存命中 ===")

	// 第 11 轮：基础存取
	t.Run("R11_基础存取", func(t *testing.T) {
		q := "午夜之锤行动 B-2轰炸机 2025年6月"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "B-2打击地下设施"})
		defer cleanupEntryMH(t, q)
		e, hit := LoadKnowledge(q)
		if !hit {
			t.Fatal("刚保存的条目必须命中")
		}
		if e.AnswerSummary != "B-2打击地下设施" {
			t.Fatalf("摘要=%q", e.AnswerSummary)
		}
	})

	// 第 12 轮：SHA-256 键确定性
	t.Run("R12_SHA256键确定性", func(t *testing.T) {
		q := "午夜之锤"
		h1 := KnowledgeHash(q)
		h2 := KnowledgeHash(q)
		if h1 != h2 {
			t.Fatal("相同查询必须产生相同哈希")
		}
		if len(h1) != 64 {
			t.Fatalf("SHA-256 应为 64 字符, got %d", len(h1))
		}
	})

	// 第 13 轮：大小写敏感
	t.Run("R13_大小写敏感", func(t *testing.T) {
		q1 := "Operation Midnight Hammer"
		q2 := "operation midnight hammer"
		SaveKnowledge(&KnowledgeEntry{Query: q1, AnswerSummary: "大写"})
		defer cleanupEntryMH(t, q1)
		_, hit := LoadKnowledge(q2)
		if hit {
			t.Fatal("大小写不同不应命中L1")
		}
	})

	// 第 14 轮：过期删除
	t.Run("R14_过期自动删除", func(t *testing.T) {
		q := "过期测试_午夜之锤"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "过期",
			CreatedAt: time.Now().Add(-8 * 24 * time.Hour),
			ExpiresAt: time.Now().Add(-time.Hour)})
		_, hit := LoadKnowledge(q)
		if hit {
			t.Fatal("过期条目必须miss+删除")
		}
	})

	// 第 15 轮：空查询
	t.Run("R15_空查询", func(t *testing.T) {
		_, hit := LoadKnowledge("")
		if hit {
			t.Fatal("空查询不应命中")
		}
	})

	// 第 16 轮：特殊字符查询
	t.Run("R16_特殊字符查询", func(t *testing.T) {
		q := "午夜之锤 🔨 B-2 \"幽灵\" 2025/06"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "特殊字符"})
		defer cleanupEntryMH(t, q)
		e, hit := LoadKnowledge(q)
		if !hit || e.AnswerSummary != "特殊字符" {
			t.Fatalf("特殊字符存取失败: hit=%v", hit)
		}
	})

	// 第 17 轮：超长查询
	t.Run("R17_超长查询", func(t *testing.T) {
		q := strings.Repeat("午夜之锤航班追踪情报分析开源信息 ", 20)
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "超长"})
		defer cleanupEntryMH(t, q)
		_, hit := LoadKnowledge(q)
		if !hit {
			t.Fatal("超长查询必须命中")
		}
	})

	// 第 18 轮：QueryHash 覆盖（安全）
	t.Run("R18_QueryHash覆盖防御", func(t *testing.T) {
		q := "安全查询"
		e := &KnowledgeEntry{Query: q, AnswerSummary: "安全", QueryHash: "../../../etc/passwd"}
		SaveKnowledge(e)
		defer cleanupEntryMH(t, q)
		e2, hit := LoadKnowledge(q)
		if !hit {
			t.Fatal("恶意 QueryHash 应被忽略，以 Query 重新派生键")
		}
		if e2.QueryHash == "../../../etc/passwd" {
			t.Fatal("QueryHash 必须被覆盖为安全派生值")
		}
	})

	// 第 19 轮：并发存取安全
	t.Run("R19_并发存取", func(t *testing.T) {
		qs := make([]string, 10)
		for i := 0; i < 10; i++ {
			qs[i] = fmt.Sprintf("并发测试_%d", i)
			SaveKnowledge(&KnowledgeEntry{Query: qs[i], AnswerSummary: fmt.Sprintf("ans-%d", i)})
		}
		defer func() {
			for _, q := range qs {
				cleanupEntryMH(t, q)
			}
		}()
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func(id int) {
				_, hit := LoadKnowledge(fmt.Sprintf("并发测试_%d", id))
				if !hit {
					t.Errorf("并发[%d]未命中", id)
				}
				done <- true
			}(i)
		}
		for i := 0; i < 10; i++ {
			<-done
		}
	})

	// 第 20 轮：ListKnowledge 全量列出
	t.Run("R20_全量列出", func(t *testing.T) {
		q := "列表测试_午夜之锤"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "列表"})
		defer cleanupEntryMH(t, q)
		all := ListKnowledge()
		found := false
		for _, e := range all {
			if e.Query == q {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("ListKnowledge 必须包含已保存条目")
		}
	})
}

// =========================================================================
// 维度 3：L2 语义相似命中（10 轮）
// =========================================================================
func TestMidnightHammer_Dim3_L2SemanticHit(t *testing.T) {
	cleanKnowledgeCacheMH(t)
	t.Log("=== 维度3：L2语义相似命中 ===")

	// 预置条目
	qBase := "2025年6月午夜之锤行动 B-2轰炸机打击伊朗地下设施"
	SaveKnowledge(&KnowledgeEntry{Query: qBase, AnswerSummary: "B-2打击详情"})
	defer cleanupEntryMH(t, qBase)

	// 第 21 轮：近义改写命中
	t.Run("R21_近义改写命中", func(t *testing.T) {
		q := "2025年6月 Operation Midnight Hammer B-2 轰炸"
		e, sim, hit := LoadKnowledgeSemantic(q, DefaultSemanticThreshold)
		if !hit {
			t.Fatalf("近义改写必须命中: sim=%.3f", sim)
		}
		_ = e
		if sim < DefaultSemanticThreshold {
			t.Fatalf("相似度=%.3f < 阈值=%.2f", sim, DefaultSemanticThreshold)
		}
	})

	// 第 22 轮：词序调换
	t.Run("R22_词序调换", func(t *testing.T) {
		q := "B-2轰炸机 午夜之锤行动 2025年6月 伊朗设施"
		_, sim, hit := LoadKnowledgeSemantic(q, DefaultSemanticThreshold)
		if !hit {
			t.Fatalf("词序调换必须命中: sim=%.3f", sim)
		}
	})

	// 第 23 轮：缩写/全称
	t.Run("R23_缩写全称", func(t *testing.T) {
		q := "B2 打击 Iran 地下 设施 Midnight Hammer"
		_, sim, hit := LoadKnowledgeSemantic(q, DefaultSemanticThreshold)
		if !hit {
			t.Fatalf("缩写/全称混用必须命中: sim=%.3f", sim)
		}
	})

	// 第 24 轮：不相关查询不应命中
	t.Run("R24_不相关查询", func(t *testing.T) {
		q := "北京今天天气怎么样"
		_, sim, hit := LoadKnowledgeSemantic(q, DefaultSemanticThreshold)
		if hit {
			t.Fatalf("不相关查询不应命中: sim=%.3f", sim)
		}
		_ = sim
	})

	// 第 25 轮：部分重叠（低于阈值）
	t.Run("R25_部分重叠低于阈值", func(t *testing.T) {
		q := "2025年6月 军事行动"
		_, _, hit := LoadKnowledgeSemantic(q, 0.6)
		if hit {
			t.Fatal("高阈值下部分重叠不应命中")
		}
	})

	// 第 26 轮：相似度计算确定性
	t.Run("R26_相似度确定性", func(t *testing.T) {
		a := "午夜之锤 B-2 轰炸"
		b := "午夜之锤 B-2 打击"
		s1 := NgramSimilarity(a, b)
		s2 := NgramSimilarity(a, b)
		if s1 != s2 {
			t.Fatal("Dice系数必须确定性")
		}
		if s1 <= 0 {
			t.Fatal("相似查询应有正相似度")
		}
	})

	// 第 27 轮：完全不同
	t.Run("R27_完全不同零相似度", func(t *testing.T) {
		s := NgramSimilarity("abc", "xyz")
		if s != 0 {
			t.Fatalf("完全不同应有零相似度, got %.3f", s)
		}
	})

	// 第 28 轮：完全相同
	t.Run("R28_完全相同满相似度", func(t *testing.T) {
		s := NgramSimilarity("午夜之锤", "午夜之锤")
		if s != 1.0 {
			t.Fatalf("完全相同应为1.0, got %.3f", s)
		}
	})

	// 第 29 轮：空字符串
	t.Run("R29_空字符串", func(t *testing.T) {
		s := NgramSimilarity("", "午夜之锤")
		if s != 0 {
			t.Fatal("空字符串相似度必须为0")
		}
	})

	// 第 30 轮：标点忽略
	t.Run("R30_标点忽略", func(t *testing.T) {
		a := "午夜之锤！B-2轰炸"
		b := "午夜之锤 B-2 轰炸"
		s := NgramSimilarity(a, b)
		if s < 0.8 {
			t.Fatalf("标点差异不应大幅影响相似度: %.3f", s)
		}
	})
}

// =========================================================================
// 维度 4：事件追踪与置信度演化（10 轮）
// =========================================================================
func TestMidnightHammer_Dim4_EventTracking(t *testing.T) {
	t.Log("=== 维度4：事件追踪与置信度演化 ===")

	// 第 31 轮：初始置信度
	t.Run("R31_初始置信度", func(t *testing.T) {
		now := time.Now()
		e := &KnowledgeEntry{TimeSensitive: true, FreshUntil: now.Add(-time.Hour)}
		advanceEvent(e, now, []string{"KC-46加油机东调至Diego Garcia"}, nil)
		if e.Confidence < baseConfidence || e.Confidence > baseConfidence+confidenceStep {
			t.Fatalf("初始置信度=%.2f 应在[%.2f,%.2f]", e.Confidence, baseConfidence, baseConfidence+confidenceStep)
		}
		if e.UpdateCount != 1 {
			t.Fatalf("更新计数=%d want 1", e.UpdateCount)
		}
	})

	// 第 32 轮：多次增量更新置信度上升
	t.Run("R32_多次增量置信度上升", func(t *testing.T) {
		now := time.Now()
		e := &KnowledgeEntry{TimeSensitive: true}
		for i := 0; i < 5; i++ {
			advanceEvent(e, now, []string{fmt.Sprintf("确认%d: 情报一致", i+1)}, nil)
		}
		if e.UpdateCount != 5 {
			t.Fatalf("更新计数=%d want 5", e.UpdateCount)
		}
		if e.Confidence <= baseConfidence {
			t.Fatalf("5次确认后置信度=%.2f 应 > %.2f", e.Confidence, baseConfidence)
		}
		if e.ConflictDetected {
			t.Fatal("无冲突不应标记冲突")
		}
	})

	// 第 33 轮：冲突信号导致置信度下降
	t.Run("R33_冲突信号置信度下降", func(t *testing.T) {
		now := time.Now()
		e := &KnowledgeEntry{TimeSensitive: true}
		advanceEvent(e, now, []string{"B-2已抵达Diego Garcia"}, nil)
		preConf := e.Confidence
		advanceEvent(e, now, []string{"伊朗否认B-2已抵达"}, nil)
		if e.Confidence >= preConf {
			t.Fatalf("冲突后置信度=%.2f 应 < %.2f", e.Confidence, preConf)
		}
		if !e.ConflictDetected {
			t.Fatal("必须检测到冲突")
		}
	})

	// 第 34 轮：置信度上限
	t.Run("R34_置信度上限0.95", func(t *testing.T) {
		now := time.Now()
		e := &KnowledgeEntry{TimeSensitive: true}
		for i := 0; i < 20; i++ {
			advanceEvent(e, now, []string{"持续确认"}, nil)
		}
		if e.Confidence > maxConfidence {
			t.Fatalf("置信度=%.2f 不能超过 %.2f", e.Confidence, maxConfidence)
		}
	})

	// 第 35 轮：置信度下限0
	t.Run("R35_置信度下限0", func(t *testing.T) {
		now := time.Now()
		e := &KnowledgeEntry{TimeSensitive: true}
		for i := 0; i < 10; i++ {
			advanceEvent(e, now, []string{"否认所有指控"}, nil)
		}
		if e.Confidence < 0 {
			t.Fatalf("置信度=%.2f 不能为负", e.Confidence)
		}
	})

	// 第 36 轮：时效刷新
	t.Run("R36_时效刷新", func(t *testing.T) {
		now := time.Now()
		e := &KnowledgeEntry{TimeSensitive: true, FreshUntil: now.Add(-time.Hour)}
		advanceEvent(e, now, []string{"新情报"}, nil)
		if e.FreshUntil.Before(now) {
			t.Fatal("更新后 FreshUntil 必须前移")
		}
	})

	// 第 37 轮：事件关联
	t.Run("R37_事件关联链接", func(t *testing.T) {
		a := &KnowledgeEntry{Query: "午夜之锤初始事件"}
		b := &KnowledgeEntry{Query: "伊朗反应"}
		c := &KnowledgeEntry{Query: "国际社会反应"}
		LinkRelatedEvent(a, b)
		LinkRelatedEvent(a, c)
		if len(a.EventChain) != 2 {
			t.Fatalf("a 事件链长度=%d want 2", len(a.EventChain))
		}
		if len(b.EventChain) != 1 {
			t.Fatalf("b 事件链长度=%d want 1", len(b.EventChain))
		}
	})

	// 第 38 轮：事件关联幂等
	t.Run("R38_事件关联幂等", func(t *testing.T) {
		a := &KnowledgeEntry{Query: "测试A"}
		b := &KnowledgeEntry{Query: "测试B"}
		for i := 0; i < 5; i++ {
			LinkRelatedEvent(a, b)
		}
		if len(a.EventChain) != 1 {
			t.Fatalf("重复关联必须去重: got %d", len(a.EventChain))
		}
	})

	// 第 39 轮：SummaryState
	t.Run("R39_SummaryState汇总", func(t *testing.T) {
		e := &KnowledgeEntry{UpdateCount: 7, Confidence: 0.82, ConflictDetected: false}
		u, c, f := e.SummaryState()
		if u != 7 || c != 0.82 || f {
			t.Fatalf("SummaryState=(%d,%.2f,%v) want (7,0.82,false)", u, c, f)
		}
	})

	// 第 40 轮：nil SummaryState 安全
	t.Run("R40_nilSummaryState安全", func(t *testing.T) {
		var e *KnowledgeEntry
		u, c, f := e.SummaryState()
		if u != 0 || c != 0 || f {
			t.Fatal("nil 必须返回零值")
		}
	})
}

// =========================================================================
// 维度 5：质量过滤与来源评分（10 轮）
// =========================================================================
func TestMidnightHammer_Dim5_QualityFiltering(t *testing.T) {
	cleanKnowledgeCacheMH(t)
	t.Log("=== 维度5：质量过滤与来源评分 ===")

	// 第 41 轮：白名单来源高分
	t.Run("R41_白名单来源", func(t *testing.T) {
		srcs := []Source{
			{URL: "https://reuters.com/article/iran-strike"},
			{URL: "https://apnews.com/article/b2-bomber"},
		}
		ScoreAndTagSources(&KnowledgeEntry{Sources: srcs})
		for _, s := range srcs {
			if s.Credibility < 0.7 {
				t.Fatalf("%s 可信度=%.2f 应≥0.7（白名单）", s.Domain, s.Credibility)
			}
		}
	})

	// 第 42 轮：垃圾来源低分/剔除
	t.Run("R42_垃圾来源剔除", func(t *testing.T) {
		srcs := []Source{
			{URL: "https://reuters.com/valid"},
			{URL: "https://spam-blog.net/?utm_source=ad&ref=clickbait"},
		}
		e := &KnowledgeEntry{Sources: srcs}
		ScoreAndTagSources(e)
		filtered := FilterSources(e.Sources, 0.5)
		if len(filtered) != 1 {
			t.Fatalf("过滤后应剩1个来源, got %d", len(filtered))
		}
		if filtered[0].Domain != "reuters.com" {
			t.Fatalf("保留的应为reuters.com, got %s", filtered[0].Domain)
		}
	})

	// 第 43 轮：gov/edu 权威加分
	t.Run("R43_权威域加分", func(t *testing.T) {
		srcs := []Source{
			{URL: "https://www.nasa.gov/press-release"},
		}
		ScoreAndTagSources(&KnowledgeEntry{Sources: srcs})
		// nasa.gov 在白名单(+0.40) + .gov权威(+0.20) = 0.60, 单源无交叉验证
		if srcs[0].Credibility < 0.55 {
			t.Fatalf("nasa.gov 可信度=%.2f 应≥0.55", srcs[0].Credibility)
		}
	})

	// 第 44 轮：域名提取
	t.Run("R44_域名提取", func(t *testing.T) {
		tests := []struct{ url, want string }{
			{"https://www.reuters.com/world/article", "reuters.com"},
			{"https://adsbexchange.com/data?icao=AE4F14", "adsbexchange.com"},
			{"http://sub.domain.co.uk/path", "sub.domain.co.uk"},
		}
		for _, tc := range tests {
			got := domainOf(tc.url)
			if got != tc.want {
				t.Fatalf("域名(%q)=%q want %q", tc.url, got, tc.want)
			}
		}
	})

	// 第 45 轮：空来源
	t.Run("R45_空来源安全", func(t *testing.T) {
		// FilterSources(minScore<=0) 会默认 minScore=0.5，返回空切片（非nil）
		filtered := FilterSources(nil, 0.5)
		if len(filtered) != 0 {
			t.Fatalf("nil 源过滤应为空切片, got %d", len(filtered))
		}
		filtered = FilterSources([]Source{}, 0.5)
		if len(filtered) != 0 {
			t.Fatalf("空源列表过滤应为空, got %d", len(filtered))
		}
	})

	// 第 46 轮：minScore≤0 默认0.5（FilterSources 内部逻辑）
	t.Run("R46_默认阈值0.5过滤", func(t *testing.T) {
		srcs := []Source{
			{URL: "https://reuters.com/a", Credibility: 0.8, Domain: "reuters.com"},
			{URL: "https://unknown-blog.com/b", Credibility: 0.3, Domain: "unknown-blog.com"},
		}
		// minScore=0 被内部重置为0.5，reuters(0.8)保留，unknown(0.3)剔除
		filtered := FilterSources(srcs, 0)
		if len(filtered) != 1 {
			t.Fatalf("minScore=0→0.5默认，应剩1个: got %d", len(filtered))
		}
		if filtered[0].Domain != "reuters.com" {
			t.Fatalf("保留的应为reuters.com, got %s", filtered[0].Domain)
		}
	})

	// 第 47 轮：高阈值严格过滤
	t.Run("R47_高阈值严格过滤", func(t *testing.T) {
		srcs := []Source{
			{URL: "https://reuters.com/a", Credibility: 0.9},
			{URL: "https://apnews.com/b", Credibility: 0.75},
			{URL: "https://blog.com/c", Credibility: 0.4},
		}
		filtered := FilterSources(srcs, 0.8)
		if len(filtered) != 1 {
			t.Fatalf("阈值=0.8应只剩1个: got %d", len(filtered))
		}
	})

	// 第 48 轮：ScoreAndTagSources 补域名
	t.Run("R48_补全域名", func(t *testing.T) {
		srcs := []Source{
			{URL: "https://example.com/page", Domain: ""},
		}
		ScoreAndTagSources(&KnowledgeEntry{Sources: srcs})
		if srcs[0].Domain != "example.com" {
			t.Fatalf("域名应被补全: got %q", srcs[0].Domain)
		}
	})

	// 第 49 轮：交叉验证加分
	t.Run("R49_交叉验证", func(t *testing.T) {
		srcs := []Source{
			{URL: "https://reuters.com/report1"},
			{URL: "https://apnews.com/report2"},
			{URL: "https://bbc.com/report3"},
		}
		e := &KnowledgeEntry{Sources: srcs}
		ScoreAndTagSources(e)
		// 每个源: 白名单(+0.40) + 交叉验证3域(+0.30) = 0.70
		for _, s := range e.Sources {
			if s.Credibility < 0.65 {
				t.Fatalf("%s 多源交叉验证可信度=%.2f 应≥0.65", s.Domain, s.Credibility)
			}
		}
	})

	// 第 50 轮：DeleteKnowledge 回滚
	t.Run("R50_按条件删除", func(t *testing.T) {
		q := "删除测试_午夜之锤"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "测试", Tier: "test"})
		defer cleanupEntryMH(t, q)
		n := DeleteKnowledge(func(e *KnowledgeEntry) bool {
			return e.Tier == "test"
		})
		if n < 1 {
			t.Fatal("应至少删除1条")
		}
		_, hit := LoadKnowledge(q)
		if hit {
			t.Fatal("删除后不应命中")
		}
	})
}

// =========================================================================
// 维度 6：策略门控与联网授权（10 轮）
// =========================================================================
func TestMidnightHammer_Dim6_PolicyGating(t *testing.T) {
	cleanKnowledgeCacheMH(t)
	t.Log("=== 维度6：策略门控与联网授权 ===")

	// 第 51 轮：默认策略拒绝联网
	t.Run("R51_默认拒绝联网", func(t *testing.T) {
		p := DefaultPolicy()
		if p.CanWebSearch(time.Now()) {
			t.Fatal("默认策略必须拒绝联网")
		}
	})

	// 第 52 轮：授权后允许
	t.Run("R52_永久授权允许", func(t *testing.T) {
		p := DefaultPolicy()
		p.Approve(GrantYear, time.Now())
		p.Frequency = FrequencyHigh
		if !p.CanWebSearch(time.Now()) {
			t.Fatal("永久授权后必须允许")
		}
	})

	// 第 53 轮：频率冷却
	t.Run("R53_低频冷却10分钟", func(t *testing.T) {
		now := time.Now()
		p := DefaultPolicy()
		p.Approve(GrantYear, now)
		p.Frequency = FrequencyLow
		p.MarkWebUsed(now)
		// FrequencyLow = 10 min cooldown
		if p.CanWebSearch(now.Add(5 * time.Minute)) {
			t.Fatal("低频冷却10min: 5min后不应允许")
		}
		if !p.CanWebSearch(now.Add(11 * time.Minute)) {
			t.Fatal("低频冷却10min: 11min后应允许")
		}
	})

	// 第 54 轮：中频冷却
	t.Run("R54_中频冷却5分钟", func(t *testing.T) {
		now := time.Now()
		p := DefaultPolicy()
		p.Approve(GrantYear, now)
		p.Frequency = FrequencyMedium
		p.MarkWebUsed(now)
		if p.CanWebSearch(now.Add(3 * time.Minute)) {
			t.Fatal("中频冷却5min: 3min后不应允许")
		}
		if !p.CanWebSearch(now.Add(6 * time.Minute)) {
			t.Fatal("中频冷却5min: 6min后应允许")
		}
	})

	// 第 55 轮：高频冷却
	t.Run("R55_高频冷却1分钟", func(t *testing.T) {
		now := time.Now()
		p := DefaultPolicy()
		p.Approve(GrantYear, now)
		p.Frequency = FrequencyHigh
		p.MarkWebUsed(now)
		// FrequencyHigh = 1 min cooldown
		if p.CanWebSearch(now.Add(30 * time.Second)) {
			t.Fatal("高频冷却1min: 30s后不应允许")
		}
		if !p.CanWebSearch(now.Add(61 * time.Second)) {
			t.Fatal("高频冷却1min: 61s后应允许")
		}
	})

	// 第 56 轮：FrequencyOff 永拒
	t.Run("R56_Off永拒", func(t *testing.T) {
		p := DefaultPolicy()
		p.Approve(GrantYear, time.Now())
		p.Frequency = FrequencyOff
		if p.CanWebSearch(time.Now()) {
			t.Fatal("Off 必须永拒")
		}
	})

	// 第 57 轮：Revoke 撤销
	t.Run("R57_Revoke撤销", func(t *testing.T) {
		p := DefaultPolicy()
		p.Approve(GrantYear, time.Now())
		p.Revoke()
		if p.IsGranted(time.Now()) {
			t.Fatal("Revoke 后必须未授权")
		}
	})

	// 第 58 轮：Session 授权
	t.Run("R58_Session授权", func(t *testing.T) {
		p := DefaultPolicy()
		p.Approve(GrantYear, time.Now())
		if !p.IsGranted(time.Now()) {
			t.Fatal("Session 授权必须激活")
		}
	})

	// 第 59 轮：动态自适应冷却
	t.Run("R59_动态自适应", func(t *testing.T) {
		p := DefaultPolicy()
		p.WebSearch = true
		p.Frequency = FrequencyDynamic
		p.ApplyDynamic(0.95, 0.9)
		if cd := p.effectiveCooldown(); cd < 15*time.Minute {
			t.Fatalf("强信号应加长冷却: got %v", cd)
		}
		p.ApplyDynamic(0.05, 0.05)
		if cd := p.effectiveCooldown(); cd > 5*time.Minute {
			t.Fatalf("弱信号应缩短冷却: got %v", cd)
		}
	})

	// 第 60 轮：WebBlocked 但不丢缓存
	t.Run("R60_WebBlocked保留缓存", func(t *testing.T) {
		q := "午夜之锤_最新动态_已过期"
		SaveKnowledge(&KnowledgeEntry{
			Query: q, AnswerSummary: "旧信息", TimeSensitive: true,
			FreshUntil: time.Now().Add(-time.Hour),
		})
		defer cleanupEntryMH(t, q)
		res, err := Retrieve(context.Background(), q, RetrieveOptions{},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				t.Fatal("不应调用fetch")
				return nil, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if !res.WebBlocked || !res.StaleServed {
			t.Fatalf("无授权时: WebBlocked=%v StaleServed=%v", res.WebBlocked, res.StaleServed)
		}
		if !strings.Contains(res.Entry.AnswerSummary, "信息截至") {
			t.Fatal("stale 标注缺失")
		}
	})
}

// =========================================================================
// 维度 7：Retrieve 闭环全链路（10 轮）
// =========================================================================
func TestMidnightHammer_Dim7_RetrieveClosedLoop(t *testing.T) {
	cleanKnowledgeCacheMH(t)
	t.Log("=== 维度7：Retrieve闭环全链路 ===")

	// 第 61 轮：未命中→分级→抓取→落盘
	t.Run("R61_未命中全链路", func(t *testing.T) {
		mockFetch := func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
			return &KnowledgeEntry{
				Query: query, AnswerSummary: "午夜之锤：B-2打击伊朗地下设施",
				KeyFacts: []string{"B-2从Diego Garcia起飞", "打击时间6月22日02:10"},
				Sources:  []Source{{URL: "https://reuters.com/report"}},
			}, nil
		}
		res, err := Retrieve(context.Background(),
			"午夜之锤 B-2 打击详情", RetrieveOptions{Policy: webPolicyMH()}, mockFetch)
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if !res.APIUsed || res.FromCache {
			t.Fatalf("未命中必须走API: APIUsed=%v FromCache=%v", res.APIUsed, res.FromCache)
		}
		if len(res.Entry.Sources) == 0 {
			t.Fatal("来源不应为空")
		}
	})

	// 第 62 轮：L1命中零API
	t.Run("R62_L1命中零API", func(t *testing.T) {
		q := "L1命中测试_午夜之锤"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "缓存答案"})
		defer cleanupEntryMH(t, q)
		fetches := 0
		res, err := Retrieve(context.Background(), q, RetrieveOptions{},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				fetches++
				return nil, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if !res.FromCache || res.APIUsed || fetches != 0 {
			t.Fatalf("L1命中: FromCache=%v APIUsed=%v fetches=%d", res.FromCache, res.APIUsed, fetches)
		}
	})

	// 第 63 轮：强制刷新
	t.Run("R63_强制刷新绕过缓存", func(t *testing.T) {
		q := "强制刷新测试"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "旧"})
		defer cleanupEntryMH(t, q)
		fetches := 0
		res, err := Retrieve(context.Background(), q, RetrieveOptions{ForceRefresh: true},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				fetches++
				return &KnowledgeEntry{Query: query, AnswerSummary: "新"}, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if fetches != 1 || !res.APIUsed {
			t.Fatalf("强制刷新: fetches=%d APIUsed=%v", fetches, res.APIUsed)
		}
		if res.Entry.AnswerSummary != "新" {
			t.Fatalf("强制刷新应返回新结果: %q", res.Entry.AnswerSummary)
		}
	})

	// 第 64 轮：时效过期→stale+刷新
	t.Run("R64_时效过期stale加刷新", func(t *testing.T) {
		q := "午夜之锤时效过期测试"
		SaveKnowledge(&KnowledgeEntry{
			Query: q, AnswerSummary: "旧情报", TimeSensitive: true,
			FreshUntil: time.Now().Add(-time.Hour),
		})
		defer cleanupEntryMH(t, q)
		res, err := Retrieve(context.Background(), q, RetrieveOptions{Policy: webPolicyMH()},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				return &KnowledgeEntry{Query: query, AnswerSummary: "新情报", KeyFacts: []string{"更新"}}, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if !res.StaleServed || !res.Refreshed {
			t.Fatalf("时效过期: StaleServed=%v Refreshed=%v", res.StaleServed, res.Refreshed)
		}
		if res.Entry.AnswerSummary != "新情报" {
			t.Fatalf("刷新后应更新: %q", res.Entry.AnswerSummary)
		}
	})

	// 第 65 轮：操作内容不透传缓存
	t.Run("R65_操作内容不落盘", func(t *testing.T) {
		q := "午夜之锤恐慌测试"
		res, err := Retrieve(context.Background(), q, RetrieveOptions{Policy: webPolicyMH()},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				return &KnowledgeEntry{
					Query: query, AnswerSummary: "紧急！灾难即将失控！转发所有人！",
					KeyFacts: []string{"零风险方案"},
				}, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if res.Entry == nil {
			t.Fatal("操作内容必须透传")
		}
		if _, hit := LoadKnowledge(q); hit {
			t.Fatal("操作内容不得落盘")
		}
	})

	// 第 66 轮：PanicMode 追加安抚
	t.Run("R66_PanicMode追加安抚", func(t *testing.T) {
		// "核" (panicHint) + "今晚" (timeUrgencyHint) → PanicScore > 0
		q := "今晚会发生核战争吗"
		res, err := Retrieve(context.Background(), q, RetrieveOptions{PanicMode: true, Policy: webPolicyMH()},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				return &KnowledgeEntry{Query: query, AnswerSummary: "无核战争风险迹象"}, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if !containsStr(res.Entry.AnswerSummary, "温馨提示") {
			t.Fatal("PanicMode 必须追加安抚行")
		}
		if !containsStr(res.Entry.AnswerSummary, "无核战争风险迹象") {
			t.Fatal("原始答案必须保留")
		}
		if e, hit := LoadKnowledge(q); !hit || !containsStr(e.AnswerSummary, "温馨提示") {
			t.Fatal("缓存也必须携带安抚行")
		}
		defer cleanupEntryMH(t, q)
	})

	// 第 67 轮：BypassProbability 绕过
	t.Run("R67_Bypass绕过刷新", func(t *testing.T) {
		qBase := "Bypass测试_午夜之锤_基础查询"
		SaveKnowledge(&KnowledgeEntry{Query: qBase, AnswerSummary: "旧快照"})
		defer cleanupEntryMH(t, qBase)
		fetches := 0
		res, err := Retrieve(context.Background(), "午夜之锤 Bypass 测试查询",
			RetrieveOptions{BypassProbability: 1, Policy: webPolicyMH()},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				fetches++
				return &KnowledgeEntry{Query: qBase, AnswerSummary: "新快照", KeyFacts: []string{"刷新"}}, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if !res.Bypassed || fetches != 1 {
			t.Fatalf("BypassProbability=1: Bypassed=%v fetches=%d", res.Bypassed, fetches)
		}
	})

	// 第 68 轮：L1精确永不Bypass
	t.Run("R68_L1精确永不Bypass", func(t *testing.T) {
		q := "精确永不Bypass测试"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "缓存"})
		defer cleanupEntryMH(t, q)
		fetches := 0
		res, err := Retrieve(context.Background(), q, RetrieveOptions{BypassProbability: 1},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				fetches++
				return nil, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if res.APIUsed || fetches != 0 {
			t.Fatal("L1精确命中即使BypassProbability=1也不能绕过")
		}
	})

	// 第 69 轮：nil fetch 报错
	t.Run("R69_nilFetch报错", func(t *testing.T) {
		_, err := Retrieve(context.Background(), "q", RetrieveOptions{}, nil)
		if err == nil {
			t.Fatal("nil fetch 必须报错")
		}
	})

	// 第 70 轮：质量过滤后落盘
	t.Run("R70_质量过滤落盘", func(t *testing.T) {
		q := "质量过滤测试_午夜之锤"
		res, err := Retrieve(context.Background(), q, RetrieveOptions{Policy: webPolicyMH()},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				return &KnowledgeEntry{
					Query: query, AnswerSummary: "结果",
					Sources: []Source{
						{URL: "https://reuters.com/a"},
						{URL: "https://spam.xyz/b?ad=1"},
					},
				}, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if len(res.Entry.Sources) != 1 || res.Entry.Sources[0].Domain != "reuters.com" {
			t.Fatalf("垃圾源应被过滤: %#v", res.Entry.Sources)
		}
		defer cleanupEntryMH(t, q)
	})
}

// =========================================================================
// 维度 8：分级路由（10 轮）
// =========================================================================
func TestMidnightHammer_Dim8_TierRouting(t *testing.T) {
	t.Log("=== 维度8：分级路由 ===")

	// 第 71 轮：简单查询
	t.Run("R71_简单查询", func(t *testing.T) {
		tier, _ := ClassifyTier("B-2轰炸机是什么")
		if tier != TierSimple {
			t.Fatalf("简单事实查询: tier=%s want simple", tier)
		}
	})

	// 第 72 轮：对比查询→complex
	t.Run("R72_对比查询complex", func(t *testing.T) {
		tier, _ := ClassifyTier("对比B-2和B-52的打击能力")
		if tier != TierComplex {
			t.Fatalf("对比查询: tier=%s want complex", tier)
		}
	})

	// 第 73 轮：分析查询→complex
	t.Run("R73_分析查询complex", func(t *testing.T) {
		tier, _ := ClassifyTier("分析午夜之锤行动的战略影响")
		if tier != TierComplex {
			t.Fatalf("分析查询: tier=%s want complex", tier)
		}
	})

	// 第 74 轮：多事实组合→general
	t.Run("R74_多事实组合general", func(t *testing.T) {
		tier, _ := ClassifyTier("总结KC-46加油机部署和E-4B出动的最新进展")
		if tier != TierGeneral {
			t.Fatalf("多事实组合: tier=%s want general", tier)
		}
	})

	// 第 75 轮：TierSimple 最大轮次
	t.Run("R75_TierSimple最大1轮", func(t *testing.T) {
		if r := TierSimple.maxRounds(); r != 1 {
			t.Fatalf("Simple最大轮次=%d want 1", r)
		}
	})

	// 第 76 轮：TierGeneral 最大轮次
	t.Run("R76_TierGeneral最大3轮", func(t *testing.T) {
		if r := TierGeneral.maxRounds(); r != 3 {
			t.Fatalf("General最大轮次=%d want 3", r)
		}
	})

	// 第 77 轮：TierComplex 最大轮次
	t.Run("R77_TierComplex最大5轮", func(t *testing.T) {
		if r := TierComplex.maxRounds(); r != 5 {
			t.Fatalf("Complex最大轮次=%d want 5", r)
		}
	})

	// 第 78 轮：TierDeep 最大轮次
	t.Run("R78_TierDeep最大12轮", func(t *testing.T) {
		if r := TierDeep.maxRounds(); r != 12 {
			t.Fatalf("Deep最大轮次=%d want 12", r)
		}
	})

	// 第 79 轮：未知 tier 默认
	t.Run("R79_未知tier默认", func(t *testing.T) {
		if r := RetrievalTier("unknown").maxRounds(); r != 1 {
			t.Fatalf("未知tier默认=%d want 1", r)
		}
	})

	// 第 80 轮：tier 持久化往返
	t.Run("R80_tier持久化", func(t *testing.T) {
		q := "tier持久化测试_午夜之锤"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "x", Tier: "deep"})
		defer cleanupEntryMH(t, q)
		e, hit := LoadKnowledge(q)
		if !hit || e.Tier != "deep" {
			t.Fatalf("tier持久化失败: hit=%v tier=%s", hit, e.Tier)
		}
	})
}

// =========================================================================
// 维度 9：冲突检测与多源矛盾（10 轮）
// =========================================================================
func TestMidnightHammer_Dim9_ConflictDetection(t *testing.T) {
	t.Log("=== 维度9：冲突检测与多源矛盾 ===")

	// 第 81 轮：中文否认
	t.Run("R81_中文否认", func(t *testing.T) {
		if !hasConflictHints([]string{"伊朗官方否认参与核活动"}) {
			t.Fatal("否认 必须触发冲突")
		}
	})

	// 第 82 轮：中文辟谣
	t.Run("R82_中文辟谣", func(t *testing.T) {
		if !hasConflictHints([]string{"美军辟谣B-2坠毁传闻"}) {
			t.Fatal("辟谣 必须触发冲突")
		}
	})

	// 第 83 轮：英文否认
	t.Run("R83_英文否认", func(t *testing.T) {
		if !hasConflictHints([]string{"The Pentagon denies the report"}) {
			t.Fatal("denies 必须触发冲突")
		}
	})

	// 第 84 轮：英文contradicts
	t.Run("R84_英文contradicts", func(t *testing.T) {
		if !hasConflictHints([]string{"New evidence contradicts earlier claims"}) {
			t.Fatal("contradicts 必须触发冲突")
		}
	})

	// 第 85 轮：无冲突的普通文本
	t.Run("R85_普通文本无冲突", func(t *testing.T) {
		if hasConflictHints([]string{"B-2轰炸机成功完成任务", "加油机顺利返航"}) {
			t.Fatal("普通军事报告不应触发冲突")
		}
	})

	// 第 86 轮：空列表
	t.Run("R86_空列表无冲突", func(t *testing.T) {
		if hasConflictHints([]string{}) {
			t.Fatal("空列表不应触发冲突")
		}
		if hasConflictHints(nil) {
			t.Fatal("nil 不应触发冲突")
		}
	})

	// 第 87 轮：撤回
	t.Run("R87_撤回触发冲突", func(t *testing.T) {
		if !hasConflictHints([]string{"情报机构撤回先前评估"}) {
			t.Fatal("撤回 必须触发冲突")
		}
	})

	// 第 88 轮：并未/并非
	t.Run("R88_并未并非_通用否定不误触", func(t *testing.T) {
		// 修复（fable5 should-fix：否定词误触）："并未/并非/不是" 出现在
		// 无害句（"今天不是周末"），不再是冲突信号；仅明确矛盾词触发。
		if hasConflictHints([]string{"该报道并未得到证实"}) {
			t.Fatal("并未 是通用否定，不应误触冲突")
		}
		if hasConflictHints([]string{"此说法并非事实"}) {
			t.Fatal("并非 是通用否定，不应误触冲突")
		}
		if hasConflictHints([]string{"今天不是周末"}) {
			t.Fatal("不是 是通用否定，不应误触冲突")
		}
		// 明确矛盾词仍触发
		if !hasConflictHints([]string{"官方否认该事件"}) {
			t.Fatal("否认 必须触发冲突")
		}
	})

	// 第 89 轮：冲突后置信度下降验证
	t.Run("R89_冲突惩罚精确验证", func(t *testing.T) {
		now := time.Now()
		e := &KnowledgeEntry{TimeSensitive: true}
		advanceEvent(e, now, []string{"确认情报"}, nil)
		pre := e.Confidence
		advanceEvent(e, now, []string{"否认情报"}, nil)
		if e.Confidence > pre-conflictPenalty+0.01 {
			t.Fatalf("冲突惩罚: pre=%.2f post=%.2f delta ~= %.2f", pre, e.Confidence, conflictPenalty)
		}
	})

	// 第 90 轮：冲突后继续更新恢复置信度
	t.Run("R90_冲突后恢复", func(t *testing.T) {
		now := time.Now()
		e := &KnowledgeEntry{TimeSensitive: true}
		advanceEvent(e, now, []string{"初始情报"}, nil)
		advanceEvent(e, now, []string{"否认"}, nil)
		postConflict := e.Confidence
		advanceEvent(e, now, []string{"新证据确认"}, nil)
		advanceEvent(e, now, []string{"第三来源证实"}, nil)
		if e.Confidence <= postConflict {
			t.Fatalf("冲突后应能恢复: post-conflict=%.2f current=%.2f", postConflict, e.Confidence)
		}
	})
}

// =========================================================================
// 维度 10：边缘情况与压力测试（10 轮）
// =========================================================================
func TestMidnightHammer_Dim10_EdgeCasesAndStress(t *testing.T) {
	cleanKnowledgeCacheMH(t)
	t.Log("=== 维度10：边缘情况与压力测试 ===")

	// 第 91 轮：超长查询 L1
	t.Run("R91_超长查询L1", func(t *testing.T) {
		q := strings.Repeat("午夜之锤行动航班追踪情报分析 ", 50)
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "超长"})
		defer cleanupEntryMH(t, q)
		_, hit := LoadKnowledge(q)
		if !hit {
			t.Fatal("超长查询L1必须命中")
		}
	})

	// 第 92 轮：Unicode 混合查询
	t.Run("R92_Unicode混合", func(t *testing.T) {
		q := "Midnight Hammer 🔨 午夜之锤 B-2🛩️ 2025年6月 🎯"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "emoji"})
		defer cleanupEntryMH(t, q)
		e, hit := LoadKnowledge(q)
		if !hit || e.AnswerSummary != "emoji" {
			t.Fatalf("Unicode混合: hit=%v", hit)
		}
	})

	// 第 93 轮：JSON 注入防御
	t.Run("R93_JSON注入防御", func(t *testing.T) {
		q := `{"query": "malicious"}`
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "safe"})
		defer cleanupEntryMH(t, q)
		e, hit := LoadKnowledge(q)
		if !hit || e.AnswerSummary != "safe" {
			t.Fatal("JSON注入应安全存取")
		}
	})

	// 第 94 轮：换行符查询
	t.Run("R94_换行符查询", func(t *testing.T) {
		q := "午夜之锤\nB-2打击\n2025年6月"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "multiline"})
		defer cleanupEntryMH(t, q)
		_, hit := LoadKnowledge(q)
		if !hit {
			t.Fatal("换行符查询必须命中")
		}
	})

	// 第 95 轮：大批量存取（100条）
	t.Run("R95_大批量存取", func(t *testing.T) {
		qs := make([]string, 100)
		for i := 0; i < 100; i++ {
			qs[i] = fmt.Sprintf("午夜之锤_批量测试_%04d", i)
			SaveKnowledge(&KnowledgeEntry{Query: qs[i], AnswerSummary: fmt.Sprintf("ans-%d", i)})
		}
		defer func() {
			for _, q := range qs {
				cleanupEntryMH(t, q)
			}
		}()
		for i, q := range qs {
			e, hit := LoadKnowledge(q)
			if !hit {
				t.Fatalf("批量[%d]未命中", i)
			}
			if e.AnswerSummary != fmt.Sprintf("ans-%d", i) {
				t.Fatalf("批量[%d]答案错误: %q", i, e.AnswerSummary)
			}
		}
		all := ListKnowledge()
		if len(all) < 100 {
			t.Fatalf("ListKnowledge仅%d条，预期≥100", len(all))
		}
	})

	// 第 96 轮：缓存目录不存在恢复
	t.Run("R96_缓存目录恢复", func(t *testing.T) {
		dir, _ := knowledgeDir()
		if dir == "" {
			t.Skip("无缓存目录")
		}
		os.RemoveAll(dir)
		q := "重建测试_午夜之锤"
		SaveKnowledge(&KnowledgeEntry{Query: q, AnswerSummary: "重建"})
		defer cleanupEntryMH(t, q)
		_, hit := LoadKnowledge(q)
		if !hit {
			t.Fatal("目录重建后应正常工作")
		}
	})

	// 第 97 轮：破壁引导不触发 PanicScore=0
	t.Run("R97_正常查询不触发Panic", func(t *testing.T) {
		score := PanicScore("B-2轰炸机的技术参数是什么")
		if score > 0 {
			t.Fatalf("技术查询 PanicScore=%d want 0", score)
		}
	})

	// 第 98 轮：灾难查询触发 PanicScore
	t.Run("R98_灾难查询触发Panic", func(t *testing.T) {
		score := PanicScore("今晚会地震吗 好害怕")
		if score <= 0 {
			t.Fatalf("灾难恐慌查询 PanicScore=%d want >0", score)
		}
	})

	// 第 99 轮：Tier 覆盖
	t.Run("R99_Tier手动覆盖", func(t *testing.T) {
		q := "Tier覆盖测试_午夜之锤"
		res, err := Retrieve(context.Background(), q,
			RetrieveOptions{Tier: TierDeep, Policy: webPolicyMH()},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				if tier != TierDeep {
					t.Fatalf("应使用手动指定的TierDeep, got %s", tier)
				}
				return &KnowledgeEntry{Query: query, AnswerSummary: "ok"}, nil
			})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if res.Tier != TierDeep {
			t.Fatalf("结果tier=%s want deep", res.Tier)
		}
		defer cleanupEntryMH(t, q)
	})

	// 第 100 轮：完整命中链路端到端
	t.Run("R100_完整命中链路端到端", func(t *testing.T) {
		q := "2025年6月午夜之锤行动完整报告"
		fetchCount := 0
		res1, err := Retrieve(context.Background(), q, RetrieveOptions{
			Tier: TierComplex, Policy: webPolicyMH(),
		}, func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
			fetchCount++
			return &KnowledgeEntry{
				Query: query, AnswerSummary: "午夜之锤行动：2025年6月美军B-2轰炸机打击伊朗地下设施",
				KeyFacts: []string{
					"6月15日：KC-46加油机20架东调至Diego Garcia",
					"6月17日：E-4B Nightwatch出动",
					"6月20日：至少6架B-2部署至Diego Garcia",
					"6月22日02:10：B-2投掷GBU-57钻地弹",
				},
				Sources: []Source{
					{URL: "https://reuters.com/report1"},
					{URL: "https://apnews.com/report2"},
					{URL: "https://twz.com/analysis"},
				},
				Confidence: 0.7,
			}, nil
		})
		if err != nil {
			t.Fatalf("首查: %v", err)
		}
		if fetchCount != 1 || !res1.APIUsed {
			t.Fatal("首查必须走API")
		}

		res2, err := Retrieve(context.Background(), q, RetrieveOptions{},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				t.Fatal("不应被调用")
				return nil, nil
			})
		if err != nil {
			t.Fatalf("L1命中: %v", err)
		}
		if !res2.FromCache || res2.APIUsed {
			t.Fatal("L1命中必须零API")
		}
		if !strings.Contains(res2.Entry.AnswerSummary, "午夜之锤") {
			t.Fatal("缓存答案必须正确")
		}

		res3, err := Retrieve(context.Background(), "2025年6月 Operation Midnight Hammer 完整报告",
			RetrieveOptions{},
			func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error) {
				t.Fatal("不应被调用")
				return nil, nil
			})
		if err != nil {
			t.Fatalf("L2命中: %v", err)
		}
		if !res3.FromCache {
			t.Fatal("语义相似必须命中L2")
		}
		if len(res3.Entry.KeyFacts) != 4 {
			t.Fatalf("关键事实数=%d want 4", len(res3.Entry.KeyFacts))
		}

		c := NewCausalChain("午夜之锤")
		c.Add(CausalEvent{Stage: StageSignal, Time: "6月15日", Detail: "KC-46加油机东调"})
		c.Add(CausalEvent{Stage: StageInference, Time: "6月17日", Detail: "E-4B出动"})
		c.Add(CausalEvent{Stage: StageAction, Time: "6月22日", Detail: "B-2打击"})
		c.Add(CausalEvent{Stage: StageConsequence, Time: "6月22日 02:10", Detail: "成功突袭"})
		c.Confidence = 0.9
		c.Sources = []string{"reuters.com", "apnews.com", "twz.com"}
		report := c.Render()
		for _, must := range []string{"午夜之锤", "信号", "推断", "行动", "后果", "KC-46", "E-4B", "B-2", "0.90"} {
			if !strings.Contains(report, must) {
				t.Fatalf("因果链报告缺失 %q", must)
			}
		}

		t.Logf("✅ R100端到端: 首查API=%v → L1命中=%v → L2命中=%v → 报告完整",
			res1.APIUsed, res2.FromCache, res3.FromCache)
	})
}

// =========================================================================
// 辅助函数
// =========================================================================

func cleanKnowledgeCacheMH(t *testing.T) {
	t.Helper()
	dir, err := knowledgeDir()
	if err != nil {
		return
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func cleanupEntryMH(t *testing.T, q string) {
	t.Helper()
	dir, err := knowledgeDir()
	if err != nil {
		return
	}
	os.Remove(filepath.Join(dir, KnowledgeHash(q)+".json"))
}

func webPolicyMH() *RetrievalPolicy {
	p := DefaultPolicy()
	p.Approve(GrantYear, time.Now())
	p.Frequency = FrequencyHigh
	return &p
}

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}

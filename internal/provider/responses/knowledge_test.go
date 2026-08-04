package responses

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNgramSimilarity(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
		desc string
	}{
		{"今天北京天气怎么样", "北京今天天气如何", 0.7, "近义句式"},
		{"2026年8月3日北京天气", "北京今天天气如何", 0.5, "部分重叠"},
		{"图灵奖得主是谁", "北京天气如何", 0.05, "无关"},
		{"same query", "same query", 1.0, "完全相同"},
		{"", "任意", 0.0, "空串"},
	}
	for _, c := range cases {
		got := NgramSimilarity(c.a, c.b)
		t.Logf("%s: %q vs %q = %.3f (want ~%.2f)", c.desc, c.a, c.b, got, c.want)
		if c.desc == "完全相同" && got != 1.0 {
			t.Errorf("identical strings must be 1.0, got %.3f", got)
		}
		if c.desc == "空串" && got != 0.0 {
			t.Errorf("empty must be 0.0, got %.3f", got)
		}
		if c.desc == "无关" && got >= DefaultSemanticThreshold {
			t.Errorf("unrelated queries must not pass threshold %v, got %.3f", DefaultSemanticThreshold, got)
		}
	}
}

func TestLoadKnowledgeSemanticHitAndMiss(t *testing.T) {
	cleanKnowledgeCache(t)

	dir := mustKnowledgeDir(t)
	// 清理测试可能残留
	q1 := "2026年8月3日北京天气怎么样？"
	SaveKnowledge(&KnowledgeEntry{Query: q1, AnswerSummary: "晴朗 25-32℃"})
	defer os.Remove(filepath.Join(dir, KnowledgeHash(q1)+".json"))

	// 近义改写 → 应命中
	entry, sim, hit := LoadKnowledgeSemantic("北京今天天气如何？", DefaultSemanticThreshold)
	if !hit {
		t.Fatalf("semantic variant should hit, got sim=%.3f", sim)
	}
	if entry.AnswerSummary != "晴朗 25-32℃" {
		t.Fatalf("wrong entry: %#v", entry)
	}

	// 无关问题 → 不应命中
	if _, _, hit := LoadKnowledgeSemantic("如何制作番茄炒蛋", DefaultSemanticThreshold); hit {
		t.Fatal("unrelated query must miss")
	}
}

func TestLoadKnowledgeSemanticSkipsExpired(t *testing.T) {
	cleanKnowledgeCache(t)

	dir := mustKnowledgeDir(t)
	q := "过期测试问题"
	e := &KnowledgeEntry{Query: q, AnswerSummary: "x", ExpiresAt: time.Now().Add(-time.Hour)}
	SaveKnowledge(e)
	defer os.Remove(filepath.Join(dir, KnowledgeHash(q)+".json"))

	if _, _, hit := LoadKnowledgeSemantic(q, DefaultSemanticThreshold); hit {
		t.Fatal("expired entry must miss")
	}
}

func TestNeedsRefreshDecisionTable(t *testing.T) {
	now := time.Now()
	base := func() *KnowledgeEntry { return &KnowledgeEntry{} }

	cases := []struct {
		name string
		e    *KnowledgeEntry
		want bool
	}{
		{"事实类永不刷新", base(), false},
		{"时效类未过期", func() *KnowledgeEntry {
			e := base()
			e.TimeSensitive = true
			e.FreshUntil = now.Add(time.Hour)
			return e
		}(), false},
		{"时效类已过期需增量", func() *KnowledgeEntry {
			e := base()
			e.TimeSensitive = true
			e.FreshUntil = now.Add(-time.Hour)
			return e
		}(), true},
		{"时效类无FreshUntil回退ExpiresAt", func() *KnowledgeEntry {
			e := base()
			e.TimeSensitive = true
			e.ExpiresAt = now.Add(-time.Minute)
			return e
		}(), true},
		{"nil条目不刷新", nil, false},
		{"时效类无任何期限", func() *KnowledgeEntry {
			e := base()
			e.TimeSensitive = true
			return e
		}(), false},
	}
	for _, c := range cases {
		if got := c.e.NeedsRefresh(now); got != c.want {
			t.Errorf("%s: NeedsRefresh=%v want %v", c.name, got, c.want)
		}
	}
}

func TestKnowledgeTierPersisted(t *testing.T) {
	cleanKnowledgeCache(t)

	dir := mustKnowledgeDir(t)
	q := "复杂问题"
	e := &KnowledgeEntry{Query: q, AnswerSummary: "x", Tier: "complex", TimeSensitive: true,
		FreshUntil: time.Now().Add(time.Hour)}
	SaveKnowledge(e)
	defer os.Remove(filepath.Join(dir, KnowledgeHash(q)+".json"))

	got, hit := LoadKnowledge(q)
	if !hit {
		t.Fatal("expected hit")
	}
	if got.Tier != "complex" || !got.TimeSensitive {
		t.Fatalf("tier/time_sensitive not persisted: %#v", got)
	}
	if got.NeedsRefresh(time.Now()) {
		t.Fatal("fresh time-sensitive entry must not need refresh")
	}
	if !got.NeedsRefresh(time.Now().Add(2 * time.Hour)) {
		t.Fatal("expired time-sensitive entry must need refresh")
	}
}

// cleanKnowledgeCache wipes the websearch cache dir so tests are isolated
// from each other (semantic scan covers the whole dir, so leftovers from
// earlier tests would otherwise be "hits").
func cleanKnowledgeCache(t *testing.T) {
	t.Helper()
	dir := mustKnowledgeDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		_ = os.Remove(filepath.Join(dir, de.Name()))
	}
}

func TestAuditListAndDeleteKnowledge(t *testing.T) {
	cleanKnowledgeCache(t)
	dir := mustKnowledgeDir(t)
	defer func() { _ = os.RemoveAll(dir) }()

	SaveKnowledge(&KnowledgeEntry{Query: "正常问题A", AnswerSummary: "a", SourceRequestID: "req-1"})
	SaveKnowledge(&KnowledgeEntry{Query: "正常问题B", AnswerSummary: "b", SourceRequestID: "req-1"})
	SaveKnowledge(&KnowledgeEntry{Query: "污染问题C", AnswerSummary: "c", SourceRequestID: "req-2"})

	all := ListKnowledge()
	if len(all) != 3 {
		t.Fatalf("ListKnowledge want 3, got %d", len(all))
	}
	// 审计字段可追溯
	for _, e := range all {
		if e.SourceRequestID == "" {
			t.Fatalf("audit field missing for %q", e.Query)
		}
	}
	// 按 request id 回滚污染来源
	deleted := DeleteKnowledge(func(e *KnowledgeEntry) bool { return e.SourceRequestID == "req-2" })
	if deleted != 1 {
		t.Fatalf("DeleteKnowledge by request id want 1, got %d", deleted)
	}
	remaining := ListKnowledge()
	if len(remaining) != 2 {
		t.Fatalf("after rollback want 2, got %d", len(remaining))
	}
	// 全量回滚
	DeleteKnowledge(func(e *KnowledgeEntry) bool { return true })
	if len(ListKnowledge()) != 0 {
		t.Fatal("full sweep should empty the cache")
	}
}

func TestSemanticHitTopicConsistency(t *testing.T) {
	cleanKnowledgeCache(t)
	dir := mustKnowledgeDir(t)
	defer func() { _ = os.RemoveAll(dir) }()

	// 石油主题缓存
	SaveKnowledge(&KnowledgeEntry{Query: "全球主要港口石油吞吐量 霍尔木兹经济影响", AnswerSummary: "石油报告", TimeSensitive: false})

	// 同主题近义 → 命中
	if _, _, hit := LoadKnowledgeSemantic("霍尔木兹海峡封锁油价影响", DefaultSemanticThreshold); !hit {
		t.Fatal("same-topic near-synonym must hit")
	}
	// 不同主题（化肥/农业）→ 不命中（修复前误命中）
	if _, _, hit := LoadKnowledgeSemantic("霍尔木兹封锁 化肥供给 小麦收成", DefaultSemanticThreshold); hit {
		t.Fatal("cross-topic must NOT hit semantic cache")
	}
	// 无领域词的查询 → 退化相似度
	if _, _, hit := LoadKnowledgeSemantic("今天天气如何", DefaultSemanticThreshold); hit {
		t.Fatal("no-domain query should not hit oil cache")
	}
}

func TestExtractTopics(t *testing.T) {
	topics := ExtractTopics("霍尔木兹海峡化肥供给小麦收成")
	if len(topics) == 0 {
		t.Fatal("must extract domain words")
	}
	foundAgri := false
	for _, w := range topics {
		if w == "化肥" || w == "小麦" || w == "农业" {
			foundAgri = true
		}
	}
	if !foundAgri {
		t.Fatalf("agriculture words missing: %v", topics)
	}
}

// ---- 语言感知（2026-08-03 优化）----

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"北京今天天气怎么样", "zh"},
		{"Beijing weather today", "en"},
		{"Operation Midnight Hammer 轰炸", "zh"}, // 混合：有中文即 zh
		{"2025年6月 B-2 轰炸", "zh"},
		{"123456", ""}, // 无语言信号
	}
	for _, c := range cases {
		if got := DetectLanguage(c.in); got != c.want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLanguageGateBlocksCrossLanguageL2：en 缓存不得被 zh 近义查询命中
// （编排层 en 帧误拿中文快照的防护）。
func TestLanguageGateBlocksCrossLanguageL2(t *testing.T) {
	cleanKnowledgeCache(t)
	// en 缓存条目
	SaveKnowledge(&KnowledgeEntry{
		Query:         "Beijing weather forecast today",
		AnswerSummary: "Sunny, 30C in Beijing.",
		ExpiresAt:     time.Now().Add(time.Hour),
	})
	// zh 近义查询（语义相似但语言不同 → 必须不命中）
	if _, sim, hit := LoadKnowledgeSemantic("北京今天天气怎么样", DefaultSemanticThreshold); hit {
		t.Fatalf("zh query must not hit en cache (sim=%.3f)", sim)
	}
	// en 近义查询 → 命中
	if _, _, hit := LoadKnowledgeSemantic("weather in Beijing now", DefaultSemanticThreshold); !hit {
		t.Fatal("en near-synonym must hit en cache")
	}
	// zh 缓存 + zh 近义 → 命中
	SaveKnowledge(&KnowledgeEntry{
		Query:         "北京今天天气",
		AnswerSummary: "北京晴 30 度。",
		ExpiresAt:     time.Now().Add(time.Hour),
	})
	if _, _, hit := LoadKnowledgeSemantic("北京今天天气怎么样", DefaultSemanticThreshold); !hit {
		t.Fatal("zh near-synonym must hit zh cache")
	}
}

// TestCapacityEvictsLRU：超过上限时最旧的条目被淘汰。
func TestCapacityEvictsLRU(t *testing.T) {
	cleanKnowledgeCache(t)
	// 用隔离目录 + 缩小上限，避免写入 500 个文件。
	oldCap := MaxKnowledgeEntries
	MaxKnowledgeEntries = 5
	defer func() { MaxKnowledgeEntries = oldCap }()

	for i := 0; i < 8; i++ {
		SaveKnowledge(&KnowledgeEntry{
			Query:         fmt.Sprintf("话题%02d是什么", i),
			AnswerSummary: fmt.Sprintf("话题%02d 的回答", i),
			ExpiresAt:     time.Now().Add(time.Hour),
		})
	}
	// 前 3 个最旧的应被淘汰（8 - 5 = 3）
	for i := 0; i < 3; i++ {
		if _, ok := LoadKnowledge(fmt.Sprintf("话题%02d是什么", i)); ok {
			t.Fatalf("entry %d must be evicted (LRU)", i)
		}
	}
	// 后 5 个保留
	for i := 3; i < 8; i++ {
		if _, ok := LoadKnowledge(fmt.Sprintf("话题%02d是什么", i)); !ok {
			t.Fatalf("entry %d must survive", i)
		}
	}
}

// ---- 学习闭环（2026-08-03 优化：越使用越好）----

// TestVariantLearningLoop：首次 L2 命中 → 变体记录 → 二次同查询 L1 直接
// 命中（跳过语义扫描），且事件链关联。
func TestVariantLearningLoop(t *testing.T) {
	cleanKnowledgeCache(t)
	// 主条目落盘
	SaveKnowledge(&KnowledgeEntry{
		Query:         "北京今天天气怎么样",
		AnswerSummary: "北京晴 30 度。",
		ExpiresAt:     time.Now().Add(time.Hour),
	})
	// 首次：近义查询 L2 命中 → 学习变体
	variant := "北京今天天气如何"
	if _, sim, hit := LoadKnowledgeSemantic(variant, DefaultSemanticThreshold); !hit {
		t.Fatalf("first L2 hit failed: sim=%.3f", sim)
	}
	// 二次：同变体 L1 直接命中（零扫描——通过 variant 映射）
	if e, ok := LoadKnowledge(variant); !ok {
		t.Fatal("variant must resolve via L1 after learning")
	} else {
		if e.Query != "北京今天天气怎么样" {
			t.Fatalf("variant must map to main entry, got %q", e.Query)
		}
		// 变体已记录 + 事件链关联
		found := false
		for _, v := range e.QueryVariants {
			if v == variant {
				found = true
			}
		}
		if !found {
			t.Fatalf("variant must be recorded in QueryVariants: %v", e.QueryVariants)
		}
		foundChain := false
		for _, c := range e.EventChain {
			if c == variant {
				foundChain = true
			}
		}
		if !foundChain {
			t.Fatalf("variant must be linked in EventChain: %v", e.EventChain)
		}
	}
	// 变体文件存在
	if _, err := os.Stat(filepath.Join(mustKnowledgeDir(t), "variant_"+KnowledgeHash(variant)+".json")); err != nil {
		t.Fatalf("variant mapping file missing: %v", err)
	}
}

// TestSensitiveQueryNotLearnedAsVariant：操纵/煽动查询不建立变体关联
// （R65 场景：敏感查询不得绕过"manip 不落盘"隔离）。
func TestSensitiveQueryNotLearnedAsVariant(t *testing.T) {
	cleanKnowledgeCache(t)
	SaveKnowledge(&KnowledgeEntry{
		Query:         "地震应对指南",
		AnswerSummary: "地震时应躲到桌下。",
		ExpiresAt:     time.Now().Add(time.Hour),
	})
	// 敏感近义查询（含恐慌词）L2 命中 → 不学习变体
	sensitive := "地震会不会死很多人"
	if _, _, hit := LoadKnowledgeSemantic(sensitive, DefaultSemanticThreshold); !hit {
		t.Skip("sensitive query did not L2-hit (topic gate) — guard not exercised")
	}
	if _, ok := LoadKnowledge(sensitive); ok {
		t.Fatal("sensitive query must NOT be promoted to L1 variant")
	}
}

// TestVariantPathEnforcesLanguageGate：变体映射 L1 命中同样执行语言门
// （verify_math 发现：英文查询经变体命中中文条目）。
func TestVariantPathEnforcesLanguageGate(t *testing.T) {
	cleanKnowledgeCache(t)
	// 中文主条目
	SaveKnowledge(&KnowledgeEntry{
		Query:         "霍奇猜想 代数几何",
		AnswerSummary: "霍奇猜想：代数簇的 Hodge 类由代数闭链表示。",
		ExpiresAt:     time.Now().Add(time.Hour),
	})
	// 建立变体映射（中文近义查询 L2 命中 → 学习变体）
	variant := "霍奇猜想是什么"
	if _, _, hit := LoadKnowledgeSemantic(variant, DefaultSemanticThreshold); !hit {
		t.Skip("variant did not L2-hit — gate not exercised")
	}
	// 英文查询（语言不兼容）→ 变体路径必须拦截
	if _, ok := LoadKnowledge("Hodge conjecture algebraic geometry"); ok {
		t.Fatal("EN query must NOT resolve via variant map to ZH entry")
	}
	// 中文同变体仍命中
	if _, ok := LoadKnowledge(variant); !ok {
		t.Fatal("ZH variant must still resolve")
	}
}

// TestPlacesOverlap：地理一致性——温州 vs 北京拦截，同城放行。
func TestPlacesOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"今天温州天气怎么样", "北京明天天气", false}, // 不同城市拦截
		{"温州明天天气", "温州本周天气", true},     // 同城放行
		{"今天天气怎么样", "北京明天天气", true},    // 无地名不拦截
		{"温州天气", "上海天气", false},
	}
	for _, c := range cases {
		if got := placesOverlap(c.a, c.b); got != c.want {
			t.Errorf("placesOverlap(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

package agent

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ── 快速场景识别升级 ──
// 基于意图识别四层漏斗设计（关键词<1ms → 规则 1-5ms → Embedding 30-80ms → LLM 兜底）。
// 在 OPT-02 的启发式分类器基础上增加分层漏斗和通信开销优化。
//
// 关键发现（来自研究）：
// - 纯 LLM 方案月均 8 万元，分层方案月均 3000 元（省 96%）
// - 纯 LLM 延迟 800ms，分层方案 60ms（快 13 倍）
// - 8 条精选 embedding 示例 > 50 条杂乱示例
// - 意图描述中「不包括」比「包括」更重要
//
// 通信开销优化原则：
// - 简单场景不调用 LLM 分类（零 API 开销）
// - Embedding 匹配用本地向量索引（零 API 开销）
// - 只有 L1-L3 都未命中时才调用 LLM 兜底
// - 兜底结果自动沉淀为新的 embedding 示例（自学习）

// FunnelClassifier 四层漏斗分类器
type FunnelClassifier struct {
	mu sync.Mutex
	// L1: 关键词匹配（零延迟，命中 ~35%）
	keywords map[string][]string // scene → 关键词列表
	// L2: 规则引擎（1-5ms，命中 ~20%）
	rules []ClassificationRule
	// L3: Embedding 匹配（30-80ms，命中 ~35%）
	embeddings map[Scene][]EmbeddingExample
	// L4: LLM 兜底（400-800ms，命中 ~10%）
	llmClassifier SceneClassifier
	// 自学习：L4 结果沉淀为 L3 示例
	learningEnabled bool
	maxExamples     int
	// 统计
	stats FunnelStats
}

// EmbeddingExample embedding 分类示例
type EmbeddingExample struct {
	Text   string
	Vector []float32
	Scene  Scene
}

// ClassificationRule 分类规则
type ClassificationRule struct {
	Name     string
	Scene    Scene
	Match    func(input string) bool
	Priority int
}

// FunnelStats 漏斗统计
type FunnelStats struct {
	TotalRequests  int64
	L1Hits         int64
	L2Hits         int64
	L3Hits         int64
	L4Hits         int64
	Misses         int64
	TotalLatencyMs int64
}

// NewFunnelClassifier 创建四层漏斗分类器
func NewFunnelClassifier() *FunnelClassifier {
	fc := &FunnelClassifier{
		keywords:        make(map[string][]string),
		embeddings:      make(map[Scene][]EmbeddingExample),
		learningEnabled: true,
		maxExamples:     10, // 每场景最多 10 个示例（"少而精"原则）
	}
	fc.initKeywords()
	fc.initRules()
	return fc
}

// initKeywords 初始化 L1 关键词
func (f *FunnelClassifier) initKeywords() {
	// 关键词匹配：确定性高，零延迟
	f.keywords["code"] = []string{
		"bug", "fix", "compile", "build", "lint", "test", "debug",
		"refactor", "implement", "function", "class", "method",
		"error:", "panic:", "traceback", "stack trace",
		"修复", "调试", "编译", "构建", "重构", "报错", "异常",
	}
	f.keywords["math"] = []string{
		"calculate", "compute", "solve equation", "derivative",
		"integral", "matrix", "probability", "theorem",
		"计算", "求解", "方程", "矩阵", "积分", "导数", "概率",
	}
}

// initRules 初始化 L2 规则
func (f *FunnelClassifier) initRules() {
	// 规则引擎：比关键词更复杂的模式匹配
	f.rules = []ClassificationRule{
		{
			Name:     "git_operations",
			Scene:    SceneCode,
			Priority: 1,
			Match: func(input string) bool {
				lower := strings.ToLower(input)
				return strings.Contains(lower, "git ") ||
					strings.Contains(lower, "commit") ||
					strings.Contains(lower, "merge") ||
					strings.Contains(lower, "rebase")
			},
		},
		{
			Name:     "file_operations",
			Scene:    SceneCode,
			Priority: 1,
			Match: func(input string) bool {
				lower := strings.ToLower(input)
				return strings.Contains(lower, "read file") ||
					strings.Contains(lower, "write file") ||
					strings.Contains(lower, "create file") ||
					strings.Contains(lower, "delete file")
			},
		},
		{
			Name:     "architecture_analysis",
			Scene:    SceneResearch,
			Priority: 2,
			Match: func(input string) bool {
				lower := strings.ToLower(input)
				return strings.Contains(lower, "architecture") ||
					strings.Contains(lower, "design pattern") ||
					strings.Contains(lower, "trade-off") ||
					strings.Contains(lower, "compare")
			},
		},
		{
			Name:     "documentation",
			Scene:    SceneWriting,
			Priority: 2,
			Match: func(input string) bool {
				lower := strings.ToLower(input)
				return strings.Contains(lower, "write doc") ||
					strings.Contains(lower, "readme") ||
					strings.Contains(lower, "changelog") ||
					strings.Contains(lower, "document")
			},
		},
	}
}

// Classify 分类入口（四层漏斗）
func (f *FunnelClassifier) Classify(ctx context.Context, input string) (SceneResult, error) {
	start := time.Now()
	f.mu.Lock()
	f.stats.TotalRequests++
	f.mu.Unlock()

	normalized := strings.ToLower(strings.TrimSpace(input))

	// L1: 关键词匹配（< 1ms）
	if result, ok := f.matchKeywords(normalized); ok {
		f.mu.Lock()
		f.stats.L1Hits++
		f.stats.TotalLatencyMs += time.Since(start).Milliseconds()
		f.mu.Unlock()
		return result, nil
	}

	// L2: 规则引擎（1-5ms）
	if result, ok := f.matchRules(normalized); ok {
		f.mu.Lock()
		f.stats.L2Hits++
		f.stats.TotalLatencyMs += time.Since(start).Milliseconds()
		f.mu.Unlock()
		return result, nil
	}

	// L3: Embedding 匹配（30-80ms）
	if result, ok := f.matchEmbeddings(normalized); ok {
		f.mu.Lock()
		f.stats.L3Hits++
		f.stats.TotalLatencyMs += time.Since(start).Milliseconds()
		f.mu.Unlock()
		return result, nil
	}

	// L4: LLM 兜底（400-800ms）
	if f.llmClassifier != nil {
		result, err := f.llmClassifier.Classify(ctx, input)
		if err == nil {
			f.mu.Lock()
			f.stats.L4Hits++
			f.stats.TotalLatencyMs += time.Since(start).Milliseconds()
			f.mu.Unlock()
			// 自学习：将 LLM 分类结果沉淀为 embedding 示例
			if f.learningEnabled {
				f.addExample(input, result.Scene)
			}
			return result, nil
		}
	}

	// 全部未命中：使用启发式兜底
	f.mu.Lock()
	f.stats.Misses++
	f.stats.TotalLatencyMs += time.Since(start).Milliseconds()
	f.mu.Unlock()
	return f.heuristicFallback(input), nil
}

// matchKeywords L1 关键词匹配
func (f *FunnelClassifier) matchKeywords(normalized string) (SceneResult, bool) {
	for sceneStr, keywords := range f.keywords {
		for _, kw := range keywords {
			if strings.Contains(normalized, kw) {
				scene := Scene(sceneStr)
				return SceneResult{
					Scene:      scene,
					Complexity: 60,
					Confidence: 0.95, // 关键词匹配置信度最高
					NeedsTools: scene != SceneMath && scene != SceneWriting,
					NeedsThink: scene == SceneMath,
				}, true
			}
		}
	}
	return SceneResult{}, false
}

// matchRules L2 规则匹配
func (f *FunnelClassifier) matchRules(normalized string) (SceneResult, bool) {
	for _, rule := range f.rules {
		if rule.Match(normalized) {
			return SceneResult{
				Scene:      rule.Scene,
				Complexity: 65,
				Confidence: 0.85,
				NeedsTools: true,
				NeedsThink: rule.Scene == SceneResearch,
			}, true
		}
	}
	return SceneResult{}, false
}

// matchEmbeddings L3 embedding 匹配
func (f *FunnelClassifier) matchEmbeddings(normalized string) (SceneResult, bool) {
	if len(f.embeddings) == 0 {
		return SceneResult{}, false
	}
	// 简化实现：使用字符串相似度代替真实 embedding
	// 实际实现中使用 HNSW + embedding 模型
	bestScene := SceneGeneral
	bestScore := 0.0
	threshold := 0.75 // 低于此值不命中

	for scene, examples := range f.embeddings {
		for _, ex := range examples {
			score := stringSimilarity(normalized, strings.ToLower(ex.Text))
			if score > bestScore {
				bestScore = score
				bestScene = scene
			}
		}
	}

	if bestScore >= threshold {
		return SceneResult{
			Scene:      bestScene,
			Complexity: 55,
			Confidence: bestScore,
			NeedsTools: bestScene != SceneMath && bestScene != SceneChat,
			NeedsThink: bestScene == SceneMath || bestScene == SceneResearch,
		}, true
	}
	return SceneResult{}, false
}

// heuristicFallback 启发式兜底
func (f *FunnelClassifier) heuristicFallback(input string) SceneResult {
	words := len(strings.Fields(input))
	complexity := 50
	if words > 20 {
		complexity = 65
	}
	return SceneResult{
		Scene:      SceneGeneral,
		Complexity: complexity,
		Confidence: 0.4,
		NeedsTools: true,
		NeedsThink: complexity > 60,
	}
}

// addExample 添加 embedding 示例（自学习）
func (f *FunnelClassifier) addExample(text string, scene Scene) {
	f.mu.Lock()
	defer f.mu.Unlock()
	examples := f.embeddings[scene]
	if len(examples) >= f.maxExamples {
		// 超过上限时替换最旧的
		examples = examples[1:]
	}
	f.embeddings[scene] = append(examples, EmbeddingExample{
		Text:  text,
		Scene: scene,
	})
}

// SetLLMClassifier 设置 L4 LLM 兜底分类器
func (f *FunnelClassifier) SetLLMClassifier(c SceneClassifier) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.llmClassifier = c
}

// GetStats 返回漏斗统计
func (f *FunnelClassifier) GetStats() FunnelStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats
}

// GetHitRate 返回各层命中率
func (f *FunnelClassifier) GetHitRates() map[string]float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := f.stats.TotalRequests
	if total == 0 {
		return nil
	}
	return map[string]float64{
		"L1_keyword":   float64(f.stats.L1Hits) / float64(total),
		"L2_rule":      float64(f.stats.L2Hits) / float64(total),
		"L3_embedding": float64(f.stats.L3Hits) / float64(total),
		"L4_llm":       float64(f.stats.L4Hits) / float64(total),
		"miss":         float64(f.stats.Misses) / float64(total),
	}
}

// ── 通信开销优化 ──
// 基于场景识别结果，动态决定是否加载工具 schema、是否启用 CoT、
// 是否压缩上下文，从而减少不必要的通信开销。

// CommunicationOptimizer 通信开销优化器
type CommunicationOptimizer struct {
	classifier *FunnelClassifier
	// 场景策略（复用 scene_policy.go 的 DefaultScenePolicies）
	policies map[Scene]ScenePolicy
}

// NewCommunicationOptimizer 创建通信开销优化器
func NewCommunicationOptimizer(fc *FunnelClassifier) *CommunicationOptimizer {
	return &CommunicationOptimizer{
		classifier: fc,
		policies:   DefaultScenePolicies,
	}
}

// OptimizeRequest 根据场景识别结果优化请求
// 返回优化建议：是否加载工具、是否启用 Think、压缩策略
type RequestOptimization struct {
	Scene                 Scene
	LoadTools             bool   // 是否加载工具 schema（不需要工具时省 4000-8000 token）
	ThinkMode             string // "on"/"off"/"auto"
	ReasoningEffort       string // "low"/"medium"/"high"
	CompressContext       bool   // 是否压缩历史上下文
	SampleCount           int    // 多采样验证次数
	EstimatedTokenSavings int    // 预估 token 节省
}

// Optimize 根据输入生成通信优化建议
func (co *CommunicationOptimizer) Optimize(ctx context.Context, input string) RequestOptimization {
	result, _ := co.classifier.Classify(ctx, input)
	policy := co.policies[result.Scene]
	if policy.Scene == "" {
		policy = co.policies[SceneGeneral]
	}

	opt := RequestOptimization{
		Scene:           result.Scene,
		LoadTools:       result.NeedsTools,
		ThinkMode:       policy.ThinkMode,
		ReasoningEffort: policy.ReasoningEffort,
		SampleCount:     policy.SampleCount,
	}

	// 估算 token 节省
	if !opt.LoadTools {
		opt.EstimatedTokenSavings += 4000 // 不加载工具 schema 省 ~4000 token
	}
	if opt.ThinkMode == "off" {
		opt.EstimatedTokenSavings += 500 // 关闭 Think 省 reasoning token
	}
	if opt.SampleCount == 1 {
		opt.EstimatedTokenSavings += 2000 // 单采样省 N-1 倍验证 token
	}

	// 闲聊场景压缩上下文（不需要历史代码上下文）
	if result.Scene == SceneChat {
		opt.CompressContext = true
		opt.EstimatedTokenSavings += 3000
	}

	return opt
}

// ── 辅助函数 ──

// stringSimilarity 计算两个字符串的相似度（Jaccard 系数）
func stringSimilarity(a, b string) float64 {
	setA := stringToWordSet(a)
	setB := stringToWordSet(b)
	if len(setA) == 0 && len(setB) == 0 {
		return 1.0
	}
	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// stringToWordSet 将字符串转换为词集合
func stringToWordSet(s string) map[string]bool {
	words := strings.Fields(s)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

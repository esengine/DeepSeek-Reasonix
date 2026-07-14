package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"reasonix/internal/provider"
)

// TaskClassifier 判断用户输入是否为任务（需要执行动作）或聊天（对话性质）
type TaskClassifier interface {
	// IsTask 返回 true 表示输入为任务，false 表示聊天
	IsTask(ctx context.Context, input string) (bool, error)
}

// llmClassifier 使用 LLM (Haiku) 进行分类
type llmClassifier struct {
	provider provider.Provider
	cache    *classificationCache
	fallback TaskClassifier
}

// heuristicClassifier 使用启发式规则进行分类（作为 fallback）
type heuristicClassifier struct{}

// classificationCache 会话级别的分类结果缓存
type classificationCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	maxSize int
	ttl     time.Duration
}

type cacheEntry struct {
	isTask    bool
	timestamp time.Time
}

const (
	defaultCacheMaxSize = 100
	defaultCacheTTL     = 5 * time.Minute
)

// 分类系统 prompt
const classificationSystemPrompt = `You are a classifier that determines whether user input is a "task" (requires action/execution) or "chat" (conversational/greeting).

Task: Any input that asks the assistant to write code, fix bugs, run commands, analyze code, create files, or perform any action.
Chat: Greetings, acknowledgments, confirmations, questions about the assistant itself, or purely conversational inputs.

Respond with ONLY one word: "task" or "chat"

Examples:
- "fix the auth bug" → task
- "create a component" → task
- "run tests" → task
- "the login isn't working" → task
- "can you help with this error?" → task
- "继续处理" → task
- "修复这个问题" → task
- "hello" → chat
- "thanks" → chat
- "ok" → chat
- "I see" → chat
- "你好" → chat
- "谢谢" → chat
- "thanks for fixing that" → chat`

// newLLMClassifier 创建新的 LLM 分类器
func newLLMClassifier(prov provider.Provider, fallback TaskClassifier) *llmClassifier {
	return &llmClassifier{
		provider: prov,
		cache:    newClassificationCache(),
		fallback: fallback,
	}
}

// IsTask 使用 LLM 判断输入是否为任务
func (c *llmClassifier) IsTask(ctx context.Context, input string) (bool, error) {
	// 1. 检查缓存
	if cached, ok := c.cache.Get(input); ok {
		return cached, nil
	}

	// 2. 调用 LLM 进行分类（使用 2s 超时）
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: classificationSystemPrompt},
			{Role: provider.RoleUser, Content: "Classify: " + input},
		},
		MaxTokens:   10,
		Temperature: provider.TemperaturePtr(0),
	}

	ch, err := c.provider.Stream(ctx, req)
	if err != nil {
		// fallback to heuristic
		return c.fallback.IsTask(ctx, input)
	}

	var response strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			// fallback to heuristic
			return c.fallback.IsTask(ctx, input)
		}
		response.WriteString(chunk.Text)
	}

	// 3. 解析响应
	result := strings.ToLower(strings.TrimSpace(response.String()))
	isTask := strings.Contains(result, "task")

	// 4. 缓存结果
	c.cache.Set(input, isTask)

	return isTask, nil
}

// newHeuristicClassifier 创建新的启发式分类器
func newHeuristicClassifier() *heuristicClassifier {
	return &heuristicClassifier{}
}

// IsTask 使用启发式规则判断输入是否为任务
func (h *heuristicClassifier) IsTask(ctx context.Context, input string) (bool, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false, nil
	}

	normalized := strings.ToLower(strings.Trim(trimmed, " \t\r\n.!?。！？,，;；:："))

	// 1. 非常短的问候语白名单（1-3 个词）
	shortGreetings := []string{
		"hello", "hi", "hey", "你好", "您好", "nihao",
		"thanks", "thank you", "谢谢", "谢了",
		"ok", "okay", "好的", "嗯", "行",
		"got it", "i see", "明白", "了解", "收到", "我知道了", "先不用",
	}

	words := strings.Fields(normalized)
	if len(words) <= 3 {
		for _, greeting := range shortGreetings {
			if normalized == greeting {
				return false, nil
			}
		}
	}

	// 2. Polite acknowledgements can contain action words from the completed
	// task ("thanks for fixing") but should stay conversational.
	chatPhrases := []string{
		"thanks for", "thank you for", "i'll check later", "i will check later",
		"i'll test it later", "i will test it later", "that test was helpful", "the test was helpful",
		"谢谢你", "辛苦了",
	}
	for _, phrase := range chatPhrases {
		if strings.Contains(normalized, phrase) {
			return false, nil
		}
	}

	// 3. 文件引用检测（任务的强信号）
	if strings.Contains(trimmed, "@") || strings.Contains(trimmed, ".go") ||
		strings.Contains(trimmed, ".js") || strings.Contains(trimmed, ".py") ||
		strings.Contains(trimmed, ".ts") {
		return true, nil
	}

	// 4. Failure/help descriptions are actionable even when phrased without an
	// imperative verb, e.g. "the auth isn't working".
	taskPhrases := []string{
		"not working", "isn't working", "doesn't work", "dont work", "don't work",
		"can you help", "help with", "broken", "error", "bug", "issue", "failed", "failing", "crash", "cannot", "can't",
		"问题", "不工作", "无法", "不能", "报错", "错误", "失败", "崩溃", "异常",
		"卡住", "卡住了", "没反应", "不生效", "异常退出",
	}
	for _, phrase := range taskPhrases {
		if strings.Contains(normalized, phrase) {
			return true, nil
		}
	}

	// 5. 动作关键词检测
	actionNeedles := []string{
		"fix", "debug", "repair", "resolve", "reproduce",
		"create", "add", "write", "edit", "update", "change", "delete", "remove", "rename",
		"review", "inspect", "analyze", "check", "test", "run", "build", "implement", "refactor",
		"continue work", "continue the", "continue this",
		"修复", "调试", "解决", "复现", "创建", "新建", "添加", "编写", "编辑", "修改", "更新",
		"删除", "移除", "重命名", "评审", "检查", "分析", "测试", "运行", "构建", "实现", "重构", "继续处理",
		"看看", "看下", "帮我看", "帮我看下", "处理下", "处理一下", "排查", "定位",
	}

	for _, needle := range actionNeedles {
		if containsTaskNeedle(normalized, needle) {
			return true, nil
		}
	}

	// 默认：不确定时，根据长度判断
	// 理由：false negative（任务→聊天）比 false positive（聊天→任务）更严重
	// 短的模糊输入 → 聊天；长的输入 → 任务
	return len(words) > 5, nil
}

func containsTaskNeedle(input, needle string) bool {
	if needle == "" {
		return false
	}
	if containsNonASCII(needle) || strings.Contains(needle, " ") {
		return strings.Contains(input, needle)
	}
	for _, word := range strings.FieldsFunc(input, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_'
	}) {
		if word == needle {
			return true
		}
	}
	return false
}

func containsNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

// newClassificationCache 创建新的分类缓存
func newClassificationCache() *classificationCache {
	return &classificationCache{
		entries: make(map[string]cacheEntry),
		maxSize: defaultCacheMaxSize,
		ttl:     defaultCacheTTL,
	}
}

// Get 从缓存中获取分类结果
func (c *classificationCache) Get(input string) (bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := normalizeInputForCache(input)
	entry, ok := c.entries[key]
	if !ok {
		return false, false
	}

	// 检查 TTL
	if time.Since(entry.timestamp) > c.ttl {
		return false, false
	}

	return entry.isTask, true
}

// Set 将分类结果存入缓存
func (c *classificationCache) Set(input string, isTask bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := normalizeInputForCache(input)

	// LRU 淘汰：如果缓存满了，清除最老的 20% 条目
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[key] = cacheEntry{
		isTask:    isTask,
		timestamp: time.Now(),
	}
}

// Clear 清除所有缓存
func (c *classificationCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]cacheEntry)
}

// evictOldest 淘汰最老的 20% 条目（LRU）
func (c *classificationCache) evictOldest() {
	// 找出所有条目按时间排序
	type entry struct {
		key       string
		timestamp time.Time
	}
	var entries []entry
	for k, v := range c.entries {
		entries = append(entries, entry{key: k, timestamp: v.timestamp})
	}

	// 如果条目少，不淘汰
	if len(entries) == 0 {
		return
	}

	// 按时间排序
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].timestamp.After(entries[j].timestamp) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// 删除最老的 20%
	toDelete := len(entries) / 5
	if toDelete == 0 {
		toDelete = 1
	}
	for i := 0; i < toDelete && i < len(entries); i++ {
		delete(c.entries, entries[i].key)
	}
}

// normalizeInputForCache 规范化输入用于缓存键
func normalizeInputForCache(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))
	// 折叠内部空白，保留结构
	normalized = strings.Join(strings.Fields(normalized), " ")
	// 使用 SHA256 hash 作为键，避免超长键
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

// ── OPT-02: 场景识别器 ──
// 将二分类（task/chat）升级为多场景识别，驱动后续优化：
// - Scene → B3 场景化策略（Think On/Off, Pride 容忍度）
// - Complexity → E3 模型路由（简单用 Flash，复杂用 Pro）
// - NeedsThink → reasoning_effort 控制
// - NeedsTools → token economy（不需要工具时不加载工具 schema）

// Scene 表示任务场景类型
type Scene string

const (
	SceneCode     Scene = "code"     // 代码生成/审查/重构
	SceneResearch Scene = "research" // 研究分析/代码库探索
	SceneWriting  Scene = "writing"  // 文档/创意写作
	SceneMath     Scene = "math"     // 数学推理
	SceneFactQA   Scene = "fact_qa"  // 事实问答
	SceneChat     Scene = "chat"     // 闲聊
	SceneGeneral  Scene = "general"  // 通用
)

// SceneResult 场景识别结果
type SceneResult struct {
	Scene      Scene
	Complexity int     // 0-100
	Confidence float64 // 0-1
	NeedsTools bool
	NeedsThink bool
}

// SceneClassifier 场景识别器接口
type SceneClassifier interface {
	Classify(ctx context.Context, input string) (SceneResult, error)
}

// sceneClassificationPrompt 场景分类 system prompt
const sceneClassificationPrompt = `You are a task scene classifier. Given user input, classify it into one of:
- code: writing/reviewing/refactoring code, fixing bugs, running commands
- research: analyzing codebase, comparing approaches, investigating issues
- writing: documentation, creative writing, content generation
- math: mathematical reasoning, calculations, proofs
- fact_qa: factual questions requiring precise answers
- chat: greetings, acknowledgments, conversational

Respond in JSON only: {"scene":"code","complexity":75,"needs_tools":true,"needs_think":false}
complexity: 0-100 (higher = more reasoning needed)
needs_tools: true if the task requires tool calls (file ops, commands, search)
needs_think: true if the task benefits from chain-of-thought reasoning`

// heuristicSceneClassifier 启发式场景识别器（不调用 LLM，零延迟）
type heuristicSceneClassifier struct{}

// NewHeuristicSceneClassifier 创建启发式场景识别器
func NewHeuristicSceneClassifier() SceneClassifier {
	return &heuristicSceneClassifier{}
}

// Classify 使用启发式规则识别场景
func (h *heuristicSceneClassifier) Classify(ctx context.Context, input string) (SceneResult, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return SceneResult{Scene: SceneChat, Complexity: 0, Confidence: 1.0}, nil
	}

	normalized := strings.ToLower(trimmed)

	// 1. 闲聊检测（复用已有规则）
	if isChatInput(normalized) {
		return SceneResult{Scene: SceneChat, Complexity: 5, Confidence: 0.9, NeedsTools: false, NeedsThink: false}, nil
	}

	// 2. 代码场景检测
	codeSignals := []string{".go", ".rs", ".ts", ".js", ".py", ".java", ".c", ".cpp", ".h",
		"func ", "func(", "import ", "package ", "class ", "def ", "fn ", "pub fn",
		"compile", "build", "lint", "test", "debug", "refactor", "bug", "stack trace",
		"panic:", "error:", "nil pointer", "segmentation fault",
		"修复", "调试", "编译", "构建", "重构", "报错", "异常"}
	for _, sig := range codeSignals {
		if strings.Contains(normalized, sig) {
			complexity := 60
			needsThink := false
			if strings.Contains(normalized, "debug") || strings.Contains(normalized, "调试") ||
				strings.Contains(normalized, "bug") || strings.Contains(normalized, "报错") {
				complexity = 75 // 调试更复杂
				needsThink = true
			}
			return SceneResult{Scene: SceneCode, Complexity: complexity, Confidence: 0.85, NeedsTools: true, NeedsThink: needsThink}, nil
		}
	}

	// 3. 数学场景检测
	mathSignals := []string{"calculate", "compute", "solve", "equation", "matrix", "integral",
		"derivative", "probability", "theorem", "proof", "algorithm complexity",
		"计算", "求解", "方程", "矩阵", "积分", "导数", "概率", "证明"}
	for _, sig := range mathSignals {
		if strings.Contains(normalized, sig) {
			return SceneResult{Scene: SceneMath, Complexity: 80, Confidence: 0.85, NeedsTools: false, NeedsThink: true}, nil
		}
	}

	// 4. 研究场景检测
	researchSignals := []string{"analyze", "compare", "investigate", "explore", "understand",
		"architecture", "design pattern", "trade-off", "evaluate",
		"分析", "对比", "调查", "探索", "理解", "架构", "评估"}
	for _, sig := range researchSignals {
		if strings.Contains(normalized, sig) {
			return SceneResult{Scene: SceneResearch, Complexity: 70, Confidence: 0.75, NeedsTools: true, NeedsThink: true}, nil
		}
	}

	// 5. 写作场景检测
	writingSignals := []string{"write", "document", "draft", "compose", "article", "blog",
		"readme", "changelog", "documentation",
		"写", "文档", "起草", "撰写", "文章"}
	for _, sig := range writingSignals {
		if strings.Contains(normalized, sig) {
			return SceneResult{Scene: SceneWriting, Complexity: 40, Confidence: 0.7, NeedsTools: false, NeedsThink: false}, nil
		}
	}

	// 6. 事实问答检测
	factSignals := []string{"what is", "what are", "how does", "why does", "when did",
		"who is", "where is", "explain",
		"什么是", "什么是", "为什么", "什么时候", "解释"}
	for _, sig := range factSignals {
		if strings.Contains(normalized, sig) {
			return SceneResult{Scene: SceneFactQA, Complexity: 30, Confidence: 0.65, NeedsTools: false, NeedsThink: false}, nil
		}
	}

	// 7. 默认通用
	wordCount := len(strings.Fields(trimmed))
	complexity := 50
	if wordCount > 20 {
		complexity = 65
	}
	return SceneResult{Scene: SceneGeneral, Complexity: complexity, Confidence: 0.5, NeedsTools: true, NeedsThink: complexity > 60}, nil
}

// isChatInput 检测是否为闲聊输入
func isChatInput(normalized string) bool {
	shortGreetings := []string{
		"hello", "hi", "hey", "你好", "您好", "nihao",
		"thanks", "thank you", "谢谢", "谢了",
		"ok", "okay", "好的", "嗯", "行",
		"got it", "i see", "明白", "了解", "收到", "我知道了",
	}
	words := strings.Fields(normalized)
	if len(words) <= 3 {
		for _, g := range shortGreetings {
			if normalized == g {
				return true
			}
		}
	}
	chatPhrases := []string{
		"thanks for", "thank you for", "i'll check later",
		"谢谢你", "辛苦了",
	}
	for _, p := range chatPhrases {
		if strings.Contains(normalized, p) {
			return true
		}
	}
	return false
}

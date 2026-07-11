package agent

import (
	"strings"
)

// ── P0-6: 语义压缩代码保护 ──
// 问题：C2 通信优化的 LLMLingua 语义压缩会破坏代码内容，
//       导致代码格式损坏、编译失败。
// 方案：在压缩前识别代码块，跳过代码内容只压缩自然语言。
//
// 与 C2 通信优化引擎协作：C2 在压缩消息前调用 CodeAwareCompressor，
// 代码块被标记为"不可压缩"，自然语言部分正常压缩。

// CodeBlock 表示消息中的一个代码块
type CodeBlock struct {
	Language string // 代码语言（go, rust, python 等）
	Content  string // 代码内容
	Start    int    // 在原始消息中的起始位置
	End      int    // 在原始消息中的结束位置
}

// ExtractCodeBlocks 从消息中提取代码块
// 支持 ```fenced``` 和缩进式代码块
func ExtractCodeBlocks(text string) []CodeBlock {
	var blocks []CodeBlock
	lines := strings.Split(text, "\n")

	i := 0
	pos := 0 // 当前在原始文本中的位置
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// 检测 fenced code block: ```language
		if strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimPrefix(trimmed, "```")
			lang = strings.TrimSpace(lang)
			startPos := pos
			contentStart := i + 1
			i++
			pos += len(line) + 1 // +1 for \n

			// 寻找结束的 ```
			found := false
			for i < len(lines) {
				line := lines[i]
				if strings.TrimSpace(line) == "```" {
					// 找到结束标记
					content := strings.Join(lines[contentStart:i], "\n")
					blocks = append(blocks, CodeBlock{
						Language: lang,
						Content:  content,
						Start:    startPos,
						End:      pos + len(line),
					})
					pos += len(line) + 1
					i++
					found = true
					break
				}
				pos += len(line) + 1
				i++
			}
			if !found {
				// 未闭合的代码块，保守处理：将剩余内容视为代码
				content := strings.Join(lines[contentStart:], "\n")
				blocks = append(blocks, CodeBlock{
					Language: lang,
					Content:  content,
					Start:    startPos,
					End:      len(text),
				})
				return blocks
			}
		} else {
			pos += len(line) + 1
			i++
		}
	}
	return blocks
}

// IsInCodeBlock 检查位置 pos 是否在代码块内
func IsInCodeBlock(text string, pos int) bool {
	blocks := ExtractCodeBlocks(text)
	for _, b := range blocks {
		if pos >= b.Start && pos < b.End {
			return true
		}
	}
	return false
}

// CodeAwareCompressor 代码感知压缩器
// 在压缩消息时跳过代码块，只压缩自然语言部分
type CodeAwareCompressor struct {
	// CompressFunc 是自然语言压缩函数（由 C2 提供）
	CompressFunc func(text string) (string, error)
}

// NewCodeAwareCompressor 创建代码感知压缩器
func NewCodeAwareCompressor(compressFunc func(string) (string, error)) *CodeAwareCompressor {
	return &CodeAwareCompressor{CompressFunc: compressFunc}
}

// Compress 压缩消息，跳过代码块
func (c *CodeAwareCompressor) Compress(text string) (string, error) {
	if c.CompressFunc == nil {
		return text, nil // 无压缩函数，返回原文
	}

	blocks := ExtractCodeBlocks(text)
	if len(blocks) == 0 {
		// 无代码块，直接压缩全部
		return c.CompressFunc(text)
	}

	var result strings.Builder
	lastEnd := 0
	for _, block := range blocks {
		// 压缩代码块之前的自然语言部分
		if block.Start > lastEnd {
			naturalText := text[lastEnd:block.Start]
			compressed, err := c.CompressFunc(naturalText)
			if err != nil {
				// 压缩失败，保留原文
				result.WriteString(naturalText)
			} else {
				result.WriteString(compressed)
			}
		}
		// 代码块原样保留
		result.WriteString(text[block.Start:block.End])
		lastEnd = block.End
	}
	// 压缩最后一段自然语言
	if lastEnd < len(text) {
		naturalText := text[lastEnd:]
		compressed, err := c.CompressFunc(naturalText)
		if err != nil {
			result.WriteString(naturalText)
		} else {
			result.WriteString(compressed)
		}
	}
	return result.String(), nil
}

// ── P0-7: A1↔B3 场景触发接口 ──
// 问题：B3 场景化策略表定义了不同场景的行为策略（Pride 容忍度、Think On/Off），
//       但 A1 编排核心没有调用 B3 的接口，导致防 Pride 机制无法生效。
// 方案：在 Agent 中注入 ScenePolicyProvider，每轮 LLM 调用前查询当前场景策略。

// ScenePolicy 场景化行为策略（与 B3 场景化策略表对齐）
type ScenePolicy struct {
	Scene               Scene   // 场景类型
	MaxPrideSignals     int     // Pride 信号容忍上限
	ThinkMode           string  // "on", "off", "auto"
	ReasoningEffort     string  // "low", "medium", "high"
	SampleCount         int     // 多采样验证次数
	ConfidenceThreshold float64 // 置信度阈值
}

// ScenePolicyProvider 场景策略提供者接口（B3 实现）
type ScenePolicyProvider interface {
	// GetPolicy 根据场景返回行为策略
	GetPolicy(scene Scene) ScenePolicy
	// DetectPride 检测文本中的 Pride 信号
	DetectPride(text string) []PrideSignal
}

// PrideSignal 表示检测到的 Pride 信号
type PrideSignal struct {
	Type     string  // 信号类型（assertion, superlative, dismissive 等）
	Text     string  // 匹配的文本
	Position int     // 位置
	Severity float64 // 严重程度 0-1
}

// DefaultScenePolicies 默认场景策略表
var DefaultScenePolicies = map[Scene]ScenePolicy{
	SceneCode: {
		Scene:               SceneCode,
		MaxPrideSignals:     2,
		ThinkMode:           "auto",
		ReasoningEffort:     "medium",
		SampleCount:         1,
		ConfidenceThreshold: 0.7,
	},
	SceneResearch: {
		Scene:               SceneResearch,
		MaxPrideSignals:     1,
		ThinkMode:           "on",
		ReasoningEffort:     "high",
		SampleCount:         2,
		ConfidenceThreshold: 0.8,
	},
	SceneWriting: {
		Scene:               SceneWriting,
		MaxPrideSignals:     3,
		ThinkMode:           "off",
		ReasoningEffort:     "low",
		SampleCount:         1,
		ConfidenceThreshold: 0.5,
	},
	SceneMath: {
		Scene:               SceneMath,
		MaxPrideSignals:     0, // 数学不允许 Pride
		ThinkMode:           "on",
		ReasoningEffort:     "high",
		SampleCount:         3, // 数学需要多采样验证
		ConfidenceThreshold: 0.9,
	},
	SceneFactQA: {
		Scene:               SceneFactQA,
		MaxPrideSignals:     0, // 事实问答不允许 Pride
		ThinkMode:           "off",
		ReasoningEffort:     "low",
		SampleCount:         1,
		ConfidenceThreshold: 0.85,
	},
	SceneChat: {
		Scene:               SceneChat,
		MaxPrideSignals:     5, // 闲聊最宽松
		ThinkMode:           "off",
		ReasoningEffort:     "low",
		SampleCount:         1,
		ConfidenceThreshold: 0.3,
	},
	SceneGeneral: {
		Scene:               SceneGeneral,
		MaxPrideSignals:     2,
		ThinkMode:           "auto",
		ReasoningEffort:     "medium",
		SampleCount:         1,
		ConfidenceThreshold: 0.7,
	},
}

// DefaultScenePolicyProvider 默认场景策略提供者
type DefaultScenePolicyProvider struct {
	policies map[Scene]ScenePolicy
}

// NewDefaultScenePolicyProvider 创建默认场景策略提供者
func NewDefaultScenePolicyProvider() *DefaultScenePolicyProvider {
	// 深拷贝默认策略
	policies := make(map[Scene]ScenePolicy, len(DefaultScenePolicies))
	for k, v := range DefaultScenePolicies {
		policies[k] = v
	}
	return &DefaultScenePolicyProvider{policies: policies}
}

// GetPolicy 返回指定场景的行为策略
func (p *DefaultScenePolicyProvider) GetPolicy(scene Scene) ScenePolicy {
	if policy, ok := p.policies[scene]; ok {
		return policy
	}
	// 未知场景使用通用策略
	return p.policies[SceneGeneral]
}

// DetectPride 检测文本中的 Pride 信号（启发式）
func (p *DefaultScenePolicyProvider) DetectPride(text string) []PrideSignal {
	if text == "" {
		return nil
	}
	var signals []PrideSignal
	lower := strings.ToLower(text)

	// Pride 信号词表（中英文）
	pridePatterns := []struct {
		Type     string
		Patterns []string
		Severity float64
	}{
		{"superlative", []string{
			"obviously", "clearly", "undoubtedly", "certainly", "definitely",
			"of course", "as everyone knows", "it goes without saying",
			"完美", "绝对", "毫无疑问", "显而易见", "当然",
		}, 0.3},
		{"assertion", []string{
			"i can guarantee", "i'm certain", "i'm sure", "trust me",
			"i promise", "i assure you",
			"我保证", "我确信", "相信我",
		}, 0.5},
		{"dismissive", []string{
			"that's trivial", "that's simple", "just do",
			"anyone can", "it's basic",
			"很简单", "很容易", "随便",
		}, 0.4},
		{"overconfident", []string{
			"100% correct", "perfect solution", "best approach",
			"no better way", "impossible to fail",
			"完全正确", "完美方案", "最佳方案",
		}, 0.7},
	}

	for _, pattern := range pridePatterns {
		for _, p := range pattern.Patterns {
			idx := strings.Index(lower, p)
			if idx >= 0 {
				signals = append(signals, PrideSignal{
					Type:     pattern.Type,
					Text:     text[idx : idx+len(p)],
					Position: idx,
					Severity: pattern.Severity,
				})
			}
		}
	}
	return signals
}

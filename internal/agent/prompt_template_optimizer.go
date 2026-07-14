package agent

import "sync"

// ── OPT-129: PromptTemplateOptimizer (Prompt 模板优化器) ──
// 优化重复使用的 prompt 模板，通过去除多余空行、压缩连续空格、
// 移除注释行来减少 token 消耗。
//
// 原理：prompt 模板中常包含不影响语义的冗余空白和注释，这些内容
// 会消耗额外 token。PromptTemplateOptimizer 对注册的模板应用一系列
// 优化规则，在保留语义的前提下压缩模板体积。
//
// 效果：减少 prompt 模板的 token 占用，降低每次调用的成本。

// PromptTemplateOptimizer 优化重复使用的 prompt 模板。
type PromptTemplateOptimizer struct {
	mu                sync.RWMutex
	templates         map[string]string
	totalOptimized    int
	totalTokensSaved  int
	optimizationRules map[string]int
}

// NewPromptTemplateOptimizer 创建一个新的 PromptTemplateOptimizer。
func NewPromptTemplateOptimizer() *PromptTemplateOptimizer {
	return &PromptTemplateOptimizer{
		templates:         make(map[string]string),
		optimizationRules: make(map[string]int),
	}
}

// RegisterTemplate 注册一个 prompt 模板。
// 若同名模板已存在，则覆盖。
func (p *PromptTemplateOptimizer) RegisterTemplate(name string, template string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.templates[name] = template
}

// OptimizeTemplate 优化指定名称的模板。
// 优化规则：去除多余空行、压缩连续空格、移除注释行。
// 优化后的模板会替换原模板存储，并记录 token 节省量。
// 返回优化后的模板内容；若模板不存在则返回空字符串。
func (p *PromptTemplateOptimizer) OptimizeTemplate(name string) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	original, ok := p.templates[name]
	if !ok {
		return ""
	}

	// 依次应用优化规则
	afterComments := ptoRemoveCommentLines(original)
	if len(afterComments) != len(original) {
		p.optimizationRules["remove_comments"]++
	}

	afterSpaces := ptoCompressSpaces(afterComments)
	if len(afterSpaces) != len(afterComments) {
		p.optimizationRules["compress_spaces"]++
	}

	afterBlanks := ptoRemoveBlankLines(afterSpaces)
	if len(afterBlanks) != len(afterSpaces) {
		p.optimizationRules["remove_blank_lines"]++
	}

	optimized := afterBlanks

	// 统计 token 节省量
	saved := ptoEstimateTokens(original) - ptoEstimateTokens(optimized)
	if saved < 0 {
		saved = 0
	}
	p.totalTokensSaved += saved
	p.totalOptimized++

	// 更新存储的模板为优化后的版本
	p.templates[name] = optimized

	return optimized
}

// GetTemplate 获取指定名称的模板。
// 若模板已优化过，返回优化后的版本。
// 返回模板内容和是否存在标志。
func (p *PromptTemplateOptimizer) GetTemplate(name string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	tmpl, ok := p.templates[name]
	return tmpl, ok
}

// EstimateSavings 估算优化指定模板后的 token 节省量。
// 基于当前存储的模板内容计算，若模板已优化过则节省量为 0。
func (p *PromptTemplateOptimizer) EstimateSavings(name string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	original, ok := p.templates[name]
	if !ok {
		return 0
	}
	optimized := ptoOptimizeText(original)
	saved := ptoEstimateTokens(original) - ptoEstimateTokens(optimized)
	if saved < 0 {
		return 0
	}
	return saved
}

// GetStats 返回优化器的统计信息。
func (p *PromptTemplateOptimizer) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	avgSavings := 0
	if p.totalOptimized > 0 {
		avgSavings = p.totalTokensSaved / p.totalOptimized
	}

	return map[string]interface{}{
		"totalOptimized":   p.totalOptimized,
		"totalTokensSaved": p.totalTokensSaved,
		"templateCount":    len(p.templates),
		"avgSavings":       avgSavings,
	}
}

// Reset 清除所有模板和统计信息。
func (p *PromptTemplateOptimizer) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.templates = make(map[string]string)
	p.optimizationRules = make(map[string]int)
	p.totalOptimized = 0
	p.totalTokensSaved = 0
}

// ptoOptimizeText 对模板文本应用全部优化规则，返回优化后的文本。
func ptoOptimizeText(template string) string {
	s := ptoRemoveCommentLines(template)
	s = ptoCompressSpaces(s)
	s = ptoRemoveBlankLines(s)
	return s
}

// ptoSplitLines 按换行符将字符串拆分为行列表。
func ptoSplitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

// ptoJoinLines 将行列表用换行符连接为字符串。
func ptoJoinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

// ptoTrimLeft 去除行首的空格、制表符和回车符。
func ptoTrimLeft(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	return s[i:]
}

// ptoIsBlankLine 判断一行是否为空白行（仅含空格、制表符、回车）。
func ptoIsBlankLine(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' && line[i] != '\r' {
			return false
		}
	}
	return true
}

// ptoStartsWithComment 判断字符串是否以注释标记（# 或 //）开头。
func ptoStartsWithComment(s string) bool {
	if len(s) >= 1 && s[0] == '#' {
		return true
	}
	if len(s) >= 2 && s[0] == '/' && s[1] == '/' {
		return true
	}
	return false
}

// ptoRemoveCommentLines 移除以 # 或 // 开头的注释行。
func ptoRemoveCommentLines(s string) string {
	lines := ptoSplitLines(s)
	var kept []string
	for _, line := range lines {
		trimmed := ptoTrimLeft(line)
		if ptoStartsWithComment(trimmed) {
			continue
		}
		kept = append(kept, line)
	}
	return ptoJoinLines(kept)
}

// ptoCompressLineSpaces 压缩一行中的连续空格/制表符为单个空格。
// 行首和行尾的空格也会被压缩或去除。
func ptoCompressLineSpaces(line string) string {
	result := ""
	inSpace := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == ' ' || c == '\t' {
			inSpace = true
		} else {
			if inSpace {
				result += " "
				inSpace = false
			}
			result += string(c)
		}
	}
	// 不追加末尾的空格（去除行尾空格）
	return result
}

// ptoCompressSpaces 压缩每行中的连续空格/制表符为单个空格。
func ptoCompressSpaces(s string) string {
	lines := ptoSplitLines(s)
	var result []string
	for _, line := range lines {
		result = append(result, ptoCompressLineSpaces(line))
	}
	return ptoJoinLines(result)
}

// ptoRemoveBlankLines 移除空白行。
func ptoRemoveBlankLines(s string) string {
	lines := ptoSplitLines(s)
	var kept []string
	for _, line := range lines {
		if !ptoIsBlankLine(line) {
			kept = append(kept, line)
		}
	}
	return ptoJoinLines(kept)
}

// ptoEstimateTokens 估算字符串的 token 数量（约每 4 个字符 1 个 token）。
func ptoEstimateTokens(s string) int {
	return len(s) / 4
}

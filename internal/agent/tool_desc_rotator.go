package agent

import (
	"sync"
	"time"
)

// ── OPT-30: 动态工具描述轮换 (Dynamic Tool Description Rotation) ──
// 工具首次使用后切换为精简描述，减少后续请求的 schema token。
//
// 原理：工具的完整描述（用法示例、注意事项）在首次调用时有用，
// 但模型一旦理解工具用法后，完整描述就是浪费 token。通过轮换：
// 1. 首次请求：完整描述（~200 token/tool）
// 2. 首次调用后：精简描述（~50 token/tool）
// 3. 如果模型调用错误，临时恢复完整描述
//
// 效果：15 个工具的 schema token 从 3000 降到 750（省 75%），
// 在第二次请求后持续生效。

// ToolDescriptionRotator 工具描述轮换器
type ToolDescriptionRotator struct {
	mu sync.RWMutex

	// 工具使用状态
	toolStates map[string]*ToolDescriptionState

	// 精简描述模板
	compactDescriptions map[string]string

	// 统计
	totalRotated int
	totalSaved   int
}

// ToolDescriptionState 工具描述状态
type ToolDescriptionState struct {
	Name           string    `json:"name"`
	FullDesc       string    `json:"fullDesc"`
	CompactDesc    string    `json:"compactDesc"`
	UsedCount      int       `json:"usedCount"`
	Rotated        bool      `json:"rotated"`
	LastUsed       time.Time `json:"lastUsed"`
	ErrorCount     int       `json:"errorCount"`
	ForceFull      bool      `json:"forceFull"` // 错误时临时恢复
	ForceFullUntil time.Time `json:"forceFullUntil"`
}

// NewToolDescriptionRotator 创建轮换器
func NewToolDescriptionRotator() *ToolDescriptionRotator {
	return &ToolDescriptionRotator{
		toolStates:          make(map[string]*ToolDescriptionState),
		compactDescriptions: getDefaultCompactDescriptions(),
	}
}

// getDefaultCompactDescriptions 获取默认精简描述
func getDefaultCompactDescriptions() map[string]string {
	return map[string]string{
		"bash":        "Execute shell command. Args: {command, timeout}",
		"read_file":   "Read file contents. Args: {path, offset, limit}",
		"write_file":  "Write file. Args: {path, content}",
		"edit_file":   "Edit file with search/replace. Args: {path, old_str, new_str}",
		"multi_edit":  "Multiple edits in one call. Args: {path, edits}",
		"grep":        "Search file contents. Args: {pattern, path, glob}",
		"glob":        "Find files by pattern. Args: {pattern, path}",
		"ls":          "List directory. Args: {path}",
		"code_index":  "Search code index. Args: {query}",
		"web_search":  "Search the web. Args: {query, num}",
		"web_fetch":   "Fetch URL content. Args: {url}",
		"todo_write":  "Write todo list. Args: {todos}",
		"task":        "Launch sub-agent. Args: {description, prompt}",
		"mcp":         "Call MCP tool. Args: {server, tool, args}",
		"interactive": "Ask user question. Args: {question, options}",
	}
}

// Register 注册工具的完整描述
func (r *ToolDescriptionRotator) Register(name, fullDesc string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	compact := r.compactDescriptions[name]
	if compact == "" {
		// 没有预设精简描述，生成一个
		compact = name + " tool"
	}

	r.toolStates[name] = &ToolDescriptionState{
		Name:        name,
		FullDesc:    fullDesc,
		CompactDesc: compact,
		UsedCount:   0,
		Rotated:     false,
	}
}

// GetDescription 获取当前应该使用的描述
func (r *ToolDescriptionRotator) GetDescription(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state, ok := r.toolStates[name]
	if !ok {
		return "" // 未注册，由调用者提供
	}

	// 检查是否需要恢复完整描述（错误后临时恢复）
	if state.ForceFull && time.Now().Before(state.ForceFullUntil) {
		return state.FullDesc
	}

	if state.Rotated {
		return state.CompactDesc
	}

	return state.FullDesc
}

// RecordUsage 记录工具使用
func (r *ToolDescriptionRotator) RecordUsage(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.toolStates[name]
	if !ok {
		return
	}

	state.UsedCount++
	state.LastUsed = time.Now()

	// 首次使用后轮换为精简描述
	if !state.Rotated && state.UsedCount >= 1 {
		state.Rotated = true
		r.totalRotated++
		// 估算节省的 token（完整描述 - 精简描述的字符差 / 4）
		saved := (len(state.FullDesc) - len(state.CompactDesc)) / 4
		if saved > 0 {
			r.totalSaved += saved
		}
	}
}

// RecordError 记录工具调用错误，临时恢复完整描述
func (r *ToolDescriptionRotator) RecordError(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.toolStates[name]
	if !ok {
		return
	}

	state.ErrorCount++
	// 连续 2 次错误才恢复完整描述
	if state.ErrorCount >= 2 {
		state.ForceFull = true
		state.ForceFullUntil = time.Now().Add(5 * time.Minute)
	}
}

// RecordSuccess 记录成功调用，清除错误状态
func (r *ToolDescriptionRotator) RecordSuccess(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.toolStates[name]
	if !ok {
		return
	}

	state.ErrorCount = 0
	state.ForceFull = false
}

// GetStats 获取统计
func (r *ToolDescriptionRotator) GetStats() RotatorStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rotated := 0
	full := 0
	for _, s := range r.toolStates {
		if s.Rotated {
			rotated++
		} else {
			full++
		}
	}

	return RotatorStats{
		TotalTools:    len(r.toolStates),
		RotatedTools:  rotated,
		FullDescTools: full,
		TotalRotated:  r.totalRotated,
		TokensSaved:   r.totalSaved,
	}
}

// RotatorStats 轮换统计
type RotatorStats struct {
	TotalTools    int `json:"totalTools"`
	RotatedTools  int `json:"rotatedTools"`
	FullDescTools int `json:"fullDescTools"`
	TotalRotated  int `json:"totalRotated"`
	TokensSaved   int `json:"tokensSaved"`
}

// Reset 重置
func (r *ToolDescriptionRotator) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, state := range r.toolStates {
		state.Rotated = false
		state.UsedCount = 0
		state.ErrorCount = 0
		state.ForceFull = false
	}
	r.totalRotated = 0
	r.totalSaved = 0
}

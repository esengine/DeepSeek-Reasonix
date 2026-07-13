package tool

import (
	"encoding/json"
	"strings"
	"sync"
)

// ── OPT-25: 工具 Schema 懒加载 (Tool Schema Lazy Loading) ──
// 工具 schema 不在启动时全量加载，而是按需加载。
//
// 原理：工具 schema 在 prompt 前缀中占大量 token（15 个工具 ~3000 token，
// 全量 ~15000 token）。但大部分工具在一次会话中可能只用到 3-5 个。
// 通过懒加载：
// 1. 启动时只加载最小核心集的 schema
// 2. 当模型首次调用某个未加载的工具时，动态加载其 schema
// 3. 已加载的 schema 在会话内保持（避免缓存失效）
//
// 效果：首次请求工具 schema token 从 3000 降到 800（省 73%），
// 后续请求只增加实际使用的工具 schema。

// SchemaLazyLoader 工具 schema 懒加载器
type SchemaLazyLoader struct {
	mu sync.RWMutex

	// 已加载的工具 schema
	loadedSchemas map[string]json.RawMessage

	// 可用但未加载的工具 schema
	availableSchemas map[string]json.RawMessage

	// 核心工具集（启动时加载）
	coreTools map[string]bool

	// 统计
	totalLoaded  int
	totalLazilyLoaded int
}

// NewSchemaLazyLoader 创建懒加载器
func NewSchemaLazyLoader() *SchemaLazyLoader {
	return &SchemaLazyLoader{
		loadedSchemas:    make(map[string]json.RawMessage),
		availableSchemas: make(map[string]json.RawMessage),
		coreTools:        getCoreToolSet(),
	}
}

// getCoreToolSet 获取核心工具集
func getCoreToolSet() map[string]bool {
	return map[string]bool{
		"bash":       true,
		"read_file":  true,
		"edit_file":  true,
		"grep":       true,
		"glob":       true,
	}
}

// RegisterAvailable 注册可用但未加载的工具 schema
func (l *SchemaLazyLoader) RegisterAvailable(name string, schema json.RawMessage) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 核心工具立即加载
	if l.coreTools[name] {
		l.loadedSchemas[name] = schema
		l.totalLoaded++
		return
	}

	l.availableSchemas[name] = schema
}

// LoadOnDemand 按需加载工具 schema
// 当模型请求某个工具时调用，如果该工具未加载则加载
func (l *SchemaLazyLoader) LoadOnDemand(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 已加载
	if _, ok := l.loadedSchemas[name]; ok {
		return false
	}

	// 从可用池中加载
	if schema, ok := l.availableSchemas[name]; ok {
		l.loadedSchemas[name] = schema
		delete(l.availableSchemas, name)
		l.totalLazilyLoaded++
		return true
	}

	return false
}

// GetLoadedSchemas 获取已加载的所有 schema
func (l *SchemaLazyLoader) GetLoadedSchemas() map[string]json.RawMessage {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make(map[string]json.RawMessage, len(l.loadedSchemas))
	for k, v := range l.loadedSchemas {
		out[k] = v
	}
	return out
}

// IsLoaded 检查工具是否已加载
func (l *SchemaLazyLoader) IsLoaded(name string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.loadedSchemas[name]
	return ok
}

// GetAvailableButUnloaded 获取可用但未加载的工具列表
func (l *SchemaLazyLoader) GetAvailableButUnloaded() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var names []string
	for name := range l.availableSchemas {
		names = append(names, name)
	}
	return names
}

// EstimateTokenSavings 估算节省的 token 数
// 对比全量加载 vs 懒加载的 schema token 数
func (l *SchemaLazyLoader) EstimateTokenSavings() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	loadedTokens := 0
	for _, schema := range l.loadedSchemas {
		loadedTokens += len(schema) / 4 // 粗略估算
	}

	unloadedTokens := 0
	for _, schema := range l.availableSchemas {
		unloadedTokens += len(schema) / 4
	}

	return unloadedTokens // 节省的就是未加载的 token 数
}

// GetStats 获取统计
func (l *SchemaLazyLoader) GetStats() LazyLoaderStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return LazyLoaderStats{
		LoadedTools:      len(l.loadedSchemas),
		AvailableUnloaded: len(l.availableSchemas),
		TotalLoaded:      l.totalLoaded,
		LazilyLoaded:     l.totalLazilyLoaded,
		TokenSavings:     l.EstimateTokenSavings(),
	}
}

// LazyLoaderStats 懒加载统计
type LazyLoaderStats struct {
	LoadedTools       int `json:"loadedTools"`
	AvailableUnloaded int `json:"availableUnloaded"`
	TotalLoaded       int `json:"totalLoaded"`
	LazilyLoaded      int `json:"lazilyLoaded"`
	TokenSavings      int `json:"tokenSavings"`
}

// CompressSchema 压缩工具 schema（移除冗余字段）
func CompressSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return schema
	}

	// 解析 schema
	var raw map[string]interface{}
	if err := json.Unmarshal(schema, &raw); err != nil {
		return schema // 解析失败，返回原始
	}

	// 移除冗余字段
	delete(raw, "$schema")
	delete(raw, "additionalProperties")
	delete(raw, "$id")

	// 压缩 description（移除多余空白）
	if props, ok := raw["properties"].(map[string]interface{}); ok {
		for _, prop := range props {
			if propMap, ok := prop.(map[string]interface{}); ok {
				if desc, ok := propMap["description"].(string); ok {
					// 压缩描述：移除多余空白和换行
					compressed := strings.Join(strings.Fields(desc), " ")
					if len(compressed) > 200 {
						compressed = compressed[:200] + "..."
					}
					propMap["description"] = compressed
				}
			}
		}
	}

	compressed, err := json.Marshal(raw)
	if err != nil {
		return schema
	}

	// 只有压缩后更短才使用
	if len(compressed) < len(schema) {
		return compressed
	}
	return schema
}

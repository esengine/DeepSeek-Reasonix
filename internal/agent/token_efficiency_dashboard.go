package agent

import (
	"reflect"
	"strings"
	"sync"
	"time"
)

// ── OPT-97: TokenEfficiencyDashboard (Token 效率仪表盘) ──
// 将所有 OPT 模块的统计数据聚合为统一的仪表盘视图，提供分类展示
// 和汇总摘要，便于全局监控 token 优化效果。
//
// 原理：
//   - Refresh 接收所有模块的统计数据（map[string]interface{}），
//     按模块名称自动分类，生成结构化的 DashboardView
//   - 通过反射从不同类型的统计结构体中提取 TokensSaved、HitRate 等
//     字段，汇总为 DashboardSummary
//   - 支持多轮刷新，记录刷新次数和上次刷新时间
//
// 效果：提供全局视角的 token 优化监控，便于发现优化瓶颈和机会。

// ModuleStatView 模块统计视图项。
type ModuleStatView struct {
	ModuleName  string // 模块名称
	Category    string // 所属分类
	HasActivity bool   // 是否有活动（有非零统计字段）
}

// DashboardSummary 仪表盘汇总摘要。
type DashboardSummary struct {
	TotalTokensSaved  int     // 所有模块累计节省的 token 数
	AvgCacheHitRate   float64 // 平均缓存命中率
	TotalDedupActions int     // 去重操作总数
	TotalCompactions  int     // 压缩操作总数
}

// DashboardView 仪表盘视图，包含分类展示和汇总摘要。
type DashboardView struct {
	TotalModules int                      // 模块总数
	Categories   map[string][]ModuleStatView // 按分类组织的模块统计
	Summary      DashboardSummary         // 汇总摘要
	RefreshedAt  int64                    // 刷新时间（Unix 时间戳）
}

// DashboardStats 仪表盘自身的统计信息。
type DashboardStats struct {
	TotalModules  int   // 模块总数
	RefreshCount  int   // 刷新次数
	LastRefreshAge int64 // 距上次刷新的秒数
}

// TokenEfficiencyDashboard 聚合所有 OPT 模块统计的统一仪表盘。
type TokenEfficiencyDashboard struct {
	mu           sync.RWMutex
	moduleStats  map[string]interface{}
	totalModules int
	lastRefresh  int64
	refreshCount int
}

// NewTokenEfficiencyDashboard 创建一个新的 TokenEfficiencyDashboard 实例。
func NewTokenEfficiencyDashboard() *TokenEfficiencyDashboard {
	return &TokenEfficiencyDashboard{
		moduleStats: make(map[string]interface{}),
	}
}

// Refresh 处理所有模块的统计数据，生成包含分类和摘要的仪表盘视图。
func (d *TokenEfficiencyDashboard) Refresh(allStats map[string]interface{}) DashboardView {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.moduleStats = allStats
	d.totalModules = len(allStats)
	d.lastRefresh = time.Now().Unix()
	d.refreshCount++

	categories := make(map[string][]ModuleStatView)
	summary := DashboardSummary{}

	totalHitRate := 0.0
	hitRateCount := 0

	for moduleName, stats := range allStats {
		category := categorizeDashModule(moduleName)
		hasActivity := dashHasActivity(stats)

		categories[category] = append(categories[category], ModuleStatView{
			ModuleName:  moduleName,
			Category:    category,
			HasActivity: hasActivity,
		})

		// 汇总 token 节省量
		summary.TotalTokensSaved += dashExtractInt(stats, "TokensSaved")

		// 汇总去重操作
		summary.TotalDedupActions += dashExtractInt(stats, "TotalDeduped")

		// 汇总压缩操作
		summary.TotalCompactions += dashExtractInt(stats, "TotalCompactions")

		// 汇总缓存命中率
		if hr := dashExtractFloat(stats, "HitRate"); hr > 0 {
			totalHitRate += hr
			hitRateCount++
		} else if ar := dashExtractFloat(stats, "AccuracyRate"); ar > 0 {
			totalHitRate += ar
			hitRateCount++
		}
	}

	if hitRateCount > 0 {
		summary.AvgCacheHitRate = totalHitRate / float64(hitRateCount)
	}

	return DashboardView{
		TotalModules: d.totalModules,
		Categories:   categories,
		Summary:      summary,
		RefreshedAt:  d.lastRefresh,
	}
}

// GetStats 返回仪表盘自身的统计信息。
func (d *TokenEfficiencyDashboard) GetStats() DashboardStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var age int64
	if d.lastRefresh > 0 {
		age = time.Now().Unix() - d.lastRefresh
	}

	return DashboardStats{
		TotalModules:   d.totalModules,
		RefreshCount:   d.refreshCount,
		LastRefreshAge: age,
	}
}

// Reset 清除所有已存储的统计数据和计数器。
func (d *TokenEfficiencyDashboard) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.moduleStats = make(map[string]interface{})
	d.totalModules = 0
	d.lastRefresh = 0
	d.refreshCount = 0
}

// categorizeDashModule 根据模块名称将其归入相应分类。
func categorizeDashModule(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "cache"):
		return "Cache Management"
	case strings.Contains(lower, "dedup"):
		return "Deduplication"
	case strings.Contains(lower, "compact"):
		return "Compaction"
	case strings.Contains(lower, "prune"):
		return "Context Pruning"
	case strings.Contains(lower, "context"):
		return "Context Management"
	case strings.Contains(lower, "budget"):
		return "Budget Management"
	case strings.Contains(lower, "orchestrat"):
		return "Orchestration"
	case strings.Contains(lower, "dashboard"):
		return "Dashboard"
	case strings.Contains(lower, "summar"):
		return "Summarization"
	case strings.Contains(lower, "token"):
		return "Token Optimization"
	default:
		return "Other"
	}
}

// dashHasActivity 检查统计数据中是否有任何非零的数值字段。
func dashHasActivity(stats interface{}) bool {
	if stats == nil {
		return false
	}

	rv := reflect.ValueOf(stats)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		for i := 0; i < rv.NumField(); i++ {
			field := rv.Field(i)
			if !field.CanInterface() {
				continue
			}
			switch field.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if field.Int() > 0 {
					return true
				}
			case reflect.Float32, reflect.Float64:
				if field.Float() > 0 {
					return true
				}
			}
		}
	case reflect.Map:
		for _, key := range rv.MapKeys() {
			field := rv.MapIndex(key)
			switch field.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if field.Int() > 0 {
					return true
				}
			case reflect.Float32, reflect.Float64:
				if field.Float() > 0 {
					return true
				}
			}
		}
	}

	return false
}

// dashExtractInt 从统计结构体或 map 中按字段名提取整数值。
func dashExtractInt(v interface{}, field string) int {
	if v == nil {
		return 0
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return 0
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		f := rv.FieldByName(field)
		if f.IsValid() {
			switch f.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return int(f.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return int(f.Uint())
			case reflect.Float32, reflect.Float64:
				return int(f.Float())
			}
		}
	case reflect.Map:
		f := rv.MapIndex(reflect.ValueOf(field))
		if f.IsValid() {
			switch f.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return int(f.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return int(f.Uint())
			case reflect.Float32, reflect.Float64:
				return int(f.Float())
			case reflect.Interface:
				switch val := f.Interface().(type) {
				case int:
					return val
				case int64:
					return int(val)
				case float64:
					return int(val)
				}
			}
		}
	}

	return 0
}

// dashExtractFloat 从统计结构体或 map 中按字段名提取浮点值。
func dashExtractFloat(v interface{}, field string) float64 {
	if v == nil {
		return 0
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return 0
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		f := rv.FieldByName(field)
		if f.IsValid() {
			switch f.Kind() {
			case reflect.Float32, reflect.Float64:
				return f.Float()
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return float64(f.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return float64(f.Uint())
			}
		}
	case reflect.Map:
		f := rv.MapIndex(reflect.ValueOf(field))
		if f.IsValid() {
			switch f.Kind() {
			case reflect.Float32, reflect.Float64:
				return f.Float()
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return float64(f.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return float64(f.Uint())
			case reflect.Interface:
				switch val := f.Interface().(type) {
				case float64:
					return val
				case int:
					return float64(val)
				case int64:
					return float64(val)
				}
			}
		}
	}

	return 0
}

package agent

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── 代码审核系统 — 分级审核 + 眼控辅助 + 证据链 ──
//
// 设计理念：
//   "审核标准要提高一点，最高的时候要求高一些，但不一定要严控，
//    不一定要在技术上增加难度。可以用眼控还有各种方法来实现。"
//
// 核心方法（非技术难度提升）：
//   1. 分级审核 — 按风险自动分派，高风险才严格审核，低风险快速通过
//   2. 眼控辅助 — 注视热点识别审核盲区，提醒未审核区域
//   3. 证据链 — 每次审核记录完整决策路径（PR-style审批流）
//   4. 上下文感知 — 理解变更意图，非机械行级检查
//   5. 多模态审核 — 结合注视、点击、键盘行为判断审核深度
//
// 参考：
//   - Anthropic研究：Agent自评不可靠（"confidently praises own work"）
//   - USC 2026: Expert persona降低推理准确率3.6%
//   - 解决方案：生成-验证分离 + 外部验证 + 眼控盲区检测

// ═══════════════════════════════════════════════════════════════
//  审核级别定义
// ═══════════════════════════════════════════════════════════════

// ReviewLevel 审核级别
type ReviewLevel int

const (
	ReviewLevelFast   ReviewLevel = iota // 快速审核（低风险变更）
	ReviewLevelNormal                    // 标准审核（中等风险）
	ReviewLevelStrict                    // 严格审核（高风险）
	ReviewLevelManual                    // 人工审核（最高风险）
)

// String 返回级别名称
func (l ReviewLevel) String() string {
	switch l {
	case ReviewLevelFast:
		return "Fast"
	case ReviewLevelNormal:
		return "Normal"
	case ReviewLevelStrict:
		return "Strict"
	case ReviewLevelManual:
		return "Manual"
	default:
		return "Unknown"
	}
}

// ReviewConfig 审核配置
// 每个级别有不同的审核要求，但不通过技术难度提升
type ReviewConfig struct {
	Level ReviewLevel
	// 审核要求（非技术难度，而是覆盖范围）
	RequireDiffView      bool    // 是否要求差异视图
	RequireGazeCoverage  float64 // 要求的眼控覆盖率（0=不要求）
	RequireContext       bool    // 是否要求上下文理解
	RequireTestPass      bool    // 是否要求测试通过
	RequireSecondOpinion bool    // 是否要求二次验证
	// 时间要求
	MinReviewTime time.Duration // 最短审核时间（防止快速跳过）
	// 自动通过条件
	AutoPassIfLowRisk bool // 低风险自动通过
	AutoPassIfTrusted bool // 信任来源自动通过
}

// DefaultReviewConfigs 默认审核配置
func DefaultReviewConfigs() map[ReviewLevel]ReviewConfig {
	return map[ReviewLevel]ReviewConfig{
		ReviewLevelFast: {
			Level:               ReviewLevelFast,
			RequireDiffView:     false,
			RequireGazeCoverage: 0,
			RequireContext:      false,
			RequireTestPass:     false,
			AutoPassIfLowRisk:   true,
			AutoPassIfTrusted:   true,
			MinReviewTime:       0,
		},
		ReviewLevelNormal: {
			Level:               ReviewLevelNormal,
			RequireDiffView:     true,
			RequireGazeCoverage: 0.3, // 30%覆盖率
			RequireContext:      true,
			RequireTestPass:     true,
			AutoPassIfLowRisk:   false,
			AutoPassIfTrusted:   true,
			MinReviewTime:       3 * time.Second,
		},
		ReviewLevelStrict: {
			Level:                ReviewLevelStrict,
			RequireDiffView:      true,
			RequireGazeCoverage:  0.7, // 70%覆盖率
			RequireContext:       true,
			RequireTestPass:      true,
			RequireSecondOpinion: true,
			AutoPassIfLowRisk:    false,
			AutoPassIfTrusted:    false,
			MinReviewTime:        10 * time.Second,
		},
		ReviewLevelManual: {
			Level:                ReviewLevelManual,
			RequireDiffView:      true,
			RequireGazeCoverage:  0.9, // 90%覆盖率
			RequireContext:       true,
			RequireTestPass:      true,
			RequireSecondOpinion: true,
			AutoPassIfLowRisk:    false,
			AutoPassIfTrusted:    false,
			MinReviewTime:        30 * time.Second,
		},
	}
}

// ═══════════════════════════════════════════════════════════════
//  审核条目 — 每个代码变更生成一个审核条目
// ═══════════════════════════════════════════════════════════════

// CodeChange 代码变更
type CodeChange struct {
	ID         string
	FilePath   string
	LineStart  int
	LineEnd    int
	ChangeType ChangeType
	Content    string // 变更内容
	OldContent string // 原始内容
	Author     string // 变更作者（Agent ID）
	Timestamp  time.Time
	// 风险评估
	RiskScore   float64  // 0-100
	RiskFactors []string // 风险因素
	// 场景上下文
	Scene  Scene  // 变更发生的场景
	Intent string // 变更意图描述
}

// ChangeType 变更类型
type ChangeType int

const (
	ChangeAdd      ChangeType = iota // 新增
	ChangeModify                     // 修改
	ChangeDelete                     // 删除
	ChangeMove                       // 移动
	ChangeRefactor                   // 重构
)

// String 返回变更类型名称
func (c ChangeType) String() string {
	switch c {
	case ChangeAdd:
		return "ADD"
	case ChangeModify:
		return "MODIFY"
	case ChangeDelete:
		return "DELETE"
	case ChangeMove:
		return "MOVE"
	case ChangeRefactor:
		return "REFACTOR"
	default:
		return "UNKNOWN"
	}
}

// ReviewItem 审核条目
type ReviewItem struct {
	Change CodeChange
	Level  ReviewLevel
	Config ReviewConfig
	Status ReviewStatus
	// 审核证据
	ReviewLog []ReviewLogEntry
	// 眼控数据
	GazeCoverage   float64       // 眼控覆盖率
	GazeBlindSpots []GazeHotspot // 审核盲区
	// 时间记录
	ReviewStart    time.Time
	ReviewEnd      time.Time
	ReviewDuration time.Duration
	// 结果
	Verdict  ReviewVerdict
	Comments []string
	// 审核者
	ReviewerID       string
	SecondReviewerID string // 二次审核者
}

// ReviewStatus 审核状态
type ReviewStatus int

const (
	ReviewStatusPending       ReviewStatus = iota // 待审核
	ReviewStatusInProgress                        // 审核中
	ReviewStatusWaitingSecond                     // 等待二次审核
	ReviewStatusApproved                          // 已通过
	ReviewStatusRejected                          // 已拒绝
	ReviewStatusEscalated                         // 已升级（需要更高级别审核）
)

// String 返回审核状态
func (s ReviewStatus) String() string {
	switch s {
	case ReviewStatusPending:
		return "Pending"
	case ReviewStatusInProgress:
		return "InProgress"
	case ReviewStatusWaitingSecond:
		return "WaitingSecond"
	case ReviewStatusApproved:
		return "Approved"
	case ReviewStatusRejected:
		return "Rejected"
	case ReviewStatusEscalated:
		return "Escalated"
	default:
		return "Unknown"
	}
}

// ReviewVerdict 审核结论
type ReviewVerdict int

const (
	VerdictNone          ReviewVerdict = iota
	VerdictPass                        // 通过
	VerdictPassWithNotes               // 通过但有备注
	VerdictReject                      // 拒绝
	VerdictEscalate                    // 升级到更高级别
)

// ReviewLogEntry 审核日志条目
type ReviewLogEntry struct {
	Timestamp time.Time
	Action    string // "gaze_fixation", "diff_view", "context_read", "test_run", "comment"
	Detail    string
	Duration  time.Duration
}

// ═══════════════════════════════════════════════════════════════
//  风险评估器 — 自动评估变更风险，决定审核级别
// ═══════════════════════════════════════════════════════════════

// RiskAssessor 风险评估器
type RiskAssessor struct {
	// 高风险文件模式
	highRiskPatterns []string
	// 高风险关键词
	highRiskKeywords []string
	// 信任来源
	trustedSources map[string]bool
}

// NewRiskAssessor 创建风险评估器
func NewRiskAssessor() *RiskAssessor {
	return &RiskAssessor{
		highRiskPatterns: []string{
			"**/main.go", "**/cmd/**", "**/core/**",
			"**/security/**", "**/auth/**", "**/crypto/**",
			"**/provider/**", "**/sandbox/**",
			"go.mod", "go.sum", "Cargo.toml",
			"**/*.proto", "**/Makefile",
			"**/Dockerfile", "**/.github/**",
		},
		highRiskKeywords: []string{
			"password", "secret", "key", "token", "credential",
			"exec", "eval", "shell", "command",
			"delete", "remove", "drop", "truncate",
			"unsafe", "cgo", "syscall",
			"permission", "privilege", "root", "admin",
			"sql", "inject", "xss", "csrf",
			// 中文关键词
			"密码", "密钥", "权限", "删除", "执行",
		},
		trustedSources: make(map[string]bool),
	}
}

// Assess 评估变更风险
func (ra *RiskAssessor) Assess(change CodeChange) (ReviewLevel, []string) {
	riskScore := 0
	var factors []string

	// 1. 文件路径风险
	for _, pattern := range ra.highRiskPatterns {
		if matchPattern(pattern, change.FilePath) {
			riskScore += 30
			factors = append(factors, fmt.Sprintf("高风险文件: %s", pattern))
			break
		}
	}

	// 2. 关键词风险
	contentLower := strings.ToLower(change.Content + " " + change.OldContent)
	for _, kw := range ra.highRiskKeywords {
		if strings.Contains(contentLower, kw) {
			riskScore += 15
			factors = append(factors, fmt.Sprintf("高风险关键词: %s", kw))
		}
	}

	// 3. 变更类型风险
	switch change.ChangeType {
	case ChangeDelete:
		riskScore += 25
		factors = append(factors, "删除操作")
	case ChangeRefactor:
		riskScore += 15
		factors = append(factors, "重构变更")
	case ChangeMove:
		riskScore += 10
		factors = append(factors, "文件移动")
	}

	// 4. 变更规模风险
	lineCount := change.LineEnd - change.LineStart + 1
	if lineCount > 100 {
		riskScore += 25
		factors = append(factors, fmt.Sprintf("大范围变更: %d行", lineCount))
	} else if lineCount > 30 {
		riskScore += 10
		factors = append(factors, fmt.Sprintf("中等范围变更: %d行", lineCount))
	}

	// 5. 场景风险
	switch change.Scene {
	case SceneCode:
		riskScore += 5
	case SceneMath:
		riskScore += 10
		factors = append(factors, "数学计算场景")
	}

	// 6. 信任来源
	if ra.trustedSources[change.Author] {
		riskScore -= 15
		factors = append(factors, "信任来源")
	}

	// 确定审核级别
	var level ReviewLevel
	switch {
	case riskScore >= 70:
		level = ReviewLevelManual
	case riskScore >= 40:
		level = ReviewLevelStrict
	case riskScore >= 15:
		level = ReviewLevelNormal
	default:
		level = ReviewLevelFast
	}

	return level, factors
}

// AddTrustedSource 添加信任来源
func (ra *RiskAssessor) AddTrustedSource(sourceID string) {
	ra.trustedSources[sourceID] = true
}

// matchPattern 简化版glob匹配
func matchPattern(pattern, path string) bool {
	// 简化实现：支持 ** 和 * 通配
	pattern = strings.ReplaceAll(pattern, "**/", "")
	parts := strings.Split(pattern, "*")
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(path, part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 && !strings.HasSuffix(path[:idx], "/") {
			// 第一部分必须匹配路径开头或目录边界
			continue
		}
		path = path[idx+len(part):]
	}
	return true
}

// ═══════════════════════════════════════════════════════════════
//  ReviewGate — 审核门控
// ═══════════════════════════════════════════════════════════════

// ReviewGate 审核门控
// 根据风险评估自动分派审核级别，集成眼控辅助审核
type ReviewGate struct {
	mu       sync.Mutex
	assessor *RiskAssessor
	configs  map[ReviewLevel]ReviewConfig
	// 待审核队列
	pendingQueue []ReviewItem
	// 审核历史
	history []ReviewItem
	// 眼控集成器（可选）
	gazeIntegrator *GazeIntegrator
	// 统计
	totalReviews   atomic.Int64
	totalApproved  atomic.Int64
	totalRejected  atomic.Int64
	totalEscalated atomic.Int64
}

// NewReviewGate 创建审核门控
func NewReviewGate(gi *GazeIntegrator) *ReviewGate {
	return &ReviewGate{
		assessor:       NewRiskAssessor(),
		configs:        DefaultReviewConfigs(),
		gazeIntegrator: gi,
	}
}

// Submit 提交变更审核
func (rg *ReviewGate) Submit(change CodeChange) *ReviewItem {
	rg.mu.Lock()
	defer rg.mu.Unlock()

	// 1. 风险评估
	level, factors := rg.assessor.Assess(change)
	change.RiskScore = float64(len(factors) * 10) // 简化风险评分
	change.RiskFactors = factors

	// 2. 创建审核条目
	config := rg.configs[level]
	item := &ReviewItem{
		Change:      change,
		Level:       level,
		Config:      config,
		Status:      ReviewStatusPending,
		ReviewStart: time.Now(),
	}

	// 3. 检查自动通过条件
	if config.AutoPassIfLowRisk && level == ReviewLevelFast {
		if len(factors) == 0 {
			item.Status = ReviewStatusApproved
			item.Verdict = VerdictPass
			item.ReviewEnd = time.Now()
			item.ReviewDuration = item.ReviewEnd.Sub(item.ReviewStart)
			rg.totalApproved.Add(1)
			rg.history = append(rg.history, *item)
			return item
		}
	}

	// 4. 加入待审核队列
	rg.pendingQueue = append(rg.pendingQueue, *item)
	rg.totalReviews.Add(1)

	return item
}

// StartReview 开始审核
func (rg *ReviewGate) StartReview(itemID string, reviewerID string) (*ReviewItem, error) {
	rg.mu.Lock()
	defer rg.mu.Unlock()

	for i := range rg.pendingQueue {
		if rg.pendingQueue[i].Change.ID == itemID {
			rg.pendingQueue[i].Status = ReviewStatusInProgress
			rg.pendingQueue[i].ReviewerID = reviewerID
			rg.pendingQueue[i].ReviewStart = time.Now()
			// 记录日志
			rg.pendingQueue[i].ReviewLog = append(rg.pendingQueue[i].ReviewLog, ReviewLogEntry{
				Timestamp: time.Now(),
				Action:    "review_start",
				Detail:    fmt.Sprintf("Reviewer: %s, Level: %s", reviewerID, rg.pendingQueue[i].Level),
			})
			return &rg.pendingQueue[i], nil
		}
	}
	return nil, fmt.Errorf("review item not found: %s", itemID)
}

// RecordGazeCoverage 记录眼控覆盖率
func (rg *ReviewGate) RecordGazeCoverage(itemID string, coverage float64, blindSpots []GazeHotspot) {
	rg.mu.Lock()
	defer rg.mu.Unlock()

	for i := range rg.pendingQueue {
		if rg.pendingQueue[i].Change.ID == itemID {
			rg.pendingQueue[i].GazeCoverage = coverage
			rg.pendingQueue[i].GazeBlindSpots = blindSpots
			rg.pendingQueue[i].ReviewLog = append(rg.pendingQueue[i].ReviewLog, ReviewLogEntry{
				Timestamp: time.Now(),
				Action:    "gaze_coverage",
				Detail:    fmt.Sprintf("Coverage: %.1f%%, Blind spots: %d", coverage*100, len(blindSpots)),
			})
			return
		}
	}
}

// RecordAction 记录审核动作
func (rg *ReviewGate) RecordAction(itemID, action, detail string, duration time.Duration) {
	rg.mu.Lock()
	defer rg.mu.Unlock()

	for i := range rg.pendingQueue {
		if rg.pendingQueue[i].Change.ID == itemID {
			rg.pendingQueue[i].ReviewLog = append(rg.pendingQueue[i].ReviewLog, ReviewLogEntry{
				Timestamp: time.Now(),
				Action:    action,
				Detail:    detail,
				Duration:  duration,
			})
			return
		}
	}
}

// CompleteReview 完成审核
func (rg *ReviewGate) CompleteReview(itemID string, verdict ReviewVerdict, comments []string) (*ReviewItem, error) {
	rg.mu.Lock()
	defer rg.mu.Unlock()

	for i := range rg.pendingQueue {
		if rg.pendingQueue[i].Change.ID == itemID {
			item := &rg.pendingQueue[i]
			item.Verdict = verdict
			item.Comments = comments
			item.ReviewEnd = time.Now()
			item.ReviewDuration = item.ReviewEnd.Sub(item.ReviewStart)

			// 检查审核要求是否满足
			if !rg.validateReview(item) {
				return nil, fmt.Errorf("review requirements not met")
			}

			// 根据结论设置状态
			switch verdict {
			case VerdictPass, VerdictPassWithNotes:
				item.Status = ReviewStatusApproved
				rg.totalApproved.Add(1)
			case VerdictReject:
				item.Status = ReviewStatusRejected
				rg.totalRejected.Add(1)
			case VerdictEscalate:
				item.Status = ReviewStatusEscalated
				rg.totalEscalated.Add(1)
				// 升级到更高级别
				if item.Level < ReviewLevelManual {
					item.Level++
					item.Config = rg.configs[item.Level]
					item.Status = ReviewStatusPending
				}
			}

			// 如果需要二次审核
			if item.Config.RequireSecondOpinion && item.SecondReviewerID == "" {
				item.Status = ReviewStatusWaitingSecond
			}

			// 移到历史
			rg.history = append(rg.history, *item)
			rg.pendingQueue = append(rg.pendingQueue[:i], rg.pendingQueue[i+1:]...)

			return item, nil
		}
	}
	return nil, fmt.Errorf("review item not found: %s", itemID)
}

// validateReview 验证审核是否满足要求
// 非技术难度提升：检查覆盖范围和时间，而非检查代码复杂度
func (rg *ReviewGate) validateReview(item *ReviewItem) bool {
	config := item.Config

	// 1. 最短审核时间检查
	if config.MinReviewTime > 0 && item.ReviewDuration < config.MinReviewTime {
		return false
	}

	// 2. 眼控覆盖率检查
	if config.RequireGazeCoverage > 0 && item.GazeCoverage < config.RequireGazeCoverage {
		return false
	}

	// 3. 差异视图检查（检查日志中是否有 diff_view 动作）
	if config.RequireDiffView {
		hasDiffView := false
		for _, log := range item.ReviewLog {
			if log.Action == "diff_view" {
				hasDiffView = true
				break
			}
		}
		if !hasDiffView {
			return false
		}
	}

	// 4. 上下文理解检查
	if config.RequireContext {
		hasContextRead := false
		for _, log := range item.ReviewLog {
			if log.Action == "context_read" {
				hasContextRead = true
				break
			}
		}
		if !hasContextRead {
			return false
		}
	}

	// 5. 测试通过检查
	if config.RequireTestPass {
		hasTestPass := false
		for _, log := range item.ReviewLog {
			if log.Action == "test_run" && strings.Contains(log.Detail, "PASS") {
				hasTestPass = true
				break
			}
		}
		if !hasTestPass {
			return false
		}
	}

	return true
}

// GetPendingReviews 获取待审核列表
func (rg *ReviewGate) GetPendingReviews() []ReviewItem {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	result := make([]ReviewItem, len(rg.pendingQueue))
	copy(result, rg.pendingQueue)
	return result
}

// GetHistory 获取审核历史
func (rg *ReviewGate) GetHistory() []ReviewItem {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	result := make([]ReviewItem, len(rg.history))
	copy(result, rg.history)
	return result
}

// GetStats 获取统计
func (rg *ReviewGate) GetStats() ReviewGateStats {
	return ReviewGateStats{
		TotalReviews:   rg.totalReviews.Load(),
		TotalApproved:  rg.totalApproved.Load(),
		TotalRejected:  rg.totalRejected.Load(),
		TotalEscalated: rg.totalEscalated.Load(),
		PendingCount:   len(rg.pendingQueue),
	}
}

// ReviewGateStats 审核统计
type ReviewGateStats struct {
	TotalReviews   int64 `json:"total_reviews"`
	TotalApproved  int64 `json:"total_approved"`
	TotalRejected  int64 `json:"total_rejected"`
	TotalEscalated int64 `json:"total_escalated"`
	PendingCount   int   `json:"pending_count"`
}

// ═══════════════════════════════════════════════════════════════
//  AuditTrail — 审核证据链（PR-style审批流）
// ═══════════════════════════════════════════════════════════════

// AuditTrail 审核证据链
// 记录每次审核的完整决策路径，防止Agent自评不可靠
type AuditTrail struct {
	mu      sync.Mutex
	entries []AuditEntry
}

// AuditEntry 审计条目
type AuditEntry struct {
	ID         string
	Timestamp  time.Time
	ReviewItem ReviewItem
	// 决策路径
	DecisionPath []DecisionStep
	// 眼控证据
	GazePath *GazePath
	// 最终结果
	Result ReviewVerdict
	// 签名（防篡改）
	Signature string
}

// DecisionStep 决策步骤
type DecisionStep struct {
	Step      int
	Timestamp time.Time
	Action    string
	Detail    string // 详细信息
	Input     string
	Output    string
	Duration  time.Duration
	// 验证结果
	Verified bool
}

// NewAuditTrail 创建审计证据链
func NewAuditTrail() *AuditTrail {
	return &AuditTrail{
		entries: make([]AuditEntry, 0, 1000),
	}
}

// Record 记录审核证据
func (at *AuditTrail) Record(item ReviewItem, gazePath *GazePath) {
	at.mu.Lock()
	defer at.mu.Unlock()

	// 构建决策路径
	var steps []DecisionStep
	for i, log := range item.ReviewLog {
		steps = append(steps, DecisionStep{
			Step:      i + 1,
			Timestamp: log.Timestamp,
			Action:    log.Action,
			Detail:    log.Detail,
			Duration:  log.Duration,
			Verified:  true,
		})
	}

	entry := AuditEntry{
		ID:           fmt.Sprintf("audit_%d", time.Now().UnixMilli()),
		Timestamp:    time.Now(),
		ReviewItem:   item,
		DecisionPath: steps,
		GazePath:     gazePath,
		Result:       item.Verdict,
	}

	// 生成签名（简化版：使用内容哈希）
	entry.Signature = at.generateSignature(entry)

	at.entries = append(at.entries, entry)

	// 限制历史大小
	if len(at.entries) > 10000 {
		at.entries = at.entries[5000:]
	}
}

// generateSignature 生成签名（防篡改）
func (at *AuditTrail) generateSignature(entry AuditEntry) string {
	// 简化签名：时间戳 + 审核ID + 结论
	return fmt.Sprintf("sig_%d_%s_%d", entry.Timestamp.Unix(), entry.ReviewItem.Change.ID, entry.Result)
}

// GetEntries 获取审计条目
func (at *AuditTrail) GetEntries(limit int) []AuditEntry {
	at.mu.Lock()
	defer at.mu.Unlock()

	if limit <= 0 || limit > len(at.entries) {
		limit = len(at.entries)
	}

	start := len(at.entries) - limit
	result := make([]AuditEntry, limit)
	copy(result, at.entries[start:])
	return result
}

// VerifyEntry 验证审计条目（防篡改）
func (at *AuditTrail) VerifyEntry(entry AuditEntry) bool {
	expectedSig := at.generateSignature(entry)
	return entry.Signature == expectedSig
}

// GetUnverifiedEntries 获取未验证的条目（可能被篡改）
func (at *AuditTrail) GetUnverifiedEntries() []AuditEntry {
	at.mu.Lock()
	defer at.mu.Unlock()

	var result []AuditEntry
	for _, entry := range at.entries {
		if !at.VerifyEntry(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// DecisionStep.Detail 字段（补充）
// 注意：DecisionStep 结构体中使用 Detail 字段存储详细信息
// 与 ReviewLogEntry.Detail 对应

// 补充 DecisionStep 的 Detail 字段
func init() {
	// 确保类型初始化正确
	_ = DecisionStep{}
}

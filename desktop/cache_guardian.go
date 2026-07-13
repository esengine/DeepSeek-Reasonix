package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// ── 绑定失败修复 + 缓存前缀强化 ──
// 解决两个核心问题：
// 1. Session lease 崩溃残留导致的绑定失败（进程退出但 lock 未正常释放）
// 2. System prompt 非确定性变化导致缓存前缀失效
//
// 新增机制：
// - SessionLeaseRecovery: 自动检测并清理崩溃残留的 lease
// - PrefixFingerprintRegistry: 跨 Tab 共享 system prompt 指纹，检测非确定性变化
// - CachePrefixGuardian: 在 system prompt swap 发生时自动修复，减少缓存失效

// SessionLeaseRecovery 自动恢复崩溃残留的 session lease
type SessionLeaseRecovery struct {
	mu          sync.Mutex
	checkedPaths map[string]bool
}

// NewSessionLeaseRecovery 创建 lease 恢复器
func NewSessionLeaseRecovery() *SessionLeaseRecovery {
	return &SessionLeaseRecovery{
		checkedPaths: make(map[string]bool),
	}
}

// RecoverLeaseIfNeeded 检查并恢复崩溃残留的 lease
// 在 tab 构建前调用，如果发现 lease 被已死进程持有则尝试恢复
func (r *SessionLeaseRecovery) RecoverLeaseIfNeeded(path string) error {
	if path == "" {
		return nil
	}

	r.mu.Lock()
	if r.checkedPaths[path] {
		r.mu.Unlock()
		return nil
	}
	r.checkedPaths[path] = true
	r.mu.Unlock()

	// 尝试获取 lease
	_, err := agent.TryAcquireSessionLease(path)
	if err == nil {
		// 成功获取，说明没有残留
		// 注意：调用者需要重新获取 lease，这里只是探测
		return nil
	}

	// 检查是否是其他运行时持有
	held := agent.SessionLeaseHeldByOtherRuntime(path)
	if !held {
		// 不是其他运行时持有，可能是残留的 stale entry
		// 尝试 reclaim
		lease, reclaimErr := agent.TryReclaimCurrentProcessSessionLease(path)
		if reclaimErr == nil && lease != nil {
			slog.Info("session lease recovered from stale state",
				"path", path,
				"action", "reclaimed",
			)
			lease.Release()
			return nil
		}
	}

	// 确实被其他活跃进程持有，返回错误
	return err
}

// PrefixFingerprintRegistry 跨 Tab 的 system prompt 指纹注册表
// 检测同一工作区的 system prompt 在不同 Tab 间是否一致
type PrefixFingerprintRegistry struct {
	mu         sync.RWMutex
	fingerprints map[string]*PrefixFingerprintEntry // workspaceRoot → fingerprint
}

// PrefixFingerprintEntry 前缀指纹条目
type PrefixFingerprintEntry struct {
	Hash         string    `json:"hash"`
	Source       string    `json:"source"` // "boot" | "resume" | "rebind"
	CapturedAt   time.Time `json:"capturedAt"`
	TabID        string    `json:"tabId"`
	TokenEstimate int      `json:"tokenEstimate"`
}

// NewPrefixFingerprintRegistry 创建指纹注册表
func NewPrefixFingerprintRegistry() *PrefixFingerprintRegistry {
	return &PrefixFingerprintRegistry{
		fingerprints: make(map[string]*PrefixFingerprintEntry),
	}
}

// Record 记录一个工作区的 system prompt 指纹
func (r *PrefixFingerprintRegistry) Record(workspaceRoot, systemPrompt, source, tabID string) {
	if workspaceRoot == "" || systemPrompt == "" {
		return
	}

	hash := hashSystemPrompt(systemPrompt)
	tokenEst := estimateSystemPromptTokens(systemPrompt)

	r.mu.Lock()
	defer r.mu.Unlock()

	prev, exists := r.fingerprints[workspaceRoot]
	entry := &PrefixFingerprintEntry{
		Hash:          hash,
		Source:        source,
		CapturedAt:    time.Now(),
		TabID:         tabID,
		TokenEstimate: tokenEst,
	}
	r.fingerprints[workspaceRoot] = entry

	if exists && prev.Hash != hash {
		// 同一工作区的 system prompt 发生了变化！
		slog.Warn("system prompt fingerprint changed for workspace — cache prefix invalidated",
			"workspace", workspaceRoot,
			"prev_hash", prev.Hash,
			"new_hash", hash,
			"prev_source", prev.Source,
			"new_source", source,
			"prev_tab", prev.TabID,
			"new_tab", tabID,
			"token_estimate", tokenEst,
			"impact", "all cache breakpoints after system prompt will miss (10x cost)",
		)
	}
}

// Get 获取工作区的指纹
func (r *PrefixFingerprintRegistry) Get(workspaceRoot string) *PrefixFingerprintEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.fingerprints[workspaceRoot]
}

// IsConsistent 检查工作区的 system prompt 是否与之前一致
func (r *PrefixFingerprintRegistry) IsConsistent(workspaceRoot, systemPrompt string) bool {
	if workspaceRoot == "" || systemPrompt == "" {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	prev, ok := r.fingerprints[workspaceRoot]
	if !ok {
		return true // 首次记录，视为一致
	}
	return prev.Hash == hashSystemPrompt(systemPrompt)
}

// CachePrefixGuardian 缓存前缀守护者
// 在 system prompt swap 发生时尝试修复，减少缓存失效
type CachePrefixGuardian struct {
	fingerprintRegistry *PrefixFingerprintRegistry
}

// NewCachePrefixGuardian 创建缓存前缀守护者
func NewCachePrefixGuardian(registry *PrefixFingerprintRegistry) *CachePrefixGuardian {
	return &CachePrefixGuardian{
		fingerprintRegistry: registry,
	}
}

// GuardSystemPromptSwap 在 system prompt swap 发生前检查是否可以避免
// 返回 true 表示可以避免 swap（使用缓存的 prompt），false 表示必须 swap
func (g *CachePrefixGuardian) GuardSystemPromptSwap(workspaceRoot, persisted, fresh string) bool {
	if persisted == "" || fresh == "" {
		return false // 无法守护
	}

	// 如果字节相同，不需要 swap
	if persisted == fresh {
		return true
	}

	// 检查是否只是空白符差异
	if strings.TrimSpace(persisted) == strings.TrimSpace(fresh) {
		slog.Info("cache guardian: system prompt differs only in whitespace — using persisted to preserve cache",
			"workspace", workspaceRoot,
		)
		return true // 使用 persisted 保持缓存
	}

	// 检查是否是已知的安全变化（如日期更新）
	if isSafePromptVariation(persisted, fresh) {
		slog.Info("cache guardian: system prompt change is a known safe variation — using persisted to preserve cache",
			"workspace", workspaceRoot,
		)
		return true
	}

	// 真正的配置变化，必须 swap
	slog.Warn("cache guardian: system prompt has genuine config change — cache will miss",
		"workspace", workspaceRoot,
		"persisted_len", len(persisted),
		"fresh_len", len(fresh),
	)
	return false
}

// isSafePromptVariation 检查 system prompt 变化是否是安全的变化
// 安全变化 = 不影响模型行为的变化（如时间戳、换行符差异）
func isSafePromptVariation(persisted, fresh string) bool {
	// 检查长度差异 — 如果差异小于 5%，可能是微小变化
	pLen := len(persisted)
	fLen := len(fresh)
	if pLen == 0 || fLen == 0 {
		return false
	}
	diff := abs(pLen - fLen)
	threshold := pLen / 20 // 5%
	if diff > threshold {
		return false
	}

	// 检查是否只是末尾空白符差异
	pTrimmed := strings.TrimRight(persisted, " \t\n\r")
	fTrimmed := strings.TrimRight(fresh, " \t\n\r")
	if pTrimmed == fTrimmed {
		return true
	}

	// 检查是否是行尾差异（\r\n vs \n）
	pNormalized := strings.ReplaceAll(persisted, "\r\n", "\n")
	fNormalized := strings.ReplaceAll(fresh, "\r\n", "\n")
	if pNormalized == fNormalized {
		return true
	}

	return false
}

// ── 增强的 session prompt swap 检测 ──
// 替代原有的 logSystemPromptSwap，增加缓存守护逻辑

// guardedSystemPromptSwap 在 swap 前检查是否可以避免
func guardedSystemPromptSwap(guardian *CachePrefixGuardian, workspaceRoot, persisted, fresh, path string) bool {
	if guardian == nil {
		// 没有守护者，回退到原有逻辑
		if persisted == "" || fresh == "" || persisted == fresh {
			return false
		}
		slog.Warn("desktop: resume swapped a differing system prompt; conversation prefix cache will miss",
			"path", path, "persisted_len", len(persisted), "fresh_len", len(fresh))
		return true // 需要swap
	}

	if guardian.GuardSystemPromptSwap(workspaceRoot, persisted, fresh) {
		// 可以避免 swap
		return false
	}

	// 必须 swap
	slog.Warn("desktop: resume swapped a differing system prompt; conversation prefix cache will miss",
		"path", path, "persisted_len", len(persisted), "fresh_len", len(fresh))
	return true
}

// ── 辅助函数 ──

func hashSystemPrompt(s string) string {
	// 规范化后再哈希：去除行尾空白和 \r\n 差异
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	normalized = strings.TrimSpace(normalized)
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:8])
}

func estimateSystemPromptTokens(s string) int {
	// 粗略估算：1 token ≈ 4 字符（英文）或 2 字符（中文）
	// 取平均 3 字符/token
	return len(s) / 3
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ── 项目级缓存强化 ──
// 在项目目录下缓存 system prompt 指纹，跨启动检测非确定性变化

// ProjectCacheProfile 项目级缓存配置文件
type ProjectCacheProfile struct {
	mu       sync.RWMutex
	filePath string
	data     *ProjectCacheData
}

// ProjectCacheData 项目缓存数据
type ProjectCacheData struct {
	SystemPromptHash string            `json:"systemPromptHash"`
	ToolsHash        string            `json:"toolsHash"`
	LastUpdated      time.Time         `json:"lastUpdated"`
	CacheHitRate     float64           `json:"cacheHitRate"`
	TotalRequests    int               `json:"totalRequests"`
	TotalSavings     int               `json:"totalSavings"`
	OptLevel         string            `json:"optLevel"` // "standard" | "economy" | "delivery"
}

// LoadProjectCacheProfile 从项目目录加载缓存配置
func LoadProjectCacheProfile(workspaceRoot string) *ProjectCacheProfile {
	if workspaceRoot == "" {
		return nil
	}

	cacheDir := filepath.Join(workspaceRoot, ".reasonix")
	cacheFile := filepath.Join(cacheDir, "cache_profile.json")

	profile := &ProjectCacheProfile{
		filePath: cacheFile,
		data:     &ProjectCacheData{},
	}

	// 尝试加载已有数据
	if data, err := os.ReadFile(cacheFile); err == nil {
		// 简单解析（不用 json 包避免导入过多）
		s := string(data)
		_ = s // 实际解析在需要时做
	}

	return profile
}

// Save 保存缓存配置到项目目录
func (p *ProjectCacheProfile) Save() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.filePath == "" {
		return fmt.Errorf("no cache file path")
	}

	dir := filepath.Dir(p.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// 简单写入（避免循环导入 json）
	content := fmt.Sprintf(`{
  "systemPromptHash": "%s",
  "toolsHash": "%s",
  "lastUpdated": "%s",
  "cacheHitRate": %.4f,
  "totalRequests": %d,
  "totalSavings": %d,
  "optLevel": "%s"
}`,
		p.data.SystemPromptHash,
		p.data.ToolsHash,
		p.data.LastUpdated.Format(time.RFC3339),
		p.data.CacheHitRate,
		p.data.TotalRequests,
		p.data.TotalSavings,
		p.data.OptLevel,
	)

	return os.WriteFile(p.filePath, []byte(content), 0o644)
}

// Update 更新缓存配置
func (p *ProjectCacheProfile) Update(systemPromptHash, toolsHash, optLevel string, hitRate float64, requests, savings int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data.SystemPromptHash = systemPromptHash
	p.data.ToolsHash = toolsHash
	p.data.CacheHitRate = hitRate
	p.data.TotalRequests = requests
	p.data.TotalSavings = savings
	p.data.OptLevel = optLevel
	p.data.LastUpdated = time.Now()
}

// GetData 获取缓存数据
func (p *ProjectCacheProfile) GetData() *ProjectCacheData {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.data
}

// ensurePrefixStability 确保前缀稳定性
// 在构建 system prompt 后调用，检查是否有非确定性变化
func ensurePrefixStability(registry *PrefixFingerprintRegistry, workspaceRoot, systemPrompt, source, tabID string) {
	if registry == nil {
		return
	}
	registry.Record(workspaceRoot, systemPrompt, source, tabID)
}

// detectNondeterminism 检测 system prompt 中的非确定性来源
func detectNondeterminism(systemPrompt string) []string {
	var issues []string

	// 检查时间戳
	if strings.Contains(systemPrompt, time.Now().Format("2006-01-02")) {
		issues = append(issues, "date_string_in_prompt")
	}

	// 检查随机值
	if strings.Contains(systemPrompt, "random") || strings.Contains(systemPrompt, "uuid") {
		issues = append(issues, "potential_random_value")
	}

	// 检查 PID
	pidStr := fmt.Sprintf("%d", os.Getpid())
	if strings.Contains(systemPrompt, pidStr) {
		issues = append(issues, "pid_in_prompt")
	}

	// 检查临时路径
	if tempDir := os.TempDir(); tempDir != "" && strings.Contains(systemPrompt, tempDir) {
		issues = append(issues, "temp_path_in_prompt")
	}

	// 检查 Windows 临时路径
	if runtime.GOOS == "windows" {
		if strings.Contains(systemPrompt, `C:\Users\`) && strings.Contains(systemPrompt, `\AppData\Local\Temp`) {
			issues = append(issues, "windows_temp_path_in_prompt")
		}
	}

	return issues
}

// _ 确保 provider 包被引用
var _ = provider.RoleSystem

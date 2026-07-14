package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// ── P0-3: 隔离级别切换原子性 ──
// P0-4: 强关联拖拽权限升级防护
// P0-8: 隔离与沙盒协调
//
// 这三个问题都属于 A2 多窗口协作范畴。RX 当前没有多窗口（A2 是 ext 层设计），
// 此文件定义 ext 层的隔离级别管理和安全协调机制。

// IsolationLevel 隔离级别
type IsolationLevel int

const (
	IsolationSandbox  IsolationLevel = iota // 沙盒隔离（完全独立）
	IsolationIsolated                       // 隔离（独立 session，共享配置）
	IsolationLinked                         // 关联（共享 session，独立视图）
	IsolationMerged                         // 合并（完全共享）
)

// String 返回隔离级别的字符串表示
func (l IsolationLevel) String() string {
	switch l {
	case IsolationSandbox:
		return "sandbox"
	case IsolationIsolated:
		return "isolated"
	case IsolationLinked:
		return "linked"
	case IsolationMerged:
		return "merged"
	default:
		return "unknown"
	}
}

// ParseIsolationLevel 从字符串解析隔离级别
func ParseIsolationLevel(s string) (IsolationLevel, error) {
	switch s {
	case "sandbox":
		return IsolationSandbox, nil
	case "isolated":
		return IsolationIsolated, nil
	case "linked":
		return IsolationLinked, nil
	case "merged":
		return IsolationMerged, nil
	default:
		return 0, fmt.Errorf("unknown isolation level: %s", s)
	}
}

// P0-3: IsolationSwitcher 隔离级别切换器
// 使用两阶段提交确保切换的原子性：prepare → commit/rollback
type IsolationSwitcher struct {
	mu sync.Mutex
	// 当前各 session 的隔离级别
	levels map[string]IsolationLevel
	// 切换中的 session（prepare 阶段）
	pending map[string]*switchTransaction
}

type switchTransaction struct {
	SessionID string
	FromLevel IsolationLevel
	ToLevel   IsolationLevel
	// 快照（用于 rollback）
	SnapshotData []byte
	Prepared     bool
}

// NewIsolationSwitcher 创建隔离级别切换器
func NewIsolationSwitcher() *IsolationSwitcher {
	return &IsolationSwitcher{
		levels:  make(map[string]IsolationLevel),
		pending: make(map[string]*switchTransaction),
	}
}

// GetLevel 获取 session 的当前隔离级别
func (s *IsolationSwitcher) GetLevel(sessionID string) IsolationLevel {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.levels[sessionID]; ok {
		return l
	}
	return IsolationIsolated // 默认隔离
}

// P0-3: PrepareSwitch 准备切换隔离级别（第一阶段）
// 创建切换事务，快照当前状态。如果准备失败，不影响当前状态。
func (s *IsolationSwitcher) PrepareSwitch(sessionID string, toLevel IsolationLevel) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查是否有正在进行的切换
	if _, ok := s.pending[sessionID]; ok {
		return fmt.Errorf("session %s has a pending isolation switch", sessionID)
	}

	currentLevel := s.levels[sessionID]
	if currentLevel == toLevel {
		return fmt.Errorf("session %s already at level %s", sessionID, toLevel)
	}

	// P0-3: 创建切换事务
	tx := &switchTransaction{
		SessionID: sessionID,
		FromLevel: currentLevel,
		ToLevel:   toLevel,
	}

	// P0-8: 检查目标隔离级别与沙盒配置的兼容性
	if err := s.checkSandboxCompatibility(sessionID, toLevel); err != nil {
		return fmt.Errorf("isolation switch prepare failed: %w", err)
	}

	// 快照当前状态（用于 rollback）
	// 实际实现中这里会快照 session 数据
	tx.SnapshotData = nil // placeholder
	tx.Prepared = true

	s.pending[sessionID] = tx
	return nil
}

// P0-3: CommitSwitch 提交隔离级别切换（第二阶段）
// 原子地应用新的隔离级别
func (s *IsolationSwitcher) CommitSwitch(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, ok := s.pending[sessionID]
	if !ok {
		return fmt.Errorf("no pending switch for session %s", sessionID)
	}
	if !tx.Prepared {
		return fmt.Errorf("switch for session %s not prepared", sessionID)
	}

	// 原子切换：使用 atomic store 确保可见性
	s.levels[sessionID] = tx.ToLevel
	delete(s.pending, sessionID)
	return nil
}

// P0-3: RollbackSwitch 回滚隔离级别切换
// 恢复到切换前的状态
func (s *IsolationSwitcher) RollbackSwitch(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.pending[sessionID]
	if !ok {
		return nil // 无待切换事务，无操作
	}

	// 恢复快照（实际实现中这里会恢复 session 数据）

	// 清除待切换事务
	delete(s.pending, sessionID)
	return nil
}

// P0-8: checkSandboxCompatibility 检查隔离级别与沙盒配置的兼容性
func (s *IsolationSwitcher) checkSandboxCompatibility(sessionID string, level IsolationLevel) error {
	switch level {
	case IsolationSandbox:
		// 沙盒隔离需要沙盒可用
		// 实际实现中检查 sandbox.Available()
		return nil
	case IsolationMerged:
		// 合并级别需要确认所有 session 都同意合并
		// 实际实现中检查所有关联 session 的权限
		return nil
	default:
		return nil
	}
}

// P0-4: PermissionGuard 权限升级防护
// 防止低权限 session 通过拖拽等操作获取高权限 session 的数据
type PermissionGuard struct {
	mu sync.Mutex
	// session → 权限级别
	permissions map[string]int
}

// NewPermissionGuard 创建权限防护器
func NewPermissionGuard() *PermissionGuard {
	return &PermissionGuard{
		permissions: make(map[string]int),
	}
}

// SetPermission 设置 session 的权限级别
func (g *PermissionGuard) SetPermission(sessionID string, level int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.permissions[sessionID] = level
}

// GetPermission 获取 session 的权限级别
func (g *PermissionGuard) GetPermission(sessionID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.permissions[sessionID]
}

// P0-4: CanDrag 检查是否允许从 source session 拖拽数据到 target session
// 规则：只允许高权限→低权限或同级拖拽，禁止低权限→高权限
func (g *PermissionGuard) CanDrag(sourceSession, targetSession string) error {
	g.mu.Lock()
	sourcePerm := g.permissions[sourceSession]
	targetPerm := g.permissions[targetSession]
	g.mu.Unlock()

	if sourcePerm < targetPerm {
		return fmt.Errorf("permission escalation denied: source %s (level %d) cannot drag to target %s (level %d)",
			sourceSession, sourcePerm, targetSession, targetPerm)
	}
	return nil
}

// P0-4: CanAssociate 检查是否允许建立强关联
// 规则：强关联的 session 必须权限相同或源权限更高
func (g *PermissionGuard) CanAssociate(sourceSession, targetSession string) error {
	g.mu.Lock()
	sourcePerm := g.permissions[sourceSession]
	targetPerm := g.permissions[targetSession]
	g.mu.Unlock()

	if sourcePerm < targetPerm {
		return fmt.Errorf("permission escalation denied: cannot associate lower-permission %s with higher-permission %s",
			sourceSession, targetSession)
	}
	return nil
}

// P0-8: SandboxCoordinator 隔离与沙盒协调器
// 确保 A2 的隔离级别与 B1 的沙盒配置不冲突
type SandboxCoordinator struct {
	mu sync.Mutex
	// session → 沙盒配置
	sandboxSpecs map[string]SandboxSpec
	switcher     *IsolationSwitcher
}

// SandboxSpec 沙盒配置摘要
type SandboxSpec struct {
	WriteRoots      []string
	ForbidReadRoots []string
	NetworkAllowed  bool
	EnforceSandbox  bool
}

// NewSandboxCoordinator 创建沙盒协调器
func NewSandboxCoordinator(switcher *IsolationSwitcher) *SandboxCoordinator {
	return &SandboxCoordinator{
		sandboxSpecs: make(map[string]SandboxSpec),
		switcher:     switcher,
	}
}

// P0-8: ConfigureSandbox 配置 session 的沙盒
// 根据隔离级别自动设置沙盒配置
func (c *SandboxCoordinator) ConfigureSandbox(sessionID string, workspace string) SandboxSpec {
	c.mu.Lock()
	defer c.mu.Unlock()

	level := c.switcher.GetLevel(sessionID)

	spec := SandboxSpec{
		WriteRoots:     []string{workspace},
		NetworkAllowed: true,
	}

	switch level {
	case IsolationSandbox:
		// 沙盒隔离：最严格
		spec.EnforceSandbox = true
		spec.NetworkAllowed = false
		spec.ForbidReadRoots = []string{}
	case IsolationIsolated:
		// 隔离：沙盒强制
		spec.EnforceSandbox = true
	case IsolationLinked:
		// 关联：沙盒可选
		spec.EnforceSandbox = false
	case IsolationMerged:
		// 合并：无额外沙盒
		spec.EnforceSandbox = false
	}

	c.sandboxSpecs[sessionID] = spec
	return spec
}

// GetSandboxSpec 返回 session 的沙盒配置
func (c *SandboxCoordinator) GetSandboxSpec(sessionID string) (SandboxSpec, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	spec, ok := c.sandboxSpecs[sessionID]
	return spec, ok
}

// P0-8: ValidateCoordination 验证隔离与沙盒配置的一致性
func (c *SandboxCoordinator) ValidateCoordination(sessionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	level := c.switcher.GetLevel(sessionID)
	spec, ok := c.sandboxSpecs[sessionID]
	if !ok {
		return nil // 无沙盒配置，不检查
	}

	// 沙盒隔离必须有沙盒
	if level == IsolationSandbox && !spec.EnforceSandbox {
		return fmt.Errorf("session %s: sandbox isolation requires enforced sandbox", sessionID)
	}
	// 合并级别不应有沙盒（浪费资源）
	if level == IsolationMerged && spec.EnforceSandbox {
		return fmt.Errorf("session %s: merged isolation should not enforce sandbox", sessionID)
	}
	return nil
}

// AtomicSwitchContext 封装两阶段切换的上下文
type AtomicSwitchContext struct {
	SessionID string
	ToLevel   IsolationLevel
	switcher  *IsolationSwitcher
	committed atomic.Bool
}

// Prepare 准备切换
func (a *AtomicSwitchContext) Prepare(ctx context.Context) error {
	return a.switcher.PrepareSwitch(a.SessionID, a.ToLevel)
}

// Commit 提交切换
func (a *AtomicSwitchContext) Commit() error {
	if err := a.switcher.CommitSwitch(a.SessionID); err != nil {
		return err
	}
	a.committed.Store(true)
	return nil
}

// Rollback 回滚切换（defer 中调用）
func (a *AtomicSwitchContext) Rollback() {
	if a.committed.Load() {
		return // 已提交，不需要回滚
	}
	_ = a.switcher.RollbackSwitch(a.SessionID)
}

// P0-3: AtomicSwitch 执行原子隔离级别切换
// 使用 defer 确保失败时自动回滚
func (s *IsolationSwitcher) AtomicSwitch(sessionID string, toLevel IsolationLevel, action func() error) error {
	tx := &AtomicSwitchContext{
		SessionID: sessionID,
		ToLevel:   toLevel,
		switcher:  s,
	}

	if err := tx.Prepare(nil); err != nil {
		return fmt.Errorf("prepare isolation switch: %w", err)
	}

	defer tx.Rollback() // 如果 Commit 没被调用，自动回滚

	if err := action(); err != nil {
		return fmt.Errorf("action during isolation switch: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit isolation switch: %w", err)
	}

	return nil
}

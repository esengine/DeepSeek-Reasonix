package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// GatewayConfig 是 BotGateway 的配置。
type GatewayConfig struct {
	Model             string
	ToolApprovalMode  string
	MaxSteps          int
	QueueMode         string
	QueueCap          int
	QueueDrop         string
	PairingEnabled    bool
	PairingTTL        time.Duration
	PairingMaxPending int
	// IgnoreSelfMessages drops messages that are clearly sent by this bot. It
	// uses configured SelfUserIDs plus recently returned outbound message IDs.
	IgnoreSelfMessages bool
	SelfUserIDs        map[Platform][]string
	ControlEnabled     bool
	ControlAddr        string
	ControlToken       string
	// ApprovalTimeout bounds how long a tool-approval/ask prompt blocks a bot
	// session waiting for a remote user's reply. Zero falls back to
	// defaultBotApprovalTimeout so an abandoned prompt can't wedge the bot forever
	// (#4626, #4402). A negative value disables the timeout (wait indefinitely).
	ApprovalTimeout    time.Duration
	WorkspaceRoot      string
	Channels           map[Platform]ChannelConfig
	ConnectionChannels map[string]ChannelConfig
	Routes             []RouteConfig
	ConnectionAccess   map[string]AccessConfig
	Allowlist          AllowlistConfig
	Enabled            map[Platform]bool
	Debounce           time.Duration
	// OnInbound observes every allowlisted inbound message before dispatch.
	//
	// Reentrancy contract for all GatewayConfig callbacks (OnInbound,
	// OnSessionReady, OnToolApprovalModeChange): they run synchronously on
	// gateway-owned dispatch/turn goroutines that Stop drains before returning.
	// A callback must therefore never call Stop, nor block until a goroutine
	// that does so completes — Stop would wait on the very goroutine running
	// the callback, a guaranteed deadlock. Hosts that want to shut the gateway
	// down in reaction to a callback must trigger the shutdown asynchronously.
	OnInbound func(InboundMessage)
	// OnSessionReady notifies the host after the bot has created or reused the
	// controller for an inbound remote. Hosts may persist the concrete session ID
	// or keep the remote as a read-only channel.
	OnSessionReady func(InboundMessage, string) error
	// OnToolApprovalModeChange persists a remote IM request such as /yolo on.
	// The gateway updates the live session and in-memory defaults first; this
	// callback lets desktop save the chosen connection mode to user config.
	OnToolApprovalModeChange func(InboundMessage, string) error
	// Desktop, when the gateway is embedded in the desktop app, gives bot
	// chats a god view over desktop sessions (/desktop commands): global
	// status, event subscriptions, and remote approvals for any live desktop
	// session. Nil when the gateway runs standalone (reasonix bot start).
	Desktop DesktopBridge
}

// ChannelConfig overrides gateway defaults for one IM channel.
type ChannelConfig struct {
	Model            string
	ToolApprovalMode string
	WorkspaceRoot    string
	SessionMappings  []SessionMapping
}

// SessionMapping is the runtime subset of a saved bot connection mapping used
// to route a remote chat/user/thread back to its intended workspace.
type SessionMapping struct {
	RemoteID      string
	SessionID     string
	SessionSource string
	ChatType      string
	UserID        string
	ThreadID      string
	Scope         string
	WorkspaceRoot string
	UpdatedAt     string
}

// RouteConfig applies per-remote overrides. Empty match fields are wildcards;
// the first matching route wins.
type RouteConfig struct {
	ConnectionID string
	Platform     Platform
	ChatType     ChatType
	ChatID       string
	UserID       string
	ThreadID     string
	Channel      ChannelConfig
}

// AdapterBinding attaches an adapter instance to one saved bot connection.
// Feishu and Lark share PlatformFeishu, so ID/Domain keep their sessions,
// replies, and per-connection settings separated at runtime.
type AdapterBinding struct {
	ID       string
	Domain   string
	Platform Platform
	Adapter  Adapter
}

// AllowlistConfig 控制哪些用户/群可以使用 bot。
type AllowlistConfig struct {
	Enabled   bool
	AllowAll  bool
	Users     map[Platform][]string
	Approvers map[Platform][]string
	Admins    map[Platform][]string
	Groups    map[Platform][]string
}

// AccessConfig controls who may use one concrete bot connection.
type AccessConfig struct {
	Enabled        bool
	AllowAll       bool
	PairingEnabled bool
	Users          []string
	Groups         []string
	Approvers      []string
	Admins         []string
}

// AdapterHealthSnapshot describes the gateway's current view of one adapter.
type AdapterHealthSnapshot struct {
	ID            string    `json:"id"`
	Platform      Platform  `json:"platform"`
	Domain        string    `json:"domain,omitempty"`
	Name          string    `json:"name,omitempty"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	LastMessageAt time.Time `json:"last_message_at,omitempty"`
	LastSendAt    time.Time `json:"last_send_at,omitempty"`
	LastErrorAt   time.Time `json:"last_error_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	Messages      int64     `json:"messages"`
	Sends         int64     `json:"sends"`
	SendErrors    int64     `json:"send_errors"`
	Closed        bool      `json:"closed"`
}

// BotGateway 是 reasonix bot 消息网关，管理 Controller 生命周期、session 并发、
// 事件渲染和平台适配器。
type BotGateway struct {
	cfg      GatewayConfig
	adapters []AdapterBinding
	sessions *SessionManager
	startErr []error

	lifecycleMu sync.Mutex
	started     bool
	stopped     bool
	runCancel   context.CancelFunc
	startDone   chan struct{}
	stopDone    chan struct{}
	gatewayWG   sync.WaitGroup
	turnWG      sync.WaitGroup

	mu                      sync.Mutex
	controllers             map[string]*sessionState // session key -> active state
	pendingReactionCleanups map[string][]func()
	allowlist               map[Platform]map[string]bool
	groupAllowlist          map[Platform]map[string]bool
	selfUserIDs             map[Platform]map[string]bool
	outboundMessageIDs      map[string]time.Time
	adapterHealth           map[string]*AdapterHealthSnapshot
	controlServer           *controlHTTPServer
	sessionOverrides        map[string]sessionRuntimeOverride

	logger *slog.Logger
}

// botController is the slice of the controller's driving port the gateway needs:
// session lifecycle, turn execution, and approval/ask handling. The bot never
// touches goals, checkpoints, or memory, so it depends on those sub-ports only —
// not the concrete *control.Controller and its ~99 methods.
type botController interface {
	control.Lifecycle
	control.TurnControl
	control.Approvals
}

type sessionState struct {
	ctrl             botController
	sink             *sessionEventSink
	platform         Platform
	connectionID     string
	model            string
	workspaceRoot    string
	toolApprovalMode string
	sessionPath      string
	cancel           context.CancelFunc
	pendingAsks      map[string][]event.AskQuestion
	pendingApprovals map[string]event.Approval
	lastApprovalID   string
	lastAskID        string
	createdAt        time.Time
	lastActive       time.Time
}

type sessionRuntimeProfile struct {
	model            string
	workspaceRoot    string
	toolApprovalMode string
	sessionPath      string
}

type sessionRuntimeOverride struct {
	channel     ChannelConfig
	sessionPath string
	label       string
}

type sessionEventSink struct {
	mu     sync.RWMutex
	target event.Sink
}

type pendingReactionAdapter interface {
	AddPendingReaction(ctx context.Context, messageID string) (func(), error)
}

const outboundEchoTTL = 10 * time.Minute

func (s *sessionEventSink) setTarget(target event.Sink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.target = target
}

func (s *sessionEventSink) Emit(e event.Event) {
	s.mu.RLock()
	target := s.target
	s.mu.RUnlock()
	if target != nil {
		target.Emit(e)
	}
}

// NewGateway 创建一个新的 BotGateway。
func NewGateway(cfg GatewayConfig, adapters map[Platform]Adapter, logger *slog.Logger) *BotGateway {
	bindings := make([]AdapterBinding, 0, len(adapters))
	for plat, adapter := range adapters {
		bindings = append(bindings, AdapterBinding{ID: string(plat), Platform: plat, Adapter: adapter})
	}
	return NewGatewayWithAdapterBindings(cfg, bindings, logger)
}

// NewGatewayWithAdapterBindings creates a gateway with one or more adapter
// instances per platform.
func NewGatewayWithAdapterBindings(cfg GatewayConfig, adapters []AdapterBinding, logger *slog.Logger) *BotGateway {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 1500 * time.Millisecond
	}
	cfg.QueueMode = NormalizeQueueMode(cfg.QueueMode)
	if cfg.QueueCap <= 0 {
		cfg.QueueCap = DefaultQueueCap
	}
	cfg.QueueDrop = NormalizeQueueDrop(cfg.QueueDrop)
	if cfg.PairingTTL <= 0 {
		cfg.PairingTTL = defaultPairingTTL
	}
	if cfg.PairingMaxPending <= 0 {
		cfg.PairingMaxPending = defaultPairingMaxPending
	}
	gw := &BotGateway{
		cfg:                     cfg,
		adapters:                normalizeAdapterBindings(adapters),
		sessions:                NewSessionManager(cfg.Debounce),
		controllers:             make(map[string]*sessionState),
		pendingReactionCleanups: make(map[string][]func()),
		allowlist:               make(map[Platform]map[string]bool),
		groupAllowlist:          make(map[Platform]map[string]bool),
		selfUserIDs:             make(map[Platform]map[string]bool),
		outboundMessageIDs:      make(map[string]time.Time),
		adapterHealth:           make(map[string]*AdapterHealthSnapshot),
		sessionOverrides:        make(map[string]sessionRuntimeOverride),
		logger:                  logger.With("component", "bot_gateway"),
	}
	gw.buildAllowlist()
	gw.buildSelfUserIDs()
	for _, binding := range gw.adapters {
		gw.setAdapterConfigured(binding)
	}
	return gw
}

func normalizeAdapterBindings(adapters []AdapterBinding) []AdapterBinding {
	out := make([]AdapterBinding, 0, len(adapters))
	for _, binding := range adapters {
		if binding.Adapter == nil {
			continue
		}
		if binding.Platform == "" {
			binding.Platform = binding.Adapter.Platform()
		}
		if strings.TrimSpace(binding.ID) == "" {
			binding.ID = string(binding.Platform)
		}
		binding.ID = strings.TrimSpace(binding.ID)
		binding.Domain = strings.TrimSpace(binding.Domain)
		out = append(out, binding)
	}
	return out
}

func (gw *BotGateway) buildAllowlist() {
	for _, plat := range []Platform{PlatformQQ, PlatformFeishu, PlatformWeixin} {
		gw.allowlist[plat] = make(map[string]bool)
		if !gw.cfg.Allowlist.Enabled {
			continue
		}
		addAllowlistUsers(gw.allowlist[plat], gw.cfg.Allowlist.Users[plat])
		addAllowlistUsers(gw.allowlist[plat], gw.cfg.Allowlist.Admins[plat])
		addAllowlistUsers(gw.allowlist[plat], gw.cfg.Allowlist.Approvers[plat])
		gw.groupAllowlist[plat] = make(map[string]bool)
		for _, gid := range gw.cfg.Allowlist.Groups[plat] {
			gw.groupAllowlist[plat][gid] = true
		}
	}
}

func addAllowlistUsers(dst map[string]bool, users []string) {
	for _, uid := range users {
		uid = strings.TrimSpace(uid)
		if uid != "" {
			dst[uid] = true
		}
	}
}

func (gw *BotGateway) buildSelfUserIDs() {
	for _, plat := range []Platform{PlatformQQ, PlatformFeishu, PlatformWeixin} {
		gw.selfUserIDs[plat] = stringSet(gw.cfg.SelfUserIDs[plat])
	}
}

// Start 启动所有已启用的平台适配器并开始处理消息。
func (gw *BotGateway) Start(ctx context.Context) (err error) {
	gw.lifecycleMu.Lock()
	if gw.stopped {
		gw.lifecycleMu.Unlock()
		return errors.New("bot gateway already stopped")
	}
	if gw.started {
		gw.lifecycleMu.Unlock()
		return errors.New("bot gateway already started")
	}
	gw.started = true
	runCtx, cancel := context.WithCancel(ctx)
	gw.runCancel = cancel
	startDone := make(chan struct{})
	gw.startDone = startDone
	gw.lifecycleMu.Unlock()
	defer func() {
		if err != nil {
			cancel()
		}
		gw.lifecycleMu.Lock()
		if err != nil {
			gw.runCancel = nil
		}
		close(startDone)
		gw.lifecycleMu.Unlock()
	}()

	started := make([]AdapterBinding, 0, len(gw.adapters))
	var startErr []error
	for _, binding := range gw.adapters {
		if !gw.cfg.Enabled[binding.Platform] {
			gw.logger.Info("platform disabled, skipping", "platform", binding.Platform, "connection", binding.ID)
			gw.markAdapterDisabled(binding)
			continue
		}
		gw.logger.Info("starting adapter", "platform", binding.Platform, "connection", binding.ID, "domain", binding.Domain)
		if err := binding.Adapter.Start(runCtx); err != nil {
			wrapped := fmt.Errorf("start adapter %s: %w", binding.ID, err)
			startErr = append(startErr, wrapped)
			gw.markAdapterStartFailed(binding, err)
			gw.logger.Warn("adapter start failed", "platform", binding.Platform, "connection", binding.ID, "domain", binding.Domain, "err", err)
			continue
		}
		gw.markAdapterStarted(binding)
		started = append(started, binding)
	}
	// SendToAdapter reads gw.adapters under gw.mu; publish the started set under
	// the same lock.
	gw.mu.Lock()
	gw.adapters = started
	gw.startErr = startErr
	gw.mu.Unlock()
	if len(started) == 0 && len(startErr) > 0 {
		return errors.Join(startErr...)
	}
	if err := gw.startControlServer(runCtx); err != nil {
		for _, binding := range started {
			_ = binding.Adapter.Stop()
		}
		return err
	}

	// 合并所有适配器的消息通道
	for _, binding := range gw.adapters {
		gw.gatewayWG.Add(1)
		go func() {
			defer gw.gatewayWG.Done()
			gw.dispatchLoop(runCtx, binding)
		}()
	}

	return nil
}

func (gw *BotGateway) AdapterCount() int {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	return len(gw.adapters)
}

func (gw *BotGateway) StartErrors() []error {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	out := make([]error, len(gw.startErr))
	copy(out, gw.startErr)
	return out
}

// AdapterHealth returns a stable snapshot of all configured adapter instances.
func (gw *BotGateway) AdapterHealth() []AdapterHealthSnapshot {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	out := make([]AdapterHealthSnapshot, 0, len(gw.adapterHealth))
	for _, health := range gw.adapterHealth {
		if health == nil {
			continue
		}
		out = append(out, *health)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (gw *BotGateway) setAdapterConfigured(binding AdapterBinding) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.ensureAdapterHealthLocked(binding).Status = "configured"
}

func (gw *BotGateway) markAdapterDisabled(binding AdapterBinding) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	health := gw.ensureAdapterHealthLocked(binding)
	health.Status = "disabled"
	health.Closed = true
}

func (gw *BotGateway) markAdapterStarted(binding AdapterBinding) {
	now := time.Now()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	health := gw.ensureAdapterHealthLocked(binding)
	health.Status = "running"
	health.StartedAt = now
	health.LastError = ""
	health.Closed = false
}

func (gw *BotGateway) markAdapterStartFailed(binding AdapterBinding, err error) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	health := gw.ensureAdapterHealthLocked(binding)
	health.Status = "error"
	health.Closed = true
	health.LastErrorAt = time.Now()
	if err != nil {
		health.LastError = err.Error()
	}
}

func (gw *BotGateway) markAdapterMessage(binding AdapterBinding) {
	now := time.Now()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	health := gw.ensureAdapterHealthLocked(binding)
	health.Status = "running"
	health.LastMessageAt = now
	health.Messages++
	health.Closed = false
}

func (gw *BotGateway) markAdapterClosed(binding AdapterBinding) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	health := gw.ensureAdapterHealthLocked(binding)
	if health.Status == "running" {
		health.Status = "closed"
	}
	health.Closed = true
}

func (gw *BotGateway) markAdapterSend(binding AdapterBinding, err error) {
	now := time.Now()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	health := gw.ensureAdapterHealthLocked(binding)
	if err != nil {
		health.SendErrors++
		health.LastErrorAt = now
		health.LastError = err.Error()
		if health.Status == "running" {
			health.Status = "degraded"
		}
		return
	}
	health.Sends++
	health.LastSendAt = now
	if health.Status == "degraded" {
		health.Status = "running"
	}
}

func (gw *BotGateway) ensureAdapterHealthLocked(binding AdapterBinding) *AdapterHealthSnapshot {
	id := strings.TrimSpace(binding.ID)
	if id == "" && binding.Adapter != nil {
		id = binding.Adapter.Name()
	}
	if id == "" {
		id = string(binding.Platform)
	}
	health := gw.adapterHealth[id]
	if health == nil {
		health = &AdapterHealthSnapshot{ID: id}
		gw.adapterHealth[id] = health
	}
	health.Platform = binding.Platform
	health.Domain = strings.TrimSpace(binding.Domain)
	if binding.Adapter != nil {
		health.Name = binding.Adapter.Name()
	}
	if strings.TrimSpace(health.Status) == "" {
		health.Status = "configured"
	}
	return health
}

// Stop 停止所有适配器并关闭所有 session。它会等待 dispatch 与 turn goroutine
// 全部退出，所以绝不能在 GatewayConfig 回调里同步调用（见 OnInbound 的
// reentrancy contract），否则 Stop 会等待正在运行该回调的 goroutine 自己。
func (gw *BotGateway) Stop() {
	gw.lifecycleMu.Lock()
	if gw.stopped {
		stopDone := gw.stopDone
		gw.lifecycleMu.Unlock()
		if stopDone != nil {
			<-stopDone
		}
		return
	}
	gw.stopped = true
	stopDone := make(chan struct{})
	gw.stopDone = stopDone
	cancel := gw.runCancel
	gw.runCancel = nil
	startDone := gw.startDone
	gw.lifecycleMu.Unlock()
	defer close(stopDone)

	if cancel != nil {
		cancel()
	}
	if startDone != nil {
		<-startDone
	}

	// Cancel sessions that already exist before waiting for dispatch to drain.
	// A dispatch already inside handleMessage may still publish a late session,
	// so closeSessions is repeated after gatewayWG and turnWG reach zero.
	gw.closeSessions()
	for _, binding := range gw.adapters {
		if err := binding.Adapter.Stop(); err != nil {
			gw.logger.Warn("error stopping adapter", "platform", binding.Platform, "connection", binding.ID, "err", err)
		}
		gw.markAdapterClosed(binding)
	}
	gw.stopControlServer()
	gw.gatewayWG.Wait()
	gw.closeSessions()
	gw.turnWG.Wait()
	gw.closeSessions()
}

func (gw *BotGateway) closeSessions() {
	var states []*sessionState
	gw.mu.Lock()
	for key, state := range gw.controllers {
		states = append(states, state)
		delete(gw.controllers, key)
	}
	gw.mu.Unlock()
	for _, state := range states {
		gw.closeSessionState(state)
	}
}

// closeSessionState tears down a session state that has been unlinked from
// gw.controllers. runTurn publishes state.cancel under gw.mu on every turn —
// possibly after the state was already unlinked — so snapshot and clear the
// field inside the lock and invoke it outside (the same discipline as
// cancelActiveSession).
func (gw *BotGateway) closeSessionState(state *sessionState) {
	if state == nil {
		return
	}
	gw.mu.Lock()
	cancel := state.cancel
	state.cancel = nil
	gw.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if state.ctrl != nil {
		state.ctrl.Close()
	}
}

func (gw *BotGateway) dispatchLoop(ctx context.Context, binding AdapterBinding) {
	for {
		select {
		case <-ctx.Done():
			gw.markAdapterClosed(binding)
			return
		case msg, ok := <-binding.Adapter.Messages():
			if !ok {
				gw.markAdapterClosed(binding)
				return
			}
			gw.markAdapterMessage(binding)
			gw.handleMessage(ctx, binding, msg)
		}
	}
}

func (gw *BotGateway) handleMessage(ctx context.Context, binding AdapterBinding, msg InboundMessage) {
	msg.Platform = binding.Platform
	if msg.ConnectionID == "" {
		msg.ConnectionID = binding.ID
	}
	if msg.Domain == "" {
		msg.Domain = binding.Domain
	}
	if gw.isSelfMessage(msg) {
		gw.logger.Debug("bot ignored self message", "platform", binding.Platform, "connection", msg.ConnectionID, "chat", hashID(msg.ChatID), "message", hashID(msg.MessageID), "user", hashID(msg.UserID))
		return
	}
	src := msg.Session()
	key := BuildSessionKey(src)
	logFields := []any{
		"platform", binding.Platform,
		"connection", msg.ConnectionID,
		"domain", msg.Domain,
		"chat_type", msg.ChatType,
		"chat", hashID(msg.ChatID),
		"user", hashID(msg.UserID),
		"operator", hashID(msg.OperatorID),
		"thread", hashID(msg.ThreadID),
		"message", hashID(msg.MessageID),
		"text_chars", len([]rune(msg.Text)),
		"session", key[:8],
	}
	gw.logger.Info("bot inbound message", logFields...)

	// allowlist 检查
	if !gw.checkAllowlist(binding.Platform, msg) {
		gw.logger.Info("user not in allowlist", "platform", binding.Platform, "connection", msg.ConnectionID, "user", hashID(msg.UserID))
		if gw.offerPairing(ctx, binding.Adapter, msg) {
			return
		}
		_ = gw.sendText(ctx, binding.Adapter, msg, "抱歉，您没有使用此 bot 的权限。")
		return
	}
	if gw.cfg.OnInbound != nil {
		gw.cfg.OnInbound(msg)
	}

	if normalized, ok := gw.normalizeApprovalShortcut(key, msg.Text); ok {
		msg.Text = normalized
	} else if normalized, ok := gw.normalizeAskShortcut(key, msg.Text); ok {
		msg.Text = normalized
	} else if _, ok := decisionShortcutCommand(msg.Text); ok && gw.sessions.IsActive(key) {
		_ = gw.sendText(ctx, binding.Adapter, msg, "没有找到可匹配的待处理操作。请重新触发一次操作后回复编号，或按消息中的 ID 使用 /approve、/deny 或 /answer。")
		return
	}

	// 斜杠命令处理
	if IsSlashBypass(msg.Text) {
		gw.logger.Info("bot slash command", logFields...)
		gw.handleSlashCommand(ctx, binding.Adapter, key, msg)
		return
	}

	// 已接管桌面会话的聊天：普通消息直接驱动那个桌面会话，不进 bot 自己的
	// 会话机器（斜杠命令仍走上面的分支，/desktop release 永远可达）。
	if gw.divertToDesktopTakeover(ctx, binding.Adapter, msg) {
		gw.logger.Info("bot message diverted to desktop takeover", logFields...)
		return
	}

	cleanup := gw.addPendingReaction(ctx, binding.Platform, binding.Adapter, msg)

	queueMode := gw.queueMode(key, msg)
	if gw.sessions.IsActive(key) {
		switch queueMode {
		case QueueModeSteer:
			if gw.steerActiveSession(ctx, binding.Adapter, key, msg) {
				gw.logger.Info("bot message steered into active turn", "session", key[:8])
				if cleanup != nil {
					cleanup()
				}
				_ = gw.sendText(ctx, binding.Adapter, msg, "已收到，会并入当前任务。")
				return
			}
		case QueueModeInterrupt:
			gw.cancelActiveSession(key)
			runReactionCleanups(gw.takeReactionCleanups(key))
			result := gw.sessions.ReplacePending(key, msg)
			gw.storeReactionCleanup(key, cleanup)
			gw.logger.Info("bot active turn interrupted; newest message queued", "session", key[:8], "pending", result.Pending)
			_ = gw.sendText(ctx, binding.Adapter, msg, "已停止当前任务，稍后处理这条新消息。")
			return
		}
	}

	// session 并发控制
	result := gw.sessions.TryAcquireWithQueue(key, msg, QueueOptions{
		Mode: queueMode,
		Cap:  gw.cfg.QueueCap,
		Drop: gw.cfg.QueueDrop,
	})
	if result.Rejected {
		gw.logger.Warn("bot queue rejected message", "session", key[:8], "pending", result.Pending, "mode", result.Mode)
		if cleanup != nil {
			cleanup()
		}
		_ = gw.sendText(ctx, binding.Adapter, msg, "当前会话排队已满，请稍后再发，或使用 /queue interrupt 中断当前任务。")
		return
	}
	if result.Queued {
		gw.logger.Debug("message queued", "session", key[:8], "mode", result.Mode, "pending", result.Pending, "dropped", result.Dropped)
		gw.storeReactionCleanup(key, cleanup)
		return
	}
	if !result.Acquired {
		gw.logger.Debug("session busy without queue action", "session", key[:8])
		gw.storeReactionCleanup(key, cleanup)
		return
	}

	// Run the turn on its own goroutine so the dispatch loop stays free to read
	// the next inbound message. A turn that hits interactive approval/ask blocks
	// inside RunTurn waiting for ctrl.Approve/AnswerQuestion — and the ONLY path
	// that calls those is handleSlashCommand on this same dispatch goroutine. Run
	// it inline and the loop can never deliver the /approve (or card) reply that
	// would unblock it: the session wedges until restart (#4701, #4863, #4402).
	// Per-session serialization is still held by the session lock (active[key]),
	// which the deferred Release inside runTurn clears.
	gw.turnWG.Add(1)
	go func() {
		defer gw.turnWG.Done()
		gw.runTurn(ctx, binding.Adapter, key, msg, cleanup)
	}()
}

func (gw *BotGateway) queueMode(key string, msg InboundMessage) string {
	return gw.sessions.QueueMode(key, gw.cfg.QueueMode)
}

func (gw *BotGateway) steerActiveSession(ctx context.Context, adapter Adapter, key string, msg InboundMessage) bool {
	text := strings.TrimSpace(msg.Text)
	if text == "" && len(msg.MediaURLs) == 0 && len(msg.Media) == 0 {
		return false
	}
	gw.mu.Lock()
	state, ok := gw.controllers[key]
	gw.mu.Unlock()
	if !ok || state.ctrl == nil {
		return false
	}
	text = gw.inputTextWithMedia(ctx, adapter, msg, state)
	if strings.TrimSpace(text) == "" {
		return false
	}
	state.ctrl.Steer(text)
	return true
}

func (gw *BotGateway) cancelActiveSession(key string) {
	// state.cancel is rewritten under gw.mu on every turn (runTurn), so copy it
	// inside the lock and invoke it outside.
	var cancel context.CancelFunc
	gw.mu.Lock()
	state, ok := gw.controllers[key]
	if ok && state != nil {
		cancel = state.cancel
	}
	gw.mu.Unlock()
	if !ok || state == nil {
		return
	}
	if cancel != nil {
		cancel()
		return
	}
	if state.ctrl != nil {
		state.ctrl.Cancel()
	}
}

func (gw *BotGateway) storeReactionCleanup(key string, cleanup func()) {
	if cleanup == nil {
		return
	}
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.pendingReactionCleanups[key] = append(gw.pendingReactionCleanups[key], cleanup)
}

func (gw *BotGateway) flushReactionCleanups(key string, cleanup func()) {
	stored := gw.takeReactionCleanups(key)
	runReactionCleanups(stored)
	if cleanup != nil {
		cleanup()
	}
}

func (gw *BotGateway) takeReactionCleanups(key string) []func() {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	stored := gw.pendingReactionCleanups[key]
	delete(gw.pendingReactionCleanups, key)
	return stored
}

func runReactionCleanups(cleanups []func()) {
	for _, cleanup := range cleanups {
		if cleanup != nil {
			cleanup()
		}
	}
}

func makeReactionCleanup(cleanups []func()) func() {
	if len(cleanups) == 0 {
		return nil
	}
	return func() {
		runReactionCleanups(cleanups)
	}
}

func (gw *BotGateway) addPendingReaction(ctx context.Context, plat Platform, adapter Adapter, msg InboundMessage) func() {
	if strings.TrimSpace(msg.MessageID) == "" {
		return nil
	}
	reactor, ok := adapter.(pendingReactionAdapter)
	if !ok {
		return nil
	}
	cleanup, err := reactor.AddPendingReaction(ctx, msg.MessageID)
	if err != nil {
		gw.logger.Warn("pending reaction failed", "platform", plat, "err", err)
		return nil
	}
	return cleanup
}

func (gw *BotGateway) isSelfMessage(msg InboundMessage) bool {
	if !gw.cfg.IgnoreSelfMessages {
		return false
	}
	actor := strings.TrimSpace(msg.UserID)
	if strings.TrimSpace(msg.OperatorID) != "" {
		actor = strings.TrimSpace(msg.OperatorID)
	}
	if actor != "" && gw.selfUserIDs[msg.Platform][actor] {
		return true
	}
	messageID := strings.TrimSpace(msg.MessageID)
	if messageID == "" {
		return false
	}
	key := outboundMessageKey(msg.Platform, msg.ConnectionID, msg.Domain, msg.ChatID, messageID)
	now := time.Now()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.pruneOutboundMessagesLocked(now)
	_, ok := gw.outboundMessageIDs[key]
	return ok
}

func (gw *BotGateway) rememberOutboundMessage(platform Platform, connID, domain, chatID, messageID string) {
	messageID = strings.TrimSpace(messageID)
	if !gw.cfg.IgnoreSelfMessages || messageID == "" {
		return
	}
	now := time.Now()
	key := outboundMessageKey(platform, connID, domain, chatID, messageID)
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.pruneOutboundMessagesLocked(now)
	gw.outboundMessageIDs[key] = now.Add(outboundEchoTTL)
}

func (gw *BotGateway) pruneOutboundMessagesLocked(now time.Time) {
	for key, expiresAt := range gw.outboundMessageIDs {
		if !expiresAt.After(now) {
			delete(gw.outboundMessageIDs, key)
		}
	}
}

func outboundMessageKey(platform Platform, connID, domain, chatID, messageID string) string {
	return strings.Join([]string{
		string(platform),
		strings.TrimSpace(connID),
		strings.TrimSpace(domain),
		strings.TrimSpace(chatID),
		strings.TrimSpace(messageID),
	}, "\x00")
}

func (gw *BotGateway) connectionAccess(msg InboundMessage) (AccessConfig, bool) {
	if gw.cfg.ConnectionAccess == nil {
		return AccessConfig{}, false
	}
	id := strings.TrimSpace(msg.ConnectionID)
	if id == "" {
		return AccessConfig{}, false
	}
	access, ok := gw.cfg.ConnectionAccess[id]
	if !ok {
		return AccessConfig{}, false
	}
	if !accessConfigActive(access) {
		return AccessConfig{}, false
	}
	return access, true
}

func accessConfigActive(access AccessConfig) bool {
	return access.Enabled ||
		access.AllowAll ||
		access.PairingEnabled ||
		len(access.Users) > 0 ||
		len(access.Groups) > 0 ||
		len(access.Approvers) > 0 ||
		len(access.Admins) > 0
}

func (gw *BotGateway) checkAllowlist(plat Platform, msg InboundMessage) bool {
	if access, ok := gw.connectionAccess(msg); ok {
		return checkConnectionAllowlist(access, msg)
	}
	if gw.cfg.Allowlist.AllowAll {
		return true
	}
	if !gw.cfg.Allowlist.Enabled {
		return false
	}
	actor := msg.UserID
	if msg.OperatorID != "" {
		actor = msg.OperatorID
	}
	if !gw.allowlist[plat][actor] {
		return false
	}
	groups := gw.groupAllowlist[plat]
	if chatUsesGroupAllowlist(msg.ChatType) && len(groups) > 0 && !groups[msg.ChatID] {
		return false
	}
	return true
}

func checkConnectionAllowlist(access AccessConfig, msg InboundMessage) bool {
	if access.AllowAll {
		return true
	}
	if !access.Enabled {
		return false
	}
	actor := msg.UserID
	if msg.OperatorID != "" {
		actor = msg.OperatorID
	}
	users := stringSet(append(append(append([]string{}, access.Users...), access.Admins...), access.Approvers...))
	groups := stringSet(access.Groups)
	actorAllowed := users[actor]
	groupAllowed := chatUsesGroupAllowlist(msg.ChatType) && groups[msg.ChatID]
	if len(users) == 0 && len(groups) == 0 {
		return false
	}
	return actorAllowed || groupAllowed
}

func (gw *BotGateway) requireCommandRole(ctx context.Context, adapter Adapter, msg InboundMessage, role string) bool {
	if gw.checkCommandRole(msg.Platform, msg, role) {
		return true
	}
	_ = gw.sendText(ctx, adapter, msg, "抱歉，你没有执行此 bot 命令的权限。")
	return false
}

func (gw *BotGateway) checkCommandRole(plat Platform, msg InboundMessage, role string) bool {
	actor := msg.UserID
	if msg.OperatorID != "" {
		actor = msg.OperatorID
	}
	if strings.TrimSpace(actor) == "" {
		return false
	}
	if access, ok := gw.connectionAccess(msg); ok {
		admins := stringSet(access.Admins)
		approvers := stringSet(access.Approvers)
		if len(admins) == 0 && len(approvers) == 0 {
			return true
		}
		if admins[actor] {
			return true
		}
		return role == "approver" && approvers[actor]
	}
	admins := stringSet(gw.cfg.Allowlist.Admins[plat])
	approvers := stringSet(gw.cfg.Allowlist.Approvers[plat])
	if len(admins) == 0 && len(approvers) == 0 {
		return true
	}
	if admins[actor] {
		return true
	}
	if role == "approver" && approvers[actor] {
		return true
	}
	return false
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func (gw *BotGateway) offerPairing(ctx context.Context, adapter Adapter, msg InboundMessage) bool {
	if access, ok := gw.connectionAccess(msg); ok {
		if !access.PairingEnabled {
			return false
		}
	} else if !gw.cfg.PairingEnabled {
		return false
	}
	req, created, err := CreateOrRefreshPairingRequest(msg, PairingConfig{
		Enabled:               true,
		RequestTTL:            gw.cfg.PairingTTL,
		MaxPendingPerPlatform: gw.cfg.PairingMaxPending,
	})
	if err != nil {
		gw.logger.Warn("bot pairing request failed", "platform", msg.Platform, "chat_type", msg.ChatType, "err", err)
		return false
	}
	prefix := "需要先完成配对。"
	if !created {
		prefix = "你已有待批准的配对请求。"
	}
	text := fmt.Sprintf("%s\n配对码: %s\n请在本机运行: reasonix bot pairing approve %s\n此码将在 %s 过期。",
		prefix, req.Code, req.Code, req.ExpiresAt.Local().Format("2006-01-02 15:04"))
	_ = gw.sendText(ctx, adapter, msg, text)
	return true
}

func chatUsesGroupAllowlist(chatType ChatType) bool {
	switch chatType {
	case ChatGroup, ChatGuild, ChatThread:
		return true
	default:
		return false
	}
}

func (gw *BotGateway) normalizeApprovalShortcut(key, text string) (string, bool) {
	command, ok := approvalShortcutCommand(text)
	if !ok {
		return "", false
	}
	approvalID := gw.currentPendingApprovalID(key)
	if approvalID == "" {
		return "", false
	}
	return command + " " + approvalID, true
}

func approvalShortcutCommand(text string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "1", "y", "yes", "ok", "同意", "批准", "允许", "允许一次":
		return "/approve", true
	case "2", "0", "n", "no", "deny", "拒绝":
		return "/deny", true
	default:
		return "", false
	}
}

func decisionShortcutCommand(text string) (string, bool) {
	if command, ok := approvalShortcutCommand(text); ok {
		return command, true
	}
	if _, ok := askShortcutAnswer(text); ok {
		return "/answer", true
	}
	return "", false
}

func (gw *BotGateway) currentPendingApprovalID(key string) string {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	state, ok := gw.controllers[key]
	if !ok || len(state.pendingApprovals) == 0 {
		return ""
	}
	if state.lastApprovalID != "" {
		if _, ok := state.pendingApprovals[state.lastApprovalID]; ok {
			return state.lastApprovalID
		}
	}
	for id := range state.pendingApprovals {
		return id
	}
	return ""
}

func (gw *BotGateway) forgetPendingApproval(key, id string) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	state, ok := gw.controllers[key]
	if !ok || state.pendingApprovals == nil {
		return
	}
	delete(state.pendingApprovals, id)
	if state.lastApprovalID == id {
		state.lastApprovalID = ""
		for nextID := range state.pendingApprovals {
			state.lastApprovalID = nextID
			break
		}
	}
}

func (gw *BotGateway) normalizeAskShortcut(key, text string) (string, bool) {
	raw := strings.TrimSpace(text)
	if raw == "" || strings.HasPrefix(raw, "/") {
		return "", false
	}
	askID := gw.currentPendingAskIDForReply(key)
	if askID == "" {
		return "", false
	}
	return "/answer " + askID + " " + raw, true
}

func askShortcutAnswer(text string) (string, bool) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return "", false
	}
	if strings.ContainsAny(raw, " \t\n;=") {
		return "", false
	}
	if _, err := strconv.Atoi(raw); err == nil {
		return raw, true
	}
	return "", false
}

func (gw *BotGateway) currentPendingAskIDForReply(key string) string {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	state, ok := gw.controllers[key]
	if !ok || len(state.pendingAsks) == 0 {
		return ""
	}
	if state.lastAskID != "" {
		if _, ok := state.pendingAsks[state.lastAskID]; ok {
			return state.lastAskID
		}
	}
	if len(state.pendingAsks) != 1 {
		return ""
	}
	for id := range state.pendingAsks {
		return id
	}
	return ""
}

func (gw *BotGateway) handleSlashCommand(ctx context.Context, adapter Adapter, key string, msg InboundMessage) {
	switch {
	case strings.HasPrefix(msg.Text, "/stop"):
		var cancel context.CancelFunc
		gw.mu.Lock()
		if state, ok := gw.controllers[key]; ok {
			cancel = state.cancel
		}
		gw.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		gw.sessions.ForceRelease(key)
		_ = gw.sendText(ctx, adapter, msg, "已停止当前任务。")

	case strings.HasPrefix(msg.Text, "/new") || strings.HasPrefix(msg.Text, "/reset"):
		var cancel context.CancelFunc
		gw.mu.Lock()
		state, ok := gw.controllers[key]
		if ok {
			cancel = state.cancel
		}
		gw.mu.Unlock()
		if ok {
			if cancel != nil {
				cancel()
			}
			// NewSession refuses to rotate while a turn is running; the cancel
			// above is asynchronous, so give the turn a bounded window to
			// unwind before rotating.
			deadline := time.Now().Add(5 * time.Second)
			for state.ctrl.Running() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if err := state.ctrl.NewSession(); err != nil {
				gw.logger.Warn("new session failed", "err", err)
				gw.sessions.ForceRelease(key)
				_ = gw.sendText(ctx, adapter, msg, "新会话创建失败，请稍后重试。")
				return
			}
			gw.rememberSessionReady(msg, state.ctrl)
		}
		gw.sessions.ForceRelease(key)
		_ = gw.sendText(ctx, adapter, msg, "已开始新会话。")

	case strings.HasPrefix(msg.Text, "/approve"):
		if !gw.requireCommandRole(ctx, adapter, msg, "approver") {
			return
		}
		// 从消息中解析 approval ID
		parts := strings.Fields(msg.Text)
		if len(parts) < 2 {
			_ = gw.sendText(ctx, adapter, msg, "用法: /approve <id>")
			return
		}
		gw.mu.Lock()
		state, ok := gw.controllers[key]
		gw.mu.Unlock()
		if ok && state.ctrl != nil {
			state.ctrl.Approve(parts[1], true, false, false)
			gw.forgetPendingApproval(key, parts[1])
			_ = gw.sendText(ctx, adapter, msg, "已批准。")
		} else {
			_ = gw.sendText(ctx, adapter, msg, "没有找到当前会话中的待审批操作，请重新触发一次操作。")
		}

	case strings.HasPrefix(msg.Text, "/deny"):
		if !gw.requireCommandRole(ctx, adapter, msg, "approver") {
			return
		}
		parts := strings.Fields(msg.Text)
		if len(parts) < 2 {
			_ = gw.sendText(ctx, adapter, msg, "用法: /deny <id>")
			return
		}
		gw.mu.Lock()
		state, ok := gw.controllers[key]
		gw.mu.Unlock()
		if ok && state.ctrl != nil {
			state.ctrl.Approve(parts[1], false, false, false)
			gw.forgetPendingApproval(key, parts[1])
			_ = gw.sendText(ctx, adapter, msg, "已拒绝。")
		} else {
			_ = gw.sendText(ctx, adapter, msg, "没有找到当前会话中的待审批操作，请重新触发一次操作。")
		}

	case strings.HasPrefix(msg.Text, "/answer"):
		parts := strings.Fields(msg.Text)
		if len(parts) < 3 {
			_ = gw.sendText(ctx, adapter, msg, "用法: /answer <id> <选项或 q1=选项;q2=选项>")
			return
		}
		askID := parts[1]
		rawAnswer := strings.TrimSpace(strings.Join(parts[2:], " "))
		gw.mu.Lock()
		state, ok := gw.controllers[key]
		var questions []event.AskQuestion
		if ok {
			questions = state.pendingAsks[askID]
			delete(state.pendingAsks, askID)
			if state.lastAskID == askID {
				state.lastAskID = ""
				for nextID := range state.pendingAsks {
					state.lastAskID = nextID
					break
				}
			}
		}
		gw.mu.Unlock()
		if !ok || state.ctrl == nil {
			_ = gw.sendText(ctx, adapter, msg, "没有找到当前会话。")
			return
		}
		answers := parseAskAnswers(questions, rawAnswer)
		state.ctrl.AnswerQuestion(askID, answers)
		_ = gw.sendText(ctx, adapter, msg, "已提交回答。")

	case strings.HasPrefix(msg.Text, "/yolo") || strings.HasPrefix(msg.Text, "/mode"):
		if !gw.requireCommandRole(ctx, adapter, msg, "admin") {
			return
		}
		mode, statusOnly, ok := parseToolApprovalModeCommand(msg.Text)
		if !ok {
			_ = gw.sendText(ctx, adapter, msg, "用法: /yolo on|off|auto|status，或 /mode yolo|ask|auto")
			return
		}
		if statusOnly {
			_ = gw.sendText(ctx, adapter, msg, gw.toolApprovalModeStatusText(key, msg))
			return
		}
		persistErr := gw.setToolApprovalModeForMessage(key, msg, mode)
		text := toolApprovalModeChangedText(mode)
		if persistErr != nil {
			text += "\n当前会话已生效，但保存到设置失败：" + persistErr.Error()
		}
		_ = gw.sendText(ctx, adapter, msg, text)

	case strings.HasPrefix(msg.Text, "/queue"):
		mode, clear, statusOnly, ok := parseQueueCommand(msg.Text)
		if !ok {
			_ = gw.sendText(ctx, adapter, msg, "用法: /queue steer|followup|collect|interrupt|status|default")
			return
		}
		if statusOnly {
			_ = gw.sendText(ctx, adapter, msg, gw.queueStatusText(key, msg))
			return
		}
		if clear {
			gw.sessions.ClearQueueMode(key)
			_ = gw.sendText(ctx, adapter, msg, "已恢复默认队列模式："+queueModeLabel(gw.queueMode(key, msg))+"。")
			return
		}
		gw.sessions.SetQueueMode(key, mode)
		_ = gw.sendText(ctx, adapter, msg, "已切换队列模式："+queueModeLabel(mode)+"。")

	case slashCommandVerb(msg.Text) == "/projects":
		if !gw.requireCommandRole(ctx, adapter, msg, "admin") {
			return
		}
		query := strings.TrimSpace(strings.TrimPrefix(msg.Text, "/projects"))
		_ = gw.sendText(ctx, adapter, msg, formatBotProjects(gw.buildProjectIndex(), query, botProjectListLimit))

	case slashCommandVerb(msg.Text) == "/use":
		if !gw.requireCommandRole(ctx, adapter, msg, "admin") {
			return
		}
		_ = gw.sendText(ctx, adapter, msg, gw.handleUseProjectCommand(key, msg.Text))

	case slashCommandVerb(msg.Text) == "/sessions":
		if !gw.requireCommandRole(ctx, adapter, msg, "admin") {
			return
		}
		_ = gw.sendText(ctx, adapter, msg, gw.handleSessionsCommand(msg.Text))

	case slashCommandVerb(msg.Text) == "/attach":
		if !gw.requireCommandRole(ctx, adapter, msg, "admin") {
			return
		}
		_ = gw.sendText(ctx, adapter, msg, gw.handleAttachSessionCommand(key, msg.Text))

	case slashCommandVerb(msg.Text) == "/search":
		if !gw.requireCommandRole(ctx, adapter, msg, "admin") {
			return
		}
		_ = gw.sendText(ctx, adapter, msg, gw.handleProjectSearchCommand(ctx, msg.Text))

	case strings.HasPrefix(msg.Text, "/desktop"):
		// God view over the embedding desktop app: listing every live desktop
		// session and answering its approvals is strictly more power than the
		// per-session approver role, so gate on admin.
		if !gw.requireCommandRole(ctx, adapter, msg, "admin") {
			return
		}
		_ = gw.sendText(ctx, adapter, msg, gw.handleDesktopCommand(msg))

	case strings.HasPrefix(msg.Text, "/status"):
		active := gw.sessions.ActiveCount()
		pending := gw.sessions.PendingCount(key)
		gw.mu.Lock()
		sessions := len(gw.controllers)
		gw.mu.Unlock()
		mode := gw.currentToolApprovalMode(key, msg)
		_ = gw.sendText(ctx, adapter, msg, fmt.Sprintf("活跃任务数: %d\n保留会话数: %d\n工具审批模式: %s\n队列模式: %s\n当前会话排队: %d\n连接健康: %s", active, sessions, toolApprovalModeLabel(mode), queueModeLabel(gw.queueMode(key, msg)), pending, gw.adapterHealthSummaryText()))

	case strings.HasPrefix(msg.Text, "/help"):
		help := "可用命令:\n" +
			"/stop - 停止当前任务\n" +
			"/new - 开始新会话\n" +
			"/reset - 重置会话\n" +
			"/approve <id> - 批准操作\n" +
			"/deny <id> - 拒绝操作\n" +
			"/answer <id> <选项> - 回答 ask 问题\n" +
			"/yolo on|off|auto|status - 切换或查看工具审批模式\n" +
			"/mode yolo|ask|auto - 切换工具审批模式\n" +
			"/queue steer|followup|collect|interrupt|status - 切换或查看队列模式\n" +
			"/projects [关键词] - 查看可切换项目索引\n" +
			"/use project <id|名称> - 将当前远端会话切到某个项目\n" +
			"/sessions search <关键词> - 搜索可 attach 的历史会话\n" +
			"/attach session <id|关键词> - 绑定当前远端会话到已有历史会话\n" +
			"/search all <关键词> - 跨已索引项目检索文件内容\n" +
			"/desktop status|watch|approve|deny|answer - 桌面端上帝视角(需内嵌运行)\n" +
			"/status - 查看状态\n" +
			"/help - 显示帮助"
		_ = gw.sendText(ctx, adapter, msg, help)
	}
}

func slashCommandVerb(text string) string {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[0])
}

func (gw *BotGateway) handleUseProjectCommand(key, text string) string {
	selector := parseUseProjectSelector(text)
	if selector == "" {
		return "用法: /use project <项目 id|名称|路径>，或 /use project default 恢复默认路由。"
	}
	if isDefaultBotSelector(selector) {
		gw.setSessionRuntimeOverride(key, sessionRuntimeOverride{}, false)
		return "已恢复当前远端会话的默认项目路由。下一条消息会按 bot 配置重新选择 workspace。"
	}
	projects := gw.buildProjectIndex()
	project, matches := resolveBotProject(projects, selector)
	if project.Root == "" {
		if len(matches) > 0 {
			return "匹配到多个项目，请使用项目 id：\n" + formatBotProjects(matches, "", botProjectListLimit)
		}
		return "没有匹配的项目。可先用 /projects 查看当前索引。"
	}
	gw.setSessionRuntimeOverride(key, sessionRuntimeOverride{
		channel: ChannelConfig{WorkspaceRoot: project.Root},
		label:   "project:" + project.ID,
	}, true)
	return fmt.Sprintf("已将当前远端会话切到项目 %s %s。\n下一条消息将在 %s 中运行。", project.ID, project.Name, displayBotPath(project.Root))
}

func parseUseProjectSelector(text string) string {
	parts := strings.Fields(text)
	if len(parts) < 2 || strings.ToLower(parts[0]) != "/use" {
		return ""
	}
	if len(parts) >= 3 && strings.EqualFold(parts[1], "project") {
		return strings.TrimSpace(strings.Join(parts[2:], " "))
	}
	return strings.TrimSpace(strings.Join(parts[1:], " "))
}

func (gw *BotGateway) handleSessionsCommand(text string) string {
	query := parseSessionsQuery(text)
	projects := gw.buildProjectIndex()
	sessions := gw.buildSessionIndex(projects)
	return formatBotSessions(sessions, query, botSessionListLimit)
}

func parseSessionsQuery(text string) string {
	parts := strings.Fields(text)
	if len(parts) <= 1 {
		return ""
	}
	if strings.EqualFold(parts[1], "search") {
		return strings.TrimSpace(strings.Join(parts[2:], " "))
	}
	return strings.TrimSpace(strings.Join(parts[1:], " "))
}

func (gw *BotGateway) handleAttachSessionCommand(key, text string) string {
	selector := parseAttachSessionSelector(text)
	if selector == "" {
		return "用法: /attach session <会话 id|关键词|path:...>"
	}
	projects := gw.buildProjectIndex()
	sessions := gw.buildSessionIndex(projects)
	session, matches := resolveBotSession(sessions, selector)
	if session.ID == "" {
		if len(matches) > 0 {
			return "匹配到多个会话，请使用会话 id：\n" + formatBotSessions(matches, "", botSessionListLimit)
		}
		return "没有匹配的会话。可先用 /sessions search <关键词> 查看当前索引。"
	}
	if session.SessionPath == "" {
		return "这个会话没有可恢复的 path: transcript，暂时不能 attach。"
	}
	if info, err := os.Stat(session.SessionPath); err != nil || info.IsDir() {
		return "会话文件不可用或已被移动：" + displayBotPath(session.SessionPath)
	}
	workspaceRoot := session.WorkspaceRoot
	if workspaceRoot == "" {
		project := botProjectForPath(projects, session.SessionPath)
		workspaceRoot = project.Root
	}
	gw.setSessionRuntimeOverride(key, sessionRuntimeOverride{
		channel:     ChannelConfig{WorkspaceRoot: workspaceRoot},
		sessionPath: session.SessionPath,
		label:       "session:" + session.ID,
	}, true)
	projectName := firstNonEmptyString(session.ProjectName, botProjectName(workspaceRoot), "global")
	return fmt.Sprintf("已 attach 到会话 %s（%s）。\n下一条消息会从 %s 继续。", session.ID, projectName, displayBotPath(session.SessionPath))
}

func parseAttachSessionSelector(text string) string {
	parts := strings.Fields(text)
	if len(parts) < 3 || !strings.EqualFold(parts[0], "/attach") || !strings.EqualFold(parts[1], "session") {
		return ""
	}
	return strings.TrimSpace(strings.Join(parts[2:], " "))
}

func (gw *BotGateway) handleProjectSearchCommand(ctx context.Context, text string) string {
	parts := strings.Fields(text)
	if len(parts) < 3 || !strings.EqualFold(parts[1], "all") {
		return "用法: /search all <关键词>"
	}
	query := strings.TrimSpace(strings.Join(parts[2:], " "))
	searchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	results, err := searchBotProjects(searchCtx, gw.buildProjectIndex(), query, botSearchListLimit)
	if err != nil {
		return "检索失败：" + err.Error()
	}
	return formatBotProjectSearchResults(results, botSearchListLimit)
}

func (gw *BotGateway) setSessionRuntimeOverride(key string, override sessionRuntimeOverride, enabled bool) {
	var old *sessionState
	gw.mu.Lock()
	if state, ok := gw.controllers[key]; ok {
		old = state
		delete(gw.controllers, key)
	}
	if enabled {
		override.sessionPath = canonicalBotPath(override.sessionPath)
		override.channel.WorkspaceRoot = canonicalBotPath(override.channel.WorkspaceRoot)
		gw.sessionOverrides[key] = override
	} else {
		delete(gw.sessionOverrides, key)
	}
	gw.mu.Unlock()
	gw.closeSessionState(old)
	gw.sessions.ForceRelease(key)
}

func (gw *BotGateway) sessionRuntimeOverrideForMessage(msg InboundMessage) (sessionRuntimeOverride, bool) {
	key := BuildSessionKey(msg.Session())
	gw.mu.Lock()
	defer gw.mu.Unlock()
	override, ok := gw.sessionOverrides[key]
	return override, ok
}

func isDefaultBotSelector(selector string) bool {
	switch strings.ToLower(strings.TrimSpace(selector)) {
	case "default", "reset", "inherit", "global", "none", "默认", "重置":
		return true
	default:
		return false
	}
}

func parseQueueCommand(text string) (mode string, clear bool, statusOnly bool, ok bool) {
	parts := strings.Fields(text)
	if len(parts) == 0 || strings.ToLower(strings.TrimSpace(parts[0])) != "/queue" {
		return "", false, false, false
	}
	if len(parts) == 1 {
		return "", false, true, true
	}
	switch strings.ToLower(strings.TrimSpace(parts[1])) {
	case "status", "state", "show", "状态", "查看":
		return "", false, true, true
	case "default", "reset", "inherit", "默认", "重置":
		return "", true, false, true
	default:
		if normalized := NormalizeOptionalQueueMode(parts[1]); normalized != "" {
			return normalized, false, false, true
		}
		return "", false, false, false
	}
}

func (gw *BotGateway) queueStatusText(key string, msg InboundMessage) string {
	return fmt.Sprintf("当前队列模式：%s\n当前会话排队: %d\n全局上限: %d\n溢出策略: %s\n用法：/queue steer|followup|collect|interrupt|status|default",
		queueModeLabel(gw.queueMode(key, msg)),
		gw.sessions.PendingCount(key),
		gw.cfg.QueueCap,
		queueDropLabel(gw.cfg.QueueDrop),
	)
}

func queueModeLabel(mode string) string {
	switch NormalizeQueueMode(mode) {
	case QueueModeFollowup:
		return "逐条跟进"
	case QueueModeCollect:
		return "合并收集"
	case QueueModeInterrupt:
		return "打断重跑"
	default:
		return "即时补充"
	}
}

func queueDropLabel(drop string) string {
	switch NormalizeQueueDrop(drop) {
	case QueueDropOld:
		return "丢弃最早消息"
	case QueueDropNew:
		return "拒绝新消息"
	default:
		return "压缩摘要"
	}
}

func (gw *BotGateway) adapterHealthSummaryText() string {
	snapshots := gw.AdapterHealth()
	if len(snapshots) == 0 {
		return "未启动"
	}
	parts := make([]string, 0, len(snapshots))
	for _, h := range snapshots {
		label := strings.TrimSpace(h.ID)
		if label == "" {
			label = string(h.Platform)
		}
		status := strings.TrimSpace(h.Status)
		if status == "" {
			status = "unknown"
		}
		parts = append(parts, fmt.Sprintf("%s=%s", label, status))
	}
	return strings.Join(parts, ", ")
}

func parseToolApprovalModeCommand(text string) (mode string, statusOnly bool, ok bool) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", false, false
	}
	cmd := strings.ToLower(strings.TrimSpace(parts[0]))
	switch cmd {
	case "/yolo":
		if len(parts) == 1 {
			return control.ToolApprovalYolo, false, true
		}
		return parseToolApprovalModeArg(parts[1])
	case "/mode":
		if len(parts) == 1 {
			return "", true, true
		}
		return parseToolApprovalModeArg(parts[1])
	default:
		return "", false, false
	}
}

func parseToolApprovalModeArg(arg string) (mode string, statusOnly bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "status", "state", "show", "状态", "查看":
		return "", true, true
	case "on", "enable", "enabled", "true", "1", "yolo", "full", "full-access", "bypass", "开启", "打开":
		return control.ToolApprovalYolo, false, true
	case "off", "disable", "disabled", "false", "0", "ask", "询问", "关闭":
		return control.ToolApprovalAsk, false, true
	case "auto", "自动":
		return control.ToolApprovalAuto, false, true
	default:
		return "", false, false
	}
}

func (gw *BotGateway) setToolApprovalModeForMessage(key string, msg InboundMessage, mode string) error {
	mode = normalizeBotToolApprovalMode(mode)
	var ctrl botController

	gw.mu.Lock()
	if state, ok := gw.controllers[key]; ok {
		ctrl = state.ctrl
	}
	gw.updateToolApprovalModeDefaultLocked(msg, mode)
	gw.mu.Unlock()

	if ctrl != nil {
		ctrl.SetToolApprovalMode(mode)
	}
	if gw.cfg.OnToolApprovalModeChange != nil {
		return gw.cfg.OnToolApprovalModeChange(msg, mode)
	}
	return nil
}

func (gw *BotGateway) updateToolApprovalModeDefaultLocked(msg InboundMessage, mode string) {
	if id := strings.TrimSpace(msg.ConnectionID); id != "" {
		if gw.cfg.ConnectionChannels == nil {
			gw.cfg.ConnectionChannels = make(map[string]ChannelConfig)
		}
		channel := gw.cfg.ConnectionChannels[id]
		channel.ToolApprovalMode = mode
		gw.cfg.ConnectionChannels[id] = channel
		return
	}
	if msg.Platform != "" {
		if gw.cfg.Channels == nil {
			gw.cfg.Channels = make(map[Platform]ChannelConfig)
		}
		channel := gw.cfg.Channels[msg.Platform]
		channel.ToolApprovalMode = mode
		gw.cfg.Channels[msg.Platform] = channel
		return
	}
	gw.cfg.ToolApprovalMode = mode
}

func (gw *BotGateway) currentToolApprovalMode(key string, msg InboundMessage) string {
	var ctrl botController
	gw.mu.Lock()
	if state, ok := gw.controllers[key]; ok {
		ctrl = state.ctrl
	}
	gw.mu.Unlock()
	if ctrl != nil {
		return ctrl.ToolApprovalMode()
	}
	_, _, mode := gw.sessionOptionsForMessage(msg)
	return mode
}

func (gw *BotGateway) toolApprovalModeStatusText(key string, msg InboundMessage) string {
	mode := gw.currentToolApprovalMode(key, msg)
	return fmt.Sprintf("当前工具审批模式：%s\n用法：/yolo on|off|auto|status，或 /mode yolo|ask|auto", toolApprovalModeLabel(mode))
}

func toolApprovalModeChangedText(mode string) string {
	switch normalizeBotToolApprovalMode(mode) {
	case control.ToolApprovalYolo:
		return "已开启 YOLO：普通工具审批将自动放行；Ask 问题和计划批准仍会等待确认。"
	case control.ToolApprovalAuto:
		return "已切换为自动模式：策略允许的工具会自动放行，仍保留需要询问或拒绝的规则。"
	default:
		return "已切回询问模式：工具执行前会请求确认。"
	}
}

func toolApprovalModeLabel(mode string) string {
	switch normalizeBotToolApprovalMode(mode) {
	case control.ToolApprovalYolo:
		return "YOLO"
	case control.ToolApprovalAuto:
		return "自动"
	default:
		return "询问"
	}
}

func (gw *BotGateway) runTurn(ctx context.Context, adapter Adapter, key string, msg InboundMessage, cleanup func()) {
	gw.logger.Info("bot turn started", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8])
	defer func() {
		// 检查是否有等待队列中的消息
		next := gw.sessions.Release(key)
		if next != nil {
			if cleanup != nil {
				cleanup()
			}
			nextCleanup := makeReactionCleanup(gw.takeReactionCleanups(key))
			gw.logger.Info("bot pending message released", "platform", next.Platform, "chat_type", next.ChatType, "chat", hashID(next.ChatID), "session", key[:8])
			gw.runTurn(ctx, adapter, key, *next, nextCleanup)
			return
		}
		gw.flushReactionCleanups(key, cleanup)
	}()

	// 获取或创建 Controller
	state := gw.getOrCreateSession(ctx, key, msg)
	if state == nil || state.ctrl == nil {
		_ = gw.sendText(ctx, adapter, msg, "内部错误：无法创建会话。")
		return
	}
	gw.rememberSessionReady(msg, state.ctrl)

	// 构建输入文本：群聊中在消息前加上发送者名，并把 IM 媒体保存为 @附件引用。
	input := gw.inputTextWithMedia(ctx, adapter, msg, state)
	if msg.ChatType == ChatGroup {
		userName := strings.TrimSpace(msg.UserName)
		if msg.ResolveUserName != nil {
			if resolved := strings.TrimSpace(msg.ResolveUserName(ctx)); resolved != "" {
				userName = resolved
			}
		}
		input = fmt.Sprintf("[%s] %s", userName, input)
	}

	// 发送"正在输入"状态
	_ = adapter.SendTyping(ctx, msg.ChatID)

	// 创建事件渲染 sink
	sink := newRenderSink(
		ctx,
		adapter,
		msg.ConnectionID,
		msg.Domain,
		msg.ChatID,
		msg.ChatType,
		msg.UserID,
		msg.MessageID,
		gw.logger,
		func(approval event.Approval) {
			gw.mu.Lock()
			if state.pendingApprovals == nil {
				state.pendingApprovals = make(map[string]event.Approval)
			}
			state.pendingApprovals[approval.ID] = approval
			state.lastApprovalID = approval.ID
			gw.mu.Unlock()
		},
		func(ask event.Ask) {
			gw.mu.Lock()
			if state.pendingAsks == nil {
				state.pendingAsks = make(map[string][]event.AskQuestion)
			}
			state.pendingAsks[ask.ID] = ask.Questions
			state.lastAskID = ask.ID
			gw.mu.Unlock()
		},
	)
	// Finish initializing the sink before publishing it as the live target: once
	// setTarget runs, other goroutines can reach this sink via state.sink.Emit.
	sink.ctrl = state.ctrl
	state.sink.setTarget(sink)
	defer state.sink.setTarget(nil)

	// 创建带取消的 context
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	gw.mu.Lock()
	live := gw.controllers[key] == state
	if live {
		state.cancel = cancel
	}
	state.lastActive = time.Now()
	gw.mu.Unlock()
	if !live {
		// The session was closed (gateway stop or runtime rebuild) after this
		// turn picked it up; a cancel published now would never be consumed, so
		// abort the turn instead of running it uncancellable.
		cancel()
	}

	// 运行一轮对话
	err := state.ctrl.RunTurn(turnCtx, input)
	sink.Emit(event.Event{Kind: event.TurnDone, Err: err})
	if err != nil {
		gw.logger.Warn("turn error", "session", key[:8], "err", err)
		return
	}
	gw.logger.Info("bot turn completed", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8])
}

func (gw *BotGateway) inputTextWithMedia(ctx context.Context, adapter Adapter, msg InboundMessage, state *sessionState) string {
	input := msg.Text
	if len(msg.MediaURLs) == 0 && len(msg.Media) == 0 {
		return input
	}
	workspaceRoot := ""
	if state != nil && state.ctrl != nil {
		workspaceRoot = state.ctrl.WorkspaceRoot()
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		_, workspaceRoot, _ = gw.sessionOptionsForMessage(msg)
	}
	refs, errs := saveInboundMedia(ctx, workspaceRoot, msg.MediaURLs)
	itemRefs, fallbacks, itemErrs := saveInboundMediaItems(ctx, workspaceRoot, msg.Media)
	refs = append(refs, itemRefs...)
	errs = append(errs, itemErrs...)
	if len(errs) > 0 {
		gw.logger.Warn("bot media attachment failed", "platform", msg.Platform, "chat", hashID(msg.ChatID), "errors", len(errs))
		_ = gw.sendText(ctx, adapter, msg, fmt.Sprintf("有 %d 个附件保存失败；我会先处理可用内容。", len(errs)))
	}
	return appendMediaRefs(appendMediaFallbacks(input, fallbacks), refs)
}

func (gw *BotGateway) getOrCreateSession(ctx context.Context, key string, msg InboundMessage) *sessionState {
	profile := gw.sessionProfileForMessage(msg)
	var stale *sessionState
	gw.mu.Lock()
	if state, ok := gw.controllers[key]; ok {
		if !sessionStateMatchesRuntime(state, profile) {
			delete(gw.controllers, key)
			stale = state
			gw.mu.Unlock()
			gw.closeSessionState(stale)
			gw.logger.Warn("bot session runtime changed; rebuilding", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8], "old_workspace_set", strings.TrimSpace(stale.workspaceRoot) != "", "new_workspace_set", profile.workspaceRoot != "", "old_model", stale.model, "new_model", profile.model)
		} else {
			updateSessionStateRuntime(state, msg, profile)
			gw.mu.Unlock()
			safeBotSetToolApprovalMode(state.ctrl, profile.toolApprovalMode)
			gw.logger.Info("bot session reused", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8])
			return state
		}
	} else {
		gw.mu.Unlock()
	}

	// 创建新 Controller
	sessionSink := &sessionEventSink{}
	gw.logger.Info("bot session creating", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8], "model", profile.model, "workspace_set", profile.workspaceRoot != "", "tool_approval_mode", profile.toolApprovalMode)
	ctrl, err := boot.Build(ctx, boot.Options{
		Model:           profile.model,
		MaxSteps:        gw.cfg.MaxSteps,
		MaxStepsKey:     "bot.max_steps",
		RequireKey:      true,
		Sink:            sessionSink,
		WorkspaceRoot:   profile.workspaceRoot,
		SessionDir:      botSessionDir(profile.workspaceRoot),
		ApprovalTimeout: gw.approvalTimeout(),
	})
	if err != nil {
		gw.logger.Error("build controller failed", "err", err)
		return nil
	}
	if profile.sessionPath != "" {
		loaded, err := agent.LoadSession(profile.sessionPath)
		if err != nil {
			ctrl.Close()
			if os.IsNotExist(err) {
				gw.logger.Error("attached bot session missing", "session_path", profile.sessionPath)
			} else {
				gw.logger.Error("attached bot session load failed", "session_path", profile.sessionPath, "err", err)
			}
			return nil
		}
		ctrl.Resume(loaded, profile.sessionPath)
	}
	ctrl.EnableInteractiveApproval()
	ctrl.SetToolApprovalMode(profile.toolApprovalMode)
	ctrl.EnsureSessionPath()

	var replace *sessionState
	gw.mu.Lock()
	// Re-check under the lock: while we were off-lock in boot.Build, a second
	// message for the same key may have built and registered its own session.
	// Reuse it only when it still targets this message's runtime profile.
	if existing, ok := gw.controllers[key]; ok {
		if sessionStateMatchesRuntime(existing, profile) {
			updateSessionStateRuntime(existing, msg, profile)
			gw.mu.Unlock()
			ctrl.Close()
			safeBotSetToolApprovalMode(existing.ctrl, profile.toolApprovalMode)
			gw.logger.Info("bot session built concurrently; discarding duplicate", "platform", msg.Platform, "chat", hashID(msg.ChatID), "session", key[:8])
			return existing
		}
		delete(gw.controllers, key)
		replace = existing
	}
	state := &sessionState{
		ctrl:             ctrl,
		sink:             sessionSink,
		platform:         msg.Platform,
		connectionID:     strings.TrimSpace(msg.ConnectionID),
		model:            profile.model,
		workspaceRoot:    profile.workspaceRoot,
		toolApprovalMode: profile.toolApprovalMode,
		sessionPath:      profile.sessionPath,
		pendingAsks:      make(map[string][]event.AskQuestion),
		createdAt:        time.Now(),
		lastActive:       time.Now(),
	}
	gw.controllers[key] = state
	gw.mu.Unlock()
	gw.closeSessionState(replace)

	gw.logger.Info("bot session created", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8])
	return state
}

func updateSessionStateRuntime(state *sessionState, msg InboundMessage, profile sessionRuntimeProfile) {
	if state == nil {
		return
	}
	if state.connectionID == "" {
		state.connectionID = strings.TrimSpace(msg.ConnectionID)
	}
	if state.platform == "" {
		state.platform = msg.Platform
	}
	state.model = profile.model
	state.workspaceRoot = profile.workspaceRoot
	state.toolApprovalMode = profile.toolApprovalMode
	state.sessionPath = profile.sessionPath
	state.lastActive = time.Now()
}

func (gw *BotGateway) sessionProfileForMessage(msg InboundMessage) sessionRuntimeProfile {
	model, workspaceRoot, toolApprovalMode := gw.sessionOptionsForMessage(msg)
	var sessionPath string
	if override, ok := gw.sessionRuntimeOverrideForMessage(msg); ok {
		sessionPath = override.sessionPath
	}
	return sessionRuntimeProfile{
		model:            strings.TrimSpace(model),
		workspaceRoot:    strings.TrimSpace(workspaceRoot),
		toolApprovalMode: normalizeBotToolApprovalMode(toolApprovalMode),
		sessionPath:      canonicalBotPath(sessionPath),
	}
}

func sessionStateMatchesRuntime(state *sessionState, profile sessionRuntimeProfile) bool {
	if state == nil || state.ctrl == nil {
		return false
	}
	if stateModel := strings.TrimSpace(state.model); stateModel != "" && profile.model != "" && stateModel != profile.model {
		return false
	}
	stateRoot := strings.TrimSpace(state.workspaceRoot)
	wantRoot := strings.TrimSpace(profile.workspaceRoot)
	if stateRoot == "" {
		root, ok := safeBotControllerWorkspaceRoot(state.ctrl)
		if ok {
			stateRoot = strings.TrimSpace(root)
		} else if wantRoot != "" {
			return false
		}
	}
	if stateRoot != wantRoot {
		return false
	}
	if canonicalBotPath(state.sessionPath) != canonicalBotPath(profile.sessionPath) {
		return false
	}
	if profile.sessionPath != "" && canonicalBotPath(state.ctrl.SessionPath()) != canonicalBotPath(profile.sessionPath) {
		return false
	}
	return true
}

func safeBotControllerWorkspaceRoot(ctrl botController) (root string, ok bool) {
	if ctrl == nil {
		return "", false
	}
	defer func() {
		if recover() != nil {
			root = ""
			ok = false
		}
	}()
	return ctrl.WorkspaceRoot(), true
}

func safeBotSetToolApprovalMode(ctrl botController, mode string) {
	if ctrl == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	ctrl.SetToolApprovalMode(mode)
}

// defaultBotApprovalTimeout caps how long a bot session waits for a remote
// user's approval/ask reply before treating it as denied, so an abandoned
// prompt (or a dropped IM event) can't leave the session wedged forever
// (#4626, #4402). 30 minutes is generous for a human reply yet bounded.
const defaultBotApprovalTimeout = 30 * time.Minute

// approvalTimeout resolves the configured bot approval wait: zero uses the
// bounded default; a negative value opts out (wait indefinitely).
func (gw *BotGateway) approvalTimeout() time.Duration {
	switch {
	case gw.cfg.ApprovalTimeout < 0:
		return 0
	case gw.cfg.ApprovalTimeout == 0:
		return defaultBotApprovalTimeout
	default:
		return gw.cfg.ApprovalTimeout
	}
}

func botSessionDir(workspaceRoot string) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return config.SessionDir()
	}
	if dir := config.ProjectSessionDir(workspaceRoot); dir != "" {
		return dir
	}
	return config.SessionDir()
}

func (gw *BotGateway) rememberSessionReady(msg InboundMessage, ctrl botController) {
	if gw.cfg.OnSessionReady == nil || ctrl == nil {
		return
	}
	sessionID := botSessionTarget(ctrl.SessionPath())
	if sessionID == "" {
		return
	}
	if err := gw.cfg.OnSessionReady(msg, sessionID); err != nil {
		gw.logger.Warn("remember bot session failed", "platform", msg.Platform, "connection", msg.ConnectionID, "err", err)
	}
}

func botSessionTarget(sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	return "path:" + sessionPath
}

func (gw *BotGateway) sessionOptionsForMessage(msg InboundMessage) (model string, workspaceRoot string, toolApprovalMode string) {
	// cfg.ToolApprovalMode / Channels / ConnectionChannels are rewritten under
	// gw.mu at runtime (/yolo, UpdateConnectionToolApprovalMode), so snapshot them
	// under a short lock and resolve outside it — applyRuntimeOverrideOptions
	// takes gw.mu itself. Copying the ChannelConfig value is enough: writers
	// replace whole map entries and never mutate SessionMappings in place.
	gw.mu.Lock()
	model = gw.cfg.Model
	workspaceRoot = gw.cfg.WorkspaceRoot
	toolApprovalMode = normalizeBotToolApprovalMode(gw.cfg.ToolApprovalMode)
	var connChannel ChannelConfig
	connOK := false
	if msg.ConnectionID != "" {
		connChannel, connOK = gw.cfg.ConnectionChannels[msg.ConnectionID]
	}
	platChannel, platOK := gw.cfg.Channels[msg.Platform]
	gw.mu.Unlock()

	var mappings []SessionMapping
	if connOK {
		applyBotChannelOptions(connChannel, &model, &workspaceRoot, &toolApprovalMode)
		mappings = connChannel.SessionMappings
		if mapping, ok := matchingSessionMapping(mappings, msg); ok {
			workspaceRoot = workspaceRootForSessionMapping(mapping, workspaceRoot)
		}
		model, workspaceRoot, toolApprovalMode = gw.applyRouteOptions(msg, model, workspaceRoot, toolApprovalMode)
		model, workspaceRoot, toolApprovalMode = gw.applyRuntimeOverrideOptions(msg, model, workspaceRoot, toolApprovalMode)
		return model, workspaceRoot, toolApprovalMode
	}
	if platOK {
		applyBotChannelOptions(platChannel, &model, &workspaceRoot, &toolApprovalMode)
		mappings = platChannel.SessionMappings
	}
	if mapping, ok := matchingSessionMapping(mappings, msg); ok {
		workspaceRoot = workspaceRootForSessionMapping(mapping, workspaceRoot)
	}
	model, workspaceRoot, toolApprovalMode = gw.applyRouteOptions(msg, model, workspaceRoot, toolApprovalMode)
	model, workspaceRoot, toolApprovalMode = gw.applyRuntimeOverrideOptions(msg, model, workspaceRoot, toolApprovalMode)
	return model, workspaceRoot, toolApprovalMode
}

func (gw *BotGateway) applyRuntimeOverrideOptions(msg InboundMessage, model, workspaceRoot, toolApprovalMode string) (string, string, string) {
	if override, ok := gw.sessionRuntimeOverrideForMessage(msg); ok {
		applyBotChannelOptions(override.channel, &model, &workspaceRoot, &toolApprovalMode)
	}
	return model, workspaceRoot, toolApprovalMode
}

func (gw *BotGateway) applyRouteOptions(msg InboundMessage, model, workspaceRoot, toolApprovalMode string) (string, string, string) {
	for _, route := range gw.cfg.Routes {
		if routeMatchesMessage(route, msg) {
			applyBotChannelOptions(route.Channel, &model, &workspaceRoot, &toolApprovalMode)
			break
		}
	}
	return model, workspaceRoot, toolApprovalMode
}

func applyBotChannelOptions(channel ChannelConfig, model *string, workspaceRoot *string, toolApprovalMode *string) {
	if value := strings.TrimSpace(channel.Model); value != "" {
		*model = value
	}
	if value := strings.TrimSpace(channel.WorkspaceRoot); value != "" {
		*workspaceRoot = value
	}
	if value := normalizeOptionalBotToolApprovalMode(channel.ToolApprovalMode); value != "" {
		*toolApprovalMode = value
	}
}

func matchingSessionMapping(mappings []SessionMapping, msg InboundMessage) (SessionMapping, bool) {
	for i := range mappings {
		if sessionMappingMatches(mappings[i], msg) {
			return mappings[i], true
		}
	}
	return SessionMapping{}, false
}

func sessionMappingMatches(mapping SessionMapping, msg InboundMessage) bool {
	if strings.TrimSpace(mapping.RemoteID) != strings.TrimSpace(msg.ChatID) {
		return false
	}
	chatType, userID, threadID := sessionMappingIdentity(msg)
	mappingChatType := strings.TrimSpace(mapping.ChatType)
	if mappingChatType == "" {
		return chatType == ""
	}
	if mappingChatType != chatType {
		return false
	}
	if strings.TrimSpace(mapping.UserID) != userID {
		return false
	}
	return strings.TrimSpace(mapping.ThreadID) == threadID
}

func sessionMappingIdentity(msg InboundMessage) (chatType string, userID string, threadID string) {
	switch msg.ChatType {
	case ChatGroup, ChatGuild:
		chatType = string(msg.ChatType)
		userID = strings.TrimSpace(msg.UserID)
	case ChatThread:
		chatType = string(msg.ChatType)
		threadID = strings.TrimSpace(msg.ThreadID)
		if threadID == "" {
			threadID = strings.TrimSpace(msg.ChatID)
		}
	}
	return chatType, userID, threadID
}

func workspaceRootForSessionMapping(mapping SessionMapping, fallback string) string {
	if root := strings.TrimSpace(mapping.WorkspaceRoot); root != "" {
		return root
	}
	if strings.EqualFold(strings.TrimSpace(mapping.Scope), "global") {
		return ""
	}
	return fallback
}

func routeMatchesMessage(route RouteConfig, msg InboundMessage) bool {
	if value := strings.TrimSpace(route.ConnectionID); value != "" && value != strings.TrimSpace(msg.ConnectionID) {
		return false
	}
	if route.Platform != "" && route.Platform != msg.Platform {
		return false
	}
	if route.ChatType != "" && route.ChatType != msg.ChatType {
		return false
	}
	if value := strings.TrimSpace(route.ChatID); value != "" && value != strings.TrimSpace(msg.ChatID) {
		return false
	}
	if value := strings.TrimSpace(route.UserID); value != "" && value != strings.TrimSpace(msg.UserID) {
		return false
	}
	if value := strings.TrimSpace(route.ThreadID); value != "" && value != strings.TrimSpace(msg.ThreadID) {
		return false
	}
	return true
}

func normalizeBotToolApprovalMode(mode string) string {
	if value := normalizeOptionalBotToolApprovalMode(mode); value != "" {
		return value
	}
	return control.ToolApprovalAsk
}

func normalizeOptionalBotToolApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case control.ToolApprovalAsk:
		return control.ToolApprovalAsk
	case control.ToolApprovalAuto:
		return control.ToolApprovalAuto
	case control.ToolApprovalYolo, "full", "full-access", "bypass":
		return control.ToolApprovalYolo
	default:
		return ""
	}
}

func (gw *BotGateway) sendText(ctx context.Context, adapter Adapter, msg InboundMessage, text string) error {
	out := OutboundMessage{
		ConnectionID: msg.ConnectionID,
		Domain:       msg.Domain,
		ChatID:       msg.ChatID,
		ChatType:     msg.ChatType,
		Text:         text,
		ReplyToMsgID: msg.MessageID,
	}
	binding := AdapterBinding{
		ID:       strings.TrimSpace(msg.ConnectionID),
		Domain:   strings.TrimSpace(msg.Domain),
		Platform: msg.Platform,
		Adapter:  adapter,
	}
	if binding.Platform == "" && adapter != nil {
		binding.Platform = adapter.Platform()
	}
	if binding.ID == "" && adapter != nil {
		binding.ID = adapter.Name()
	}
	result, err := gw.sendViaAdapter(ctx, binding, out)
	if err != nil {
		gw.logger.Warn("bot send failed", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "reply_to", hashID(msg.MessageID), "err", err)
		return err
	}
	gw.logger.Info("bot send completed", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "reply_to", hashID(msg.MessageID), "message", hashID(result.MessageID))
	return err
}

func (gw *BotGateway) sendViaAdapter(ctx context.Context, binding AdapterBinding, msg OutboundMessage) (SendResult, error) {
	if binding.Adapter == nil {
		return SendResult{}, errors.New("bot send: adapter is nil")
	}
	if strings.TrimSpace(msg.ConnectionID) == "" {
		msg.ConnectionID = binding.ID
	}
	if strings.TrimSpace(msg.Domain) == "" {
		msg.Domain = binding.Domain
	}
	result, err := binding.Adapter.Send(ctx, msg)
	gw.markAdapterSend(binding, err)
	for _, messageID := range result.DeliveredMessageIDs() {
		gw.rememberOutboundMessage(binding.Platform, binding.ID, binding.Domain, msg.ChatID, messageID)
	}
	return result, err
}

func parseAskAnswers(questions []event.AskQuestion, raw string) []event.AskAnswer {
	raw = strings.TrimSpace(raw)
	if len(questions) == 0 {
		return []event.AskAnswer{{Selected: []string{raw}}}
	}
	byID := make(map[string]*event.AskQuestion, len(questions))
	for i := range questions {
		q := &questions[i]
		byID[q.ID] = q
		byID[fmt.Sprintf("%d", i+1)] = q
	}
	answerMap := make(map[string][]string, len(questions))
	if strings.Contains(raw, "=") {
		for _, part := range strings.Split(raw, ";") {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			q := byID[strings.TrimSpace(k)]
			if q == nil {
				continue
			}
			answerMap[q.ID] = normalizeAskSelection(*q, strings.TrimSpace(v))
		}
	} else if len(questions) == 1 {
		answerMap[questions[0].ID] = normalizeAskSelection(questions[0], raw)
	}
	out := make([]event.AskAnswer, 0, len(questions))
	for _, q := range questions {
		out = append(out, event.AskAnswer{QuestionID: q.ID, Selected: answerMap[q.ID]})
	}
	return out
}

func normalizeAskSelection(q event.AskQuestion, raw string) []string {
	parts := []string{raw}
	if q.Multi && strings.Contains(raw, ",") {
		parts = strings.Split(raw, ",")
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx, err := strconv.Atoi(part); err == nil && idx >= 1 && idx <= len(q.Options) {
			out = append(out, q.Options[idx-1].Label)
			continue
		}
		out = append(out, part)
	}
	return out
}

// UpdateConnectionToolApprovalMode updates the in-memory tool approval mode for
// a single bot connection without restarting the gateway. Empty mode clears the
// connection override, so existing sessions inherit the current gateway default.
func (gw *BotGateway) UpdateConnectionToolApprovalMode(connID, mode string) {
	connID = strings.TrimSpace(connID)
	if connID == "" {
		return
	}
	mode = normalizeOptionalBotToolApprovalMode(mode)
	type controllerMode struct {
		ctrl botController
		mode string
	}
	var updates []controllerMode

	gw.mu.Lock()
	if gw.cfg.ConnectionChannels == nil {
		gw.cfg.ConnectionChannels = make(map[string]ChannelConfig)
	}
	ch := gw.cfg.ConnectionChannels[connID]
	ch.ToolApprovalMode = mode
	gw.cfg.ConnectionChannels[connID] = ch
	// Update every active session that belongs to this connection.
	for _, state := range gw.controllers {
		if state == nil || state.ctrl == nil || strings.TrimSpace(state.connectionID) != connID {
			continue
		}
		effectiveMode := mode
		if effectiveMode == "" {
			effectiveMode = normalizeBotToolApprovalMode(gw.cfg.ToolApprovalMode)
		}
		updates = append(updates, controllerMode{ctrl: state.ctrl, mode: effectiveMode})
	}
	gw.mu.Unlock()

	for _, update := range updates {
		update.ctrl.SetToolApprovalMode(update.mode)
	}
}

// SendToAdapter sends a message through the adapter identified by connID.
// Returns an error if no matching adapter is found.
func (gw *BotGateway) SendToAdapter(ctx context.Context, connID, domain string, msg OutboundMessage) (SendResult, error) {
	connID = strings.TrimSpace(connID)
	domain = strings.TrimSpace(domain)
	var target AdapterBinding
	gw.mu.Lock()
	for _, binding := range gw.adapters {
		if strings.TrimSpace(binding.ID) == connID &&
			(domain == "" || strings.EqualFold(strings.TrimSpace(binding.Domain), domain)) {
			target = binding
			break
		}
	}
	gw.mu.Unlock()
	if target.Adapter != nil {
		return gw.sendViaAdapter(ctx, target, msg)
	}
	return SendResult{}, fmt.Errorf("SendToAdapter: no adapter found for connection %q (domain %q)", connID, domain)
}

// SendTextToAdapter sends a plain text message through the adapter identified by connID.
func (gw *BotGateway) SendTextToAdapter(ctx context.Context, connID, domain, chatID string, chatType ChatType, text string) (SendResult, error) {
	return gw.SendToAdapter(ctx, connID, domain, OutboundMessage{
		ChatID:   chatID,
		ChatType: chatType,
		Text:     text,
	})
}

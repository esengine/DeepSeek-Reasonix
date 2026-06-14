package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/fileutil"
)

// GatewayConfig 是 BotGateway 的配置。
type GatewayConfig struct {
	Model              string
	ToolApprovalMode   string
	MaxSteps           int
	WorkspaceRoot      string
	SessionDir         string
	SessionSearchDirs  []string
	SessionMappingPath string
	SessionMappings    []SessionMapping
	Channels           map[Platform]ChannelConfig
	ConnectionChannels map[string]ChannelConfig
	Allowlist          AllowlistConfig
	Enabled            map[Platform]bool
	Debounce           time.Duration
	OnInbound          func(InboundMessage)
	// OnToolApprovalModeChange persists a remote IM request such as /yolo on.
	// The gateway updates the live session and in-memory defaults first; this
	// callback lets desktop save the chosen connection mode to user config.
	OnToolApprovalModeChange func(InboundMessage, string) error
}

// ChannelConfig overrides gateway defaults for one IM channel.
type ChannelConfig struct {
	Model            string
	ToolApprovalMode string
	WorkspaceRoot    string
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
	Enabled  bool
	AllowAll bool
	Users    map[Platform][]string
	Groups   map[Platform][]string
}

// SessionMapping binds a remote IM session key to a Reasonix session file.
type SessionMapping struct {
	RemoteKey     string    `json:"remote_key"`
	SessionPath   string    `json:"session_path"`
	SessionID     string    `json:"session_id,omitempty"`
	WorkspaceRoot string    `json:"workspace_root,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type sessionMappingFile struct {
	Version  int              `json:"version"`
	Mappings []SessionMapping `json:"mappings"`
}

// BotGateway 是 reasonix bot 消息网关，管理 Controller 生命周期、session 并发、
// 事件渲染和平台适配器。
type BotGateway struct {
	cfg      GatewayConfig
	adapters []AdapterBinding
	sessions *SessionManager
	startErr []error

	mu             sync.Mutex
	controllers    map[string]*sessionState // session key -> active state
	mappings       map[string]SessionMapping
	allowlist      map[Platform]map[string]bool
	groupAllowlist map[Platform]map[string]bool

	logger *slog.Logger
}

type sessionState struct {
	ctrl             *control.Controller
	sink             *sessionEventSink
	cancel           context.CancelFunc
	pendingAsks      map[string][]event.AskQuestion
	pendingApprovals map[string]event.Approval
	lastApprovalID   string
	lastAskID        string
	createdAt        time.Time
	lastActive       time.Time
}

type sessionEventSink struct {
	mu     sync.RWMutex
	target event.Sink
}

type pendingReactionAdapter interface {
	AddPendingReaction(ctx context.Context, messageID string) error
}

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
	gw := &BotGateway{
		cfg:            cfg,
		adapters:       normalizeAdapterBindings(adapters),
		sessions:       NewSessionManager(cfg.Debounce),
		controllers:    make(map[string]*sessionState),
		mappings:       make(map[string]SessionMapping),
		allowlist:      make(map[Platform]map[string]bool),
		groupAllowlist: make(map[Platform]map[string]bool),
		logger:         logger.With("component", "bot_gateway"),
	}
	gw.loadSessionMappings()
	gw.buildAllowlist()
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
		for _, uid := range gw.cfg.Allowlist.Users[plat] {
			gw.allowlist[plat][uid] = true
		}
		gw.groupAllowlist[plat] = make(map[string]bool)
		for _, gid := range gw.cfg.Allowlist.Groups[plat] {
			gw.groupAllowlist[plat][gid] = true
		}
	}
}

// Start 启动所有已启用的平台适配器并开始处理消息。
func (gw *BotGateway) Start(ctx context.Context) error {
	started := make([]AdapterBinding, 0, len(gw.adapters))
	var startErr []error
	for _, binding := range gw.adapters {
		if !gw.cfg.Enabled[binding.Platform] {
			gw.logger.Info("platform disabled, skipping", "platform", binding.Platform, "connection", binding.ID)
			continue
		}
		gw.logger.Info("starting adapter", "platform", binding.Platform, "connection", binding.ID, "domain", binding.Domain)
		if err := binding.Adapter.Start(ctx); err != nil {
			wrapped := fmt.Errorf("start adapter %s: %w", binding.ID, err)
			startErr = append(startErr, wrapped)
			gw.logger.Warn("adapter start failed", "platform", binding.Platform, "connection", binding.ID, "domain", binding.Domain, "err", err)
			continue
		}
		started = append(started, binding)
	}
	gw.adapters = started
	gw.startErr = startErr
	if len(started) == 0 && len(startErr) > 0 {
		return errors.Join(startErr...)
	}

	// 合并所有适配器的消息通道
	for _, binding := range gw.adapters {
		go gw.dispatchLoop(ctx, binding)
	}

	return nil
}

func (gw *BotGateway) AdapterCount() int {
	return len(gw.adapters)
}

func (gw *BotGateway) StartErrors() []error {
	out := make([]error, len(gw.startErr))
	copy(out, gw.startErr)
	return out
}

// Stop 停止所有适配器并关闭所有 session。
func (gw *BotGateway) Stop() {
	gw.mu.Lock()
	for key, state := range gw.controllers {
		if state.cancel != nil {
			state.cancel()
		}
		state.ctrl.Close()
		delete(gw.controllers, key)
	}
	gw.mu.Unlock()

	for _, binding := range gw.adapters {
		if err := binding.Adapter.Stop(); err != nil {
			gw.logger.Warn("error stopping adapter", "platform", binding.Platform, "connection", binding.ID, "err", err)
		}
	}
}

func (gw *BotGateway) dispatchLoop(ctx context.Context, binding AdapterBinding) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-binding.Adapter.Messages():
			if !ok {
				return
			}
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

	gw.addPendingReaction(ctx, binding.Platform, binding.Adapter, msg)

	// session 并发控制
	acquired, merged := gw.sessions.TryAcquire(key, msg)
	if merged {
		gw.logger.Debug("message merged to pending queue", "session", key[:8])
		return
	}
	if !acquired {
		// 正在处理中且非 bypass 命令，已在 TryAcquire 中入队
		gw.logger.Debug("session busy, queued", "session", key[:8])
		return
	}

	gw.runTurn(ctx, binding.Adapter, key, msg)
}

func (gw *BotGateway) addPendingReaction(ctx context.Context, plat Platform, adapter Adapter, msg InboundMessage) {
	if strings.TrimSpace(msg.MessageID) == "" {
		return
	}
	reactor, ok := adapter.(pendingReactionAdapter)
	if !ok {
		return
	}
	if err := reactor.AddPendingReaction(ctx, msg.MessageID); err != nil {
		gw.logger.Warn("pending reaction failed", "platform", plat, "err", err)
	}
}

func (gw *BotGateway) checkAllowlist(plat Platform, msg InboundMessage) bool {
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
	answer, ok := askShortcutAnswer(text)
	if !ok {
		return "", false
	}
	askID := gw.currentPendingAskID(key)
	if askID == "" {
		return "", false
	}
	return "/answer " + askID + " " + answer, true
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

func (gw *BotGateway) currentPendingAskID(key string) string {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	state, ok := gw.controllers[key]
	if !ok || len(state.pendingAsks) == 0 {
		return ""
	}
	if state.lastAskID != "" {
		if questions, ok := state.pendingAsks[state.lastAskID]; ok {
			if askQuestionsSupportNumericShortcut(questions) {
				return state.lastAskID
			}
			return ""
		}
	}
	var singleID string
	for id, questions := range state.pendingAsks {
		if askQuestionsSupportNumericShortcut(questions) {
			if singleID != "" {
				return ""
			}
			singleID = id
		}
	}
	return singleID
}

func askQuestionsSupportNumericShortcut(questions []event.AskQuestion) bool {
	return len(questions) == 1 && len(questions[0].Options) > 0
}

func (gw *BotGateway) handleSlashCommand(ctx context.Context, adapter Adapter, key string, msg InboundMessage) {
	switch {
	case strings.HasPrefix(msg.Text, "/stop"):
		gw.mu.Lock()
		state, ok := gw.controllers[key]
		gw.mu.Unlock()
		if ok && state.cancel != nil {
			state.cancel()
		}
		gw.sessions.ForceRelease(key)
		_ = gw.sendText(ctx, adapter, msg, "已停止当前任务。")

	case strings.HasPrefix(msg.Text, "/new") || strings.HasPrefix(msg.Text, "/reset"):
		gw.mu.Lock()
		state, ok := gw.controllers[key]
		gw.mu.Unlock()
		if ok {
			if state.cancel != nil {
				state.cancel()
			}
			if err := state.ctrl.NewSession(); err != nil {
				gw.logger.Warn("new session failed", "err", err)
				_ = gw.sendText(ctx, adapter, msg, "新会话创建失败："+err.Error())
				return
			}
			gw.ensureControllerSessionMapping(key, msg, state.ctrl)
		}
		gw.sessions.ForceRelease(key)
		_ = gw.sendText(ctx, adapter, msg, "已开始新会话。")

	case strings.HasPrefix(msg.Text, "/approve"):
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
			gw.clearRuntimeWait(key, "approval", parts[1])
			_ = gw.sendText(ctx, adapter, msg, "已批准。")
		} else {
			_ = gw.sendText(ctx, adapter, msg, "没有找到当前会话中的待审批操作，请重新触发一次操作。")
		}

	case strings.HasPrefix(msg.Text, "/deny"):
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
			gw.clearRuntimeWait(key, "approval", parts[1])
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
		gw.clearRuntimeWait(key, "ask", askID)
		_ = gw.sendText(ctx, adapter, msg, "已提交回答。")

	case strings.HasPrefix(msg.Text, "/yolo") || strings.HasPrefix(msg.Text, "/mode"):
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

	case strings.HasPrefix(msg.Text, "/status"):
		active := gw.sessions.ActiveCount()
		gw.mu.Lock()
		sessions := len(gw.controllers)
		state, hasState := gw.controllers[key]
		gw.mu.Unlock()
		status := gw.renderStatus(key, state, hasState, active, sessions)
		mode := gw.currentToolApprovalMode(key, msg)
		_ = gw.sendText(ctx, adapter, msg, status+"\n工具审批模式: "+toolApprovalModeLabel(mode))

	case strings.HasPrefix(msg.Text, "/approvals"):
		_ = gw.sendText(ctx, adapter, msg, gw.renderApprovals(key))

	case strings.HasPrefix(msg.Text, "/timeline"):
		_ = gw.sendText(ctx, adapter, msg, gw.renderTimeline(key, msg.Text))

	case strings.HasPrefix(msg.Text, "/wakeups"):
		_ = gw.sendText(ctx, adapter, msg, gw.renderWakeupHistory(key, msg.Text))

	case strings.HasPrefix(msg.Text, "/recap"):
		_ = gw.sendText(ctx, adapter, msg, gw.renderRecap(key, msg.Text))

	case strings.HasPrefix(msg.Text, "/sessions"):
		_ = gw.sendText(ctx, adapter, msg, gw.renderSessionList(8))

	case strings.HasPrefix(msg.Text, "/attach"):
		parts := strings.Fields(msg.Text)
		if len(parts) < 2 {
			_ = gw.sendText(ctx, adapter, msg, "用法: /attach <session-id-or-path>")
			return
		}
		if err := gw.attachSession(ctx, key, msg, parts[1]); err != nil {
			_ = gw.sendText(ctx, adapter, msg, "绑定失败："+err.Error())
			return
		}
		mapping, _ := gw.sessionMapping(key)
		_ = gw.sendText(ctx, adapter, msg, fmt.Sprintf("已绑定会话：%s", shortSessionID(mapping.SessionPath)))

	case strings.HasPrefix(msg.Text, "/detach"):
		if ok := gw.clearSessionMapping(key); ok {
			gw.mu.Lock()
			if state, exists := gw.controllers[key]; exists {
				if state.cancel != nil {
					state.cancel()
				}
				if state.ctrl != nil {
					state.ctrl.Close()
				}
				delete(gw.controllers, key)
			}
			gw.mu.Unlock()
			gw.sessions.ForceRelease(key)
			_ = gw.sendText(ctx, adapter, msg, "已解除当前 IM 会话绑定。")
		} else {
			_ = gw.sendText(ctx, adapter, msg, "当前 IM 会话没有绑定 Reasonix session。")
		}

	case strings.HasPrefix(msg.Text, "/goal"):
		gw.handleGoalCommand(ctx, adapter, key, msg)

	case strings.HasPrefix(msg.Text, "/help"):
		help := "可用命令:\n" +
			"/stop - 停止当前任务\n" +
			"/new - 开始新会话\n" +
			"/reset - 重置会话\n" +
			"/goal <text> - 设置目标\n" +
			"/goal continue - 继续执行目标\n" +
			"/goal status - 查看目标\n" +
			"/goal clear - 清除目标\n" +
			"/approve <id> - 批准操作\n" +
			"/deny <id> - 拒绝操作\n" +
			"/answer <id> <选项> - 回答 ask 问题\n" +
			"/approvals - 查看等待审批或回答的任务\n" +
			"/yolo on|off|auto|status - 切换或查看工具审批模式\n" +
			"/mode yolo|ask|auto - 切换工具审批模式\n" +
			"/sessions - 列出可绑定的 Reasonix 会话\n" +
			"/attach <id> - 绑定并恢复已有 Reasonix 会话\n" +
			"/detach - 解除当前 IM 会话绑定\n" +
			"/status - 查看状态\n" +
			"/timeline [n] - 查看当前会话最近运行事件\n" +
			"/wakeups [n] - 查看当前会话最近唤醒历史\n" +
			"/recap [YYYY-MM-DD] - 复盘当天自动处理和待决策任务\n" +
			"/help - 显示帮助"
		_ = gw.sendText(ctx, adapter, msg, help)
	}
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
	var ctrl *control.Controller

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
	var ctrl *control.Controller
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

func (gw *BotGateway) runTurn(ctx context.Context, adapter Adapter, key string, msg InboundMessage) {
	gw.logger.Info("bot turn started", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8])
	defer func() {
		// 检查是否有等待队列中的消息
		next := gw.sessions.Release(key)
		if next != nil {
			gw.logger.Info("bot pending message released", "platform", next.Platform, "chat_type", next.ChatType, "chat", hashID(next.ChatID), "session", key[:8])
			gw.runTurn(ctx, adapter, key, *next)
			return
		}
	}()

	// 构建输入文本：群聊中在消息前加上发送者名
	input := msg.Text
	if msg.ChatType == ChatGroup {
		input = fmt.Sprintf("[%s] %s", msg.UserName, msg.Text)
	}

	// 获取或创建 Controller
	state := gw.getOrCreateSession(ctx, key, msg)
	if state == nil || state.ctrl == nil {
		_ = gw.sendText(ctx, adapter, msg, "内部错误：无法创建会话。")
		return
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
			gw.recordRuntimeWait(key, agent.RuntimeWaitMeta{
				Kind:       "approval",
				Reason:     "approval required",
				ApprovalID: approval.ID,
				Tool:       approval.Tool,
				Subject:    approval.Subject,
				Since:      time.Now().UTC(),
			})
		},
		func(ask event.Ask) {
			gw.mu.Lock()
			if state.pendingAsks == nil {
				state.pendingAsks = make(map[string][]event.AskQuestion)
			}
			state.pendingAsks[ask.ID] = ask.Questions
			state.lastAskID = ask.ID
			gw.mu.Unlock()
			gw.recordRuntimeWait(key, agent.RuntimeWaitMeta{
				Kind:    "ask",
				Reason:  "user answer required",
				AskID:   ask.ID,
				Subject: askSubject(ask),
				Since:   time.Now().UTC(),
			})
		},
	)
	state.sink.setTarget(sink)
	defer state.sink.setTarget(nil)

	// 创建带取消的 context
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	gw.mu.Lock()
	state.cancel = cancel
	state.lastActive = time.Now()
	gw.mu.Unlock()

	// 运行一轮对话
	sink.ctrl = state.ctrl
	err := state.ctrl.RunTurn(turnCtx, input)
	sink.Emit(event.Event{Kind: event.TurnDone, Err: err})
	if err != nil {
		gw.logger.Warn("turn error", "session", key[:8], "err", err)
		return
	}
	gw.logger.Info("bot turn completed", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8])
}

func (gw *BotGateway) getOrCreateSession(ctx context.Context, key string, msg InboundMessage) *sessionState {
	gw.mu.Lock()
	if state, ok := gw.controllers[key]; ok {
		state.lastActive = time.Now()
		gw.mu.Unlock()
		gw.logger.Info("bot session reused", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8])
		return state
	}
	gw.mu.Unlock()

	if mapping, ok := gw.sessionMapping(key); ok {
		if state := gw.resumeMappedSession(ctx, key, msg, mapping); state != nil {
			return state
		}
	}

	// 创建新 Controller
	sessionSink := &sessionEventSink{}
	model, workspaceRoot, toolApprovalMode := gw.sessionOptionsForMessage(msg)
	gw.logger.Info("bot session creating", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8], "model", model, "workspace_set", strings.TrimSpace(workspaceRoot) != "", "tool_approval_mode", normalizeBotToolApprovalMode(toolApprovalMode))
	ctrl, err := gw.buildController(ctx, msg, sessionSink)
	if err != nil {
		gw.logger.Error("build controller failed", "err", err)
		return nil
	}
	ctrl.EnableInteractiveApproval()
	gw.ensureControllerSessionMapping(key, msg, ctrl)

	gw.mu.Lock()
	gw.controllers[key] = &sessionState{
		ctrl:        ctrl,
		sink:        sessionSink,
		pendingAsks: make(map[string][]event.AskQuestion),
		createdAt:   time.Now(),
		lastActive:  time.Now(),
	}
	state := gw.controllers[key]
	gw.mu.Unlock()

	gw.logger.Info("bot session created", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8])
	return state
}

func (gw *BotGateway) buildController(ctx context.Context, msg InboundMessage, sink event.Sink) (*control.Controller, error) {
	model, workspaceRoot, toolApprovalMode := gw.sessionOptionsForMessage(msg)
	ctrl, err := boot.Build(ctx, boot.Options{
		Model:         model,
		MaxSteps:      gw.cfg.MaxSteps,
		RequireKey:    true,
		Sink:          sink,
		WorkspaceRoot: workspaceRoot,
		SessionDir:    gw.cfg.SessionDir,
	})
	if err != nil {
		return nil, err
	}
	ctrl.SetToolApprovalMode(toolApprovalMode)
	return ctrl, nil
}

func (gw *BotGateway) resumeMappedSession(ctx context.Context, key string, msg InboundMessage, mapping SessionMapping) *sessionState {
	if strings.TrimSpace(mapping.SessionPath) == "" {
		return nil
	}
	loaded, err := agent.LoadSession(mapping.SessionPath)
	if err != nil {
		gw.logger.Warn("bot mapped session load failed", "session", shortSessionID(mapping.SessionPath), "err", err)
		return nil
	}
	sessionSink := &sessionEventSink{}
	ctrl, err := gw.buildController(ctx, msg, sessionSink)
	if err != nil {
		gw.logger.Warn("bot mapped controller build failed", "session", shortSessionID(mapping.SessionPath), "err", err)
		return nil
	}
	ctrl.Resume(loaded, mapping.SessionPath)
	ctrl.EnableInteractiveApproval()
	state := &sessionState{
		ctrl:        ctrl,
		sink:        sessionSink,
		pendingAsks: make(map[string][]event.AskQuestion),
		createdAt:   time.Now(),
		lastActive:  time.Now(),
	}
	gw.mu.Lock()
	gw.controllers[key] = state
	gw.mu.Unlock()
	gw.logger.Info("bot resumed mapped session", "session", shortSessionID(mapping.SessionPath))
	return state
}

func (gw *BotGateway) attachSession(ctx context.Context, key string, msg InboundMessage, ref string) error {
	path, err := gw.resolveSessionRef(ref)
	if err != nil {
		return err
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		return err
	}
	sessionSink := &sessionEventSink{}
	ctrl, err := gw.buildController(ctx, msg, sessionSink)
	if err != nil {
		return err
	}
	ctrl.Resume(loaded, path)
	ctrl.EnableInteractiveApproval()
	gw.mu.Lock()
	if existing, ok := gw.controllers[key]; ok {
		if existing.ctrl != nil && existing.ctrl.Running() {
			gw.mu.Unlock()
			ctrl.Close()
			return fmt.Errorf("当前会话正在运行，无法绑定")
		}
		if existing.cancel != nil {
			existing.cancel()
		}
		if existing.ctrl != nil {
			existing.ctrl.Close()
		}
	}
	gw.controllers[key] = &sessionState{
		ctrl:        ctrl,
		sink:        sessionSink,
		pendingAsks: make(map[string][]event.AskQuestion),
		createdAt:   time.Now(),
		lastActive:  time.Now(),
	}
	gw.mu.Unlock()
	if err := gw.setSessionMapping(key, path, gw.workspaceRootForMessage(msg)); err != nil {
		return err
	}
	return nil
}

func (gw *BotGateway) sessionOptionsForMessage(msg InboundMessage) (model string, workspaceRoot string, toolApprovalMode string) {
	model = gw.cfg.Model
	workspaceRoot = gw.cfg.WorkspaceRoot
	toolApprovalMode = normalizeBotToolApprovalMode(gw.cfg.ToolApprovalMode)
	if gw.cfg.ConnectionChannels != nil && msg.ConnectionID != "" {
		if channel, ok := gw.cfg.ConnectionChannels[msg.ConnectionID]; ok {
			if value := strings.TrimSpace(channel.Model); value != "" {
				model = value
			}
			if value := strings.TrimSpace(channel.WorkspaceRoot); value != "" {
				workspaceRoot = value
			}
			if value := normalizeOptionalBotToolApprovalMode(channel.ToolApprovalMode); value != "" {
				toolApprovalMode = value
			}
			return model, workspaceRoot, toolApprovalMode
		}
	}
	if gw.cfg.Channels == nil {
		return model, workspaceRoot, toolApprovalMode
	}
	channel, ok := gw.cfg.Channels[msg.Platform]
	if !ok {
		return model, workspaceRoot, toolApprovalMode
	}
	if value := strings.TrimSpace(channel.Model); value != "" {
		model = value
	}
	if value := strings.TrimSpace(channel.WorkspaceRoot); value != "" {
		workspaceRoot = value
	}
	if value := normalizeOptionalBotToolApprovalMode(channel.ToolApprovalMode); value != "" {
		toolApprovalMode = value
	}
	return model, workspaceRoot, toolApprovalMode
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

func (gw *BotGateway) workspaceRootForMessage(msg InboundMessage) string {
	_, workspaceRoot, _ := gw.sessionOptionsForMessage(msg)
	return workspaceRoot
}

func (gw *BotGateway) ensureControllerSessionMapping(key string, msg InboundMessage, ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	path := strings.TrimSpace(ctrl.SessionPath())
	if path == "" {
		dir := strings.TrimSpace(ctrl.SessionDir())
		if dir == "" {
			return
		}
		path = agent.NewSessionPath(dir, ctrl.Label())
		ctrl.SetSessionPath(path)
	}
	if err := gw.setSessionMapping(key, path, gw.workspaceRootForMessage(msg)); err != nil {
		gw.logger.Warn("bot session mapping save failed", "err", err, "session", shortSessionID(path))
	}
}

func (gw *BotGateway) setSessionMapping(key, path, workspaceRoot string) error {
	key = strings.TrimSpace(key)
	path = strings.TrimSpace(path)
	if key == "" || path == "" {
		return nil
	}
	gw.mu.Lock()
	gw.mappings[key] = SessionMapping{
		RemoteKey:     key,
		SessionPath:   path,
		SessionID:     shortSessionID(path),
		WorkspaceRoot: workspaceRoot,
		UpdatedAt:     time.Now().UTC(),
	}
	gw.mu.Unlock()
	return gw.saveSessionMappings()
}

func (gw *BotGateway) loadSessionMappings() {
	for _, mapping := range gw.cfg.SessionMappings {
		if mapping.RemoteKey == "" || mapping.SessionPath == "" {
			continue
		}
		gw.mappings[mapping.RemoteKey] = mapping
	}
	path := strings.TrimSpace(gw.cfg.SessionMappingPath)
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			gw.logger.Warn("bot session mappings load failed", "err", err)
		}
		return
	}
	var file sessionMappingFile
	if err := json.Unmarshal(b, &file); err != nil {
		gw.logger.Warn("bot session mappings decode failed", "err", err)
		return
	}
	for _, mapping := range file.Mappings {
		if mapping.RemoteKey == "" || mapping.SessionPath == "" {
			continue
		}
		gw.mappings[mapping.RemoteKey] = mapping
	}
}

func (gw *BotGateway) saveSessionMappings() error {
	path := strings.TrimSpace(gw.cfg.SessionMappingPath)
	if path == "" {
		return nil
	}
	gw.mu.Lock()
	mappings := make([]SessionMapping, 0, len(gw.mappings))
	for _, mapping := range gw.mappings {
		if strings.TrimSpace(mapping.RemoteKey) == "" || strings.TrimSpace(mapping.SessionPath) == "" {
			continue
		}
		mappings = append(mappings, mapping)
	}
	gw.mu.Unlock()
	data, err := json.MarshalIndent(sessionMappingFile{Version: 1, Mappings: mappings}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bot-session-mappings.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

func (gw *BotGateway) sessionMapping(key string) (SessionMapping, bool) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	mapping, ok := gw.mappings[key]
	return mapping, ok
}

func (gw *BotGateway) clearSessionMapping(key string) bool {
	gw.mu.Lock()
	_, ok := gw.mappings[key]
	if ok {
		delete(gw.mappings, key)
	}
	gw.mu.Unlock()
	if ok {
		if err := gw.saveSessionMappings(); err != nil {
			gw.logger.Warn("bot session mapping save failed", "err", err)
		}
	}
	return ok
}

func (gw *BotGateway) renderSessionList(limit int) string {
	sessions := gw.availableSessions()
	if len(sessions) == 0 {
		return "没有可绑定的 Reasonix session。"
	}
	if limit <= 0 || limit > len(sessions) {
		limit = len(sessions)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "可绑定会话（使用 /attach <id>）：")
	for _, session := range sessions[:limit] {
		id := shortSessionID(session.Path)
		preview := strings.TrimSpace(session.Preview)
		if preview == "" {
			preview = "(empty)"
		}
		fmt.Fprintf(&b, "\n%s  %s", id, truncateBotText(preview, 48))
	}
	if omitted := len(sessions) - limit; omitted > 0 {
		fmt.Fprintf(&b, "\n... 还有 %d 个", omitted)
	}
	return b.String()
}

func (gw *BotGateway) resolveSessionRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("session id required")
	}
	if filepath.IsAbs(ref) || strings.Contains(ref, string(filepath.Separator)) {
		path := ref
		if !filepath.IsAbs(path) {
			path = filepath.Clean(path)
		}
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
	var matches []agent.SessionInfo
	for _, session := range gw.availableSessions() {
		id := shortSessionID(session.Path)
		base := strings.TrimSuffix(filepath.Base(session.Path), filepath.Ext(session.Path))
		if strings.HasPrefix(id, ref) || strings.HasPrefix(base, ref) {
			matches = append(matches, session)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("session %q not found", ref)
	case 1:
		return matches[0].Path, nil
	default:
		return "", fmt.Errorf("session %q is ambiguous", ref)
	}
}

func (gw *BotGateway) availableSessions() []agent.SessionInfo {
	var out []agent.SessionInfo
	seen := make(map[string]struct{})
	for _, dir := range gw.sessionSearchDirs() {
		sessions, err := agent.ListSessions(dir)
		if err != nil {
			gw.logger.Warn("bot list sessions failed", "dir", dir, "err", err)
			continue
		}
		for _, session := range sessions {
			if _, ok := seen[session.Path]; ok {
				continue
			}
			seen[session.Path] = struct{}{}
			out = append(out, session)
		}
	}
	return out
}

func (gw *BotGateway) sessionSearchDirs() []string {
	var dirs []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		clean := filepath.Clean(dir)
		for _, existing := range dirs {
			if existing == clean {
				return
			}
		}
		dirs = append(dirs, clean)
	}
	add(gw.cfg.SessionDir)
	for _, dir := range gw.cfg.SessionSearchDirs {
		add(dir)
	}
	return dirs
}

func shortSessionID(path string) string {
	id := agent.BranchID(path)
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (gw *BotGateway) recordRuntimeWait(key string, wait agent.RuntimeWaitMeta) {
	sessionPath := gw.sessionPathForKey(key)
	if sessionPath == "" || wait.Kind == "" {
		return
	}
	meta, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil {
		gw.logger.Warn("bot runtime wait load failed", "session", shortSessionID(sessionPath), "err", err)
		return
	}
	if !ok {
		meta = agent.RuntimeMeta{}
	}
	gw.mu.Lock()
	state := gw.controllers[key]
	gw.mu.Unlock()
	if state != nil && state.ctrl != nil {
		snapshot := state.ctrl.RuntimeSnapshot()
		if snapshot.Goal.Text != "" || snapshot.Goal.Status != "" {
			meta.Goal = snapshot.Goal
		}
		if meta.Model == "" {
			meta.Model = snapshot.Model
		}
		if meta.WorkspaceRoot == "" {
			meta.WorkspaceRoot = snapshot.WorkspaceRoot
		}
		if meta.Run.LastTurnAt.IsZero() {
			meta.Run.LastTurnAt = snapshot.Run.LastTurnAt
		}
	}
	meta.Wait = wait
	meta.Run.Status = botWaitRunStatus(wait.Kind)
	if err := agent.SaveRuntimeMeta(sessionPath, meta); err != nil {
		gw.logger.Warn("bot runtime wait save failed", "session", shortSessionID(sessionPath), "err", err)
		return
	}
	_ = agent.AppendRuntimeTimeline(sessionPath, agent.RuntimeTimelineEvent{
		Type:       "wait_started",
		Source:     "bot",
		RunStatus:  meta.Run.Status,
		GoalStatus: meta.Goal.Status,
		WaitKind:   wait.Kind,
		WaitID:     firstNonEmpty(wait.ApprovalID, wait.AskID, wait.EventID),
		Tool:       wait.Tool,
		Subject:    wait.Subject,
		Reason:     wait.Reason,
		EventID:    wait.EventID,
	})
}

func (gw *BotGateway) clearRuntimeWait(key, kind, id string) {
	sessionPath := gw.sessionPathForKey(key)
	if sessionPath == "" {
		return
	}
	meta, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil {
		gw.logger.Warn("bot runtime wait clear load failed", "session", shortSessionID(sessionPath), "err", err)
		return
	}
	if !ok || meta.Wait.Kind == "" {
		return
	}
	if kind != "" && meta.Wait.Kind != kind {
		return
	}
	if id != "" && firstNonEmpty(meta.Wait.ApprovalID, meta.Wait.AskID, meta.Wait.EventID) != id {
		return
	}
	cleared := meta.Wait
	meta.Wait = agent.RuntimeWaitMeta{}
	meta.Run.Status = agent.RunStatusRunning
	if err := agent.SaveRuntimeMeta(sessionPath, meta); err != nil {
		gw.logger.Warn("bot runtime wait clear save failed", "session", shortSessionID(sessionPath), "err", err)
		return
	}
	_ = agent.AppendRuntimeTimeline(sessionPath, agent.RuntimeTimelineEvent{
		Type:       "wait_cleared",
		Source:     "bot",
		RunStatus:  meta.Run.Status,
		GoalStatus: meta.Goal.Status,
		WaitKind:   cleared.Kind,
		WaitID:     firstNonEmpty(cleared.ApprovalID, cleared.AskID, cleared.EventID),
		Tool:       cleared.Tool,
		Subject:    cleared.Subject,
		Reason:     cleared.Reason,
		EventID:    cleared.EventID,
	})
}

func botWaitRunStatus(kind string) string {
	switch kind {
	case "approval":
		return agent.RunStatusWaitingApproval
	case "ask":
		return agent.RunStatusWaitingAsk
	case "event":
		return agent.RunStatusWaitingEvent
	case "time":
		return agent.RunStatusWaitingTime
	case "file":
		return agent.RunStatusWaitingFile
	default:
		return "waiting_" + kind
	}
}

func (gw *BotGateway) sessionPathForKey(key string) string {
	gw.mu.Lock()
	state := gw.controllers[key]
	gw.mu.Unlock()
	if state != nil && state.ctrl != nil {
		if path := strings.TrimSpace(state.ctrl.SessionPath()); path != "" {
			return path
		}
	}
	if mapping, ok := gw.sessionMapping(key); ok {
		return strings.TrimSpace(mapping.SessionPath)
	}
	return ""
}

func (gw *BotGateway) renderStatus(key string, state *sessionState, hasState bool, active, sessions int) string {
	lines := []string{
		fmt.Sprintf("活跃任务数: %d", active),
		fmt.Sprintf("保留会话数: %d", sessions),
	}

	var sessionPath string
	if hasState && state != nil && state.ctrl != nil {
		sessionPath = strings.TrimSpace(state.ctrl.SessionPath())
	}
	if sessionPath == "" {
		if mapping, ok := gw.sessionMapping(key); ok {
			sessionPath = strings.TrimSpace(mapping.SessionPath)
		}
	}
	if sessionPath != "" {
		lines = append(lines, "会话: "+shortSessionID(sessionPath))
	}

	var meta agent.RuntimeMeta
	hasMeta := false
	if sessionPath != "" {
		loaded, ok, err := agent.LoadRuntimeMeta(sessionPath)
		if err != nil {
			gw.logger.Warn("bot status runtime load failed", "session", shortSessionID(sessionPath), "err", err)
			lines = append(lines, "runtime: 读取失败")
		} else if ok {
			meta = loaded
			hasMeta = true
		}
	}

	if hasState && state != nil && state.ctrl != nil {
		goal := state.ctrl.Goal()
		goalStatus := state.ctrl.GoalStatus()
		if goal != "" {
			lines = append(lines, "目标: "+truncateBotText(goal, 120))
		} else if hasMeta && meta.Goal.Text != "" {
			lines = append(lines, "目标: "+truncateBotText(meta.Goal.Text, 120))
		}
		if goalStatus != "" {
			lines = append(lines, "目标状态: "+goalStatus)
		} else if hasMeta && meta.Goal.Status != "" {
			lines = append(lines, "目标状态: "+meta.Goal.Status)
		}
		if hasMeta && meta.Run.Status != "" {
			lines = append(lines, "运行状态: "+meta.Run.Status)
		} else if state.ctrl.Running() {
			lines = append(lines, "运行状态: running")
		} else {
			lines = append(lines, "运行状态: idle")
		}
	} else if hasMeta {
		if meta.Goal.Text != "" {
			lines = append(lines, "目标: "+truncateBotText(meta.Goal.Text, 120))
		}
		if meta.Goal.Status != "" {
			lines = append(lines, "目标状态: "+meta.Goal.Status)
		}
		if meta.Run.Status != "" {
			lines = append(lines, "运行状态: "+meta.Run.Status)
		}
	}

	if hasMeta {
		lines = append(lines, renderRuntimeStatusLines(meta)...)
	}
	return strings.Join(lines, "\n")
}

func (gw *BotGateway) renderApprovals(key string) string {
	sessionPath := gw.sessionPathForKey(key)
	if sessionPath == "" {
		return "当前 IM 会话没有绑定 Reasonix session。"
	}
	meta, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil {
		gw.logger.Warn("bot approvals load failed", "session", shortSessionID(sessionPath), "err", err)
		return "审批台读取失败。"
	}
	if !ok || (meta.Wait.Kind != "approval" && meta.Wait.Kind != "ask") {
		return "当前没有等待审批或回答的任务。"
	}

	lines := []string{fmt.Sprintf("待处理审批/提问（%s）：", shortSessionID(sessionPath))}
	switch meta.Wait.Kind {
	case "approval":
		line := "审批: " + firstNonEmpty(meta.Wait.ApprovalID, "(unknown)")
		if meta.Wait.Tool != "" {
			line += "  tool=" + meta.Wait.Tool
		}
		if meta.Wait.Subject != "" {
			line += "  subject=" + truncateBotText(meta.Wait.Subject, 80)
		}
		lines = append(lines, line)
		if meta.Wait.Reason != "" {
			lines = append(lines, "原因: "+truncateBotText(meta.Wait.Reason, 100))
		}
		if meta.Wait.ApprovalID != "" {
			lines = append(lines, "命令: /approve "+meta.Wait.ApprovalID+" 或 /deny "+meta.Wait.ApprovalID)
		}
	case "ask":
		line := "提问: " + firstNonEmpty(meta.Wait.AskID, "(unknown)")
		if meta.Wait.Subject != "" {
			line += "  subject=" + truncateBotText(meta.Wait.Subject, 80)
		}
		lines = append(lines, line)
		if meta.Wait.Reason != "" {
			lines = append(lines, "原因: "+truncateBotText(meta.Wait.Reason, 100))
		}
		for _, qLine := range gw.renderPendingAskQuestions(key, meta.Wait.AskID) {
			lines = append(lines, qLine)
		}
		if meta.Wait.AskID != "" {
			lines = append(lines, "命令: /answer "+meta.Wait.AskID+" <选项>")
		}
	}
	return strings.Join(lines, "\n")
}

func (gw *BotGateway) renderPendingAskQuestions(key, askID string) []string {
	if askID == "" {
		return nil
	}
	gw.mu.Lock()
	state := gw.controllers[key]
	var questions []event.AskQuestion
	if state != nil && len(state.pendingAsks) > 0 {
		questions = append(questions, state.pendingAsks[askID]...)
	}
	gw.mu.Unlock()
	if len(questions) == 0 {
		return nil
	}
	lines := make([]string, 0, len(questions))
	for _, q := range questions {
		line := "问题"
		if q.ID != "" {
			line += " " + q.ID
		}
		if q.Prompt != "" {
			line += ": " + truncateBotText(q.Prompt, 90)
		}
		var options []string
		for _, opt := range q.Options {
			if opt.Label != "" {
				options = append(options, opt.Label)
			}
		}
		if len(options) > 0 {
			line += " [" + strings.Join(options, " / ") + "]"
		}
		lines = append(lines, line)
	}
	return lines
}

func (gw *BotGateway) renderTimeline(key, command string) string {
	sessionPath := gw.sessionPathForKey(key)
	if sessionPath == "" {
		return "当前 IM 会话没有绑定 Reasonix session。"
	}
	limit := parseTimelineLimit(command, 8)
	events, ok, err := agent.LoadRuntimeTimeline(sessionPath, limit)
	if err != nil {
		gw.logger.Warn("bot timeline load failed", "session", shortSessionID(sessionPath), "err", err)
		return "timeline 读取失败。"
	}
	if !ok || len(events) == 0 {
		return "当前会话还没有 runtime timeline 事件。"
	}
	lines := []string{fmt.Sprintf("最近运行事件（%s）：", shortSessionID(sessionPath))}
	for _, e := range events {
		lines = append(lines, renderTimelineEvent(e))
	}
	return strings.Join(lines, "\n")
}

func (gw *BotGateway) renderWakeupHistory(key, command string) string {
	sessionPath := gw.sessionPathForKey(key)
	if sessionPath == "" {
		return "当前 IM 会话没有绑定 Reasonix session。"
	}
	limit := parseTimelineLimit(command, 8)
	readLimit := limit * 4
	if readLimit < 32 {
		readLimit = 32
	}
	events, ok, err := agent.LoadRuntimeTimeline(sessionPath, readLimit)
	if err != nil {
		gw.logger.Warn("bot wakeup history load failed", "session", shortSessionID(sessionPath), "err", err)
		return "wakeup history 读取失败。"
	}
	if !ok || len(events) == 0 {
		return "当前会话还没有唤醒历史。"
	}
	wakeups := make([]agent.RuntimeTimelineEvent, 0, limit)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if !isWakeupTimelineEvent(e) {
			continue
		}
		wakeups = append(wakeups, e)
		if limit > 0 && len(wakeups) >= limit {
			break
		}
	}
	if len(wakeups) == 0 {
		return "当前会话还没有唤醒历史。"
	}
	for i, j := 0, len(wakeups)-1; i < j; i, j = i+1, j-1 {
		wakeups[i], wakeups[j] = wakeups[j], wakeups[i]
	}
	lines := []string{fmt.Sprintf("最近唤醒历史（%s）：", shortSessionID(sessionPath))}
	for _, e := range wakeups {
		lines = append(lines, renderTimelineEvent(e))
	}
	return strings.Join(lines, "\n")
}

func (gw *BotGateway) renderRecap(key, command string) string {
	sessionPath := gw.sessionPathForKey(key)
	if sessionPath == "" {
		return "当前 IM 会话没有绑定 Reasonix session。"
	}
	start, end, label, ok := parseRecapWindow(command, time.Now)
	if !ok {
		return "用法: /recap [YYYY-MM-DD]"
	}
	events, hasTimeline, err := agent.LoadRuntimeTimeline(sessionPath, 0)
	if err != nil {
		gw.logger.Warn("bot recap timeline load failed", "session", shortSessionID(sessionPath), "err", err)
		return "复盘 timeline 读取失败。"
	}
	meta, hasMeta, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil {
		gw.logger.Warn("bot recap runtime load failed", "session", shortSessionID(sessionPath), "err", err)
		return "复盘 runtime 读取失败。"
	}

	dayEvents := filterRecapEvents(events, start, end)
	stats := summarizeRecapEvents(dayEvents)
	lines := []string{fmt.Sprintf("任务复盘（%s，%s）：", shortSessionID(sessionPath), label)}
	if !hasTimeline || len(dayEvents) == 0 {
		lines = append(lines, "自动处理: 今天还没有记录到 runtime 事件。")
	} else {
		lines = append(lines, fmt.Sprintf(
			"自动处理: 唤醒 %d 次，运行完成 %d 次，等待用户 %d 次，预算阻断 %d 次。",
			stats.Wakeups, stats.RunFinished, stats.WaitStarted, stats.BudgetBlocked,
		))
		if stats.ModelCalls > 0 {
			cost := ""
			if stats.Cost > 0 {
				currency := stats.Currency
				if currency == "" {
					currency = "cost"
				}
				cost = fmt.Sprintf("，费用 %s %.4f", currency, stats.Cost)
			}
			lines = append(lines, fmt.Sprintf("模型使用: 调用 %d 次，tokens %d%s。", stats.ModelCalls, stats.Tokens, cost))
		}
		lines = append(lines, "最近事件:")
		for _, event := range lastRecapEvents(dayEvents, 5) {
			lines = append(lines, "- "+renderTimelineEvent(event))
		}
	}

	decisionLines := gw.renderRecapDecisionLines(key, meta, hasMeta)
	lines = append(lines, decisionLines...)
	return strings.Join(lines, "\n")
}

type recapStats struct {
	Wakeups       int
	RunFinished   int
	WaitStarted   int
	BudgetBlocked int
	ModelCalls    int
	Tokens        int
	Cost          float64
	Currency      string
}

func parseRecapWindow(command string, now func() time.Time) (time.Time, time.Time, string, bool) {
	if now == nil {
		now = time.Now
	}
	parts := strings.Fields(command)
	loc := time.Local
	if len(parts) >= 2 {
		day, err := time.ParseInLocation("2006-01-02", parts[1], loc)
		if err != nil {
			return time.Time{}, time.Time{}, "", false
		}
		return day, day.Add(24 * time.Hour), parts[1], true
	}
	t := now().In(loc)
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	return start, start.Add(24 * time.Hour), start.Format("2006-01-02"), true
}

func filterRecapEvents(events []agent.RuntimeTimelineEvent, start, end time.Time) []agent.RuntimeTimelineEvent {
	if start.IsZero() || end.IsZero() {
		return nil
	}
	out := make([]agent.RuntimeTimelineEvent, 0, len(events))
	for _, event := range events {
		if event.Time.IsZero() {
			continue
		}
		t := event.Time.In(start.Location())
		if (t.Equal(start) || t.After(start)) && t.Before(end) {
			out = append(out, event)
		}
	}
	return out
}

func summarizeRecapEvents(events []agent.RuntimeTimelineEvent) recapStats {
	var stats recapStats
	for _, event := range events {
		if isWakeupTimelineEvent(event) {
			stats.Wakeups++
		}
		switch event.Type {
		case "run_finished":
			stats.RunFinished++
		case "wait_started":
			stats.WaitStarted++
		case "wakeup_budget_blocked":
			stats.BudgetBlocked++
		case "model_usage":
			stats.ModelCalls++
			stats.Tokens += event.Total
			if event.Cost > 0 {
				stats.Cost += event.Cost
				if stats.Currency == "" {
					stats.Currency = event.Currency
				}
			}
		}
	}
	return stats
}

func lastRecapEvents(events []agent.RuntimeTimelineEvent, limit int) []agent.RuntimeTimelineEvent {
	if limit <= 0 || len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}

func (gw *BotGateway) renderRecapDecisionLines(key string, meta agent.RuntimeMeta, hasMeta bool) []string {
	if !hasMeta || (meta.Wait.Kind != "approval" && meta.Wait.Kind != "ask") {
		return []string{"待决策: 无。"}
	}
	lines := []string{"待决策:"}
	switch meta.Wait.Kind {
	case "approval":
		id := firstNonEmpty(meta.Wait.ApprovalID, "(unknown)")
		line := "- 审批 " + id
		if meta.Wait.Tool != "" {
			line += " tool=" + meta.Wait.Tool
		}
		if meta.Wait.Subject != "" {
			line += " subject=" + truncateBotText(meta.Wait.Subject, 80)
		}
		lines = append(lines, line)
		if meta.Wait.Reason != "" {
			lines = append(lines, "  原因: "+truncateBotText(meta.Wait.Reason, 100))
		}
		if meta.Wait.ApprovalID != "" {
			lines = append(lines, "  命令: /approve "+meta.Wait.ApprovalID+" 或 /deny "+meta.Wait.ApprovalID)
		}
	case "ask":
		id := firstNonEmpty(meta.Wait.AskID, "(unknown)")
		line := "- 回答 " + id
		if meta.Wait.Subject != "" {
			line += " subject=" + truncateBotText(meta.Wait.Subject, 80)
		}
		lines = append(lines, line)
		if meta.Wait.Reason != "" {
			lines = append(lines, "  原因: "+truncateBotText(meta.Wait.Reason, 100))
		}
		for _, qLine := range gw.renderPendingAskQuestions(key, meta.Wait.AskID) {
			lines = append(lines, "  "+qLine)
		}
		if meta.Wait.AskID != "" {
			lines = append(lines, "  命令: /answer "+meta.Wait.AskID+" <选项>")
		}
	}
	return lines
}

func parseTimelineLimit(command string, fallback int) int {
	parts := strings.Fields(command)
	if len(parts) < 2 {
		return fallback
	}
	limit, err := strconv.Atoi(parts[1])
	if err != nil || limit < 0 {
		return fallback
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func isWakeupTimelineEvent(e agent.RuntimeTimelineEvent) bool {
	switch e.Type {
	case "wakeup_budget_blocked", "wait_time_reached", "wait_event_failure_detected", "file_change_detected":
		return true
	case "intent_queued":
		switch e.Source {
		case "cron", "time", "webhook", "file_watch":
			return true
		}
	}
	return false
}

func renderTimelineEvent(e agent.RuntimeTimelineEvent) string {
	when := ""
	if !e.Time.IsZero() {
		when = formatBotStatusTime(e.Time)
	}
	parts := []string{}
	if when != "" {
		parts = append(parts, when)
	}
	if e.Type != "" {
		parts = append(parts, e.Type)
	}
	if e.Source != "" {
		parts = append(parts, "source="+e.Source)
	}
	if e.RunStatus != "" {
		parts = append(parts, "run="+e.RunStatus)
	}
	if e.WaitKind != "" {
		wait := e.WaitKind
		if e.WaitID != "" {
			wait += ":" + e.WaitID
		}
		parts = append(parts, "wait="+wait)
	}
	if e.Tool != "" {
		parts = append(parts, "tool="+e.Tool)
	}
	if e.Subject != "" {
		parts = append(parts, "subject="+truncateBotText(e.Subject, 60))
	}
	if e.Reason != "" {
		parts = append(parts, "reason="+truncateBotText(e.Reason, 60))
	}
	if e.Error != "" {
		parts = append(parts, "error="+truncateBotText(e.Error, 60))
	}
	return strings.Join(parts, "  ")
}

func renderRuntimeStatusLines(meta agent.RuntimeMeta) []string {
	var lines []string
	if meta.Wait.Kind != "" {
		wait := "等待: " + meta.Wait.Kind
		if id := firstNonEmpty(meta.Wait.ApprovalID, meta.Wait.AskID, meta.Wait.EventID); id != "" {
			wait += " " + id
		}
		if meta.Wait.Subject != "" {
			wait += " (" + truncateBotText(meta.Wait.Subject, 80) + ")"
		}
		lines = append(lines, wait)
		if meta.Wait.Reason != "" {
			lines = append(lines, "等待原因: "+truncateBotText(meta.Wait.Reason, 100))
		}
		if event := runtimeEventWaitSummary(meta.Wait); event != "" {
			lines = append(lines, "事件条件: "+event)
		}
		if !meta.Wait.Until.IsZero() {
			lines = append(lines, "等待到: "+formatBotStatusTime(meta.Wait.Until))
		}
		if len(meta.Wait.FilePaths) > 0 {
			lines = append(lines, "等待文件: "+truncateBotText(strings.Join(meta.Wait.FilePaths, ", "), 100))
		}
	}
	if meta.Run.LastWakeupReason != "" {
		lines = append(lines, "运行唤醒: "+truncateBotText(meta.Run.LastWakeupReason, 80))
	}
	if sched := runtimeScheduleSummary(meta.Scheduler); sched != "" {
		lines = append(lines, "调度: "+sched)
	}
	if !meta.Scheduler.LastWakeupAt.IsZero() {
		line := "上次唤醒: " + formatBotStatusTime(meta.Scheduler.LastWakeupAt)
		if meta.Scheduler.LastWakeupReason != "" {
			line += " (" + meta.Scheduler.LastWakeupReason + ")"
		}
		lines = append(lines, line)
	}
	if !meta.Scheduler.NextWakeupAt.IsZero() {
		lines = append(lines, "下次唤醒: "+formatBotStatusTime(meta.Scheduler.NextWakeupAt))
	}
	if meta.Budget.DailyWakeupLimit > 0 {
		lines = append(lines, fmt.Sprintf("唤醒预算: %d/%d", meta.Budget.DailyWakeups, meta.Budget.DailyWakeupLimit))
	}
	if meta.Budget.DailyModelCallLimit > 0 {
		lines = append(lines, fmt.Sprintf("模型调用预算: %d/%d", meta.Budget.DailyModelCalls, meta.Budget.DailyModelCallLimit))
	}
	if meta.Budget.DailyModelCostLimit > 0 {
		currency := meta.Budget.ModelCostCurrency
		if currency == "" {
			currency = "cost"
		}
		lines = append(lines, fmt.Sprintf("模型费用预算: %s %.4f/%.4f", currency, meta.Budget.DailyModelCost, meta.Budget.DailyModelCostLimit))
	}
	if meta.Budget.MaxGoalAutoTurns > 0 {
		lines = append(lines, fmt.Sprintf("自动续跑上限: %d 轮", meta.Budget.MaxGoalAutoTurns))
	}
	if meta.Budget.LastBlockedReason != "" {
		lines = append(lines, "预算阻塞: "+truncateBotText(meta.Budget.LastBlockedReason, 100))
	}
	return lines
}

func runtimeEventWaitSummary(wait agent.RuntimeWaitMeta) string {
	var parts []string
	if wait.EventSource != "" {
		parts = append(parts, "source="+wait.EventSource)
	}
	if wait.EventStatus != "" {
		parts = append(parts, "status="+wait.EventStatus)
	}
	if wait.EventConclusion != "" {
		parts = append(parts, "conclusion="+wait.EventConclusion)
	}
	return strings.Join(parts, " ")
}

func runtimeScheduleSummary(sched agent.RuntimeSchedMeta) string {
	var parts []string
	if sched.Enabled {
		parts = append(parts, "enabled")
	}
	if sched.DailyAt != "" {
		parts = append(parts, "daily="+sched.DailyAt)
	}
	if sched.Interval > 0 {
		parts = append(parts, "interval="+sched.Interval.String())
	}
	return strings.Join(parts, " ")
}

func formatBotStatusTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func askSubject(ask event.Ask) string {
	if len(ask.Questions) == 0 {
		return ""
	}
	if ask.Questions[0].Prompt != "" {
		return ask.Questions[0].Prompt
	}
	return ask.Questions[0].Header
}

func truncateBotText(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func (gw *BotGateway) sendText(ctx context.Context, adapter Adapter, msg InboundMessage, text string) error {
	result, err := adapter.Send(ctx, OutboundMessage{
		ConnectionID: msg.ConnectionID,
		Domain:       msg.Domain,
		ChatID:       msg.ChatID,
		ChatType:     msg.ChatType,
		Text:         text,
		ReplyToMsgID: msg.MessageID,
	})
	if err != nil {
		gw.logger.Warn("bot send failed", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "reply_to", hashID(msg.MessageID), "err", err)
		return err
	}
	gw.logger.Info("bot send completed", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "reply_to", hashID(msg.MessageID), "message", hashID(result.MessageID))
	return err
}

// handleGoalCommand dispatches /goal subcommands from the bot.
func (gw *BotGateway) handleGoalCommand(ctx context.Context, adapter Adapter, key string, msg InboundMessage) {
	args := strings.TrimSpace(strings.TrimPrefix(msg.Text, "/goal"))

	gw.mu.Lock()
	state, ok := gw.controllers[key]
	gw.mu.Unlock()

	switch strings.ToLower(args) {
	case "", "status":
		if !ok || state.ctrl == nil {
			_ = gw.sendText(ctx, adapter, msg, "目标：无（没有活跃会话）")
			return
		}
		goal := state.ctrl.Goal()
		goalStatus := state.ctrl.GoalStatus()
		if goal == "" {
			_ = gw.sendText(ctx, adapter, msg, fmt.Sprintf("目标：无\n状态：%s", goalStatus))
		} else {
			_ = gw.sendText(ctx, adapter, msg, fmt.Sprintf("目标：%s\n状态：%s", goal, goalStatus))
		}

	case "clear", "off", "stop", "done":
		if !ok || state.ctrl == nil {
			_ = gw.sendText(ctx, adapter, msg, "没有活跃会话。")
			return
		}
		state.ctrl.ClearGoal()
		_ = gw.sendText(ctx, adapter, msg, "目标已清除。")

	case "continue", "resume":
		if !ok || state.ctrl == nil {
			_ = gw.sendText(ctx, adapter, msg, "没有活跃会话。")
			return
		}
		if state.ctrl.Running() {
			_ = gw.sendText(ctx, adapter, msg, "当前正在运行，无法重复启动。")
			return
		}
		goal := state.ctrl.Goal()
		goalStatus := state.ctrl.GoalStatus()
		if goal == "" && (goalStatus == "" || goalStatus == control.GoalStatusStopped) {
			_ = gw.sendText(ctx, adapter, msg, "没有活跃目标可以继续。")
			return
		}
		if goalStatus == control.GoalStatusComplete {
			_ = gw.sendText(ctx, adapter, msg, "目标已完成，请设置新目标。")
			return
		}
		if !gw.sessions.TryStart(key) {
			_ = gw.sendText(ctx, adapter, msg, "当前正在运行，无法重复启动。")
			return
		}
		_ = gw.sendText(ctx, adapter, msg, "继续执行目标…")
		go func() {
			defer func() {
				next := gw.sessions.Release(key)
				if next != nil {
					gw.runTurn(ctx, adapter, key, *next)
				}
			}()

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
					gw.recordRuntimeWait(key, agent.RuntimeWaitMeta{
						Kind:       "approval",
						Reason:     "approval required",
						ApprovalID: approval.ID,
						Tool:       approval.Tool,
						Subject:    approval.Subject,
						Since:      time.Now().UTC(),
					})
				},
				func(ask event.Ask) {
					gw.mu.Lock()
					if state.pendingAsks == nil {
						state.pendingAsks = make(map[string][]event.AskQuestion)
					}
					state.pendingAsks[ask.ID] = ask.Questions
					state.lastAskID = ask.ID
					gw.mu.Unlock()
					gw.recordRuntimeWait(key, agent.RuntimeWaitMeta{
						Kind:    "ask",
						Reason:  "user answer required",
						AskID:   ask.ID,
						Subject: askSubject(ask),
						Since:   time.Now().UTC(),
					})
				},
			)
			state.sink.setTarget(sink)
			defer state.sink.setTarget(nil)

			turnCtx, cancel := context.WithCancel(ctx)
			gw.mu.Lock()
			if s, ok2 := gw.controllers[key]; ok2 {
				s.cancel = cancel
				s.lastActive = time.Now()
			}
			gw.mu.Unlock()
			defer cancel()
			sink.ctrl = state.ctrl
			err := state.ctrl.ContinueGoal(turnCtx, "bot")
			sink.Emit(event.Event{Kind: event.TurnDone, Err: err})
			if err != nil {
				gw.logger.Warn("bot goal continue failed", "err", err)
			}
		}()

	default:
		// /goal <text> — set a new goal
		if !ok || state.ctrl == nil {
			_ = gw.sendText(ctx, adapter, msg, "没有活跃会话，请先发送一条消息创建会话。")
			return
		}
		state.ctrl.SetPlanMode(false)
		state.ctrl.SetGoal(args)
		_ = gw.sendText(ctx, adapter, msg, fmt.Sprintf("目标已设置 → %s", args))
	}
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

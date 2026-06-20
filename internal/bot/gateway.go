package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	Model            string
	ToolApprovalMode string
	MaxSteps         int
	// ApprovalTimeout bounds how long a tool-approval/ask prompt blocks a bot
	// session waiting for a remote user's reply. Zero falls back to
	// defaultBotApprovalTimeout so an abandoned prompt can't wedge the bot forever
	// (#4626, #4402). A negative value disables the timeout (wait indefinitely).
	ApprovalTimeout    time.Duration
	WorkspaceRoot      string
	Channels           map[Platform]ChannelConfig
	ConnectionChannels map[string]ChannelConfig
	Allowlist          AllowlistConfig
	Enabled            map[Platform]bool
	Debounce           time.Duration
	OnInbound          func(InboundMessage)
	// OnSessionReady notifies the host after the bot has created or reused the
	// controller for an inbound remote. Hosts may persist the concrete session ID
	// or keep the remote as a read-only channel.
	OnSessionReady func(InboundMessage, string) error
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

// BotGateway 是 reasonix bot 消息网关，管理 Controller 生命周期、session 并发、
// 事件渲染和平台适配器。
type BotGateway struct {
	cfg      GatewayConfig
	adapters []AdapterBinding
	sessions *SessionManager
	startErr []error

	mu             sync.Mutex
	controllers    map[string]*sessionState // session key -> active state
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
		allowlist:      make(map[Platform]map[string]bool),
		groupAllowlist: make(map[Platform]map[string]bool),
		logger:         logger.With("component", "bot_gateway"),
	}
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
			}
			gw.rememberSessionReady(msg, state.ctrl)
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
		gw.mu.Unlock()
		mode := gw.currentToolApprovalMode(key, msg)
		_ = gw.sendText(ctx, adapter, msg, fmt.Sprintf("活跃任务数: %d\n保留会话数: %d\n工具审批模式: %s", active, sessions, toolApprovalModeLabel(mode)))

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
			"/status - 查看状态\n" +
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
	gw.rememberSessionReady(msg, state.ctrl)

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

	// 创建新 Controller
	sessionSink := &sessionEventSink{}
	model, workspaceRoot, toolApprovalMode := gw.sessionOptionsForMessage(msg)
	gw.logger.Info("bot session creating", "platform", msg.Platform, "chat_type", msg.ChatType, "chat", hashID(msg.ChatID), "session", key[:8], "model", model, "workspace_set", strings.TrimSpace(workspaceRoot) != "", "tool_approval_mode", normalizeBotToolApprovalMode(toolApprovalMode))
	ctrl, err := boot.Build(ctx, boot.Options{
		Model:           model,
		MaxSteps:        gw.cfg.MaxSteps,
		RequireKey:      true,
		Sink:            sessionSink,
		WorkspaceRoot:   workspaceRoot,
		SessionDir:      botSessionDir(workspaceRoot),
		ApprovalTimeout: gw.approvalTimeout(),
	})
	if err != nil {
		gw.logger.Error("build controller failed", "err", err)
		return nil
	}
	ctrl.EnableInteractiveApproval()
	ctrl.SetToolApprovalMode(toolApprovalMode)
	ensureControllerSessionPath(ctrl)

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

func ensureControllerSessionPath(ctrl *control.Controller) {
	if ctrl == nil || ctrl.SessionPath() != "" || ctrl.SessionDir() == "" {
		return
	}
	ctrl.SetSessionPath(agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
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

func (gw *BotGateway) rememberSessionReady(msg InboundMessage, ctrl *control.Controller) {
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

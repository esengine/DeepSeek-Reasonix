package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// GatewayConfig 是 BotGateway 的配置。
type GatewayConfig struct {
	Model         string
	MaxSteps      int
	WorkspaceRoot string
	Channels      map[Platform]ChannelConfig
	Allowlist     AllowlistConfig
	Enabled       map[Platform]bool
	Debounce      time.Duration
}

// ChannelConfig overrides gateway defaults for one IM channel.
type ChannelConfig struct {
	Model         string
	WorkspaceRoot string
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
	adapters map[Platform]Adapter
	sessions *SessionManager

	mu             sync.Mutex
	controllers    map[string]*sessionState // session key -> active state
	allowlist      map[Platform]map[string]bool
	groupAllowlist map[Platform]map[string]bool

	logger *slog.Logger
}

type sessionState struct {
	ctrl        *control.Controller
	sink        *sessionEventSink
	cancel      context.CancelFunc
	pendingAsks map[string][]event.AskQuestion
	createdAt   time.Time
	lastActive  time.Time
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
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 1500 * time.Millisecond
	}
	gw := &BotGateway{
		cfg:            cfg,
		adapters:       adapters,
		sessions:       NewSessionManager(cfg.Debounce),
		controllers:    make(map[string]*sessionState),
		allowlist:      make(map[Platform]map[string]bool),
		groupAllowlist: make(map[Platform]map[string]bool),
		logger:         logger.With("component", "bot_gateway"),
	}
	gw.buildAllowlist()
	return gw
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
	for plat, adapter := range gw.adapters {
		if !gw.cfg.Enabled[plat] {
			gw.logger.Info("platform disabled, skipping", "platform", plat)
			continue
		}
		gw.logger.Info("starting adapter", "platform", plat)
		if err := adapter.Start(ctx); err != nil {
			return fmt.Errorf("start adapter %s: %w", plat, err)
		}
	}

	// 合并所有适配器的消息通道
	for plat, adapter := range gw.adapters {
		if !gw.cfg.Enabled[plat] {
			continue
		}
		go gw.dispatchLoop(ctx, plat, adapter)
	}

	return nil
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

	for _, adapter := range gw.adapters {
		if err := adapter.Stop(); err != nil {
			gw.logger.Warn("error stopping adapter", "err", err)
		}
	}
}

func (gw *BotGateway) dispatchLoop(ctx context.Context, plat Platform, adapter Adapter) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-adapter.Messages():
			if !ok {
				return
			}
			gw.handleMessage(ctx, plat, adapter, msg)
		}
	}
}

func (gw *BotGateway) handleMessage(ctx context.Context, plat Platform, adapter Adapter, msg InboundMessage) {
	msg.Platform = plat

	// allowlist 检查
	if !gw.checkAllowlist(plat, msg) {
		gw.logger.Info("user not in allowlist", "platform", plat, "user", hashID(msg.UserID))
		_ = gw.sendText(ctx, adapter, msg, "抱歉，您没有使用此 bot 的权限。")
		return
	}

	src := msg.Session()
	key := BuildSessionKey(src)

	// 斜杠命令处理
	if IsSlashBypass(msg.Text) {
		gw.handleSlashCommand(ctx, adapter, key, msg)
		return
	}

	gw.addPendingReaction(ctx, plat, adapter, msg)

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

	gw.runTurn(ctx, adapter, key, msg)
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
	if !gw.allowlist[plat][msg.UserID] {
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
		if ok {
			state.ctrl.Approve(parts[1], true, false, false)
			_ = gw.sendText(ctx, adapter, msg, "已批准。")
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
		if ok {
			state.ctrl.Approve(parts[1], false, false, false)
			_ = gw.sendText(ctx, adapter, msg, "已拒绝。")
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
		}
		gw.mu.Unlock()
		if !ok || state.ctrl == nil {
			_ = gw.sendText(ctx, adapter, msg, "没有找到当前会话。")
			return
		}
		answers := parseAskAnswers(questions, rawAnswer)
		state.ctrl.AnswerQuestion(askID, answers)
		_ = gw.sendText(ctx, adapter, msg, "已提交回答。")

	case strings.HasPrefix(msg.Text, "/status"):
		active := gw.sessions.ActiveCount()
		gw.mu.Lock()
		sessions := len(gw.controllers)
		gw.mu.Unlock()
		_ = gw.sendText(ctx, adapter, msg, fmt.Sprintf("活跃任务数: %d\n保留会话数: %d", active, sessions))

	case strings.HasPrefix(msg.Text, "/help"):
		help := "可用命令:\n" +
			"/stop - 停止当前任务\n" +
			"/new - 开始新会话\n" +
			"/reset - 重置会话\n" +
			"/approve <id> - 批准操作\n" +
			"/deny <id> - 拒绝操作\n" +
			"/answer <id> <选项> - 回答 ask 问题\n" +
			"/status - 查看状态\n" +
			"/help - 显示帮助"
		_ = gw.sendText(ctx, adapter, msg, help)
	}
}

func (gw *BotGateway) runTurn(ctx context.Context, adapter Adapter, key string, msg InboundMessage) {
	defer func() {
		// 检查是否有等待队列中的消息
		next := gw.sessions.Release(key)
		if next != nil {
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
	sink := newRenderSink(ctx, adapter, msg.ChatID, msg.ChatType, msg.MessageID, gw.logger, func(ask event.Ask) {
		gw.mu.Lock()
		if state.pendingAsks == nil {
			state.pendingAsks = make(map[string][]event.AskQuestion)
		}
		state.pendingAsks[ask.ID] = ask.Questions
		gw.mu.Unlock()
	})
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
	}
}

func (gw *BotGateway) getOrCreateSession(ctx context.Context, key string, msg InboundMessage) *sessionState {
	gw.mu.Lock()
	if state, ok := gw.controllers[key]; ok {
		state.lastActive = time.Now()
		gw.mu.Unlock()
		return state
	}
	gw.mu.Unlock()

	// 创建新 Controller
	sessionSink := &sessionEventSink{}
	model, workspaceRoot := gw.sessionOptionsForPlatform(msg.Platform)
	ctrl, err := boot.Build(ctx, boot.Options{
		Model:         model,
		MaxSteps:      gw.cfg.MaxSteps,
		RequireKey:    true,
		Sink:          sessionSink,
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		gw.logger.Error("build controller failed", "err", err)
		return nil
	}
	ctrl.EnableInteractiveApproval()

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

	return state
}

func (gw *BotGateway) sessionOptionsForPlatform(plat Platform) (model string, workspaceRoot string) {
	model = gw.cfg.Model
	workspaceRoot = gw.cfg.WorkspaceRoot
	if gw.cfg.Channels == nil {
		return model, workspaceRoot
	}
	channel, ok := gw.cfg.Channels[plat]
	if !ok {
		return model, workspaceRoot
	}
	if value := strings.TrimSpace(channel.Model); value != "" {
		model = value
	}
	if value := strings.TrimSpace(channel.WorkspaceRoot); value != "" {
		workspaceRoot = value
	}
	return model, workspaceRoot
}

func (gw *BotGateway) sendText(ctx context.Context, adapter Adapter, msg InboundMessage, text string) error {
	_, err := adapter.Send(ctx, OutboundMessage{
		ChatID:       msg.ChatID,
		ChatType:     msg.ChatType,
		Text:         text,
		ReplyToMsgID: msg.MessageID,
	})
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

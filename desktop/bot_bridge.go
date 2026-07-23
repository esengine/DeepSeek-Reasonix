package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/event"
)

// errDriveBusy signals that a takeover drive could not start because the target
// controller was already running a turn. The hub translates it into a
// user-facing "session busy" message rather than a generic drive failure.
var errDriveBusy = errors.New("desktop session busy")

// botBridgeHub 是 bot 网关对桌面端的"上帝视角"桥（bot.DesktopBridge 的实现）。
//
// 职责边界（刻意保持窄）：
//   - 观察：tabEventSink.Emit 把每个桌面会话的事件旁路到 observe；hub 记录
//     待审批/待回答项，并把审批请求、任务完成/出错推送给订阅聊天。
//   - 遥控审批：/desktop approve|deny|answer 经 App.ApproveTab /
//     AnswerQuestionForTab 按 tab 寻址回写。controller 侧幂等（先到者赢），
//     桌面 UI 与远端并发应答互不干扰。
//   - 不做：向桌面会话注入输入、抢占写 lease。将来的显式接管在
//     bot.DesktopBridge 上扩展，底层走 internal/control 的接管端口。
//
// observe 跑在 controller 的事件 goroutine 上，绝不能做网络调用——通知统一
// 进有界队列由 worker 异步发送，队列满时丢弃并告警。
type botBridgeHub struct {
	sessions   func() []bot.DesktopSessionInfo
	approveTab func(tabID, id string, allow, session, persist bool)
	answerTab  func(tabID, id string, answers []QuestionAnswer)
	notify     func(ctx context.Context, connectionID, domain string, msg bot.OutboundMessage) (bot.SendResult, error)
	// drive 把一条远程文本提交为 tab 的新 turn，并把该 turn 的输出转发回 route。
	drive func(tabID, text string, route bot.DesktopWatchRoute) error
	// announce 往会话 transcript 里发一条 Notice，让桌面用户看到接管状态变化。
	announce func(tabID, text string)
	// persistWatchers 把订阅全集回写用户配置（bot.desktop_watchers）。
	persistWatchers func(routes []bot.DesktopWatchRoute) error
	// takeoverChanged 通知桌面前端刷新（TabMeta.RemoteControlled 变化）。
	takeoverChanged func()
	logger          *slog.Logger

	mu       sync.Mutex
	watchers map[string]bot.DesktopWatchRoute
	pending  map[string]desktopPendingPrompt
	// takeovers: tabID -> 驾驶该会话的聊天路由；takeoverTabs: routeKey -> tabID。
	takeovers    map[string]bot.DesktopWatchRoute
	takeoverTabs map[string]string
	// watchSeq 单调递增，标记订阅快照的新旧；persist 时用它丢弃过期写入。
	watchSeq uint64
	// watchPersistDirty keeps a failed local mutation authoritative in memory;
	// a later runtime refresh must not silently restore the older disk snapshot.
	watchPersistDirty bool

	// persistMu 串行化订阅落盘，并保证只写最新快照（见 SetWatch）。
	persistMu      sync.Mutex
	lastPersistSeq uint64

	queue chan desktopBridgeNotification
}

type desktopPendingPrompt struct {
	tabID     string
	kind      string // "approval" | "ask"
	tool      string
	subject   string
	questions []event.AskQuestion
}

// desktopBridgeNotification 是一条待推送的桌面事件。text 与 card 都按订阅路由
// 现做，因此群聊(多用户)与私聊可给出不同详略——命令行/错误详情只发私聊，群里
// 只给摘要。route 非 nil 时定向发给该聊天(不看 watch 订阅)，用于接管收回等必达通知。
type desktopBridgeNotification struct {
	text  func(route bot.DesktopWatchRoute) string
	card  func(route bot.DesktopWatchRoute) *bot.InteractiveCard
	route *bot.DesktopWatchRoute
}

// isSharedChat 判断一个聊天是否为多用户场景(群/话题群/服务器频道)。私聊/单聊
// 只有操作者本人,可安全展示命令行等敏感详情。
func isSharedChat(ct bot.ChatType) bool {
	return ct != bot.ChatDM && ct != bot.ChatDirect
}

func constText(s string) func(bot.DesktopWatchRoute) string {
	return func(bot.DesktopWatchRoute) string { return s }
}

const (
	botBridgeQueueSize     = 64
	botBridgeSendTimeout   = 15 * time.Second
	botBridgeSubjectLimit  = 200
	botBridgePendingLimit  = 200
	botBridgeErrTextLimit  = 300
	botBridgePromptPreview = 500
)

// botBridgeDeps 打包 hub 对宿主(App)的全部依赖，便于测试注入。
type botBridgeDeps struct {
	sessions        func() []bot.DesktopSessionInfo
	approveTab      func(tabID, id string, allow, session, persist bool)
	answerTab       func(tabID, id string, answers []QuestionAnswer)
	notify          func(ctx context.Context, connectionID, domain string, msg bot.OutboundMessage) (bot.SendResult, error)
	drive           func(tabID, text string, route bot.DesktopWatchRoute) error
	announce        func(tabID, text string)
	persistWatchers func(routes []bot.DesktopWatchRoute) error
	takeoverChanged func()
	logger          *slog.Logger
}

func newBotBridgeHub(deps botBridgeDeps) *botBridgeHub {
	logger := deps.logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &botBridgeHub{
		sessions:        deps.sessions,
		approveTab:      deps.approveTab,
		answerTab:       deps.answerTab,
		notify:          deps.notify,
		drive:           deps.drive,
		announce:        deps.announce,
		persistWatchers: deps.persistWatchers,
		takeoverChanged: deps.takeoverChanged,
		logger:          logger.With("component", "bot_bridge"),
		watchers:        make(map[string]bot.DesktopWatchRoute),
		pending:         make(map[string]desktopPendingPrompt),
		takeovers:       make(map[string]bot.DesktopWatchRoute),
		takeoverTabs:    make(map[string]string),
		queue:           make(chan desktopBridgeNotification, botBridgeQueueSize),
	}
	go h.run()
	return h
}

// observe 接收某个桌面会话的一条事件。在 controller 事件 goroutine 上运行，
// 只做内存记账和入队，不做任何阻塞调用。
func (h *botBridgeHub) observe(tabID string, e event.Event) {
	switch e.Kind {
	case event.ApprovalRequest:
		h.mu.Lock()
		h.rememberPendingLocked(e.Approval.ID, desktopPendingPrompt{
			tabID:   tabID,
			kind:    "approval",
			tool:    e.Approval.Tool,
			subject: truncateForBridge(e.Approval.Subject, botBridgeSubjectLimit),
		})
		watching := len(h.watchers) > 0
		h.mu.Unlock()
		if watching {
			h.enqueue(h.approvalNotification(tabID, e.Approval))
		}
	case event.AskRequest:
		h.mu.Lock()
		h.rememberPendingLocked(e.Ask.ID, desktopPendingPrompt{
			tabID:     tabID,
			kind:      "ask",
			questions: e.Ask.Questions,
		})
		watching := len(h.watchers) > 0
		h.mu.Unlock()
		if watching {
			h.enqueue(h.askNotification(tabID, e.Ask))
		}
	case event.TurnDone:
		h.mu.Lock()
		for id, p := range h.pending {
			if p.tabID == tabID {
				delete(h.pending, id)
			}
		}
		watching := len(h.watchers) > 0
		h.mu.Unlock()
		if !watching {
			return
		}
		if e.Err != nil && strings.Contains(e.Err.Error(), "context canceled") {
			// 桌面端主动停止的任务不推送，避免正常操作变成噪音。
			return
		}
		h.enqueue(h.turnDoneNotification(tabID, e.Err))
	}
}

// rememberPendingLocked 记录待处理项；容量兜底防泄漏（正常路径 TurnDone 会清理）。
func (h *botBridgeHub) rememberPendingLocked(id string, p desktopPendingPrompt) {
	if strings.TrimSpace(id) == "" {
		return
	}
	if len(h.pending) >= botBridgePendingLimit {
		h.pending = make(map[string]desktopPendingPrompt)
	}
	h.pending[id] = p
}

func (h *botBridgeHub) enqueue(n desktopBridgeNotification) {
	select {
	case h.queue <- n:
	default:
		h.logger.Warn("desktop bridge notification queue full; dropping")
	}
}

func (h *botBridgeHub) run() {
	for n := range h.queue {
		h.deliver(n)
	}
}

func (h *botBridgeHub) deliver(n desktopBridgeNotification) {
	h.mu.Lock()
	var routes []bot.DesktopWatchRoute
	if n.route != nil {
		routes = []bot.DesktopWatchRoute{*n.route}
	} else {
		routes = h.watcherRoutesLocked()
	}
	notify := h.notify
	h.mu.Unlock()
	if notify == nil || len(routes) == 0 {
		return
	}
	// Fan out per route: a single slow/hung connection must not hold the queue
	// worker for its full timeout and back-pressure everyone else's approvals.
	var wg sync.WaitGroup
	for _, route := range routes {
		wg.Add(1)
		go func(route bot.DesktopWatchRoute) {
			defer wg.Done()
			msg := bot.OutboundMessage{
				ChatID:   route.ChatID,
				ChatType: route.ChatType,
			}
			if n.text != nil {
				msg.Text = n.text(route)
			}
			if n.card != nil {
				msg.Card = n.card(route)
			}
			ctx, cancel := context.WithTimeout(context.Background(), botBridgeSendTimeout)
			defer cancel()
			if _, err := notify(ctx, route.ConnectionID, route.Domain, msg); err != nil {
				h.logger.Warn("desktop bridge notification send failed", "platform", route.Platform, "err", err)
			}
		}(route)
	}
	wg.Wait()
}

// tabLabel 把 tabID 解析成人类可读的会话名。
func (h *botBridgeHub) tabLabel(tabID string) string {
	if s, ok := h.sessionByTabID(tabID); ok {
		if label := strings.TrimSpace(s.Label); label != "" {
			return label
		}
		if title := strings.TrimSpace(s.Topic); title != "" {
			return title
		}
	}
	return "(未命名会话)"
}

func (h *botBridgeHub) sessionByTabID(tabID string) (bot.DesktopSessionInfo, bool) {
	if h.sessions == nil {
		return bot.DesktopSessionInfo{}, false
	}
	for _, s := range h.sessions() {
		if s.TabID == tabID {
			return s, true
		}
	}
	return bot.DesktopSessionInfo{}, false
}

func (h *botBridgeHub) approvalNotification(tabID string, approval event.Approval) desktopBridgeNotification {
	label := h.tabLabel(tabID)
	// The approval subject is the pending command line; only reveal it in a
	// private chat. In a shared chat show the tool name and point the operator
	// to the desktop / a DM instead of leaking the command to the whole group.
	subjectFor := func(route bot.DesktopWatchRoute) string {
		if isSharedChat(route.ChatType) {
			return "(命令详情仅在桌面端或私聊显示)"
		}
		return truncateForBridge(approval.Subject, botBridgeSubjectLimit)
	}
	return desktopBridgeNotification{
		text: func(route bot.DesktopWatchRoute) string {
			return fmt.Sprintf("⚠️ 桌面会话「%s」需要批准操作\n工具: %s\n操作: %s\n\nID: `%s`\n用 /desktop approve %s 批准，/desktop deny %s 拒绝。桌面端先处理则以先到者为准。",
				label, approval.Tool, subjectFor(route), approval.ID, approval.ID, approval.ID)
		},
		card: func(route bot.DesktopWatchRoute) *bot.InteractiveCard {
			return &bot.InteractiveCard{
				Header: "桌面会话需要批准",
				Elements: []bot.InteractiveCardElement{
					{Tag: "markdown", Content: fmt.Sprintf("**会话**: %s\n\n**工具**: %s\n\n**操作**: %s\n\nID: `%s`", label, approval.Tool, subjectFor(route), approval.ID)},
					{Tag: "action", Extra: map[string]any{
						"actions": []map[string]any{
							desktopCardButton("允许一次", "primary", "/desktop approve "+approval.ID, route),
							desktopCardButton("拒绝", "danger", "/desktop deny "+approval.ID, route),
						},
					}},
				},
			}
		},
	}
}

func (h *botBridgeHub) askNotification(tabID string, ask event.Ask) desktopBridgeNotification {
	label := h.tabLabel(tabID)
	var b strings.Builder
	fmt.Fprintf(&b, "❓ 桌面会话「%s」在等待回答:\n", label)
	for i, q := range ask.Questions {
		fmt.Fprintf(&b, "\n**%d. %s**\n", i+1, truncateForBridge(q.Prompt, botBridgePromptPreview))
		for j, opt := range q.Options {
			fmt.Fprintf(&b, "  %d. %s\n", j+1, opt.Label)
		}
	}
	fmt.Fprintf(&b, "\nID: `%s`\n用 /desktop answer %s <选项编号或文本> 回答；桌面端先处理则以先到者为准。", ask.ID, ask.ID)
	privateText := b.String()
	sharedText := fmt.Sprintf("❓ 桌面会话「%s」正在等待回答（问题详情仅在桌面端或私聊显示）。\n\nID: `%s`", label, ask.ID)
	textFor := func(route bot.DesktopWatchRoute) string {
		if isSharedChat(route.ChatType) {
			return sharedText
		}
		return privateText
	}

	var card func(route bot.DesktopWatchRoute) *bot.InteractiveCard
	if len(ask.Questions) == 1 && len(ask.Questions[0].Options) > 0 {
		options := ask.Questions[0].Options
		card = func(route bot.DesktopWatchRoute) *bot.InteractiveCard {
			if isSharedChat(route.ChatType) {
				return nil
			}
			actions := make([]map[string]any, 0, len(options))
			for i, opt := range options {
				optLabel := strings.TrimSpace(opt.Label)
				if optLabel == "" {
					optLabel = fmt.Sprintf("选项 %d", i+1)
				}
				actions = append(actions, desktopCardButton(optLabel, "primary", fmt.Sprintf("/desktop answer %s %d", ask.ID, i+1), route))
			}
			return &bot.InteractiveCard{
				Header: "桌面会话在等待回答",
				Elements: []bot.InteractiveCardElement{
					{Tag: "markdown", Content: privateText},
					{Tag: "action", Extra: map[string]any{"actions": actions}},
				},
			}
		}
	}
	return desktopBridgeNotification{text: textFor, card: card}
}

func (h *botBridgeHub) turnDoneNotification(tabID string, err error) desktopBridgeNotification {
	label := h.tabLabel(tabID)
	if err != nil {
		// Error text can contain paths/tokens; only detail it in a private chat.
		return desktopBridgeNotification{text: func(route bot.DesktopWatchRoute) string {
			if isSharedChat(route.ChatType) {
				return fmt.Sprintf("❌ 桌面会话「%s」任务出错（详情见桌面端或私聊）。", label)
			}
			return fmt.Sprintf("❌ 桌面会话「%s」任务出错: %s", label, truncateForBridge(err.Error(), botBridgeErrTextLimit))
		}}
	}
	return desktopBridgeNotification{text: constText(fmt.Sprintf("✅ 桌面会话「%s」任务完成。", label))}
}

func desktopCardButton(label, style, command string, route bot.DesktopWatchRoute) map[string]any {
	return map[string]any{
		"tag":  "button",
		"text": map[string]string{"tag": "plain_text", "content": label},
		"type": style,
		"value": map[string]string{
			"command":   command,
			"chat_type": string(route.ChatType),
		},
	}
}

func truncateForBridge(s string, limit int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// ---- bot.DesktopBridge 实现 ----

func (h *botBridgeHub) Sessions() []bot.DesktopSessionInfo {
	if h.sessions == nil {
		return nil
	}
	sessions := h.sessions()
	h.mu.Lock()
	byTab := make(map[string][]bot.DesktopPendingInfo, len(h.pending))
	for id, p := range h.pending {
		byTab[p.tabID] = append(byTab[p.tabID], bot.DesktopPendingInfo{ID: id, Kind: p.kind, Tool: p.tool})
	}
	h.mu.Unlock()
	for i := range sessions {
		if pend := byTab[sessions[i].TabID]; len(pend) > 0 {
			sort.Slice(pend, func(a, b int) bool { return pend[a].ID < pend[b].ID })
			sessions[i].Pending = pend
		}
	}
	return sessions
}

func (h *botBridgeHub) SetWatch(route bot.DesktopWatchRoute, enable bool) error {
	h.mu.Lock()
	if enable {
		h.watchers[route.Key()] = route
	} else {
		delete(h.watchers, route.Key())
	}
	h.watchSeq++
	h.watchPersistDirty = true
	seq := h.watchSeq
	routes := h.watcherRoutesLocked()
	persist := h.persistWatchers
	h.mu.Unlock()
	if persist == nil {
		return nil
	}
	// Serialize persists and drop stale ones: two concurrent SetWatch calls
	// (different connections) compute snapshots under h.mu but write config
	// outside it, so their writes could otherwise reorder and let an older
	// snapshot clobber a newer one, silently losing a subscription.
	h.persistMu.Lock()
	defer h.persistMu.Unlock()
	if seq <= h.lastPersistSeq {
		return nil
	}
	if err := persist(routes); err != nil {
		return err
	}
	h.lastPersistSeq = seq
	h.mu.Lock()
	if h.watchSeq == seq {
		h.watchPersistDirty = false
	}
	h.mu.Unlock()
	return nil
}

func (h *botBridgeHub) watcherVersion() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.watchSeq
}

// seedWatchers applies a config snapshot only if no watch command changed the
// runtime after the config read began. Fresh external config edits still apply;
// stale refreshes and failed local persists do not erase newer runtime state.
func (h *botBridgeHub) seedWatchers(routes []bot.DesktopWatchRoute, expectedSeq uint64) {
	h.persistMu.Lock()
	defer h.persistMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.watchSeq != expectedSeq || h.watchPersistDirty {
		return
	}
	h.watchers = make(map[string]bot.DesktopWatchRoute, len(routes))
	for _, r := range routes {
		if strings.TrimSpace(r.ChatID) == "" {
			continue
		}
		h.watchers[r.Key()] = r
	}
}

func (h *botBridgeHub) watcherRoutesLocked() []bot.DesktopWatchRoute {
	routes := make([]bot.DesktopWatchRoute, 0, len(h.watchers))
	for _, r := range h.watchers {
		routes = append(routes, r)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Key() < routes[j].Key() })
	return routes
}

func (h *botBridgeHub) Watching(route bot.DesktopWatchRoute) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.watchers[route.Key()]
	return ok
}

func (h *botBridgeHub) Approve(approvalID string, allow bool) (string, error) {
	approvalID = strings.TrimSpace(approvalID)
	h.mu.Lock()
	p, ok := h.pending[approvalID]
	if ok && p.kind == "approval" {
		delete(h.pending, approvalID)
	}
	h.mu.Unlock()
	if !ok || p.kind != "approval" {
		return "", fmt.Errorf("未找到待处理的审批 %s（可能已在桌面端处理或已超时）。用 /desktop status 查看当前会话。", approvalID)
	}
	if h.approveTab == nil {
		return "", fmt.Errorf("桌面端审批通道不可用。")
	}
	h.approveTab(p.tabID, approvalID, allow, false, false)
	action := "批准"
	if !allow {
		action = "拒绝"
	}
	return fmt.Sprintf("已提交%s「%s」的操作（%s）。桌面端若已先处理，以先到者为准。", action, h.tabLabel(p.tabID), p.tool), nil
}

func (h *botBridgeHub) AskQuestions(askID string) ([]event.AskQuestion, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[strings.TrimSpace(askID)]
	if !ok || p.kind != "ask" {
		return nil, false
	}
	return p.questions, true
}

func (h *botBridgeHub) Answer(askID string, answers []event.AskAnswer) (string, error) {
	askID = strings.TrimSpace(askID)
	h.mu.Lock()
	p, ok := h.pending[askID]
	if ok && p.kind == "ask" {
		delete(h.pending, askID)
	}
	h.mu.Unlock()
	if !ok || p.kind != "ask" {
		return "", fmt.Errorf("未找到待回答的提问 %s（可能已在桌面端回答或已超时）。", askID)
	}
	if h.answerTab == nil {
		return "", fmt.Errorf("桌面端问答通道不可用。")
	}
	out := make([]QuestionAnswer, 0, len(answers))
	for _, an := range answers {
		out = append(out, QuestionAnswer{QuestionID: an.QuestionID, Selected: an.Selected})
	}
	h.answerTab(p.tabID, askID, out)
	return fmt.Sprintf("已提交「%s」的回答。桌面端若已先处理，以先到者为准。", h.tabLabel(p.tabID)), nil
}

// ---- 显式接管 ----

func (h *botBridgeHub) Takeover(route bot.DesktopWatchRoute, tabID string) (string, error) {
	tabID = strings.TrimSpace(tabID)
	// DM only. In a group the binding is keyed on the group chat, so after an
	// admin takes over, ANY allowlisted member's plain message would be diverted
	// to drive the session — a privilege escalation past the admin gate that
	// establishes the takeover. Restricting to DM keeps the driver identical to
	// the operator who established it.
	if route.ChatType != bot.ChatDM {
		return "", fmt.Errorf("接管仅支持私聊：在群里接管会让其他成员也能驱动你的桌面会话。请在与 bot 的私聊中接管。")
	}
	session, ok := h.sessionByTabID(tabID)
	if !ok {
		return "", fmt.Errorf("未找到会话 %s。用 /desktop status 查看可接管的会话。", tabID)
	}
	if session.Detached {
		return "", fmt.Errorf("会话「%s」在后台运行，暂不支持接管；请先在桌面端打开它。", h.tabLabel(tabID))
	}
	h.mu.Lock()
	if holder, held := h.takeovers[tabID]; held && holder.Key() != route.Key() {
		h.mu.Unlock()
		return "", fmt.Errorf("会话「%s」已被另一个聊天接管。", h.tabLabel(tabID))
	}
	// 同一聊天换目标：先解除旧绑定，并记下旧 tab 以便公告解除。
	released := ""
	if prev, ok := h.takeoverTabs[route.Key()]; ok && prev != tabID {
		delete(h.takeovers, prev)
		released = prev
	}
	h.takeovers[tabID] = route
	h.takeoverTabs[route.Key()] = tabID
	announce := h.announce
	changed := h.takeoverChanged
	h.mu.Unlock()
	if announce != nil {
		if released != "" {
			announce(released, "IM 远程接管已解除（接管方切换到了另一个会话）。")
		}
		announce(tabID, "此会话已被 IM 远程接管（bot 管理员）。在此本地发送任意消息即可收回控制。")
	}
	if changed != nil {
		changed()
	}
	label := h.tabLabel(tabID)
	return fmt.Sprintf("已接管「%s」。现在直接发消息即可驱动它，输出会流回本聊天；/desktop release 解除接管。桌面端本地发言会自动收回控制。", label), nil
}

func (h *botBridgeHub) Release(route bot.DesktopWatchRoute) (string, error) {
	h.mu.Lock()
	tabID, ok := h.takeoverTabs[route.Key()]
	if ok {
		delete(h.takeoverTabs, route.Key())
		delete(h.takeovers, tabID)
	}
	announce := h.announce
	changed := h.takeoverChanged
	h.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("本聊天当前没有接管任何桌面会话。")
	}
	if announce != nil {
		announce(tabID, "IM 远程接管已解除。")
	}
	if changed != nil {
		changed()
	}
	return fmt.Sprintf("已解除对「%s」的接管。", h.tabLabel(tabID)), nil
}

func (h *botBridgeHub) TakeoverTab(route bot.DesktopWatchRoute) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.takeoverTabs[route.Key()]
}

func (h *botBridgeHub) DriveInput(route bot.DesktopWatchRoute, text string) (string, error) {
	h.mu.Lock()
	tabID := h.takeoverTabs[route.Key()]
	h.mu.Unlock()
	if tabID == "" {
		return "", fmt.Errorf("本聊天没有接管任何桌面会话。")
	}
	session, ok := h.sessionByTabID(tabID)
	if !ok || session.Detached {
		// 会话被关闭或转入后台：自动解除绑定，避免消息黑洞。
		h.mu.Lock()
		delete(h.takeoverTabs, route.Key())
		delete(h.takeovers, tabID)
		h.mu.Unlock()
		if changed := h.takeoverChanged; changed != nil {
			changed()
		}
		return "", fmt.Errorf("被接管的会话已关闭或转入后台，接管已自动解除。")
	}
	if session.Running {
		return "", h.busyError(tabID)
	}
	if h.drive == nil {
		return "", fmt.Errorf("桌面端驱动通道不可用。")
	}
	if err := h.drive(tabID, text, route); err != nil {
		if errors.Is(err, errDriveBusy) {
			return "", h.busyError(tabID)
		}
		return "", fmt.Errorf("驱动失败: %v", err)
	}
	return "", nil
}

func (h *botBridgeHub) busyError(tabID string) error {
	return fmt.Errorf("会话「%s」正在执行中，等它完成后再发；或用 /desktop watch on 订阅完成通知。", h.tabLabel(tabID))
}

// reclaimFromDesktop 在桌面用户本地提交输入时收回控制权：解除绑定并通知
// 远端聊天。由 App.SubmitToTab 调用（bridge 自己的驱动不走这条路）。
func (h *botBridgeHub) reclaimFromDesktop(tabID string) {
	h.mu.Lock()
	route, ok := h.takeovers[tabID]
	if ok {
		delete(h.takeovers, tabID)
		delete(h.takeoverTabs, route.Key())
	}
	notify := h.notify
	changed := h.takeoverChanged
	h.mu.Unlock()
	if !ok {
		return
	}
	if changed != nil {
		changed()
	}
	if notify == nil {
		return
	}
	label := h.tabLabel(tabID)
	// 直接入通知队列（不依赖 watch 订阅）：接管者必须知道控制权没了。
	h.enqueue(desktopBridgeNotification{
		text:  constText(fmt.Sprintf("🔓 桌面端已收回会话「%s」的控制权，接管已解除。", label)),
		route: &route,
	})
}

// remoteControlledTabs 返回当前被接管的 tabID 集合（TabMeta 标记用）。
func (h *botBridgeHub) remoteControlledTabs() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.takeovers) == 0 {
		return nil
	}
	out := make(map[string]bool, len(h.takeovers))
	for tabID := range h.takeovers {
		out[tabID] = true
	}
	return out
}

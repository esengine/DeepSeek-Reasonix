package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"reasonix/internal/event"
)

// messageEditor 是适配器的可选能力：原地编辑已发送的消息。实现它的适配器
// （目前是飞书，经 Im.Message.Patch）获得回合中的流式输出——渲染器不断更新
// 同一条“live 消息”，而不是攒到回合结束一次性分段发送。
type messageEditor interface {
	EditMessage(ctx context.Context, messageID string, msg OutboundMessage) error
}

// renderSink 将 Reasonix 事件流渲染为平台消息。
type renderSink struct {
	ctx        context.Context
	adapter    Adapter
	editor     messageEditor // 非 nil 时启用原地编辑流式输出
	connID     string
	domain     string
	chatID     string
	chatType   ChatType
	userID     string
	replyTo    string
	logger     *slog.Logger
	ctrl       botController
	onApproval func(event.Approval)
	onAsk      func(event.Ask)

	// 渲染缓冲
	buf           strings.Builder
	thinking      strings.Builder
	inThinking    bool
	toolNames     map[string]string // tool ID -> name
	lastFlush     time.Time
	lastProgress  time.Time
	progressCount int

	// 流式 live 消息状态（editor != nil 时使用）
	liveMsgID     string    // 正在原地编辑的消息 ID；空表示当前块还没创建消息
	liveSentBytes int       // buf 前缀中已成功送达 live 消息的字节数
	lastEdit      time.Time // 上次成功 create/edit 的时间，用于限频
}

const (
	renderSoftFlushAfter      = 1200 * time.Millisecond
	renderMaxChunkRunes       = 1800
	renderHardChunkRunes      = 3500
	renderProgressMinInterval = 2 * time.Second
	renderMaxProgressMessages = 3
)

func newRenderSink(ctx context.Context, adapter Adapter, connID, domain, chatID string, chatType ChatType, userID string, replyTo string, logger *slog.Logger, onApproval func(event.Approval), onAsk func(event.Ask)) *renderSink {
	editor, _ := adapter.(messageEditor)
	return &renderSink{
		ctx:        ctx,
		adapter:    adapter,
		editor:     editor,
		connID:     connID,
		domain:     domain,
		chatID:     chatID,
		chatType:   chatType,
		userID:     userID,
		replyTo:    replyTo,
		logger:     logger,
		onApproval: onApproval,
		onAsk:      onAsk,
		toolNames:  make(map[string]string),
		lastFlush:  time.Now(),
	}
}

func (s *renderSink) Emit(e event.Event) {
	switch e.Kind {
	case event.TurnStarted:
		s.buf.Reset()
		s.thinking.Reset()
		s.inThinking = false
		s.toolNames = make(map[string]string)
		s.progressCount = 0
		s.lastProgress = time.Time{}
		s.liveMsgID = ""
		s.liveSentBytes = 0
		s.lastEdit = time.Time{}

	case event.Reasoning:
		if !s.inThinking {
			s.inThinking = true
		}
		s.thinking.WriteString(e.Text)

	case event.Text:
		if s.inThinking {
			s.inThinking = false
		}
		s.buf.WriteString(e.Text)
		s.maybeStream()

	case event.Message:
		// full message received, do nothing extra

	case event.ToolDispatch:
		if e.Tool.Refreshed {
			break
		}
		name := renderToolName(e.Tool)
		s.toolNames[e.Tool.ID] = name
		s.sendProgress(fmt.Sprintf("正在执行: %s", name), false)

	case event.ToolResult:
		name := s.toolNames[e.Tool.ID]
		if name == "" {
			name = renderToolName(e.Tool)
		}
		if e.Tool.Err != "" {
			s.sendProgress(fmt.Sprintf("%s 执行失败，稍后会在结果中说明。", name), true)
		}

	case event.ToolProgress:
		// Keep streaming tool output out of IM channels; the session transcript
		// still records the complete controller turn for desktop review.

	case event.ApprovalRequest:
		// 发送审批请求
		if s.onApproval != nil {
			s.onApproval(e.Approval)
		}
		approvalText := renderApprovalText(e.Approval)
		msg := OutboundMessage{
			ConnectionID: s.connID,
			Domain:       s.domain,
			ChatID:       s.chatID,
			ChatType:     s.chatType,
			Text:         approvalText,
			ReplyToMsgID: s.replyTo,
		}
		switch s.adapter.Platform() {
		case PlatformQQ:
			if isRecoveryApproval(e.Approval) {
				msg.Keyboard = recoveryKeyboard(e.Approval)
			} else {
				msg.Keyboard = approvalKeyboard(e.Approval.ID)
			}
		case PlatformFeishu:
			if isRecoveryApproval(e.Approval) {
				msg.Card = recoveryCard(e.Approval, s.chatType, s.userID)
			} else {
				msg.Card = approvalCard(e.Approval, s.chatType, s.userID)
			}
		}
		_ = s.send(msg)

	case event.AskRequest:
		if s.onAsk != nil {
			s.onAsk(e.Ask)
		}
		// 发送问答请求
		askText := renderAskText(e.Ask)
		msg := OutboundMessage{
			ConnectionID: s.connID,
			Domain:       s.domain,
			ChatID:       s.chatID,
			ChatType:     s.chatType,
			Text:         askText,
			ReplyToMsgID: s.replyTo,
		}
		if s.adapter.Platform() == PlatformFeishu {
			msg.Card = askCard(e.Ask, askText, s.chatType, s.userID)
		}
		_ = s.send(msg)

	case event.TurnDone:
		// 刷新缓冲
		s.flush()
		if e.Err != nil {
			if !strings.Contains(e.Err.Error(), "context canceled") {
				_ = s.send(OutboundMessage{
					ConnectionID: s.connID,
					Domain:       s.domain,
					ChatID:       s.chatID,
					ChatType:     s.chatType,
					Text:         fmt.Sprintf("❌ 执行出错: %v", e.Err),
					ReplyToMsgID: s.replyTo,
				})
			}
		}

	case event.Notice:
		if e.Level == event.LevelWarn {
			_ = s.send(OutboundMessage{
				ConnectionID: s.connID,
				Domain:       s.domain,
				ChatID:       s.chatID,
				ChatType:     s.chatType,
				Text:         fmt.Sprintf("⚠️ %s", e.Text),
				ReplyToMsgID: s.replyTo,
			})
		}

	case event.CompactionStarted:
		_ = s.send(OutboundMessage{
			ConnectionID: s.connID,
			Domain:       s.domain,
			ChatID:       s.chatID,
			ChatType:     s.chatType,
			Text:         "🔄 正在压缩上下文...",
			ReplyToMsgID: s.replyTo,
		})
	}
}

func (s *renderSink) flush() {
	for strings.TrimSpace(s.buf.String()) != "" {
		raw := s.buf.String()
		// When streaming into a live message, finalize the whole remaining text
		// with one edit instead of splitting at a semantic boundary — otherwise
		// a final answer that does not end on a boundary (code block, list, URL)
		// gets shrunk in place and its tail re-sent as a separate message,
		// defeating the point of in-place streaming. Only fall back to boundary
		// chunking when the remainder genuinely exceeds the hard cap.
		if s.editor != nil && s.liveMsgID != "" && len([]rune(raw)) < renderHardChunkRunes {
			s.flushPrefix(len(raw))
			continue
		}
		idx := renderFlushIndex(raw, renderSoftFlushAfter)
		if idx <= 0 {
			idx = byteIndexForRuneLimit(raw, renderMaxChunkRunes)
		}
		if idx <= 0 || idx > len(raw) {
			idx = len(raw)
		}
		s.flushPrefix(idx)
	}
}

func (s *renderSink) flushPrefix(idx int) {
	raw := s.buf.String()
	if idx <= 0 || idx > len(raw) {
		idx = len(raw)
	}
	text := strings.TrimSpace(raw[:idx])
	if text == "" {
		remaining := raw[idx:]
		s.buf.Reset()
		s.buf.WriteString(remaining)
		s.lastFlush = time.Now()
		return
	}
	// resumeFrom marks where the not-yet-delivered remainder starts. On success
	// it is idx (the block boundary). On edit failure the live message is frozen
	// at raw[:liveSentBytes], so anything already shown past idx must NOT be
	// re-queued — the resume point becomes max(idx, liveSentBytes), otherwise the
	// [idx, liveSentBytes] span is both displayed and re-sent (duplication).
	resumeFrom := idx
	if s.liveMsgID != "" {
		// 当前块已有 live 消息：把最终内容原地编辑进去，而不是再发一条。
		if err := s.editLive(text); err != nil {
			s.logger.Warn("bot live message final edit failed; sending tail as new message", "err", err)
			if tail := strings.TrimSpace(raw[min(s.liveSentBytes, idx):idx]); tail != "" {
				_ = s.send(s.textMessage(tail))
			}
			if s.liveSentBytes > resumeFrom {
				resumeFrom = s.liveSentBytes
			}
		}
		s.liveMsgID = ""
		s.liveSentBytes = 0
	} else {
		_ = s.send(s.textMessage(text))
	}
	if resumeFrom > len(raw) {
		resumeFrom = len(raw)
	}
	remaining := raw[resumeFrom:]
	s.buf.Reset()
	s.buf.WriteString(remaining)
	s.lastFlush = time.Now()
}

// maybeStream 在每个文本增量后驱动流式输出：把已缓冲文本 create/edit 到
// live 消息。仅当适配器支持原地编辑时启用；限频间隔复用 renderSoftFlushAfter
// （1.2s，低于飞书单消息 Patch 的 QPS 上限）。
func (s *renderSink) maybeStream() {
	if s.editor == nil {
		return
	}
	raw := s.buf.String()
	if len([]rune(raw)) >= renderHardChunkRunes {
		// 当前块过长：按语义边界收尾 live 消息，剩余文本进入下一块。
		idx := lastSemanticBoundary(raw, renderMaxChunkRunes)
		if idx <= 0 {
			idx = byteIndexForRuneLimit(raw, renderMaxChunkRunes)
		}
		s.flushPrefix(idx)
		return
	}
	last := s.lastEdit
	if s.liveMsgID == "" {
		last = s.lastFlush
	}
	if time.Since(last) < renderSoftFlushAfter {
		return
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		return
	}
	if s.liveMsgID == "" {
		res, err := s.adapter.Send(s.ctx, s.textMessage(text))
		if err != nil {
			// 创建失败（可能是瞬时网络错误）：文本留在 buf 里，限频后重试；
			// 就算一直失败，回合末的 flush 也会兜底发送。
			s.logger.Warn("bot live message create failed", "err", err)
			s.lastFlush = time.Now()
			return
		}
		if strings.TrimSpace(res.MessageID) == "" {
			// 平台没回消息 ID，无法编辑：本回合退回“攒到回合末分段发送”，
			// 已发出的前缀从 buf 里去掉避免重复。
			s.editor = nil
			s.cutBufPrefix(len(raw))
			return
		}
		s.liveMsgID = res.MessageID
		s.liveSentBytes = len(raw)
		s.lastEdit = time.Now()
		return
	}
	if err := s.editLive(text); err != nil {
		// 编辑失败（限频/超长/消息被撤回）：结束这个块，已送达前缀不再重发，
		// 未送达的尾部留在 buf 里由下一条消息续上。
		s.logger.Warn("bot live message edit failed; rotating to new message", "err", err)
		s.cutBufPrefix(s.liveSentBytes)
		s.liveMsgID = ""
		s.liveSentBytes = 0
		return
	}
	s.liveSentBytes = len(raw)
	s.lastEdit = time.Now()
}

func (s *renderSink) editLive(text string) error {
	err := s.editor.EditMessage(s.ctx, s.liveMsgID, s.textMessage(text))
	if err == nil {
		s.lastEdit = time.Now()
	}
	return err
}

// cutBufPrefix 从 buf 头部移除 n 个字节（已送达 live 消息的内容）。
func (s *renderSink) cutBufPrefix(n int) {
	raw := s.buf.String()
	if n <= 0 {
		return
	}
	if n > len(raw) {
		n = len(raw)
	}
	remaining := raw[n:]
	s.buf.Reset()
	s.buf.WriteString(remaining)
	s.lastFlush = time.Now()
}

func (s *renderSink) textMessage(text string) OutboundMessage {
	return OutboundMessage{
		ConnectionID: s.connID,
		Domain:       s.domain,
		ChatID:       s.chatID,
		ChatType:     s.chatType,
		Text:         text,
		ReplyToMsgID: s.replyTo,
	}
}

func (s *renderSink) sendProgress(text string, force bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	now := time.Now()
	if s.progressCount >= renderMaxProgressMessages {
		return
	}
	if !force && !s.lastProgress.IsZero() && now.Sub(s.lastProgress) < renderProgressMinInterval {
		return
	}
	_ = s.send(OutboundMessage{
		ConnectionID: s.connID,
		Domain:       s.domain,
		ChatID:       s.chatID,
		ChatType:     s.chatType,
		Text:         text,
		ReplyToMsgID: s.replyTo,
	})
	s.progressCount++
	s.lastProgress = now
}

func renderToolName(t event.Tool) string {
	if name := strings.TrimSpace(t.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(t.ID); id != "" {
		return id
	}
	return "tool"
}

func renderFlushIndex(text string, elapsed time.Duration) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	runes := []rune(text)
	if len(runes) >= renderHardChunkRunes {
		if idx := lastSemanticBoundary(text, renderHardChunkRunes); idx > 0 {
			return idx
		}
		return byteIndexForRuneLimit(text, renderMaxChunkRunes)
	}
	if len(runes) >= renderMaxChunkRunes {
		if idx := lastSemanticBoundary(text, renderMaxChunkRunes); idx > 0 {
			return idx
		}
	}
	if elapsed < renderSoftFlushAfter {
		return 0
	}
	return lastSemanticBoundary(text, len(runes))
}

func lastSemanticBoundary(text string, maxRunes int) int {
	if maxRunes <= 0 {
		return 0
	}
	count := 0
	lastBoundary := 0
	lastNonSpaceBoundary := 0
	inFence := false
	for idx, r := range text {
		if strings.HasPrefix(text[idx:], "```") {
			inFence = !inFence
		}
		count++
		if count > maxRunes {
			break
		}
		next := idx + len(string(r))
		if r == '\n' && !inFence {
			lastNonSpaceBoundary = next
			lastBoundary = next
			continue
		}
		if unicode.IsSpace(r) {
			if lastNonSpaceBoundary > 0 {
				lastBoundary = next
			}
			continue
		}
		if inFence {
			continue
		}
		if isSemanticBoundaryRune(r) {
			lastNonSpaceBoundary = next
			lastBoundary = next
		}
	}
	return lastBoundary
}

func isSemanticBoundaryRune(r rune) bool {
	switch r {
	case '.', '!', '?', ';', '。', '！', '？', '；', '…':
		return true
	default:
		return false
	}
}

func byteIndexForRuneLimit(text string, maxRunes int) int {
	if maxRunes <= 0 {
		return 0
	}
	count := 0
	for idx, r := range text {
		count++
		if count >= maxRunes {
			return idx + len(string(r))
		}
	}
	return len(text)
}

func (s *renderSink) send(msg OutboundMessage) error {
	_, err := s.adapter.Send(s.ctx, msg)
	return err
}

func approvalKeyboard(id string) *InlineKeyboard {
	return &InlineKeyboard{Rows: []InlineKeyboardRow{{
		Buttons: []InlineKeyboardButton{
			{ID: "allow_once", Label: "允许一次", Style: 1, CallbackID: "/approve " + id},
			{ID: "deny", Label: "拒绝", Style: 2, CallbackID: "/deny " + id},
		},
	}}}
}

func recoveryKeyboard(a event.Approval) *InlineKeyboard {
	if isRecoveryPlanChange(a) {
		return &InlineKeyboard{Rows: []InlineKeyboardRow{{Buttons: []InlineKeyboardButton{
			{ID: "recovery_continue", Label: "1 采用并继续", Style: 0, CallbackID: "/recovery-continue " + a.ID},
			{ID: "recovery_revise", Label: "2 不采用并调整", Style: 0, CallbackID: "/recovery-revise " + a.ID},
		}}}}
	}
	buttons := []InlineKeyboardButton{{ID: "recovery_continue", Label: "1 继续一次", Style: 1, CallbackID: "/recovery-continue " + a.ID}}
	if a.Recovery != nil && a.Recovery.CanGrantTask {
		buttons = append(buttons, InlineKeyboardButton{ID: "recovery_continue_task", Label: "2 本任务允许同类", Style: 0, CallbackID: "/recovery-continue-task " + a.ID})
		return &InlineKeyboard{Rows: []InlineKeyboardRow{{Buttons: buttons}, {Buttons: []InlineKeyboardButton{{ID: "recovery_revise", Label: "3 换个办法", Style: 0, CallbackID: "/recovery-revise " + a.ID}}}}}
	}
	buttons = append(buttons, InlineKeyboardButton{ID: "recovery_revise", Label: "2 换个办法", Style: 0, CallbackID: "/recovery-revise " + a.ID})
	return &InlineKeyboard{Rows: []InlineKeyboardRow{{Buttons: buttons}}}
}

func isRecoveryApproval(a event.Approval) bool {
	return strings.EqualFold(strings.TrimSpace(a.Kind), "recovery") || a.Recovery != nil
}

func isRecoveryPlanChange(a event.Approval) bool {
	if !isRecoveryApproval(a) || a.Recovery == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(a.Recovery.ChangeKind)) {
	case "strategy", "scope":
		return true
	default:
		return false
	}
}

func renderApprovalText(a event.Approval) string {
	if isRecoveryApproval(a) {
		return renderRecoveryText(a)
	}
	return fmt.Sprintf("⚠️ 需要批准操作:\n工具: %s\n操作: %s\n\nID: `%s`\n回复 1 批准，回复 2 拒绝；也可用 /approve %s 或 /deny %s。",
		a.Tool, a.Subject, a.ID, a.ID, a.ID)
}

func renderRecoveryText(a event.Approval) string {
	var b strings.Builder
	if isRecoveryPlanChange(a) {
		b.WriteString("⚠️ 执行计划需要你的决定\n")
	} else {
		b.WriteString("⚠️ 执行前确认\n")
	}
	rec := a.Recovery
	if rec != nil {
		if isRecoveryPlanChange(a) && (strings.TrimSpace(rec.PlanBefore) != "" || strings.TrimSpace(rec.PlanAfter) != "") {
			if before := clipBotPlan(rec.PlanBefore); before != "" {
				fmt.Fprintf(&b, "原计划:\n%s\n", before)
			}
			if after := clipBotPlan(rec.PlanAfter); after != "" {
				fmt.Fprintf(&b, "新计划:\n%s\n", after)
			}
		} else if next := firstNonEmptyBot(rec.NextAction, a.Subject, a.Tool); next != "" {
			fmt.Fprintf(&b, "即将执行: %s\n", next)
		}
		why := firstNonEmptyBot(rec.ChangeRationale, rec.ReviewRationale, a.Reason)
		if why != "" {
			fmt.Fprintf(&b, "原因: %s\n", why)
		}
	} else {
		fmt.Fprintf(&b, "即将执行: %s\n", firstNonEmptyBot(a.Subject, a.Tool))
	}
	if isRecoveryPlanChange(a) {
		fmt.Fprintf(&b, "\nID: `%s`\n回复 1 采用新计划并继续，2 不采用并让 Auto 调整。需要给出具体意见时，可使用 `/recovery-revise %s <调整意见>`。", a.ID, a.ID)
	} else if rec != nil && rec.CanGrantTask {
		if scope := strings.TrimSpace(rec.TaskGrantScope); scope != "" {
			fmt.Fprintf(&b, "授权范围: %s\n", scope)
		}
		fmt.Fprintf(&b, "\nID: `%s`\n回复 1 继续一次，2 在本任务内允许同类操作，3 换个办法。范围扩大或风险升级仍会再次确认。", a.ID)
	} else {
		fmt.Fprintf(&b, "\nID: `%s`\n回复 1 继续，2 换个办法。", a.ID)
	}
	return b.String()
}

func approvalCard(a event.Approval, chatType ChatType, userID string) *InteractiveCard {
	return &InteractiveCard{
		Header: "需要批准操作",
		Elements: []InteractiveCardElement{
			{Tag: "markdown", Content: fmt.Sprintf("**工具**: %s\n\n**操作**: %s\n\nID: `%s`", a.Tool, a.Subject, a.ID)},
			{Tag: "action", Extra: map[string]any{
				"actions": []map[string]any{
					{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "允许一次"}, "type": "primary", "value": cardActionValue("/approve "+a.ID, chatType, userID)},
					{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "拒绝"}, "type": "danger", "value": cardActionValue("/deny "+a.ID, chatType, userID)},
				},
			}},
		},
	}
}

func recoveryCard(a event.Approval, chatType ChatType, userID string) *InteractiveCard {
	if isRecoveryPlanChange(a) {
		return &InteractiveCard{
			Header: "执行计划需要你的决定",
			Elements: []InteractiveCardElement{
				{Tag: "markdown", Content: renderRecoveryText(a)},
				{Tag: "action", Extra: map[string]any{
					"actions": []map[string]any{
						{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "采用并继续"}, "type": "default", "value": cardActionValue("/recovery-continue "+a.ID, chatType, userID)},
						{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "不采用并调整"}, "type": "default", "value": cardActionValue("/recovery-revise "+a.ID, chatType, userID)},
					},
				}},
			},
		}
	}
	actions := []map[string]any{
		{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "继续一次"}, "type": "primary", "value": cardActionValue("/recovery-continue "+a.ID, chatType, userID)},
	}
	if a.Recovery != nil && a.Recovery.CanGrantTask {
		actions = append(actions, map[string]any{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "本任务允许同类"}, "type": "default", "value": cardActionValue("/recovery-continue-task "+a.ID, chatType, userID)})
	}
	actions = append(actions, map[string]any{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "换个办法"}, "type": "default", "value": cardActionValue("/recovery-revise "+a.ID, chatType, userID)})
	return &InteractiveCard{
		Header: "执行前确认",
		Elements: []InteractiveCardElement{
			{Tag: "markdown", Content: renderRecoveryText(a)},
			{Tag: "action", Extra: map[string]any{
				"actions": actions,
			}},
		},
	}
}

func clipBotPlan(plan string) string {
	plan = strings.TrimSpace(plan)
	const maxRunes = 800
	runes := []rune(plan)
	if len(runes) <= maxRunes {
		return plan
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}

func firstNonEmptyBot(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cardActionValue(command string, chatType ChatType, userID string) map[string]string {
	value := map[string]string{
		"command":   command,
		"chat_type": string(chatType),
	}
	if strings.TrimSpace(userID) != "" {
		value["user_id"] = strings.TrimSpace(userID)
	}
	return value
}

func renderAskText(ask event.Ask) string {
	var qb strings.Builder
	qb.WriteString("❓ 请回答以下问题:\n")
	for i, q := range ask.Questions {
		fmt.Fprintf(&qb, "\n**%d. %s**\n", i+1, q.Prompt)
		for j, opt := range q.Options {
			fmt.Fprintf(&qb, "  %d. %s", j+1, opt.Label)
			if opt.Description != "" {
				fmt.Fprintf(&qb, " — %s", opt.Description)
			}
			qb.WriteString("\n")
		}
		if q.Multi {
			qb.WriteString("  (可多选)\n")
		}
	}
	fmt.Fprintf(&qb, "\nID: `%s`", ask.ID)
	if askSupportsNumericShortcut(ask) {
		fmt.Fprintf(&qb, "\n直接回复选项编号即可回答；也可用 /answer %s <选项编号或文本>。", ask.ID)
	} else {
		fmt.Fprintf(&qb, "\n用 /answer %s <选项编号或文本> 回答；多题可用 q1=1;q2=2。", ask.ID)
	}
	return qb.String()
}

func askCard(ask event.Ask, fallback string, chatType ChatType, userID string) *InteractiveCard {
	card := &InteractiveCard{
		Header: "需要回答问题",
		Elements: []InteractiveCardElement{
			{Tag: "markdown", Content: fallback},
		},
	}
	if !askSupportsNumericShortcut(ask) {
		return card
	}
	question := ask.Questions[0]
	actions := make([]map[string]any, 0, len(question.Options))
	for i, opt := range question.Options {
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			label = fmt.Sprintf("选项 %d", i+1)
		}
		actions = append(actions, map[string]any{
			"tag":   "button",
			"text":  map[string]string{"tag": "plain_text", "content": label},
			"type":  "primary",
			"value": cardActionValue(fmt.Sprintf("/answer %s %d", ask.ID, i+1), chatType, userID),
		})
	}
	if len(actions) > 0 {
		card.Elements = append(card.Elements, InteractiveCardElement{Tag: "action", Extra: map[string]any{"actions": actions}})
	}
	return card
}

func askSupportsNumericShortcut(ask event.Ask) bool {
	return len(ask.Questions) == 1 && len(ask.Questions[0].Options) > 0
}

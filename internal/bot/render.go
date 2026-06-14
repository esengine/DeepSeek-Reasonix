package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// renderSink 将 Reasonix 事件流渲染为平台消息。
type renderSink struct {
	ctx        context.Context
	adapter    Adapter
	connID     string
	domain     string
	chatID     string
	chatType   ChatType
	userID     string
	replyTo    string
	logger     *slog.Logger
	ctrl       *control.Controller
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
}

const (
	renderSoftFlushAfter      = 1200 * time.Millisecond
	renderMaxChunkRunes       = 1800
	renderHardChunkRunes      = 3500
	renderProgressMinInterval = 2 * time.Second
	renderMaxProgressMessages = 3
)

func newRenderSink(ctx context.Context, adapter Adapter, connID, domain, chatID string, chatType ChatType, userID string, replyTo string, logger *slog.Logger, onApproval func(event.Approval), onAsk func(event.Ask)) *renderSink {
	return &renderSink{
		ctx:        ctx,
		adapter:    adapter,
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

	case event.Message:
		// full message received, do nothing extra

	case event.ToolDispatch:
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
		approvalText := fmt.Sprintf("⚠️ 需要批准操作:\n工具: %s\n操作: %s\n\nID: `%s`\n回复 1 批准，回复 2 拒绝；也可用 /approve %s 或 /deny %s。",
			e.Approval.Tool, e.Approval.Subject, e.Approval.ID, e.Approval.ID, e.Approval.ID)
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
			msg.Keyboard = approvalKeyboard(e.Approval.ID)
		case PlatformFeishu:
			msg.Card = approvalCard(e.Approval, s.chatType, s.userID)
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
		idx := renderFlushIndex(s.buf.String(), renderSoftFlushAfter)
		if idx <= 0 {
			idx = byteIndexForRuneLimit(s.buf.String(), renderMaxChunkRunes)
		}
		if idx <= 0 || idx > len(s.buf.String()) {
			idx = len(s.buf.String())
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
	_ = s.send(OutboundMessage{
		ConnectionID: s.connID,
		Domain:       s.domain,
		ChatID:       s.chatID,
		ChatType:     s.chatType,
		Text:         text,
		ReplyToMsgID: s.replyTo,
	})
	remaining := raw[idx:]
	s.buf.Reset()
	s.buf.WriteString(remaining)
	s.lastFlush = time.Now()
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

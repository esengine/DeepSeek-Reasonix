package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// renderSink 将 Reasonix 事件流渲染为平台消息。
type renderSink struct {
	ctx      context.Context
	adapter  Adapter
	chatID   string
	chatType ChatType
	replyTo  string
	logger   *slog.Logger
	ctrl     *control.Controller
	onAsk    func(event.Ask)

	// 渲染缓冲
	buf        strings.Builder
	thinking   strings.Builder
	inThinking bool
	toolNames  map[string]string // tool ID -> name
	lastFlush  time.Time
}

func newRenderSink(ctx context.Context, adapter Adapter, chatID string, chatType ChatType, replyTo string, logger *slog.Logger, onAsk func(event.Ask)) *renderSink {
	return &renderSink{
		ctx:       ctx,
		adapter:   adapter,
		chatID:    chatID,
		chatType:  chatType,
		replyTo:   replyTo,
		logger:    logger,
		onAsk:     onAsk,
		toolNames: make(map[string]string),
		lastFlush: time.Now(),
	}
}

func (s *renderSink) Emit(e event.Event) {
	switch e.Kind {
	case event.TurnStarted:
		s.buf.Reset()
		s.thinking.Reset()
		s.inThinking = false
		s.toolNames = make(map[string]string)

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
		s.maybeFlush()

	case event.Message:
		// full message received, do nothing extra

	case event.ToolDispatch:
		s.toolNames[e.Tool.ID] = e.Tool.Name
		txt := fmt.Sprintf("\n🔧 执行工具: %s", e.Tool.Name)
		if e.Tool.ReadOnly {
			txt += " (只读)"
		}
		s.buf.WriteString(txt)
		s.maybeFlush()

	case event.ToolResult:
		name := s.toolNames[e.Tool.ID]
		if name == "" {
			name = e.Tool.ID
		}
		if e.Tool.Err != "" {
			fmt.Fprintf(&s.buf, "\n❌ %s 出错: %s", name, e.Tool.Err)
		} else {
			// 截断输出
			output := e.Tool.Output
			if len(output) > 500 {
				output = output[:500] + "\n... (已截断)"
			}
			fmt.Fprintf(&s.buf, "\n✅ %s 完成", name)
			if output != "" {
				fmt.Fprintf(&s.buf, "\n```\n%s\n```", output)
			}
		}
		s.maybeFlush()

	case event.ToolProgress:
		// 流式输出，不单独渲染
		s.maybeFlush()

	case event.ApprovalRequest:
		// 发送审批请求
		approvalText := fmt.Sprintf("⚠️ 需要批准操作:\n工具: %s\n操作: %s\n\nID: `%s`\n用 /approve %s 批准，/deny %s 拒绝。",
			e.Approval.Tool, e.Approval.Subject, e.Approval.ID, e.Approval.ID, e.Approval.ID)
		msg := OutboundMessage{
			ChatID:       s.chatID,
			ChatType:     s.chatType,
			Text:         approvalText,
			ReplyToMsgID: s.replyTo,
		}
		switch s.adapter.Platform() {
		case PlatformQQ:
			msg.Keyboard = approvalKeyboard(e.Approval.ID)
		case PlatformFeishu:
			msg.Card = approvalCard(e.Approval, s.chatType)
		}
		_ = s.send(msg)

	case event.AskRequest:
		if s.onAsk != nil {
			s.onAsk(e.Ask)
		}
		// 发送问答请求
		var qb strings.Builder
		qb.WriteString("❓ 请回答以下问题:\n")
		for i, q := range e.Ask.Questions {
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
		fmt.Fprintf(&qb, "\nID: `%s`", e.Ask.ID)
		fmt.Fprintf(&qb, "\n用 /answer %s <选项编号或文本> 回答。", e.Ask.ID)
		msg := OutboundMessage{
			ChatID:       s.chatID,
			ChatType:     s.chatType,
			Text:         qb.String(),
			ReplyToMsgID: s.replyTo,
		}
		if s.adapter.Platform() == PlatformFeishu {
			msg.Card = askCard(e.Ask, qb.String())
		}
		_ = s.send(msg)

	case event.TurnDone:
		// 刷新缓冲
		s.flush()
		if e.Err != nil {
			if !strings.Contains(e.Err.Error(), "context canceled") {
				_ = s.send(OutboundMessage{
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
				ChatID:       s.chatID,
				ChatType:     s.chatType,
				Text:         fmt.Sprintf("⚠️ %s", e.Text),
				ReplyToMsgID: s.replyTo,
			})
		}

	case event.CompactionStarted:
		_ = s.send(OutboundMessage{
			ChatID:       s.chatID,
			ChatType:     s.chatType,
			Text:         "🔄 正在压缩上下文...",
			ReplyToMsgID: s.replyTo,
		})
	}
}

func (s *renderSink) maybeFlush() {
	if time.Since(s.lastFlush) > 500*time.Millisecond {
		s.flush()
	}
}

func (s *renderSink) flush() {
	text := strings.TrimSpace(s.buf.String())
	if text == "" {
		return
	}
	_ = s.send(OutboundMessage{
		ChatID:       s.chatID,
		ChatType:     s.chatType,
		Text:         text,
		ReplyToMsgID: s.replyTo,
	})
	s.buf.Reset()
	s.lastFlush = time.Now()
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

func approvalCard(a event.Approval, chatType ChatType) *InteractiveCard {
	return &InteractiveCard{
		Header: "需要批准操作",
		Elements: []InteractiveCardElement{
			{Tag: "markdown", Content: fmt.Sprintf("**工具**: %s\n\n**操作**: %s\n\nID: `%s`", a.Tool, a.Subject, a.ID)},
			{Tag: "action", Extra: map[string]any{
				"actions": []map[string]any{
					{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "允许一次"}, "type": "primary", "value": cardActionValue("/approve "+a.ID, chatType)},
					{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "拒绝"}, "type": "danger", "value": cardActionValue("/deny "+a.ID, chatType)},
				},
			}},
		},
	}
}

func cardActionValue(command string, chatType ChatType) map[string]string {
	return map[string]string{
		"command":   command,
		"chat_type": string(chatType),
	}
}

func askCard(ask event.Ask, fallback string) *InteractiveCard {
	return &InteractiveCard{
		Header: "需要回答问题",
		Elements: []InteractiveCardElement{
			{Tag: "markdown", Content: fallback},
		},
	}
}

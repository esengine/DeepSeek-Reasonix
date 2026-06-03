package adapter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

const (
	flushInterval = 200 * time.Millisecond
)

type Options struct {
	ChatID            string
	MessageID         string
	ShowReasoning     bool
	ShowToolProgress  bool
	MaxResponseLength int
}

type toolRecord struct {
	Name      string
	Output    string
	Err       string
	Truncated bool
}

type EventAdapter struct {
	ch   channeltypes.Channel
	opts Options

	streamCtrl channeltypes.StreamController
	totalChars int
	started    bool

	buf     strings.Builder
	bufMu   sync.Mutex
	flushCh chan struct{}
	done    chan struct{}

	toolLog      []toolRecord
	promptTokens int
	outputTokens int
}

func New(ch channeltypes.Channel, opts Options) *EventAdapter {
	if opts.MaxResponseLength <= 0 {
		opts.MaxResponseLength = 8000
	}
	return &EventAdapter{
		ch:      ch,
		opts:    opts,
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

func (a *EventAdapter) Flush(ctx context.Context) error {
	a.bufMu.Lock()
	text := a.buf.String()
	a.buf.Reset()
	a.bufMu.Unlock()

	if text != "" && a.streamCtrl != nil {
		return a.streamCtrl.Append(ctx, text)
	}
	if a.streamCtrl != nil {
		return a.streamCtrl.Flush(ctx)
	}
	return nil
}

func (a *EventAdapter) ProcessEvents(ctx context.Context, events []event.Event) error {
	for _, ev := range events {
		switch ev.Kind {
		case event.TurnStarted:
			a.handleTurnStarted(ctx)
		case event.Reasoning:
			a.handleReasoning(ctx, ev.Text)
		case event.Text:
			a.handleText(ctx, ev.Text)
		case event.ToolDispatch:
			a.handleToolDispatch(ctx, ev.Tool)
		case event.ToolProgress:
			a.handleToolProgress(ctx, ev.Text)
		case event.ToolResult:
			a.handleToolResult(ctx, ev.Tool)
		case event.Phase:
			a.handlePhase(ctx, ev.Text)
		case event.Notice:
			a.handleNotice(ctx, ev)
		case event.ApprovalRequest:
			a.handleApprovalRequest(ctx, ev)
		case event.Usage:
			a.handleUsage(ev.Usage)
		case event.TurnDone:
			a.handleTurnDone(ctx, ev)
		case event.CompactionStarted:
			a.appendBuf("\n> 🗜️ *压缩上下文中...*\n")
		case event.CompactionDone:
			a.appendBuf("\n> ✅ *上下文已压缩*\n")
		}
	}
	return nil
}

func (a *EventAdapter) startStream(ctx context.Context) error {
	if a.started {
		return nil
	}
	ctrl, err := a.ch.Stream(ctx, &channeltypes.SendInput{
		ChatID:         a.opts.ChatID,
		ReplyMessageID: a.opts.MessageID,
	})
	if err != nil {
		return err
	}
	a.streamCtrl = ctrl
	a.started = true
	a.totalChars = 0
	a.done = make(chan struct{})

	go a.flushLoop(ctx)

	return nil
}

func (a *EventAdapter) CloseAndRestart(ctx context.Context, replyMessageID string) error {
	a.flushBuffer(ctx)

	if a.streamCtrl != nil {
		_ = a.streamCtrl.Close(ctx)
		a.streamCtrl = nil
	}
	if a.started {
		a.started = false
		close(a.done)
	}

	if replyMessageID != "" {
		a.opts.MessageID = replyMessageID
		return a.startStream(ctx)
	}
	return nil
}

func (a *EventAdapter) closeStream(ctx context.Context) {
	if a.streamCtrl != nil {
		_ = a.streamCtrl.Flush(ctx)
		_ = a.streamCtrl.Close(ctx)
		a.streamCtrl = nil
	}
	if a.started {
		a.started = false
		close(a.done)
	}
}

func (a *EventAdapter) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.flushBuffer(ctx)
		case <-a.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (a *EventAdapter) flushBuffer(ctx context.Context) {
	a.bufMu.Lock()
	text := a.buf.String()
	a.buf.Reset()
	a.bufMu.Unlock()

	if text == "" || a.streamCtrl == nil {
		return
	}
	_ = a.streamCtrl.Append(ctx, text)
}

func (a *EventAdapter) handleTurnStarted(ctx context.Context) {
	a.streamCtrl = nil
	a.started = false
	a.totalChars = 0
	a.toolLog = nil
	a.promptTokens = 0
	a.outputTokens = 0
	a.buf.Reset()
	_ = a.startStream(ctx)
}

func (a *EventAdapter) handleReasoning(ctx context.Context, text string) {
	if !a.opts.ShowReasoning {
		return
	}
	a.appendBuf(text)
}

func (a *EventAdapter) handleText(ctx context.Context, text string) {
	a.appendBuf(text)
}

func (a *EventAdapter) handleToolDispatch(ctx context.Context, tool event.Tool) {
	if a.opts.ShowToolProgress {
		a.appendBuf("\n> ⏳ **" + tool.Name + "**\n")
		return
	}
	a.toolLog = append(a.toolLog, toolRecord{Name: tool.Name})
}

func (a *EventAdapter) handleToolProgress(ctx context.Context, text string) {
	if !a.opts.ShowToolProgress {
		return
	}
	a.appendBuf(text)
}

func (a *EventAdapter) handleToolResult(ctx context.Context, tool event.Tool) {
	if a.opts.ShowToolProgress {
		if tool.Err != "" {
			a.appendBuf("\n> ❌ **" + tool.Name + "** — " + tool.Err + "\n")
		} else {
			a.appendBuf("\n> ✅ **" + tool.Name + "**\n")
		}
		return
	}
	if len(a.toolLog) > 0 {
		last := &a.toolLog[len(a.toolLog)-1]
		preview := tool.Output
		if len(preview) > 500 {
			preview = preview[:497] + "..."
		}
		last.Output = preview
		last.Err = tool.Err
		last.Truncated = tool.Truncated
	}
}

func (a *EventAdapter) handlePhase(ctx context.Context, label string) {
	if !a.opts.ShowToolProgress {
		return
	}
	a.appendBuf("\n> 📋 *" + label + "*\n")
}

func (a *EventAdapter) handleNotice(ctx context.Context, ev event.Event) {
	if ev.Level == event.LevelWarn {
		_, _ = a.ch.Send(ctx, &channeltypes.SendInput{
			ChatID: a.opts.ChatID,
			Text:   ev.Text,
		})
	} else {
		a.appendBuf(ev.Text)
	}
}

func (a *EventAdapter) handleUsage(usage *provider.Usage) {
	if usage == nil {
		return
	}
	a.promptTokens = usage.PromptTokens
	a.outputTokens = usage.CompletionTokens
}

func (a *EventAdapter) handleApprovalRequest(ctx context.Context, ev event.Event) {
	a.flushBuffer(ctx)
	if a.streamCtrl != nil {
		_ = a.streamCtrl.Flush(ctx)
	}
}

func (a *EventAdapter) handleTurnDone(ctx context.Context, ev event.Event) {
	a.flushBuffer(ctx)

	if ev.Err != nil {
		a.closeStream(ctx)
		_, _ = a.ch.Send(ctx, &channeltypes.SendInput{
			ChatID:   a.opts.ChatID,
			Markdown: "> ⚠️ **错误**: " + ev.Err.Error(),
		})
		return
	}

	a.closeStream(ctx)

	var parts []string
	if !a.opts.ShowToolProgress && len(a.toolLog) > 0 {
		parts = append(parts, formatToolSummary(a.toolLog))
	}
	if a.promptTokens > 0 || a.outputTokens > 0 {
		parts = append(parts, formatUsageFooter(a.promptTokens, a.outputTokens))
	}
	if a.totalChars >= a.opts.MaxResponseLength {
		parts = append(parts, "> response truncated")
	}
	if len(parts) > 0 {
		_ = a.sendMarkdown(ctx, strings.Join(parts, "\n"))
	}
}

func (a *EventAdapter) sendMarkdown(ctx context.Context, md string) error {
	_, err := a.ch.Send(ctx, &channeltypes.SendInput{
		ChatID:   a.opts.ChatID,
		Markdown: md,
	})
	return err
}

func (a *EventAdapter) appendBuf(text string) {
	a.bufMu.Lock()
	defer a.bufMu.Unlock()

	textLen := len(text)
	if a.totalChars+textLen > a.opts.MaxResponseLength {
		remaining := a.opts.MaxResponseLength - a.totalChars
		if remaining > 0 {
			a.buf.WriteString(text[:remaining])
			a.totalChars += remaining
		}
		return
	}
	a.buf.WriteString(text)
	a.totalChars += textLen
}

func (a *EventAdapter) appendBufDirect(ctx context.Context, text string) {
	if a.streamCtrl == nil {
		return
	}
	textLen := len(text)
	if a.totalChars+textLen > a.opts.MaxResponseLength {
		remaining := a.opts.MaxResponseLength - a.totalChars
		if remaining > 0 {
			_ = a.streamCtrl.Append(ctx, text[:remaining])
			a.totalChars += remaining
		}
		return
	}
	_ = a.streamCtrl.Append(ctx, text)
	a.totalChars += textLen
}

func formatToolSummary(log []toolRecord) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("**🔧 工具调用 (%d)**\n", len(log)))
	for i, t := range log {
		status := "✅"
		if t.Err != "" {
			status = "❌"
		}
		b.WriteString(fmt.Sprintf("%d. %s `%s`", i+1, status, t.Name))
		if t.Err != "" {
			b.WriteString(" — " + truncateForSummary(t.Err, 80))
		} else if t.Output != "" {
			b.WriteString(": " + truncateForSummary(t.Output, 60))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatUsageFooter(prompt, output int) string {
	total := prompt + output
	return fmt.Sprintf("---\n📊 **Token 用量**: %d (输入 %d + 输出 %d)", total, prompt, output)
}

func truncateForSummary(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

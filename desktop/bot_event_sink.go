package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/event"
)

const botForwardSendTimeout = 30 * time.Second
const botForwardQueueSize = 64

// ── Forward target ──────────────────────────────────────────────────────────

// botForwardTarget identifies one remote chat to send forwarded events to.
type botForwardTarget struct {
	ConnID   string
	Domain   string
	ChatID   string
	ChatType bot.ChatType
}

// ── Event forwarder ─────────────────────────────────────────────────────────

// botEventForwarder implements event.Sink and forwards relevant events to
// connected bot channels through the desktopBotRuntime. It is attached to a
// tabEventSink when a heartbeat task should push AI output to IM channels.
//
// It accumulates Text events and sends them as complete messages on TurnDone
// (and occasionally during generation when the buffer grows large enough), so
// the remote side sees progressive streaming output rather than one big blob.
type botEventForwarder struct {
	runtime *desktopBotRuntime
	targets []botForwardTarget

	mu        sync.Mutex
	buf       strings.Builder
	queueMu   sync.Mutex
	queue     chan string
	closed    bool
	closeOnce sync.Once
}

// newBotEventForwarder creates a forwarder that sends to all given targets.
// runtime may be nil — Emit calls are then no-ops.
func newBotEventForwarder(runtime *desktopBotRuntime, targets []botForwardTarget) *botEventForwarder {
	f := &botEventForwarder{
		runtime: runtime,
		targets: targets,
		queue:   make(chan string, botForwardQueueSize),
	}
	go f.run()
	return f
}

// Emit implements event.Sink. It forwards text and lifecycle events to the
// connected bot channels; reasoning, tool dispatch, and other internal events
// are dropped to avoid noisy IM output.
func (f *botEventForwarder) Emit(e event.Event) {
	if f.runtime == nil || len(f.targets) == 0 {
		return
	}
	switch e.Kind {
	case event.TurnStarted:
		f.mu.Lock()
		f.buf.Reset()
		f.mu.Unlock()

	case event.Text:
		f.mu.Lock()
		f.buf.WriteString(e.Text)
		size := f.buf.Len()
		f.mu.Unlock()
		// Flush opportunistically when the buffer crosses a threshold, so long
		// streams (e.g. "tell me three jokes") produce multiple messages.
		if size >= 400 {
			f.flush()
		}

	case event.TurnDone:
		f.flush()
		f.Close()

	case event.ApprovalRequest:
		// The heartbeat turn belongs to the desktop tab controller, not the bot
		// gateway session, so remote /approve replies cannot satisfy this ID.
		text := "⚠️ 需要在 Reasonix 桌面端批准操作: " + e.Approval.Tool + " — " + e.Approval.Subject
		text += "\n请回到桌面窗口处理。"
		f.sendToAll(text)

	case event.AskRequest:
		var qb strings.Builder
		qb.WriteString("❓ 需要在 Reasonix 桌面端回答问题:\n")
		for i, q := range e.Ask.Questions {
			if i > 0 {
				qb.WriteString("\n")
			}
			qb.WriteString(q.Prompt)
		}
		qb.WriteString("\n请回到桌面窗口处理。")
		f.sendToAll(qb.String())

	case event.Notice:
		if e.Level == event.LevelWarn {
			f.sendToAll("⚠️ " + e.Text)
		}

	case event.CompactionStarted:
		f.sendToAll("🔄 正在压缩上下文...")
	}
}

// flush sends the accumulated buffer as one message per target channel.
func (f *botEventForwarder) flush() {
	f.mu.Lock()
	text := strings.TrimSpace(f.buf.String())
	if text == "" {
		f.mu.Unlock()
		return
	}
	f.buf.Reset()
	f.mu.Unlock()

	f.sendToAll(text)
}

// sendToAll dispatches text to every target channel. Errors are logged and
// non-fatal; a failed target does not block other targets.
func (f *botEventForwarder) sendToAll(text string) {
	text = strings.TrimSpace(text)
	if f.runtime == nil || len(f.targets) == 0 || text == "" {
		return
	}
	f.queueMu.Lock()
	defer f.queueMu.Unlock()
	if f.closed {
		return
	}
	select {
	case f.queue <- text:
	default:
		log.Printf("[bot-forward] send queue full; dropping message for %d target(s)", len(f.targets))
	}
}

func (f *botEventForwarder) run() {
	for text := range f.queue {
		f.sendToAllNow(text)
	}
}

func (f *botEventForwarder) sendToAllNow(text string) {
	for _, tgt := range f.targets {
		ctx, cancel := context.WithTimeout(context.Background(), botForwardSendTimeout)
		_, err := f.runtime.SendToAdapter(ctx, tgt.ConnID, tgt.Domain, bot.OutboundMessage{
			ChatID:   tgt.ChatID,
			ChatType: tgt.ChatType,
			Text:     text,
		})
		cancel()
		if err != nil {
			log.Printf("[bot-forward] send to %s/%s failed: %v", tgt.ConnID, tgt.ChatType, err)
		}
	}
}

func (f *botEventForwarder) Close() {
	f.closeOnce.Do(func() {
		f.flush()
		f.queueMu.Lock()
		f.closed = true
		close(f.queue)
		f.queueMu.Unlock()
	})
}

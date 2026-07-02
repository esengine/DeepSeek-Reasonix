package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/event"
)

type desktopForwardTestAdapter struct {
	platform bot.Platform
	name     string
	messages chan bot.InboundMessage
	entered  chan struct{}
	release  chan struct{}
	sent     chan bot.OutboundMessage
	once     sync.Once
}

func newDesktopForwardTestAdapter() *desktopForwardTestAdapter {
	return &desktopForwardTestAdapter{
		platform: bot.PlatformFeishu,
		name:     "forward-test",
		messages: make(chan bot.InboundMessage),
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		sent:     make(chan bot.OutboundMessage, 8),
	}
}

func (a *desktopForwardTestAdapter) Platform() bot.Platform { return a.platform }
func (a *desktopForwardTestAdapter) Name() string           { return a.name }
func (a *desktopForwardTestAdapter) Start(context.Context) error {
	return nil
}
func (a *desktopForwardTestAdapter) Stop() error { return nil }
func (a *desktopForwardTestAdapter) SendTyping(context.Context, string) error {
	return nil
}
func (a *desktopForwardTestAdapter) Messages() <-chan bot.InboundMessage {
	return a.messages
}
func (a *desktopForwardTestAdapter) Send(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	a.once.Do(func() { close(a.entered) })
	select {
	case <-a.release:
	case <-ctx.Done():
		return bot.SendResult{}, ctx.Err()
	}
	a.sent <- msg
	return bot.SendResult{MessageID: "sent"}, nil
}

func newDesktopForwardTestRuntime(adapter bot.Adapter) *desktopBotRuntime {
	gw := bot.NewGatewayWithAdapterBindings(bot.GatewayConfig{}, []bot.AdapterBinding{{
		ID:       "feishu-lark",
		Domain:   "lark",
		Platform: bot.PlatformFeishu,
		Adapter:  adapter,
	}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &desktopBotRuntime{gw: gw}
}

func TestBotEventForwarderDoesNotBlockEventEmissionOnSlowSend(t *testing.T) {
	adapter := newDesktopForwardTestAdapter()
	forwarder := newBotEventForwarder(newDesktopForwardTestRuntime(adapter), []botForwardTarget{{
		ConnID:   "feishu-lark",
		Domain:   "lark",
		ChatID:   "oc-group-1",
		ChatType: bot.ChatGroup,
	}})

	done := make(chan struct{})
	go func() {
		forwarder.Emit(event.Event{Kind: event.Text, Text: strings.Repeat("x", 400)})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Emit blocked behind slow bot send")
	}
	select {
	case <-adapter.entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("adapter send did not start")
	}

	forwarder.Close()
	close(adapter.release)
	select {
	case <-adapter.sent:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queued bot message was not sent after adapter release")
	}
}

func TestBotEventForwarderApprovalNoticeDoesNotExposeReplyID(t *testing.T) {
	adapter := newDesktopForwardTestAdapter()
	close(adapter.release)
	forwarder := newBotEventForwarder(newDesktopForwardTestRuntime(adapter), []botForwardTarget{{
		ConnID:   "feishu-lark",
		Domain:   "lark",
		ChatID:   "oc-group-1",
		ChatType: bot.ChatGroup,
	}})

	forwarder.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID:      "approval-1",
		Tool:    "shell",
		Subject: "run command",
	}})
	forwarder.Close()

	select {
	case msg := <-adapter.sent:
		if strings.Contains(msg.Text, "approval-1") || strings.Contains(msg.Text, "/approve") {
			t.Fatalf("approval notice exposed unusable reply routing: %q", msg.Text)
		}
		if !strings.Contains(msg.Text, "桌面") {
			t.Fatalf("approval notice = %q, want desktop guidance", msg.Text)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("approval notice was not sent")
	}
}

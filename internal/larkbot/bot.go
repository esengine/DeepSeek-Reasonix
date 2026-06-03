package larkbot

import (
	"context"
	"log/slog"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkchannel "github.com/larksuite/oapi-sdk-go/v3/channel"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	dispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/larkbot/adapter"
	"reasonix/internal/larkbot/approval"
	"reasonix/internal/larkbot/session"
)

type Options struct {
	AppID     string
	AppSecret string
	LogLevel  larkcore.LogLevel
	Cfg       *config.LarkConfig
}

type Bot struct {
	appID     string
	appSecret string
	logLevel  larkcore.LogLevel
	cfg       *config.LarkConfig

	client        *lark.Client
	wsClient      *larkws.Client
	ch            channeltypes.Channel
	approvalHandler *approval.Handler

	router       *session.Router
	cancelSweep context.CancelFunc
}

func New(opts Options) (*Bot, error) {
	if opts.LogLevel == 0 {
		opts.LogLevel = larkcore.LogLevelInfo
	}

	client := lark.NewClient(opts.AppID, opts.AppSecret, lark.WithLogLevel(opts.LogLevel))
	wsClient := larkws.NewClient(opts.AppID, opts.AppSecret,
		larkws.WithLogLevel(opts.LogLevel),
		larkws.WithEventHandler(dispatcher.NewEventDispatcher("", "")),
	)
	ch := larkchannel.NewChannel(client, wsClient)
	approvalHandler := approval.NewHandler(ch)

	sessionTTL, err := time.ParseDuration(opts.Cfg.ResolvedSessionTTL())
	if err != nil {
		sessionTTL = 1 * time.Hour
	}

	router := session.NewRouter(session.Options{
		GroupPermission: session.PermissionMode(opts.Cfg.ResolvedGroupPermission()),
		DMPermission:    session.PermissionMode(opts.Cfg.ResolvedDMPermission()),
		SessionTTL:      sessionTTL,
		MaxSessions:     opts.Cfg.ResolvedMaxSessions(),
	})

	b := &Bot{
		appID:           opts.AppID,
		appSecret:       opts.AppSecret,
		logLevel:        opts.LogLevel,
		cfg:             opts.Cfg,
		client:          client,
		wsClient:        wsClient,
		ch:              ch,
		approvalHandler: approvalHandler,
		router:          router,
	}

	b.wireLifecycle()
	b.applyPolicy()
	b.wireMessageHandler()
	b.wireCardActionHandler()
	b.wireRejectHandler()

	return b, nil
}

func (b *Bot) wireLifecycle() {
	b.ch.OnReady(func() {
		slog.Info("lark bot ready")
	})

	b.ch.OnError(func(err error) {
		slog.Error("lark bot error", "err", err)
	})

	b.ch.OnReconnecting(func() {
		slog.Info("lark bot reconnecting")
	})

	b.ch.OnReconnected(func() {
		slog.Info("lark bot reconnected")
	})

	b.ch.OnDisconnected(func() {
		slog.Info("lark bot disconnected")
	})
}

func (b *Bot) applyPolicy() {
	requireMention := b.cfg.RequireMention
	respondToMentionAll := b.cfg.RespondToMentionAll

	b.ch.UpdatePolicy(channeltypes.PolicyConfig{
		GroupAllowlist:      b.cfg.AllowGroups,
		DMAllowlist:         b.cfg.AllowDMs,
		RequireMention:      &requireMention,
		RespondToMentionAll: &respondToMentionAll,
	})
}

func (b *Bot) wireCardActionHandler() {
	b.ch.OnCardAction(func(ctx context.Context, ev *channeltypes.CardActionEvent) error {
		return b.approvalHandler.OnCardAction(ctx, ev)
	})
}

func (b *Bot) wireRejectHandler() {
	b.ch.OnReject(func(ctx context.Context, event *channeltypes.RejectEvent) error {
		slog.Warn("lark message rejected", "chat_id", event.ChatID, "reason", event.Reason, "sender_id", event.SenderID)
		return nil
	})
}

func (b *Bot) wireMessageHandler() {
	b.ch.OnMessage(func(ctx context.Context, msg *channeltypes.NormalizedMessage) error {
		content := strings.TrimSpace(msg.Content)
		slog.Info("lark message received", "chat_id", msg.ChatID, "chat_type", msg.ChatType, "user_id", msg.UserID)
		if content == "" {
			return nil
		}

		if strings.HasPrefix(content, "/new") {
			b.router.RemoveAndClose(msg.ChatID)
			_ = b.sendText(msg.ChatID, msg.MessageID, "Session reset. Starting fresh conversation.")
			return nil
		}

		if strings.HasPrefix(content, "/model ") {
			modelRef := strings.TrimSpace(strings.TrimPrefix(content, "/model"))
			if modelRef == "" {
				_ = b.sendText(msg.ChatID, msg.MessageID, "Usage: /model <provider-name>")
				return nil
			}
			if err := b.router.SwitchModel(ctx, msg.ChatID, modelRef); err != nil {
				_ = b.sendText(msg.ChatID, msg.MessageID, "Failed to switch model: "+err.Error())
				return nil
			}
			_ = b.sendText(msg.ChatID, msg.MessageID, "Switched to "+modelRef)
			return nil
		}

		ctrl, sink, err := b.router.GetOrCreate(ctx, msg.ChatID, msg.ChatType)
		if err != nil {
			slog.Error("lark session create", "err", err, "chat_id", msg.ChatID)
			_ = b.sendText(msg.ChatID, msg.MessageID, "Failed to create session: "+err.Error())
			return nil
		}

		ctrl.Send(content)
		go b.pumpEvents(ctrl, sink, msg.ChatID, msg.MessageID)

		return nil
	})
}

func (b *Bot) pumpEvents(ctrl *control.Controller, sink *session.SinkAdapter, chatID, messageID string) {
	adp := adapter.New(b.ch, adapter.Options{
		ChatID:            chatID,
		MessageID:         messageID,
		ShowReasoning:     b.cfg.ShowReasoning,
		ShowToolProgress:  b.cfg.ShowToolProgress,
		MaxResponseLength: b.cfg.ResolvedMaxResponseLength(),
	})

	timeout, err := time.ParseDuration(b.cfg.ResolvedApprovalTimeout())
	if err != nil {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			events, err := sink.WaitForEvent(ctx)
			if err != nil {
				return
			}

			approvalIdx := -1
			askIdx := -1
			for i, ev := range events {
				if ev.Kind == event.ApprovalRequest {
					if ctrl.Bypass() {
						ctrl.Approve(ev.Approval.ID, true, false, false)
						continue
					}
					approvalIdx = i
					break
				}
				if ev.Kind == event.AskRequest {
					askIdx = i
					break
				}
				if ev.Kind == event.TurnDone {
					adp.ProcessEvents(ctx, events[:i+1])
					return
				}
			}

			if approvalIdx >= 0 {
				adp.ProcessEvents(ctx, events[:approvalIdx])
				adp.CloseAndRestart(ctx, "")

				approvalCtx, approvalCancel := context.WithTimeout(ctx, timeout)
				cardMsgID, err := b.approvalHandler.HandleApproval(approvalCtx, ctrl, chatID, events[approvalIdx], timeout)
				approvalCancel()

				if cardMsgID != "" {
					adp.CloseAndRestart(ctx, cardMsgID)
				}

				if err != nil {
					slog.Error("lark approval", "err", err)
				}

				adp.ProcessEvents(ctx, events[approvalIdx+1:])
				continue
			}

			if askIdx >= 0 {
				adp.ProcessEvents(ctx, events[:askIdx])
				adp.CloseAndRestart(ctx, "")

				askCtx, askCancel := context.WithTimeout(ctx, timeout)
				cardMsgID, err := b.approvalHandler.HandleAsk(askCtx, ctrl, chatID, events[askIdx], timeout)
				askCancel()

				if cardMsgID != "" {
					adp.CloseAndRestart(ctx, cardMsgID)
				}

				if err != nil {
					slog.Error("lark ask", "err", err)
				}

				adp.ProcessEvents(ctx, events[askIdx+1:])
				continue
			}

			adp.ProcessEvents(ctx, events)

			for _, ev := range events {
				if ev.Kind == event.TurnDone {
					return
				}
			}
		}
	}()

	<-done
}

func (b *Bot) sendText(chatID, messageID, text string) error {
	_, err := b.ch.Send(context.Background(), &channeltypes.SendInput{
		ChatID:         chatID,
		ReplyMessageID: messageID,
		Text:           text,
	})
	return err
}

func (b *Bot) Run(ctx context.Context) error {
	sweepCtx, cancelSweep := context.WithCancel(ctx)
	b.cancelSweep = cancelSweep
	go b.sweepLoop(sweepCtx)

	if err := b.ch.Start(ctx); err != nil {
		cancelSweep()
		return err
	}

	<-ctx.Done()
	cancelSweep()

	if err := b.ch.Stop(context.Background()); err != nil {
		slog.Warn("lark bot stop", "err", err)
	}
	return nil
}

func (b *Bot) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.router.SweepExpired(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bot) Close() {
	b.router.CloseAll()
	if b.cancelSweep != nil {
		b.cancelSweep()
	}
}

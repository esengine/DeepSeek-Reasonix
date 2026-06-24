// Package qq 实现 QQ 官方 Bot API v2 适配器。
// 参考 Hermes Agent 的 qqbot adapter 实现：
// - app token 获取与刷新
// - WebSocket gateway 连接、heartbeat、resume
// - REST API 回复消息
// - C2C / group / guild / direct message 支持
// - inline keyboard 审批
package qq

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

// New 创建 QQ Bot 适配器。
func New(cfg config.QQBotConfig, logger *slog.Logger) bot.Adapter {
	return &adapter{
		cfg:    cfg,
		logger: logger.With("platform", "qq"),
	}
}

type adapter struct {
	cfg    config.QQBotConfig
	logger *slog.Logger
	msgCh  chan bot.InboundMessage
	cancel context.CancelFunc

	// gateway 状态
	ws          *wsClient
	sessionID   string
	seq         int64
	token       string
	tokenExpiry time.Time
	tokenMu     sync.Mutex

	sendMu             sync.Mutex
	nextOutboundMsgSeq int
	markdownDisabled   bool
}

func (a *adapter) Platform() bot.Platform { return bot.PlatformQQ }
func (a *adapter) Name() string           { return "qq" }

func (a *adapter) Start(ctx context.Context) error {
	a.msgCh = make(chan bot.InboundMessage, 64)
	startupCtx, startupCancel := context.WithTimeout(ctx, qqStartupValidationTimeout)
	defer startupCancel()
	token, err := a.getAccessToken(startupCtx)
	if err != nil {
		return err
	}
	if _, err := a.getGatewayURL(startupCtx, token); err != nil {
		return err
	}
	ctx, a.cancel = context.WithCancel(ctx)

	go a.gatewayLoop(ctx)
	return nil
}

func (a *adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func (a *adapter) Send(ctx context.Context, msg bot.OutboundMessage) (bot.SendResult, error) {
	return a.sendMessage(ctx, msg)
}

func (a *adapter) SendTyping(ctx context.Context, chatID string) error {
	return nil // QQ Bot 暂不支持 typing 指示器
}

func (a *adapter) Messages() <-chan bot.InboundMessage {
	return a.msgCh
}

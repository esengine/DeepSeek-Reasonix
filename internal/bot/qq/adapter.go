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

	"golang.org/x/net/websocket"
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
	loopWG sync.WaitGroup

	// gateway 状态
	connMu      sync.Mutex
	conn        *websocket.Conn // live gateway connection, closed by Stop to unblock reads
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

	a.loopWG.Add(1)
	go func() {
		defer a.loopWG.Done()
		a.gatewayLoop(ctx)
	}()
	return nil
}

// Stop 取消 gateway context、关闭当前 WebSocket 连接并等待 gatewayLoop 退出。
// websocket 的阻塞读不响应 context，只有关闭连接才能解除阻塞；不等待就返回
// 会在宿主重建 bot runtime 后留下仍占用 QQ gateway session 的僵尸连接。
func (a *adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	a.closeConn()
	a.loopWG.Wait()
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

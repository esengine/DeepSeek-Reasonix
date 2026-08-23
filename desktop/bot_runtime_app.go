package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/botruntime"
	"reasonix/internal/config"
)

type BotRuntimeStatusView struct {
	Running     bool              `json:"running"`
	Status      string            `json:"status"`
	Message     string            `json:"message"`
	Connections int               `json:"connections"`
	StartedAt   string            `json:"startedAt"`
	Platforms   map[string]string `json:"platforms,omitempty"`
}

type desktopBotRuntime struct {
	// lifecycleMu serializes start/stop transitions so two apply/stop calls
	// can't race a gateway into existence. The slow work (gw.Stop teardown,
	// gw.Start dials) runs while holding it but NOT r.mu, so status/send reads
	// never block on a restart.
	lifecycleMu sync.Mutex
	mu          sync.Mutex
	cancel      context.CancelFunc
	gw          *bot.BotGateway
	status      BotRuntimeStatusView
}

func newDesktopBotRuntime() *desktopBotRuntime {
	return &desktopBotRuntime{status: BotRuntimeStatusView{Status: "stopped", Message: "bot runtime is not started"}}
}

func desktopBotChannelsWithLegacyQQ(qq config.QQBotConfig, channels map[bot.Platform]bot.ChannelConfig, connectionChannels map[string]bot.ChannelConfig) (map[bot.Platform]bot.ChannelConfig, map[string]bot.ChannelConfig) {
	channel := bot.ChannelConfig{
		Model:            strings.TrimSpace(qq.Model),
		ToolApprovalMode: normalizeBotConnectionToolApprovalMode(qq.ToolApprovalMode),
		WorkspaceRoot:    strings.TrimSpace(qq.WorkspaceRoot),
	}
	if channel.Model == "" && channel.ToolApprovalMode == "" && channel.WorkspaceRoot == "" {
		return channels, connectionChannels
	}
	if channels == nil {
		channels = make(map[bot.Platform]bot.ChannelConfig)
	}
	if _, ok := channels[bot.PlatformQQ]; !ok {
		channels[bot.PlatformQQ] = channel
	}
	if connectionChannels == nil {
		connectionChannels = make(map[string]bot.ChannelConfig)
	}
	if _, ok := connectionChannels[string(bot.PlatformQQ)]; !ok {
		connectionChannels[string(bot.PlatformQQ)] = channel
	}
	return channels, connectionChannels
}

// desktopBotChannelsWithLegacyDingtalk 把 legacy [bot.dingtalk] 的模型/权限/
// 工作目录合成进 Channels 与 ConnectionChannels，使直配（无 connection）的
// 钉钉 bot 也能从设置面板配置这些运行选项（与 legacy QQ 同路径）。
func desktopBotChannelsWithLegacyDingtalk(dt config.DingtalkBotConfig, channels map[bot.Platform]bot.ChannelConfig, connectionChannels map[string]bot.ChannelConfig) (map[bot.Platform]bot.ChannelConfig, map[string]bot.ChannelConfig) {
	channel := bot.ChannelConfig{
		Model:            strings.TrimSpace(dt.Model),
		ToolApprovalMode: normalizeBotConnectionToolApprovalMode(dt.ToolApprovalMode),
		WorkspaceRoot:    strings.TrimSpace(dt.WorkspaceRoot),
		SessionMappings:  botruntime.SessionMappings(dt.SessionMappings),
	}
	if channel.Model == "" && channel.ToolApprovalMode == "" && channel.WorkspaceRoot == "" && len(channel.SessionMappings) == 0 {
		return channels, connectionChannels
	}
	if channels == nil {
		channels = make(map[bot.Platform]bot.ChannelConfig)
	}
	if _, ok := channels[bot.PlatformDingtalk]; !ok {
		channels[bot.PlatformDingtalk] = channel
	}
	if connectionChannels == nil {
		connectionChannels = make(map[string]bot.ChannelConfig)
	}
	if _, ok := connectionChannels[string(bot.PlatformDingtalk)]; !ok {
		connectionChannels[string(bot.PlatformDingtalk)] = channel
	}
	return channels, connectionChannels
}

func (a *App) refreshBotRuntimeAsync() {
	if a.ctx == nil {
		return
	}
	a.goSafe("refreshBotRuntime", a.refreshBotRuntime)
}

func (a *App) refreshBotRuntime() {
	// NewApp always pre-fills botRuntime; a nil here means a test-constructed
	// App with no bot runtime, which must not lazily create one from a
	// background goroutine (that would race a concurrent refresh).
	if a.botRuntime == nil {
		return
	}
	var watcherVersion uint64
	if a.botBridge != nil {
		watcherVersion = a.botBridge.watcherVersion()
	}
	cfg, err := a.loadDesktopBotConfig()
	if err != nil {
		a.botRuntime.stop("error", err.Error())
		return
	}
	// Assign through a typed local so a nil *botBridgeHub never becomes a
	// non-nil bot.DesktopBridge interface inside the gateway config.
	var bridge bot.DesktopBridge
	if a.botBridge != nil {
		// 配置是订阅的持久化事实源：每次运行时重算前重新种子，桌面重启后
		// /desktop watch 的订阅继续生效。
		a.botBridge.seedWatchers(bridgeRoutesFromConfig(cfg.Bot.DesktopWatchers), watcherVersion)
		bridge = a.botBridge
	}
	_ = a.botRuntime.apply(a.bootContext(), cfg, globalTabWorkspaceRoot(), a.persistRemoteBotToolApprovalMode, bridge)
}

func (a *App) loadDesktopBotConfig() (*config.Config, error) {
	// Read-only load feeding the bot runtime and connection diagnostics. It
	// must load credentials: the runtime resolves app secrets and control
	// tokens from the process env (AppSecretEnv, Control.TokenEnv), which the
	// credential-free view load would leave unset on a fresh process.
	cfg, _, err := a.loadDesktopUserConfigForViewWithCredentials()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (a *App) stopBotRuntime() {
	if a.botRuntime != nil {
		a.botRuntime.stop("stopped", "bot runtime stopped")
	}
}

func (a *App) BotRuntimeStatus() BotRuntimeStatusView {
	if a.botRuntime == nil {
		return BotRuntimeStatusView{Status: "stopped", Message: "bot runtime is not started"}
	}
	return a.botRuntime.snapshot()
}

func (r *desktopBotRuntime) apply(parent context.Context, cfg *config.Config, workspaceRoot string, onToolApprovalModeChange func(bot.InboundMessage, string) error, bridge bot.DesktopBridge) error {
	if r == nil {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	plan := desktopBotRuntimePlan(cfg)
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.stopCurrent()
	if !plan.Start {
		r.setStatus(BotRuntimeStatusView{Status: plan.Status, Message: plan.Message})
		return nil
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, cancel := context.WithCancel(parent)
	modelName := botruntime.ModelName(cfg, "")
	channels := botruntime.ChannelConfigs(cfg.Bot.Connections, true, true)
	connectionChannels := botruntime.ConnectionChannelConfigs(cfg.Bot.Connections, true, true)
	channels, connectionChannels = desktopBotChannelsWithLegacyQQ(cfg.Bot.QQ, channels, connectionChannels)
	channels, connectionChannels = desktopBotChannelsWithLegacyDingtalk(cfg.Bot.Dingtalk, channels, connectionChannels)
	gwCfg := bot.GatewayConfig{
		Model:              modelName,
		ToolApprovalMode:   cfg.Bot.ToolApprovalMode,
		MaxSteps:           cfg.Bot.MaxSteps,
		QueueMode:          cfg.Bot.QueueMode,
		QueueCap:           cfg.Bot.QueueCap,
		QueueDrop:          cfg.Bot.QueueDrop,
		PairingEnabled:     cfg.Bot.Pairing.Enabled,
		PairingTTL:         time.Duration(cfg.Bot.Pairing.RequestTTLMinutes) * time.Minute,
		PairingMaxPending:  cfg.Bot.Pairing.MaxPendingPerPlatform,
		IgnoreSelfMessages: cfg.Bot.IgnoreSelfMessages,
		SelfUserIDs: map[bot.Platform][]string{
			bot.PlatformQQ:       cfg.Bot.SelfUserIDs.QQ,
			bot.PlatformFeishu:   cfg.Bot.SelfUserIDs.Feishu,
			bot.PlatformWeixin:   cfg.Bot.SelfUserIDs.Weixin,
			bot.PlatformDingtalk: cfg.Bot.SelfUserIDs.Dingtalk,
		},
		ControlEnabled:     cfg.Bot.Control.Enabled,
		ControlAddr:        cfg.Bot.Control.Addr,
		ControlToken:       os.Getenv(strings.TrimSpace(cfg.Bot.Control.TokenEnv)),
		WorkspaceRoot:      workspaceRoot,
		Channels:           channels,
		ConnectionChannels: connectionChannels,
		Routes:             botruntime.RouteConfigs(cfg.Bot.Routes, true, true),
		ConnectionAccess:   botruntime.ConnectionAccessConfigs(cfg),
		Enabled:            plan.Enabled,
		Allowlist: bot.AllowlistConfig{
			Enabled:  cfg.Bot.Allowlist.Enabled,
			AllowAll: cfg.Bot.Allowlist.AllowAll,
			Users: map[bot.Platform][]string{
				bot.PlatformQQ:       cfg.Bot.Allowlist.QQUsers,
				bot.PlatformFeishu:   cfg.Bot.Allowlist.FeishuUsers,
				bot.PlatformWeixin:   cfg.Bot.Allowlist.WeixinUsers,
				bot.PlatformDingtalk: cfg.Bot.Allowlist.DingtalkUsers,
			},
			Approvers: map[bot.Platform][]string{
				bot.PlatformQQ:       cfg.Bot.Allowlist.QQApprovers,
				bot.PlatformFeishu:   cfg.Bot.Allowlist.FeishuApprovers,
				bot.PlatformWeixin:   cfg.Bot.Allowlist.WeixinApprovers,
				bot.PlatformDingtalk: cfg.Bot.Allowlist.DingtalkApprovers,
			},
			Admins: map[bot.Platform][]string{
				bot.PlatformQQ:       cfg.Bot.Allowlist.QQAdmins,
				bot.PlatformFeishu:   cfg.Bot.Allowlist.FeishuAdmins,
				bot.PlatformWeixin:   cfg.Bot.Allowlist.WeixinAdmins,
				bot.PlatformDingtalk: cfg.Bot.Allowlist.DingtalkAdmins,
			},
			Groups: map[bot.Platform][]string{
				bot.PlatformQQ:       cfg.Bot.Allowlist.QQGroups,
				bot.PlatformFeishu:   cfg.Bot.Allowlist.FeishuGroups,
				bot.PlatformWeixin:   cfg.Bot.Allowlist.WeixinGroups,
				bot.PlatformDingtalk: cfg.Bot.Allowlist.DingtalkGroups,
			},
		},
		Debounce:                 time.Duration(cfg.Bot.DebounceMs) * time.Millisecond,
		ModelResolver:            botruntime.ModelResolver(cfg),
		OnInbound:                botruntime.NewRemoteRememberer(logger),
		OnSessionReady:           botruntime.NewSessionRemembererWithWorkspace(logger, workspaceRoot),
		OnToolApprovalModeChange: onToolApprovalModeChange,
		Desktop:                  bridge,
	}
	bindings := botruntime.AdapterBindings(cfg, plan.Enabled, nil, logger)
	if len(bindings) == 0 {
		cancel()
		r.setStatus(BotRuntimeStatusView{Status: "stopped", Message: "no bot adapters configured"})
		return nil
	}
	gw := bot.NewGatewayWithAdapterBindings(gwCfg, bindings, logger)
	if err := gw.Start(ctx); err != nil {
		cancel()
		gw.Stop()
		r.setStatus(BotRuntimeStatusView{Status: "error", Message: err.Error(), Connections: gw.AdapterCount()})
		return err
	}
	runningConnections := gw.AdapterCount()
	startErrors := gw.StartErrors()
	status := "running"
	message := fmt.Sprintf("%d bot connection(s) running", runningConnections)
	if len(startErrors) > 0 {
		status = "degraded"
		message = fmt.Sprintf("%d bot connection(s) running; %d failed to start: %s", runningConnections, len(startErrors), summarizeBotRuntimeErrors(startErrors))
	}
	r.mu.Lock()
	r.cancel = cancel
	r.gw = gw
	r.status = BotRuntimeStatusView{
		Running:     true,
		Status:      status,
		Message:     message,
		Connections: runningConnections,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	r.mu.Unlock()
	return nil
}

func (a *App) persistRemoteBotToolApprovalMode(msg bot.InboundMessage, mode string) error {
	mode = normalizeBotConnectionToolApprovalMode(mode)
	if mode == "" {
		return nil
	}
	return a.applyConfigOnly(func(c *config.Config) error {
		id := strings.TrimSpace(msg.ConnectionID)
		now := time.Now().UTC().Format(time.RFC3339)
		if id != "" {
			for i := range c.Bot.Connections {
				if c.Bot.Connections[i].ID == id || botruntime.ConnectionRuntimeID(c.Bot.Connections[i]) == id {
					c.Bot.Connections[i].ToolApprovalMode = mode
					c.Bot.Connections[i].UpdatedAt = now
					return nil
				}
			}
		}
		c.Bot.ToolApprovalMode = mode
		return nil
	})
}

func summarizeBotRuntimeErrors(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		parts = append(parts, err.Error())
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 3 {
		hidden := len(parts) - 3
		parts = append(parts[:3], fmt.Sprintf("%d more", hidden))
	}
	return strings.Join(parts, "; ")
}

type botRuntimePlan struct {
	Start   bool
	Status  string
	Message string
	Enabled map[bot.Platform]bool
}

func desktopBotRuntimePlan(cfg *config.Config) botRuntimePlan {
	if cfg == nil {
		return botRuntimePlan{Status: "error", Message: "config is unavailable"}
	}
	if !cfg.Bot.Enabled {
		return botRuntimePlan{Status: "stopped", Message: "bot is disabled"}
	}
	if !botruntime.BotConfigHasAccessControl(cfg.Bot) {
		return botRuntimePlan{Status: "blocked", Message: "bot requires an allowlist, pairing, per-bot access, or allow_all=true"}
	}
	enabled, unknown := botruntime.EnabledPlatforms(cfg, nil)
	if len(unknown) > 0 {
		return botRuntimePlan{Status: "error", Message: "unknown bot channel: " + strings.Join(unknown, ", ")}
	}
	if !botruntime.HasEnabledPlatform(enabled) {
		return botRuntimePlan{Status: "stopped", Message: "no bot channels enabled"}
	}
	return botRuntimePlan{Start: true, Status: "running", Message: "bot runtime can start", Enabled: enabled}
}

func (r *desktopBotRuntime) stop(status, message string) {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.stopCurrent()
	r.setStatus(BotRuntimeStatusView{Status: status, Message: message})
}

// stopCurrent detaches the running gateway under r.mu, then tears it down
// off-lock: gw.Stop() closes every session controller (up to the jobs teardown
// grace each) and must not stall status/send readers. Callers hold lifecycleMu.
func (r *desktopBotRuntime) stopCurrent() {
	r.mu.Lock()
	cancel := r.cancel
	gw := r.gw
	r.cancel = nil
	r.gw = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if gw != nil {
		gw.Stop()
	}
}

func (r *desktopBotRuntime) setStatus(status BotRuntimeStatusView) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
}

func (r *desktopBotRuntime) snapshot() BotRuntimeStatusView {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.status
	if r.gw != nil {
		s.Platforms = botAdapterPlatformStatuses(r.gw.AdapterHealth())
	}
	return s
}

// botAdapterPlatformStatuses 把 gateway 的适配器健康快照收敛为
// platform → status 映射（如 dingtalk → running），供设置面板显示在线状态。
func botAdapterPlatformStatuses(health []bot.AdapterHealthSnapshot) map[string]string {
	out := make(map[string]string, len(health))
	for _, h := range health {
		if strings.TrimSpace(string(h.Platform)) != "" {
			out[string(h.Platform)] = h.Status
		}
	}
	return out
}

// updateConnectionToolApprovalMode updates a connection's tool approval mode
// on the running gateway without restarting. Returns true if updated, false if
// the gateway is not running or the connection is unknown.
func (r *desktopBotRuntime) updateConnectionToolApprovalMode(connID, mode string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gw == nil {
		return false
	}
	mode = normalizeBotConnectionToolApprovalMode(mode)
	// Update ConnectionChannels in the internal GatewayConfig so new sessions
	// pick up the mode. Existing sessions are updated by the gateway directly.
	r.gw.UpdateConnectionToolApprovalMode(connID, mode)
	return true
}

// SendToAdapter sends a message through the running gateway's adapter
// identified by connID. Returns an error if the gateway is not running
// or no matching adapter is found.
func (r *desktopBotRuntime) SendToAdapter(ctx context.Context, connID, domain string, msg bot.OutboundMessage) (bot.SendResult, error) {
	r.mu.Lock()
	gw := r.gw
	r.mu.Unlock()
	if gw == nil {
		return bot.SendResult{}, nil // gateway not running — silent no-op
	}
	return gw.SendToAdapter(ctx, connID, domain, msg)
}

// TestSendToAdapter sends a test message through the running gateway's adapter
// identified by connID. The adapter must implement bot.TestSender (dingtalk).
func (r *desktopBotRuntime) TestSendToAdapter(ctx context.Context, connID, domain, text string) (bot.SendResult, error) {
	r.mu.Lock()
	gw := r.gw
	r.mu.Unlock()
	if gw == nil {
		return bot.SendResult{}, fmt.Errorf("bot runtime is not running")
	}
	return gw.TestSendToAdapter(ctx, connID, domain, text)
}

// Running returns true if the bot gateway is currently active.
func (r *desktopBotRuntime) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gw != nil
}

// ForwardTargets returns the list of bot forward targets derived from the
// current config's bot connections and their session mappings. Each mapping
// produces one target (connID + chatID + chatType) for event forwarding.
func (r *desktopBotRuntime) ForwardTargets(cfg *config.Config) []botForwardTarget {
	if cfg == nil {
		return nil
	}
	var targets []botForwardTarget
	seen := make(map[botForwardTarget]bool)
	for _, conn := range cfg.Bot.Connections {
		if !conn.Enabled {
			continue
		}
		connID := botruntime.ConnectionRuntimeID(conn)
		domain := strings.TrimSpace(conn.Domain)
		for _, sm := range conn.SessionMappings {
			remoteID := strings.TrimSpace(sm.RemoteID)
			if remoteID == "" {
				continue
			}
			chatType := bot.ChatDM
			if sm.ChatType != "" {
				chatType = bot.ChatType(sm.ChatType)
			}
			target := botForwardTarget{
				ConnID:   connID,
				Domain:   domain,
				ChatID:   remoteID,
				ChatType: chatType,
			}
			if seen[target] {
				continue
			}
			seen[target] = true
			targets = append(targets, target)
		}
	}
	return targets
}

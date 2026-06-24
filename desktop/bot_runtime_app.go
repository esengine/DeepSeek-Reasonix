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
	Running     bool   `json:"running"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Connections int    `json:"connections"`
	StartedAt   string `json:"startedAt"`
}

type desktopBotRuntime struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	gw     *bot.BotGateway
	status BotRuntimeStatusView
}

func newDesktopBotRuntime() *desktopBotRuntime {
	return &desktopBotRuntime{status: BotRuntimeStatusView{Status: "stopped", Message: "bot runtime is not started"}}
}

func (a *App) refreshBotRuntimeAsync() {
	if a.ctx == nil {
		return
	}
	a.goSafe("refreshBotRuntime", a.refreshBotRuntime)
}

func (a *App) refreshBotRuntime() {
	if a.botRuntime == nil {
		a.botRuntime = newDesktopBotRuntime()
	}
	cfg, err := a.loadDesktopBotConfig()
	if err != nil {
		a.botRuntime.stop("error", err.Error())
		return
	}
	_ = a.botRuntime.apply(a.bootContext(), cfg, globalTabWorkspaceRoot(), a.persistRemoteBotToolApprovalMode)
}

func (a *App) loadDesktopBotConfig() (*config.Config, error) {
	cfg, _, err := a.loadDesktopUserConfigForEdit()
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

func (r *desktopBotRuntime) apply(parent context.Context, cfg *config.Config, workspaceRoot string, onToolApprovalModeChange func(bot.InboundMessage, string) error) error {
	if r == nil {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	plan := desktopBotRuntimePlan(cfg)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
	if !plan.Start {
		r.status = BotRuntimeStatusView{Status: plan.Status, Message: plan.Message}
		return nil
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, cancel := context.WithCancel(parent)
	modelName := botruntime.ModelName(cfg, "")
	gwCfg := bot.GatewayConfig{
		Model:              modelName,
		ToolApprovalMode:   cfg.Bot.ToolApprovalMode,
		MaxSteps:           cfg.Bot.MaxSteps,
		WorkspaceRoot:      workspaceRoot,
		Channels:           botruntime.ChannelConfigs(cfg.Bot.Connections, true, true),
		ConnectionChannels: botruntime.ConnectionChannelConfigs(cfg.Bot.Connections, true, true),
		Enabled:            plan.Enabled,
		Allowlist: bot.AllowlistConfig{
			Enabled:  cfg.Bot.Allowlist.Enabled,
			AllowAll: cfg.Bot.Allowlist.AllowAll,
			Users: map[bot.Platform][]string{
				bot.PlatformQQ:     cfg.Bot.Allowlist.QQUsers,
				bot.PlatformFeishu: cfg.Bot.Allowlist.FeishuUsers,
				bot.PlatformWeixin: cfg.Bot.Allowlist.WeixinUsers,
			},
			Groups: map[bot.Platform][]string{
				bot.PlatformQQ:     cfg.Bot.Allowlist.QQGroups,
				bot.PlatformFeishu: cfg.Bot.Allowlist.FeishuGroups,
				bot.PlatformWeixin: cfg.Bot.Allowlist.WeixinGroups,
			},
		},
		Debounce:                 time.Duration(cfg.Bot.DebounceMs) * time.Millisecond,
		OnInbound:                botruntime.NewRemoteRememberer(logger),
		OnSessionReady:           botruntime.NewSessionRemembererWithWorkspace(logger, workspaceRoot),
		OnToolApprovalModeChange: onToolApprovalModeChange,
	}
	bindings := botruntime.AdapterBindings(cfg, plan.Enabled, nil, logger)
	if len(bindings) == 0 {
		cancel()
		r.status = BotRuntimeStatusView{Status: "stopped", Message: "no bot adapters configured"}
		return nil
	}
	gw := bot.NewGatewayWithAdapterBindings(gwCfg, bindings, logger)
	if err := gw.Start(ctx); err != nil {
		cancel()
		gw.Stop()
		r.status = BotRuntimeStatusView{Status: "error", Message: err.Error(), Connections: gw.AdapterCount()}
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
	r.cancel = cancel
	r.gw = gw
	r.status = BotRuntimeStatusView{
		Running:     true,
		Status:      status,
		Message:     message,
		Connections: runningConnections,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
	}
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
	if !cfg.Bot.Allowlist.AllowAll && (!cfg.Bot.Allowlist.Enabled || botruntime.AllowlistUserCount(cfg.Bot.Allowlist) == 0) {
		return botRuntimePlan{Status: "blocked", Message: "bot requires an allowlist or allow_all=true"}
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
	r.status = BotRuntimeStatusView{Status: status, Message: message}
}

func (r *desktopBotRuntime) stopLocked() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.gw != nil {
		r.gw.Stop()
		r.gw = nil
	}
}

func (r *desktopBotRuntime) snapshot() BotRuntimeStatusView {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

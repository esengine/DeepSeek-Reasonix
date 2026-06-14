package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/bot"
	"reasonix/internal/bot/weixin"
	"reasonix/internal/botruntime"
	"reasonix/internal/config"
)

func botCommand(args []string, version string) int {
	if len(args) < 1 {
		botUsage()
		return 2
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "start":
		return botStart(rest, version)
	case "doctor":
		return botDoctor(rest)
	case "weixin-login":
		return botWeixinLogin(rest)
	case "help", "--help", "-h":
		botUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown bot subcommand %q\n\n", sub)
		botUsage()
		return 2
	}
}

func botStart(args []string, version string) int {
	fs := flag.NewFlagSet("bot start", flag.ContinueOnError)
	channels := fs.String("channels", "", "启用的平台，逗号分隔：qq,feishu,lark,weixin")
	dir := fs.String("dir", "", "工作目录")
	model := fs.String("model", "", "模型名（空则用 default_model）")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := loadBotCommandConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		return 1
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	prepared, err := prepareBotGateway(cfg, botGatewayOptions{
		Channels:      *channels,
		WorkspaceRoot: *dir,
		Model:         *model,
	}, logger, func(format string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, "warning: "+format+"\n", args...)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	gw := prepared.Gateway

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nshutting down...")
		cancel()
		gw.Stop()
	}()

	fmt.Fprintf(os.Stderr, "reasonix bot starting (model: %s, channels: %s)...\n", prepared.Model, prepared.ChannelSummary)
	fmt.Fprintf(os.Stderr, "version: %s\n", version)

	if err := gw.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: start gateway: %v\n", err)
		return 1
	}

	// 等待信号或 context 取消
	<-ctx.Done()
	return 0
}

type botGatewayOptions struct {
	Channels      string
	WorkspaceRoot string
	Model         string
}

type preparedBotGateway struct {
	Gateway        *bot.BotGateway
	Model          string
	ChannelSummary string
}

func prepareBotGateway(cfg *config.Config, opts botGatewayOptions, logger *slog.Logger, warnf func(string, ...interface{})) (*preparedBotGateway, error) {
	if cfg == nil {
		return nil, fmt.Errorf("bot config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if !cfg.Bot.Enabled {
		return nil, fmt.Errorf("bot is not enabled in config — set [bot] enabled = true")
	}
	if !cfg.Bot.Allowlist.AllowAll && (!cfg.Bot.Allowlist.Enabled || botruntime.AllowlistUserCount(cfg.Bot.Allowlist) == 0) {
		return nil, fmt.Errorf("bot requires an explicit allowlist; set [bot.allowlist] enabled = true with platform user ids, or set allow_all = true intentionally")
	}

	workspaceRoot := opts.WorkspaceRoot
	if workspaceRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			workspaceRoot = wd
		}
	}

	requestedChannels := splitBotChannels(opts.Channels)
	enabledPlatforms, unknownChannels := botruntime.EnabledPlatforms(cfg, requestedChannels)
	for _, ch := range unknownChannels {
		if warnf != nil {
			warnf("unknown channel %q", ch)
		}
	}
	if !botruntime.HasEnabledPlatform(enabledPlatforms) {
		return nil, fmt.Errorf("no bot channels enabled — enable at least one in config")
	}

	modelName := botruntime.ModelName(cfg, opts.Model)

	rememberInboundRemote := botruntime.NewRemoteRememberer(logger)
	sessionSearchDirs := botSessionSearchDirs(workspaceRoot)

	// 构建网关配置
	gwCfg := bot.GatewayConfig{
		Model:              modelName,
		ToolApprovalMode:   cfg.Bot.ToolApprovalMode,
		MaxSteps:           cfg.Bot.MaxSteps,
		WorkspaceRoot:      workspaceRoot,
		SessionDir:         botPrimarySessionDir(workspaceRoot),
		SessionSearchDirs:  sessionSearchDirs,
		SessionMappingPath: botSessionMappingPath(),
		SessionMappings:    botStaticSessionMappings(cfg.Bot.Connections, sessionSearchDirs),
		Channels:           botruntime.ChannelConfigs(cfg.Bot.Connections, strings.TrimSpace(opts.Model) == "", strings.TrimSpace(opts.WorkspaceRoot) == ""),
		ConnectionChannels: botruntime.ConnectionChannelConfigs(cfg.Bot.Connections, strings.TrimSpace(opts.Model) == "", strings.TrimSpace(opts.WorkspaceRoot) == ""),
		Enabled:            enabledPlatforms,
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
		Debounce:  time.Duration(cfg.Bot.DebounceMs) * time.Millisecond,
		OnInbound: rememberInboundRemote,
	}

	feishuDomains := botruntime.RequestedFeishuDomains(requestedChannels)
	gw := bot.NewGatewayWithAdapterBindings(gwCfg, botruntime.AdapterBindings(cfg, enabledPlatforms, feishuDomains, logger), logger)
	return &preparedBotGateway{
		Gateway:        gw,
		Model:          modelName,
		ChannelSummary: botChannelSummaryFromRequest(opts.Channels, enabledPlatforms),
	}, nil
}

func resolveBotEnabledPlatforms(cfg config.BotConfig, channels string, warnf func(string, ...interface{})) map[bot.Platform]bool {
	enabledPlatforms := make(map[bot.Platform]bool)
	if strings.TrimSpace(channels) != "" {
		for _, ch := range strings.Split(channels, ",") {
			ch = strings.TrimSpace(ch)
			switch bot.Platform(ch) {
			case bot.PlatformQQ:
				enabledPlatforms[bot.PlatformQQ] = cfg.QQ.Enabled
			case bot.PlatformFeishu:
				enabledPlatforms[bot.PlatformFeishu] = cfg.Feishu.Enabled
			case bot.PlatformWeixin:
				enabledPlatforms[bot.PlatformWeixin] = cfg.Weixin.Enabled
			default:
				if warnf != nil {
					warnf("unknown channel %q", ch)
				}
			}
		}
		return enabledPlatforms
	}
	enabledPlatforms[bot.PlatformQQ] = cfg.QQ.Enabled
	enabledPlatforms[bot.PlatformFeishu] = cfg.Feishu.Enabled
	enabledPlatforms[bot.PlatformWeixin] = cfg.Weixin.Enabled
	return enabledPlatforms
}

func hasEnabledBotPlatform(enabled map[bot.Platform]bool) bool {
	for _, v := range enabled {
		if v {
			return true
		}
	}
	return false
}

func botChannelSummary(enabled map[bot.Platform]bool) string {
	var names []string
	for _, plat := range []bot.Platform{bot.PlatformQQ, bot.PlatformFeishu, bot.PlatformWeixin} {
		if enabled[plat] {
			names = append(names, string(plat))
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ",")
}

func botChannelSummaryFromRequest(channels string, enabled map[bot.Platform]bool) string {
	if trimmed := strings.TrimSpace(channels); trimmed != "" {
		return trimmed
	}
	return botChannelSummary(enabled)
}

func botSessionMappingPath() string {
	root := config.MemoryUserDir()
	if strings.TrimSpace(root) == "" {
		return ""
	}
	return filepath.Join(root, "bot-session-mappings.json")
}

func botPrimarySessionDir(workspaceRoot string) string {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot != "" {
		if dir := config.ProjectSessionDir(workspaceRoot); dir != "" {
			return dir
		}
	}
	return config.SessionDir()
}

func botSessionSearchDirs(workspaceRoot string) []string {
	var dirs []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		for _, existing := range dirs {
			if existing == dir {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	add(botPrimarySessionDir(workspaceRoot))
	add(config.SessionDir())
	return dirs
}

func botStaticSessionMappings(connections []config.BotConnectionConfig, dirs []string) []bot.SessionMapping {
	var out []bot.SessionMapping
	for _, conn := range connections {
		if !conn.Enabled {
			continue
		}
		searchDirs := append([]string(nil), dirs...)
		if conn.WorkspaceRoot != "" {
			searchDirs = append(searchDirs, botSessionSearchDirs(conn.WorkspaceRoot)...)
		}
		for _, mapping := range conn.SessionMappings {
			remoteID := strings.TrimSpace(mapping.RemoteID)
			sessionID := strings.TrimSpace(mapping.SessionID)
			if remoteID == "" || sessionID == "" {
				continue
			}
			if mapping.WorkspaceRoot != "" {
				searchDirs = append(searchDirs, botSessionSearchDirs(mapping.WorkspaceRoot)...)
			}
			path := resolveBotSessionPath(sessionID, searchDirs)
			if path == "" {
				continue
			}
			out = append(out, bot.SessionMapping{
				RemoteKey:     remoteID,
				SessionPath:   path,
				SessionID:     sessionID,
				WorkspaceRoot: firstNonEmptyString(mapping.WorkspaceRoot, conn.WorkspaceRoot),
			})
		}
	}
	return out
}

func resolveBotSessionPath(sessionID string, dirs []string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	if filepath.IsAbs(sessionID) || strings.Contains(sessionID, string(filepath.Separator)) {
		if _, err := os.Stat(sessionID); err == nil {
			return sessionID
		}
		return ""
	}
	for _, dir := range dirs {
		sessions, err := agent.ListSessions(dir)
		if err != nil {
			continue
		}
		for _, session := range sessions {
			id := agent.BranchID(session.Path)
			base := strings.TrimSuffix(filepath.Base(session.Path), filepath.Ext(session.Path))
			if id == sessionID || strings.HasPrefix(id, sessionID) || base == sessionID || strings.HasPrefix(base, sessionID) {
				return session.Path
			}
		}
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func splitBotChannels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func botDoctor(args []string) int {
	fs := flag.NewFlagSet("bot doctor", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadBotCommandConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		return 1
	}

	bc := cfg.Bot

	type checkResult struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}

	var results []checkResult

	addCheck := func(name, status, detail string) {
		results = append(results, checkResult{Name: name, Status: status, Detail: detail})
	}

	// 基础检查
	if bc.Enabled {
		addCheck("bot.enabled", "ok", "")
	} else {
		addCheck("bot.enabled", "disabled", "bot is not enabled in config")
	}

	// QQ 检查
	if bc.QQ.Enabled {
		addCheck("bot.qq.enabled", "ok", "")
		secret := os.Getenv(bc.QQ.AppSecretEnv)
		if secret == "" {
			addCheck("bot.qq.app_secret", "missing", bc.QQ.AppSecretEnv+" is not set")
		} else {
			addCheck("bot.qq.app_secret", "ok", bc.QQ.AppSecretEnv+" is set")
		}
		if bc.QQ.AppID == "" {
			addCheck("bot.qq.app_id", "missing", "app_id is empty")
		} else {
			addCheck("bot.qq.app_id", "ok", "app_id configured")
		}
	} else {
		addCheck("bot.qq", "disabled", "")
	}

	// 飞书检查
	if bc.Feishu.Enabled {
		addCheck("bot.feishu.enabled", "ok", "")
		secret := os.Getenv(bc.Feishu.AppSecretEnv)
		if secret == "" {
			addCheck("bot.feishu.app_secret", "missing", bc.Feishu.AppSecretEnv+" is not set")
		} else {
			addCheck("bot.feishu.app_secret", "ok", bc.Feishu.AppSecretEnv+" is set")
		}
		if bc.Feishu.AppID == "" {
			addCheck("bot.feishu.app_id", "missing", "app_id is empty")
		} else {
			addCheck("bot.feishu.app_id", "ok", "app_id configured")
		}
		mode := bc.Feishu.Mode
		if mode == "" {
			mode = "webhook"
		}
		addCheck("bot.feishu.mode", "ok", mode)
	} else {
		addCheck("bot.feishu", "disabled", "")
	}

	// 微信检查
	if bc.Weixin.Enabled {
		addCheck("bot.weixin.enabled", "ok", "")
		token := os.Getenv(bc.Weixin.TokenEnv)
		if token != "" {
			addCheck("bot.weixin.token", "ok", bc.Weixin.TokenEnv+" is set")
		} else if weixin.HasSavedAccount(bc.Weixin.AccountID) {
			addCheck("bot.weixin.token", "ok", "saved iLink account is available")
		} else {
			addCheck("bot.weixin.token", "missing", bc.Weixin.TokenEnv+" is not set; run `reasonix bot weixin-login` to save an iLink account")
		}
	} else {
		addCheck("bot.weixin", "disabled", "")
	}

	enabledConnections := 0
	for _, conn := range bc.Connections {
		if conn.Enabled {
			enabledConnections++
		}
	}
	addCheck("bot.connections", "ok", fmt.Sprintf("enabled=%d total=%d", enabledConnections, len(bc.Connections)))
	for _, conn := range bc.Connections {
		id := strings.TrimSpace(conn.ID)
		if id == "" {
			id = strings.TrimSpace(conn.Provider)
		}
		status := "ok"
		if !conn.Enabled {
			status = "disabled"
		} else if len(conn.SessionMappings) == 0 && (conn.Provider == string(bot.PlatformFeishu) || conn.Provider == string(bot.PlatformWeixin)) {
			status = "missing"
		}
		addCheck("bot.connection."+id+".session_mappings", status,
			fmt.Sprintf("provider=%s mappings=%d", conn.Provider, len(conn.SessionMappings)))
	}

	// Allowlist 检查
	if bc.Allowlist.AllowAll {
		addCheck("bot.allowlist", "open", "allow_all=true — every reachable user can trigger local tools")
	} else if bc.Allowlist.Enabled {
		addCheck("bot.allowlist", "enabled",
			fmt.Sprintf("qq=%d feishu=%d weixin=%d users",
				len(bc.Allowlist.QQUsers),
				len(bc.Allowlist.FeishuUsers),
				len(bc.Allowlist.WeixinUsers)))
	} else {
		addCheck("bot.allowlist", "missing", "bot start will refuse without allowlist or allow_all=true")
	}

	if *jsonOut {
		fmt.Println("[")
		for i, r := range results {
			comma := ","
			if i == len(results)-1 {
				comma = ""
			}
			fmt.Printf("  {\"name\":%q,\"status\":%q,\"detail\":%q}%s\n", r.Name, r.Status, r.Detail, comma)
		}
		fmt.Println("]")
	} else {
		for _, r := range results {
			marker := "✓"
			if r.Status == "missing" || r.Status == "disabled" {
				marker = "✗"
			}
			fmt.Printf("  %s %s: %s", marker, r.Name, r.Status)
			if r.Detail != "" {
				fmt.Printf(" — %s", r.Detail)
			}
			fmt.Println()
		}
	}

	return 0
}

func botWeixinLogin(args []string) int {
	fs := flag.NewFlagSet("bot weixin-login", flag.ContinueOnError)
	timeoutSeconds := fs.Int("timeout", 480, "登录超时时间（秒）")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadBotCommandConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		return 1
	}

	if !cfg.Bot.Weixin.Enabled {
		fmt.Fprintln(os.Stderr, "error: weixin bot is not enabled in config")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSeconds)*time.Second)
	defer cancel()
	result, err := weixin.Login(ctx, os.Stdout, time.Duration(*timeoutSeconds)*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: weixin login failed: %v\n", err)
		return 1
	}
	fmt.Printf("\n微信登录成功: account_id=%s user_id=%s base_url=%s\n", result.AccountID, result.UserID, result.BaseURL)
	fmt.Println("凭据已保存到 Reasonix 用户配置目录；也可以把 [bot.weixin] account_id 设置为该 account_id。")

	return 0
}

func loadBotCommandConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	userPath := config.UserConfigPath()
	if strings.TrimSpace(userPath) == "" {
		return cfg, nil
	}
	if _, err := os.Stat(userPath); err != nil {
		return cfg, nil
	}
	userCfg := config.LoadForEdit(userPath)
	if botConfigIsUserOwned(userCfg.Bot) {
		cfg.Bot = userCfg.Bot
	}
	return cfg, nil
}

func botConfigIsUserOwned(bc config.BotConfig) bool {
	if bc.Enabled || len(bc.Connections) > 0 || bc.QQ.Enabled || bc.Feishu.Enabled || bc.Weixin.Enabled {
		return true
	}
	if bc.Allowlist.AllowAll || botruntime.AllowlistUserCount(bc.Allowlist) > 0 {
		return true
	}
	return len(bc.Allowlist.QQGroups)+len(bc.Allowlist.FeishuGroups)+len(bc.Allowlist.WeixinGroups) > 0
}

func botUsage() {
	fmt.Print(`reasonix bot — multi-channel IM bot gateway (QQ / Feishu / WeChat)

Usage:
  reasonix bot start   [--channels qq,feishu,lark,weixin] [--dir PATH] [--model NAME]
  reasonix bot doctor  [--json]
  reasonix bot weixin-login [--timeout SECONDS]

Subcommands:
  start         启动 bot 网关
  doctor        诊断 bot 配置和连通性
  weixin-login  微信 iLink 二维码登录

Examples:
  reasonix bot start --channels qq,feishu
  reasonix bot start --dir /path/to/project --model deepseek-pro
  reasonix bot doctor --json

Configuration:
  Edit reasonix.toml:
    [bot]           enabled / model / max_steps
    [bot.allowlist]  enabled / qq_users / feishu_users / weixin_users
    [bot.qq]         enabled / app_id / app_secret_env
    [bot.feishu]     enabled / app_id / app_secret_env / verification_token / mode
    [bot.weixin]     enabled / account_id / token_env / api_base

  All secrets are read from environment variables; never put keys in config files.
`)
}

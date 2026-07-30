package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	case "pairing":
		return botPairing(rest)
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

	if !cfg.Bot.Enabled {
		fmt.Fprintln(os.Stderr, "error: bot is not enabled in config — set [bot] enabled = true")
		return 1
	}
	if !botruntime.BotConfigHasAccessControl(cfg.Bot) {
		fmt.Fprintln(os.Stderr, "error: bot requires explicit access control; set per-connection access, enable pairing, configure [bot.allowlist], or set allow_all = true intentionally")
		return 1
	}

	workspaceRoot := *dir
	if workspaceRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			workspaceRoot = wd
		}
	}

	requestedChannels := splitBotChannels(*channels)
	enabledPlatforms, unknownChannels := botruntime.EnabledPlatforms(cfg, requestedChannels)
	for _, ch := range unknownChannels {
		fmt.Fprintf(os.Stderr, "warning: unknown channel %q\n", ch)
	}
	if !botruntime.HasEnabledPlatform(enabledPlatforms) {
		fmt.Fprintln(os.Stderr, "error: no bot channels enabled — enable at least one in config")
		return 1
	}

	modelName := botruntime.ModelName(cfg, *model)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	rememberInboundRemote := botruntime.NewRemoteRememberer(logger)

	// 构建网关配置
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
			bot.PlatformQQ:     cfg.Bot.SelfUserIDs.QQ,
			bot.PlatformFeishu: cfg.Bot.SelfUserIDs.Feishu,
			bot.PlatformWeixin: cfg.Bot.SelfUserIDs.Weixin,
		},
		ControlEnabled:     cfg.Bot.Control.Enabled,
		ControlAddr:        cfg.Bot.Control.Addr,
		ControlToken:       os.Getenv(strings.TrimSpace(cfg.Bot.Control.TokenEnv)),
		WorkspaceRoot:      workspaceRoot,
		Channels:           botruntime.ChannelConfigs(cfg.Bot.Connections, *model == "", *dir == ""),
		ConnectionChannels: botruntime.ConnectionChannelConfigs(cfg.Bot.Connections, *model == "", *dir == ""),
		Routes:             botruntime.RouteConfigs(cfg.Bot.Routes, *model == "", *dir == ""),
		ConnectionAccess:   botruntime.ConnectionAccessConfigs(cfg),
		Enabled:            enabledPlatforms,
		Allowlist: bot.AllowlistConfig{
			Enabled:  cfg.Bot.Allowlist.Enabled,
			AllowAll: cfg.Bot.Allowlist.AllowAll,
			Users: map[bot.Platform][]string{
				bot.PlatformQQ:     cfg.Bot.Allowlist.QQUsers,
				bot.PlatformFeishu: cfg.Bot.Allowlist.FeishuUsers,
				bot.PlatformWeixin: cfg.Bot.Allowlist.WeixinUsers,
			},
			Approvers: map[bot.Platform][]string{
				bot.PlatformQQ:     cfg.Bot.Allowlist.QQApprovers,
				bot.PlatformFeishu: cfg.Bot.Allowlist.FeishuApprovers,
				bot.PlatformWeixin: cfg.Bot.Allowlist.WeixinApprovers,
			},
			Admins: map[bot.Platform][]string{
				bot.PlatformQQ:     cfg.Bot.Allowlist.QQAdmins,
				bot.PlatformFeishu: cfg.Bot.Allowlist.FeishuAdmins,
				bot.PlatformWeixin: cfg.Bot.Allowlist.WeixinAdmins,
			},
			Groups: map[bot.Platform][]string{
				bot.PlatformQQ:     cfg.Bot.Allowlist.QQGroups,
				bot.PlatformFeishu: cfg.Bot.Allowlist.FeishuGroups,
				bot.PlatformWeixin: cfg.Bot.Allowlist.WeixinGroups,
			},
		},
		Debounce:       time.Duration(cfg.Bot.DebounceMs) * time.Millisecond,
		OnInbound:      rememberInboundRemote,
		OnSessionReady: botruntime.NewSessionRemembererWithWorkspace(logger, workspaceRoot),
	}

	feishuDomains := botruntime.RequestedFeishuDomains(requestedChannels)
	gw := bot.NewGatewayWithAdapterBindings(gwCfg, botruntime.AdapterBindings(cfg, enabledPlatforms, feishuDomains, logger), logger)

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nshutting down...")
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "reasonix bot starting (model: %s, channels: %s)...\n", modelName, *channels)
	fmt.Fprintf(os.Stderr, "version: %s\n", version)

	if err := gw.Start(ctx); err != nil {
		gw.Stop()
		fmt.Fprintf(os.Stderr, "error: start gateway: %v\n", err)
		return 1
	}
	defer gw.Stop()

	// 等待信号或 context 取消
	<-ctx.Done()
	return 0
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
	deep := fs.Bool("deep", false, "执行更详细的本机诊断")

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
	if *deep {
		if path := config.UserConfigPath(); path != "" {
			if _, err := os.Stat(path); err == nil {
				addCheck("bot.config.user", "ok", path)
			} else {
				addCheck("bot.config.user", "missing", path)
			}
		}
		if dir := config.SessionDir(); dir != "" {
			addCheck("bot.sessions.dir", "ok", dir)
		}
	}
	queueMode := bot.NormalizeQueueMode(bc.QueueMode)
	queueCap := bc.QueueCap
	if queueCap <= 0 {
		queueCap = bot.DefaultQueueCap
	}
	addCheck("bot.queue", "ok", fmt.Sprintf("mode=%s cap=%d drop=%s", queueMode, queueCap, bot.NormalizeQueueDrop(bc.QueueDrop)))
	if bc.Pairing.Enabled {
		addCheck("bot.pairing", "enabled", fmt.Sprintf("ttl=%dm max_pending=%d", bc.Pairing.RequestTTLMinutes, bc.Pairing.MaxPendingPerPlatform))
	} else {
		addCheck("bot.pairing", "disabled", "")
	}
	if *deep {
		reqs, err := bot.ListPairingRequests()
		if err != nil {
			addCheck("bot.pairing.pending", "error", err.Error())
		} else {
			addCheck("bot.pairing.pending", "ok", fmt.Sprintf("%d pending", len(reqs)))
		}
		if path := bot.PairingStorePath(); path != "" {
			if info, err := os.Stat(path); err == nil {
				addCheck("bot.pairing.store", "ok", fmt.Sprintf("%s mode=%s", path, info.Mode().Perm()))
			} else {
				addCheck("bot.pairing.store", "missing", path)
			}
		}
	}
	if *deep {
		selfStatus := "disabled"
		if bc.IgnoreSelfMessages {
			selfStatus = "enabled"
		}
		addCheck("bot.self_protection", selfStatus,
			fmt.Sprintf("self_ids=%d", len(bc.SelfUserIDs.QQ)+len(bc.SelfUserIDs.Feishu)+len(bc.SelfUserIDs.Weixin)))
		controlStatus := "disabled"
		controlDetail := ""
		if bc.Control.Enabled {
			controlStatus = "enabled"
			tokenStatus := "missing_token"
			if strings.TrimSpace(bc.Control.TokenEnv) != "" && os.Getenv(strings.TrimSpace(bc.Control.TokenEnv)) != "" {
				tokenStatus = "token_set"
			}
			addr := strings.TrimSpace(bc.Control.Addr)
			if addr == "" {
				addr = "127.0.0.1:37913"
			}
			controlDetail = fmt.Sprintf("addr=%s token_env=%s %s", addr, bc.Control.TokenEnv, tokenStatus)
		}
		addCheck("bot.control", controlStatus, controlDetail)
		addCheck("bot.routes", "ok", fmt.Sprintf("%d routes", len(bc.Routes)))
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
			fmt.Sprintf("qq=%d feishu=%d weixin=%d users approvers=%d admins=%d",
				len(bc.Allowlist.QQUsers),
				len(bc.Allowlist.FeishuUsers),
				len(bc.Allowlist.WeixinUsers),
				len(bc.Allowlist.QQApprovers)+len(bc.Allowlist.FeishuApprovers)+len(bc.Allowlist.WeixinApprovers),
				len(bc.Allowlist.QQAdmins)+len(bc.Allowlist.FeishuAdmins)+len(bc.Allowlist.WeixinAdmins)))
	} else {
		addCheck("bot.allowlist", "missing", "bot start will refuse without allowlist or allow_all=true")
	}
	if *deep {
		addCheck("bot.roles", "ok",
			fmt.Sprintf("approvers=%d admins=%d",
				len(bc.Allowlist.QQApprovers)+len(bc.Allowlist.FeishuApprovers)+len(bc.Allowlist.WeixinApprovers),
				len(bc.Allowlist.QQAdmins)+len(bc.Allowlist.FeishuAdmins)+len(bc.Allowlist.WeixinAdmins)))
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

func botPairing(args []string) int {
	if len(args) < 1 {
		botPairingUsage()
		return 2
	}
	switch args[0] {
	case "list":
		reqs, err := bot.ListPairingRequests()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: list pairing requests: %v\n", err)
			return 1
		}
		if len(reqs) == 0 {
			fmt.Println("No pending bot pairing requests.")
			return 0
		}
		for _, req := range reqs {
			fmt.Printf("%s\t%s\t%s\tuser=%s\tchat=%s\texpires=%s\n",
				req.Code,
				req.Platform,
				req.ChatType,
				req.UserID,
				req.ChatID,
				req.ExpiresAt.Local().Format("2006-01-02 15:04"),
			)
		}
		return 0
	case "approve":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "error: pairing approve requires a code")
			return 2
		}
		req, err := bot.ApprovePairingCode(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: approve pairing: %v\n", err)
			return 1
		}
		fmt.Printf("Approved %s user %s for %s.\n", req.Platform, req.UserID, req.ChatID)
		return 0
	case "reject", "deny":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "error: pairing reject requires a code")
			return 2
		}
		req, err := bot.RejectPairingCode(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: reject pairing: %v\n", err)
			return 1
		}
		fmt.Printf("Rejected %s user %s for %s.\n", req.Platform, req.UserID, req.ChatID)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown bot pairing subcommand %q\n\n", args[0])
		botPairingUsage()
		return 2
	}
}

func botPairingUsage() {
	fmt.Print(`reasonix bot pairing — approve pending bot DM pairings

Usage:
  reasonix bot pairing list
  reasonix bot pairing approve CODE
  reasonix bot pairing reject CODE
`)
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
	if botruntime.BotAccessActive(bc.QQ.Access) {
		return true
	}
	for _, conn := range bc.Connections {
		if botruntime.BotAccessActive(conn.Access) {
			return true
		}
	}
	return len(bc.Allowlist.QQGroups)+len(bc.Allowlist.FeishuGroups)+len(bc.Allowlist.WeixinGroups)+
		len(bc.Allowlist.QQApprovers)+len(bc.Allowlist.FeishuApprovers)+len(bc.Allowlist.WeixinApprovers)+
		len(bc.Allowlist.QQAdmins)+len(bc.Allowlist.FeishuAdmins)+len(bc.Allowlist.WeixinAdmins) > 0
}

func botUsage() {
	fmt.Print(`reasonix bot — multi-channel IM bot gateway (QQ / Feishu / WeChat)

Usage:
  reasonix bot start   [--channels qq,feishu,lark,weixin] [--dir PATH] [--model NAME]
  reasonix bot doctor  [--json] [--deep]
  reasonix bot pairing list|approve|reject
  reasonix bot weixin-login [--timeout SECONDS]

Subcommands:
  start         启动 bot 网关
  doctor        诊断 bot 配置和连通性
  pairing       查看或批准 IM 私聊配对
  weixin-login  微信 iLink 二维码登录

Examples:
  reasonix bot start --channels qq,feishu
  reasonix bot start --dir /path/to/project --model deepseek-pro
  reasonix bot doctor --json

Configuration:
  Edit reasonix.toml:
    [bot]           enabled / model / max_steps
    [bot]           queue_mode / queue_cap / queue_drop
    [bot.pairing]   enabled / request_ttl_minutes / max_pending_per_platform
    [bot.allowlist]  enabled / users / approvers / admins / groups
    [bot.qq]         enabled / app_id / app_secret_env
    [bot.feishu]     enabled / app_id / app_secret_env / verification_token / mode
    [bot.weixin]     enabled / account_id / token_env / api_base

  All secrets are read from environment variables; never put keys in config files.
`)
}

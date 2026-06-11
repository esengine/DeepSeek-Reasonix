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
	"reasonix/internal/bot/feishu"
	"reasonix/internal/bot/qq"
	"reasonix/internal/bot/weixin"
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
	channels := fs.String("channels", "", "启用的平台，逗号分隔：qq,feishu,weixin")
	dir := fs.String("dir", "", "工作目录")
	model := fs.String("model", "", "模型名（空则用 default_model）")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		return 1
	}

	if !cfg.Bot.Enabled {
		fmt.Fprintln(os.Stderr, "error: bot is not enabled in config — set [bot] enabled = true")
		return 1
	}
	if !cfg.Bot.Allowlist.AllowAll && (!cfg.Bot.Allowlist.Enabled || botAllowlistUserCount(cfg.Bot.Allowlist) == 0) {
		fmt.Fprintln(os.Stderr, "error: bot requires an explicit allowlist; set [bot.allowlist] enabled = true with platform user ids, or set allow_all = true intentionally")
		return 1
	}

	workspaceRoot := *dir
	if workspaceRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			workspaceRoot = wd
		}
	}

	// 确定启用的平台
	enabledPlatforms := make(map[bot.Platform]bool)
	if *channels != "" {
		for _, ch := range strings.Split(*channels, ",") {
			ch = strings.TrimSpace(ch)
			switch bot.Platform(ch) {
			case bot.PlatformQQ:
				enabledPlatforms[bot.PlatformQQ] = cfg.Bot.QQ.Enabled
			case bot.PlatformFeishu:
				enabledPlatforms[bot.PlatformFeishu] = cfg.Bot.Feishu.Enabled
			case bot.PlatformWeixin:
				enabledPlatforms[bot.PlatformWeixin] = cfg.Bot.Weixin.Enabled
			default:
				fmt.Fprintf(os.Stderr, "warning: unknown channel %q\n", ch)
			}
		}
	} else {
		enabledPlatforms[bot.PlatformQQ] = cfg.Bot.QQ.Enabled
		enabledPlatforms[bot.PlatformFeishu] = cfg.Bot.Feishu.Enabled
		enabledPlatforms[bot.PlatformWeixin] = cfg.Bot.Weixin.Enabled
	}

	hasEnabled := false
	for _, v := range enabledPlatforms {
		if v {
			hasEnabled = true
			break
		}
	}
	if !hasEnabled {
		fmt.Fprintln(os.Stderr, "error: no bot channels enabled — enable at least one in config")
		return 1
	}

	modelName := *model
	if modelName == "" {
		modelName = cfg.Bot.Model
	}
	if modelName == "" {
		modelName = cfg.DefaultModel
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// 构建网关配置
	gwCfg := bot.GatewayConfig{
		Model:         modelName,
		MaxSteps:      cfg.Bot.MaxSteps,
		WorkspaceRoot: workspaceRoot,
		Channels:      botChannelConfigs(cfg.Bot.Connections, *model == "", *dir == ""),
		Enabled:       enabledPlatforms,
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
		Debounce: time.Duration(cfg.Bot.DebounceMs) * time.Millisecond,
	}

	// 创建适配器
	adapters := make(map[bot.Platform]bot.Adapter)
	if enabledPlatforms[bot.PlatformQQ] {
		adapters[bot.PlatformQQ] = qq.New(cfg.Bot.QQ, logger)
	}
	if enabledPlatforms[bot.PlatformFeishu] {
		adapters[bot.PlatformFeishu] = feishu.New(cfg.Bot.Feishu, logger)
	}
	if enabledPlatforms[bot.PlatformWeixin] {
		adapters[bot.PlatformWeixin] = weixin.New(cfg.Bot.Weixin, logger)
	}

	gw := bot.NewGateway(gwCfg, adapters, logger)

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nshutting down...")
		cancel()
		gw.Stop()
	}()

	fmt.Fprintf(os.Stderr, "reasonix bot starting (model: %s, channels: %s)...\n", modelName, *channels)
	fmt.Fprintf(os.Stderr, "version: %s\n", version)

	if err := gw.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: start gateway: %v\n", err)
		return 1
	}

	// 等待信号或 context 取消
	<-ctx.Done()
	return 0
}

func botChannelConfigs(connections []config.BotConnectionConfig, includeModel bool, includeWorkspaceRoot bool) map[bot.Platform]bot.ChannelConfig {
	if len(connections) == 0 {
		return nil
	}
	out := make(map[bot.Platform]bot.ChannelConfig)
	for _, conn := range connections {
		if !conn.Enabled {
			continue
		}
		plat := bot.Platform(strings.TrimSpace(conn.Provider))
		switch plat {
		case bot.PlatformQQ, bot.PlatformFeishu, bot.PlatformWeixin:
		default:
			continue
		}
		channel := out[plat]
		if includeModel {
			channel.Model = strings.TrimSpace(conn.Model)
		}
		if includeWorkspaceRoot {
			channel.WorkspaceRoot = strings.TrimSpace(conn.WorkspaceRoot)
		}
		if channel.Model != "" || channel.WorkspaceRoot != "" {
			out[plat] = channel
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func botDoctor(args []string) int {
	fs := flag.NewFlagSet("bot doctor", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load()
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

func botAllowlistUserCount(a config.BotAllowlist) int {
	return len(a.QQUsers) + len(a.FeishuUsers) + len(a.WeixinUsers)
}

func botWeixinLogin(args []string) int {
	fs := flag.NewFlagSet("bot weixin-login", flag.ContinueOnError)
	timeoutSeconds := fs.Int("timeout", 480, "登录超时时间（秒）")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load()
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

func botUsage() {
	fmt.Print(`reasonix bot — multi-channel IM bot gateway (QQ / Feishu / WeChat)

Usage:
  reasonix bot start   [--channels qq,feishu,weixin] [--dir PATH] [--model NAME]
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

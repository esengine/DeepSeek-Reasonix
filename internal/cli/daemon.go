package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"log/slog"

	"reasonix/internal/config"
	"reasonix/internal/daemon"
)

const defaultDaemonLogMaxSize = 10 << 20

func daemonCommand(args []string) int {
	if len(args) < 1 {
		daemonUsage()
		return 2
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "start":
		return daemonStart(rest)
	case "status":
		return daemonStatus(rest)
	case "doctor":
		return daemonDoctor(rest)
	case "startup":
		return daemonStartupCmd(rest)
	case "token":
		return daemonTokenCmd(rest)
	case "sessions":
		return daemonSessions(rest)
	case "approvals":
		return daemonApprovals(rest)
	case "timeline":
		return daemonTimeline(rest)
	case "stop":
		return daemonStopCmd(rest)
	case "continue":
		return daemonContinueCmd(rest)
	case "schedule":
		return daemonScheduleCmd(rest)
	case "disable-schedule":
		return daemonDisableScheduleCmd(rest)
	case "budget":
		return daemonBudgetCmd(rest)
	case "budgets":
		return daemonBudgetsCmd(rest)
	case "templates":
		return daemonTemplatesCmd(rest)
	case "apply-template":
		return daemonApplyTemplateCmd(rest)
	case "daily-triage":
		return daemonDailyTriageCmd(rest)
	case "ci-watch":
		return daemonCIWatchCmd(rest)
	case "release-assist":
		return daemonReleaseAssistCmd(rest)
	case "repo-health":
		return daemonRepoHealthCmd(rest)
	case "wait-event":
		return daemonWaitEventCmd(rest)
	case "wait-time":
		return daemonWaitTimeCmd(rest)
	case "wait-file":
		return daemonWaitFileCmd(rest)
	case "disable-watch":
		return daemonDisableWatchCmd(rest)
	case "approve":
		return daemonApprovalCmd(rest, true)
	case "deny":
		return daemonApprovalCmd(rest, false)
	case "answer":
		return daemonAnswerCmd(rest)
	case "help", "--help", "-h":
		daemonUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown daemon subcommand %q\n\n", sub)
		daemonUsage()
		return 2
	}
}

func daemonTokenCmd(args []string) int {
	if len(args) < 1 {
		daemonTokenUsage()
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "rotate":
		return daemonTokenRotate(rest)
	case "help", "--help", "-h":
		daemonTokenUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown daemon token subcommand %q\n\n", sub)
		daemonTokenUsage()
		return 2
	}
}

func daemonTokenRotate(args []string) int {
	fs := flag.NewFlagSet("daemon token rotate", flag.ContinueOnError)
	dir := fs.String("dir", "", "会话目录（默认用户配置）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sessionDir := strings.TrimSpace(*dir)
	if sessionDir == "" {
		sessionDir = config.SessionDir()
	}
	if _, err := daemon.RotateToken(sessionDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: rotate daemon token: %v\n", err)
		return 1
	}
	fmt.Printf("daemon token rotated: %s\n", daemon.TokenFile(sessionDir))
	fmt.Println("restart any running daemon for the new token to take effect")
	return 0
}

func daemonTokenUsage() {
	fmt.Print(`reasonix daemon token — 管理本地 daemon API token

Usage:
  reasonix daemon token rotate [--dir PATH]

Subcommands:
  rotate   生成并写入新的本地 daemon API token（不打印 token 内容）
`)
}

func daemonTimeline(args []string) int {
	fs := flag.NewFlagSet("daemon timeline", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "session ID")
	limit := fs.Int("limit", 50, "最多显示多少条事件，0 表示全部")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sessionID == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	q := url.Values{}
	q.Set("session_id", *sessionID)
	q.Set("limit", fmt.Sprintf("%d", *limit))
	resp, err := daemonGet(*addr, *dir, "/timeline?"+q.Encode())
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	var timeline daemon.TimelineResponse
	if err := json.NewDecoder(resp.Body).Decode(&timeline); err != nil {
		fmt.Fprintf(os.Stderr, "invalid response: %v\n", err)
		return 1
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(timeline)
		return 0
	}
	if len(timeline.Events) == 0 {
		fmt.Println("no timeline events")
		return 0
	}
	for _, e := range timeline.Events {
		when := e.Time.Local().Format("2006-01-02 15:04:05")
		parts := []string{when, e.Type}
		if e.Source != "" {
			parts = append(parts, "source="+e.Source)
		}
		if e.RunStatus != "" {
			parts = append(parts, "run="+e.RunStatus)
		}
		if e.WaitKind != "" {
			wait := e.WaitKind
			if e.WaitID != "" {
				wait += ":" + e.WaitID
			}
			parts = append(parts, "wait="+wait)
		}
		if e.Error != "" {
			parts = append(parts, "error="+e.Error)
		}
		fmt.Println(strings.Join(parts, "  "))
	}
	return 0
}

func daemonStart(args []string) int {
	fs := flag.NewFlagSet("daemon start", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "监听地址")
	dir := fs.String("dir", "", "会话目录（默认用户配置）")
	logFile := fs.String("log-file", "", "daemon 日志文件（默认 <session-dir>/.daemon.log，none 表示关闭文件日志）")
	logMaxSize := fs.String("log-max-size", "10MB", "daemon 日志轮转阈值，0 表示关闭轮转")
	startBot := fs.Bool("bot", false, "同时启动 bot gateway")
	botChannels := fs.String("bot-channels", "", "bot 平台，逗号分隔：qq,feishu,weixin")
	botDir := fs.String("bot-dir", "", "bot 工作目录（空则使用当前目录或 bot connection 配置）")
	botModel := fs.String("bot-model", "", "bot 模型名（空则用 bot/default_model 配置）")
	webhook := fs.Bool("webhook", false, "启用 /webhook 外部事件入口")
	webhookSecret := fs.String("webhook-secret", "", "webhook HMAC secret（也可用 REASONIX_DAEMON_WEBHOOK_SECRET）")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	maxLogBytes, err := parseDaemonLogSize(*logMaxSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --log-max-size: %v\n", err)
		return 2
	}
	webhookCfg, err := resolveDaemonWebhookConfig(*webhook, *webhookSecret, os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\ndaemon shutting down...")
		cancel()
	}()

	logPath := resolveDaemonLogFile(*dir, *logFile)
	logger, logCloser, err := newDaemonLogger(os.Stderr, logPath, maxLogBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open daemon log: %v\n", err)
		return 1
	}
	if logCloser != nil {
		defer logCloser.Close()
		logger.Info("daemon logging to file", "path", logPath)
	}

	if *startBot {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: load config for bot: %v\n", err)
			return 1
		}
		prepared, err := prepareBotGateway(cfg, botGatewayOptions{
			Channels:      *botChannels,
			WorkspaceRoot: *botDir,
			Model:         *botModel,
		}, logger, func(format string, args ...interface{}) {
			logger.Warn(fmt.Sprintf(format, args...))
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: bot gateway: %v\n", err)
			return 1
		}
		if err := prepared.Gateway.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "error: start bot gateway: %v\n", err)
			prepared.Gateway.Stop()
			return 1
		}
		defer prepared.Gateway.Stop()
		logger.Info("daemon bot gateway started", "model", prepared.Model, "channels", prepared.ChannelSummary)
	}

	d := daemon.New(daemon.Options{
		Addr:       *addr,
		SessionDir: *dir,
		Logger:     logger,
		Webhook:    webhookCfg,
	})

	if err := d.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func resolveDaemonWebhookConfig(enabled bool, secret string, getenv func(string) string) (*daemon.WebhookConfig, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" && getenv != nil {
		secret = strings.TrimSpace(getenv("REASONIX_DAEMON_WEBHOOK_SECRET"))
	}
	if !enabled && secret == "" {
		return nil, nil
	}
	if secret == "" {
		return nil, fmt.Errorf("--webhook requires --webhook-secret or REASONIX_DAEMON_WEBHOOK_SECRET")
	}
	return &daemon.WebhookConfig{Enabled: true, Secret: secret}, nil
}

func resolveDaemonLogFile(sessionDir, requested string) string {
	requested = strings.TrimSpace(requested)
	switch strings.ToLower(requested) {
	case "none", "off", "false":
		return ""
	case "":
		sessionDir = strings.TrimSpace(sessionDir)
		if sessionDir == "" {
			sessionDir = config.SessionDir()
		}
		if sessionDir == "" {
			return ""
		}
		return daemon.LogFile(sessionDir)
	default:
		return requested
	}
}

func parseDaemonLogSize(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return defaultDaemonLogMaxSize, nil
	}
	upper := strings.ToUpper(s)
	mult := int64(1)
	for _, suffix := range []struct {
		name string
		mult int64
	}{
		{"GB", 1 << 30},
		{"G", 1 << 30},
		{"MB", 1 << 20},
		{"M", 1 << 20},
		{"KB", 1 << 10},
		{"K", 1 << 10},
		{"B", 1},
	} {
		if strings.HasSuffix(upper, suffix.name) {
			mult = suffix.mult
			s = strings.TrimSpace(s[:len(s)-len(suffix.name)])
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("must be >= 0")
	}
	if n > 0 && n > (1<<63-1)/mult {
		return 0, fmt.Errorf("too large")
	}
	return n * mult, nil
}

func newDaemonLogger(stderr io.Writer, logPath string, maxSize int64) (*slog.Logger, io.Closer, error) {
	writer := stderr
	if writer == nil {
		writer = io.Discard
	}
	if strings.TrimSpace(logPath) == "" {
		return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo})), nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, nil, err
	}
	if err := rotateDaemonLogIfNeeded(logPath, maxSize); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return slog.New(slog.NewTextHandler(io.MultiWriter(writer, f), &slog.HandlerOptions{Level: slog.LevelInfo})), f, nil
}

func rotateDaemonLogIfNeeded(logPath string, maxSize int64) error {
	if maxSize <= 0 {
		return nil
	}
	info, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() || info.Size() < maxSize {
		return nil
	}
	backup := logPath + ".1"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(logPath, backup)
}

func daemonStatus(args []string) int {
	fs := flag.NewFlagSet("daemon status", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	resp, err := daemonGet(*addr, *dir, "/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	var status daemon.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Fprintf(os.Stderr, "invalid response: %v\n", err)
		return 1
	}
	fmt.Printf("status: %s\n", status.Status)
	fmt.Printf("addr: %s\n", status.Addr)
	fmt.Printf("pid: %d\n", status.PID)
	fmt.Printf("sessions: %d\n", status.Sessions)
	fmt.Printf("uptime: %s\n", status.Uptime)
	return 0
}

func daemonSessions(args []string) int {
	fs := flag.NewFlagSet("daemon sessions", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	scope := fs.String("scope", "", "过滤范围：global 或 project")
	workspaceRoot := fs.String("workspace-root", "", "project 范围过滤的工作区路径")
	status := fs.String("status", "", "过滤状态：running、waiting、blocked、active、scheduled、watched 或具体 run 状态")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	path := daemonSessionsPath(*scope, *workspaceRoot, *status)
	resp, err := daemonGet(*addr, *dir, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	var sessions daemon.SessionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		fmt.Fprintf(os.Stderr, "invalid response: %v\n", err)
		return 1
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(sessions)
	} else {
		if len(sessions.Sessions) == 0 {
			fmt.Println("no tracked sessions")
			return 0
		}
		for _, s := range sessions.Sessions {
			goal := s.GoalText
			if goal == "" {
				goal = "(none)"
			}
			wait := ""
			if s.WaitKind != "" {
				wait = "  wait=" + s.WaitKind
				if s.WaitID != "" {
					wait += ":" + s.WaitID
				}
				if s.WaitTool != "" {
					wait += "(" + s.WaitTool + ")"
				}
			}
			active := ""
			if s.Active {
				active = "  active=true"
			}
			next := ""
			if s.NextWakeupAt != nil {
				next = "  next=" + s.NextWakeupAt.Format(time.RFC3339)
			}
			flags := daemonSessionFlags(s)
			if flags != "" {
				flags = "  " + flags
			}
			budget := daemonSessionBudgetSummary(s)
			if budget != "" {
				budget = "  budget=" + budget
			}
			fmt.Printf("  %s  scope=%s  goal=%s  status=%s  run=%s%s%s%s%s%s\n", shortDaemonSessionID(s.ID), daemonSessionScope(s.Scope), truncate(goal, 40), s.GoalStatus, s.RunStatus, wait, next, budget, flags, active)
		}
	}
	return 0
}

func daemonSessionsPath(scope, workspaceRoot, status string) string {
	q := url.Values{}
	if strings.TrimSpace(scope) != "" {
		q.Set("scope", strings.TrimSpace(scope))
	}
	if strings.TrimSpace(workspaceRoot) != "" {
		q.Set("workspace_root", strings.TrimSpace(workspaceRoot))
	}
	if strings.TrimSpace(status) != "" {
		q.Set("status", strings.TrimSpace(status))
	}
	if encoded := q.Encode(); encoded != "" {
		return "/sessions?" + encoded
	}
	return "/sessions"
}

func shortDaemonSessionID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func daemonSessionScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "unknown"
	}
	return scope
}

func daemonSessionFlags(s daemon.SessionView) string {
	flags := make([]string, 0, 2)
	if s.Scheduled {
		flags = append(flags, "scheduled=true")
	}
	if s.Watched {
		flags = append(flags, "watched=true")
	}
	return strings.Join(flags, "  ")
}

func daemonSessionBudgetSummary(s daemon.SessionView) string {
	parts := make([]string, 0, 4)
	if s.DailyWakeupLimit > 0 || s.DailyWakeups > 0 {
		parts = append(parts, fmt.Sprintf("wakeups %d/%d", s.DailyWakeups, s.DailyWakeupLimit))
	}
	if s.DailyModelCallLimit > 0 || s.DailyModelCalls > 0 {
		parts = append(parts, fmt.Sprintf("models %d/%d", s.DailyModelCalls, s.DailyModelCallLimit))
	}
	if s.DailyModelCostLimit > 0 || s.DailyModelCost > 0 {
		currency := s.ModelCostCurrency
		if currency == "" {
			currency = "$"
		}
		parts = append(parts, fmt.Sprintf("cost %s%.4g/%s%.4g", currency, s.DailyModelCost, currency, s.DailyModelCostLimit))
	}
	if s.MaxGoalAutoTurns > 0 {
		parts = append(parts, fmt.Sprintf("auto-turns %d", s.MaxGoalAutoTurns))
	}
	if s.BudgetBlockedReason != "" {
		parts = append(parts, "blocked")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ",")
}

func daemonApprovals(args []string) int {
	fs := flag.NewFlagSet("daemon approvals", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	resp, err := daemonGet(*addr, *dir, "/approvals")
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	var approvals daemon.ApprovalDeskResponse
	if err := json.NewDecoder(resp.Body).Decode(&approvals); err != nil {
		fmt.Fprintf(os.Stderr, "invalid response: %v\n", err)
		return 1
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(approvals)
		return 0
	}
	printDaemonApprovals(os.Stdout, approvals)
	return 0
}

func printDaemonApprovals(w io.Writer, approvals daemon.ApprovalDeskResponse) {
	if len(approvals.Items) == 0 {
		fmt.Fprintln(w, "no pending daemon approvals or asks")
		return
	}
	for _, item := range approvals.Items {
		session := item.SessionID
		if len(session) > 12 {
			session = session[:12]
		}
		id := item.ID
		if id == "" {
			id = "(unknown)"
		}
		status := item.RunStatus
		if status == "" {
			status = "waiting"
		}
		active := "inactive"
		if item.Active {
			active = "active"
		}
		parts := []string{
			fmt.Sprintf("%s  %s:%s", session, item.Kind, id),
			"run=" + status,
			active,
		}
		if item.Tool != "" {
			parts = append(parts, "tool="+item.Tool)
		}
		if item.Subject != "" {
			parts = append(parts, "subject="+truncate(item.Subject, 60))
		}
		if item.Reason != "" {
			parts = append(parts, "reason="+truncate(item.Reason, 60))
		}
		fmt.Fprintln(w, strings.Join(parts, "  "))
		switch item.Kind {
		case "approval":
			fmt.Fprintf(w, "  next: reasonix daemon approve --session %s --approval %s  # or daemon deny\n", item.SessionID, id)
		case "ask":
			fmt.Fprintf(w, "  next: reasonix daemon answer --session %s --ask %s --selected TEXT\n", item.SessionID, id)
			for _, q := range item.Questions {
				if q.Prompt == "" && len(q.Options) == 0 {
					continue
				}
				label := q.ID
				if label == "" {
					label = "question"
				}
				fmt.Fprintf(w, "  %s: %s", label, truncate(q.Prompt, 80))
				if len(q.Options) > 0 {
					options := make([]string, 0, len(q.Options))
					for _, opt := range q.Options {
						if opt.Label != "" {
							options = append(options, opt.Label)
						}
					}
					if len(options) > 0 {
						fmt.Fprintf(w, " [%s]", strings.Join(options, " / "))
					}
				}
				fmt.Fprintln(w)
			}
		}
	}
}

func daemonStopCmd(args []string) int {
	fs := flag.NewFlagSet("daemon stop", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要停止的 session ID")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sessionID == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}

	body := fmt.Sprintf(`{"session_id":%q}`, *sessionID)
	resp, err := daemonPost(*addr, *dir, "/stop", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonContinueCmd(args []string) int {
	fs := flag.NewFlagSet("daemon continue", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要继续的 session ID")
	reason := fs.String("reason", "cli", "唤醒原因")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sessionID == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	body := fmt.Sprintf(`{"session_id":%q,"reason":%q}`, *sessionID, *reason)
	resp, err := daemonPost(*addr, *dir, "/continue-goal", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonScheduleCmd(args []string) int {
	fs := flag.NewFlagSet("daemon schedule", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要调度的 session ID")
	scope := fs.String("scope", "", "调度范围：global 或 project（替代 --session）")
	workspaceRoot := fs.String("workspace-root", "", "project scope 的工作区根目录")
	dailyAt := fs.String("daily-at", "", "每日唤醒时间 HH:MM")
	timezone := fs.String("timezone", "", "daily-at 使用的 IANA 时区，例如 Asia/Shanghai")
	interval := fs.String("interval", "", "固定间隔，例如 1h")
	enabled := fs.Bool("enable", true, "是否启用调度")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sessionID) == "" && strings.TrimSpace(*scope) == "" {
		fmt.Fprintln(os.Stderr, "error: --session or --scope is required")
		return 2
	}
	if strings.TrimSpace(*sessionID) != "" && strings.TrimSpace(*scope) != "" {
		fmt.Fprintln(os.Stderr, "error: --session and --scope are mutually exclusive")
		return 2
	}
	payload := map[string]interface{}{
		"enabled": *enabled,
	}
	if strings.TrimSpace(*sessionID) != "" {
		payload["session_id"] = strings.TrimSpace(*sessionID)
	} else {
		payload["scope"] = strings.TrimSpace(*scope)
		if strings.TrimSpace(*workspaceRoot) != "" {
			payload["workspace_root"] = strings.TrimSpace(*workspaceRoot)
		}
	}
	if *dailyAt != "" {
		payload["daily_at"] = *dailyAt
	}
	if *timezone != "" {
		payload["timezone"] = *timezone
	}
	if *interval != "" {
		payload["interval"] = *interval
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/schedule", string(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonDisableScheduleCmd(args []string) int {
	fs := flag.NewFlagSet("daemon disable-schedule", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要关闭调度的 session ID")
	scope := fs.String("scope", "", "关闭调度范围：global 或 project（替代 --session）")
	workspaceRoot := fs.String("workspace-root", "", "project scope 的工作区根目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sessionID) == "" && strings.TrimSpace(*scope) == "" {
		fmt.Fprintln(os.Stderr, "error: --session or --scope is required")
		return 2
	}
	if strings.TrimSpace(*sessionID) != "" && strings.TrimSpace(*scope) != "" {
		fmt.Fprintln(os.Stderr, "error: --session and --scope are mutually exclusive")
		return 2
	}
	payload := map[string]interface{}{"enabled": false}
	if strings.TrimSpace(*sessionID) != "" {
		payload["session_id"] = strings.TrimSpace(*sessionID)
	} else {
		payload["scope"] = strings.TrimSpace(*scope)
		if strings.TrimSpace(*workspaceRoot) != "" {
			payload["workspace_root"] = strings.TrimSpace(*workspaceRoot)
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/schedule", string(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	if out, ok := readDaemonCommandResponse(resp); ok {
		fmt.Println(out)
		return 0
	}
	return 1
}

func daemonBudgetCmd(args []string) int {
	fs := flag.NewFlagSet("daemon budget", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要配置预算的 session ID")
	scope := fs.String("scope", "", "要配置聚合预算的范围：global 或 project")
	workspaceRoot := fs.String("workspace-root", "", "project 聚合预算的工作区路径")
	dailyWakeups := fs.Int("daily-wakeups", -1, "每日自动唤醒次数上限，0 表示关闭限制")
	maxGoalAutoTurns := fs.Int("max-goal-auto-turns", -1, "每个 goal 最大自动续跑轮次，0 表示使用内置默认值")
	dailyModelCalls := fs.Int("daily-model-calls", -1, "每日模型调用次数上限，0 表示关闭限制")
	dailyModelCost := fs.Float64("daily-model-cost", -1, "每日模型费用上限，0 表示关闭限制")
	reset := fs.Bool("reset", false, "重置当前 UTC 日预算计数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	*sessionID = strings.TrimSpace(*sessionID)
	*scope = strings.TrimSpace(*scope)
	*workspaceRoot = strings.TrimSpace(*workspaceRoot)
	if *sessionID == "" && *scope == "" {
		fmt.Fprintln(os.Stderr, "error: --session or --scope is required")
		return 2
	}
	if *sessionID != "" && *scope != "" {
		fmt.Fprintln(os.Stderr, "error: --session and --scope are mutually exclusive")
		return 2
	}
	if *scope != "" && *scope != "global" && *scope != "project" {
		fmt.Fprintln(os.Stderr, "error: --scope must be global or project")
		return 2
	}
	if *scope == "project" && *workspaceRoot == "" {
		fmt.Fprintln(os.Stderr, "error: --workspace-root is required for project scope")
		return 2
	}
	if *dailyWakeups < 0 && *maxGoalAutoTurns < 0 && *dailyModelCalls < 0 && *dailyModelCost < 0 && !*reset {
		fmt.Fprintln(os.Stderr, "error: --daily-wakeups, --max-goal-auto-turns, --daily-model-calls, --daily-model-cost, or --reset is required")
		return 2
	}
	if *dailyWakeups < -1 {
		fmt.Fprintln(os.Stderr, "error: --daily-wakeups must be >= 0")
		return 2
	}
	if *maxGoalAutoTurns < -1 {
		fmt.Fprintln(os.Stderr, "error: --max-goal-auto-turns must be >= 0")
		return 2
	}
	if *dailyModelCalls < -1 {
		fmt.Fprintln(os.Stderr, "error: --daily-model-calls must be >= 0")
		return 2
	}
	if *dailyModelCost < -1 {
		fmt.Fprintln(os.Stderr, "error: --daily-model-cost must be >= 0")
		return 2
	}
	if *scope != "" && (*dailyWakeups >= 0 || *maxGoalAutoTurns >= 0) {
		fmt.Fprintln(os.Stderr, "error: scope budgets support --daily-model-calls and --daily-model-cost only")
		return 2
	}
	payload := map[string]interface{}{}
	if *sessionID != "" {
		payload["session_id"] = *sessionID
	} else {
		payload["scope"] = *scope
		if *workspaceRoot != "" {
			payload["workspace_root"] = *workspaceRoot
		}
	}
	if *dailyWakeups >= 0 {
		payload["daily_wakeup_limit"] = *dailyWakeups
	}
	if *maxGoalAutoTurns >= 0 {
		payload["max_goal_auto_turns"] = *maxGoalAutoTurns
	}
	if *dailyModelCalls >= 0 {
		payload["daily_model_call_limit"] = *dailyModelCalls
	}
	if *dailyModelCost >= 0 {
		payload["daily_model_cost_limit"] = *dailyModelCost
	}
	if *reset {
		payload["reset"] = true
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/budget", string(bodyBytes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonBudgetsCmd(args []string) int {
	fs := flag.NewFlagSet("daemon budgets", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	resp, err := daemonGet(*addr, *dir, "/budgets")
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	var out daemon.BudgetAggregatesResponse
	if err := json.Unmarshal(b, &out); err != nil {
		fmt.Fprintf(os.Stderr, "invalid response: %v\n", err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	return 0
}

func daemonCIWatchCmd(args []string) int {
	fs := flag.NewFlagSet("daemon ci-watch", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要设置 CI watcher 的 session ID")
	source := fs.String("source", "workflow_run", "GitHub CI 事件来源：workflow_run、check_suite 或 status")
	pr := fs.Int("pr", 0, "关联的 PR 编号（用于 subject 展示）")
	repo := fs.String("repo", "", "关联仓库，例如 owner/repo（用于 subject 展示）")
	subject := fs.String("subject", "", "等待对象说明，默认由 --repo/--pr 生成")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sessionID) == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	eventSource, eventStatus, err := ciWatchEventFields(*source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	waitSubject := strings.TrimSpace(*subject)
	if waitSubject == "" {
		waitSubject = ciWatchSubject(*repo, *pr)
	}
	payload := map[string]interface{}{
		"session_id":       strings.TrimSpace(*sessionID),
		"event_source":     eventSource,
		"event_conclusion": "success",
		"reason":           "waiting for CI success",
	}
	if eventStatus != "" {
		payload["event_status"] = eventStatus
	}
	if waitSubject != "" {
		payload["subject"] = waitSubject
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/wait-event", string(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func ciWatchEventFields(source string) (eventSource, eventStatus string, err error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "workflow", "workflow_run", "github.workflow_run":
		return "github.workflow_run", "completed", nil
	case "check", "checks", "check_suite", "github.check_suite":
		return "github.check_suite", "completed", nil
	case "status", "commit_status", "github.status":
		return "github.status", "", nil
	default:
		return "", "", fmt.Errorf("--source must be workflow_run, check_suite, or status")
	}
}

func ciWatchSubject(repo string, pr int) string {
	repo = strings.TrimSpace(repo)
	switch {
	case repo != "" && pr > 0:
		return fmt.Sprintf("%s PR #%d", repo, pr)
	case pr > 0:
		return fmt.Sprintf("PR #%d", pr)
	case repo != "":
		return repo
	default:
		return ""
	}
}

func daemonDailyTriageCmd(args []string) int {
	fs := flag.NewFlagSet("daemon daily-triage", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要配置每日 triage 的 session ID")
	dailyAt := fs.String("daily-at", "09:00", "每日 triage 唤醒时间 HH:MM")
	timezone := fs.String("timezone", "", "daily-at 使用的 IANA 时区，例如 Asia/Shanghai")
	dailyWakeups := fs.Int("daily-wakeups", 1, "每日自动唤醒次数上限，0 表示关闭限制，-1 表示不修改预算")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	session := strings.TrimSpace(*sessionID)
	if session == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	if strings.TrimSpace(*dailyAt) == "" {
		fmt.Fprintln(os.Stderr, "error: --daily-at is required")
		return 2
	}
	if *dailyWakeups < -1 {
		fmt.Fprintln(os.Stderr, "error: --daily-wakeups must be >= -1")
		return 2
	}

	schedulePayload := map[string]interface{}{
		"session_id": session,
		"daily_at":   strings.TrimSpace(*dailyAt),
		"enabled":    true,
	}
	if tz := strings.TrimSpace(*timezone); tz != "" {
		schedulePayload["timezone"] = tz
	}
	scheduleBody, err := json.Marshal(schedulePayload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/schedule", string(scheduleBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	scheduleResp, ok := readDaemonCommandResponse(resp)
	if !ok {
		return 1
	}
	fmt.Println(scheduleResp)

	if *dailyWakeups >= 0 {
		budgetPayload := map[string]interface{}{
			"session_id":          session,
			"daily_wakeup_limit":  *dailyWakeups,
			"max_goal_auto_turns": 0,
		}
		budgetBody, err := json.Marshal(budgetPayload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		resp, err = daemonPost(*addr, *dir, "/budget", string(budgetBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
			return 1
		}
		budgetResp, ok := readDaemonCommandResponse(resp)
		if !ok {
			return 1
		}
		fmt.Println(budgetResp)
	}
	return 0
}

func daemonReleaseAssistCmd(args []string) int {
	fs := flag.NewFlagSet("daemon release-assist", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要配置发布助手的 session ID")
	paths := fs.String("paths", defaultReleaseAssistPaths(), "要等待的 changelog/version 文件，逗号分隔")
	ignore := fs.String("ignore", "", "额外忽略 glob，逗号分隔")
	debounce := fs.String("debounce", "3s", "文件变化防抖时间，例如 3s")
	subject := fs.String("subject", "release readiness", "等待对象说明")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	session := strings.TrimSpace(*sessionID)
	if session == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	watchPaths := splitCSV(*paths)
	if len(watchPaths) == 0 {
		fmt.Fprintln(os.Stderr, "error: --paths must include at least one file")
		return 2
	}
	payload := map[string]interface{}{
		"session_id": session,
		"paths":      watchPaths,
		"reason":     "release files changed; check changelog and version before publishing",
		"subject":    strings.TrimSpace(*subject),
	}
	if patterns := splitCSV(*ignore); len(patterns) > 0 {
		payload["ignore_patterns"] = patterns
	}
	if strings.TrimSpace(*debounce) != "" {
		payload["debounce"] = strings.TrimSpace(*debounce)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/wait-file", string(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	out, ok := readDaemonCommandResponse(resp)
	if !ok {
		return 1
	}
	fmt.Println(out)
	return 0
}

func defaultReleaseAssistPaths() string {
	return "CHANGELOG.md,package.json,package-lock.json,pnpm-lock.yaml,yarn.lock,go.mod,Cargo.toml,pyproject.toml"
}

func daemonRepoHealthCmd(args []string) int {
	fs := flag.NewFlagSet("daemon repo-health", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要配置仓库健康巡检的 session ID")
	dailyAt := fs.String("daily-at", "10:00", "每日仓库巡检唤醒时间 HH:MM")
	timezone := fs.String("timezone", "", "daily-at 使用的 IANA 时区，例如 Asia/Shanghai")
	dailyWakeups := fs.Int("daily-wakeups", 1, "每日自动唤醒次数上限，0 表示关闭限制，-1 表示不修改预算")
	maxGoalAutoTurns := fs.Int("max-goal-auto-turns", 0, "每个巡检 goal 最大自动续跑轮次，0 表示使用内置默认值，-1 表示不修改")
	dailyModelCalls := fs.Int("daily-model-calls", -1, "每日模型调用次数上限，0 表示关闭限制，-1 表示不修改")
	dailyModelCost := fs.Float64("daily-model-cost", -1, "每日模型费用上限，0 表示关闭限制，-1 表示不修改")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	session := strings.TrimSpace(*sessionID)
	if session == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	if strings.TrimSpace(*dailyAt) == "" {
		fmt.Fprintln(os.Stderr, "error: --daily-at is required")
		return 2
	}
	if *dailyWakeups < -1 {
		fmt.Fprintln(os.Stderr, "error: --daily-wakeups must be >= -1")
		return 2
	}
	if *maxGoalAutoTurns < -1 {
		fmt.Fprintln(os.Stderr, "error: --max-goal-auto-turns must be >= -1")
		return 2
	}
	if *dailyModelCalls < -1 {
		fmt.Fprintln(os.Stderr, "error: --daily-model-calls must be >= -1")
		return 2
	}
	if *dailyModelCost < -1 {
		fmt.Fprintln(os.Stderr, "error: --daily-model-cost must be >= -1")
		return 2
	}

	schedulePayload := map[string]interface{}{
		"session_id": session,
		"daily_at":   strings.TrimSpace(*dailyAt),
		"enabled":    true,
	}
	if tz := strings.TrimSpace(*timezone); tz != "" {
		schedulePayload["timezone"] = tz
	}
	scheduleBody, err := json.Marshal(schedulePayload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/schedule", string(scheduleBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	scheduleResp, ok := readDaemonCommandResponse(resp)
	if !ok {
		return 1
	}
	fmt.Println(scheduleResp)

	budgetPayload := map[string]interface{}{
		"session_id": session,
	}
	if *dailyWakeups >= 0 {
		budgetPayload["daily_wakeup_limit"] = *dailyWakeups
	}
	if *maxGoalAutoTurns >= 0 {
		budgetPayload["max_goal_auto_turns"] = *maxGoalAutoTurns
	}
	if *dailyModelCalls >= 0 {
		budgetPayload["daily_model_call_limit"] = *dailyModelCalls
	}
	if *dailyModelCost >= 0 {
		budgetPayload["daily_model_cost_limit"] = *dailyModelCost
	}
	if len(budgetPayload) > 1 {
		budgetBody, err := json.Marshal(budgetPayload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		resp, err = daemonPost(*addr, *dir, "/budget", string(budgetBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
			return 1
		}
		budgetResp, ok := readDaemonCommandResponse(resp)
		if !ok {
			return 1
		}
		fmt.Println(budgetResp)
	}
	return 0
}

func daemonWaitEventCmd(args []string) int {
	fs := flag.NewFlagSet("daemon wait-event", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要设置等待条件的 session ID")
	source := fs.String("source", "", "等待的事件来源，例如 github.workflow_run")
	eventID := fs.String("event-id", "", "等待的具体 event id")
	status := fs.String("status", "", "等待的事件状态，例如 completed")
	conclusion := fs.String("conclusion", "", "等待的事件结果，例如 success")
	reason := fs.String("reason", "", "等待原因")
	subject := fs.String("subject", "", "等待对象，例如 PR #42")
	clear := fs.Bool("clear", false, "清除当前 event wait 条件")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sessionID == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	if !*clear && *source == "" && *eventID == "" {
		fmt.Fprintln(os.Stderr, "error: --source or --event-id is required unless --clear is set")
		return 2
	}

	body := fmt.Sprintf(`{"session_id":%q`, *sessionID)
	if *clear {
		body += `,"clear":true`
	} else {
		if *source != "" {
			body += fmt.Sprintf(`,"event_source":%q`, *source)
		}
		if *eventID != "" {
			body += fmt.Sprintf(`,"event_id":%q`, *eventID)
		}
		if *status != "" {
			body += fmt.Sprintf(`,"event_status":%q`, *status)
		}
		if *conclusion != "" {
			body += fmt.Sprintf(`,"event_conclusion":%q`, *conclusion)
		}
		if *reason != "" {
			body += fmt.Sprintf(`,"reason":%q`, *reason)
		}
		if *subject != "" {
			body += fmt.Sprintf(`,"subject":%q`, *subject)
		}
	}
	body += "}"
	resp, err := daemonPost(*addr, *dir, "/wait-event", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonWaitTimeCmd(args []string) int {
	fs := flag.NewFlagSet("daemon wait-time", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要设置等待条件的 session ID")
	until := fs.String("until", "", "等待到 RFC3339 时间，例如 2026-06-13T10:00:00Z")
	after := fs.String("after", "", "从现在起等待多久，例如 30m 或 2h")
	reason := fs.String("reason", "", "等待原因")
	subject := fs.String("subject", "", "等待对象")
	clear := fs.Bool("clear", false, "清除当前 time wait 条件")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sessionID == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	if !*clear {
		if (*until == "" && *after == "") || (*until != "" && *after != "") {
			fmt.Fprintln(os.Stderr, "error: exactly one of --until or --after is required unless --clear is set")
			return 2
		}
	}

	body := fmt.Sprintf(`{"session_id":%q`, *sessionID)
	if *clear {
		body += `,"clear":true`
	} else {
		if *until != "" {
			body += fmt.Sprintf(`,"until":%q`, *until)
		}
		if *after != "" {
			body += fmt.Sprintf(`,"after":%q`, *after)
		}
		if *reason != "" {
			body += fmt.Sprintf(`,"reason":%q`, *reason)
		}
		if *subject != "" {
			body += fmt.Sprintf(`,"subject":%q`, *subject)
		}
	}
	body += "}"
	resp, err := daemonPost(*addr, *dir, "/wait-time", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonWaitFileCmd(args []string) int {
	fs := flag.NewFlagSet("daemon wait-file", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要设置等待条件的 session ID")
	pathsRaw := fs.String("paths", "", "等待变化的文件/目录/glob，逗号分隔")
	ignoreRaw := fs.String("ignore", "", "额外忽略 glob，逗号分隔")
	debounce := fs.String("debounce", "", "文件变化防抖时间，例如 3s")
	reason := fs.String("reason", "", "等待原因")
	subject := fs.String("subject", "", "等待对象")
	clear := fs.Bool("clear", false, "清除当前 file wait 条件")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sessionID == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	paths := splitCSV(*pathsRaw)
	if !*clear && len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "error: --paths is required unless --clear is set")
		return 2
	}

	payload := map[string]interface{}{
		"session_id": *sessionID,
	}
	if *clear {
		payload["clear"] = true
	} else {
		payload["paths"] = paths
		if ignore := splitCSV(*ignoreRaw); len(ignore) > 0 {
			payload["ignore_patterns"] = ignore
		}
		if *debounce != "" {
			payload["debounce"] = *debounce
		}
		if *reason != "" {
			payload["reason"] = *reason
		}
		if *subject != "" {
			payload["subject"] = *subject
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/wait-file", string(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonDisableWatchCmd(args []string) int {
	fs := flag.NewFlagSet("daemon disable-watch", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "要关闭文件监听的 session ID")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sessionID) == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	body, err := json.Marshal(map[string]interface{}{
		"session_id": strings.TrimSpace(*sessionID),
		"enabled":    false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	resp, err := daemonPost(*addr, *dir, "/watch", string(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	if out, ok := readDaemonCommandResponse(resp); ok {
		fmt.Println(out)
		return 0
	}
	return 1
}

func daemonApprovalCmd(args []string, allow bool) int {
	name := "daemon approve"
	path := "/approvals/approve"
	if !allow {
		name = "daemon deny"
		path = "/approvals/deny"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "session ID")
	approvalID := fs.String("approval", "", "approval ID")
	sessionGrant := fs.Bool("session-grant", false, "本 session 内记住该批准范围")
	persist := fs.Bool("persist", false, "持久化批准规则")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sessionID == "" || *approvalID == "" {
		fmt.Fprintln(os.Stderr, "error: --session and --approval are required")
		return 2
	}
	body := fmt.Sprintf(`{"session_id":%q,"approval_id":%q,"session":%t,"persist":%t}`, *sessionID, *approvalID, *sessionGrant, *persist)
	resp, err := daemonPost(*addr, *dir, path, body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonAnswerCmd(args []string) int {
	fs := flag.NewFlagSet("daemon answer", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	sessionID := fs.String("session", "", "session ID")
	askID := fs.String("ask", "", "ask ID")
	selected := fs.String("selected", "", "选择/回答文本")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sessionID == "" || *askID == "" || *selected == "" {
		fmt.Fprintln(os.Stderr, "error: --session, --ask and --selected are required")
		return 2
	}
	body := fmt.Sprintf(`{"session_id":%q,"ask_id":%q,"selected":%q}`, *sessionID, *askID, *selected)
	resp, err := daemonPost(*addr, *dir, "/asks/answer", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not reachable: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func daemonGet(addr, dir, path string) (*http.Response, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	url := "http://" + addr + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	addDaemonAuth(req, dir)
	return client.Do(req)
}

func daemonPost(addr, dir, path, body string) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := "http://" + addr + path
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	addDaemonAuth(req, dir)
	return client.Do(req)
}

func readDaemonCommandResponse(resp *http.Response) (string, bool) {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(b))
		return "", false
	}
	return string(b), true
}

func addDaemonAuth(req *http.Request, dir string) {
	token := readDaemonToken(dir)
	if token != "" {
		req.Header.Set("X-Reasonix-Daemon-Token", token)
	}
}

func readDaemonToken(dir string) string {
	if strings.TrimSpace(dir) == "" {
		dir = config.SessionDir()
	}
	b, err := os.ReadFile(daemon.TokenFile(dir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func daemonUsage() {
	fmt.Print(`reasonix daemon — 常驻后台 agent 服务

Usage:
  reasonix daemon start    [--addr HOST:PORT] [--dir PATH] [--log-file PATH|none] [--log-max-size 10MB] [--bot --bot-channels qq,feishu,weixin] [--webhook --webhook-secret SECRET]
  reasonix daemon status   [--addr HOST:PORT] [--dir PATH]
  reasonix daemon doctor   [--addr HOST:PORT] [--dir PATH] [--log-file PATH|none] [--json]
  reasonix daemon startup  install|uninstall|print [--exe PATH] [--addr HOST:PORT] [--dir PATH] [--log-file PATH|none]
  reasonix daemon token rotate [--dir PATH]
  reasonix daemon sessions [--addr HOST:PORT] [--dir PATH] [--scope global|project] [--workspace-root PATH] [--status running|waiting|blocked|active|scheduled|watched] [--json]
  reasonix daemon approvals [--addr HOST:PORT] [--dir PATH] [--json]
  reasonix daemon timeline --session ID [--limit N] [--json]
  reasonix daemon continue --session ID [--addr HOST:PORT] [--dir PATH]
	reasonix daemon schedule (--session ID | --scope global|project [--workspace-root PATH]) [--daily-at HH:MM] [--timezone Area/City] [--interval 1h]
	reasonix daemon disable-schedule (--session ID | --scope global|project [--workspace-root PATH])
  reasonix daemon budget   (--session ID | --scope global|project [--workspace-root PATH]) [--daily-wakeups N] [--max-goal-auto-turns N] [--daily-model-calls N] [--daily-model-cost N] [--reset]
  reasonix daemon budgets  [--addr HOST:PORT] [--dir PATH]
  reasonix daemon templates [--json]
  reasonix daemon apply-template --template daily-triage|ci-watcher|release-assist|repo-health --session ID [template options]
  reasonix daemon daily-triage --session ID [--daily-at HH:MM] [--timezone Area/City] [--daily-wakeups N]
  reasonix daemon ci-watch --session ID [--source workflow_run|check_suite|status] [--repo owner/repo] [--pr N]
  reasonix daemon release-assist --session ID [--paths CHANGELOG.md,package.json] [--debounce 3s]
  reasonix daemon repo-health --session ID [--daily-at HH:MM] [--timezone Area/City] [--daily-wakeups N]
  reasonix daemon wait-event --session ID --source TYPE [--event-id ID] [--status completed] [--conclusion success]
  reasonix daemon wait-time --session ID (--until RFC3339 | --after 1h)
  reasonix daemon wait-file --session ID --paths PATH[,PATH...] [--ignore GLOB[,GLOB...]]
  reasonix daemon disable-watch --session ID
  reasonix daemon approve  --session ID --approval ID
  reasonix daemon deny     --session ID --approval ID
  reasonix daemon answer   --session ID --ask ID --selected TEXT
  reasonix daemon stop     --session ID [--addr HOST:PORT] [--dir PATH]

Subcommands:
  start      启动 daemon（前台运行，Ctrl-C 停止）
  status     查询 daemon 状态
  doctor     检查 daemon token、lock、runtime sidecar 和在线状态
  startup    安装、卸载或打印用户级开机/登录自启动 helper
  token      管理本地 daemon API token
  sessions   列出所有跟踪的 session 及其 goal/run 状态
  approvals  列出等待审批或 ask 回答的 daemon 待办
  timeline   查看指定 session 的运行事件时间线
  continue   显式唤醒并继续指定 goal
  schedule   设置 daily/interval 定时唤醒和 daily 时区
  disable-schedule 关闭 session/project/global 调度但保留配置
  budget     设置自动唤醒、模型调用、模型费用、goal 自动续跑或 project/global 聚合预算
  budgets    查看 project/global 聚合预算视图
  templates  列出可复制的个人 AgentOS 场景模板
  apply-template 将模板配置应用到已有 session，并输出 goal starter
  daily-triage 配置每日 PR / issue triage 场景
  ci-watch   配置“等 GitHub CI 成功后继续”的个人 AgentOS 场景
  release-assist 配置发布文件变化后检查 changelog / version 的发布助手场景
  repo-health 配置每日仓库健康巡检场景
  wait-event 设置或清除等待外部事件条件
  wait-time  设置或清除等待到指定时间的条件
  wait-file  设置或清除等待文件变化的条件
  disable-watch 关闭 session 文件监听但保留配置
  approve    批准 daemon 中等待的审批
  deny       拒绝 daemon 中等待的审批
  answer     回答 daemon 中等待的 ask 问题
  stop       停止指定 session 的目标

The daemon scans session directories for *.runtime.json files, recovers
interrupted sessions, and exposes a localhost HTTP API for status queries
and goal continuation. The local HTTP API uses a token stored beside the
session directory. Webhooks require an HMAC secret supplied by
--webhook-secret or REASONIX_DAEMON_WEBHOOK_SECRET.
`)
}

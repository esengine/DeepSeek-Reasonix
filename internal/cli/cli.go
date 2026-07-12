// Package cli implements reasonix's command-line entry: subcommand routing, flag
// parsing, assembly from config, and exit codes. The core is config-driven —
// providers and tools are resolved from configuration, not hardcoded.
package cli

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/i18n"
	"reasonix/internal/notify"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
	"reasonix/internal/sandbox"
	"reasonix/internal/serve"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

var (
	runInteractiveSession = chatREPL
	cliIsInteractive      = isInteractive
)

// Run is the CLI entry point; it returns a process exit code.
func Run(args []string, version string) int {
	// This binary routes the hidden Windows sandbox helper subcommand below;
	// registering that fact is what lets sandbox.Available() report true.
	sandbox.RegisterHelperDispatch()
	if len(args) > 0 && args[0] == sandbox.WindowsHelperCommand {
		return sandbox.RunWindowsSandboxHelper(args[1:], os.Stdin, os.Stdout, os.Stderr)
	}
	// Pick the UI language up front so even pre-config paths (the first-run
	// welcome banner) come through localized. Env-only first; if a config
	// exists and pins a language, that wins.
	i18n.DetectLanguage("")
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	if cmd == "--acp" {
		cmd = "acp"
	}
	if len(args) > 0 && isDefaultInteractiveFlag(cmd) {
		cmd = ""
	}
	if shouldMigrateLegacyConfigForCLI(cmd) {
		migrateLegacyConfigForCLI()
	}
	if cfg, err := config.Load(); err == nil {
		if cfg.Language != "" {
			i18n.DetectLanguage(cfg.Language)
		}
	}

	if len(args) == 0 && cliIsInteractive() {
		return runInteractiveSession(nil)
	}
	if len(args) == 0 {
		configureCLIThemeFromConfigForTTYOutput()
		usage()
		return 0
	}
	if cmd == "" {
		return runInteractiveSession(args)
	}

	rest := args[1:]
	switch cmd {
	case "run":
		return runAgent(rest)
	case "chat", "code": // "code" is the v0.x name for the interactive session
		return runInteractiveSession(rest)
	case "serve":
		return runServe(rest)
	case "setup":
		configureCLIThemeFromConfigForTTYOutput()
		return setupConfig(rest)
	case "config":
		configureCLIThemeFromConfigNoProbe()
		return configCommand(rest)
	case "init":
		// Project memory (AGENTS.md) is model-generated in-session — `/init` runs
		// the codebase analysis. This CLI entry just points there (and to `setup`
		// for config), so `reasonix init` isn't a dead end.
		configureCLIThemeFromConfigNoProbe()
		return initHint()
	case "acp":
		configureCLIThemeFromConfigNoProbe()
		return acpCommand(rest, version)
	case "mcp":
		configureCLIThemeFromConfigNoProbe()
		return mcpCommand(rest)
	case "plugin":
		configureCLIThemeFromConfigNoProbe()
		return pluginCommand(rest)
	case "subagent":
		configureCLIThemeFromConfigForTTYOutput()
		return subagentCommand(rest)
	case "doctor":
		configureCLIThemeFromConfigNoProbe()
		return doctorCommand(rest, version)
	case "review":
		configureCLIThemeFromConfigNoProbe()
		return reviewCommand(rest)
	case "bot":
		configureCLIThemeFromConfigNoProbe()
		return botCommand(rest, version)
	case "upgrade", "update":
		configureCLIThemeFromConfigNoProbe()
		return upgradeCommand(rest, version)
	case "version", "--version", "-v":
		fmt.Println("reasonix", version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, i18n.M.UnknownCommandFmt+"\n\n", cmd)
		usage()
		return 2
	}
}

func isDefaultInteractiveFlag(arg string) bool {
	switch arg {
	case "--model", "--max-steps", "--continue", "-c", "--resume", "--copy", "--dangerously-skip-permissions", "--yolo", "--dir":
		return true
	}
	if name, _, ok := strings.Cut(arg, "="); ok && isDefaultInteractiveFlag(name) {
		return true
	}
	return false
}

func shouldMigrateLegacyConfigForCLI(cmd string) bool {
	switch cmd {
	case "", "run", "chat", "code", "serve", "setup", "config", "init", "acp", "mcp", "plugin", "subagent", "doctor", "bot", "upgrade", "update":
		return true
	default:
		return false
	}
}

func migrateLegacyConfigForCLI() {
	if _, err := config.MigrateLegacyIfNeeded(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: config migration failed:", err)
	}
	if _, err := config.ApplyUserConfigUpgradesOnStartup(config.UserConfigPath()); err != nil {
		fmt.Fprintln(os.Stderr, "warning: config upgrade failed:", err)
	}
}

func migrateMCPConfigForCLIWorkspace() {
	if wd, err := os.Getwd(); err == nil {
		if _, err := config.MigrateMCPToUserConfigOnUpgrade([]string{wd}); err != nil {
			fmt.Fprintln(os.Stderr, "warning: MCP config migration failed:", err)
		}
	}
}

func configureCLIThemeFromConfig() {
	if cfg, err := config.Load(); err == nil {
		configureCLIThemeWithStyle(cfg.UITheme(), cfg.UIThemeStyle())
		cliCursorShape = cfg.UICursorShape()
	} else {
		configureCLITheme("auto")
		cliCursorShape = "underline"
	}
}

func configureCLIThemeFromConfigForTTYOutput() {
	if isTTY(os.Stdout) {
		configureCLIThemeFromConfig()
		return
	}
	configureCLIThemeFromConfigNoProbe()
}

func configureCLIThemeFromConfigNoProbe() {
	withoutTerminalProbe(configureCLIThemeFromConfig)
}

// setupProfile builds a ready-to-drive Controller from config via boot.Build.
// The assembly (model resolution, tool registry, permission gate, two-model
// Coordinator) lives in internal/boot, shared with the desktop frontend.
// requireKey forces the executor's API key to be present (used by run); chat
// passes false so the session UI is reachable before a key is set. sink receives
// the agent's typed event stream — runAgent passes a TextSink that renders to
// stdout, the TUI passes an event-channel sink so events become tea.Msgs.
// profile selects economy|balanced|delivery (empty = balanced/full).
func setupProfile(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink, profile string) (*control.Controller, error) {
	migrateMCPConfigForCLIWorkspace()
	return boot.Build(ctx, boot.Options{
		Model:      modelName,
		MaxSteps:   maxStepsOverride,
		RequireKey: requireKey,
		Sink:       sink,
		TokenMode:  profile,
		SessionDir: resolveCLISessionDir(),
	})
}

// resolveCLISessionDir returns the session dir for CLI invocations. When the
// current working directory maps to a project session dir, the project dir is
// used so /resume shows project history. Falls back to the global session dir.
func resolveCLISessionDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return config.SessionDir()
	}
	if projDir := config.ProjectSessionDir(cwd); projDir != "" && projDir != config.SessionDir() {
		return projDir
	}
	return config.SessionDir()
}

// setupQuietProfile is like setupProfile but suppresses plugin subprocess
// stderr. Used during model switch inside a bubbletea session to prevent plugin
// logs from corrupting the TUI's terminal raw mode.
func setupQuietProfile(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink, profile string) (*control.Controller, error) {
	return boot.Build(ctx, boot.Options{
		Model:      modelName,
		MaxSteps:   maxStepsOverride,
		RequireKey: requireKey,
		Sink:       sink,
		Stderr:     io.Discard,
		TokenMode:  profile,
	})
}

func parseRuntimeProfile(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "balanced", boot.TokenModeFull:
		return boot.TokenModeFull, nil
	case boot.TokenModeEconomy:
		return boot.TokenModeEconomy, nil
	case boot.TokenModeDelivery:
		return boot.TokenModeDelivery, nil
	default:
		return "", fmt.Errorf("unknown runtime profile %q (want economy, balanced, or delivery)", value)
	}
}

// chdirTo honours --dir: it switches the working directory before anything reads
// it, so config discovery, the sandbox root, and file tools all resolve from the
// chosen project root. Returns 2 (already reported) on failure, 0 otherwise.
func chdirTo(dir string) int {
	if dir == "" {
		return 0
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	return 0
}

func modelForResumePath(modelName, resumePath string, cfg *config.Config) string {
	if strings.TrimSpace(modelName) != "" || strings.TrimSpace(resumePath) == "" {
		return modelName
	}
	sessionModel, ok := agent.LoadSessionModel(resumePath)
	if !ok {
		return modelName
	}
	if cfg == nil {
		return sessionModel
	}
	if _, ok := cfg.ResolveModel(sessionModel); !ok {
		return modelName
	}
	return sessionModel
}

func loadResumableSession(path string) (*agent.Session, error) {
	if agent.IsCleanupPending(path) {
		return nil, fmt.Errorf("session is pending cleanup")
	}
	return agent.LoadSession(path)
}

var newNotificationSender = func() notify.Sender { return notify.NewPlatformSender() }

// withNotifications adds system notifications to CLI event streams when configured.
func withNotifications(sink event.Sink, cfg *config.Config) event.Sink {
	if cfg == nil || !cfg.Notifications.Enabled {
		return sink
	}
	return notify.NewSink(sink, newNotificationSender(), cfg.Notifications)
}

func runAgent(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	profileFlag := fs.String("profile", "balanced", "runtime profile: economy | balanced | delivery")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	showThinking := fs.Bool("show-thinking", false, "show thinking text instead of the collapsed thinking marker")
	metricsPath := fs.String("metrics", "", "write a JSON token/cache/cost summary of the run to this path")
	dir := fs.String("dir", "", "change to this directory first (project root); config, sandbox and file tools resolve from here")
	cont := fs.Bool("continue", false, "resume the most recent saved session")
	fs.BoolVar(cont, "c", false, "shorthand for --continue")
	resume := fs.String("resume", "", "resume a specific session file (non-interactive; takes precedence over --continue)")
	copySession := fs.Bool("copy", false, "with --resume/--continue: duplicate the session and continue in the copy (escape hatch when the original is held by another Reasonix process)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	profile, err := parseRuntimeProfile(*profileFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}
	cfg, _ := config.Load()
	configureCLIThemeFromConfigForTTYOutput()

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		prompt = readStdin()
	}
	if prompt == "" {
		fmt.Fprintln(os.Stderr, i18n.M.UsageRunHint)
		return 2
	}

	// Resolve the resume target up front so --copy and the session lease can be
	// handled before any heavy assembly. --resume takes precedence over
	// --continue, matching the Resume call below.
	resumePath := strings.TrimSpace(*resume)
	if resumePath == "" && *cont {
		sessions, err := agent.ListSessions(resolveCLISessionDir())
		if err != nil || len(sessions) == 0 {
			fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResume)
			return 1
		}
		resumePath = sessions[0].Path
	}
	if *copySession && resumePath == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--copy requires --resume or --continue")
		return 2
	}
	if *copySession {
		copied, err := copySessionForWriting(resumePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("continuing in a session copy: %s\n", copied)
		resumePath = copied
	}

	// Own the session file for the lifetime of this run so a desktop window (or
	// another CLI) writing the same session is refused up front instead of
	// silently double-writing. Released after the controller closes.
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	var resumeSession *agent.Session
	if resumePath != "" {
		if err := leases.Rebind(resumePath); err != nil {
			if errors.Is(err, agent.ErrSessionLeaseHeld) {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, sessionLeaseResumeRefusal(err))
			} else {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			}
			return 1
		}
		var err error
		resumeSession, err = loadResumableSession(resumePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	// Live run: render the agent's event stream to stdout. Markdown post-stream
	// redraw (cursor moves) is enabled only on a TTY; piped / captured output
	// keeps the raw stream.
	var renderer agent.Renderer
	termW := 80
	if isTTY(os.Stdout) {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			termW = w
		}
		renderer = newMarkdownRenderer(termW)
	}
	textSink := agent.NewTextSink(os.Stdout, renderer, termW)
	textSink.SetShowReasoning(*showThinking)
	var sink event.Sink = textSink
	var metrics *metricsSink
	if *metricsPath != "" {
		metrics = &metricsSink{inner: textSink}
		sink = metrics
	}
	sink = withNotifications(sink, cfg)
	if resumePath != "" {
		*model = modelForResumePath(*model, resumePath, cfg)
	}
	ctrl, err := setupProfile(ctx, *model, *maxSteps, true, sink, profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()

	// --resume: load a specific session file (non-interactive, meant for
	// MCP/API callers that manage their own per-project session). Takes
	// precedence over --continue.
	// --continue: resume the most recent saved session.
	if resumePath != "" {
		ctrl.Resume(resumeSession, resumePath)
	}
	if ctrl.SessionPath() == "" && ctrl.SessionDir() != "" {
		ctrl.SetSessionPath(agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	}
	// Fresh sessions take the lease too (defensive: the path is brand new); a
	// resumed path is already held, making this a no-op.
	if err := leases.Rebind(ctrl.SessionPath()); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, control.SessionInUseMessage(err)+"; "+control.SessionLeaseCloseHint)
		return 1
	}

	runErr := ctrl.Run(ctx, prompt)
	if cfg != nil {
		notify.SendEvent(newNotificationSender(), cfg.Notifications, event.Event{Kind: event.TurnDone, Err: runErr})
	}
	if metrics != nil {
		if exec := ctrl.Executor(); exec != nil {
			if audit := exec.CapabilityAudit(); audit != nil {
				snap := audit.Snapshot()
				metrics.m.MergeCapabilityAuditCounters(
					snap.Routes, snap.RoutedCandidates, snap.RoutedRequire, snap.RoutedPrefer, snap.RoutedSuggest, snap.Declines,
					snap.SemanticRoutes, snap.SemanticFallbacks,
					snap.RequireMissing, snap.RequireRecovered, snap.PreferMissing, snap.PreferRecovered,
					snap.SkillInvocations, snap.SkillFailures, snap.SkillUnavailable,
					snap.MCPInspect, snap.MCPCall, snap.MCPCallFailures,
					snap.ReviewBlocks, snap.SecurityReviewBlocks,
					snap.RouterPromptTokens, snap.RouterCompletionTokens,
					snap.RouterCost, snap.RouterLatencyMs,
				)
			}
		}
		if err := writeMetrics(*metricsPath, metrics.m); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "\n"+i18n.M.ErrorPrefix, runErr)
		return 1
	}
	return 0
}

// runServe exposes the controller over HTTP+SSE: events stream to the browser,
// commands arrive as JSON POSTs. The Broadcaster is the controller's event sink,
// so the same typed stream the chat TUI consumes reaches web clients — the
// transport-agnostic controller driven by a second frontend.
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	profileFlag := fs.String("profile", "balanced", "runtime profile: economy | balanced | delivery")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	resume := fs.String("resume", "", "resume a saved session file")
	auth := fs.String("auth", "", "auth mode: none, token, or password (default: none)")
	token := fs.String("token", "", "pre-shared token for auth=token (auto-generated if empty)")
	password := fs.String("password", "", "password for auth=password (use --hash-password to store a hash instead)")
	hashPassword := fs.Bool("hash-password", false, "print a bcrypt hash of --password and exit")
	behindProxy := fs.Bool("behind-proxy", false, "trust X-Forwarded-For / X-Forwarded-Proto headers from a reverse proxy")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	profile, err := parseRuntimeProfile(*profileFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}

	// --hash-password: generate a bcrypt hash and exit.
	if *hashPassword {
		if *password == "" {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--hash-password requires --password")
			return 1
		}
		h, err := serve.HashPassword(*password)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Println(h)
		return 0
	}

	ctx := context.Background()
	bc := serve.NewBroadcaster()
	cfg, _ := config.Load()

	// Build serve config, merging CLI flags over config file.
	serveCfg := cfg.Serve
	if *auth != "" {
		serveCfg.AuthMode = *auth
	}
	if *token != "" {
		serveCfg.Token = *token
	}
	if *behindProxy {
		serveCfg.BehindProxy = true
	}
	mode, err := serve.NormalizeAuthMode(serveCfg.AuthMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	serveCfg.AuthMode = mode
	if *password != "" && serveCfg.AuthMode == "password" {
		// Hash the password at startup so the config never stores plaintext.
		// If a PasswordHash is already set in config, the CLI password overrides it.
		h, err := serve.HashPassword(*password)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "failed to hash password:", err)
			return 1
		}
		serveCfg.PasswordHash = h
	}
	if serveCfg.AuthMode == "password" && strings.TrimSpace(serveCfg.PasswordHash) == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "auth mode password requires --password or serve.password_hash")
		return 1
	}

	// Own the active session file for the server's lifetime; the serve
	// handlers that rebind sessions (/resume, /new, /fork) move the lease
	// through the same keeper. Released after the controller closes.
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	var resumeSession *agent.Session
	if *resume != "" {
		if err := leases.Rebind(*resume); err != nil {
			if errors.Is(err, agent.ErrSessionLeaseHeld) {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, control.SessionInUseMessage(err)+"; "+control.SessionLeaseCloseHint)
			} else {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			}
			return 1
		}
		var err error
		resumeSession, err = loadResumableSession(*resume)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
	}
	*model = modelForResumePath(*model, *resume, cfg)
	// Serve always uses the user's global default_model, ignoring any
	// project-level override, so the model choice stays consistent across
	// projects and matches the user's account-level preference.
	if *model == "" {
		if uc := config.UserConfigPath(); uc != "" {
			if userCfg := config.LoadForEdit(uc); userCfg != nil && userCfg.DefaultModel != "" {
				*model = userCfg.DefaultModel
			}
		}
	}
	ctrl, err := setupProfile(ctx, *model, *maxSteps, true, bc, profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()

	// Auto-save target: reuse the resumed file, else a fresh one — same as chat.
	if *resume != "" {
		ctrl.Resume(resumeSession, *resume)
	}
	ctrl.EnsureSessionPath()
	// Fresh sessions take the lease too (defensive: the path is brand new); a
	// resumed path is already held, making this a no-op.
	if err := leases.Rebind(ctrl.SessionPath()); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, control.SessionInUseMessage(err)+"; "+control.SessionLeaseCloseHint)
		return 1
	}

	srv := serve.New(ctrl, bc, serveCfg)
	srv.SetSessionLeases(leases)
	fmt.Printf("reasonix serve — %s on http://%s\n", ctrl.Label(), *addr)
	if srv.AuthMode() == "token" {
		fmt.Printf("  auth: token\n")
		fmt.Printf("  share: http://%s/?token=%s\n", *addr, srv.AuthToken())
	} else if srv.AuthMode() == "password" {
		fmt.Printf("  auth: password (login at http://%s/login)\n", *addr)
	}
	if warning := serve.PlainHTTPAuthWarning(serveCfg, *addr); warning != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", warning)
	}
	// Diagnostic: check whether balance endpoint is reachable
	if b, err := ctrl.Balance(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "  balance: error — %v\n", err)
	} else if b == nil {
		fmt.Fprintf(os.Stderr, "  balance: not configured (no balance_url for this provider)\n")
	} else {
		fmt.Printf("  balance: %s\n", b.Display())
	}

	// Use graceful shutdown so SIGINT/SIGTERM drain active connections.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.RunGraceful(ctx, *addr); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	return 0
}

// chatREPL is an interactive session: a single persistent agent/session and a
// prompt loop that keeps conversation context across turns. Exit with
// 'exit'/'quit' or Ctrl-D.
func chatREPL(args []string) int {
	fs := flag.NewFlagSet("reasonix", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	profileFlag := fs.String("profile", "balanced", "runtime profile: economy | balanced | delivery")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	cont := fs.Bool("continue", false, "resume the most recent saved session")
	fs.BoolVar(cont, "c", false, "shorthand for --continue")
	resume := fs.Bool("resume", false, "list saved sessions and pick one to resume")
	copySession := fs.Bool("copy", false, "with --resume/--continue: duplicate the selected session and continue in the copy (escape hatch when the original is held by another Reasonix process)")
	yolo := fs.Bool("dangerously-skip-permissions", false, "YOLO: auto-approve approval-gated tool calls this session; same runtime mode as Ctrl+Y")
	fs.BoolVar(yolo, "yolo", false, "alias for --dangerously-skip-permissions")
	dir := fs.String("dir", "", "change to this directory first (project root); config, sandbox and file tools resolve from here")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	profile, err := parseRuntimeProfile(*profileFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}
	cfg, err := config.Load()
	if err == nil {
		configureCLIThemeWithStyle(cfg.UITheme(), cfg.UIThemeStyle())
		cliCursorShape = cfg.UICursorShape()
	}

	// Decide whether we're starting fresh or resuming. --resume opens an
	// interactive picker; --continue / -c jumps straight into the newest.
	var resumePath string
	switch {
	case *resume:
		path, rc := pickSessionToResume()
		if rc != 0 {
			return rc
		}
		resumePath = path
	case *cont:
		sessions, err := agent.ListSessions(resolveCLISessionDir())
		if err != nil || len(sessions) == 0 {
			fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResume)
			return 1
		}
		resumePath = sessions[0].Path
	}
	if *copySession && resumePath == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--copy requires --resume or --continue")
		return 2
	}
	if *copySession {
		copied, err := copySessionForWriting(resumePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("continuing in a session copy: %s\n", copied)
		resumePath = copied
	}

	// Own the active session file for the TUI's lifetime; in-TUI switches
	// (/resume, /switch, /new, ...) move the lease with the active path.
	// Refusing a held resume target up front is what keeps a desktop window
	// and this chat from silently double-writing one transcript.
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if resumePath != "" {
		if err := leases.Rebind(resumePath); err != nil {
			if errors.Is(err, agent.ErrSessionLeaseHeld) {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, sessionLeaseResumeRefusal(err))
			} else {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			}
			return 1
		}
	}

	ctx := context.Background()
	*model = modelForResumePath(*model, resumePath, cfg)

	// Plumb the controller's typed event stream through a channel so each event
	// can become a tea.Msg inside the TUI's update loop. Buffered generously:
	// streaming bursts (tool results, long answers) shouldn't backpressure the
	// agent goroutine.
	eventCh := make(chan event.Event, 1024)

	var sink event.Sink = &eventSink{ch: eventCh}
	sink = withNotifications(sink, cfg)
	ctrl, err := setupProfile(ctx, *model, *maxSteps, false, sink, profile)
	if err != nil && errors.Is(err, boot.ErrUnknownModel) && isInteractive() && config.SourcePath() == "" {
		// True first run whose default model can't resolve: guide setup, then retry.
		// With a config present, fall through to the descriptive error — re-running
		// the wizard would overwrite the user's config (#2856).
		fmt.Fprintln(os.Stderr, i18n.M.ReconfigureOnUnknownModel)
		if rc := interactiveSetup(defaultConfigTarget(), defaultEnvTarget()); rc != 0 {
			return rc
		}
		ctrl, err = setupProfile(ctx, *model, *maxSteps, false, sink, profile)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}

	// Decide where this conversation's auto-save lands. A resume reuses the
	// file so closing/reopening keeps appending to the same history; a fresh
	// session lands in a new file stamped with the model name.
	if resumePath != "" {
		loaded, err := agent.LoadSession(resumePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		ctrl.Resume(loaded, resumePath)
	}
	ctrl.EnsureSessionPath()
	// Fresh sessions take the lease too (defensive: the path is brand new); a
	// resumed path is already held, making this a no-op.
	if err := leases.Rebind(ctrl.SessionPath()); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, control.SessionInUseMessage(err)+"; "+control.SessionLeaseCloseHint)
		return 1
	}

	// Surface a missing-key warning inside the TUI banner so the first message
	// failing is at least pre-announced; the user can still enter chat.
	missing := ""
	if cfg, loadErr := config.Load(); loadErr == nil {
		name := *model
		if name == "" {
			name = cfg.DefaultModel
		}
		if vErr := cfg.Validate(name); vErr != nil {
			missing = vErr.Error()
		}
	}

	// Initial terminal width — the TUI re-flows on every WindowSizeMsg so
	// this is just a starting estimate before the first resize event lands.
	termW := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termW = w
	}

	// Route "ask" decisions to the TUI: the controller emits an ApprovalRequest
	// event and blocks until the user answers via ctrl.Approve. Sub-agents (the
	// task tool) keep their headless gate from setup — no UI to prompt through.
	ctrl.EnableInteractiveApproval()
	// YOLO: skip every tool approval request for the session (deny rules still
	// apply; ask questions and plan approvals still wait for the user).
	if *yolo {
		ctrl.SetAutoApproveTools(true)
	}

	m := newChatTUI(ctrl, missing, eventCh, termW)
	m.leases = leases
	if cfg, err := config.Load(); err == nil {
		m.outputStyle = cfg.Agent.OutputStyle    // shown as the active entry in /output-style
		m.statuslineCmd = cfg.Statusline.Command // custom status-line command, "" = built-in row
		m.showReasoning = cfg.UI.ShowReasoning   // /verbose persistence: start with config default
		m.cfg = cfg
	}

	// /model support: a pure builder the TUI calls to rebuild on a different
	// model (carrying the conversation). It must NOT touch the running model —
	// runModelSubcommand performs the swap on the live copy. The same stable sink
	// feeds the new controller, so events keep flowing to this TUI.
	m.buildController = func(spec controllerBuildSpec, carry []provider.Message, resumePath string) (*control.Controller, error) {
		c, err := setupQuietProfile(ctx, spec.ModelRef, *maxSteps, false, sink, spec.RuntimeProfile)
		if err != nil {
			return nil, err
		}
		// Keep the carried conversation in its existing file so the switch doesn't
		// orphan a duplicate (#2807).
		path := agent.ContinueSessionPath(resumePath, c.SessionDir(), c.Label())
		c.AdoptHistory(carry, path)
		c.EnableInteractiveApproval()
		c.SetPlanMode(spec.PlanMode)
		if spec.ToolApprovalMode != "" {
			c.SetToolApprovalMode(spec.ToolApprovalMode)
		}
		return c, nil
	}
	m.runtimeProfile = profile
	if cfg, e := config.Load(); e == nil {
		name := *model
		if name == "" {
			name = cfg.DefaultModel
		}
		if entry, ok := cfg.ResolveModel(name); ok {
			m.modelRef = entry.Name + "/" + entry.Model
		}
	}
	m.refreshEffortStatus()

	if m.nativeScrollback {
		prepareNativeScrollback(os.Stdout, m.bottomRows())
	}

	// Non-Termux terminals use an alt-screen transcript viewport. Termux stays
	// in the normal buffer so native touch scrollback and soft-keyboard focus
	// keep working; finalized transcript lines are emitted via tea.Println.
	p := tea.NewProgram(m)
	// SSH drop (SIGHUP) or service stop (SIGTERM): persist the conversation
	// before the terminal goes away, then unwind through the normal close path
	// so resume picks up the interrupted session (#3772).
	hangup := make(chan os.Signal, 1)
	signal.Notify(hangup, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		for range hangup {
			p.Send(tuiShutdownMsg{})
		}
	}()
	final, runErr := p.Run()
	signal.Stop(hangup)
	// Close the active controller plus any retired ones from /model switches.
	// Retired controllers were stashed rather than closed at switch time
	// because Controller.Close() runs SessionEnd hooks and kills plugin
	// subprocesses — operations that corrupt bubbletea's terminal raw mode
	// when executed while the TUI is alive.
	if fm, ok := final.(chatTUI); ok {
		for _, oc := range fm.oldControllers {
			oc.Close()
		}
		if fm.ctrl != nil {
			fm.ctrl.Close()
		} else {
			ctrl.Close()
		}
	} else {
		ctrl.Close()
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, runErr)
		return 1
	}
	return 0
}

func prepareNativeScrollback(w io.Writer, rows int) {
	// Clear the terminal's scrollback history so a reopened chat starts
	// with a clean slate (Termux stays in the normal buffer, so prior
	// output would otherwise remain visible above the banner).
	fmt.Fprint(w, "\x1B[3J\x1B[2J\x1B[H")
	reserveNativeScrollbackFrame(w, rows)
}

func reserveNativeScrollbackFrame(w io.Writer, rows int) {
	for i := 0; i < rows; i++ {
		fmt.Fprintln(w)
	}
}

// setupTargets is where the wizard writes: the TOML config and the credential
// store. Keys always go to Reasonix's global .env so they
// never land in a project's own .env; only the config location is project-local
// under --local.
type setupTargets struct {
	config string
	env    string
}

// defaultConfigTarget is the user-global config file, falling back to a
// project-local reasonix.toml only when the user config dir can't be resolved.
func defaultConfigTarget() string {
	if p := config.UserConfigPath(); p != "" {
		return p
	}
	return "reasonix.toml"
}

// defaultEnvTarget is the display target for the reasonix-owned global
// Reasonix global .env.
func defaultEnvTarget() string {
	return config.CredentialsTargetDescription()
}

// resolveSetupTargets picks where `reasonix setup` writes. Keys always go to the
// global env. The config goes to the user-global dir by default, to ./reasonix.toml
// under --local, or to an explicit path argument when given.
func resolveSetupTargets(args []string) setupTargets {
	t := setupTargets{config: defaultConfigTarget(), env: defaultEnvTarget()}
	for _, a := range args {
		switch a {
		case "--local", "-l":
			t.config = "reasonix.toml"
		default:
			t.config = a
		}
	}
	return t
}

// displayPath shortens a home-relative path to ~/… for readable wizard output.
func displayPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// setupConfig runs the configuration wizard (the `reasonix setup` command),
// writing config.toml to the user-global dir (or ./reasonix.toml under --local)
// and API keys to Reasonix's global .env — never a project's own .env.
// Project memory is a separate concern — the in-session `/init` skill generates
// AGENTS.md (see initHint).
func setupConfig(args []string) int {
	t := resolveSetupTargets(args)
	path := t.config
	if _, err := os.Stat(path); err == nil {
		// Non-interactive must not clobber an existing config silently.
		if !isInteractive() {
			fmt.Fprintf(os.Stderr, i18n.M.NotOverwritingFmt+"\n", path)
			return 1
		}
		in := bufio.NewScanner(os.Stdin)
		if !confirmReconfigureExistingConfig(path, in, os.Stdout) {
			fmt.Println(i18n.M.KeepingExisting)
			return 0
		}
	}

	// Interactive wizard on a TTY; fall back to the annotated default when piped.
	if isInteractive() {
		rc := interactiveSetup(t.config, t.env)
		if rc == 0 {
			fmt.Printf(i18n.M.TryHintFmt+"\n", bold("reasonix"))
		}
		return rc
	}
	return writeDefaultConfig(t.config)
}

func confirmReconfigureExistingConfig(path string, in *bufio.Scanner, w io.Writer) bool {
	ans := ask(in, w, fmt.Sprintf(i18n.M.ConfirmReconfigureFmt, path), "y/N")
	return ans == "y" || ans == "Y"
}

func writeDefaultConfig(path string) int {
	c := config.Default()
	if err := c.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
	fmt.Printf(i18n.M.WroteFileFmt+"\n", displayPath(path))
	fmt.Println(i18n.M.NextHint)
	return 0
}

// initHint handles `reasonix init`. Unlike a config scaffold, project memory is
// model-generated by analyzing the codebase, so it lives as the in-session
// `/init` skill rather than a CLI command. This entry just points the user there
// (and to `reasonix setup` for config) so the verb isn't a dead end.
func initHint() int {
	fmt.Println(i18n.M.InitHint)
	return 0
}

// interactiveSetup runs the setup wizard, then writes the config to configPath
// and any entered API keys to Reasonix's global .env. The wizard
// is intentionally minimal: pick language, pick
// provider, enter API keys. Language is asked first so every subsequent prompt
// is already in the user's language even when env auto-detection got it wrong.
// Two-model collaboration is left as a manual config edit (planner_model) so
// first-run never confronts newcomers with advanced choices.
func interactiveSetup(configPath, envPath string) int {
	// Seed from the existing config when reconfiguring, so a re-run to fix a key
	// preserves the user's providers / agent settings instead of resetting to
	// defaults. First run (no file) falls back to the built-in defaults.
	cfg := config.LoadForEdit(configPath)
	prevDefault := cfg.DefaultModel

	lang, err := selectLanguage()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nsetup cancelled.")
		return 1
	}
	cfg.Language = lang
	cfg.ApplyDeepSeekOfficialDefaultPricing()
	i18n.DetectLanguage(lang)

	// Now that the catalogue matches the user's choice, show the welcome banner
	// in their language before any substantive prompt.
	fmt.Println()
	fmt.Print(boxed([]string{
		accent("◆") + " " + fmt.Sprintf(i18n.M.WelcomeTitleFmt, bold("reasonix")),
		"",
		dim(i18n.M.NoConfigYet),
	}))
	fmt.Println()

	enabled, err := selectEnabledProviders(cfg.Providers, cfg.DeepSeekOfficialPricingLanguage())
	if err != nil {
		fmt.Fprintln(os.Stderr, "\n"+i18n.M.SetupCancelled)
		return 1
	}

	envLines := configureKeys(enabled, os.Stdin, os.Stdout)

	cfg.Providers = enabled
	// Keep the previous default model if it's still enabled; otherwise fall back
	// to the first selected provider.
	cfg.DefaultModel = enabled[0].Name
	for _, p := range enabled {
		if p.Name == prevDefault {
			cfg.DefaultModel = prevDefault
			break
		}
	}

	if err := cfg.SaveTo(configPath); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
	fmt.Printf("\n%s %s\n", green("✓"), fmt.Sprintf(i18n.M.WroteFileFmt, displayPath(configPath)))

	if len(envLines) > 0 {
		target, err := config.StoreCredentialLines(envLines)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.WriteEnvErr, err)
			return 1
		}
		if target == "" {
			target = envPath
		}
		fmt.Printf("%s %s\n", green("✓"), fmt.Sprintf(i18n.M.WroteFileFmt, displayPath(target)))
	}

	fmt.Printf("\n%s %s\n", accent("◆"), i18n.M.SetupComplete)
	return 0
}

// pickSessionToResume scans the session dir, takes the 10 most recent, and
// shows a single-choice menu with timestamp + turn count + first user
// message so the user can pick one. Returns the chosen path and a process
// exit code (non-zero when there's nothing to pick or the user cancelled).
func pickSessionToResume() (string, int) {
	sessions, err := agent.ListSessions(resolveCLISessionDir())
	if err != nil || len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResume)
		return "", 1
	}
	if !isInteractive() {
		fmt.Fprintln(os.Stderr, i18n.M.ResumeRequiresTTY)
		return "", 1
	}
	const cap = 10
	if len(sessions) > cap {
		sessions = sessions[:cap]
	}
	items := make([]menuItem, len(sessions))
	for i, s := range sessions {
		when := s.ModTime.Local().Format("01-02 15:04")
		preview := s.Preview
		if preview == "" {
			preview = "(no user message yet)"
		}
		items[i] = menuItem{
			name: when,
			desc: fmt.Sprintf("%d turns · %s", s.Turns, preview),
		}
	}
	idx, err := selectOne(i18n.M.PickSessionLabel, items)
	if err != nil {
		return "", 1
	}
	return sessions[idx].Path, 0
}

// selectLanguage is the wizard's first prompt: it shows the two UI languages
// in their native form and pre-selects the env-detected one (so a single Enter
// confirms the auto-detection, a single arrow + Enter picks the other). The
// label is bilingual because we don't yet know which catalogue to trust.
func selectLanguage() (string, error) {
	detected := i18n.DetectLanguage("")
	items := []menuItem{{name: "English"}, {name: "中文 (简体)"}}
	tags := []string{"en", "zh"}
	if detected == "zh" {
		items[0], items[1] = items[1], items[0]
		tags[0], tags[1] = tags[1], tags[0]
	}
	idx, err := selectOne("Language · 语言", items)
	if err != nil {
		return "", err
	}
	return tags[idx], nil
}

// selectEnabledProviders prompts a single multi-select of provider families
// (DeepSeek / custom / …) and returns one ProviderEntry per chosen
// family, carrying the models the user picked. Built-in families try the
// OpenAI-compatible GET /models endpoint first (so the user sees the real
// list, not a stale hard-coded one) and fall back to the preset's static
// model list when the call fails — offline first-run, missing key, or a
// vendor that doesn't expose /models. All paths funnel through the same
// fetchOrFallback / buildFamilyEntry helpers, so adding a new family only
// requires a familyOf case.
func selectEnabledProviders(providers []config.ProviderEntry, pricingLanguage string) ([]config.ProviderEntry, error) {
	providers, stale := filterStaleCustomEntries(providers)
	for _, s := range stale {
		fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.SkipStaleCustomEntryFmt, s.Name, s.BaseURL)))
	}
	providers = withBuiltinFamiliesForLanguage(providers, pricingLanguage)

	famOrder, famMembers, famInfo := groupByFamily(providers)

	famItems := make([]menuItem, len(famOrder))
	for i, k := range famOrder {
		famItems[i] = menuItem{name: famInfo[k].name, desc: famInfo[k].desc}
	}
	customIdx := len(famItems)
	famItems = append(famItems, menuItem{name: i18n.M.CustomProviderLabel, desc: i18n.M.CustomProviderDesc})
	anthropicIdx := len(famItems)
	famItems = append(famItems, menuItem{name: i18n.M.AnthropicProviderLabel, desc: i18n.M.AnthropicProviderDesc})

	famIdxs, err := selectMany(i18n.M.SelectProvidersLabel, famItems)
	if err != nil {
		return nil, err
	}

	var enabled []config.ProviderEntry
	for _, fi := range famIdxs {
		switch fi {
		case customIdx:
			cps, err := promptCustomProvider()
			if err != nil {
				fmt.Fprintf(os.Stderr, "custom provider error: %v\n", err)
				continue
			}
			enabled = append(enabled, cps...)
			continue
		case anthropicIdx:
			aps, err := promptAnthropicProvider()
			if err != nil {
				fmt.Fprintf(os.Stderr, "anthropic provider error: %v\n", err)
				continue
			}
			enabled = append(enabled, aps...)
			continue
		}

		familyKey := famOrder[fi]
		probe := providers[famMembers[familyKey][0]]
		famName := famInfo[familyKey].name

		// Seed the probe's static list with every member of the family (e.g. the
		// flash and pro SKUs), not just the first — so a failed /models probe
		// falls back to the whole family instead of collapsing to one model.
		probe.Models = familyStaticModels(providers, famMembers[familyKey])

		// Collect the key before probing /models: a keyless probe 401s and the
		// fallback would hide the live SKUs. Mirrors the custom/anthropic flows;
		// configureKeys later sees the env var set and won't ask twice.
		ensureProbeKey(&probe, famName)

		models := fetchOrFallback(&probe, famName)
		if len(models) == 0 {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.NoModelsAvailableFmt, famName)))
			continue
		}

		items := make([]menuItem, len(models))
		for i, m := range models {
			items[i] = menuItem{name: m}
		}
		idxs, err := selectMany(fmt.Sprintf(i18n.M.SelectModelsLabel, famName), items)
		if err != nil || len(idxs) == 0 {
			continue
		}

		selected := make([]string, 0, len(idxs))
		for _, idx := range idxs {
			selected = append(selected, models[idx])
		}
		members := make([]config.ProviderEntry, 0, len(famMembers[familyKey]))
		for _, idx := range famMembers[familyKey] {
			members = append(members, providers[idx])
		}
		enabled = append(enabled, buildFamilyEntries(probe, members, selected)...)
	}
	return enabled, nil
}

// familyStaticModels unions the preset model lists of every entry in the family,
// preserving order and dropping duplicates. It is the fallback offered when the
// live /models probe fails, so a family with separate flash/pro preset entries
// still surfaces both rather than only the first member's model.
func familyStaticModels(providers []config.ProviderEntry, idxs []int) []string {
	var out []string
	seen := map[string]bool{}
	for _, i := range idxs {
		for _, m := range providers[i].ModelList() {
			if m != "" && !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// ensureProbeKey prompts once for the family's API key when it isn't already in
// the environment, so the /models probe can run and return the live SKU list.
// The value is set in the env for the probe; configureKeys returns the same key
// for Reasonix's global .env later and skips re-asking. A blank entry is fine —
// the static fallback covers it.
func ensureProbeKey(probe *config.ProviderEntry, famName string) {
	if probe.APIKeyEnv == "" || os.Getenv(probe.APIKeyEnv) != "" {
		probe.ResolveAPIKeyFromProcessEnvForProbe()
		return
	}
	fmt.Printf("  %s\n", dim(fmt.Sprintf(i18n.M.FamilyKeyPromptFmt, famName)))
	in := bufio.NewScanner(os.Stdin)
	if key := strings.TrimSpace(ask(in, os.Stdout, "  "+probe.APIKeyEnv, "")); key != "" {
		os.Setenv(probe.APIKeyEnv, key)
	}
	probe.ResolveAPIKeyFromProcessEnvForProbe()
}

// fetchOrFallback tries the OpenAI-compatible GET /models endpoint
// (honoring the entry's ModelsURL when set) and returns the live model IDs.
// On any failure — no base URL, no key set yet (the key is collected in a
// later wizard step), network/auth error, or a vendor without /models — it
// silently returns the preset's static model list so the wizard can always
// present something. The fetch has a 10s timeout and is best-effort.
func fetchOrFallback(probe *config.ProviderEntry, famName string) []string {
	static := probe.ModelList()
	if probe.BaseURL == "" {
		return static
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := probe.FetchModels(ctx)
	if err != nil || len(models) == 0 {
		if len(static) > 0 {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.FetchModelsUsingPresetsFmt, famName)))
		}
		return static
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.FetchModelsSuccessFmt, len(models), famName)))
	return models
}

// fetchModelListCompat walks the full set of model-list URL candidates a given
// base URL can resolve to (root, /v1, known OpenAI/Anthropic compat suffixes)
// and returns the first successful fetch. This is the wizard-time probe for a
// *user-supplied* custom provider — its baseURL is whatever the user pasted,
// and "whatever they pasted" might be https://x.com (root, probe /v1/models)
// or https://x.com/v1 (versioned, probe /v1/models directly). Previously the
// wizard hardcoded `baseURL + "/models"`, which works for OpenAI-shape URLs
// but silently fails for Anthropic-shape roots and the reverse — so the
// wizard's idea of "what models exist" diverged from the chat client's actual
// endpoint. Returning the empty slice (not an error) on full miss lets the
// wizard fall through to a manual text input without an error message.
func fetchModelListCompat(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	candidates, err := config.BuildModelFetchURLs(baseURL, "")
	if err != nil {
		return nil, err
	}
	var lastErr error
	var firstHardErr error
	for _, u := range candidates {
		models, err := openai.FetchModels(ctx, u, apiKey, nil)
		if err == nil {
			return models, nil
		}
		lastErr = err
		if !openai.IsModelFetchEndpointMiss(err) && firstHardErr == nil {
			firstHardErr = err
		}
	}
	if firstHardErr != nil {
		return nil, firstHardErr
	}
	if lastErr != nil {
		slog.Debug("model-list probe: all candidates missed", "base_url", baseURL, "err", lastErr)
	}
	return nil, nil
}

// buildFamilyEntry returns a single ProviderEntry exposing the user's
// selected models under one entry. It preserves the preset's API key env,
// base URL, kind, context window, pricing, and effort — the things that
// vary per vendor but not per model. The Default pointer is reset to the
// first selected model if it would otherwise reference a model the user
// didn't pick (or was empty).
// buildFamilyEntries splits the user's selection back across the family's preset
// members so each model keeps its own entry — and therefore its own pricing,
// context window, and balance URL. A family like DeepSeek ships flash and pro as
// separate presets with different prices; collapsing them into one entry would
// bill pro at flash's rate. Models the live /models list returned that match no
// preset (a new SKU) fall under the probe entry. Member order is preserved;
// within a member, selection order is preserved.
func buildFamilyEntries(probe config.ProviderEntry, members []config.ProviderEntry, selected []string) []config.ProviderEntry {
	tmpl := map[string]config.ProviderEntry{probe.Name: probe}
	ownerName := map[string]string{}
	for _, m := range members {
		tmpl[m.Name] = m
		for _, id := range m.ModelList() {
			ownerName[id] = m.Name
		}
	}
	var order []string
	groups := map[string][]string{}
	for _, sm := range selected {
		name, ok := ownerName[sm]
		if !ok {
			name = probe.Name
		}
		if _, seen := groups[name]; !seen {
			order = append(order, name)
		}
		groups[name] = append(groups[name], sm)
	}
	out := make([]config.ProviderEntry, 0, len(order))
	for _, name := range order {
		out = append(out, buildFamilyEntry(tmpl[name], groups[name]))
	}
	return out
}

func buildFamilyEntry(probe config.ProviderEntry, selected []string) config.ProviderEntry {
	entry := probe
	entry.Models = selected
	entry.Model = selected[0]
	if entry.Default == "" || !containsString(selected, entry.Default) {
		entry.Default = selected[0]
	}
	return entry
}

func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// filterStaleCustomEntries drops the wizard's own magic-name entries
// (Name="custom" with Kind="openai" or Name="anthropic" with Kind="anthropic")
// that older versions of the wizard wrote into reasonix.toml. They collide
// with the wizard's "custom" / "anthropic" menu items on re-run, showing up
// as duplicate broken entries. The new wizard writes host-derived slugs
// (e.g. "custom-token-sensenova-cn") so a hit on the magic name is
// unambiguously stale. The returned slice is the dropped set so the caller
// can warn the user to clean up reasonix.toml by hand.
func filterStaleCustomEntries(providers []config.ProviderEntry) (kept, dropped []config.ProviderEntry) {
	for _, p := range providers {
		if p.Name == "custom" && p.Kind == "openai" {
			dropped = append(dropped, p)
			continue
		}
		if p.Name == "anthropic" && p.Kind == "anthropic" {
			dropped = append(dropped, p)
			continue
		}
		kept = append(kept, p)
	}
	return
}

// providerSlug derives a stable, human-readable entry name for a custom
// OpenAI / Anthropic-compatible provider from its base URL, e.g.
// "custom-token-sensenova-cn" or "anthropic-api-anthropic-com". We can't
// reuse the wizard's menu-item labels ("custom" / "anthropic") because
// those would collide with the menu item itself and end up rendered as
// duplicate provider entries on subsequent re-runs of `reasonix setup`.
// The host-based slug also gives users a meaningful name to grep for in
// reasonix.toml. Falls back to a short sha1 of the raw URL when the URL
// doesn't parse, so even malformed input still produces a unique name.
func providerSlug(kind, baseURL string) string {
	var host string
	if u, err := url.Parse(baseURL); err == nil {
		host = u.Host
	}
	if host == "" {
		sum := sha1.Sum([]byte(baseURL))
		return kind + "-" + hex.EncodeToString(sum[:4])
	}
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	var b strings.Builder
	prevDash := false
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	if slug == "" {
		sum := sha1.Sum([]byte(baseURL))
		return kind + "-" + hex.EncodeToString(sum[:4])
	}
	return kind + "-" + slug
}

func apiKeyEnvFromProviderName(name string) string {
	stem := strings.ToUpper(strings.TrimSpace(name))
	stem = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, stem)
	stem = strings.Trim(stem, "_")
	if stem == "" {
		return "CUSTOM_" + fnv1a32Hex(name) + "_API_KEY"
	}
	return stem + "_API_KEY"
}

func fnv1a32Hex(s string) string {
	hash := uint32(0x811c9dc5)
	for _, unit := range utf16.Encode([]rune(strings.TrimSpace(s))) {
		hash ^= uint32(unit)
		hash *= 0x01000193
	}
	return fmt.Sprintf("%08x", hash)
}

// providerFamily is a wizard-only grouping of provider SKUs by vendor; it does
// not exist in config because users editing reasonix.toml deal with SKU names
// directly.
type providerFamily struct {
	key  string
	name string
	desc string
}

func familyOf(name string) providerFamily {
	switch {
	case strings.HasPrefix(name, "deepseek"):
		return providerFamily{key: "deepseek", name: "DeepSeek", desc: "fast & cheap, plus a stronger Pro SKU"}
	default:
		return providerFamily{key: name, name: name}
	}
}

// promptCustomProvider handles the custom provider entry flow.
func promptCustomProvider() ([]config.ProviderEntry, error) {
	methodIdx, err := selectOne(i18n.M.CustomAddMethodLabel, []menuItem{
		{name: i18n.M.CustomMethodManual},
		{name: i18n.M.CustomMethodURL},
	})
	if err != nil {
		return nil, err
	}
	if methodIdx == 0 {
		return promptCustomProviderManual()
	}
	return promptCustomProviderFromURL()
}

// promptCustomProviderManual handles manual model entry.
func promptCustomProviderManual() ([]config.ProviderEntry, error) {
	return promptCustomProviderManualWith(bufio.NewScanner(os.Stdin), "", "", "")
}

// promptCustomProviderManualWith is the shared backend for manual entry.
// Pre-filled values (baseURL, keyEnv, apiKey) are reused as-is when non-empty
// so the URL-fetch flow can fall through to manual entry without re-asking
// the user for information they've already typed. An empty apiKey is allowed
// — the key step happens later in the wizard and Reasonix's global .env is updated then.
func promptCustomProviderManualWith(in *bufio.Scanner, baseURL, keyEnv, apiKey string) ([]config.ProviderEntry, error) {
	fmt.Println()
	if baseURL == "" {
		baseURL = ask(in, os.Stdout, i18n.M.CustomPromptBaseURL, "")
		if baseURL == "" {
			return nil, fmt.Errorf("base URL is required")
		}
	}
	providerName := providerSlug("custom", baseURL)
	if keyEnv == "" {
		keyEnv = ask(in, os.Stdout, i18n.M.CustomPromptKeyEnv, apiKeyEnvFromProviderName(providerName))
	}
	if apiKey == "" {
		apiKey = ask(in, os.Stdout, i18n.M.CustomPromptAPIKey, "")
	}
	if apiKey != "" {
		os.Setenv(keyEnv, apiKey)
	}
	modelName := ask(in, os.Stdout, i18n.M.CustomPromptModel, "")
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	entry := config.ProviderEntry{
		Name: providerName, Kind: "openai", BaseURL: baseURL,
		Model: modelName, APIKeyEnv: keyEnv, ContextWindow: 128000,
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.CustomAddedFmt, entry.Name+"/"+modelName)))
	return []config.ProviderEntry{entry}, nil
}

// promptCustomProviderFromURL tries the OpenAI-compatible GET /models
// endpoint and shows a checkbox of the returned models. If the call fails
// (network error, auth failure, or a vendor without /models) it falls
// through to manual entry, reusing the URL and key the user already typed.
func promptCustomProviderFromURL() ([]config.ProviderEntry, error) {
	in := bufio.NewScanner(os.Stdin)
	fmt.Println()

	baseURL := ask(in, os.Stdout, i18n.M.CustomPromptBaseURL, "")
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	providerName := providerSlug("custom", baseURL)
	keyEnv := ask(in, os.Stdout, i18n.M.CustomPromptKeyEnv, apiKeyEnvFromProviderName(providerName))
	apiKey := ask(in, os.Stdout, i18n.M.CustomPromptAPIKey, "")
	if apiKey != "" {
		os.Setenv(keyEnv, apiKey)
	}

	fmt.Printf("  %s\n", dim(fmt.Sprintf(i18n.M.FetchingModelsFmt, "custom")))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := fetchModelListCompat(ctx, baseURL, apiKey)
	if err != nil || len(models) == 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.FetchModelsFailedFmt, "custom", err)))
		} else {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(i18n.M.CustomFetchEmpty))
		}
		return promptCustomProviderManualWith(in, baseURL, keyEnv, apiKey)
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.FetchModelsSuccessFmt, len(models), "custom")))

	items := make([]menuItem, len(models))
	for i, m := range models {
		items[i] = menuItem{name: m}
	}
	idxs, err := selectMany(fmt.Sprintf(i18n.M.SelectModelsLabel, "custom"), items)
	if err != nil || len(idxs) == 0 {
		return nil, fmt.Errorf("no models selected")
	}
	var selected []string
	for _, i := range idxs {
		selected = append(selected, models[i])
	}
	entry := config.ProviderEntry{
		Name: providerName, Kind: "openai", BaseURL: baseURL,
		Models: selected, Model: selected[0], APIKeyEnv: keyEnv, ContextWindow: 128000,
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.CustomAddedFmt, entry.Name+"/"+selected[0])))
	return []config.ProviderEntry{entry}, nil
}

// promptAnthropicProvider handles the Anthropic compatible provider entry flow.
func promptAnthropicProvider() ([]config.ProviderEntry, error) {
	methodIdx, err := selectOne(i18n.M.AnthropicAddMethodLabel, []menuItem{
		{name: i18n.M.AnthropicMethodManual},
		{name: i18n.M.AnthropicMethodURL},
	})
	if err != nil {
		return nil, err
	}
	if methodIdx == 0 {
		return promptAnthropicProviderManual()
	}
	return promptAnthropicProviderFromURL()
}

// promptAnthropicProviderManual handles manual model entry.
func promptAnthropicProviderManual() ([]config.ProviderEntry, error) {
	return promptAnthropicProviderManualWith(bufio.NewScanner(os.Stdin), "", "", "")
}

// promptAnthropicProviderManualWith is the shared backend for manual entry
// of an Anthropic-compatible custom provider. Pre-filled values (baseURL,
// keyEnv, apiKey) are reused as-is when non-empty so the URL-fetch flow
// can fall through to manual entry without re-asking the user.
func promptAnthropicProviderManualWith(in *bufio.Scanner, baseURL, keyEnv, apiKey string) ([]config.ProviderEntry, error) {
	fmt.Println()
	if baseURL == "" {
		baseURL = ask(in, os.Stdout, i18n.M.AnthropicPromptBaseURL, "")
		if baseURL == "" {
			return nil, fmt.Errorf("base URL is required")
		}
	}
	if keyEnv == "" {
		keyEnv = ask(in, os.Stdout, i18n.M.AnthropicPromptKeyEnv, "ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		apiKey = ask(in, os.Stdout, i18n.M.AnthropicPromptAPIKey, "")
	}
	if apiKey != "" {
		os.Setenv(keyEnv, apiKey)
	}
	modelName := ask(in, os.Stdout, i18n.M.AnthropicPromptModel, "")
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	entry := config.ProviderEntry{
		Name: providerSlug("anthropic", baseURL), Kind: "anthropic", BaseURL: baseURL,
		Model: modelName, APIKeyEnv: keyEnv, ContextWindow: 128000,
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.AnthropicAddedFmt, entry.Name+"/"+modelName)))
	return []config.ProviderEntry{entry}, nil
}

// promptAnthropicProviderFromURL tries the OpenAI-compatible GET /models
// endpoint (some Anthropic-compatible proxies do expose one). Most don't
// — Anthropic's own API has no public model list — so on any failure the
// flow falls through to manual entry with the URL/key already filled in,
// rather than aborting the wizard.
func promptAnthropicProviderFromURL() ([]config.ProviderEntry, error) {
	in := bufio.NewScanner(os.Stdin)
	fmt.Println()

	baseURL := ask(in, os.Stdout, i18n.M.AnthropicPromptBaseURL, "")
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	keyEnv := ask(in, os.Stdout, i18n.M.AnthropicPromptKeyEnv, "ANTHROPIC_API_KEY")
	apiKey := ask(in, os.Stdout, i18n.M.AnthropicPromptAPIKey, "")
	if apiKey != "" {
		os.Setenv(keyEnv, apiKey)
	}

	fmt.Printf("  %s\n", dim(fmt.Sprintf(i18n.M.AnthropicFetchingModelsFmt, "anthropic")))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := fetchModelListCompat(ctx, baseURL, apiKey)
	if err != nil || len(models) == 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.AnthropicFetchModelsFailedFmt, "anthropic", err)))
		} else {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(i18n.M.AnthropicFetchEmpty))
		}
		return promptAnthropicProviderManualWith(in, baseURL, keyEnv, apiKey)
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.AnthropicFetchModelsSuccessFmt, len(models), "anthropic")))

	items := make([]menuItem, len(models))
	for i, m := range models {
		items[i] = menuItem{name: m}
	}
	idxs, err := selectMany(fmt.Sprintf(i18n.M.AnthropicSelectModelsLabel, "anthropic"), items)
	if err != nil || len(idxs) == 0 {
		return nil, fmt.Errorf("no models selected")
	}
	var selected []string
	for _, i := range idxs {
		selected = append(selected, models[i])
	}
	entry := config.ProviderEntry{
		Name: providerSlug("anthropic", baseURL), Kind: "anthropic", BaseURL: baseURL,
		Models: selected, Model: selected[0], APIKeyEnv: keyEnv, ContextWindow: 128000,
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.AnthropicAddedFmt, entry.Name+"/"+selected[0])))
	return []config.ProviderEntry{entry}, nil
}

func groupByFamily(providers []config.ProviderEntry) ([]string, map[string][]int, map[string]providerFamily) {
	var order []string
	members := map[string][]int{}
	info := map[string]providerFamily{}
	for i, p := range providers {
		f := familyOf(p.Name)
		if _, seen := members[f.key]; !seen {
			order = append(order, f.key)
			info[f.key] = f
		}
		members[f.key] = append(members[f.key], i)
	}
	return order, members, info
}

// withBuiltinFamilies guarantees the wizard always offers the built-in DeepSeek
// family even when the loaded config replaced the defaults.
// Built-in entries whose exact name already exists in the user's config are
// kept as-is (preserving customizations); missing built-in entries within an
// existing family are appended so the model picker always shows the full
// catalogue rather than only the previously selected subset.
func withBuiltinFamilies(providers []config.ProviderEntry) []config.ProviderEntry {
	return withBuiltinFamiliesForLanguage(providers, "")
}

func withBuiltinFamiliesForLanguage(providers []config.ProviderEntry, pricingLanguage string) []config.ProviderEntry {
	haveName := map[string]bool{}
	for _, p := range providers {
		haveName[p.Name] = true
	}
	defaults := config.Default()
	defaults.Language = pricingLanguage
	defaults.ApplyDeepSeekOfficialDefaultPricing()
	for _, bp := range defaults.Providers {
		if !haveName[bp.Name] {
			providers = append(providers, bp)
		}
	}
	return providers
}

// providersWithMissingKeys returns the providers the active configuration
// actually references (default/planner/subagent models) whose api_key_env is
// declared but not set. Merely-available providers stay silent; the chat banner
// still warns if users later switch to a model whose key is missing.
// configureKeys dedupes shared envs, so duplicates are fine to leave in.
func providersWithMissingKeys(cfg *config.Config) []config.ProviderEntry {
	if cfg == nil {
		return nil
	}
	refs := []string{
		cfg.DefaultModel,
		cfg.Agent.PlannerModel,
		cfg.Agent.SubagentModel,
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Agent.AutoPlan), "off") {
		refs = append(refs, cfg.Agent.AutoPlanClassifier)
	}
	if len(cfg.Agent.SubagentModels) > 0 {
		keys := make([]string, 0, len(cfg.Agent.SubagentModels))
		for key := range cfg.Agent.SubagentModels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			refs = append(refs, cfg.Agent.SubagentModels[key])
		}
	}

	var out []config.ProviderEntry
	seen := map[string]bool{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		p, ok := cfg.ResolveModel(ref)
		if !ok || p.APIKeyEnv == "" || os.Getenv(p.APIKeyEnv) != "" || seen[p.APIKeyEnv] {
			continue
		}
		seen[p.APIKeyEnv] = true
		out = append(out, *p)
	}
	return out
}

// configureKeys reconciles each enabled provider's API key with the
// environment. For every distinct api_key_env: if the variable is already set,
// setup asks whether to re-enter it; Enter keeps and re-pins the existing value.
// Otherwise the user is asked once per env var (deduped across providers that
// share one, e.g. both DeepSeek models). Returns KEY=value lines for the
// Reasonix global .env. Re-pinning keeps hand-edited or previously saved values
// aligned with the user's latest setup choice.
func configureKeys(selected []config.ProviderEntry, r io.Reader, w io.Writer) []string {
	in := bufio.NewScanner(r)
	fmt.Fprintln(w, "\n"+i18n.M.EnterAPIKeysHeader)

	seen := map[string]bool{}
	var envLines []string
	for _, p := range selected {
		if p.APIKeyEnv == "" || seen[p.APIKeyEnv] {
			continue
		}
		seen[p.APIKeyEnv] = true

		if cur := os.Getenv(p.APIKeyEnv); cur != "" {
			reset := ask(in, w, "  "+fmt.Sprintf(i18n.M.APIKeyResetPromptFmt, p.APIKeyEnv), "y/N")
			if reset == "y" || reset == "Y" {
				if key := ask(in, w, "  "+p.APIKeyEnv, ""); key != "" {
					envLines = append(envLines, p.APIKeyEnv+"="+key)
					continue
				}
			}
			fmt.Fprintf(w, "  %s %s\n", green("✓"), fmt.Sprintf(i18n.M.APIKeyAlreadySetFmt, p.APIKeyEnv))
			envLines = append(envLines, p.APIKeyEnv+"="+cur)
			continue
		}

		if key := ask(in, w, "  "+p.APIKeyEnv, ""); key != "" {
			envLines = append(envLines, p.APIKeyEnv+"="+key)
		}
	}
	return envLines
}

// ask prints a prompt to w and returns the entered line, or def if input is empty.
func ask(in *bufio.Scanner, w io.Writer, label, def string) string {
	if def != "" {
		fmt.Fprintf(w, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(w, "%s: ", label)
	}
	if !in.Scan() {
		return def
	}
	if v := strings.TrimSpace(in.Text()); v != "" {
		return v
	}
	return def
}

// isInteractive reports whether we're attached to a real terminal on both
// stdin and stdout — required for prompting. Redirected or piped I/O is not
// interactive, so wizards never block or auto-default in scripts and CI.
func isInteractive() bool {
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// appendEnv merges KEY=value lines into a .env file. Existing assignments of
// any key that's about to be written are dropped first, then the new values
// are appended — so re-running `reasonix setup` with a corrected key replaces the
// stale one instead of stacking duplicates. The new values are also
// pinned into the current process env so a chat session started right after
// init picks up the fresh keys without a restart.
func appendEnv(path string, lines []string) error {
	target := map[string]bool{}
	for _, l := range lines {
		if k, _, ok := strings.Cut(l, "="); ok {
			target[strings.TrimSpace(k)] = true
		}
	}

	var kept []string
	if data, err := fileencoding.ReadFileUTF8(path); err == nil {
		for _, raw := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(raw)
			check := strings.TrimPrefix(trimmed, "export ")
			if k, _, ok := strings.Cut(check, "="); ok && target[strings.TrimSpace(k)] {
				continue
			}
			kept = append(kept, raw)
		}
		// strings.Split on a string ending with \n leaves a trailing empty
		// element; trim it so we don't grow a blank line on every rewrite.
		if n := len(kept); n > 0 && kept[n-1] == "" {
			kept = kept[:n-1]
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	var b strings.Builder
	for _, l := range kept {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
		if k, v, ok := strings.Cut(l, "="); ok {
			os.Setenv(strings.TrimSpace(k), v)
		}
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// readStdin reads piped input if present; an interactive terminal yields "".
func readStdin() string {
	stat, err := os.Stdin.Stat()
	if err != nil || stat.Mode()&os.ModeCharDevice != 0 {
		return ""
	}
	data, _ := io.ReadAll(os.Stdin)
	return strings.TrimSpace(string(data))
}

func usage() {
	fmt.Print(i18n.M.UsageBody)
}

func configCommand(args []string) int {
	if len(args) == 0 {
		configUsage()
		return 2
	}
	switch args[0] {
	case "auto-plan":
		return configAutoPlanCommand(args[1:])
	case "memory-v5":
		return configMemoryV5Command(args[1:])
	case "reasoning-language":
		return configReasoningLanguageCommand(args[1:])
	default:
		configUsage()
		return 2
	}
}

func configAutoPlanCommand(args []string) int {
	fs := flag.NewFlagSet("config auto-plan", flag.ContinueOnError)
	local := fs.Bool("local", false, "unsupported; auto-plan is user-level only")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *local {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "auto-plan is user-level only; --local is not supported")
		return 2
	}
	rest := fs.Args()
	if len(rest) > 1 {
		configAutoPlanUsage()
		return 2
	}
	if len(rest) == 0 {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		mode := cfg.Agent.AutoPlan
		mode = cliAutoPlanMode(mode)
		fmt.Printf("auto_plan = %q\n", mode)
		return 0
	}
	path := config.UserConfigPath()
	if path == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "cannot resolve config path")
		return 1
	}
	// Serialize the load-modify-save against other in-process user-config
	// editors so concurrent writers don't drop each other's fields.
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetAutoPlan(rest[0]); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if err := cfg.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("auto_plan = %q (%s)\n", cfg.Agent.AutoPlan, displayPath(path))
	return 0
}

func configMemoryV5Command(args []string) int {
	fs := flag.NewFlagSet("config memory-v5", flag.ContinueOnError)
	local := fs.Bool("local", false, "unsupported; Memory v5 is user-level only")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *local {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "memory-v5 is user-level only; --local is not supported")
		return 2
	}
	rest := fs.Args()
	if len(rest) > 1 {
		configMemoryV5Usage()
		return 2
	}
	if len(rest) == 0 || strings.EqualFold(rest[0], "status") {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("memory_compiler.enabled = %v\n", cfg.MemoryCompilerEnabled())
		fmt.Printf("memory_compiler.verbosity = %q\n", cfg.MemoryCompilerVerbosity())
		return 0
	}
	setting, err := parseCLIMemoryV5Setting(rest[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	path := config.UserConfigPath()
	if path == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "cannot resolve config path")
		return 1
	}
	// Serialize the load-modify-save against other in-process user-config
	// editors so concurrent writers don't drop each other's fields.
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetMemoryCompilerEnabled(setting.enabled); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if setting.setVerbosity {
		if err := cfg.SetMemoryCompilerVerbosity(setting.verbosity); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 2
		}
	}
	if err := cfg.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("memory_compiler.enabled = %v\n", cfg.MemoryCompilerEnabled())
	fmt.Printf("memory_compiler.verbosity = %q (%s)\n", cfg.MemoryCompilerVerbosity(), displayPath(path))
	return 0
}

func configReasoningLanguageCommand(args []string) int {
	fs := flag.NewFlagSet("config reasoning-language", flag.ContinueOnError)
	local := fs.Bool("local", false, "write ./reasonix.toml instead of the user config")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) > 1 {
		configReasoningLanguageUsage()
		return 2
	}
	if len(rest) == 0 {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("reasoning_language = %q\n", cliReasoningLanguageMode(cfg.ReasoningLanguage()))
		return 0
	}
	mode, err := parseCLIReasoningLanguage(rest[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	path := config.UserConfigPath()
	if *local {
		path = "reasonix.toml"
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "cannot resolve config path")
		return 1
	}
	if *local {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			lang, err := config.SaveMinimalProjectReasoningLanguage(path, mode)
			if err != nil {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
				return 1
			}
			fmt.Printf("reasoning_language = %q (%s)\n", lang, displayPath(path))
			return 0
		} else if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
	}
	if !*local {
		// Non-local writes target the user config; serialize the
		// load-modify-save against other in-process user-config editors.
		// --local writes ./reasonix.toml and needs no user-config lock.
		unlock := config.LockUserConfigEdits()
		defer unlock()
	}
	cfg := config.LoadForEdit(path)
	if err := cfg.SetReasoningLanguage(mode); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if err := cfg.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("reasoning_language = %q (%s)\n", cfg.ReasoningLanguage(), displayPath(path))
	return 0
}

func configUsage() {
	fmt.Print(`Usage:
  reasonix config auto-plan [off|on]
  reasonix config memory-v5 [off|observe|compact|on|status]
  reasonix config reasoning-language [--local] [auto|zh|en]
`)
}

func configAutoPlanUsage() {
	fmt.Print(`Usage:
  reasonix config auto-plan [off|on]
`)
}

func configMemoryV5Usage() {
	fmt.Print(`Usage:
  reasonix config memory-v5 [off|observe|compact|on|status]
`)
}

func configReasoningLanguageUsage() {
	fmt.Print(`Usage:
  reasonix config reasoning-language [--local] [auto|zh|en]
`)
}

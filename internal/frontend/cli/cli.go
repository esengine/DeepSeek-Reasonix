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
	"math"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"reasonix/internal/assembly/boot"
	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/frontend/termrender"
	"reasonix/internal/platform/notify"
	"reasonix/internal/platform/telemetry"
	"reasonix/internal/session/control"

	"github.com/spf13/pflag"
	"golang.org/x/term"
)

var (
	cliIsInteractive = isInteractive
	runWebCommand    = runWeb
	openBrowserURL   = openInBrowser
)

// Run is the CLI entry point; it returns a process exit code.
// Prefer RunWithBuildInfo when git commit / build time are available from ldflags.
func Run(args []string, version string) int {
	return RunWithBuildInfo(args, BuildInfo{Version: version})
}

// RunWithBuildInfo is the full CLI entry with optional build metadata for
// `reasonix version --verbose` / `--json`.
func RunWithBuildInfo(args []string, info BuildInfo) int {
	info = info.withDefaults()
	version := info.Version
	// Usage recording is asynchronous so provider/UI paths never wait on disk.
	// Drain accepted records and fence the projection worker before returning.
	// An embedded Run may outlive one invocation and remove its CacheDir.
	defer closeCLIUsageCatalogs()
	// Pick the UI language up front so even pre-config paths (the first-run
	// welcome banner) come through localized. Env-only first; if a config
	// exists and pins a language, that wins.
	i18n.DetectLanguage("")
	cmd, args := normalizeCommand(args)
	doctorRepair := isDoctorRepairCommand(args)
	if shouldMigrateLegacyConfigForCLI(cmd) && !doctorRepair {
		migrateLegacyConfigForCLI()
	}
	if !doctorRepair {
		if cfg, err := config.Load(); err == nil {
			if cfg.Language != "" {
				i18n.DetectLanguage(cfg.Language)
			}
		}
	}

	if len(args) == 0 || cmd == "" {
		// A bare reasonix, or one given only session options, is the chat a
		// person opens in a terminal, as it was in 1.x; without a terminal
		// there is no one to chat with, so it prints how to run a task.
		if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
			return runTUI(args, version)
		}
		return bareUsage()
	}

	rest := args[1:]
	switch cmd {
	case "run":
		return runAgent(rest, version)
	case "serve":
		return runServe(rest, version)
	case "tui":
		return runTUI(rest, version)
	case "web":
		return runWebCommand(rest, version)
	case "setup":
		configureThemeForTTYOutput()
		return setupConfig(rest)
	case "config":
		termrender.ConfigureThemeFromConfig()
		return configCommand(rest)
	case "init":
		termrender.ConfigureThemeFromConfig()
		return initHint()
	case "acp":
		termrender.ConfigureThemeFromConfig()
		return acpCommand(rest, version)
	case "mcp":
		termrender.ConfigureThemeFromConfig()
		return mcpCommand(rest)
	case "login", "whoami", "logout":
		return accountCommand(cmd, rest, version)
	case "remote":
		termrender.ConfigureThemeFromConfig()
		return remoteCommand(rest, version)
	case "plugin":
		termrender.ConfigureThemeFromConfig()
		return pluginCommand(rest)
	case "subagent":
		configureThemeForTTYOutput()
		return subagentCommand(rest)
	case "doctor":
		if !doctorRepair {
			termrender.ConfigureThemeFromConfig()
		}
		return doctorCommand(rest, version)
	case "report":
		termrender.ConfigureThemeFromConfig()
		return reportCommand(rest)
	case "session", "sessions", "catalogs":
		return runSessionOrCatalogCommand(cmd, rest)
	case "hook", "hooks":
		termrender.ConfigureThemeFromConfig()
		return hookCommand(rest)
	case "task":
		termrender.ConfigureThemeFromConfig()
		return taskCommand(rest)
	case "review":
		termrender.ConfigureThemeFromConfig()
		return reviewCommand(rest)
	case "upgrade", "update":
		termrender.ConfigureThemeFromConfig()
		return upgradeCommand(rest, version)
	case "version":
		// Detailed identity: version --verbose / --json. Top-level --version/-v
		// stay single-line for script compatibility (Integration D/E).
		return versionCommand(rest, info, true)
	case "--version", "-v":
		return versionCommand(nil, info, false)
	case "completion":
		return completionCommand(rest)
	case "docs-manifest":
		return docsManifestCommand(rest, version)
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, i18n.M.UnknownCommandFmt+"\n\n", cmd)
		usage()
		return 2
	}
}

func isDoctorRepairCommand(args []string) bool {
	return len(args) > 1 && args[0] == "doctor" && args[1] == "repair"
}

func isDefaultInteractiveFlag(arg string) bool {
	switch arg {
	case "--model", "--max-steps", "--continue", "-c", "--resume", "-r", "--copy", "--dangerously-skip-permissions", "--yolo", "--permission-mode", "--effort", "--dir", "--add-dir", "--allowed-tools", "--allowedTools", "--profile":
		return true
	}
	if name, _, ok := strings.Cut(arg, "="); ok && isDefaultInteractiveFlag(name) {
		return true
	}
	return false
}

func shouldMigrateLegacyConfigForCLI(cmd string) bool {
	switch cmd {
	case "", "run", "serve", "web", "setup", "config", "init", "acp", "mcp", "remote", "plugin", "subagent", "doctor", "upgrade", "update", "login", "whoami", "logout":
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

// configureThemeForTTYOutput is the one theme setup that may probe the
// terminal; metadata commands must never reach it.
var configureThemeForTTYOutput = termrender.ConfigureThemeFromConfigForTTYOutput

// setupProfile builds a ready-to-drive Controller from config via boot.Build.
// The assembly (model resolution, tool registry, permission gate, two-model
// Coordinator) lives in internal/assembly/boot, shared with the desktop frontend.
// requireKey forces the executor's API key to be present (used by run); chat
// passes false so the session UI is reachable before a key is set. sink receives
// the agent's typed event stream — runAgent passes a TextSink that renders to
// stdout, the TUI passes an event-channel sink so events become tea.Msgs.
// profile selects economy|balanced|delivery (empty = balanced/full).
// workspaceRoot pins the project root explicitly (from --dir); empty falls back
// to git-root detection.
func setupProfile(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink, profile string, workspaceRoot string) (*control.Controller, error) {
	return setupProfileWithOverrides(ctx, modelName, maxStepsOverride, requireKey, sink, profile, cliBuildOverrides{WorkspaceRoot: workspaceRoot})
}

type cliPermissionMode struct {
	approval string
	plan     bool
	allow    []string
}

// parsePermissionMode also reads 1.x's three names, so a 1.x command line
// keeps working: workspace-write is Auto, danger-full-access is Yolo, and
// read-only, which 2.x has no mode for, falls back to the most careful one.
func parsePermissionMode(value string) (cliPermissionMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "default", "ask", "read-only":
		return cliPermissionMode{approval: control.ToolApprovalAsk}, nil
	case "auto", "workspace-write":
		return cliPermissionMode{approval: control.ToolApprovalAuto}, nil
	case "acceptedits", "accept-edits":
		return cliPermissionMode{approval: control.ToolApprovalAsk, allow: []string{
			"write_file", "edit_file", "multi_edit", "move_file", "notebook_edit", "delete_range", "delete_symbol",
		}}, nil
	case "manual":
		return cliPermissionMode{approval: control.ToolApprovalAsk}, nil
	case "dontask", "dont-ask":
		return cliPermissionMode{approval: control.ToolApprovalDontAsk}, nil
	case "plan":
		return cliPermissionMode{approval: control.ToolApprovalAsk, plan: true}, nil
	case "bypasspermissions", "bypass-permissions", "yolo", "danger-full-access":
		return cliPermissionMode{approval: control.ToolApprovalYolo}, nil
	default:
		return cliPermissionMode{}, fmt.Errorf("unknown permission mode %q (want manual, ask, auto, acceptEdits, dontAsk, plan, or bypassPermissions; 1.x's read-only, workspace-write and danger-full-access also work)", value)
	}
}

func resolveRunPermissionMode(value string, auto, modeExplicit bool) (string, error) {
	if !auto {
		return value, nil
	}
	if modeExplicit {
		return "", errors.New("--auto/-y cannot be combined with --permission-mode")
	}
	return "auto", nil
}

func parseRuntimeProfile(value string) (string, error) {
	// Accept both --preset balanced|delivery and legacy --profile
	// economy|full|delivery. Returns dual-write TokenMode values.
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "balanced", boot.TokenModeFull:
		return boot.TokenModeFull, nil
	case boot.TokenModeEconomy, "light", "lite", "eco":
		return boot.TokenModeEconomy, nil
	case boot.TokenModeDelivery, "deliver", "quality":
		return boot.TokenModeDelivery, nil
	default:
		return "", fmt.Errorf("unknown execution setting %q (want light, balanced, or delivery; legacy: economy, full)", value)
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
	sessionModel, ok := sessionstore.LoadSessionModel(resumePath)
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

func loadResumableSession(path string) (*sessionstore.Session, error) {
	if sessionstore.IsCleanupPending(path) {
		return nil, fmt.Errorf("session is pending cleanup")
	}
	return sessionstore.LoadSession(path)
}

// registerContinueFlag registers --continue with its -c shorthand. The
// shorthand must go through BoolP (pflag shorthand), not BoolVar: BoolVar
// registers "c" as a long flag name, which leaves "-c" unparseable
// ("unknown shorthand flag: 'c' in -c") while accidentally accepting "--c".
func registerContinueFlag(fs *pflag.FlagSet) *bool {
	return fs.BoolP("continue", "c", false, "resume the most recent saved session")
}

func runAgent(args []string, version string) int {
	defer closeCLIUsageCatalogs()
	fs := pflag.NewFlagSet("run", pflag.ContinueOnError)
	fs.SetInterspersed(true)
	model := fs.String("model", "", "provider name (default: config default_model)")
	profileFlag := fs.String("profile", "", "deprecated: use --preset (economy|balanced|delivery)")
	presetFlag := fs.String("preset", "balanced", "agent execution setting: light | balanced | delivery")
	maxSteps := fs.Int("max-steps", 0, "one-off max tool-call rounds (0 = automatic)")
	showThinking := fs.Bool("show-thinking", false, "show thinking text instead of the collapsed thinking marker")
	metricsPath := fs.String("metrics", "", "write a JSON token/cache/cost summary of the run to this path")
	trajectoryPath := fs.String("trajectory", "", "append a timestamped JSONL trajectory of the run's full event stream (tool calls, reasoning, decisions) to this path")
	ablateFlag, foldIndexFlag := registerArmFlags(fs)
	dir := fs.String("dir", "", "change to this directory first (project root); config, sandbox and file tools resolve from here")
	cont := registerContinueFlag(fs)
	resume := fs.String("resume", "", "resume by session file path, session ID, or machine session ID (takes precedence over --continue)")
	copySession := fs.Bool("copy", false, "with --resume/--continue: duplicate the session and continue in the copy (escape hatch when the original is held by another Reasonix process)")
	effort := fs.String("effort", "", "session reasoning effort override")
	permissionMode := fs.String("permission-mode", "ask", "permission mode: manual | ask | auto | acceptEdits | dontAsk | plan | bypassPermissions")
	autoApprove := fs.BoolP("auto", "y", false, "explicitly auto-approve ordinary writer fallbacks (alias for --permission-mode auto)")
	printOnly := fs.BoolP("print", "p", false, "print only the final response")
	eventsJSONL := fs.Bool("events-jsonl", false, "emit a redacted structured event stream as JSONL")
	outputFormat := fs.String("output-format", "text", "output format: text | json | stream-json")
	var additionalDirs []string
	fs.StringArrayVar(&additionalDirs, "add-dir", nil, "allow tool access to an additional directory (repeatable)")
	var allowedToolValues []string
	fs.StringArrayVar(&allowedToolValues, "allowed-tools", nil, "comma or space-separated permission rules to allow")
	fs.StringArrayVar(&allowedToolValues, "allowedTools", nil, "alias for --allowed-tools")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	resolvedPermissionMode, err := resolveRunPermissionMode(*permissionMode, *autoApprove, fs.Changed("permission-mode"))
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	*permissionMode = resolvedPermissionMode
	allowedTools, err := splitAllowedToolRules(allowedToolValues)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	format, err := parseRunOutputFormat(*outputFormat)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if *eventsJSONL {
		if fs.Changed("output-format") {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--events-jsonl cannot be combined with --output-format")
			return 2
		}
		format = runOutputEventsJSONL
	}
	profileRaw := strings.TrimSpace(*profileFlag)
	if profileRaw != "" {
		fmt.Fprintln(os.Stderr, "warning: --profile is deprecated; use --preset light|balanced|delivery")
	} else {
		profileRaw = strings.TrimSpace(*presetFlag)
	}
	profile, err := parseRuntimeProfile(profileRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	ablated, err := ablation.ParseArm(*ablateFlag, *foldIndexFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	permissions, err := parsePermissionMode(*permissionMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if permissions.plan {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--permission-mode plan requires an interactive session")
		return 2
	}
	allowedTools = uniqueStrings(append(allowedTools, permissions.allow...))
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}
	workspaceRoot, err := workspaceRootForDir(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	cfg, _ := config.Load()
	configureThemeForTTYOutput()

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		prompt = readStdin()
	}
	if prompt == "" {
		fmt.Fprintln(os.Stderr, i18n.M.UsageRunHint)
		return 2
	}
	var machineIdentityKey []byte
	if format == runOutputEventsJSONL {
		machineIdentityKey, err = loadMachineIdentityKey()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "machine identity is unavailable")
			return 1
		}
	}

	// Resolve the resume target up front so --copy and the session lease can be
	// handled before any heavy assembly. --resume takes precedence over
	// --continue, matching the Resume call below. Accept file paths, branch
	// IDs, preview text, and opaque machine session IDs (#7429).
	resumePath := strings.TrimSpace(*resume)
	if resumePath != "" {
		resolved, err := resolveSessionQuery(resolveCLISessionDirFor(workspaceRoot), resumePath)
		if err != nil {
			reportResumeQueryError(os.Stderr, err)
			return 1
		}
		resumePath = resolved
	}
	if resumePath == "" && *cont {
		sessionDir := resolveCLISessionDirFor(workspaceRoot)
		reclaimCLIRecoveryBranches(sessionDir)
		session, ok := mostRecentSession(sessionDir)
		if !ok {
			fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResume)
			return 1
		}
		resumePath = session.Path
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
		// Keep structured (json/stream-json) and --print stdout a single
		// machine-readable payload: the human copy notice goes to stderr there.
		// Plain text runs keep it on stdout, where callers scrape the copied path.
		if format == runOutputText && !*printOnly {
			fmt.Printf("continuing in a session copy: %s\n", copied)
		} else {
			fmt.Fprintf(os.Stderr, "continuing in a session copy: %s\n", copied)
		}
		resumePath = copied
	}
	sessionMode := cliTelemetrySessionMode(*cont, strings.TrimSpace(*resume) != "", *copySession)
	reporter := startCLITelemetry(cfg, telemetry.Options{
		Version: version, Interactive: false, CLIMode: "run", Profile: profile,
		PermissionMode: *permissionMode, SessionMode: sessionMode,
	})

	// Own the session file for the lifetime of this run so a desktop window (or
	// another CLI) writing the same session is refused up front instead of
	// silently double-writing. Released after the controller closes.
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	var resumeSession *sessionstore.Session
	if resumePath != "" {
		if err := leases.Rebind(resumePath); err != nil {
			if errors.Is(err, sessionstore.ErrSessionLeaseHeld) {
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
	started := time.Now()

	chain, err := buildRunSink(format, *printOnly, *showThinking, *metricsPath, *trajectoryPath, cfg, reporter)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	sink, resultOutput, metrics := chain.sink, chain.resultOutput, chain.metrics
	if resumePath != "" {
		*model = modelForResumePath(*model, resumePath, cfg)
	}
	var effortOverride *string
	if strings.TrimSpace(*effort) != "" {
		effortOverride = effort
	}
	// `reasonix run` is headless: there is no key loop to answer approval or ask
	// prompts, and the approval timeout defaults to infinite. Installing the
	// interactive approver/asker here would let an Ask rule, the `ask` tool, or a
	// sandbox/config approval wedge the run forever. Map the mode onto a
	// non-blocking headless gate instead — passed into boot.Build so every
	// headless-only gate it constructs (task/read_only_task, writer-capable
	// skill sub-agents, the planner runner) gets the same contract as the parent
	// executor, not just the top-level one. Default/ask fails closed because no
	// UI can answer; unattended writes require explicit --auto/-y,
	// --permission-mode auto, or yolo.
	overrides := runBuildOverrides(effortOverride, allowedTools, additionalDirs, workspaceRoot,
		permissions.approval, cliSessionRecoveredHandler(leases), ablated)
	overrides.Version = version
	ctrl, err := setupProfileWithOverrides(ctx, *model, *maxSteps, true, sink, profile, overrides)
	if err != nil {
		if resultOutput != nil && format != runOutputText {
			if encodeErr := resultOutput.Finalize("", started, err); encodeErr != nil {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, encodeErr)
			}
			return 1
		}
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()
	SetTaskJobKiller(ctrlKillerAdapter{ctrl})
	ctrl.ApplyHeadlessApprovalMode(permissions.approval)

	if err := bindRunSession(ctrl, leases, resumeSession, resumePath); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, control.SessionInUseMessage(err)+"; "+control.SessionLeaseCloseHint)
		return 1
	}

	recordTrajectoryHeader(chain.trajectory, ctrl)
	runErr := ctrl.Run(ctx, prompt)
	reporter.RecordRecovery(ctrl.DrainRecoveryMetrics())
	completion := classifyRunCompletion(runErr)
	if cfg != nil {
		notify.SendEvent(newNotificationSender(), i18n.M, cfg.Notifications, event.Event{
			Kind:    event.TurnDone,
			Err:     runErr,
			Outcome: completion.outcome,
		})
	}
	if metrics != nil {
		// Snapshot under the sink's lock: a background job can still be emitting
		// into it while this goroutine assembles the final record.
		final := metrics.Snapshot()
		final.DurationMs = time.Since(started).Milliseconds()
		final.Outcome = completion.class
		final.Arm = ablated.Arm()
		if exec := ctrl.Executor(); exec != nil {
			if audit := exec.CapabilityAudit(); audit != nil {
				snap := audit.Snapshot()
				final.MergeCapabilityAuditCounters(
					snap.Routes, snap.RoutedCandidates, snap.RoutedRequire, snap.RoutedPrefer, snap.RoutedSuggest, snap.Declines,
					snap.Router.SemanticRoutes, snap.Router.SemanticFallbacks,
					snap.RequireMissing, snap.RequireRecovered, snap.PreferMissing, snap.PreferRecovered,
					snap.SkillInvocations, snap.SkillFailures, snap.SkillUnavailable,
					snap.MCPInspect, snap.MCPCall, snap.MCPCallFailures,
					snap.ReviewBlocks, snap.SecurityReviewBlocks,
					snap.Router.PromptTokens, snap.Router.CompletionTokens,
					snap.Router.Cost, snap.Router.LatencyMs,
				)
			}
		}
		if err := writeMetrics(*metricsPath, final); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		}
	}
	if chain.trajectory != nil {
		if err := chain.trajectory.Close(); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		}
	}
	if resultOutput != nil {
		sessionID := runOutputSessionID(format, sessionstore.BranchID(ctrl.SessionPath()), machineIdentityKey)
		if err := resultOutput.Finalize(sessionID, started, runErr); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
	}
	if runErr != nil {
		reportRunFailure(os.Stderr, format, resultOutput != nil, completion, runErr)
		return completion.exitCode
	}
	return completion.exitCode
}

// defaultConfigTarget is the user-global config file, falling back to a
// project-local reasonix.toml only when the user config dir can't be resolved.
type setupTargets struct {
	config string
	env    string
}

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
		// Non-interactive must not clobber an existing config silently. On a TTY,
		// setup is a non-destructive configuration manager, so opening an existing
		// file no longer needs an overwrite confirmation.
		if !isInteractive() {
			fmt.Fprintf(os.Stderr, i18n.M.NotOverwritingFmt+"\n", path)
			return 1
		}
	}

	// Interactive wizard on a TTY; fall back to the annotated default when piped.
	if isInteractive() {
		rc := interactiveSetup(t.config, t.env)
		if rc == 0 {
			fmt.Printf(i18n.M.TryHintFmt+"\n", termrender.Bold("reasonix"))
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
	unlock, err := config.LockConfigFileEdits(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
	defer unlock()
	if _, err := os.Lstat(path); err == nil {
		fmt.Fprintf(os.Stderr, i18n.M.NotOverwritingFmt+"\n", path)
		return 1
	} else if !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
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

// interactiveSetup opens the staged provider manager. Nothing is written until
// the user explicitly chooses Save and exit; q/Ctrl-C leaves both config and
// credentials untouched.
func interactiveSetup(configPath, envPath string) int {
	// Seed from the existing config when reconfiguring, so a re-run to fix a key
	// preserves the user's providers / agent settings instead of resetting to
	// defaults. First run (no file) falls back to the built-in defaults.
	cfg, err := config.LoadForEditReadOnlyStrict(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
	session := newProviderSetupSessionForPath(cfg, configPath)
	lang, err := selectLanguage()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nsetup cancelled.")
		return 1
	}
	session.setLanguage(lang)
	session.applyOfficialDefaultPricing()
	session.resetProviderSummaryBaseline()
	i18n.DetectLanguage(lang)

	// Now that the catalogue matches the user's choice, show the welcome banner
	// in their language before any substantive prompt.
	fmt.Println()
	fmt.Print(termrender.Boxed([]string{
		termrender.Accent("◆") + " " + fmt.Sprintf(i18n.M.WelcomeTitleFmt, termrender.Bold("reasonix")),
		"",
		termrender.Dim(i18n.M.NoConfigYet),
	}))
	fmt.Println()

	return runProviderSetupManager(session, configPath, envPath)
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
	return slices.Contains(xs, v)
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
	if stem[0] >= '0' && stem[0] <= '9' {
		stem = "CUSTOM_" + stem
	}
	return stem + "_API_KEY"
}

type providerKeyEnvRepair struct {
	provider string
	old      string
	new      string
}

func repairInvalidProviderKeyEnvs(providers []config.ProviderEntry) ([]config.ProviderEntry, []providerKeyEnvRepair) {
	providers = append([]config.ProviderEntry(nil), providers...)
	var repairs []providerKeyEnvRepair
	for i := range providers {
		old := strings.TrimSpace(providers[i].APIKeyEnv)
		if old == "" || config.IsValidCredentialKey(old) {
			continue
		}
		keyEnv := apiKeyEnvFromProviderName(providers[i].Name)
		providers[i].APIKeyEnv = keyEnv
		repairs = append(repairs, providerKeyEnvRepair{provider: providers[i].Name, old: old, new: keyEnv})
	}
	return providers, repairs
}

func promptAPIKeyEnvName(in *bufio.Scanner, w io.Writer, label, def string) string {
	for {
		keyEnv := ask(in, w, label, def)
		if config.IsValidCredentialKey(keyEnv) {
			return keyEnv
		}
		fmt.Fprintf(w, i18n.M.InvalidAPIKeyEnvFmt+"\n", keyEnv)
	}
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

type providerPromptResult struct {
	entries     []config.ProviderEntry
	credentials map[string]string
}

func newProviderPromptResult(entries []config.ProviderEntry, key, value string) providerPromptResult {
	result := providerPromptResult{entries: entries}
	if key != "" && value != "" {
		result.credentials = map[string]string{key: value}
	}
	return result
}

// promptAnthropicProvider handles the Anthropic compatible provider entry flow.
func promptAnthropicProvider() (providerPromptResult, error) {
	methodIdx, err := selectOne(i18n.M.AnthropicAddMethodLabel, []menuItem{
		{name: i18n.M.AnthropicMethodManual},
		{name: i18n.M.AnthropicMethodURL},
	})
	if err != nil {
		return providerPromptResult{}, err
	}
	if methodIdx == 0 {
		return promptAnthropicProviderManual()
	}
	return promptAnthropicProviderFromURL()
}

// promptAnthropicProviderManual handles manual model entry.
func promptAnthropicProviderManual() (providerPromptResult, error) {
	return promptAnthropicProviderManualWith(bufio.NewScanner(os.Stdin), "", "", "")
}

// promptAnthropicProviderManualWith is the shared backend for manual entry
// of an Anthropic-compatible custom provider. Pre-filled values (baseURL,
// keyEnv, apiKey) are reused as-is when non-empty so the URL-fetch flow
// can fall through to manual entry without re-asking the user.
func promptAnthropicProviderManualWith(in *bufio.Scanner, baseURL, keyEnv, apiKey string) (providerPromptResult, error) {
	fmt.Println()
	if baseURL == "" {
		baseURL = ask(in, os.Stdout, i18n.M.AnthropicPromptBaseURL, "")
		if baseURL == "" {
			return providerPromptResult{}, fmt.Errorf("base URL is required")
		}
	}
	modelName := ask(in, os.Stdout, i18n.M.AnthropicPromptModel, "")
	if modelName == "" {
		return providerPromptResult{}, fmt.Errorf("model name is required")
	}
	if keyEnv == "" {
		keyEnv = promptAPIKeyEnvName(in, os.Stdout, i18n.M.AnthropicPromptKeyEnv, "ANTHROPIC_API_KEY")
	} else if !config.IsValidCredentialKey(keyEnv) {
		return providerPromptResult{}, fmt.Errorf("invalid API key variable name %q", keyEnv)
	}
	if apiKey == "" {
		apiKey = ask(in, os.Stdout, i18n.M.AnthropicPromptAPIKey, "")
	}
	entry := config.ProviderEntry{
		Name: providerSlug("anthropic", baseURL), Kind: "anthropic", BaseURL: baseURL,
		Model: modelName, APIKeyEnv: keyEnv, ContextWindow: askContextWindow(in, os.Stdout),
	}
	fmt.Printf("  %s\n", termrender.Green(fmt.Sprintf(i18n.M.AnthropicAddedFmt, entry.Name+"/"+modelName)))
	return newProviderPromptResult([]config.ProviderEntry{entry}, keyEnv, apiKey), nil
}

// promptAnthropicProviderFromURL tries the OpenAI-compatible GET /models
// endpoint (some Anthropic-compatible proxies do expose one). Most don't
// — Anthropic's own API has no public model list — so on any failure the
// flow falls through to manual entry with the URL/key already filled in,
// rather than aborting the wizard.
func promptAnthropicProviderFromURL() (providerPromptResult, error) {
	in := bufio.NewScanner(os.Stdin)
	fmt.Println()

	baseURL := ask(in, os.Stdout, i18n.M.AnthropicPromptBaseURL, "")
	if baseURL == "" {
		return providerPromptResult{}, fmt.Errorf("base URL is required")
	}
	keyEnv := promptAPIKeyEnvName(in, os.Stdout, i18n.M.AnthropicPromptKeyEnv, "ANTHROPIC_API_KEY")
	apiKey := ask(in, os.Stdout, i18n.M.AnthropicPromptAPIKey, "")

	fmt.Printf("  %s\n", termrender.Dim(fmt.Sprintf(i18n.M.AnthropicFetchingModelsFmt, "anthropic")))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := fetchModelListCompat(ctx, baseURL, apiKey)
	if err != nil || len(models) == 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s\n", termrender.Dim(fmt.Sprintf(i18n.M.AnthropicFetchModelsFailedFmt, "anthropic", err)))
		} else {
			fmt.Fprintf(os.Stderr, "  %s\n", termrender.Dim(i18n.M.AnthropicFetchEmpty))
		}
		return promptAnthropicProviderManualWith(in, baseURL, keyEnv, apiKey)
	}
	fmt.Printf("  %s\n", termrender.Green(fmt.Sprintf(i18n.M.AnthropicFetchModelsSuccessFmt, len(models), "anthropic")))

	items := make([]menuItem, len(models))
	for i, m := range models {
		items[i] = menuItem{name: m}
	}
	idxs, err := selectMany(fmt.Sprintf(i18n.M.AnthropicSelectModelsLabel, "anthropic"), items)
	if err != nil || len(idxs) == 0 {
		return providerPromptResult{}, fmt.Errorf("no models selected")
	}
	var selected []string
	for _, i := range idxs {
		selected = append(selected, models[i])
	}
	entry := config.ProviderEntry{
		Name: providerSlug("anthropic", baseURL), Kind: "anthropic", BaseURL: baseURL,
		Models: selected, Model: selected[0], APIKeyEnv: keyEnv, ContextWindow: askContextWindow(in, os.Stdout),
	}
	fmt.Printf("  %s\n", termrender.Green(fmt.Sprintf(i18n.M.AnthropicAddedFmt, entry.Name+"/"+selected[0])))
	return newProviderPromptResult([]config.ProviderEntry{entry}, keyEnv, apiKey), nil
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
	defaults.ApplyOfficialDefaultPricing()
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
	if len(cfg.Agent.SubagentModels) > 0 {
		keys := make([]string, 0, len(cfg.Agent.SubagentModels))
		for key := range cfg.Agent.SubagentModels {
			keys = append(keys, key)
		}
		slices.Sort(keys)
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
			fmt.Fprintf(w, "  %s %s\n", termrender.Green("✓"), fmt.Sprintf(i18n.M.APIKeyAlreadySetFmt, p.APIKeyEnv))
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
		for raw := range strings.SplitSeq(string(data), "\n") {
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

// normalizeCommand names the subcommand argv asks for. -p/--print is one-shot
// print mode and reasonix has no interactive -p, so a print flag anywhere in a
// leading flag run routes the whole set to `run --print`: `reasonix --model X
// -p "task"` works, not only `reasonix -p ...`. Other leading flags name none.
func normalizeCommand(args []string) (string, []string) {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	if cmd == "--acp" {
		cmd = "acp"
	}
	if cmd == "-p" || cmd == "--print" || (isDefaultInteractiveFlag(cmd) && hasLeadingPrintFlag(args)) {
		return "run", append([]string{"run", "--print"}, stripLeadingPrintFlag(args)...)
	}
	if len(args) > 0 && isDefaultInteractiveFlag(cmd) {
		cmd = ""
	}
	return cmd, args
}

func usage() {
	fmt.Print(i18n.M.UsageBody)
}

// bareUsage answers bare argv: there is no interactive session to fall into.
// A console this process owns alone was opened by a double-click and closes
// on exit, so it points at Studio and waits instead of vanishing.
func bareUsage() int {
	configureThemeForTTYOutput()
	usage()
	if ownsConsoleAlone() {
		fmt.Print("\n" + i18n.M.StandaloneConsoleHint)
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	return 2
}

type ctrlKillerAdapter struct{ ctrl *control.Controller }

func (a ctrlKillerAdapter) Kill(sessionID, id string) bool {
	if sessionID != "" && sessionstore.BranchID(a.ctrl.SessionPath()) != sessionID {
		return false
	}
	return a.ctrl.CancelJob(id)
}

func configCommand(args []string) int {
	if len(args) == 0 {
		configUsage()
		return 2
	}
	switch args[0] {
	case "auto-plan":
		return configAutoPlanCompatibilityCommand(args[1:])
	case "reasoning-language":
		return configReasoningLanguageCommand(args[1:])
	case "compact-ratio":
		return configCompactRatioCommand(args[1:])
	case "currency":
		return configCurrencyCommand(args[1:])
	case "telemetry":
		return configTelemetryCommand(args[1:])
	default:
		configUsage()
		return 2
	}
}

func configCurrencyCommand(args []string) int {
	fs := flag.NewFlagSet("config currency", flag.ContinueOnError)
	local := fs.Bool("local", false, "unsupported; pricing currency is user-level only")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if *local {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "currency is user-level only; --local is not supported")
		return 2
	}
	rest := fs.Args()
	if len(rest) > 1 {
		configCurrencyUsage()
		return 2
	}
	if len(rest) == 0 {
		cfg, err := config.LoadForRootReadOnly(".")
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("currency = %q (display: %s)\n", pricingCurrencyDisplay(cfg.DisplayCurrencyPref()), cfg.ResolveDisplayCurrency())
		return 0
	}
	mode, err := parseCLIPricingCurrency(rest[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	path := config.UserConfigPath()
	if path == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "cannot resolve user config path")
		return 1
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetDisplayCurrency(mode); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	resolved := cfg.ResolveDisplayCurrency()
	if err := cfg.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("currency = %q (display: %s, %s)\n", pricingCurrencyDisplay(mode), resolved, displayPath(path))
	return 0
}

var (
	cleanupCLITelemetry        = telemetry.Cleanup
	startCLITelemetryReporter  = telemetry.Start
	persistCLITelemetryConsent = func(mode string) error {
		path := config.UserConfigPath()
		if strings.TrimSpace(path) == "" {
			return errors.New("cannot resolve config path")
		}
		unlock := config.LockUserConfigEdits()
		defer unlock()
		cfg, err := config.LoadForEditReadOnlyStrict(path)
		if err != nil {
			return err
		}
		if err := cfg.SetCLITelemetryMode(mode); err != nil {
			return err
		}
		return cfg.SaveTo(path)
	}
)

func configTelemetryCommand(args []string) int {
	fs := flag.NewFlagSet("config telemetry", flag.ContinueOnError)
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	rest := fs.Args()
	if len(rest) > 1 {
		configTelemetryUsage()
		return 2
	}
	if len(rest) == 0 {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("cli_metrics = %q\n", cfg.CLITelemetryMode())
		return 0
	}
	path := config.UserConfigPath()
	if path == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "cannot resolve config path")
		return 1
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetCLITelemetryMode(rest[0]); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if err := cfg.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if cfg.CLITelemetryMode() == "off" {
		if err := cleanupCLITelemetry(config.ReasonixHomeDir()); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "telemetry disabled, but pending metrics could not be deleted:", err)
			return 1
		}
	}
	fmt.Printf("cli_metrics = %q (%s)\n", cfg.CLITelemetryMode(), displayPath(path))
	return 0
}

// configAutoPlanCompatibilityCommand preserves the released shell interface
// without restoring Automatic Plan Mode. Reading and writing "off" are safe
// no-ops; every attempt to enable the retired feature is rejected.
func configAutoPlanCompatibilityCommand(args []string) int {
	fs := flag.NewFlagSet("config auto-plan", flag.ContinueOnError)
	local := fs.Bool("local", false, "unsupported; automatic plan mode is retired")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if *local {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "auto-plan is user-level only; --local is not supported")
		return 2
	}
	rest := fs.Args()
	if len(rest) > 1 {
		configAutoPlanCompatibilityUsage()
		return 2
	}
	if len(rest) == 0 {
		fmt.Println(`auto_plan = "off"`)
		return 0
	}
	cfg := config.Default()
	if err := cfg.SetAutoPlan(rest[0]); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	fmt.Println(`auto_plan = "off"`)
	return 0
}

func configReasoningLanguageCommand(args []string) int {
	fs := flag.NewFlagSet("config reasoning-language", flag.ContinueOnError)
	local := fs.Bool("local", false, "write ./reasonix.toml instead of the user config")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
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
	unlock, err := config.LockConfigFileEdits(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer unlock()
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
	cfg, err := config.LoadForEditReadOnlyStrict(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
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

func configCompactRatioCommand(args []string) int {
	fs := flag.NewFlagSet("config compact-ratio", flag.ContinueOnError)
	local := fs.Bool("local", false, "write ./reasonix.toml instead of the user config")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) > 1 {
		configCompactRatioUsage()
		return 2
	}
	if len(rest) == 0 {
		cfg, err := config.LoadForRootReadOnly(".")
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("compact_ratio = %s (%s)\n", formatCompactRatioPercent(cfg.Agent.CompactRatio), compactRatioSource())
		return 0
	}
	percent, err := strconv.ParseFloat(strings.TrimSpace(rest[0]), 64)
	if err != nil || math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 65 || percent > 85 {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "compact ratio must be a percentage between 65 and 85")
		return 2
	}
	ratio := percent / 100
	path := config.UserConfigPath()
	scope := "user"
	if *local {
		path = "reasonix.toml"
		scope = "project"
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "cannot resolve config path")
		return 1
	}
	unlock, err := config.LockConfigFileEdits(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer unlock()
	if *local {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			saved, err := config.SaveMinimalProjectCompactRatio(path, ratio)
			if err != nil {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
				return 1
			}
			fmt.Printf("compact_ratio = %s (%s: %s)\n", formatCompactRatioPercent(saved), scope, displayPath(path))
			return 0
		} else if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
	}
	cfg, err := config.LoadForEditReadOnlyStrict(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if err := cfg.SetCompactRatio(ratio); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if err := cfg.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("compact_ratio = %s (%s: %s)\n", formatCompactRatioPercent(cfg.Agent.CompactRatio), scope, displayPath(path))
	return 0
}

func compactRatioSource() string {
	if config.ConfigFileDefinesCompactRatio("reasonix.toml") {
		return "project: " + displayPath("reasonix.toml")
	}
	if path := config.UserConfigPath(); path != "" && config.ConfigFileDefinesCompactRatio(path) {
		return "user: " + displayPath(path)
	}
	return "built-in default"
}

func formatCompactRatioPercent(ratio float64) string {
	value := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", ratio*100), "0"), ".")
	return value + "%"
}

func configUsage() {
	fmt.Print(`Usage:
  reasonix config reasoning-language [--local] [auto|zh|en]
  reasonix config compact-ratio [--local] [65..85]
  reasonix config currency [auto|CNY|USD]
  reasonix config telemetry [auto|on|off]
`)
}

func configTelemetryUsage() {
	fmt.Print(`Usage:
  reasonix config telemetry [auto|on|off]
`)
}

func configCompactRatioUsage() {
	fmt.Print(`Usage:
  reasonix config compact-ratio [--local] [65..85]
`)
}

func startCLITelemetry(cfg *config.Config, opts telemetry.Options) *telemetry.Reporter {
	return startCLITelemetryWithIO(cfg, opts, os.Stdin, os.Stdout, os.Stderr)
}

func startCLITelemetryWithIO(cfg *config.Config, opts telemetry.Options, in io.Reader, out, errOut io.Writer) *telemetry.Reporter {
	if cfg == nil {
		cfg = config.Default()
	}
	opts.Mode = cfg.CLITelemetryMode()
	opts.HomeDir = config.ReasonixHomeDir()
	opts.Proxy = cfg.NetworkProxySpec()
	opts.Language = cfg.Language

	if cfg.CLITelemetryConfigured() || !telemetry.Enabled(opts.Mode, opts.Version, opts.Interactive) {
		return startCLITelemetryReporter(opts)
	}

	fmt.Fprintln(out, i18n.M.CLITelemetryConsentNotice)
	scanner := bufio.NewScanner(in)
	mode := ""
	for mode == "" {
		answer := strings.ToLower(strings.TrimSpace(ask(scanner, out, i18n.M.CLITelemetryConsentPrompt, "Y/n")))
		switch answer {
		case "y", "yes", "y/n":
			mode = "auto"
		case "n", "no":
			mode = "off"
		default:
			fmt.Fprintln(out, i18n.M.CLITelemetryConsentInvalid)
		}
	}

	if err := persistCLITelemetryConsent(mode); err != nil {
		fmt.Fprintf(errOut, i18n.M.CLITelemetryConsentSaveFailedFmt+"\n", err)
		return nil
	}
	cfg.Telemetry.CLIMetrics = mode
	opts.Mode = mode
	if mode == "off" {
		if err := cleanupCLITelemetry(opts.HomeDir); err != nil {
			fmt.Fprintf(errOut, i18n.M.CLITelemetryConsentCleanupFailedFmt+"\n", err)
		}
		return nil
	}
	return startCLITelemetryReporter(opts)
}

func cliTelemetrySessionMode(cont, resume, copySession bool) string {
	switch {
	case copySession:
		return "copy"
	case resume:
		return "resume"
	case cont:
		return "continue"
	default:
		return "fresh"
	}
}

func configAutoPlanCompatibilityUsage() {
	fmt.Print(`Usage:
  reasonix config auto-plan [off]
`)
}

func configReasoningLanguageUsage() {
	fmt.Print(`Usage:
  reasonix config reasoning-language [--local] [auto|zh|en]
`)
}

func configCurrencyUsage() {
	fmt.Print(`Usage:
  reasonix config currency [auto|CNY|USD]
`)
}

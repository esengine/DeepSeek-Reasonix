// Package boot assembles a ready-to-drive control.Controller from configuration:
// it loads config, resolves the model(s), builds the tool registry (built-ins +
// plugins), wires the permission gate, and constructs the executor — optionally
// wrapping it in a two-model Coordinator. It is the one place that turns "what the
// user configured" into "a Controller a frontend can drive", so every frontend —
// the terminal TUI, the HTTP/SSE server, the desktop webview — shares the exact
// same assembly instead of each re-deriving it. Frontends pass only a sink and a
// couple of run knobs; everything else comes from config.
package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/capability"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/providerext"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/extension/uihub"
	"reasonix/internal/goaleval"
	"reasonix/internal/guardian"
	"reasonix/internal/history"
	"reasonix/internal/hook"
	"reasonix/internal/lsp"
	"reasonix/internal/mcplaunch"
	"reasonix/internal/memory"
	"reasonix/internal/netclient"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/pluginspec"
	"reasonix/internal/productdocs"
	"reasonix/internal/provider"
	"reasonix/internal/recovery"
	"reasonix/internal/sandbox"
	"reasonix/internal/secrets"
	"reasonix/internal/sessiontemp"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
	"reasonix/internal/tool/sessiontool"
)

// ErrUnknownModel is returned by Build when the configured model can't be
// resolved to a provider — e.g. a default_model left over from a renamed or
// removed provider. Callers can detect it (errors.Is) to re-run setup.
var ErrUnknownModel = errors.New("unknown model")

func agentKeepPolicy(keep []string) agent.KeepPolicy {
	if keep == nil {
		return agent.KeepErrors | agent.KeepUserMarked
	}
	var p agent.KeepPolicy
	for _, k := range keep {
		switch strings.TrimSpace(k) {
		case "errors":
			p |= agent.KeepErrors
		case "user_marked":
			p |= agent.KeepUserMarked
		}
	}
	return p
}

func recoveryHeadlessMode(opts Options) bool {
	return strings.TrimSpace(opts.HeadlessApprovalMode) != ""
}

// build is the assembly body behind BuildRuntime (and the Build compat
// wrapper): it loads config, resolves the model(s), wires the full runtime,
// and freezes the extension kernel snapshot from the objects it just
// assembled. The returned controller owns plugin subprocesses; call Close
// (via Controller.Close) to release them.
func build(ctx context.Context, opts Options) (*BuildResult, error) {
	timer := newPhaseTimer()
	ctx, opts, owner, fileWriteReceipt := bindRuntimeOwner(ctx, opts)
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	root, roots := resolveWorkspaceRoot(opts.WorkspaceRoot), opts.roots()
	additionalDirs, err := normalizeAdditionalDirs(root, opts.AdditionalDirs)
	if err != nil {
		return nil, err
	}
	// Import v1/v0.5 config before Load so this boot sees the new config + ~/.env.
	// CLI Run also calls this before config-only commands; keep a shared fallback.
	migrated, migErr := roots.MigrateLegacyIfNeededForRoot(root)
	deepSeekProtocolMigrated, deepSeekProtocolMigErr := roots.MigrateLegacyDeepSeekProtocolUserConfig()
	stepLimitsMigrated, stepLimitMigErr := roots.MigrateLegacyAgentStepLimitsForRoot(root)
	redactToolOutputMigrated, redactToolOutputMigErr := roots.MigrateLegacyRedactToolOutputForRoot(root)
	memoryCompilerMigrated, memoryCompilerMigErr := roots.MigrateLegacyMemoryCompilerForRoot(root)
	multiThresholdMigrated, multiThresholdMigErr := roots.MigrateLegacyMultiThresholdCompactionForRoot(root)
	cfg, err := roots.LoadForRoot(root)
	if err != nil {
		return nil, err
	}
	deepSeekProtocolMigErr = deepSeekProtocolMigrationNoticeError(handleConfigLoadWarnings(opts, cfg), deepSeekProtocolMigErr)
	// Arm the credential-protection layers from the user-global [secrets]
	// section before any tool, hook, or plugin subprocess can spawn. Package
	// globals are correct here because [secrets] is user-global (project
	// reasonix.toml cannot override it), so concurrent workspaces agree.
	secrets.SetFilterSubprocessEnv(cfg.Secrets.FilterSubprocessEnv)
	secrets.SetProtectSensitiveFiles(cfg.Secrets.ProtectSensitiveFiles)
	secrets.SetProtectCredentialFiles(cfg.Secrets.ProtectCredentialFiles)
	secrets.RegisterCredentialEnvKeys(cfg.CredentialEnvNames())

	// Serialize the frontend's sink once: background jobs (below) emit from their
	// own goroutines, which can overlap a running turn's emission, so every emitter
	// shares this synchronized sink. It is created before extension preflight so
	// sidecar warnings and host/ui/* publishes land on the same channel as every
	// later notice. The job manager is session-scoped — its jobs outlive a turn
	// and are cancelled by Controller.Close.
	//
	sink := quotedSink(cfg, opts)

	// Both sink wraps must complete BEFORE the extension UI hub closes over the
	// sink variable: a sidecar publish during preflight lands on this closure
	// from a wire-handler goroutine, and any later reassignment races it.
	// Goal token-budget accounting: the controller detects this tee and
	// attributes billable usage to the active goal turn's recorder. Both the
	// tee and the delta coalescer must ride the shared sink agents emit into
	// directly — wrapping only the controller's reference would leave the
	// executor's per-chunk Text/Reasoning stream uncoalesced.
	sink = control.NewGoalUsageTee(event.Coalesce(sink, event.DefaultStreamDeltaWindow))

	// Extension preflight (stages 5b/7): start the installed, enabled v2 runtime
	// packages ONCE, here, before model resolution, so plugin-namespaced refs
	// (plugin/<plugin>/<provider>/<model>) resolve on the very first boot and the
	// same sidecar generation feeds the executor, planner, guardian, sub-agents,
	// the snapshot assembly, and the frontend catalog. With no runtime package
	// installed preflight is a no-op and the whole build below takes the
	// untouched pre-sidecar path. The generation moves up with it: the sidecar
	// handshake's session context carries this build's generation, and a fresh
	// controller has no session path yet, so the session ID is generation-scoped
	// (the handshake only requires a stable, non-empty identity).
	generation := nextRuntimeGeneration()
	sessionID := fmt.Sprintf("boot-%d", generation)
	proxySpec := cfg.NetworkProxySpec()
	extWarn := func(msg string) {
		redacted := secrets.RedactCredentials(msg)
		slog.Warn("boot: extension runtime: "+redacted, "root", root)
		report(sink, event.Event{Level: event.LevelWarn, Text: redacted})
	}
	// Stage 8a: the host extension UI hub serves every sidecar's host/ui/* calls
	// for this generation — publications become frontend events through the
	// controller sink, blocking prompts ride the controller's Ask channel. The
	// controller only exists after control.New below, so both seams indirect
	// through ctrlRef; traffic before that (a sidecar publishing during its
	// handshake) falls back to the same sink directly, matching the emission the
	// controller would have made.
	var ctrlRef atomic.Pointer[control.Controller]
	// Readiness signals for gateExtensionUIRequest: a sidecar may legally
	// issue host/ui/request right after extension/initialized, before the
	// controller exists. ready closes at ctrlRef.Store; failed closes on any
	// build error before the RuntimeSet takes ownership (the pendingMgr defer
	// below), so a startup request never hangs a dying build.
	controllerReady := make(chan struct{})
	controllerBuildFailed := make(chan struct{})
	extUIHub := uihub.New(uihub.Options{
		SessionID:  sessionID,
		Generation: generation,
		Owner:      owner,
		Emit: func(ev event.Event) {
			if c := ctrlRef.Load(); c != nil {
				c.EmitExtensionEvent(ev)
				return
			}
			sink.Emit(ev)
		},
		Request: func(reqCtx context.Context, req uihub.HubRequest) (map[string]any, bool, error) {
			return gateExtensionUIRequest(reqCtx, ctrlRef.Load, controllerReady, controllerBuildFailed,
				func(c *control.Controller) (map[string]any, bool, error) {
					return uihub.AskRequestFunc(c.Ask)(reqCtx, req)
				})
		},
		Warn: func(msg string) {
			slog.Warn("boot: extension UI hub: "+msg, "root", root)
		},
	})
	extensionMgr, err := preflightExtensionRuntimes(ctx, roots.Home(), extensionBoot{
		session:   protocol.SessionContext{SessionID: sessionID, WorkspaceRoot: root, Generation: generation},
		ui:        extUIHub,
		onWarning: extWarn,
	}, opts.Extensions, planForPreflight(opts, generation))
	if err != nil {
		return nil, fmt.Errorf("boot: %w", err)
	}
	timer.mark("extensions")
	// Until the RuntimeSet takes ownership at snapshot assembly, every error
	// path between here and there must retire the preflighted sidecars — no
	// process may outlive a failed build.
	pendingMgr := extensionMgr
	defer func() {
		if pendingMgr != nil {
			close(controllerBuildFailed)
			_ = pendingMgr.Close()
		}
	}()

	// The build's provider resolution base: the caller-owned broker when
	// injected, the local config-backed resolver otherwise. When a started
	// sidecar declares providers, fold them in NOW (stage 7) with the
	// provider:<ref> slot claims from the same manifest data the kernel's
	// ReplaceClaims pass uses, so first-boot model resolution sees them. A
	// conflict with the base catalog that lacks the plugin's claim is fatal,
	// the same class as a required runtime that cannot start: booting without
	// the declared provider would silently change what the session is.
	baseResolver := opts.ProviderResolver
	if baseResolver == nil {
		baseResolver = NewLocalProviderResolver(cfg, proxySpec)
	}
	effectiveResolver := opts.ProviderResolver
	var extensionResolver provider.Resolver
	if extensionMgr != nil {
		declares := false
		for _, client := range extensionMgr.Clients() {
			if len(client.Handshake().Providers) > 0 {
				declares = true
				break
			}
		}
		if declares {
			claims, claimsErr := resolveReplacementClaims(extensionMgr.Contributions())
			if claimsErr != nil {
				return nil, fmt.Errorf("boot: %w", claimsErr)
			}
			merged, mergeErr := mergeSidecarProviders(baseResolver, extensionMgr, claims, owner)
			if mergeErr != nil {
				return nil, fmt.Errorf("boot: %w", mergeErr)
			}
			installSidecarStreamRouters(extensionMgr, merged)
			effectiveResolver = merged
			extensionResolver = merged
		}
	}

	// Fall through a keyless default_model to the next configured chat model
	// instead of hard-failing every command on "missing env X_API_KEY" (issue
	// #6996). The fallback only kicks in when the caller did not pass an
	// explicit opts.Model; explicit choices still fail loudly.
	modelName := opts.Model
	if modelName == "" {
		if resolved, _, ok := cfg.ResolveNewSessionChatModel(); ok {
			modelName = resolved
		}
	}
	config.NormalizeLegacyMimoCustomProvidersForRefs(cfg, modelName)
	agentPreset := strings.TrimSpace(opts.AgentPreset)
	if agentPreset == "" {
		agentPreset = AgentPresetFromTokenMode(opts.TokenMode)
	}
	agentPreset = NormalizeAgentPreset(agentPreset)
	tokenDelivery := agentPreset == AgentPresetDelivery
	runtimeProfile := capability.ProfileBalanced
	if agentPreset == AgentPresetDelivery {
		runtimeProfile = capability.ProfileDelivery
	}
	keepPolicy := agentKeepPolicy(cfg.Agent.Keep)
	// Entry resolution: the caller-owned broker is authoritative for every
	// ref; the extension-merged resolver only owns plugin refs — a config ref
	// keeps the full config entry (kind, endpoint, credentials, balance URL,
	// missing-key notice), exactly as without extensions installed.
	entryResolver := opts.ProviderResolver
	if entryResolver == nil && extensionResolver != nil && providerext.PluginRefOwner(modelName) != "" {
		entryResolver = extensionResolver
	}
	entry, modelRef, err := resolveModelEntry(entryResolver, cfg, modelName)
	if err != nil {
		return nil, err
	}
	if opts.EffortOverride != nil {
		entry.Effort = *opts.EffortOverride
		if entry.Kind == "anthropic" && strings.TrimSpace(entry.Effort) != "" && strings.TrimSpace(entry.Thinking) == "" {
			entry.Thinking = "adaptive"
		}
	}
	// RequireKey fails fast on a missing credential (run/serve); plugin-
	// namespaced refs carry no config credential — the extension provider holds
	// its own keys — so the merged resolver's resolution is their only gate.
	if opts.RequireKey && opts.ProviderResolver == nil && providerext.PluginRefOwner(modelName) == "" {
		if err := cfg.Validate(modelName); err != nil {
			return nil, err
		}
	}

	if migErr != nil {
		report(sink, event.Event{Level: event.LevelWarn, Text: "Config migration did not complete.", Detail: "config migration from ~/.reasonix failed: " + migErr.Error()})
	} else if migrated != nil {
		report(sink, event.Event{Level: event.LevelInfo, Text: migrated.Notice()})
	}
	if deepSeekProtocolMigrated {
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelInfo,
			Text:   "DeepSeek official access was upgraded to Anthropic Messages.",
			Detail: "Your unmodified legacy OpenAI Chat Completions configuration now uses DeepSeek's recommended Anthropic endpoint with server-side web search. Existing model names and pricing were preserved. The first request starts a new provider cache prefix; later requests rebuild normal prefix-cache reuse.",
		})
	} else if deepSeekProtocolMigErr != nil {
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelWarn,
			Text:   "DeepSeek protocol migration did not complete.",
			Detail: deepSeekProtocolMigErr.Error(),
		})
	}
	if stepLimitsMigrated || cfg.IgnoredLegacyAgentStepLimits() {
		level := event.LevelInfo
		text := "Deprecated agent step limits were removed."
		detail := "[agent].max_steps and planner_max_steps are no longer used; Reasonix now manages interactive progress automatically. " +
			"Use the CLI --max-steps flag for a one-off run or [bot].max_steps for unattended bot sessions."
		if stepLimitMigErr != nil {
			level = event.LevelWarn
			text = "Deprecated agent step limits were ignored."
			detail += " The old keys were ignored but could not be removed: " + stepLimitMigErr.Error()
		}
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  level,
			Text:   text,
			Detail: detail,
		})
	} else if stepLimitMigErr != nil {
		report(sink, event.Event{Level: event.LevelWarn, Text: "Deprecated agent step-limit migration did not complete.", Detail: stepLimitMigErr.Error()})
	}
	if redactToolOutputMigrated || redactToolOutputMigErr != nil {
		level := event.LevelInfo
		text := "Deprecated redact_tool_output setting was removed."
		detail := "[secrets].redact_tool_output no longer has any effect: ordinary model/tool content and local session/job artifacts now preserve their original text. Explicit diagnostics and reasonix doctor redact-sessions still redact credential values."
		if redactToolOutputMigErr != nil {
			level = event.LevelWarn
			text = "Deprecated redact_tool_output setting was ignored."
			detail += " The old key could not be removed: " + redactToolOutputMigErr.Error()
		}
		report(sink, event.Event{Level: level, Text: text, Detail: detail})
	}
	if memoryCompilerMigrated || memoryCompilerMigErr != nil {
		level := event.LevelInfo
		text := "Deprecated memory_compiler setting was removed."
		detail := "The Memory v5 execution compiler has been removed from Reasonix: [agent].memory_compiler no longer has any effect, user turns are never replaced by compiled execution contracts, and no compiler state is written. Old transcripts containing compiled turns still display normally."
		if memoryCompilerMigErr != nil {
			level = event.LevelWarn
			text = "Deprecated memory_compiler setting was ignored."
			detail += " The old key could not be removed: " + memoryCompilerMigErr.Error()
		}
		report(sink, event.Event{Level: level, Text: text, Detail: detail})
	}
	if multiThresholdMigrated || multiThresholdMigErr != nil {
		level := event.LevelInfo
		text := "上下文维护已简化为单一自动压缩阈值。"
		detail := "Context maintenance now uses a single automatic compact_ratio (default 0.85). soft_compact_ratio, tool_result_snip_ratio, compact_force_ratio, cold_resume_prune, and context_editing were removed from config."
		if multiThresholdMigErr != nil {
			level = event.LevelWarn
			text = "Deprecated multi-threshold compaction keys were ignored."
			detail += " The old keys could not be removed: " + multiThresholdMigErr.Error()
		}
		report(sink, event.Event{Level: level, Text: text, Detail: detail})
	}
	timer.mark("config")
	migrateLegacySources(opts, sink)
	timer.mark("migrations")
	if ignored := cfg.IgnoredProjectDefaultModel(); ignored != "" {
		report(sink, event.Event{Level: event.LevelWarn, Text: "Ignored the project config's default_model.", Detail: fmt.Sprintf("./reasonix.toml sets default_model = %q but no configured provider serves it; using %q from your user config instead. Edit or remove that default_model line to silence this notice.", ignored, cfg.DefaultModel)})
	}

	// A resolvable model whose API key env is unset would otherwise build fine
	// (RequireKey is false so the UI stays reachable) and then fail silently on the
	// first request, showing as an empty/dead model. Surface the cause up front.
	if !opts.RequireKey && entry.RequiresAPIKey() && entry.APIKey() == "" {
		report(sink, event.Event{Text: "Selected model is missing its API key.", Detail: fmt.Sprintf("model %q is selected but its API key %s is not set — requests will fail until you set it", modelName, entry.APIKeyEnv)})
	}
	session, err := startSessionRuntime(opts, cfg, root, sink)
	if err != nil {
		return nil, err
	}
	timer.mark("sessions")

	// proxySpec was computed during extension preflight (the merged resolver's
	// local base needs it); validate it before any provider construction.
	if err := netclient.Validate(proxySpec); err != nil {
		return nil, err
	}
	balanceClient, err := netclient.NewHTTPClient(proxySpec, netclient.TransportOptions{})
	if err != nil {
		return nil, err
	}
	execProv, err := resolveProvider(effectiveResolver, cfg, proxySpec, provider.Selection{Ref: modelRef, Effort: opts.EffortOverride})
	if err != nil {
		return nil, err
	}
	timer.mark("provider")
	shell := sandbox.ResolveShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path, stderr)

	prompt, err := buildPromptAssembly(ctx, opts, cfg, root, shell, sink, timer)
	if err != nil {
		return nil, err
	}

	reg := tool.NewRegistry()
	writeRoots := cfg.WriteRootsForRoot(root)
	writeRoots = appendUniquePaths(writeRoots, additionalDirs...)
	if opts.WorkspaceOnly {
		writeRoots = []string{root}
	}
	networkEnabled := cfg.Sandbox.Network
	if opts.SandboxNetworkOverride != nil {
		networkEnabled = *opts.SandboxNetworkOverride
	}
	bashMode := cfg.BashMode()
	if override := strings.TrimSpace(opts.SandboxBashOverride); override != "" {
		bashMode = override
	}
	forbidReadRoots := RuntimeForbidReadRoots(cfg, root)
	// managedConfig names the Reasonix-owned config FILES (config.toml,
	// compatibility TOMLs, legacy v0.x config.json) the file-writers may repair
	// outside the workspace after a fresh per-write human approval. The bash
	// OS-sandbox write roots deliberately stay unwidened: config repair goes
	// through the approval-gated file tools, not raw shell writes.
	managedConfig := builtin.NewManagedConfigPaths(config.ReasonixManagedConfigPaths())
	bashSpec := sandbox.Spec{Mode: bashMode, WriteRoots: writeRoots, ForbidReadRoots: forbidReadRoots, Network: networkEnabled,
		HostAuthorities: sandbox.ParseAuthorities(cfg.Sandbox.HostAuthorities)}
	bashSpec.Shell = shell
	// The session-data guard blocks agent writes into Reasonix's own session
	// stores (they race the app's saves and surface as conflict-copy loops);
	// explicit allow_write entries stay a sanctioned escape hatch.
	allowWriteRoots := cfg.AllowWriteRoots()
	if opts.WorkspaceOnly {
		allowWriteRoots = nil
	}
	sessionGuard := builtin.NewSessionDataGuard(roots.MemoryUserDir(), allowWriteRoots)
	if bashSpec.Mode == "enforce" && !sandbox.Available() {
		fmt.Fprintln(stderr, "warning: "+sandbox.UnavailableMessage())
	}
	if autoShellPrefer(cfg.Tools.Shell.Prefer) && shell.Kind == sandbox.ShellPowerShell {
		fmt.Fprintln(stderr, "warning: bash not found on PATH; the shell tool will run commands under Windows PowerShell. Install Git for Windows or WSL to use bash, or set [tools.shell] prefer=\"powershell\" to silence this.")
	}
	searchSpec := builtin.ResolveSearch(cfg.Tools.Search.Engine, cfg.Tools.Search.RgPath, stderr)
	bashTimeout := time.Duration(cfg.BashTimeoutSeconds()) * time.Second
	enabledBuiltins := cfg.Tools.Enabled
	readPathResolver := builtin.NewPathResolver()
	// Session-private temporary directory manager for Bash/grep. Rebuild
	// reuses the previous Controller's Manager; a fresh build creates one
	// here so tools and the Controller share the same instance from boot.
	sessionTemp := opts.SessionTemp
	if sessionTemp == nil {
		sessionTemp = sessiontemp.New()
	}
	// Register the full built-in inventory for use_capability dispatch. The
	// provider-visible surface is narrowed later via SetProviderVisibleTools.
	addBuiltins(reg, enabledBuiltins, writeRoots, bashSpec, bashTimeout, searchSpec, stderr, root, proxySpec, forbidReadRoots, readPathResolver, sessionGuard, managedConfig, opts.FileOverlay, opts.TerminalRunner, sessionTemp, fileWriteReceipt)
	// Use the caller-supplied shared host when set, so controllers for the same
	// workspace root reuse running MCP processes (e.g. one CodeGraph daemon
	// instead of one per tab). Otherwise construct a private host per controller.
	pluginHost := opts.SharedHost
	if pluginHost == nil {
		pluginHost = plugin.NewHost()
	}
	// Where the host reports that a server's state changed. Without it a lazy
	// server that connects in the background — the common case, since a
	// cache-miss server is started by its first real tool call — leaves every
	// status view showing whatever it saw at boot.
	pluginHost.SetStatusSink(opts.Sink)

	// Enabled MCP servers enter the tool catalog at boot. Cached schemas
	// register placeholders without starting processes; cache-miss servers get
	// a single background catalog discovery. First real tool call uses
	// EnsureConnected so parent/child/tab runtimes share one process.
	pluginSpecOptions := pluginspec.Options{
		DefaultStartupTimeout: time.Duration(cfg.MCPStartupTimeoutSeconds()) * time.Second,
		DefaultCallTimeout:    time.Duration(cfg.MCPCallTimeoutSeconds()) * time.Second,
		LaunchManager:         mcplaunch.ForWorkspace(roots.Home(), root),
		ConfigSource:          "workspace_config",
		StateHome:             roots.Home(),
		WriterRoots:           writeRoots,
		ForbidReadRoots:       forbidReadRoots,
		Network:               networkEnabled,
		PackageOwners:         pluginspec.PackageOwners(cfg),
		OAuthHTTPClient:       balanceClient,
	}
	mcp := resolveMCPSpecs(opts, cfg, root, pluginSpecOptions)

	// Host-session ExtraPlugins (for example ACP session servers) are explicit
	// for this controller and still take a short readiness probe so recovery and
	// session-scoped servers are deterministic. User/project config MCP stays
	// catalog-first and process-idle until first real tool call.
	if len(mcp.extra) > 0 {
		for _, s := range mcp.extra {
			if pluginHost.HasClient(s.Name) {
				if tools, err := pluginHost.ToolsFor(ctx, s.Name); err == nil {
					for _, t := range tools {
						reg.Add(t)
					}
					continue
				}
			}
			addCtx, addCancel := context.WithTimeout(ctx, 5*time.Second)
			tools, err := pluginHost.EnsureConnectedWithLifecycle(ctx, addCtx, s, 0)
			addCancel()
			if err != nil {
				if plugin.IsServerAlreadyConnected(err) {
					if tools, err2 := pluginHost.ToolsFor(ctx, s.Name); err2 == nil {
						for _, t := range tools {
							reg.Add(t)
						}
						continue
					}
				}
				// Leave a catalog entry for diagnostics; failures surface in /mcp.
				cs, _ := plugin.LoadCachedSchemaForSpec(s)
				for _, t := range plugin.LazyToolset(s, cs, pluginHost, reg, ctx, false) {
					reg.Add(t)
				}
				report(sink, event.Event{Level: event.LevelWarn,
					Text: "An MCP server failed to start.", Detail: fmt.Sprintf("mcp %s: %v", s.Name, err)})
				continue
			}
			for _, t := range tools {
				reg.Add(t)
			}
		}
	}

	// Configured enabled MCP: cache-hit placeholders without starting processes;
	// cache-miss servers get one background catalog discovery.
	registerEnabledMCP := func(specs []plugin.Spec) {
		for _, s := range specs {
			if pluginHost.HasClient(s.Name) {
				tools, err := pluginHost.ToolsFor(ctx, s.Name)
				if err == nil {
					for _, t := range tools {
						reg.Add(t)
					}
					continue
				}
			}
			cs, _ := plugin.LoadCachedSchemaForSpec(s)
			// Only kick a process for catalog discovery when no usable schema is
			// cached. Cache-hit sessions stay process-idle until first tool call.
			kick := cs == nil || len(cs.Tools) == 0
			for _, t := range plugin.LazyToolset(s, cs, pluginHost, reg, ctx, kick) {
				reg.Add(t)
			}
		}
	}
	// mcp.eager already includes mcp.extra; avoid double
	// registration of host-session servers that connected above.
	configSpecs := append(append([]plugin.Spec{}, mcp.eager...), mcp.background...)
	if len(mcp.extra) > 0 {
		extraNames := map[string]bool{}
		for _, s := range mcp.extra {
			extraNames[s.Name] = true
		}
		filtered := configSpecs[:0]
		for _, s := range configSpecs {
			if extraNames[s.Name] {
				continue
			}
			filtered = append(filtered, s)
		}
		configSpecs = filtered
	}
	registerEnabledMCP(configSpecs)

	for _, msg := range mcp.demotions {
		report(sink, event.Event{Level: event.LevelInfo, Text: msg})
	}

	cleanup := pluginHost.Close
	if opts.SharedHost != nil {
		// The caller owns the shared host's lifecycle; the controller must not
		// close it. A no-op cleanup keeps Controller.Close happy without
		// shutting down MCP processes that other controllers still use.
		cleanup = func() {}
	}

	// LSP tools resolve their servers on PATH and spawn lazily on first query, so
	// registering them is cheap even when no server is installed (a query then
	// returns an install hint). The manager is session-scoped; chain its shutdown
	// into the controller's cleanup so servers stop with the session, not the turn.
	var lspMgr *lsp.Manager
	if cfg.LSP.Enabled {
		lspMgr = lsp.NewManager(root, LSPSpecs(cfg.LSP))
		for _, t := range lsp.Tools(lspMgr) {
			if t != nil {
				reg.Add(t)
			}
		}
		prev := cleanup
		cleanup = func() { prev(); lspMgr.Close() }
	}

	timer.mark("mcp")
	maxSteps := max(opts.MaxSteps, 0)
	subagentStore, err := newSubagentStore(session.dir, opts.SubagentParentLive)
	if err != nil {
		return nil, err
	}
	if subagentStore != nil {
		subagentStore.WithDestroyedChecker(session.jobs.IsDestroying)
	}

	// Permission policy gates every tool call. With no HeadlessApprovalMode
	// (interactive bootstrap), the temporary gate preserves the legacy behavior
	// until chat/desktop installs an interactive gate. A real headless caller
	// such as `reasonix run` always supplies a mode: Ask fails closed, Auto
	// allows ordinary writer fallbacks, and DontAsk denies them (#6927).
	// The selected contract is also applied to sub-agents, so they cannot be a
	// weaker path around the parent gate.
	// Sub-agents always run headless: they have no UI to answer a prompt, so they
	// inherit this same gate.
	policy := permission.New(cfg.Permissions.Mode, cfg.Permissions.Allow, cfg.Permissions.Ask, cfg.Permissions.Deny).
		WithAllowDynamicBashFallback(cfg.Permissions.AllowDynamicBash).
		WithSessionAllow(opts.PermissionAllow)
	headlessGate := control.NewSharedHeadlessGate(policy, opts.HeadlessApprovalMode)

	var resolvedHooks []hook.ResolvedHook
	if opts.ReuseAssembly != nil && shouldReuseDiscovery(opts.PreviousPlan) {
		resolvedHooks = opts.ReuseAssembly.Hooks
	} else {
		resolvedHooks = hook.Load(hook.LoadOptions{ProjectRoot: root, ReasonixHomeDir: roots.Home()})
	}
	hookRuntime := hook.RuntimeOptions{}
	if shell.Kind == sandbox.ShellBash {
		hookRuntime.BashPath = shell.Path
	}
	hookRunner := hook.NewRunner(
		resolvedHooks, root, hook.NewDefaultSpawner(hookRuntime),
		func(n hook.Notice) { sink.Emit(hookNoticeEvent(n)) },
	)
	// The `task` tool spawns sub-agents that reuse the parent's provider and
	// tool registry. Wired here after the built-ins / plugins are loaded so
	// sub-agents inherit the full tool set (minus `task` itself, to keep
	// nesting out of the picture). It registers into the same reg the
	// executor uses, so the model surfaces it like any other tool.
	sub := newSubagentConfig(opts, cfg, entry, modelName, effectiveResolver, proxySpec, prompt.skillStore)
	bashSandboxEnforced := bashSpec.Enforce
	var taskTool *agent.TaskTool
	// capRuntime is assigned after the MCP specs load, so the task tool gets it
	// through WithCapabilityRuntime once it exists.
	var capRuntime *agent.MCPCapabilityRuntime
	newTaskTool := func() *agent.TaskTool {
		return agent.NewTaskToolWithOptions(agent.TaskToolOptions{
			Provider:          execProv,
			Pricing:           entry.Price,
			ParentRegistry:    reg,
			MaxSteps:          maxSteps,
			ContextWindow:     entry.ContextWindow,
			RecentKeep:        cfg.Agent.RecentKeep,
			CompactionBudgets: compactionBudgets(cfg),
			CompactRatio:      cfg.Agent.CompactRatio,
			ContextEditing:    cfg.Agent.ContextEditing,
			Temperature:       cfg.Agent.Temperature,
			ArchiveDir:        roots.ArchiveDir(),
			SysPrompt:         "",
			Gate:              headlessGate,
			KeepPolicy:        keepPolicy,
			SubagentModel:     sub.taskModel,
			SubagentEffort:    sub.taskEffort,
			ResolveProvider:   sub.resolveProvider,
		}).
			WithTranscripts(subagentStore, root, modelName, entry.Effort).
			WithTranscriptIdentityResolver(sub.identity).
			WithMaxSubagentDepth(sub.maxDepth).
			WithDeliveryProfile(tokenDelivery).
			WithAblation(opts.Ablation).
			WithWorkspaceLease(session.lease).
			WithScheduler(sub.scheduler).
			WithProfileLookup(sub.profileLookup).
			WithProfileConfigResolvers(sub.profileModel, sub.profileEffort).
			WithBashSandboxEnforced(bashSandboxEnforced).
			WithCapabilityRuntime(capRuntime)
	}
	if !opts.Ablation.Off(ablation.Subagent) {
		taskTool = newTaskTool()
		addDelegationTools(reg, taskTool)
	}

	// Product documentation, session, and memory tools are always present on the
	// unified host registry for every role setting. Provider-visible surface stays
	// lean via use_capability; these tools are dispatchable without schema growth.
	reg.Add(productdocs.NewTool())
	// history and memory are the BM25-backed surfaces; the ablation arm drops
	// only those two and leaves the direct-access tools alone, so a lost solve
	// is attributable to retrieval and not to a missing session reader.
	retrievalOff := opts.Ablation.Off(ablation.Retrieval)
	if !retrievalOff {
		reg.Add(history.NewIndexedTool(history.Options{SessionDir: session.dir, GlobalSessionDir: roots.SessionDir(), ArchiveDir: roots.ArchiveDir()}))
	}
	reg.Add(sessiontool.NewListSessionsTool(session.dir))
	reg.Add(sessiontool.NewReadSessionTool(session.dir))
	if !retrievalOff {
		reg.Add(memory.NewRecallTool(prompt.memory.Store))
	}
	reg.Add(memory.NewRememberTool(prompt.memory.Store))
	reg.Add(memory.NewForgetTool(prompt.memory.Store))
	addTurnExitTools(reg)

	// Skill tools: read_only_skill is a narrow explicitly read-only entry point; the
	// full skills source adds run_skill / install_skill plus the dedicated
	// subagent wrappers (explore / research / review / security_review). Read-only
	// subagent skills run ephemerally with the same registry boundary as
	// read_only_task, so they cannot write, install, mutate memory, resume/fork
	// transcripts, or delegate further.
	//
	// subagentSkillOptions is the single construction point for skill sub-agent
	// run options, so the read-only and writer-capable runners cannot drift on
	// compaction or language settings — add new fields here, not per runner.
	subagentSkillOptions := func(sctx context.Context, steps int, price *provider.Pricing, ctxWin, childDepth int) agent.Options {
		return agent.Options{
			MaxSteps:          steps,
			Temperature:       cfg.Agent.Temperature,
			Pricing:           price,
			UsageSource:       event.UsageSourceSubagent,
			Gate:              headlessGate,
			ContextWindow:     ctxWin,
			RecentKeep:        cfg.Agent.RecentKeep,
			CompactionBudgets: compactionBudgets(cfg),
			CompactRatio:      cfg.Agent.CompactRatio,
			ContextEditing:    cfg.Agent.ContextEditing,
			ArchiveDir:        roots.ArchiveDir(),
			KeepPolicy:        keepPolicy,
			ResponseLanguage:  agent.ResponseLanguageFromContext(sctx),
			ReasoningLanguage: agent.ReasoningLanguageFromContext(sctx),
			SubagentDepth:     childDepth,
			MaxSubagentDepth:  sub.maxDepth,
			DeliveryProfile:   tokenDelivery,
			Ablation:          opts.Ablation,
			WorkspaceLease:    session.lease,
		}
	}
	skillRun := &skillSubagents{
		root:            root,
		cfg:             cfg,
		registry:        reg,
		tasks:           taskTool,
		scheduler:       sub.scheduler,
		provider:        execProv,
		entry:           entry,
		maxDepth:        sub.maxDepth,
		maxSteps:        maxSteps,
		resolveProvider: sub.resolveProvider,
		identity:        sub.identity,
		runOptions:      subagentSkillOptions,
	}
	readOnlySkillRunner := skillRun.runReadOnly
	skillRunner := skillRun.run
	skillProfile := func(sk skill.Skill) *event.Profile {
		model, effort := subagentModelRef(cfg, sk), subagentEffortRef(cfg, sk)
		if model == "" && effort == "" {
			return nil
		}
		return &event.Profile{Model: model, Effort: effort}
	}
	var cmds []command.Command
	if opts.ReuseAssembly != nil && shouldReuseDiscovery(opts.PreviousPlan) {
		cmds = opts.ReuseAssembly.Commands
	} else {
		cmds, _ = command.LoadRoots(config.CommandRootsForRoot(root)...)
	}
	addInstallSourceTool(ctx, reg, pluginHost, root, balanceClient, pluginSpecOptions, opts.Stderr)
	// Skill tools and their slash entries ride the same switch: automatic
	// invocation is what decides whether the model sees skills at all. Slash
	// commands register last so the tool carries every entry collected above.
	var slashEntries []command.SlashEntry
	if prompt.implicitSkills {
		reg.Add(skill.NewReadOnlySkillTool(prompt.skillStore, gateSubagentArm(opts.Ablation, readOnlySkillRunner), skillProfile))
		reg.Add(skill.NewRunSkillTool(prompt.skillStore, gateSubagentArm(opts.Ablation, skillRunner), skillProfile))
		reg.Add(skill.NewReadSkillTool(prompt.skillStore))
		reg.Add(skill.NewInstallSkillTool(prompt.skillStore, nil))
		for _, t := range builtinSubagentTools(opts.Ablation, prompt.skillStore, skillRunner, skillProfile) {
			reg.Add(t)
		}
		for _, sk := range prompt.skillStore.SlashList() {
			slashEntries = append(slashEntries, command.SlashEntry{
				Name:        sk.SlashName(),
				Description: sk.Description,
				Render:      func(args []string) string { return prompt.skillStore.Render(sk, strings.Join(args, " ")) },
			})
		}
	}
	for _, cmd := range cmds {
		if cmd.Hidden {
			continue
		}
		slashEntries = append(slashEntries, command.SlashEntry{
			Name:        cmd.Name,
			Description: cmd.Description,
			ArgHint:     cmd.ArgHint,
			Render:      func(args []string) string { return cmd.Render(args) },
		})
	}
	reg.Add(command.NewSlashCommandTool(slashEntries))

	// Session-shared MCP runtime: Host, specs, and connection snapshots. Each
	// agent gets its own use_capability frontend (ledger/audit isolation) while
	// reusing processes. Delivery puts a frontend on the executor registry;
	// dual-model Planner and all task/fleet sub-agents get their own frontends
	// without inheriting dynamic mcp__* schemas.
	var capLedger *capability.Ledger
	var capAudit *capability.Audit
	capSpecs := pluginspec.ForRootWithOptions(cfg.Plugins, root, pluginSpecOptions)
	cachedTools, cacheKeyOK := capability.LoadCachedToolsForSpecs(capSpecs)
	prompt.skillStore.ConfigureToolBindings(func(sk skill.Skill) []tool.MCPBinding {
		return skillMCPBindings(sk, reg, capSpecs, cachedTools, cacheKeyOK)
	})
	// Detect dual-model planner early so Balanced/Delivery can attach the same
	// stable use_capability surface to both Planner and Executor.
	var capProxy *agent.UseCapabilityTool
	// Catalog closes over capRuntime so proxy-connected tools stay routable.
	// Use AllContractEntries so tool: capabilities include non-provider-visible
	// tools that use_capability can still dispatch.
	catalogFn := func() capability.Catalog {
		conn := map[string]bool{}
		failedNow := map[string]string{}
		if pluginHost != nil {
			for _, n := range pluginHost.ServerNames() {
				conn[n] = true
			}
			for _, failure := range pluginHost.Failures() {
				failedNow[failure.Name] = failure.Error
			}
		}
		catOpts := capability.CatalogOptions{
			Tools:       reg.AllContractEntries(),
			Skills:      prompt.skillStore.List(),
			Plugins:     cfg.Plugins,
			Profile:     runtimeProfile,
			Connected:   conn,
			Failed:      failedNow,
			CachedTools: cachedTools,
			CacheKeyOK:  cacheKeyOK,
		}
		if capRuntime != nil {
			catOpts.Plugins, catOpts.CachedTools, catOpts.CacheKeyOK, catOpts.Disabled, catOpts.ProxyTools = capRuntime.CapabilityCatalogState()
		}
		return capability.BuildCatalog(catOpts)
	}
	// One runtime and proxy so both role settings share a tool schema.
	// WithoutCancel: ctx is often one request; MCP children outlive it.
	capRuntime = agent.NewMCPCapabilityRuntime(context.WithoutCancel(ctx), pluginHost, capSpecs, reg, catalogFn)
	skillRun.capRuntime = capRuntime
	capRuntime.ConfigureServers(cfg.Plugins, capSpecs, mcp.enabled)
	capLedger = capability.NewLedger()
	capAudit = &capability.Audit{}
	capProxy = capRuntime.NewFrontend(capLedger, capAudit)
	reg.Add(capProxy)
	prompt.skillStore.ConfigureInvocationPolicy(func(requires []string) []string {
		_, missing := catalogFn().RequiresReady(requires)
		return missing
	})

	execSess := newObservedSession(prompt.prompt)
	triageProv, triageRef, triagePrice := resolveTriage(cfg, modelRef, proxySpec)
	executor := agent.New(execProv, reg, execSess, agent.Options{
		MaxSteps:       maxSteps,
		MaxStepsKey:    opts.MaxStepsKey,
		Temperature:    cfg.Agent.Temperature,
		TaskBudget:     taskBudgetFromConfig(cfg),
		Pricing:        entry.Price,
		ModelRef:       modelRef,
		TriageProvider: triageProv, TriageModelRef: triageRef, TriagePricing: triagePrice,
		Gate:  headlessGate,
		Hooks: hookRunner,
		Jobs:  session.jobs,
		// Parent write reservation at the executor entry covers all writers
		// (including late Economy/MCP adds) without wrapping tool schemas.
		WriteScheduler:     sub.scheduler,
		WriteWorkspaceRoot: root, WorkspaceVCS: prompt.workspaceVCS,
		ProjectChecks: prompt.projectChecks, ProjectSensitivePaths: prompt.sensitivePaths,
		AgentPreset:                  agentPreset,
		DeliveryProfile:              tokenDelivery,
		Ablation:                     opts.Ablation,
		WorkspaceLease:               session.lease,
		CapabilityLedger:             capLedger,
		CapabilityAudit:              capAudit,
		ContextWindow:                entry.ContextWindow,
		CompactRatio:                 cfg.Agent.CompactRatio,
		ContextEditing:               cfg.Agent.ContextEditing,
		RecentKeep:                   cfg.Agent.RecentKeep,
		CompactionBudgets:            compactionBudgets(cfg),
		ArchiveDir:                   roots.ArchiveDir(),
		KeepPolicy:                   keepPolicy,
		ReasoningLanguage:            cfg.ReasoningLanguage(),
		SubagentDepth:                0,
		MaxSubagentDepth:             sub.maxDepth,
		MissingReasoningWarnStateDir: config.MissingReasoningWarnStateDir(),
	}, sink)

	var runner agent.Runner = executor
	label := entry.Model
	// Two-model collaboration: a distinct planner_model wraps the executor in a
	// Coordinator with its own session, kept separate for cache stability. The
	// planner gets the same standing memory context and a filtered read-only
	// research tool set, so it can inspect rules/code without side effects.
	pm := effectivePlannerModel(cfg, opts)
	pe, plannerResolved := resolveOptionalEntry(effectiveResolver, cfg, pm)
	if pm != "" && !plannerResolved {
		// An unusable optional planner must not take the session down with it —
		// the executor is what the user talks to. Degrades like the guardian
		// model below (#4615).
		slog.Warn("planner model is not a configured provider — planning disabled", "model", pm)
		report(sink, event.Event{Level: event.LevelWarn,
			Text: fmt.Sprintf("planner_model %q is not a configured provider — continuing with the executor alone", pm)})
	}
	if pm != "" && plannerResolved {
		if pe.Model != entry.Model {
			plannerProv, err := resolveProvider(effectiveResolver, cfg, proxySpec, provider.Selection{Ref: modelRefFromEntry(pe)})
			if err != nil {
				return nil, fmt.Errorf("planner %q: %w", pm, err)
			}
			plannerSess := agent.NewSession(agent.PlannerPromptWithContext(prompt.memory.StaticContext()))
			// Planner owns an independent ledger/audit and use_capability frontend
			// so its MCP calls cannot satisfy or poison Executor Delivery gates.
			plannerLedger := capability.NewLedger()
			plannerAudit := &capability.Audit{}
			plannerTools := agent.PlannerToolRegistry(reg)
			if capRuntime != nil {
				// Replace any cloned parent frontend with one bound to the
				// planner ledger (PlannerToolRegistry clones with nil ledger).
				if _, ok := plannerTools.Get("use_capability"); ok {
					plannerTools.RemovePrefix("use_capability")
				}
				plannerTools.Add(capRuntime.NewFrontend(plannerLedger, plannerAudit))
			}
			plannerOpts := agent.Options{
				MaxSteps:                     0,
				Gate:                         headlessGate,
				ModelRef:                     modelRefFromEntry(pe),
				ContextWindow:                pe.ContextWindow,
				CompactRatio:                 cfg.Agent.CompactRatio,
				ContextEditing:               cfg.Agent.ContextEditing,
				RecentKeep:                   cfg.Agent.RecentKeep,
				CompactionBudgets:            compactionBudgets(cfg),
				ArchiveDir:                   roots.ArchiveDir(),
				KeepPolicy:                   keepPolicy,
				ReasoningLanguage:            cfg.ReasoningLanguage(),
				CapabilityLedger:             plannerLedger,
				CapabilityAudit:              plannerAudit,
				MissingReasoningWarnStateDir: config.MissingReasoningWarnStateDir(),
			}
			runner = agent.NewCoordinatorWithPlannerPolicy(plannerProv, plannerSess, pe.Price, plannerTools, plannerOpts, executor, cfg.Agent.Temperature, sink, control.NewPlannerPolicy())
			label = entry.Model + " + planner " + pe.Model
		}
	}

	ctrlOpts := control.Options{
		TaskBudget:                     taskBudgetFromConfig(cfg),
		GoalTokenBudget:                cfg.Agent.GoalTokenBudget,
		Runner:                         runner,
		Executor:                       executor,
		Sink:                           sink,
		Policy:                         policy,
		SubagentGate:                   headlessGate,
		Label:                          label,
		ModelRef:                       modelRef,
		SystemPrompt:                   prompt.prompt,
		SessionDir:                     session.dir,
		Host:                           pluginHost,
		Commands:                       cmds,
		Skills:                         prompt.skills,
		AllSkills:                      prompt.allSkills,
		SkillStore:                     prompt.skillStore,
		AllSkillStore:                  prompt.allSkillStore,
		DisableImplicitSkillInvocation: !prompt.implicitSkills,
		SkillRunner:                    skillRunner,
		ReadOnlySkillRunner:            readOnlySkillRunner,
		SkillProfile:                   skillProfile,
		Hooks:                          hookRunner,
		Memory:                         prompt.memory,
		// Indirection: the cleanup variable gains the extension runtime set at
		// the end of build (snapshot assembly runs after control.New), and the
		// controller must observe the final chain at Close time.
		Cleanup:               func() { cleanup() },
		Balance:               opts.BalanceStore.Cache(balanceClient, entry.BalanceURL, entry.APIKey()),
		Jobs:                  session.jobs,
		TaskStore:             opts.TaskStore,
		WorkspaceLease:        session.lease,
		Registry:              reg,
		PluginCtx:             ctx,
		MCPDefaultCallTimeout: pluginSpecOptions.DefaultCallTimeout,
		MCPConfigureSpec: func(spec *plugin.Spec) {
			if spec == nil {
				return
			}
			spec.LaunchManager = pluginSpecOptions.LaunchManager
			if strings.TrimSpace(spec.ConfigSource) == "" {
				spec.ConfigSource = pluginSpecOptions.ConfigSource
			}
			if spec.DefaultStartupTimeout <= 0 {
				spec.DefaultStartupTimeout = pluginSpecOptions.DefaultStartupTimeout
			}
			pluginspec.ApplyIsolation(spec, root, pluginSpecOptions)
		},
		CapabilityRuntime:      capRuntime,
		WorkspaceRoot:          root,
		ExternalFolderToolRefs: readPathResolver,
		ResponseLanguage:       cfg.ResponseLanguage(),
		ReasoningLanguage:      cfg.ReasoningLanguage(),
		DisableColdResumePrune: !cfg.ColdResumePruneEnabled(),
		Shell:                  shell,
		ApprovalTimeout:        opts.ApprovalTimeout,
		RuntimeProfile:         runtimeProfile,
		Ablation:               opts.Ablation,
		OnRemember: func(rule string) control.RememberResult {
			return rememberPermissionRule(roots, root, rule)
		},
		SessionRecoveryMeta: opts.SessionRecoveryMeta,
		OnSessionRecovered:  opts.OnSessionRecovered,
		// The merged catalog (nil without provider-declaring sidecars) lets
		// frontends enumerate plugin/... models through ProviderCatalog.
		ProviderResolver:  extensionResolver,
		RuntimeGeneration: generation,
		RuntimeOwner:      owner,
		// Share the Manager already bound into bash/grep so tools and the
		// Controller observe the same temporary generation across rebuilds.
		SessionTemp: sessionTemp,
	}
	// Guardian: when guardian_model is configured, spawn an LLM safety reviewer
	// that can auto-allow safe Ask decisions and annotate risky ones before
	// escalating to the human approval prompt.
	if guardianModel := cfg.Agent.GuardianModel; guardianModel != "" {
		ge, ok := resolveOptionalEntry(effectiveResolver, cfg, guardianModel)
		if !ok {
			slog.Warn("guardian model is not a configured provider — guardian disabled", "model", guardianModel)
			report(sink, event.Event{Level: event.LevelWarn, Text: "Guardian was disabled because its model was not found.", Detail: fmt.Sprintf("guardian_model %q not found — guardian disabled", guardianModel)})
		} else {
			pProv, err := resolveProvider(effectiveResolver, cfg, proxySpec, provider.Selection{Ref: modelRefFromEntry(ge)})
			if err != nil {
				slog.Warn("guardian provider construction failed — guardian disabled", "model", guardianModel, "err", err)
				report(sink, event.Event{Level: event.LevelWarn, Text: "Guardian was disabled because it could not start.", Detail: fmt.Sprintf("guardian construction failed: %v — guardian disabled", err)})
			} else {
				guardianReg := agent.FilterReadOnlyRegistry(reg, agent.SubagentMetaTools()...)
				ctrlOpts.Guardian = guardian.NewSession(pProv, guardianReg, guardian.PolicyPrompt(), modelRefFromEntry(ge), cfg.Agent.GuardianTemperature, ge.Price, sink)
				report(sink, event.Event{Level: event.LevelInfo, Text: fmt.Sprintf("guardian enabled · model=%s", ge.Model)})
			}
		}
	}
	// Recovery reviewer: prefer recovery_model, then guardian_model, then the
	// active main model with an isolated session/policy.
	{
		recoveryModel := strings.TrimSpace(cfg.Agent.RecoveryModel)
		if recoveryModel == "" {
			recoveryModel = strings.TrimSpace(cfg.Agent.GuardianModel)
		}
		if recoveryModel == "" {
			recoveryModel = modelRef
		}
		if recoveryModel != "" {
			if extensionResolver != nil && providerext.PluginRefOwner(recoveryModel) != "" {
				// A plugin-namespaced recovery reviewer resolves through the
				// merged resolver; the config path cannot see extension refs.
				if re, ok := resolveOptionalEntry(extensionResolver, cfg, recoveryModel); ok {
					if rProv, err := extensionResolver.Resolve(provider.Selection{Ref: modelRefFromEntry(re)}); err == nil {
						ctrlOpts.RecoveryReviewer = recovery.NewSessionWithSink(rProv, re.Price, modelRefFromEntry(re), sink)
					} else {
						slog.Warn("recovery reviewer provider construction failed — rule-only recovery", "model", recoveryModel, "err", err)
					}
				}
			} else if re, ok := cfg.ResolveModel(recoveryModel); ok {
				if rProv, err := NewProviderWithProxy(re, proxySpec); err == nil {
					ctrlOpts.RecoveryReviewer = recovery.NewSessionWithSink(rProv, re.Price, modelRefFromEntry(re), sink)
				} else {
					slog.Warn("recovery reviewer provider construction failed — rule-only recovery", "model", recoveryModel, "err", err)
				}
			}
		}
		// HeadlessApprovalMode is an explicit declaration that this frontend has
		// no decision channel (`reasonix run`). ApprovalTimeout is not a proxy for
		// that capability: bots have a bounded timeout and can still answer cards.
		ctrlOpts.RecoveryHeadless = recoveryHeadlessMode(opts)
	}
	// Goal evaluator: the same zero-config model fallback as the recovery
	// reviewer (recovery_model → guardian_model → main model), isolated session
	// and policy. When unavailable, Goal turns without an update_goal report
	// fail closed and pause instead of defaulting to continue.
	{
		evalModel := strings.TrimSpace(cfg.Agent.RecoveryModel)
		if evalModel == "" {
			evalModel = strings.TrimSpace(cfg.Agent.GuardianModel)
		}
		if evalModel == "" {
			evalModel = modelRef
		}
		if evalModel != "" {
			if re, ok := cfg.ResolveModel(evalModel); ok {
				if eProv, err := NewProviderWithProxy(re, proxySpec); err == nil {
					ctrlOpts.GoalEvaluator = goaleval.NewSessionWithSink(eProv, re.Price, modelRefFromEntry(re), sink)
				} else {
					slog.Warn("goal evaluator provider construction failed — goals without an update_goal report will pause", "model", evalModel, "err", err)
				}
			}
		}
	}
	ctrl := control.New(ctrlOpts)
	// Publish the controller to the extension UI hub's indirection: from here
	// on, host/ui/* publishes ride ctrl.EmitExtensionEvent and blocking prompts
	// ride ctrl.Ask, exactly as if the hub had been built after control.New.
	ctrlRef.Store(ctrl)
	close(controllerReady)
	// Share the recovery checkpoint with task/fleet sub-agents so background
	// writers observe the same failure state as the root agent.
	if taskTool != nil {
		if g := ctrl.Executor(); g != nil {
			taskTool.WithRecoveryGate(g.RecoveryGate())
		}
	}
	if capRuntime != nil {
		ctrl.SetCapabilityProxyTools(capRuntime.ConnectedProxyTools)
	}
	// Task tools created before capRuntime assignment still need the runtime if
	// they were built early; re-bind when present.
	if taskTool != nil && capRuntime != nil {
		taskTool.WithCapabilityRuntime(capRuntime)
	}
	// Build one role-neutral semantic router so an in-place switch never needs a
	// controller rebuild. The frozen TaskPolicy decides whether a turn may call
	// it; construction alone does not add a provider request.
	var router *capability.SemanticRouter
	if modelRef := strings.TrimSpace(cfg.Agent.SubagentModels["capability-router"]); modelRef != "" {
		effortRef := strings.TrimSpace(cfg.Agent.SubagentEfforts["capability-router"])
		if p, price, _, err := sub.resolveProvider(modelRef, effortRef); err == nil && p != nil {
			usageModelRef, _ := sub.identity(modelRef, effortRef)
			router = &capability.SemanticRouter{Provider: p, Sink: sink, Model: usageModelRef, Pricing: price, Audit: capAudit}
		}
	}
	if router == nil {
		router = &capability.SemanticRouter{Provider: execProv, Sink: sink, Model: modelRef, Pricing: entry.Price, Audit: capAudit}
	}
	ctrl.WireCapabilityRouting(cfg.Plugins, capSpecs, router, capAudit)
	ctrl.SetCapabilityProxyRouting(true)

	// Provider-visible tool surface is identical for every role setting before
	// the extension snapshot freezes registry schemas for cache diagnostics.
	applyUnifiedProviderToolSurface(reg, opts.GoalTurnsUnreachable, opts.Ablation)

	// Freeze the extension kernel's snapshot of exactly what this build wired.
	// The snapshot is assembled from the in-hand objects above — discovery
	// never re-runs — and assembly must never fail the boot: a kernel error
	// degrades to a nil snapshot (logged) while the controller behaves exactly
	// as before. The sidecar Manager comes from preflight (started once,
	// before model resolution); assembly takes over its ownership and freezes
	// the same generation the sidecars were handshaken with. The frozen
	// provider catalog is the BASE catalog, exactly as before the preflight
	// refactor: sidecar providers enter the snapshot through the Manager's own
	// contributions, not through the legacy provider list.
	mcpSpecs := enabledMCPSpecs(configSpecs, mcp.extra)
	snap, runtimeSet, extensionDispatcher, snapErr := assembleLegacySnapshot(ctx, legacyAssembly{
		systemPrompt: prompt.prompt,
		registry:     reg,
		skills:       prompt.skills,
		commands:     cmds,
		hooks:        resolvedHooks,
		mcpSpecs:     mcpSpecs,
		providers:    baseResolver.Catalog(),
	}, generation, extensionBoot{
		session:            protocol.SessionContext{SessionID: sessionID, WorkspaceRoot: root, Generation: generation},
		ui:                 extUIHub,
		onWarning:          extWarn,
		skipPromptStrategy: shouldSkipPromptStrategy(opts.PreviousPlan),
		previousDispatcher: opts.PreviousDispatcher,
	}, extensionMgr)
	// Ownership of the preflighted Manager transferred to assembly on every
	// path: it was either closed inside or registered into the RuntimeSet.
	pendingMgr = nil
	if snapErr != nil {
		// These assembly failures are fatal rather than degradable: two
		// runtimes claiming the same replacement slot (the kernel's
		// ReplaceClaims verdict) and a failed system_prompt.build strategy
		// ruling (the slot owner is required-class, so dispatch surfaces its
		// failure as one of these types) mean the extension contract the user
		// installed cannot be honored; booting without it would silently
		// change what the session is. (A required runtime that cannot start
		// fails earlier, in preflight, with the same fatality.)
		var requiredErr *sidecar.RequiredStartError
		var slotErr *extension.SlotConflictError
		var blockErr *dispatch.BlockError
		var failureErr *dispatch.FailureError
		var violationErr *dispatch.ViolationError
		if errors.As(snapErr, &requiredErr) || errors.As(snapErr, &slotErr) ||
			errors.As(snapErr, &blockErr) || errors.As(snapErr, &failureErr) || errors.As(snapErr, &violationErr) {
			ctrl.ReleaseResources()
			return nil, fmt.Errorf("boot: %w", snapErr)
		}
		slog.Warn("boot: extension snapshot assembly failed; continuing without a runtime snapshot", "err", snapErr)
		runtimeSet = extension.NewRuntimeSet(generation)
		// Assembly retired the preflighted Manager on the error path; the
		// controller must not bind a hub or expose a manager whose sidecars
		// are already shut down.
		extensionMgr = nil
	}
	// The stage-7 provider merge happened at preflight, before model
	// resolution; BuildResult.ProviderResolver exposes that same merged
	// resolver (the base when no sidecar declared providers).
	providerResolver := baseResolver
	if extensionResolver != nil {
		providerResolver = extensionResolver
	}
	cleanup = wireRuntimeScopeCleanup(runtimeSet, cleanup, opts.SharedHost, pluginHost, lspMgr, opts.SessionTemp)
	ctrl.SetExtensions(extensionDispatcher)
	if extensionMgr == nil {
		extUIHub = nil
	} else {
		ctrl.SetExtensionUI(extUIHub)
	}
	if providerResolver != nil {
		ctrl.SetProviderResolver(providerResolver)
	}
	// Stage 6b2 system-prompt handoff: the 6b1 strategy pass may have replaced
	// the prompt while the snapshot was freezing, but the executor session was
	// built earlier with the host-composed prompt. Swap in a fresh session
	// carrying the final prompt now — before any turn or history resume, so
	// the live session and the frozen snapshot describe the same session.
	if snap != nil {
		if final := snap.SystemPrompt(); final != prompt.prompt {
			ctrl.ApplyExtensionSystemPrompt(final)
		}
	}
	assembly := &ReusedAssembly{
		SystemPrompt:            prompt.prompt,
		Skills:                  prompt.skills,
		Commands:                cmds,
		Hooks:                   resolvedHooks,
		Registry:                reg,
		ImplicitSkillInvocation: prompt.implicitSkills,
		Memory:                  prompt.memory,
		ProjectChecks:           prompt.projectChecks, ProjectSensitivePaths: prompt.sensitivePaths,
	}
	return finalizeBuildResult(roots, &BuildResult{Controller: ctrl, Snapshot: snap, Runtime: runtimeSet, Owner: owner, Extensions: extensionMgr, Dispatcher: extensionDispatcher, ExtensionUI: extUIHub, ProviderResolver: providerResolver, BaseProviderResolver: baseResolver, Assembly: assembly, Phases: timer.done("assemble")}, !opts.deferPublish), nil
}

// effectivePlannerModel centralizes planner precedence. Every role setting
// builds the configured planner so later in-place switches retain the same
// runtime; the per-turn TaskPolicy decides whether it is invoked.
func effectivePlannerModel(cfg *config.Config, opts Options) string {
	if cfg == nil || opts.Ablation.Off(ablation.Planner) {
		return ""
	}
	return strings.TrimSpace(cfg.Agent.PlannerModel)
}

func rememberPermissionRule(roots config.Roots, workspaceRoot, rule string) control.RememberResult {
	path := rememberPermissionConfigPath(roots, workspaceRoot)
	result := control.RememberResult{Rule: strings.TrimSpace(rule), Path: path}
	unlock, err := config.LockConfigFileEdits(path)
	if err != nil {
		slog.Warn("lock config for permission rule", "path", path, "err", err)
		result.Err = err
		return result
	}
	defer unlock()

	edit, err := config.LoadForEditReadOnlyStrict(path)
	if err != nil {
		slog.Warn("load config for permission rule", "path", path, "err", err)
		result.Err = err
		return result
	}
	if coveredBy := coveredPermissionRule(edit.Permissions.Allow, result.Rule); coveredBy != "" {
		result.CoveredBy = coveredBy
		return result
	}
	edit.Permissions.Allow = pruneCoveredPermissionRules(edit.Permissions.Allow, result.Rule)
	if err := edit.AddPermissionRule("allow", rule); err != nil {
		slog.Warn("persist permission rule", "rule", rule, "err", err)
		result.Err = err
		return result
	}
	if err := config.WritePermissionsAllow(path, edit.Permissions.Allow); err != nil {
		slog.Warn("save config after permission rule", "err", err)
		result.Err = err
		return result
	}
	result.Saved = true
	return result
}

func rememberPermissionConfigPath(roots config.Roots, workspaceRoot string) string {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot != "" {
		return filepath.Join(workspaceRoot, "reasonix.toml")
	}
	path := roots.SourcePathForRoot(".")
	if path == "" {
		path = "reasonix.toml" // match Config.Save() fallback
	}
	return path
}

func coveredPermissionRule(rules []string, rule string) string {
	for _, existing := range rules {
		if permission.RuleCoversString(existing, rule) {
			return strings.TrimSpace(existing)
		}
	}
	return ""
}

func pruneCoveredPermissionRules(rules []string, rule string) []string {
	out := rules[:0]
	for _, existing := range rules {
		if strings.TrimSpace(existing) == "" || permission.RuleCoversString(rule, existing) {
			continue
		}
		out = append(out, existing)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func subagentModelRef(cfg *config.Config, sk skill.Skill) string {
	if cfg != nil {
		for _, key := range SubagentModelKeys(sk.Name) {
			if m := strings.TrimSpace(cfg.Agent.SubagentModels[key]); m != "" {
				return m
			}
		}
	}
	if m := strings.TrimSpace(sk.Model); m != "" {
		return m
	}
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Agent.SubagentModel)
}

func subagentEffortRef(cfg *config.Config, sk skill.Skill) string {
	if cfg != nil {
		for _, key := range SubagentModelKeys(sk.Name) {
			if e := strings.TrimSpace(cfg.Agent.SubagentEfforts[key]); e != "" {
				return e
			}
		}
	}
	if e := strings.TrimSpace(sk.Effort); e != "" {
		return e
	}
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Agent.SubagentEffort)
}

// SubagentModelKeys returns the cfg.Agent.SubagentModels/SubagentEfforts map
// keys that resolve for a subagent name, in precedence order: the exact name
// first, then its underscore/hyphen alias variants (the dedicated tool
// security_review dispatches the skill security-review, so either spelling in
// config must reach it). Any surface that reads OR clears these maps must
// iterate this same key set — an exact-key delete leaves an alias entry
// silently active.
func SubagentModelKeys(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	keys := []string{name}
	for _, alias := range []string{
		strings.ReplaceAll(name, "-", "_"),
		strings.ReplaceAll(name, "_", "-"),
	} {
		if alias == "" {
			continue
		}
		seen := slices.Contains(keys, alias)
		if !seen {
			keys = append(keys, alias)
		}
	}
	return keys
}

func resolveWorkspaceRoot(explicit string) string {
	if explicit != "" {
		return explicit
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if root, ok := nearestGitRoot(wd); ok {
		return root
	}
	return wd
}

func normalizeAdditionalDirs(root string, dirs []string) ([]string, error) {
	if len(dirs) == 0 {
		return nil, nil
	}
	base := strings.TrimSpace(root)
	if base == "" {
		base = "."
	}
	if !filepath.IsAbs(base) {
		abs, err := filepath.Abs(base)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root: %w", err)
		}
		base = abs
	}

	var out []string
	for _, raw := range dirs {
		dir := strings.TrimSpace(raw)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(base, dir)
		}
		dir, err := filepath.Abs(filepath.Clean(dir))
		if err != nil {
			return nil, fmt.Errorf("resolve additional directory %q: %w", raw, err)
		}
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil, fmt.Errorf("resolve additional directory %q: %w", raw, err)
		}
		info, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("inspect additional directory %q: %w", raw, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("additional path %q is not a directory", raw)
		}
		out = appendUniquePaths(out, filepath.Clean(real))
	}
	return out, nil
}

func appendUniquePaths(base []string, extra ...string) []string {
	out := append([]string(nil), base...)
	seen := make(map[string]struct{}, len(out)+len(extra))
	for _, path := range out {
		seen[pathComparisonKey(path)] = struct{}{}
	}
	for _, path := range extra {
		path = filepath.Clean(path)
		key := pathComparisonKey(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

// RuntimeForbidReadRoots returns the configured deny roots plus every path the
// secrets package denies readers: Reasonix's own credential FILE, the host's
// SSH private keys and cloud credential files, and the broad denylist's
// directories when it is on. It also registers the corresponding credential
// environment names for subprocess filtering. Runtime tool assemblers outside
// Build must use this helper instead of reading the config roots directly.
//
// These roots are what reaches the OS sandbox, which is where a protection
// stops being advisory: a denylist only the in-process readers consult leaves
// `cat` reading what read_file refuses.
func RuntimeForbidReadRoots(cfg *config.Config, root string) []string {
	if cfg == nil {
		return nil
	}
	secrets.RegisterCredentialEnvKeys(cfg.CredentialEnvNames())
	base := cfg.ForbidReadRootsForRoot(root)
	base = appendUniquePaths(base, secrets.ForbiddenReadPaths(cfg.Secrets.ProtectSensitiveFiles)...)
	credentialPath := strings.TrimSpace(cfg.Roots().UserCredentialsPath())
	if credentialPath == "" {
		return append([]string(nil), base...)
	}
	info, err := os.Stat(credentialPath)
	if err != nil || info.IsDir() {
		return append([]string(nil), base...)
	}
	if real, err := filepath.EvalSymlinks(credentialPath); err == nil {
		credentialPath = real
	}
	return appendUniquePaths(base, credentialPath)
}

func pathComparisonKey(path string) string {
	path = filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func nearestGitRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		dir = filepath.Clean(start)
	}
	for {
		if isGitMarker(filepath.Join(dir, ".git")) {
			return dir, true
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", false
		}
		dir = next
	}
}

func isGitMarker(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && (fi.IsDir() || fi.Mode().IsRegular())
}

func newSubagentStore(sessionDir string, parentLive func(sessionPath string) bool) (*agent.SubagentStore, error) {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return nil, nil
	}
	store := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents")).WithParentSessionProbe(parentLive)
	if _, err := store.CleanupStaleRunning(); err != nil {
		return nil, fmt.Errorf("cleanup stale subagents: %w", err)
	}
	return store, nil
}

func subagentEffectiveIdentity(cfg *config.Config, resolver provider.Resolver, baseModelRef string, base *config.ProviderEntry, modelRef, effort string) (string, string) {
	var entry config.ProviderEntry
	if base != nil {
		entry = *base
	}
	ref := strings.TrimSpace(modelRef)
	explicit := ref != ""
	if !explicit {
		ref = strings.TrimSpace(baseModelRef)
	}
	if explicit && cfg != nil && ref != "" {
		if resolved, ok := cfg.ResolveModel(ref); ok {
			entry = *resolved
		} else if resolved := syntheticEntryFromResolver(resolver, ref); strings.TrimSpace(resolved.Name) != "" {
			entry = *resolved
		} else {
			entry.Model = ref
		}
	} else if explicit {
		if resolved := syntheticEntryFromResolver(resolver, ref); strings.TrimSpace(resolved.Name) != "" {
			entry = *resolved
		} else {
			entry.Model = ref
		}
	} else if base == nil && ref != "" {
		if resolved := syntheticEntryFromResolver(resolver, ref); strings.TrimSpace(resolved.Name) != "" {
			entry = *resolved
		} else if cfg != nil {
			if resolved, ok := cfg.ResolveModel(ref); ok {
				entry = *resolved
			}
		}
	}
	if rawEffort := strings.TrimSpace(effort); rawEffort != "" {
		if normalized, err := config.NormalizeEffort(&entry, rawEffort); err == nil {
			entry.Effort = normalized
		} else {
			entry.Effort = rawEffort
		}
	}
	modelID := strings.TrimSpace(entry.Name)
	model := strings.TrimSpace(entry.Model)
	if modelID != "" && model != "" {
		modelID += "/" + model
	} else if model != "" {
		modelID = model
	} else if modelID == "" {
		modelID = ref
	}
	return modelID, strings.TrimSpace(config.EffectiveEffort(&entry))
}

// NewProvider builds a provider.Provider from a configured entry. Exported so
// custom assemblers (e.g. the ACP per-session factory) can reuse it without
// going through the full Build.
func NewProvider(e *config.ProviderEntry) (provider.Provider, error) {
	return NewProviderWithProxy(e, netclient.ProxySpec{Mode: netclient.ModeAuto})
}

// NewProviderWithProxy builds a provider.Provider with the configured ordinary
// network proxy settings.
func NewProviderWithProxy(e *config.ProviderEntry, proxy netclient.ProxySpec) (provider.Provider, error) {
	return provider.New(e.Kind, provider.Config{
		Name:    e.Name,
		BaseURL: e.BaseURL,
		Model:   e.Model,
		APIKey:  e.APIKey(), APIKeyFunc: e.APIKey, // live: a replaced key reaches the next request
		// Pass the key's env var so auth failures can name where to fix it, plus
		// provider-kind-specific knobs. EffectiveEffort applies a configured
		// default_effort when the user has not explicitly selected /effort.
		Extra: map[string]any{
			"api_key_env":        e.APIKeyEnv,
			"api_key_source":     e.APIKeySourceLabel(),
			"thinking":           e.Thinking,
			"effort":             config.EffectiveEffort(e),
			"supported_efforts":  e.SupportedEfforts,
			"reasoning_protocol": config.ReasoningProtocolForEntry(e),
			"max_output_tokens":  e.MaxOutputTokens,
			"chat_url":           e.ChatURL,
			"request_url":        e.RequestURL,
			"headers":            e.Headers,
			"extra_body":         e.ExtraBody,
			"auth_header":        e.AuthHeader,
			"proxy_spec":         proxy,
			"vision":             config.EffectiveVision(e),
			"vision_detail":      e.VisionDetail,
			"web_search":         config.EffectiveWebSearch(e),
			"mode":               e.ResponsesMode,
			// Keep nil as nil so the responses provider can vendor-detect its
			// default instead of accidentally treating every endpoint as stateful.
			"stateful": e.ResponsesStateful,
		},
	})
}

// addBuiltins adds enabled built-in tools to reg. An empty list means all of
// them. writeRoots confines the file-writing built-ins to the workspace: after
// the (unconfined) defaults are added, each enabled writer is replaced by an
// instance bound to writeRoots (preserving registry order).
// forbidReadRoots confines the read/list/search built-ins so they cannot peek at
// the listed directories.
// When workDir is non-empty, tools resolve relative paths against it instead of
// the process cwd, enabling concurrent multi-project sessions.
// sessionGuard blocks writer-tool targets inside Reasonix's own session stores
// and makes bash warn when a command references them. managedConfig names the
// Reasonix-owned config files writable outside writeRoots after a fresh
// per-write human approval.
func addBuiltins(reg *tool.Registry, enabled, writeRoots []string, bashSpec sandbox.Spec, bashTimeout time.Duration, searchSpec builtin.SearchSpec, stderr io.Writer, workDir string, proxySpec netclient.ProxySpec, forbidReadRoots []string, readPathResolver *builtin.PathResolver, sessionGuard builtin.SessionDataGuard, managedConfig builtin.ManagedConfigPaths, overlay builtin.FileOverlay, terminal builtin.TerminalRunner, sessionTemp *sessiontemp.Manager, fileWriteReceipt func(path string, hadPrior bool, prior []byte)) {
	// If a workspace directory is set, use workspace-bound tools that resolve
	// paths relative to that directory. Otherwise fall back to the process-cwd
	// compile-time builtins.
	if workDir != "" {
		ws := builtin.Workspace{Dir: workDir, WriteRoots: writeRoots, ForbidReadRoots: forbidReadRoots, Bash: bashSpec, BashTimeout: bashTimeout, Search: searchSpec, ProxySpec: proxySpec, ReadPaths: readPathResolver, SessionGuard: sessionGuard, ManagedConfig: managedConfig, FileOverlay: overlay, Terminal: terminal, SessionTemp: sessionTemp, FileWriteReceipt: fileWriteReceipt}
		for _, t := range ws.Tools(enabled...) {
			reg.Add(t)
		}
		return
	}

	if len(enabled) == 0 {
		for _, t := range tool.Builtins() {
			reg.Add(t)
		}
	} else {
		for _, name := range enabled {
			if t, ok := tool.LookupBuiltin(name); ok {
				reg.Add(t)
			} else {
				fmt.Fprintf(stderr, "warning: unknown built-in tool %q\n", name)
			}
		}
	}
	// Replace the unconfined defaults with confined instances (registry order is
	// preserved on replace): file-writers bound to the workspace, read tools
	// bound to forbid-read roots, bash to the OS sandbox, web_fetch to the proxy.
	// Only replace tools actually enabled/present.
	bashTool := builtin.ConfineBash(bashSpec, sessionGuard, bashTimeout)
	if rebound, ok := builtin.BindSessionTemp(bashTool, sessionTemp); ok {
		bashTool = rebound
	}
	searchTool := builtin.ConfineSearch(searchSpec, bashSpec, forbidReadRoots)
	if rebound, ok := builtin.BindSessionTemp(searchTool, sessionTemp); ok {
		searchTool = rebound
	}
	writers := builtin.ConfineWriters(writeRoots, sessionGuard, managedConfig)
	for i, writer := range writers {
		if rebound, ok := builtin.BindSessionTemp(writer, sessionTemp); ok {
			writer = rebound
		}
		writers[i] = builtin.BindFileWriteReceipt(writer, fileWriteReceipt)
	}
	confined := append(writers,
		bashTool,
		searchTool,
		builtin.ConfineWebFetch(proxySpec))
	confined = append(confined, builtin.ConfineReaders(forbidReadRoots)...)
	for _, t := range confined {
		if _, ok := reg.Get(t.Name()); ok {
			reg.Add(t)
		}
	}
}

// autoShellPrefer reports whether [tools.shell] left the interpreter to
// auto-detection, so the "fell back to PowerShell" hint is suppressed once the
// user has explicitly chosen a shell.
func autoShellPrefer(prefer string) bool {
	p := strings.ToLower(strings.TrimSpace(prefer))
	return p == "" || p == "auto"
}

// LSPSpecs returns the language → server map: the built-in defaults overlaid with
// any user overrides. A user entry may set only the fields it wants to change;
// empty fields keep the default for that language.
func LSPSpecs(cfg config.LSPConfig) map[string]lsp.ServerSpec {
	specs := lsp.DefaultSpecs()
	for lang, s := range cfg.Servers {
		spec := specs[lang]
		if s.Command != "" {
			spec.Command = s.Command
		}
		if s.Args != nil {
			spec.Args = s.Args
		}
		if s.Env != nil {
			spec.Env = s.Env
		}
		if s.LanguageID != "" {
			spec.LanguageID = s.LanguageID
		}
		if s.Extensions != nil {
			spec.Extensions = s.Extensions
		}
		if s.InstallHint != "" {
			spec.InstallHint = s.InstallHint
		}
		if spec.LanguageID == "" {
			spec.LanguageID = lang
		}
		specs[lang] = spec
	}
	return specs
}

func providerNames(cfg *config.Config) string {
	names := make([]string, len(cfg.Providers))
	for i, p := range cfg.Providers {
		names[i] = p.Name
	}
	return strings.Join(names, "/")
}

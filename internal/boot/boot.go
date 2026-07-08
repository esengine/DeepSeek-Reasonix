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
	"strconv"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/environment"
	"reasonix/internal/event"
	"reasonix/internal/guardian"
	"reasonix/internal/history"
	"reasonix/internal/hook"
	"reasonix/internal/installsource"
	"reasonix/internal/instruction"
	"reasonix/internal/jobs"
	"reasonix/internal/lsp"
	"reasonix/internal/memory"
	"reasonix/internal/memorycompiler"
	"reasonix/internal/migration"
	"reasonix/internal/netclient"
	"reasonix/internal/outputstyle"
	"reasonix/internal/permission"
	"reasonix/internal/planmode"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/secrets"
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
		return agent.KeepErrors
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

// Options carries the per-run knobs a frontend chooses; everything else is read
// from configuration. Model "" falls back to the configured default_model;
// MaxSteps 0 uses the config/default. RequireKey forces the executor's API key to
// be present (run/serve pass true so a missing key fails fast; chat/desktop pass
// false so the UI is reachable before a key is set). Sink receives the agent's
// typed event stream.
type Options struct {
	Model      string
	MaxSteps   int
	RequireKey bool
	Sink       event.Sink
	// EffortOverride is a session-local reasoning effort override. Nil means use
	// the resolved provider config; a non-nil empty string means provider default.
	EffortOverride *string
	// Stderr is the writer for diagnostic warnings and plugin subprocess
	// stderr output. When nil, defaults to os.Stderr. Set to io.Discard
	// during model switch inside a bubbletea session to prevent any output
	// from corrupting the TUI's terminal raw mode.
	Stderr io.Writer
	// WorkspaceRoot is the project root directory for config, skills, memory,
	// commands, hooks, and tool confinement. When empty, the current working
	// directory is used (CLI default). Desktop tabs pass their project root here
	// so each tab loads its own config/skills/hooks without changing the process
	// cwd — enabling concurrent multi-project sessions.
	WorkspaceRoot string
	// ExtraPlugins are session-scoped MCP servers supplied by a host transport
	// (for example ACP session/new). They are connected eagerly for this
	// controller but are not persisted to reasonix.toml.
	ExtraPlugins []plugin.Spec
	// TokenMode selects how much optional context/tool surface this session exposes
	// at boot. Empty/full preserves the normal capability surface. "economy" keeps
	// the core coding tools visible and moves skills, MCP, LSP, web_fetch,
	// install_source, and task behind connect_tool_source.
	TokenMode string
	// SessionDir overrides where persisted chat transcripts are written. When
	// empty, the shared CLI/global session directory is used.
	SessionDir string
	// SharedHost is an optional plugin.Host shared across controllers for the
	// same workspace root. When set, boot.Build reuses its running clients
	// instead of creating new subprocesses, and the caller manages the host's
	// lifecycle. When nil, Build creates and owns a new host as before.
	SharedHost *plugin.Host
	// CleanupPendingReconciler retries delayed physical cleanup for session
	// artifacts left by a previous process. Nil uses the core physical-delete
	// reconciler; frontends with different deletion semantics can override it.
	CleanupPendingReconciler func(sessionDir string) error
	// ApprovalTimeout bounds how long a tool-approval or ask prompt blocks for a
	// user decision. Zero (default) waits forever — correct for an interactive
	// terminal. Headless/bot frontends pass a positive value so an unanswered
	// prompt can't wedge the session indefinitely (#4626, #4402).
	ApprovalTimeout time.Duration
	// SessionRecoveryMeta and OnSessionRecovered let richer frontends attach
	// local UI metadata to automatic transcript recovery branches.
	SessionRecoveryMeta func(control.SessionRecoveryRequest) agent.BranchMeta
	OnSessionRecovered  func(control.SessionRecoveryInfo) error
}

// Build loads config, resolves the model(s), and returns a Controller wrapping a
// single Agent, or a two-model Coordinator when agent.planner_model is set. The
// returned controller owns plugin subprocesses; call Close (via Controller.Close)
// to release them.
func Build(ctx context.Context, opts Options) (*control.Controller, error) {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	root := resolveWorkspaceRoot(opts.WorkspaceRoot)
	// One-time import of v1/v0.5 legacy config — runs before Load so the freshly
	// written config + ~/.env are picked up this same boot. CLI Run also calls this
	// before config-only commands; this call stays as the shared frontend fallback.
	migrated, migErr := config.MigrateLegacyIfNeededForRoot(root)
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return nil, err
	}
	// Arm the credential-protection layers from the user-global [secrets]
	// section before any tool, hook, or plugin subprocess can spawn. Package
	// globals are correct here because [secrets] is user-global (project
	// reasonix.toml cannot override it), so concurrent workspaces agree.
	secrets.SetRedactToolOutput(cfg.SecretsRedactToolOutput())
	secrets.SetFilterSubprocessEnv(cfg.Secrets.FilterSubprocessEnv)
	secrets.SetProtectSensitiveFiles(cfg.Secrets.ProtectSensitiveFiles)
	modelName := opts.Model
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	config.NormalizeLegacyMimoCustomProvidersForRefs(cfg, modelName)
	tokenMode := NormalizeTokenMode(opts.TokenMode)
	tokenEconomy := tokenMode == TokenModeEconomy
	keepPolicy := agentKeepPolicy(cfg.Agent.Keep)
	entry, ok := cfg.ResolveModel(modelName)
	if !ok {
		return nil, fmt.Errorf("%w %q (configured: %s); note: defining [[providers]] replaces the built-in presets, so add a [[providers]] entry for it or use a configured name, or run `reasonix setup` to reconfigure", ErrUnknownModel, modelName, providerNames(cfg))
	}
	modelRef := entry.Name + "/" + entry.Model
	if opts.EffortOverride != nil {
		entry.Effort = *opts.EffortOverride
		if entry.Kind == "anthropic" && strings.TrimSpace(entry.Effort) != "" && strings.TrimSpace(entry.Thinking) == "" {
			entry.Thinking = "adaptive"
		}
	}
	if opts.RequireKey {
		if err := cfg.Validate(modelName); err != nil {
			return nil, err
		}
	}

	// Serialize the frontend's sink once: background jobs (below) emit from their
	// own goroutines, which can overlap a running turn's emission, so every emitter
	// shares this synchronized sink. The job manager is session-scoped — its jobs
	// outlive a turn and are cancelled by Controller.Close.
	sink := event.Sync(opts.Sink)

	planModePolicy := planmode.Policy{
		AllowedTools:     cfg.Agent.PlanModeAllowedTools,
		ReadOnlyCommands: cfg.Agent.PlanModeReadOnlyCommands,
	}
	if ignored := planModePolicy.IgnoredAllowedTools(); len(ignored) > 0 {
		detail := fmt.Sprintf("plan_mode_allowed_tools ignored known blocked entries: %s; this setting only declares extra read-only custom tools and cannot unlock known blocked tools or unsafe bash. For shell exploration, declare concrete read-only prefixes in plan_mode_read_only_commands (for example \"gh issue view\"); use read_only_task/read_only_skill instead of task/run_skill while planning.", strings.Join(ignored, ", "))
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelWarn,
			Text:   "Some plan-mode tool settings were ignored.",
			Detail: detail,
		})
	}
	if ignored := planModePolicy.IgnoredReadOnlyCommands(); len(ignored) > 0 {
		detail := fmt.Sprintf("plan_mode_read_only_commands ignored unsafe entries: %s; declare concrete read-only commands such as \"gh issue view\", not shell interpreters, overly broad prefixes, malformed prefixes, or writer-capable command verbs", strings.Join(ignored, ", "))
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelWarn,
			Text:   "Some plan-mode command settings were ignored.",
			Detail: detail,
		})
	}
	if migErr != nil {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Config migration did not complete.", Detail: "config migration from ~/.reasonix failed: " + migErr.Error()})
	} else if migrated != nil {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: migrated.Notice()})
	}
	migration.MigrateLegacyMemorySources(sink)
	migration.MigrateLegacySessionSources(sink)

	// A resolvable model whose API key env is unset would otherwise build fine
	// (RequireKey is false so the UI stays reachable) and then fail silently on the
	// first request, showing as an empty/dead model. Surface the cause up front.
	if !opts.RequireKey && entry.RequiresAPIKey() && entry.APIKey() == "" {
		sink.Emit(event.Event{Kind: event.Notice, Text: "Selected model is missing its API key.", Detail: fmt.Sprintf("model %q is selected but its API key %s is not set — requests will fail until you set it", modelName, entry.APIKeyEnv)})
	}
	jm := jobs.NewManager(sink, jobs.WithStalledWarningAfter(time.Duration(cfg.BackgroundJobStalledWarningSeconds())*time.Second))
	sessionDir := opts.SessionDir
	if sessionDir == "" {
		sessionDir = config.SessionDir()
	}
	reconcileCleanupPending := opts.CleanupPendingReconciler
	if reconcileCleanupPending == nil {
		reconcileCleanupPending = control.ReconcileCleanupPending
	}
	if err := reconcileCleanupPending(sessionDir); err != nil {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "cleanup-pending reconciliation failed: " + err.Error()})
	}

	proxySpec := cfg.NetworkProxySpec()
	if err := netclient.Validate(proxySpec); err != nil {
		return nil, err
	}
	balanceClient, err := netclient.NewHTTPClient(proxySpec, netclient.TransportOptions{})
	if err != nil {
		return nil, err
	}

	execProv, err := NewProviderWithProxy(entry, proxySpec)
	if err != nil {
		return nil, err
	}
	shell := sandbox.ResolveShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path, stderr)

	sysPrompt, err := cfg.ResolveSystemPromptForRoot(root)
	if err != nil {
		return nil, err
	}
	// Output style: fold the selected persona/tone block into the base prompt
	// before language/memory/skills append, so a "replace" style (keep-coding
	// false) still keeps those. Applied once, into the cache-stable prefix.
	if st, ok := outputstyle.Resolve(cfg.Agent.OutputStyle, outputstyle.Dirs()); ok {
		sysPrompt = outputstyle.Apply(sysPrompt, st)
	}
	sysPrompt += "\n\n" + config.UserDecisionPolicy
	sysPrompt += "\n\n" + config.LanguagePolicy
	if workspaceLine := currentWorkspacePromptLine(root); workspaceLine != "" {
		sysPrompt += "\n\n" + workspaceLine
	}
	if tokenEconomy {
		sysPrompt += "\n\n" + tokenEconomyPrompt
	}
	if cfg.EnvironmentEnabled() {
		shellLabel := shell.Kind.String()
		if strings.TrimSpace(cfg.Tools.Shell.Path) != "" {
			shellLabel = shell.Path
		}
		envSection := environment.FormatSection(
			environment.RunProbesWithOptions(ctx, environment.DefaultProbes(), environment.ProbeOptions{
				Overrides: cfg.Environment.Tools,
				DenyRoots: []string{root},
				// Persist probe results across restarts: the section below sits
				// inside the provider-cached prompt prefix, and re-observing
				// per boot let transient probe flaps (timeouts, PATH drift)
				// rewrite the prefix and cold-start every session's cache.
				SnapshotDir: config.CacheDir(),
			}),
			runtime.GOOS+"/"+runtime.GOARCH,
			shellLabel,
			cfg.Environment.Tools,
		)
		if envSection != "" {
			sysPrompt += "\n\n" + envSection
		}
	}

	// Persistent memory (REASONIX.md / AGENTS.md hierarchy + auto-memory index)
	// folds into the system prompt exactly here, once: it becomes part of the
	// durable, cache-stable prefix every turn reuses, so memory costs nothing per
	// turn. Mid-session changes never touch this prefix — they ride the
	// controller's transient turn-injection and fold in on the next session.
	mem := memory.Load(memory.Options{CWD: root, UserDir: config.MemoryUserDir()})
	projectChecks := instruction.ExtractHostChecks(mem.Docs)
	sysPrompt = memory.Compose(sysPrompt, mem)

	// Skills: discover playbooks (built-in + project/custom/global) and fold their
	// one-liner index into the same cache-stable prefix — names + descriptions
	// only; bodies load on demand via run_skill or "/<name>". Bodies never enter
	// the prefix, so the index costs a fixed, small amount per turn.
	skillStore := skill.New(skill.Options{
		ProjectRoot:   root,
		CustomPaths:   cfg.SkillCustomPaths(),
		ExcludedPaths: cfg.SkillExcludedPaths(),
		DisabledNames: cfg.DisabledSkillNames(),
		MaxDepth:      cfg.SkillMaxDepth(),
		Stderr:        opts.Stderr,
	})
	skills := skillStore.List()
	allSkillStore := skill.New(skill.Options{ProjectRoot: root, CustomPaths: cfg.SkillCustomPaths(), ExcludedPaths: cfg.SkillExcludedPaths(), MaxDepth: cfg.SkillMaxDepth(), Stderr: io.Discard})
	allSkills := allSkillStore.List()
	if !tokenEconomy {
		sysPrompt = skill.ApplyIndex(sysPrompt, skills)
	}

	reg := tool.NewRegistry()
	bashSpec := sandbox.Spec{Mode: cfg.BashMode(), WriteRoots: cfg.WriteRootsForRoot(root), ForbidReadRoots: cfg.ForbidReadRootsForRoot(root), Network: cfg.Sandbox.Network}
	bashSpec.Shell = shell
	// The session-data guard blocks agent writes into Reasonix's own session
	// stores (they race the app's saves and surface as conflict-copy loops);
	// explicit allow_write entries stay a sanctioned escape hatch.
	sessionGuard := builtin.NewSessionDataGuard(config.MemoryUserDir(), cfg.AllowWriteRoots())
	if bashSpec.Mode == "enforce" && !sandbox.Available() {
		fmt.Fprintln(stderr, "warning: "+sandbox.UnavailableMessage())
	}
	if autoShellPrefer(cfg.Tools.Shell.Prefer) && shell.Kind == sandbox.ShellPowerShell {
		fmt.Fprintln(stderr, "warning: bash not found on PATH; the shell tool will run commands under Windows PowerShell. Install Git for Windows or WSL to use bash, or set [tools.shell] prefer=\"powershell\" to silence this.")
	}
	searchSpec := builtin.ResolveSearch(cfg.Tools.Search.Engine, cfg.Tools.Search.RgPath, stderr)
	bashTimeout := time.Duration(cfg.BashTimeoutSeconds()) * time.Second
	enabledBuiltins := cfg.Tools.Enabled
	if tokenEconomy {
		enabledBuiltins = tokenEconomyBuiltins(enabledBuiltins)
	}
	readPathResolver := builtin.NewPathResolver()
	addBuiltins(reg, enabledBuiltins, cfg.WriteRootsForRoot(root), bashSpec, bashTimeout, searchSpec, stderr, root, proxySpec, cfg.ForbidReadRootsForRoot(root), readPathResolver, sessionGuard)
	// Use the caller-supplied shared host when set, so controllers for the same
	// workspace root reuse running MCP processes (e.g. one CodeGraph daemon
	// instead of one per tab). Otherwise construct a private host per controller.
	pluginHost := opts.SharedHost
	if pluginHost == nil {
		pluginHost = plugin.NewHost()
	}

	// Partition configured plugins by tier so eager can block when explicitly
	// requested while every other enabled MCP warms up in the background.
	pluginSpecOptions := PluginSpecOptions{
		DefaultCallTimeout:   time.Duration(cfg.MCPCallTimeoutSeconds()) * time.Second,
		PlanModeAllowedTools: cfg.Agent.PlanModeAllowedTools,
	}
	autoStartEntries := cfg.AutoStartPlugins()
	eagerEntries, bgEntries := partitionByTier(autoStartEntries)
	extraSpecs := applyDefaultMCPCallTimeout(
		applyPlanModeAllowedMCPToolTrust(applyKnownPluginOverrides(opts.ExtraPlugins, root), cfg.Agent.PlanModeAllowedTools),
		pluginSpecOptions.DefaultCallTimeout,
	)
	onDemandMCPSpecs := map[string]plugin.Spec{}
	onDemandMCPNames := []string{}
	if tokenEconomy {
		for _, spec := range append(PluginSpecsForRootWithOptions(autoStartEntries, root, pluginSpecOptions), extraSpecs...) {
			name := strings.TrimSpace(spec.Name)
			if name == "" {
				continue
			}
			if _, exists := onDemandMCPSpecs[name]; !exists {
				onDemandMCPNames = append(onDemandMCPNames, name)
			}
			onDemandMCPSpecs[name] = spec
		}
		eagerEntries, bgEntries = nil, nil
	}
	trustedMCPServers := planModeTrustedMCPServers(onDemandMCPSpecs)

	// Auto-demote: any eager plugin that has been chronically slow (recent
	// samples repeatedly hit the blocking startup budget) drops to background
	// for this session. The user keeps eager intent, just doesn't pay for it
	// on a server that's been misbehaving. A notice surfaces the demotion.
	var demoteMessages []string
	budget := plugin.DefaultStartupBudget()
	kept := eagerEntries[:0]
	for _, e := range eagerEntries {
		rec := plugin.Recommend(e.Name, budget, 0)
		if rec.Demote {
			demoteMessages = append(demoteMessages, rec.Reason)
			bgEntries = append(bgEntries, e)
			continue
		}
		kept = append(kept, e)
	}
	eagerEntries = kept

	eagerSpecs := PluginSpecsForRootWithOptions(eagerEntries, root, pluginSpecOptions)
	bgSpecs := PluginSpecsForRootWithOptions(bgEntries, root, pluginSpecOptions)

	if !tokenEconomy {
		eagerSpecs = append(eagerSpecs, extraSpecs...)
	}

	// Apply caller-supplied stderr override to every spec across tiers.
	if opts.Stderr != nil {
		for i := range eagerSpecs {
			eagerSpecs[i].Stderr = opts.Stderr
		}
		for i := range bgSpecs {
			bgSpecs[i].Stderr = opts.Stderr
		}
	}

	// Eager: block until handshake. Failures show up in /mcp.
	if len(eagerSpecs) > 0 {
		// When using a shared host, reuse already-connected clients and
		// add new ones directly to the host instead of creating a separate one.
		if opts.SharedHost != nil {
			for _, s := range eagerSpecs {
				if pluginHost.HasClient(s.Name) {
					tools, err := pluginHost.ToolsFor(ctx, s.Name)
					if err == nil {
						for _, t := range tools {
							reg.Add(t)
						}
						continue
					}
				}
				// Use a bounded per-plugin timeout matching StartAvailable's
				// defaultStartTimeout (5s) so a hanging MCP server doesn't
				// block the tab boot indefinitely.
				addCtx, addCancel := context.WithTimeout(ctx, 5*time.Second)
				tools, err := pluginHost.Add(addCtx, s)
				addCancel()
				if err != nil {
					if plugin.IsServerAlreadyConnected(err) || errors.Is(err, plugin.ErrSpawningInFlight) {
						// Race: another tab connected the same server between
						// HasClient and Add, or is currently spawning it.
						// Fetch tools from the existing client, or wait briefly.
						tools, err2 := pluginHost.ToolsFor(ctx, s.Name)
						if err2 == nil {
							for _, t := range tools {
								reg.Add(t)
							}
							continue
						}
					}
					sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
						Text: "An MCP server failed to start.", Detail: fmt.Sprintf("mcp %s: %v", s.Name, err)})
					continue
				}
				for _, t := range tools {
					reg.Add(t)
				}
			}
		} else {
			host, ptools := plugin.StartAvailable(ctx, eagerSpecs)
			pluginHost = host
			for _, t := range ptools {
				reg.Add(t)
			}
			// PhaseB (prompts + resources) runs on the boot ctx — which is the
			// controller's session-scoped PluginCtx — so the auxiliary surfaces
			// keep streaming in after Start returns without holding up the agent.
			go host.StartPhaseB(ctx, sink)
			if text, detail, ok := MCPStartupNotice(host.Failures()); ok {
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text, Detail: detail})
			}
		}
	}

	// Background: register placeholder tools now and kick off the real spawn.
	// Everything shares the same pluginHost so /mcp status, hot-add, and Close
	// see one cohesive set of servers.
	registerBackground := func(specs []plugin.Spec) {
		for _, s := range specs {
			// Already running on the shared host? Register tools directly.
			if pluginHost.HasClient(s.Name) {
				tools, err := pluginHost.ToolsFor(ctx, s.Name)
				if err == nil {
					for _, t := range tools {
						reg.Add(t)
					}
					continue
				}
			}
			if opts.SharedHost != nil {
				// Shared host relies on Host's spawn guard to avoid duplicate
				// processes across tabs for the same workspace root.
				cs, _ := plugin.LoadCachedSchema(s.Name, plugin.SpecFingerprint(s))
				for _, t := range plugin.LazyToolset(s, cs, pluginHost, reg, ctx, true) {
					reg.Add(t)
				}
			} else {
				cs, _ := plugin.LoadCachedSchema(s.Name, plugin.SpecFingerprint(s))
				for _, t := range plugin.LazyToolset(s, cs, pluginHost, reg, ctx, true) {
					reg.Add(t)
				}
			}
		}
	}
	registerBackground(bgSpecs)

	for _, msg := range demoteMessages {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: msg})
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
	lspToolsAdded := false
	addLSPTools := func() []string {
		if lspMgr == nil || lspToolsAdded {
			return nil
		}
		lspToolsAdded = true
		return addTools(reg, lsp.Tools(lspMgr))
	}
	if cfg.LSP.Enabled {
		lspMgr = lsp.NewManager(root, LSPSpecs(cfg.LSP))
		if !tokenEconomy {
			addLSPTools()
		}
		prev := cleanup
		cleanup = func() { prev(); lspMgr.Close() }
	}

	maxSteps := cfg.Agent.MaxSteps
	if opts.MaxSteps > 0 {
		maxSteps = opts.MaxSteps
	}
	subagentStore, err := newSubagentStore(sessionDir)
	if err != nil {
		return nil, err
	}
	if subagentStore != nil {
		subagentStore.WithDestroyedChecker(jm.IsDestroying)
	}

	// Permission policy gates every tool call. The headless gate (no Approver)
	// resolves ordinary "ask" decisions to allow — preserving `reasonix run`
	// autonomy — while deny rules and fresh-human approval tools hard-block.
	// Interactive frontends (chat, desktop) swap in an interactive gate later via
	// Controller.EnableInteractiveApproval.
	// Sub-agents always run headless: they have no UI to answer a prompt, so they
	// inherit this same gate.
	policy := permission.New(cfg.Permissions.Mode, cfg.Permissions.Allow, cfg.Permissions.Ask, cfg.Permissions.Deny)
	headlessGate := control.NewHeadlessPermissionGate(policy)

	// Hooks: load the global settings.json plus the project's (only when trusted —
	// project hooks run arbitrary shell commands, so cloning a repo must not
	// silently execute them). Non-blocking hook output is surfaced to the user as
	// a Notice through the shared sink. The runner fires PreToolUse/PostToolUse in
	// the agent loop and PermissionRequest/UserPromptSubmit/Stop at the controller
	// boundary.
	hooksTrusted := hook.IsTrusted(root, "")
	hookRunner := hook.NewRunner(
		hook.Load(hook.LoadOptions{ProjectRoot: root, Trusted: hooksTrusted}),
		root, nil,
		func(msg string) { sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg}) },
	)
	if hook.ProjectDefinesHooks(root) && !hooksTrusted {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: "this project defines hooks but they are not trusted — run /hooks trust to enable them"})
	}

	// The `task` tool spawns sub-agents that reuse the parent's provider and
	// tool registry. Wired here after the built-ins / plugins are loaded so
	// sub-agents inherit the full tool set (minus `task` itself, to keep
	// nesting out of the picture). It registers into the same reg the
	// executor uses, so the model surfaces it like any other tool.
	resolveSubagentProvider := func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error) {
		me := *entry
		if strings.TrimSpace(modelRef) != "" {
			resolved, ok := cfg.ResolveModel(modelRef)
			if !ok {
				return nil, nil, 0, fmt.Errorf("unknown model %q", modelRef)
			}
			me = *resolved
		}
		if strings.TrimSpace(effort) != "" {
			normalized, err := config.NormalizeEffort(&me, effort)
			if err != nil {
				return nil, nil, 0, err
			}
			me.Effort = normalized
			if me.Kind == "anthropic" && strings.TrimSpace(me.Effort) != "" && strings.TrimSpace(me.Thinking) == "" {
				me.Thinking = "adaptive"
			}
		}
		p, err := NewProviderWithProxy(&me, proxySpec)
		if err != nil {
			return nil, nil, 0, err
		}
		return p, me.Price, me.ContextWindow, nil
	}
	subagentIdentity := func(modelRef, effort string) (string, string) {
		return subagentEffectiveIdentity(cfg, modelName, entry, modelRef, effort)
	}
	taskModel := firstNonEmpty(cfg.Agent.SubagentModels["task"], cfg.Agent.SubagentModel)
	taskEffort := firstNonEmpty(cfg.Agent.SubagentEfforts["task"], cfg.Agent.SubagentEffort)
	maxSubagentDepth := agent.NormalizeMaxSubagentDepth(cfg.Agent.MaxSubagentDepth)
	taskToolAdded := false
	readOnlyTaskToolAdded := false
	var taskTool *agent.TaskTool
	newTaskTool := func() *agent.TaskTool {
		return agent.NewTaskTool(execProv, entry.Price, reg, maxSteps,
			entry.ContextWindow, cfg.Agent.RecentKeep, cfg.Agent.SoftCompactRatio, cfg.Agent.ToolResultSnipRatio, cfg.Agent.CompactRatio, cfg.Agent.CompactForceRatio,
			cfg.Agent.Temperature, config.ArchiveDir(), "", headlessGate,
			keepPolicy,
			taskModel, taskEffort, resolveSubagentProvider).
			WithTranscripts(subagentStore, root, modelName, entry.Effort).
			WithTranscriptIdentityResolver(subagentIdentity).
			WithMaxSubagentDepth(maxSubagentDepth)
	}
	addTaskTool := func() string {
		if taskToolAdded {
			return "task tool is already enabled."
		}
		taskToolAdded = true
		if taskTool == nil {
			taskTool = newTaskTool()
		}
		reg.Add(taskTool)
		reg.Add(agent.NewParallelTasksTool(taskTool, reg))
		return "enabled task."
	}
	addReadOnlyTaskTool := func() string {
		if readOnlyTaskToolAdded {
			return "read_only_task tool is already enabled."
		}
		readOnlyTaskToolAdded = true
		if taskTool == nil {
			taskTool = newTaskTool()
		}
		reg.Add(agent.NewReadOnlyTaskTool(taskTool))
		return "enabled read_only_task."
	}
	if !tokenEconomy {
		addTaskTool()
		addReadOnlyTaskTool()
	}

	// The `memory` tool searches/reads saved facts on demand; `remember` persists
	// durable facts to the project's auto-memory store; `forget` prunes ones that
	// turn out wrong. The saved index loads into the prefix on the next session.
	reg.Add(history.NewTool(history.Options{SessionDir: sessionDir, GlobalSessionDir: config.SessionDir(), ArchiveDir: config.ArchiveDir()}))

	// Session history tools let the AI discover and read past conversations.
	// `list_sessions` returns all saved session files; `read_session` loads one
	// and renders the full conversation as readable text.
	reg.Add(sessiontool.NewListSessionsTool(sessionDir))
	reg.Add(sessiontool.NewReadSessionTool(sessionDir))

	reg.Add(memory.NewRecallTool(mem.Store))
	reg.Add(memory.NewRememberTool(mem.Store))
	reg.Add(memory.NewForgetTool(mem.Store))

	// The `ask` tool puts structured multiple-choice questions to the user. It
	// reaches them through the Asker on the call context, which interactive
	// frontends wire to the controller (EnableInteractiveApproval); a headless run
	// has none, so ask resolves to "decide for yourself".
	reg.Add(agent.NewAskTool())

	// Skill tools: read_only_skill is a narrow plan-mode-safe entry point; the
	// full skills source adds run_skill / install_skill plus the dedicated
	// subagent wrappers (explore / research / review / security_review). Read-only
	// subagent skills run ephemerally with the same registry boundary as
	// read_only_task, so they cannot write, install, mutate memory, resume/fork
	// transcripts, or delegate further.
	readOnlySkillRunner := func(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (string, error) {
		if strings.TrimSpace(runOpts.ContinueFrom) != "" || strings.TrimSpace(runOpts.ForkFrom) != "" {
			return "", fmt.Errorf("read_only_skill does not support continue_from/fork_from")
		}
		sk = skill.WithCodeGraphTools(sk, skill.CodeGraphReadTools(reg))
		prov, price, ctxWin := execProv, entry.Price, entry.ContextWindow
		modelRef := subagentModelRef(cfg, sk)
		effortRef := subagentEffortRef(cfg, sk)
		if modelRef != "" || effortRef != "" {
			p, pr, cw, err := resolveSubagentProvider(modelRef, effortRef)
			if err != nil {
				return "", fmt.Errorf("read-only subagent skill %q profile: %w", sk.Name, err)
			}
			prov, price, ctxWin = p, pr, cw
		}
		childDepth := agent.SubagentDepth(sctx) + 1
		if childDepth > maxSubagentDepth {
			return "", fmt.Errorf("subagent delegation depth limit reached (max_subagent_depth=%d)", maxSubagentDepth)
		}
		subReg := agent.ReadOnlySubagentToolRegistryForDepth(reg, sk.AllowedTools, childDepth, maxSubagentDepth)
		if subReg.Len() == 0 {
			return "", fmt.Errorf("read_only_skill: skill %q has no read-only tools available", sk.Name)
		}
		steps := maxSteps
		if steps > 0 {
			if steps /= 2; steps < 5 {
				steps = 5
			}
		}
		sysPrompt := agent.DefaultReadOnlyTaskSystemPrompt + "\n\nSkill instructions:\n" + sk.Body
		return agent.RunSubAgentWithSession(sctx, prov, subReg, agent.NewSession(sysPrompt), task, agent.Options{
			MaxSteps:            steps,
			Temperature:         cfg.Agent.Temperature,
			Pricing:             price,
			UsageSource:         event.UsageSourceSubagent,
			Gate:                headlessGate,
			ContextWindow:       ctxWin,
			RecentKeep:          cfg.Agent.RecentKeep,
			SoftCompactRatio:    cfg.Agent.SoftCompactRatio,
			ToolResultSnipRatio: cfg.Agent.ToolResultSnipRatio,
			CompactRatio:        cfg.Agent.CompactRatio,
			CompactForceRatio:   cfg.Agent.CompactForceRatio,
			ArchiveDir:          config.ArchiveDir(),
			KeepPolicy:          keepPolicy,
			ReasoningLanguage:   agent.ReasoningLanguageFromContext(sctx),
			SubagentDepth:       childDepth,
			MaxSubagentDepth:    maxSubagentDepth,
		}, agent.NestedSink(sctx, event.Discard))
	}
	// Writer-capable subagent skills reuse the sub-agent machinery via this
	// runner: an isolated loop with the skill body as system prompt, a tool set
	// scoped to the skill's allowed-tools (minus recursive meta-tools), optional
	// per-skill model, and resumable transcripts when the parent session supports
	// them. Its tool activity nests under the invoking call, like `task`.
	skillRunner := func(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (string, error) {
		sk = skill.WithCodeGraphTools(sk, skill.CodeGraphReadTools(reg))
		prov, price, ctxWin := execProv, entry.Price, entry.ContextWindow
		modelRef := subagentModelRef(cfg, sk)
		effortRef := subagentEffortRef(cfg, sk)
		if modelRef != "" || effortRef != "" {
			p, pr, cw, err := resolveSubagentProvider(modelRef, effortRef)
			if err != nil {
				return "", fmt.Errorf("subagent skill %q profile: %w", sk.Name, err)
			}
			prov, price, ctxWin = p, pr, cw
		}
		childDepth := agent.SubagentDepth(sctx) + 1
		if childDepth > maxSubagentDepth {
			return "", fmt.Errorf("subagent delegation depth limit reached (max_subagent_depth=%d)", maxSubagentDepth)
		}
		subReg := agent.SubagentToolRegistryForDepth(reg, sk.AllowedTools, childDepth, maxSubagentDepth)
		continueFrom := strings.TrimSpace(runOpts.ContinueFrom)
		legacyForkFrom := strings.TrimSpace(runOpts.ForkFrom)
		if continueFrom != "" && legacyForkFrom != "" {
			return "", fmt.Errorf("continue_from and fork_from are mutually exclusive; pass only continue_from")
		}
		parentID, _, _, _ := agent.CallContext(sctx)
		parentSession := agent.ParentSession(sctx)
		var run *agent.SubagentRun
		if subagentStore == nil || parentSession == "" {
			// Headless runs (e.g. `reasonix run`) have no persistent session to
			// own a transcript. Run the skill sub-agent ephemerally, as before
			// persisted transcripts existed, instead of failing. Continuation needs
			// a persisted owner, so it errors here.
			if continueFrom != "" || legacyForkFrom != "" {
				return "", fmt.Errorf("subagent continuation requires a persisted session; none is active in this run")
			}
			run = agent.EphemeralSubagentRun(sk.Body)
		} else {
			identityModel, identityEffort := subagentIdentity(modelRef, effortRef)
			spec := agent.SubagentSpec{
				Kind:             "skill",
				Name:             sk.Name,
				WorkspaceRoot:    root,
				ParentSession:    parentSession,
				ParentToolCallID: parentID,
				SystemPrompt:     sk.Body,
				Registry:         subReg,
				Model:            identityModel,
				Effort:           identityEffort,
			}
			var prepErr error
			if continueFrom != "" {
				run, prepErr = subagentStore.PrepareContinue(continueFrom, spec)
			} else if legacyForkFrom != "" {
				run, prepErr = subagentStore.PrepareLegacyForkFrom(legacyForkFrom, spec)
			} else {
				run, prepErr = subagentStore.PrepareFresh(spec)
			}
			if prepErr != nil {
				return "", prepErr
			}
		}
		defer run.Release()
		steps := maxSteps
		if steps > 0 {
			if steps /= 2; steps < 5 {
				steps = 5
			}
		}
		answer, err := agent.RunSubAgentWithSession(sctx, prov, subReg, run.Session, task, agent.Options{
			MaxSteps:          steps,
			Temperature:       cfg.Agent.Temperature,
			Pricing:           price,
			UsageSource:       event.UsageSourceSubagent,
			Gate:              headlessGate,
			ContextWindow:     ctxWin,
			RecentKeep:        cfg.Agent.RecentKeep,
			ArchiveDir:        config.ArchiveDir(),
			KeepPolicy:        keepPolicy,
			ReasoningLanguage: agent.ReasoningLanguageFromContext(sctx),
			SubagentDepth:     childDepth,
			MaxSubagentDepth:  maxSubagentDepth,
		}, agent.NestedSink(sctx, event.Discard))
		if err != nil {
			return "", errors.Join(err, subagentStore.SaveFailed(run))
		}
		if err := subagentStore.SaveCompleted(run); err != nil {
			return "", errors.Join(err, subagentStore.SaveFailed(run))
		}
		return agent.FormatSubagentRunResult(answer, run, false), nil
	}
	skillProfile := func(sk skill.Skill) *event.Profile {
		model, effort := subagentModelRef(cfg, sk), subagentEffortRef(cfg, sk)
		if model == "" && effort == "" {
			return nil
		}
		return &event.Profile{Model: model, Effort: effort}
	}
	// Custom slash commands (.reasonix/commands + user dir). Best-effort: a malformed
	// file is skipped, and a load error never blocks the session.
	cmds, _ := command.Load(config.CommandDirsForRoot(root)...)
	addSlashCommandTool := func(includeSkills bool) {
		// Expose loaded slash commands to the model via slash_command. In economy
		// mode skills join this list only after the skills source is enabled.
		var slashEntries []command.SlashEntry
		if includeSkills {
			for _, sk := range skills {
				sk := sk
				slashEntries = append(slashEntries, command.SlashEntry{
					Name:        sk.Name,
					Description: sk.Description,
					Render:      func(args []string) string { return skill.Render(sk, strings.Join(args, " ")) },
				})
			}
		}
		for _, cmd := range cmds {
			cmd := cmd
			slashEntries = append(slashEntries, command.SlashEntry{
				Name:        cmd.Name,
				Description: cmd.Description,
				ArgHint:     cmd.ArgHint,
				Render:      func(args []string) string { return cmd.Render(args) },
			})
		}
		reg.Add(command.NewSlashCommandTool(slashEntries))
	}
	installSourceAdded := false
	addInstallSourceTool := func() string {
		if installSourceAdded {
			return "install_source is already enabled."
		}
		installSourceAdded = true
		reg.Add(installsource.NewTool(installsource.Options{
			ProjectRoot: root,
			HTTPClient:  balanceClient,
			ConnectMCP: func(e config.PluginEntry) (installsource.MCPConnectResult, error) {
				spec := pluginSpecFromEntryWithOptions(e, root, pluginSpecOptions)
				if opts.Stderr != nil {
					spec.Stderr = opts.Stderr
				}
				tools, err := pluginHost.Add(ctx, spec)
				if err != nil {
					return installsource.MCPConnectResult{}, err
				}
				reg.RemovePrefix(plugin.ToolPrefix(spec.Name))
				for _, t := range tools {
					reg.Add(t)
				}
				// Disconnect closes the server and drops its namespaced tools.
				// Used by the install_source rollback path when SaveTo fails.
				disconnect := func() {
					if prefix, ok := pluginHost.Remove(spec.Name); ok {
						reg.RemovePrefix(prefix)
					}
				}
				return installsource.MCPConnectResult{
					ToolCount:  len(tools),
					Disconnect: disconnect,
				}, nil
			},
			OnDisconnect: func(serverName string) bool {
				if prefix, ok := pluginHost.Remove(serverName); ok {
					reg.RemovePrefix(prefix)
					return true
				}
				return false
			},
		}))
		return "enabled install_source."
	}
	readOnlySkillToolsAdded := false
	addReadOnlySkillTools := func() string {
		if readOnlySkillToolsAdded {
			return "read_only_skill tool is already enabled.\n\n" + skill.ReadOnlyIndexBlock(skills)
		}
		readOnlySkillToolsAdded = true
		reg.Add(skill.NewReadOnlySkillTool(skillStore, readOnlySkillRunner, skillProfile))
		return "enabled read_only_skill. Use read_only_skill for inline skills or read-only subagent skills on the next model request.\n\n" + skill.ReadOnlyIndexBlock(skills)
	}
	skillToolsAdded := false
	addSkillTools := func() string {
		if skillToolsAdded {
			return "skills are already enabled.\n\n" + skill.IndexBlock(skills)
		}
		skillToolsAdded = true
		addReadOnlySkillTools()
		reg.Add(skill.NewRunSkillTool(skillStore, skillRunner, skillProfile))
		reg.Add(skill.NewReadSkillTool(skillStore))
		reg.Add(skill.NewInstallSkillTool(skillStore, nil))
		for _, t := range skill.BuiltinSubagentTools(skillStore, skillRunner, skillProfile) {
			reg.Add(t)
		}
		addSlashCommandTool(true)
		return "enabled skills. Use run_skill/read_skill/read_only_skill or the dedicated skill tools on the next model request.\n\n" + skill.IndexBlock(skills)
	}
	if tokenEconomy {
		addSlashCommandTool(false)
	} else {
		addInstallSourceTool()
		addSkillTools()
	}
	if tokenEconomy {
		reg.Add(&toolSourceConnector{
			skills: func(context.Context) (string, error) {
				return addSkillTools(), nil
			},
			task: func(context.Context) (string, error) {
				return addTaskTool(), nil
			},
			readOnlyTask: func(context.Context) (string, error) {
				return addReadOnlyTaskTool(), nil
			},
			readOnlySkill: func(context.Context) (string, error) {
				return addReadOnlySkillTools(), nil
			},
			install: func(context.Context) (string, error) {
				return addInstallSourceTool(), nil
			},
			webFetch: func(context.Context) (string, error) {
				if !builtinToolEnabled(cfg.Tools.Enabled, "web_fetch") {
					return "web_fetch is disabled by [tools].enabled.", nil
				}
				names := addTools(reg, builtin.Workspace{
					Dir:         root,
					WriteRoots:  cfg.WriteRootsForRoot(root),
					Bash:        bashSpec,
					BashTimeout: bashTimeout,
					Search:      searchSpec,
					ProxySpec:   proxySpec,
				}.Tools("web_fetch"))
				if len(names) == 0 {
					return "web_fetch is already enabled or unavailable.", nil
				}
				return "enabled " + strings.Join(names, ", ") + ".", nil
			},
			lsp: func(context.Context) (string, error) {
				if lspMgr == nil {
					return "", fmt.Errorf("LSP is disabled in config")
				}
				names := addLSPTools()
				if len(names) == 0 {
					return "LSP tools are already enabled.", nil
				}
				return "enabled " + strings.Join(names, ", ") + ".", nil
			},
			mcp: func(_ context.Context, name string) (string, error) {
				spec, ok := onDemandMCPSpecs[name]
				if !ok {
					return "", fmt.Errorf("no configured MCP server named %q", name)
				}
				if opts.Stderr != nil {
					spec.Stderr = opts.Stderr
				}
				tools, err := pluginHost.Add(ctx, spec)
				if err != nil {
					// On a shared host the server may already be connected
					// (e.g. another tab started it). Fall back to fetching
					// its tools from the existing client.
					if errors.Is(err, plugin.ErrServerAlreadyConnected) || errors.Is(err, plugin.ErrSpawningInFlight) {
						tools, err2 := pluginHost.ToolsFor(ctx, spec.Name)
						if err2 != nil {
							return "", err2
						}
						reg.RemovePrefix(plugin.ToolPrefix(spec.Name))
						names := addTools(reg, tools)
						if len(names) == 0 {
							return fmt.Sprintf("MCP server %q connected but exposed no tools.", spec.Name), nil
						}
						return fmt.Sprintf("enabled MCP server %q tools: %s.", spec.Name, strings.Join(names, ", ")), nil
					}
					return "", err
				}
				reg.RemovePrefix(plugin.ToolPrefix(spec.Name))
				names := addTools(reg, tools)
				if len(names) == 0 {
					return fmt.Sprintf("MCP server %q connected but exposed no tools.", spec.Name), nil
				}
				return fmt.Sprintf("enabled MCP server %q tools: %s.", spec.Name, strings.Join(names, ", ")), nil
			},
			mcpNames:                 onDemandMCPNames,
			planModeAllowedTools:     cfg.Agent.PlanModeAllowedTools,
			planModeTrustedMCPServer: trustedMCPServers,
		})
	}

	execSess := agent.NewSession(sysPrompt)
	var memCompiler *memorycompiler.Runtime
	if cfg.MemoryCompilerEnabled() {
		memCompiler = memorycompiler.New(config.MemoryCompilerDir(root))
	}
	executor := agent.New(execProv, reg, execSess, agent.Options{
		MaxSteps:                           maxSteps,
		Temperature:                        cfg.Agent.Temperature,
		Pricing:                            entry.Price,
		Gate:                               headlessGate,
		Hooks:                              hookRunner,
		Jobs:                               jm,
		ProjectChecks:                      projectChecks,
		ContextWindow:                      entry.ContextWindow,
		SoftCompactRatio:                   cfg.Agent.SoftCompactRatio,
		ToolResultSnipRatio:                cfg.Agent.ToolResultSnipRatio,
		CompactRatio:                       cfg.Agent.CompactRatio,
		CompactForceRatio:                  cfg.Agent.CompactForceRatio,
		RecentKeep:                         cfg.Agent.RecentKeep,
		ArchiveDir:                         config.ArchiveDir(),
		KeepPolicy:                         keepPolicy,
		ReasoningLanguage:                  cfg.ReasoningLanguage(),
		PlanModeAllowedTools:               cfg.Agent.PlanModeAllowedTools,
		PlanModeReadOnlyCommands:           cfg.Agent.PlanModeReadOnlyCommands,
		SubagentDepth:                      0,
		MaxSubagentDepth:                   maxSubagentDepth,
		MemoryCompiler:                     memCompiler,
		MemoryCompilerVerbosity:            cfg.MemoryCompilerVerbosity(),
		UseMemoryCompilerLLMClassification: strings.TrimSpace(os.Getenv("REASONIX_MEMORY_COMPILER_LLM_CLASSIFICATION")) == "true",
	}, sink)

	var runner agent.Runner = executor
	label := entry.Model
	var classifier *control.ProviderAutoPlanClassifier

	if !tokenEconomy && !strings.EqualFold(strings.TrimSpace(cfg.Agent.AutoPlan), "off") && cfg.Agent.AutoPlanClassifier != "" {
		cm := cfg.Agent.AutoPlanClassifier
		ce, ok := cfg.ResolveModel(cm)
		if !ok {
			return nil, fmt.Errorf("auto_plan_classifier %q is not a configured provider", cm)
		}
		classifierProv, err := NewProviderWithProxy(ce, proxySpec)
		if err != nil {
			return nil, fmt.Errorf("auto_plan_classifier %q: %w", cm, err)
		}
		classifier = control.NewBillableProviderAutoPlanClassifier(classifierProv, ce.Price, sink)
	}

	// Two-model collaboration: a distinct planner_model wraps the executor in a
	// Coordinator with its own session, kept separate for cache stability. The
	// planner gets the same standing memory context and a filtered read-only
	// research tool set, so it can inspect rules/code without side effects.
	if pm := cfg.Agent.PlannerModel; pm != "" && !tokenEconomy {
		pe, ok := cfg.ResolveModel(pm)
		if !ok {
			return nil, fmt.Errorf("planner_model %q is not a configured provider", pm)
		}
		if pe.Model != entry.Model {
			plannerProv, err := NewProviderWithProxy(pe, proxySpec)
			if err != nil {
				return nil, fmt.Errorf("planner %q: %w", pm, err)
			}
			plannerSess := agent.NewSession(agent.PlannerPromptWithContext(mem.Block()))
			plannerTools := agent.PlannerToolRegistry(reg)
			runner = agent.NewCoordinator(plannerProv, plannerSess, pe.Price, plannerTools, agent.Options{
				MaxSteps:                 cfg.Agent.PlannerMaxSteps,
				MaxStepsKey:              "agent.planner_max_steps",
				Gate:                     headlessGate,
				ContextWindow:            pe.ContextWindow,
				SoftCompactRatio:         cfg.Agent.SoftCompactRatio,
				ToolResultSnipRatio:      cfg.Agent.ToolResultSnipRatio,
				CompactRatio:             cfg.Agent.CompactRatio,
				CompactForceRatio:        cfg.Agent.CompactForceRatio,
				RecentKeep:               cfg.Agent.RecentKeep,
				ArchiveDir:               config.ArchiveDir(),
				KeepPolicy:               keepPolicy,
				ReasoningLanguage:        cfg.ReasoningLanguage(),
				PlanModeReadOnlyCommands: cfg.Agent.PlanModeReadOnlyCommands,
			}, executor, cfg.Agent.Temperature, sink, control.NewPlannerGate(classifier))
			label = entry.Model + " + planner " + pe.Model
		}
	}

	ctrlOpts := control.Options{
		Runner:                 runner,
		Executor:               executor,
		Sink:                   sink,
		Policy:                 policy,
		Label:                  label,
		ModelRef:               modelRef,
		SystemPrompt:           sysPrompt,
		SessionDir:             sessionDir,
		Host:                   pluginHost,
		Commands:               cmds,
		Skills:                 skills,
		AllSkills:              allSkills,
		SkillStore:             skillStore,
		AllSkillStore:          allSkillStore,
		Hooks:                  hookRunner,
		Memory:                 mem,
		Cleanup:                cleanup,
		BalanceURL:             entry.BalanceURL,
		BalanceKey:             entry.APIKey(),
		BalanceClient:          balanceClient,
		Jobs:                   jm,
		Registry:               reg,
		PluginCtx:              ctx,
		WorkspaceRoot:          root,
		ExternalFolderToolRefs: readPathResolver,
		AutoPlan:               cfg.Agent.AutoPlan,
		ResponseLanguage:       cfg.ResponseLanguage(),
		ReasoningLanguage:      cfg.ReasoningLanguage(),
		DisableColdResumePrune: !cfg.ColdResumePruneEnabled(),
		Shell:                  shell,
		PlanModeAllowedTools:   cfg.Agent.PlanModeAllowedTools,
		ApprovalTimeout:        opts.ApprovalTimeout,
		OnRemember: func(rule string) control.RememberResult {
			return rememberPermissionRule(root, rule)
		},
		OnRememberMCPReadOnlyTrust: func(serverName, rawToolName string) control.MCPReadOnlyTrustResult {
			return rememberMCPReadOnlyTrust(root, serverName, rawToolName)
		},
		OnRememberPlanModeReadOnlyCommand: func(prefix string) control.PlanModeReadOnlyCommandTrustResult {
			return rememberPlanModeReadOnlyCommand(root, prefix)
		},
		SessionRecoveryMeta: opts.SessionRecoveryMeta,
		OnSessionRecovered:  opts.OnSessionRecovered,
	}
	// Guardian: when guardian_model is configured, spawn an LLM safety reviewer
	// that can auto-allow safe Ask decisions and annotate risky ones before
	// escalating to the human approval prompt.
	if guardianModel := cfg.Agent.GuardianModel; guardianModel != "" {
		ge, ok := cfg.ResolveModel(guardianModel)
		if !ok {
			slog.Warn("guardian model is not a configured provider — guardian disabled", "model", guardianModel)
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Guardian was disabled because its model was not found.", Detail: fmt.Sprintf("guardian_model %q not found — guardian disabled", guardianModel)})
		} else {
			pProv, err := NewProviderWithProxy(ge, proxySpec)
			if err != nil {
				slog.Warn("guardian provider construction failed — guardian disabled", "model", guardianModel, "err", err)
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Guardian was disabled because it could not start.", Detail: fmt.Sprintf("guardian construction failed: %v — guardian disabled", err)})
			} else {
				guardianReg := agent.FilterReadOnlyRegistry(reg, agent.SubagentMetaTools()...)
				ctrlOpts.Guardian = guardian.NewSession(pProv, guardianReg, guardian.PolicyPrompt(), guardianModel, cfg.Agent.GuardianTemperature, ge.Price, sink)
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf("guardian enabled · model=%s", ge.Model)})
			}
		}
	}
	if classifier != nil {
		ctrlOpts.Classifier = classifier
	}
	return control.New(ctrlOpts), nil
}

func rememberPermissionRule(workspaceRoot, rule string) control.RememberResult {
	path := rememberPermissionConfigPath(workspaceRoot)
	edit := config.LoadForEdit(path)
	result := control.RememberResult{Rule: strings.TrimSpace(rule), Path: path}
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
	if err := config.WritePermissionsSection(path, edit.Permissions.Allow); err != nil {
		slog.Warn("save config after permission rule", "err", err)
		result.Err = err
		return result
	}
	result.Saved = true
	return result
}

func rememberPermissionConfigPath(workspaceRoot string) string {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot != "" {
		return filepath.Join(workspaceRoot, "reasonix.toml")
	}
	path := config.SourcePath()
	if path == "" {
		path = "reasonix.toml" // match Config.Save() fallback
	}
	return path
}

func rememberMCPReadOnlyTrust(workspaceRoot, serverName, rawToolName string) control.MCPReadOnlyTrustResult {
	serverName = strings.TrimSpace(serverName)
	rawToolName = strings.TrimSpace(rawToolName)
	result := control.MCPReadOnlyTrustResult{Server: serverName, Tool: rawToolName}
	_, changed, path, err := config.TrustPluginReadOnlyToolInSourceForRoot(workspaceRoot, serverName, rawToolName)
	result.Path = path
	if err != nil {
		slog.Warn("persist MCP read-only trust", "server", serverName, "tool", rawToolName, "err", err)
		result.Err = err
		return result
	}
	if changed {
		result.Saved = true
		return result
	}
	result.CoveredBy = rawToolName
	return result
}

func rememberPlanModeReadOnlyCommand(workspaceRoot, prefix string) control.PlanModeReadOnlyCommandTrustResult {
	prefix = strings.TrimSpace(prefix)
	path := rememberPermissionConfigPath(workspaceRoot)
	edit := config.LoadForEdit(path)
	result := control.PlanModeReadOnlyCommandTrustResult{Prefix: prefix, Path: path}
	if prefix == "" {
		result.Err = fmt.Errorf("empty plan-mode read-only command prefix")
		return result
	}
	if coveredBy := coveredPlanModeReadOnlyCommand(edit.Agent.PlanModeReadOnlyCommands, prefix); coveredBy != "" {
		result.CoveredBy = coveredBy
		return result
	}
	edit.Agent.PlanModeReadOnlyCommands = append(edit.Agent.PlanModeReadOnlyCommands, prefix)
	if err := edit.SaveTo(path); err != nil {
		slog.Warn("persist plan-mode read-only command trust", "prefix", prefix, "err", err)
		result.Err = err
		return result
	}
	result.Saved = true
	return result
}

func coveredPlanModeReadOnlyCommand(existing []string, candidate string) string {
	candidateFields := strings.Fields(strings.TrimSpace(candidate))
	if len(candidateFields) == 0 {
		return ""
	}
	for _, item := range existing {
		itemFields := strings.Fields(strings.TrimSpace(item))
		if len(itemFields) == 0 || len(itemFields) > len(candidateFields) {
			continue
		}
		matches := true
		for i, field := range itemFields {
			if candidateFields[i] != field {
				matches = false
				break
			}
		}
		if matches {
			return strings.Join(itemFields, " ")
		}
	}
	return ""
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
		for _, key := range subagentModelKeys(sk.Name) {
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
		for _, key := range subagentModelKeys(sk.Name) {
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

func subagentModelKeys(name string) []string {
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
		seen := false
		for _, key := range keys {
			if key == alias {
				seen = true
				break
			}
		}
		if !seen {
			keys = append(keys, alias)
		}
	}
	return keys
}

func currentWorkspacePromptLine(root string) string {
	if root == "" {
		return ""
	}
	return "Current workspace: " + strconv.Quote(root)
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

func newSubagentStore(sessionDir string) (*agent.SubagentStore, error) {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return nil, nil
	}
	store := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	if _, err := store.CleanupStaleRunning(); err != nil {
		return nil, fmt.Errorf("cleanup stale subagents: %w", err)
	}
	return store, nil
}

func subagentEffectiveIdentity(cfg *config.Config, baseModelRef string, base *config.ProviderEntry, modelRef, effort string) (string, string) {
	var entry config.ProviderEntry
	if base != nil {
		entry = *base
	}
	ref := strings.TrimSpace(modelRef)
	if ref == "" {
		ref = strings.TrimSpace(baseModelRef)
	}
	if cfg != nil && ref != "" {
		if resolved, ok := cfg.ResolveModel(ref); ok {
			entry = *resolved
		} else if strings.TrimSpace(modelRef) != "" {
			entry.Model = ref
		}
	} else if strings.TrimSpace(modelRef) != "" {
		entry.Model = strings.TrimSpace(modelRef)
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
		APIKey:  e.APIKey(),
		// Pass the key's env var so auth failures can name where to fix it, plus
		// provider-kind-specific knobs. EffectiveEffort applies a configured
		// default_effort when the user has not explicitly selected /effort.
		Extra: map[string]any{
			"api_key_env":        e.APIKeyEnv,
			"api_key_source":     e.APIKeySourceLabel(),
			"thinking":           e.Thinking,
			"effort":             config.EffectiveEffort(e),
			"reasoning_protocol": config.ReasoningProtocolForEntry(e),
			"chat_url":           e.ChatURL,
			"headers":            e.Headers,
			"extra_body":         e.ExtraBody,
			"auth_header":        e.AuthHeader,
			"proxy_spec":         proxy,
			"vision":             config.EffectiveVision(e),
			"vision_detail":      e.VisionDetail,
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
// and makes bash warn when a command references them.
func addBuiltins(reg *tool.Registry, enabled, writeRoots []string, bashSpec sandbox.Spec, bashTimeout time.Duration, searchSpec builtin.SearchSpec, stderr io.Writer, workDir string, proxySpec netclient.ProxySpec, forbidReadRoots []string, readPathResolver *builtin.PathResolver, sessionGuard builtin.SessionDataGuard) {
	// If a workspace directory is set, use workspace-bound tools that resolve
	// paths relative to that directory. Otherwise fall back to the process-cwd
	// compile-time builtins.
	if workDir != "" {
		ws := builtin.Workspace{Dir: workDir, WriteRoots: writeRoots, ForbidReadRoots: forbidReadRoots, Bash: bashSpec, BashTimeout: bashTimeout, Search: searchSpec, ProxySpec: proxySpec, ReadPaths: readPathResolver, SessionGuard: sessionGuard}
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
	confined := append(builtin.ConfineWriters(writeRoots, sessionGuard),
		builtin.ConfineBash(bashSpec, sessionGuard, bashTimeout),
		builtin.ConfineSearch(searchSpec, bashSpec, forbidReadRoots),
		builtin.ConfineWebFetch(proxySpec))
	confined = append(confined, builtin.ConfineReaders(forbidReadRoots)...)
	for _, t := range confined {
		if _, ok := reg.Get(t.Name()); ok {
			reg.Add(t)
		}
	}
}

func builtinToolEnabled(enabled []string, name string) bool {
	if len(enabled) == 0 {
		return true
	}
	name = strings.TrimSpace(name)
	for _, candidate := range enabled {
		if strings.TrimSpace(candidate) == name {
			return true
		}
	}
	return false
}

// partitionByTier splits configured plugin entries into eager (block boot until
// ready) and background (placeholder + start spawn now). Entries with an empty,
// legacy lazy, or unrecognised tier land in background.
func partitionByTier(entries []config.PluginEntry) (eager, bg []config.PluginEntry) {
	for _, e := range entries {
		switch e.ResolvedTier() {
		case "eager":
			eager = append(eager, e)
		default:
			bg = append(bg, e)
		}
	}
	return eager, bg
}

// PluginSpecs maps configured plugin entries to plugin.Spec, expanding ${VAR}
// references. Exported so custom assemblers can connect the config's plugins
// alongside their own (e.g. ACP's per-session MCP servers).
func PluginSpecs(entries []config.PluginEntry) []plugin.Spec {
	return PluginSpecsForRoot(entries, "")
}

// PluginSpecsForRoot maps configured plugin entries to plugin.Spec and applies
// workspace-aware compatibility overrides for known cwd-sensitive servers.
func PluginSpecsForRoot(entries []config.PluginEntry, workspaceRoot string) []plugin.Spec {
	return PluginSpecsForRootWithPlanModeAllowedTools(entries, workspaceRoot, nil)
}

// PluginSpecOptions carries runtime policy that is not stored on each plugin
// entry but still needs to reach plugin.Spec.
type PluginSpecOptions struct {
	DefaultCallTimeout   time.Duration
	PlanModeAllowedTools []string
}

// PluginSpecsForRootWithPlanModeAllowedTools also promotes model-visible MCP
// names declared in agent.plan_mode_allowed_tools to trusted read-only model
// names for their matching server. This keeps the planner/read-only research
// trust path aligned with the plan-mode execution escape valve.
func PluginSpecsForRootWithPlanModeAllowedTools(entries []config.PluginEntry, workspaceRoot string, allowedTools []string) []plugin.Spec {
	return PluginSpecsForRootWithOptions(entries, workspaceRoot, PluginSpecOptions{
		PlanModeAllowedTools: allowedTools,
	})
}

// PluginSpecsForRootWithOptions maps configured plugin entries to plugin.Spec
// and injects runtime policy such as the global MCP call timeout.
func PluginSpecsForRootWithOptions(entries []config.PluginEntry, workspaceRoot string, opts PluginSpecOptions) []plugin.Spec {
	specs := make([]plugin.Spec, len(entries))
	for i, e := range entries {
		specs[i] = pluginSpecFromEntryWithOptions(e, workspaceRoot, opts)
	}
	return applyPlanModeAllowedMCPToolTrust(specs, opts.PlanModeAllowedTools)
}

func pluginSpecFromEntryWithOptions(e config.PluginEntry, workspaceRoot string, opts PluginSpecOptions) plugin.Spec {
	e = e.ExpandedPlugin() // resolve ${VAR} / ${VAR:-default} from the environment
	return plugin.ApplyKnownOverrides(plugin.Spec{
		Name:               e.Name,
		Type:               e.Type,
		Command:            e.Command,
		Args:               e.Args,
		Env:                e.Env,
		URL:                e.URL,
		Headers:            e.Headers,
		DefaultCallTimeout: opts.DefaultCallTimeout,
		CallTimeout:        secondsDuration(e.CallTimeoutSeconds),
		ToolTimeouts:       toolTimeoutDurations(e.ToolTimeoutSeconds),
		ReadOnlyToolNames:  trustedRawReadOnlyToolNames(e.TrustedReadOnlyTools),
	}, workspaceRoot)
}

func secondsDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func toolTimeoutDurations(seconds map[string]int) map[string]time.Duration {
	if len(seconds) == 0 {
		return nil
	}
	out := make(map[string]time.Duration, len(seconds))
	for name, sec := range seconds {
		name = strings.TrimSpace(name)
		if name == "" || sec <= 0 {
			continue
		}
		out[name] = time.Duration(sec) * time.Second
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func applyKnownPluginOverrides(specs []plugin.Spec, workspaceRoot string) []plugin.Spec {
	out := make([]plugin.Spec, len(specs))
	for i, spec := range specs {
		out[i] = plugin.ApplyKnownOverrides(spec, workspaceRoot)
	}
	return out
}

func applyDefaultMCPCallTimeout(specs []plugin.Spec, timeout time.Duration) []plugin.Spec {
	if len(specs) == 0 || timeout <= 0 {
		return specs
	}
	out := make([]plugin.Spec, len(specs))
	for i, spec := range specs {
		out[i] = spec
		if out[i].DefaultCallTimeout <= 0 {
			out[i].DefaultCallTimeout = timeout
		}
	}
	return out
}

func applyPlanModeAllowedMCPToolTrust(specs []plugin.Spec, allowedTools []string) []plugin.Spec {
	if len(specs) == 0 || len(allowedTools) == 0 {
		return specs
	}
	out := make([]plugin.Spec, len(specs))
	for i, spec := range specs {
		out[i] = spec
		prefix := plugin.ToolPrefix(spec.Name)
		clonedModelNames := false
		for _, name := range allowedTools {
			name = strings.TrimSpace(name)
			if !strings.HasPrefix(name, prefix) || len(name) <= len(prefix) {
				continue
			}
			if out[i].ReadOnlyModelToolNames == nil {
				out[i].ReadOnlyModelToolNames = map[string]bool{}
				clonedModelNames = true
			} else if !clonedModelNames {
				out[i].ReadOnlyModelToolNames = cloneBoolMap(spec.ReadOnlyModelToolNames)
				clonedModelNames = true
			}
			out[i].ReadOnlyModelToolNames[name] = true
		}
	}
	return out
}

func trustedRawReadOnlyToolNames(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func planModeTrustedMCPServers(specs map[string]plugin.Spec) map[string]bool {
	if len(specs) == 0 {
		return nil
	}
	out := map[string]bool{}
	for name, spec := range specs {
		if len(spec.ReadOnlyToolNames) > 0 || len(spec.ReadOnlyModelToolNames) > 0 {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// autoShellPrefer reports whether [tools.shell] left the interpreter to
// auto-detection, so the "fell back to PowerShell" hint is suppressed once the
// user has explicitly chosen a shell.
func autoShellPrefer(prefer string) bool {
	p := strings.ToLower(strings.TrimSpace(prefer))
	return p == "" || p == "auto"
}

// MCPStartupNotice formats the warning shown when configured MCP servers failed
// to connect, naming the first few; ok is false when none failed.
func MCPStartupNotice(failures []plugin.Failure) (text, detail string, ok bool) {
	if len(failures) == 0 {
		return "", "", false
	}
	names := make([]string, 0, min(len(failures), 3))
	details := make([]string, 0, len(failures))
	for i, f := range failures {
		if i >= 3 {
			continue
		}
		names = append(names, f.Name)
	}
	for _, f := range failures {
		line := f.Name
		if strings.TrimSpace(f.Error) != "" {
			line += ": " + strings.TrimSpace(f.Error)
		}
		details = append(details, line)
	}
	more := ""
	if len(failures) > len(names) {
		more = fmt.Sprintf(" (+%d more)", len(failures)-len(names))
	}
	return "Some MCP servers failed to start; run /mcp for details.", fmt.Sprintf("%d MCP server(s) failed to start: %s%s\n%s",
		len(failures), strings.Join(names, ", "), more, strings.Join(details, "\n")), true
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

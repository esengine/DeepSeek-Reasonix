package boot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/sidecar"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/mcplaunch"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/pluginspec"
	"reasonix/internal/platform/browser"
	"reasonix/internal/platform/lsp"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/safety/permission"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/script"
)

// builder carries one build's state from stage to stage. Each stage reads
// what the earlier ones settled and adds its own; none reaches back.
type builder struct {
	ctx              context.Context
	opts             Options
	timer            *phaseTimer
	owner            *extension.RuntimeOwner
	fileWriteReceipt func(path string, hadPrior bool, prior []byte)
	stderr           io.Writer
	root             string
	roots            config.Roots
	cfg              *config.Config
	sink             event.Sink
	proxy            netclient.ProxySpec
	ext              *extensionStage
	pendingMgr       *sidecar.Manager // sidecars no RuntimeSet owns yet
	providers        providerStage
	model            modelSelection
	keep             agent.KeepPolicy
	session          sessionRuntime
	balanceClient    *http.Client
	execProv         provider.Provider
	shell            sandbox.Shell
	prompt           promptAssembly
	additionalDirs   []string

	tools   toolStage
	cleanup func()
}

// toolStage is everything the registry was built from, which the controller
// and the extension snapshot both read.
type toolStage struct {
	reg         *tool.Registry
	env         toolEnvironment
	host        *plugin.Host
	specOptions pluginspec.Options
	mcp         mcpSpecPlan
	configSpecs []plugin.Spec
	lsp         *lsp.Manager
	browser     *browser.Session
	maxSteps    int
	policy      permission.Policy
	gate        *control.SharedHeadlessGate
	hooks       []hook.ResolvedHook
	hookRunner  *hook.Runner
	roles       roleWiring
	sub         subagentConfig
	taskTool    *delegation.TaskTool
	skillRun    *skillSubagents
	runners     skillRunners
	cmds        []command.Command
	caps        *capabilitySurface
	candidates  *candidateApproval // nil unless best_of_n is offered
	isolation   *isolationWiring   // nil unless worktree isolation is offered
	// mcpSchemaKnown names the configured servers whose tools were known at
	// registration, the only ones an always-load can show this session.
	mcpSchemaKnown map[string]bool
}

// build is the assembly behind BuildRuntime: it loads config, resolves the
// models, wires the runtime, and freezes the extension snapshot from the
// objects it just assembled. The controller owns plugin subprocesses until
// Controller.Close releases them.
func build(ctx context.Context, opts Options) (*BuildResult, error) {
	b := &builder{timer: newPhaseTimer()}
	// The runtime outlives the request that built it (Studio opens a pane with
	// one), and its MCP servers and sidecars start on that context later.
	b.ctx, b.opts, b.owner, b.fileWriteReceipt = bindRuntimeOwner(context.WithoutCancel(ctx), opts)
	defer b.retireUnownedSidecars()
	if err := b.load(); err != nil {
		return nil, err
	}
	if err := b.wireTools(); err != nil {
		return nil, err
	}
	ctrl, err := b.controller()
	if err != nil {
		return nil, err
	}
	b.tools.candidates.bind(ctrl)
	b.tools.isolation.bind(ctrl)
	return b.freeze(ctrl)
}

// retireUnownedSidecars closes the preflighted sidecars when the build fails
// before the extension snapshot takes ownership: no process outlives a failed build.
func (b *builder) retireUnownedSidecars() {
	if b.pendingMgr != nil {
		close(b.ext.failed)
		_ = b.pendingMgr.Close()
	}
}

// load settles configuration, the extension generation, the executor model,
// the session resources and the system prompt.
func (b *builder) load() error {
	opts := b.opts
	b.stderr = opts.Stderr
	if b.stderr == nil {
		b.stderr = os.Stderr
	}
	b.root, b.roots = resolveWorkspaceRoot(opts.WorkspaceRoot), opts.roots()
	var err error
	if b.additionalDirs, err = normalizeAdditionalDirs(b.root, opts.AdditionalDirs); err != nil {
		return err
	}
	migrations := runConfigMigrations(b.roots, b.root)
	if b.cfg, err = b.roots.LoadForRoot(b.root); err != nil {
		return err
	}
	cfg := b.cfg
	migrations.deepSeekErr = deepSeekProtocolMigrationNoticeError(handleConfigLoadWarnings(opts, cfg, b.stderr), migrations.deepSeekErr)
	// [secrets] is user-global, so arming these package globals before any
	// subprocess can spawn is correct for every concurrent workspace.
	secrets.SetFilterSubprocessEnv(cfg.Secrets.FilterSubprocessEnv)
	secrets.SetProtectSensitiveFiles(cfg.Secrets.ProtectSensitiveFiles)
	secrets.SetProtectCredentialFiles(cfg.Secrets.ProtectCredentialFiles)
	secrets.RegisterCredentialEnvKeys(cfg.CredentialEnvNames())
	// One synchronized sink for every emitter: background jobs and sidecars
	// emit from their own goroutines. The goal tee and the coalescer wrap it
	// here, before the extension hub captures it, so agents emit through both.
	b.sink = control.NewGoalUsageTee(event.Coalesce(quotedSink(cfg, opts), event.DefaultStreamDeltaWindow))

	b.proxy = cfg.NetworkProxySpec()
	if b.ext, err = startExtensions(b.ctx, opts, b.roots, b.root, b.owner, b.sink); err != nil {
		return err
	}
	b.pendingMgr = b.ext.mgr
	b.timer.mark("extensions")
	if b.providers, err = resolveProviders(opts, cfg, b.proxy, b.ext.mgr, b.owner); err != nil {
		return err
	}
	if b.model, err = selectModel(opts, cfg, b.providers.extension); err != nil {
		return err
	}
	b.keep = agentKeepPolicy(cfg.Agent.Keep)

	migrations.report(b.sink, cfg)
	b.timer.mark("config")
	migrateLegacySources(opts, b.sink)
	b.timer.mark("migrations")
	b.reportModelNotices()
	if b.session, err = startSessionRuntime(opts, cfg, b.root, b.sink); err != nil {
		return err
	}
	b.timer.mark("sessions")

	// Validated before the first provider is constructed.
	if err := netclient.Validate(b.proxy); err != nil {
		return err
	}
	if b.balanceClient, err = netclient.NewHTTPClient(b.proxy, netclient.TransportOptions{}); err != nil {
		return err
	}
	if b.execProv, err = resolveProvider(b.providers.effective, cfg, b.proxy, provider.Selection{Ref: b.model.ref, Effort: opts.EffortOverride}); err != nil {
		return err
	}
	b.timer.mark("provider")
	b.shell = sandbox.ResolveShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path, b.stderr)
	b.prompt, err = buildPromptAssembly(b.ctx, opts, cfg, b.root, b.shell, b.sink, b.timer)
	return err
}

func (b *builder) reportModelNotices() {
	cfg, entry := b.cfg, b.model.entry
	if ignored := cfg.IgnoredProjectDefaultModel(); ignored != "" {
		report(b.sink, event.Event{Level: event.LevelWarn, Text: "Ignored the project config's default_model.", Detail: fmt.Sprintf("./reasonix.toml sets default_model = %q but no configured provider serves it; using %q from your user config instead. Edit or remove that default_model line to silence this notice.", ignored, cfg.DefaultModel)})
	}
	// Without RequireKey the UI stays reachable, so a missing key would
	// otherwise surface only as a silently failing first request.
	if !b.opts.RequireKey && entry.RequiresAPIKey() && entry.APIKey() == "" {
		report(b.sink, event.Event{Text: "Selected model is missing its API key.", Detail: fmt.Sprintf("model %q is selected but its API key %s is not set — requests will fail until you set it", b.model.name, entry.APIKeyEnv)})
	}
}

// wireTools builds the registry: built-ins, MCP and LSP, delegation, session,
// skill and slash tools, and the use_capability surface over all of them.
func (b *builder) wireTools() error {
	opts, cfg, root := b.opts, b.cfg, b.root
	t := &b.tools
	t.reg = tool.NewRegistry()
	t.env = resolveToolEnvironment(opts, cfg, b.roots, root, b.additionalDirs, b.shell, b.stderr)
	env := t.env
	// The full inventory registers for use_capability; the provider-visible surface narrows later.
	addBuiltins(t.reg, cfg.Tools.Enabled, env.writeRoots, env.bash, env.bashTimeout, env.search, b.stderr, root, b.proxy, env.forbidReadRoots, env.readPaths, env.sessionGuard, env.managedConfig, opts.FileOverlay, opts.TerminalRunner, env.sessionTemp, b.fileWriteReceipt)
	bindFileViews(t.reg, cfg.Tools.ChangedFilesProtected())
	addSystemOne(t.reg, cfg.Tools.Enabled, cfg, b.balanceClient)
	addAdvisor(t.reg, cfg, b.proxy, b.sink)
	b.addBestOf()
	if cfg.Agent.CodeMode {
		t.reg.Add(script.New())
	}
	b.wireMCP()
	t.browser = bindMachineTools(t.reg, cfg, root, opts.BrowserSession)
	b.timer.mark("mcp")

	t.maxSteps = max(opts.MaxSteps, 0)
	subagentStore, err := newSubagentStore(b.session.dir, opts.SubagentParentLive)
	if err != nil {
		return err
	}
	if subagentStore != nil {
		subagentStore.WithDestroyedChecker(b.session.jobs.IsDestroying)
	}
	// One gate for the executor and every sub-agent, so a child is never a
	// weaker path around the parent. A headless caller always names a mode:
	// Ask fails closed, Auto allows writer fallbacks, DontAsk denies (#6927).
	t.policy = permission.New(cfg.Permissions.Mode, cfg.Permissions.Allow, cfg.Permissions.Ask, cfg.Permissions.Deny).
		WithAllowDynamicBashFallback(cfg.Permissions.AllowDynamicBash).
		WithSessionAllow(opts.PermissionAllow)
	t.gate = control.NewSharedHeadlessGate(t.policy, opts.HeadlessApprovalMode)
	t.hooks, t.hookRunner = loadHooks(opts, b.roots, root, b.shell, b.sink)

	t.roles = roleWiring{cfg: cfg, roots: b.roots, resolver: b.providers.effective, extension: b.providers.extension,
		proxy: b.proxy, sink: b.sink, gate: t.gate, reg: t.reg, keep: b.keep, hooks: t.hookRunner}
	t.sub = newSubagentConfig(opts, cfg, b.model.entry, b.model.name, b.providers.effective, b.proxy, b.prompt.skillStore)
	t.taskTool, t.skillRun = t.roles.delegation(delegationInputs{opts: opts, sub: t.sub, exec: b.execProv, entry: b.model.entry,
		modelName: b.model.name, root: root, maxSteps: t.maxSteps, delivery: b.model.delivery, store: subagentStore,
		session: b.session, bashEnforced: env.bash.Enforce})
	b.addIsolation()
	registerSessionTools(t.reg, opts.Ablation, b.roots, b.session.dir, b.prompt.memory.Store)

	t.runners = skillRunners{readOnly: t.skillRun.runReadOnly, run: t.skillRun.run, profile: skillProfile(cfg)}
	t.cmds = loadCommands(opts, root)
	addInstallSourceTool(b.ctx, t.reg, t.host, root, b.balanceClient, t.specOptions, opts.Stderr)
	registerSkillTools(t.reg, opts.Ablation, b.prompt.skillStore, b.prompt.implicitSkills, t.runners, t.cmds)

	t.caps = newCapabilitySurface(b.ctx, cfg, root, t.specOptions, t.host, t.reg, b.prompt.skillStore, b.model.profile, t.mcp.enabled)
	t.skillRun.capRuntime = t.caps.runtime
	addExtensionTools(t.reg, b.ext.mgr, b.ext.warn)
	t.reg.Add(t.caps.proxy)
	gateSkillsOnCatalog(b.prompt.skillStore, t.caps.catalog)
	return nil
}

// wireMCP registers the MCP servers and LSP tools and chains their shutdown
// into the controller's cleanup. A caller-supplied shared host lets
// controllers for one workspace reuse running MCP processes; its owner closes it.
func (b *builder) wireMCP() {
	opts, cfg, root, t := b.opts, b.cfg, b.root, &b.tools
	t.host = opts.SharedHost
	if t.host == nil {
		t.host = plugin.NewHost()
	}
	// A lazy server connects in the background; without a status sink every
	// status view keeps showing what it saw at boot.
	t.host.SetStatusSink(opts.Sink)
	t.specOptions = pluginspec.Options{
		DefaultStartupTimeout: time.Duration(cfg.MCPStartupTimeoutSeconds()) * time.Second,
		DefaultCallTimeout:    time.Duration(cfg.MCPCallTimeoutSeconds()) * time.Second,
		LaunchManager:         mcplaunch.ForWorkspace(b.roots.Home(), root),
		ConfigSource:          "workspace_config",
		StateHome:             b.roots.Home(),
		WriterRoots:           t.env.writeRoots,
		ForbidReadRoots:       t.env.forbidReadRoots,
		Network:               t.env.network,
		PackageOwners:         pluginspec.PackageOwners(cfg),
		OAuthHTTPClient:       b.balanceClient,
	}
	t.mcp = resolveMCPSpecs(opts, cfg, root, t.specOptions)
	t.configSpecs, t.mcpSchemaKnown = registerMCPTools(b.ctx, t.host, t.reg, t.mcp, b.sink)
	b.cleanup = t.host.Close
	if opts.SharedHost != nil {
		b.cleanup = func() {}
	}
	// The LSP manager is session-scoped: its servers stop with the controller.
	if t.lsp = registerLSP(t.reg, cfg, root); t.lsp != nil {
		prev, mgr := b.cleanup, t.lsp
		b.cleanup = func() { prev(); mgr.Close() }
	}
	if proxy := t.env.egress; proxy != nil {
		prev := b.cleanup
		b.cleanup = func() { prev(); _ = proxy.Close() }
	}
}

// controller builds the executor, the optional planner around it, and the
// controller that drives them, then binds what could only exist after it.
func (b *builder) controller() (*control.Controller, error) {
	t := &b.tools
	executor := b.executor()
	runner, label, err := t.roles.planner(b.opts, executor, b.model.entry.Model, b.prompt.memory.StaticContext(), t.caps.runtime)
	if err != nil {
		return nil, err
	}
	ctrlOpts := b.controllerOptions(runner, executor, label)
	ctrl := withWindowPosture(control.New(ctrlOpts), b.cfg, b.opts.StatsSource)
	b.ext.publish(ctrl)
	// Task and fleet sub-agents share the root agent's recovery checkpoint.
	if t.taskTool != nil {
		if g := ctrl.Executor(); g != nil {
			t.taskTool.WithRecoveryGate(g.RecoveryGate())
		}
	}
	if t.caps.runtime != nil {
		ctrl.SetCapabilityProxyTools(t.caps.runtime.ConnectedProxyTools)
	}
	// The task tool was built before the capability runtime existed.
	if t.taskTool != nil && t.caps.runtime != nil {
		t.taskTool.WithCapabilityRuntime(t.caps.runtime)
	}
	router := t.roles.semanticRouter(t.sub, b.execProv, b.model.ref, b.model.entry.Price, t.caps.audit)
	ctrl.WireCapabilityRouting(b.cfg.Plugins, t.caps.specs, router, t.caps.audit)
	ctrl.SetCapabilityProxyRouting(true)
	// Every role setting sees one provider-visible surface, fixed before the
	// snapshot freezes registry schemas for cache diagnostics.
	applyUnifiedProviderToolSurface(t.reg, b.opts.GoalTurnsUnreachable, b.opts.Ablation, pinnedMCPServers(t.mcp.alwaysLoad, t.mcpSchemaKnown))
	return ctrl, nil
}

func (b *builder) executor() *agent.Agent {
	cfg, entry, t := b.cfg, b.model.entry, &b.tools
	triageProv, triageRef, triagePrice := resolveTriage(cfg, b.model.ref, b.proxy)
	return agent.New(b.execProv, t.reg, newObservedSession(b.prompt.prompt), agent.Options{
		MaxSteps:       t.maxSteps,
		MaxStepsKey:    b.opts.MaxStepsKey,
		Temperature:    cfg.Agent.Temperature,
		TaskBudget:     taskBudgetFromConfig(cfg),
		Pricing:        entry.Price,
		ModelRef:       b.model.ref,
		TriageProvider: triageProv, TriageModelRef: triageRef, TriagePricing: triagePrice,
		ScreenExternalContent: cfg.Agent.ScreenExternalContent,
		Gate:                  t.gate,
		Hooks:                 t.hookRunner,
		Jobs:                  b.session.jobs,
		// Reserving writes at the executor entry covers every writer, late MCP
		// adds included, without wrapping tool schemas.
		WriteScheduler:     t.sub.scheduler,
		WriteWorkspaceRoot: b.root, WorkspaceVCS: b.prompt.workspaceVCS, RenderRoot: renderRoot(t.browser, entry, b.root),
		ProjectChecks: b.prompt.projectChecks, ProjectSensitivePaths: b.prompt.sensitivePaths,
		AgentPreset:                  b.model.preset,
		DeliveryProfile:              b.model.delivery,
		Ablation:                     b.opts.Ablation,
		WorkspaceLease:               b.session.lease,
		CapabilityLedger:             t.caps.ledger,
		CapabilityAudit:              t.caps.audit,
		ContextWindow:                entry.ContextWindow,
		CompactRatio:                 cfg.Agent.CompactRatio,
		ContextEditing:               cfg.Agent.ContextEditing,
		RecentKeep:                   cfg.Agent.RecentKeep,
		CompactionBudgets:            compactionBudgets(cfg),
		ArchiveDir:                   b.roots.ArchiveDir(),
		KeepPolicy:                   b.keep,
		ReasoningLanguage:            cfg.ReasoningLanguage(),
		SubagentDepth:                0,
		MaxSubagentDepth:             t.sub.maxDepth,
		MissingReasoningWarnStateDir: config.MissingReasoningWarnStateDir(),
	}, b.sink)
}

func (b *builder) controllerOptions(runner agent.Runner, executor *agent.Agent, label string) control.Options {
	opts, cfg, root, entry, t := b.opts, b.cfg, b.root, b.model.entry, &b.tools
	specOptions := t.specOptions
	return control.Options{
		TaskBudget:                     taskBudgetFromConfig(cfg),
		GoalTokenBudget:                cfg.Agent.GoalTokenBudget,
		Runner:                         runner,
		Executor:                       executor,
		Sink:                           b.sink,
		Policy:                         t.policy,
		SubagentGate:                   t.gate,
		Label:                          label,
		ModelRef:                       b.model.ref,
		SystemPrompt:                   b.prompt.prompt,
		SessionDir:                     b.session.dir,
		Host:                           t.host,
		Commands:                       t.cmds,
		Skills:                         b.prompt.skills,
		AllSkills:                      b.prompt.allSkills,
		SkillStore:                     b.prompt.skillStore,
		AllSkillStore:                  b.prompt.allSkillStore,
		DisableImplicitSkillInvocation: !b.prompt.implicitSkills,
		SkillRunner:                    t.runners.run,
		ReadOnlySkillRunner:            t.runners.readOnly,
		SkillProfile:                   t.runners.profile,
		Hooks:                          t.hookRunner,
		Memory:                         b.prompt.memory,
		// Read at Close time: freeze chains the extension runtime set onto it.
		Cleanup:               func() { b.cleanup() },
		Balance:               opts.BalanceStore.Cache(b.balanceClient, entry.BalanceURL, entry.APIKey()),
		Jobs:                  b.session.jobs,
		TaskStore:             opts.TaskStore,
		WorkspaceLease:        b.session.lease,
		Registry:              t.reg,
		PluginCtx:             b.ctx,
		MCPDefaultCallTimeout: specOptions.DefaultCallTimeout,
		MCPConfigureSpec: func(spec *plugin.Spec) {
			if spec == nil {
				return
			}
			spec.LaunchManager = specOptions.LaunchManager
			if strings.TrimSpace(spec.ConfigSource) == "" {
				spec.ConfigSource = specOptions.ConfigSource
			}
			if spec.DefaultStartupTimeout <= 0 {
				spec.DefaultStartupTimeout = specOptions.DefaultStartupTimeout
			}
			pluginspec.ApplyIsolation(spec, root, specOptions)
		},
		CapabilityRuntime:      t.caps.runtime,
		WorkspaceRoot:          root,
		ExternalFolderToolRefs: t.env.readPaths,
		ResponseLanguage:       cfg.ResponseLanguage(),
		ReasoningLanguage:      cfg.ReasoningLanguage(),
		DisableColdResumePrune: !cfg.ColdResumePruneEnabled(),
		Shell:                  b.shell,
		ApprovalTimeout:        opts.ApprovalTimeout,
		RuntimeProfile:         b.model.profile,
		Ablation:               opts.Ablation,
		OnRemember: func(rule string) control.RememberResult {
			return rememberPermissionRule(b.roots, root, rule)
		},
		SessionRecoveryMeta: opts.SessionRecoveryMeta,
		OnSessionRecovered:  opts.OnSessionRecovered,
		// Nil without provider-declaring sidecars; otherwise frontends list plugin/... models through it.
		ProviderResolver:  b.providers.extension,
		RuntimeGeneration: b.ext.generation,
		RuntimeOwner:      b.owner,
		// The manager bash and grep were bound to, so tools and controller share one generation.
		SessionTemp:      t.env.sessionTemp,
		BrowserSession:   t.browser,
		Guardian:         t.roles.guardian(),
		RecoveryReviewer: t.roles.recoveryReviewer(b.model.ref),
		// HeadlessApprovalMode declares the frontend has no decision channel;
		// ApprovalTimeout is not a proxy for that.
		RecoveryHeadless: recoveryHeadlessMode(opts),
		GoalEvaluator:    goalEvaluator(cfg, b.model.ref, b.proxy, b.sink),
		PromptRefiner:    promptRefiner(entry, b.proxy, b.sink),
	}
}

// freeze assembles the extension snapshot from the objects this build wired,
// hands the sidecars to its runtime set, and binds the result to the
// controller. The snapshot records the base provider catalog: sidecar
// providers enter through the manager's own contributions.
func (b *builder) freeze(ctrl *control.Controller) (*BuildResult, error) {
	t, ext := &b.tools, b.ext
	snap, runtimeSet, dispatcher, snapErr := assembleLegacySnapshot(b.ctx, legacyAssembly{
		systemPrompt: b.prompt.prompt,
		registry:     t.reg,
		skills:       b.prompt.skills,
		commands:     t.cmds,
		hooks:        t.hooks,
		mcpSpecs:     enabledMCPSpecs(t.configSpecs, t.mcp.extra),
		providers:    b.providers.base.Catalog(),
	}, ext.generation, extensionBoot{
		session:            ext.session(),
		ui:                 ext.hub,
		onWarning:          ext.warn,
		skipPromptStrategy: shouldSkipPromptStrategy(b.opts.PreviousPlan),
		previousDispatcher: b.opts.PreviousDispatcher,
	}, ext.mgr)
	// Assembly owns the sidecars on every path: closed inside, or in the runtime set.
	b.pendingMgr = nil
	extensionMgr, hub := ext.mgr, ext.hub
	if snapErr != nil {
		if extensionContractBroken(snapErr) {
			ctrl.ReleaseResources()
			return nil, fmt.Errorf("boot: %w", snapErr)
		}
		slog.Warn("boot: extension snapshot assembly failed; continuing without a runtime snapshot", "err", snapErr)
		runtimeSet = extension.NewRuntimeSet(ext.generation)
		// The failed assembly already retired the sidecars; bind neither hub nor manager.
		extensionMgr = nil
	}
	providerResolver := b.providers.base
	if b.providers.extension != nil {
		providerResolver = b.providers.extension
	}
	b.cleanup = wireRuntimeScopeCleanup(runtimeSet, b.cleanup, b.opts.SharedHost, t.host, t.lsp, b.opts.SessionTemp)
	ctrl.SetExtensions(dispatcher)
	if extensionMgr == nil {
		hub = nil
	} else {
		ctrl.SetExtensionUI(hub)
	}
	if providerResolver != nil {
		ctrl.SetProviderResolver(providerResolver)
	}
	// A prompt strategy may have replaced the prompt the executor session was
	// built with; swap it in before any turn so session and snapshot agree.
	if snap != nil {
		if final := snap.SystemPrompt(); final != b.prompt.prompt {
			ctrl.ApplyExtensionSystemPrompt(final)
		}
	}
	assembly := &ReusedAssembly{
		SystemPrompt:            b.prompt.prompt,
		Skills:                  b.prompt.skills,
		Commands:                t.cmds,
		Hooks:                   t.hooks,
		Registry:                t.reg,
		ImplicitSkillInvocation: b.prompt.implicitSkills,
		Memory:                  b.prompt.memory,
		ProjectChecks:           b.prompt.projectChecks, ProjectSensitivePaths: b.prompt.sensitivePaths,
	}
	return finalizeBuildResult(b.roots, &BuildResult{Controller: ctrl, Snapshot: snap, Runtime: runtimeSet, Owner: b.owner, Extensions: extensionMgr, Dispatcher: dispatcher, ExtensionUI: hub, ProviderResolver: providerResolver, BaseProviderResolver: b.providers.base, Assembly: assembly, Phases: b.timer.done("assemble")}, !b.opts.deferPublish), nil
}

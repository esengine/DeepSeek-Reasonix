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
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/builtinmcp"
	"reasonix/internal/codegraph"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/history"
	"reasonix/internal/hook"
	"reasonix/internal/installsource"
	"reasonix/internal/instruction"
	"reasonix/internal/jobs"
	"reasonix/internal/lsp"
	"reasonix/internal/memory"
	"reasonix/internal/netclient"
	"reasonix/internal/outputstyle"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

// ErrUnknownModel is returned by Build when the configured model can't be
// resolved to a provider — e.g. a default_model left over from a renamed or
// removed provider. Callers can detect it (errors.Is) to re-run setup.
var ErrUnknownModel = errors.New("unknown model")

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
	// SessionDir overrides where persisted chat transcripts are written. When
	// empty, the shared CLI/global session directory is used.
	SessionDir string
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
	migrated, migErr := config.MigrateLegacyIfNeeded()
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return nil, err
	}
	modelName := opts.Model
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(modelName)
	if !ok {
		return nil, fmt.Errorf("%w %q (configured: %s); note: defining [[providers]] replaces the built-in presets, so add a [[providers]] entry for it or use a configured name, or run `reasonix setup` to reconfigure", ErrUnknownModel, modelName, providerNames(cfg))
	}
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

	if migErr != nil {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "config migration from ~/.reasonix failed: " + migErr.Error()})
	} else if migrated != nil {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: migrated.Notice()})
	}
	migrateLegacySessionSources(sink)

	// A resolvable model whose API key env is unset would otherwise build fine
	// (RequireKey is false so the UI stays reachable) and then fail silently on the
	// first request, showing as an empty/dead model. Surface the cause up front.
	if !opts.RequireKey && entry.APIKeyEnv != "" && entry.APIKey() == "" {
		sink.Emit(event.Event{Kind: event.Notice, Text: fmt.Sprintf("model %q is selected but its API key %s is not set — requests will fail until you set it", modelName, entry.APIKeyEnv)})
	}
	jm := jobs.NewManager(sink)

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
	sysPrompt += "\n\n" + config.LanguagePolicy

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
	sysPrompt = skill.ApplyIndex(sysPrompt, skills)

	sessionDir := opts.SessionDir
	if sessionDir == "" {
		sessionDir = config.SessionDir()
	}

	reg := tool.NewRegistry()
	bashSpec := sandbox.Spec{Mode: cfg.BashMode(), WriteRoots: cfg.WriteRootsForRoot(root), Network: cfg.Sandbox.Network}
	shell := sandbox.ResolveShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path, stderr)
	bashSpec.Shell = shell
	if bashSpec.Mode == "enforce" && !sandbox.Available() {
		fmt.Fprintln(stderr, "warning: bash sandbox requested but unavailable on this platform; running bash unconfined")
	}
	if autoShellPrefer(cfg.Tools.Shell.Prefer) && shell.Kind == sandbox.ShellPowerShell {
		fmt.Fprintln(stderr, "warning: bash not found on PATH; the shell tool will run commands under Windows PowerShell. Install Git for Windows or WSL to use bash, or set [tools.shell] prefer=\"powershell\" to silence this.")
	}
	searchSpec := builtin.ResolveSearch(cfg.Tools.Search.Engine, cfg.Tools.Search.RgPath, stderr)
	bashTimeout := time.Duration(cfg.BashTimeoutSeconds()) * time.Second
	addBuiltins(reg, cfg.Tools.Enabled, cfg.WriteRootsForRoot(root), bashSpec, bashTimeout, searchSpec, stderr, root, proxySpec)
	// Always construct a host, even with no plugins configured, so the controller's
	// host pointer is stable for the session and `/mcp add` can hot-add into it.
	pluginHost := plugin.NewHost()

	// Partition configured plugins by tier so eager/lazy/background can each
	// take the path that fits them. User entries default to background: the
	// session starts immediately while enabled MCP servers warm up.
	autoStartEntries := builtinmcp.AppendEnabled(cfg.AutoStartPlugins(), cfg.Plugins, cfg.BuiltInMCP.EnabledNames(), pluginSpecNames(opts.ExtraPlugins)...)
	eagerEntries, lazyEntries, bgEntries := partitionByTier(autoStartEntries)

	// Auto-demote: any eager plugin that has been chronically slow (recent
	// samples repeatedly hit the blocking startup budget) drops to lazy
	// for this session. The user keeps eager intent, just doesn't pay for it
	// on a server that's been misbehaving. A notice surfaces the demotion.
	var demoteMessages []string
	budget := plugin.DefaultStartupBudget()
	kept := eagerEntries[:0]
	for _, e := range eagerEntries {
		rec := plugin.Recommend(e.Name, budget, 0)
		if rec.Demote {
			demoteMessages = append(demoteMessages, rec.Reason)
			lazyEntries = append(lazyEntries, e)
			continue
		}
		kept = append(kept, e)
	}
	eagerEntries = kept

	eagerSpecs := PluginSpecs(eagerEntries)
	lazySpecs := PluginSpecs(lazyEntries)
	bgSpecs := PluginSpecs(bgEntries)

	// CodeGraph is a built-in MCP server fetched on first use. When it resolves,
	// inject it as one more stdio plugin pinned to the project root (it is
	// cwd-aware); EnsureInit only creates .codegraph/ (fast, size-independent),
	// serve's daemon then indexes in the background, so startup never blocks even
	// on a large repo. When it is not yet installed, fetch it in the background
	// (one-time, ~45MB) if auto_install is on — startup still never blocks, the
	// tools come online next session — otherwise point the user at the explicit
	// install command. A failed init or fetch is a notice, not fatal.
	//
	// CodeGraph is fixed to background startup. Legacy tier values are ignored so
	// enabling it never blocks chat startup.
	if cfg.Codegraph.Enabled {
		bin, ok := codegraph.Resolve(cfg.Codegraph.Path)
		switch {
		case ok && !codegraph.IndexableRoot(root):
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
				Text: "codegraph: project root is a filesystem root — skipped to avoid indexing the whole volume"})
		case ok:
			spec := plugin.Spec{
				Name:              "codegraph",
				StripRawPrefix:    "codegraph_",
				Command:           bin,
				Args:              []string{"serve", "--mcp"},
				Dir:               root,
				ReadOnlyToolNames: codegraph.ReadOnlyToolNames(),
				// The daemon walks and indexes the whole tree; below-normal
				// priority keeps it from starving the user's machine (#3797).
				LowPriority: true,
			}
			warm := codegraph.Initialized(root)
			if err := codegraph.EnsureInit(ctx, bin, root); err != nil {
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
					Text: "codegraph: init failed (" + err.Error() + ") — symbol-graph tools disabled this session"})
				break
			}
			bgNotice := func() {
				if !warm {
					sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
						Text: "codegraph: preparing code-intelligence tools in the background — tools will appear when ready"})
				}
			}
			bgSpecs = append(bgSpecs, spec)
			bgNotice()
		case cfg.Codegraph.AutoInstall:
			notify := func(msg string) { sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: msg}) }
			notify("codegraph: fetching code-intelligence runtime in the background (one-time) — symbol-graph tools available next session")
			codegraphClient, err := netclient.NewHTTPClient(proxySpec, netclient.TransportOptions{})
			if err != nil {
				notify("codegraph: install skipped (" + err.Error() + ")")
			} else {
				go func() {
					if _, err := codegraph.InstallWithClient(context.WithoutCancel(ctx), codegraphClient, nil); err != nil {
						notify("codegraph: install failed (" + err.Error() + ") — using grep/glob; retries next session")
					} else {
						notify("codegraph: installed — symbol-graph tools available next session")
					}
				}()
			}
		default:
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
				Text: "codegraph: not installed — run `reasonix codegraph install` to enable symbol-graph tools"})
		}
	}
	eagerSpecs = append(eagerSpecs, opts.ExtraPlugins...)

	// Apply caller-supplied stderr override to every spec across tiers.
	if opts.Stderr != nil {
		for i := range eagerSpecs {
			eagerSpecs[i].Stderr = opts.Stderr
		}
		for i := range lazySpecs {
			lazySpecs[i].Stderr = opts.Stderr
		}
		for i := range bgSpecs {
			bgSpecs[i].Stderr = opts.Stderr
		}
	}

	// Eager: block until handshake. Failures show up in /mcp.
	if len(eagerSpecs) > 0 {
		host, ptools := plugin.StartAvailable(ctx, eagerSpecs)
		pluginHost = host
		for _, t := range ptools {
			reg.Add(t)
		}
		// PhaseB (prompts + resources) runs on the boot ctx — which is the
		// controller's session-scoped PluginCtx — so the auxiliary surfaces
		// keep streaming in after Start returns without holding up the agent.
		go host.StartPhaseB(ctx, sink)
		if text, ok := MCPStartupNotice(host.Failures()); ok {
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text})
		}
	}

	// Lazy / background: register placeholder tools now; the real spawn waits
	// for either the first model call (lazy) or a goroutine kicked off here
	// (background). Both share the same pluginHost so /mcp status, hot-add,
	// and Close see one cohesive set of servers regardless of tier.
	registerDeferred := func(specs []plugin.Spec, kick bool) {
		for _, s := range specs {
			cs, _ := plugin.LoadCachedSchema(s.Name, plugin.SpecFingerprint(s))
			for _, t := range plugin.LazyToolset(s, cs, pluginHost, reg, ctx, kick) {
				reg.Add(t)
			}
		}
	}
	registerDeferred(lazySpecs, false)
	registerDeferred(bgSpecs, true)

	// Inject codegraph steering into the system prompt when symbol-graph tools
	// are available, so the model knows to prefer them for architecture / call-graph
	// questions over grep/read_file. Also register codegraph tool names in the
	// subagent allowed-tools list so explore/research/review can use them.
	if cfg.Codegraph.Enabled {
		prefix := plugin.ToolPrefix("codegraph")
		var cgTools []string
		for _, name := range reg.Names() {
			if strings.HasPrefix(name, prefix) {
				cgTools = append(cgTools, name)
			}
		}
		if len(cgTools) > 0 {
			sysPrompt += "\n\n" + codegraph.SteerText
			skill.SetExtraReadTools(cgTools)
		}
	}

	for _, msg := range demoteMessages {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: msg})
	}

	cleanup := pluginHost.Close

	// LSP tools resolve their servers on PATH and spawn lazily on first query, so
	// registering them is cheap even when no server is installed (a query then
	// returns an install hint). The manager is session-scoped; chain its shutdown
	// into the controller's cleanup so servers stop with the session, not the turn.
	if cfg.LSP.Enabled {
		lspMgr := lsp.NewManager(root, LSPSpecs(cfg.LSP))
		for _, t := range lsp.Tools(lspMgr) {
			reg.Add(t)
		}
		prev := cleanup
		cleanup = func() { prev(); lspMgr.Close() }
	}

	maxSteps := cfg.Agent.MaxSteps
	if opts.MaxSteps > 0 {
		maxSteps = opts.MaxSteps
	}
	subagentStore := newSubagentStore(config.SessionDir())

	// Permission policy gates every tool call. The headless gate (no Approver)
	// resolves "ask" to allow — preserving `reasonix run` autonomy — while deny
	// rules hard-block in every mode. Interactive frontends (chat, desktop) swap
	// in an interactive gate later via Controller.EnableInteractiveApproval.
	// Sub-agents always run headless: they have no UI to answer a prompt, so they
	// inherit this same gate.
	policy := permission.New(cfg.Permissions.Mode, cfg.Permissions.Allow, cfg.Permissions.Ask, cfg.Permissions.Deny)
	headlessGate := permission.NewGate(policy, nil)

	// Hooks: load the global settings.json plus the project's (only when trusted —
	// project hooks run arbitrary shell commands, so cloning a repo must not
	// silently execute them). Non-blocking hook output is surfaced to the user as
	// a Notice through the shared sink. The runner fires PreToolUse/PostToolUse in
	// the agent loop and UserPromptSubmit/Stop at the controller's turn boundary.
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
	reg.Add(agent.NewTaskTool(execProv, entry.Price, reg, maxSteps,
		entry.ContextWindow, cfg.Agent.SoftCompactRatio, cfg.Agent.CompactRatio, cfg.Agent.CompactForceRatio,
		cfg.Agent.Temperature, config.ArchiveDir(), "", headlessGate,
		taskModel, taskEffort, resolveSubagentProvider).
		WithTranscripts(subagentStore, root, modelName, entry.Effort).
		WithTranscriptIdentityResolver(subagentIdentity))

	// The `memory` tool searches/reads saved facts on demand; `remember` persists
	// durable facts to the project's auto-memory store; `forget` prunes ones that
	// turn out wrong. The saved index loads into the prefix on the next session.
	reg.Add(history.NewTool(history.Options{SessionDir: sessionDir, GlobalSessionDir: config.SessionDir(), ArchiveDir: config.ArchiveDir()}))
	reg.Add(memory.NewRecallTool(mem.Store))
	reg.Add(memory.NewRememberTool(mem.Store))
	reg.Add(memory.NewForgetTool(mem.Store))

	// The `ask` tool puts structured multiple-choice questions to the user. It
	// reaches them through the Asker on the call context, which interactive
	// frontends wire to the controller (EnableInteractiveApproval); a headless run
	// has none, so ask resolves to "decide for yourself".
	reg.Add(agent.NewAskTool())

	// Skill tools: run_skill / install_skill plus the dedicated subagent wrappers
	// (explore / research / review / security_review). A subagent skill reuses the
	// sub-agent machinery via this runner — an isolated loop with the skill body
	// as system prompt, a tool set scoped to the skill's allowed-tools (minus the
	// task/skill meta-tools, to bar recursion), and an optional per-skill model.
	// Its tool activity nests under the invoking call, like `task`.
	skillRunner := func(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (string, error) {
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
		subReg := agent.FilterRegistry(reg, sk.AllowedTools, agent.SubagentMetaTools()...)
		continueFrom, forkFrom := strings.TrimSpace(runOpts.ContinueFrom), strings.TrimSpace(runOpts.ForkFrom)
		if continueFrom != "" && forkFrom != "" {
			return "", fmt.Errorf("continue_from and fork_from are mutually exclusive")
		}
		parentID, _, _, _ := agent.CallContext(sctx)
		parentSession := agent.ParentSession(sctx)
		var run *agent.SubagentRun
		if subagentStore == nil || parentSession == "" {
			// Headless runs (e.g. `reasonix run`) have no persistent session to
			// own a transcript. Run the skill sub-agent ephemerally, as before
			// persisted transcripts existed, instead of failing. Continuation and
			// fork need a persisted owner, so they error here.
			if continueFrom != "" || forkFrom != "" {
				return "", fmt.Errorf("continue_from/fork_from require a persisted session; none is active in this run")
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
			switch {
			case continueFrom != "":
				run, prepErr = subagentStore.PrepareContinue(continueFrom, spec)
			case forkFrom != "":
				run, prepErr = subagentStore.PrepareFork(forkFrom, spec)
			default:
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
			MaxSteps:      steps,
			Temperature:   cfg.Agent.Temperature,
			Pricing:       price,
			Gate:          headlessGate,
			ContextWindow: ctxWin,
			ArchiveDir:    config.ArchiveDir(),
		}, agent.NestedSink(sctx, event.Discard))
		if err != nil {
			return "", errors.Join(err, subagentStore.SaveFailed(run))
		}
		if err := subagentStore.SaveCompleted(run); err != nil {
			return "", errors.Join(err, subagentStore.SaveFailed(run))
		}
		return agent.FormatSubagentResult(answer, run.Ref, false), nil
	}
	skillProfile := func(sk skill.Skill) *event.Profile {
		model, effort := subagentModelRef(cfg, sk), subagentEffortRef(cfg, sk)
		if model == "" && effort == "" {
			return nil
		}
		return &event.Profile{Model: model, Effort: effort}
	}
	reg.Add(skill.NewRunSkillTool(skillStore, skillRunner, skillProfile))
	reg.Add(skill.NewReadSkillTool(skillStore))
	reg.Add(skill.NewInstallSkillTool(skillStore, nil))
	reg.Add(installsource.NewTool(installsource.Options{
		ProjectRoot: root,
		HTTPClient:  balanceClient,
		ConnectMCP: func(e config.PluginEntry) (installsource.MCPConnectResult, error) {
			exp := e.ExpandedPlugin()
			spec := plugin.Spec{
				Name:    exp.Name,
				Type:    exp.Type,
				Command: exp.Command,
				Args:    exp.Args,
				Env:     exp.Env,
				URL:     exp.URL,
				Headers: exp.Headers,
			}
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
	for _, t := range skill.BuiltinSubagentTools(skillStore, skillRunner, skillProfile) {
		reg.Add(t)
	}

	execSess := agent.NewSession(sysPrompt)
	executor := agent.New(execProv, reg, execSess, agent.Options{
		MaxSteps:          maxSteps,
		Temperature:       cfg.Agent.Temperature,
		Pricing:           entry.Price,
		Gate:              headlessGate,
		Hooks:             hookRunner,
		Jobs:              jm,
		ProjectChecks:     projectChecks,
		ContextWindow:     entry.ContextWindow,
		SoftCompactRatio:  cfg.Agent.SoftCompactRatio,
		CompactRatio:      cfg.Agent.CompactRatio,
		CompactForceRatio: cfg.Agent.CompactForceRatio,
		ArchiveDir:        config.ArchiveDir(),
	}, sink)

	// Custom slash commands (.reasonix/commands + user dir). Best-effort: a malformed
	// file is skipped, and a load error never blocks the session.
	cmds, _ := command.Load(config.CommandDirsForRoot(root)...)

	// Expose the loaded slash commands (skills + custom commands) to the model via
	// the slash_command tool, so it can invoke a project playbook by name the way a
	// user types "/name". Skills are added first, then commands, so a command wins
	// a name clash — matching the prompt's command-over-skill precedence.
	var slashEntries []command.SlashEntry
	for _, sk := range skills {
		sk := sk
		slashEntries = append(slashEntries, command.SlashEntry{
			Name:        sk.Name,
			Description: sk.Description,
			Render:      func(args []string) string { return skill.Render(sk, strings.Join(args, " ")) },
		})
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

	var runner agent.Runner = executor
	label := entry.Model
	var classifier *control.ProviderAutoPlanClassifier

	// Two-model collaboration: a distinct planner_model wraps the executor in a
	// Coordinator with its own session, kept separate for cache stability. The
	// planner gets the same standing memory context and a filtered read-only
	// research tool set, so it can inspect rules/code without side effects.
	if pm := cfg.Agent.PlannerModel; pm != "" {
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
				MaxSteps:          cfg.Agent.PlannerMaxSteps,
				MaxStepsKey:       "agent.planner_max_steps",
				Gate:              headlessGate,
				ContextWindow:     pe.ContextWindow,
				SoftCompactRatio:  cfg.Agent.SoftCompactRatio,
				CompactRatio:      cfg.Agent.CompactRatio,
				CompactForceRatio: cfg.Agent.CompactForceRatio,
				ArchiveDir:        config.ArchiveDir(),
			}, executor, cfg.Agent.Temperature, sink, control.TaskWarrantsPlanner)
			label = entry.Model + " + planner " + pe.Model
		}
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Agent.AutoPlan), "off") && cfg.Agent.AutoPlanClassifier != "" {
		cm := cfg.Agent.AutoPlanClassifier
		ce, ok := cfg.ResolveModel(cm)
		if !ok {
			return nil, fmt.Errorf("auto_plan_classifier %q is not a configured provider", cm)
		}
		classifierProv, err := NewProviderWithProxy(ce, proxySpec)
		if err != nil {
			return nil, fmt.Errorf("auto_plan_classifier %q: %w", cm, err)
		}
		classifier = control.NewProviderAutoPlanClassifier(classifierProv)
	}

	ctrlOpts := control.Options{
		Runner:        runner,
		Executor:      executor,
		Sink:          sink,
		Policy:        policy,
		Label:         label,
		SystemPrompt:  sysPrompt,
		SessionDir:    sessionDir,
		Host:          pluginHost,
		Commands:      cmds,
		Skills:        skills,
		AllSkills:     allSkills,
		SkillStore:    skillStore,
		AllSkillStore: allSkillStore,
		Hooks:         hookRunner,
		Memory:        mem,
		Cleanup:       cleanup,
		BalanceURL:    entry.BalanceURL,
		BalanceKey:    entry.APIKey(),
		BalanceClient: balanceClient,
		Jobs:          jm,
		Registry:      reg,
		PluginCtx:     ctx,
		WorkspaceRoot: root,
		AutoPlan:      cfg.Agent.AutoPlan,
		Shell:         shell,
		OnRemember: func(rule string) control.RememberResult {
			return rememberPermissionRule(root, rule)
		},
	}
	if classifier != nil {
		ctrlOpts.Classifier = classifier
	}
	return control.New(ctrlOpts), nil
}

func migrateLegacySessionSources(sink event.Sink) {
	dest := config.SessionDir()
	if strings.TrimSpace(dest) == "" {
		return
	}
	type legacySource struct {
		dir     string
		label   string
		migrate func(srcDir, globalDest string, projectDir func(string) string) (int, error)
	}
	var sources []legacySource
	if home, herr := os.UserHomeDir(); herr == nil {
		sources = append(sources, legacySource{
			dir:     filepath.Join(home, ".reasonix", "sessions"),
			label:   "~/.reasonix/sessions",
			migrate: agent.MigrateLegacySessions,
		})
	}
	// Back-fill v0.x sessions from the current user config session directory as
	// well. This covers users whose platform config root was redirected before the
	// Go rewrite; their event logs can already live where v2 stores sessions.
	sources = append(sources, legacySource{
		dir:     dest,
		label:   dest,
		migrate: agent.MigrateLegacySessionsFromConfigDir,
	})

	seen := map[string]bool{}
	for _, src := range sources {
		if strings.TrimSpace(src.dir) == "" {
			continue
		}
		key := filepath.Clean(src.dir)
		if seen[key] {
			continue
		}
		seen[key] = true
		if n, serr := src.migrate(src.dir, dest, config.ProjectSessionDir); serr == nil && n > 0 {
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf("imported %d past session(s) from %s — resume them with --resume or the history panel", n, src.label)})
		}
	}
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
	if err := edit.SaveTo(path); err != nil {
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

func newSubagentStore(sessionDir string) *agent.SubagentStore {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return nil
	}
	return agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
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
			"thinking":           e.Thinking,
			"effort":             config.EffectiveEffort(e),
			"reasoning_protocol": config.ReasoningProtocolForEntry(e),
			"proxy_spec":         proxy,
		},
	})
}

// addBuiltins adds enabled built-in tools to reg. An empty list means all of
// them. writeRoots confines the file-writing built-ins to the workspace: after
// the (unconfined) defaults are added, each enabled writer is replaced by an
// instance bound to writeRoots (preserving registry order).
// When workDir is non-empty, tools resolve relative paths against it instead of
// the process cwd, enabling concurrent multi-project sessions.
func addBuiltins(reg *tool.Registry, enabled, writeRoots []string, bashSpec sandbox.Spec, bashTimeout time.Duration, searchSpec builtin.SearchSpec, stderr io.Writer, workDir string, proxySpec netclient.ProxySpec) {
	// If a workspace directory is set, use workspace-bound tools that resolve
	// paths relative to that directory. Otherwise fall back to the process-cwd
	// compile-time builtins.
	if workDir != "" {
		ws := builtin.Workspace{Dir: workDir, WriteRoots: writeRoots, Bash: bashSpec, BashTimeout: bashTimeout, Search: searchSpec, ProxySpec: proxySpec}
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
	// preserved on replace): file-writers bound to the workspace, bash to the OS
	// sandbox, web_fetch to the proxy. Only replace tools actually enabled/present.
	confined := append(builtin.ConfineWriters(writeRoots), builtin.ConfineBash(bashSpec, bashTimeout), builtin.ConfineSearch(searchSpec), builtin.ConfineWebFetch(proxySpec))
	for _, t := range confined {
		if _, ok := reg.Get(t.Name()); ok {
			reg.Add(t)
		}
	}
}

// partitionByTier splits configured plugin entries into the three startup
// buckets — eager (block boot until ready), lazy (placeholder until first
// model use), background (placeholder + start spawn now). Entries with an
// empty tier land in background; unrecognised non-empty tiers land in lazy so a
// typo never triggers unexpected background work.
func partitionByTier(entries []config.PluginEntry) (eager, lazy, bg []config.PluginEntry) {
	for _, e := range entries {
		switch e.ResolvedTier() {
		case "eager":
			eager = append(eager, e)
		case "background":
			bg = append(bg, e)
		default:
			lazy = append(lazy, e)
		}
	}
	return eager, lazy, bg
}

// PluginSpecs maps configured plugin entries to plugin.Spec, expanding ${VAR}
// references. Exported so custom assemblers can connect the config's plugins
// alongside their own (e.g. ACP's per-session MCP servers).
func PluginSpecs(entries []config.PluginEntry) []plugin.Spec {
	specs := make([]plugin.Spec, len(entries))
	for i, e := range entries {
		e = e.ExpandedPlugin() // resolve ${VAR} / ${VAR:-default} from the environment
		specs[i] = plugin.Spec{
			Name:    e.Name,
			Type:    e.Type,
			Command: e.Command,
			Args:    e.Args,
			Env:     e.Env,
			URL:     e.URL,
			Headers: e.Headers,
		}
	}
	return specs
}

func pluginSpecNames(specs []plugin.Spec) []string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
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
func MCPStartupNotice(failures []plugin.Failure) (text string, ok bool) {
	if len(failures) == 0 {
		return "", false
	}
	names := make([]string, 0, min(len(failures), 3))
	for i, f := range failures {
		if i >= 3 {
			break
		}
		names = append(names, f.Name)
	}
	more := ""
	if len(failures) > len(names) {
		more = fmt.Sprintf(" (+%d more)", len(failures)-len(names))
	}
	return fmt.Sprintf("%d MCP server(s) failed to start: %s%s — run /mcp for details",
		len(failures), strings.Join(names, ", "), more), true
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

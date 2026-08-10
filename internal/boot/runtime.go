package boot

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/extension/uihub"
	"reasonix/internal/hook"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// BuildResult is the full product of one runtime build: the ready-to-drive
// controller plus the extension kernel's frozen view of the same assembly.
// Snapshot is nil when kernel assembly failed — boot behavior is never
// allowed to depend on it (see build). Runtime is the snapshot's bound
// closable set; it holds the extension sidecar Manager when any v2 runtime
// package is installed, and its Close is chained into the controller's
// cleanup so sidecars die with their controller generation.
type BuildResult struct {
	Controller *control.Controller
	Snapshot   *extension.RuntimeSnapshot
	Runtime    *extension.RuntimeSet
	// Owner is the session-lineage lifecycle owner. Independent builds receive
	// independent owners; RebuildFrom reuses the previous owner so only that
	// lineage's old generation drains.
	Owner *extension.RuntimeOwner
	// Extensions is the started extension sidecar Manager (nil when no v2
	// runtime package is installed). Its lifecycle belongs to Runtime; the
	// field exists so later stages (and tests) can reach the live clients.
	Extensions *sidecar.Manager
	// Dispatcher is the frozen interceptor dispatcher for this runtime
	// generation (nil when no sidecar started, or when snapshot assembly
	// degraded). It is immutable and safe for concurrent turns; the
	// controller receives it through SetExtensions right after assembly.
	Dispatcher *dispatch.Dispatcher
	// ExtensionUI is the host extension UI hub for this runtime generation
	// (stage 8a; nil when no sidecar started). It is bound to the build's
	// session ID and generation; the controller receives it through
	// SetExtensionUI right after assembly, and a Rebuild creates a fresh hub
	// on the new generation.
	ExtensionUI *uihub.Hub
	// ProviderResolver is the build's effective provider resolver: the
	// caller-owned broker when Options.ProviderResolver is set, the local
	// config-backed resolver otherwise, merged with any extension-hosted
	// sidecar providers (stage 7). Plugin-namespaced refs route to the owning
	// sidecar; every other ref resolves through the base resolver.
	ProviderResolver provider.Resolver
	// BaseProviderResolver is the pre-sidecar catalog used to re-merge after
	// a narrow rebuild replaces the Manager (must not re-merge already-merged).
	BaseProviderResolver provider.Resolver
	// Plan is the RuntimePlan for this generation (from the previous graph
	// when Rebuild supplies one; cold start when nil previous).
	Plan *extension.RuntimePlan
	// Status is the diagnostic component status snapshot for doctor/UI.
	Status *extension.RuntimeStatus
	// Lifecycle tracks component state transitions for this generation.
	Lifecycle *extension.LifecycleRegistry
	// Assembly is retained so a subsequent RebuildFrom can skip rediscovery
	// when the RuntimePlan is no-op or interceptor/UI-only.
	Assembly *ReusedAssembly
	// ReusedController is true when a true subgraph rebuild kept the previous
	// controller pointer (no control.New / BuildRuntime). Callers must not
	// Close the "old" controller when it is the same pointer as Controller.
	ReusedController bool
}

// runtimeGeneration is the process-wide build generation counter. The first
// build gets generation 1 so 0 can mean "no snapshot" on a RuntimeSet built
// outside the kernel pipeline.
var runtimeGeneration atomic.Uint64

// nextRuntimeGeneration returns the next build generation. Generations pair
// with RuntimeSet.CloseIfGeneration so stale cleanup can never close a newer
// runtime's resources.
func nextRuntimeGeneration() uint64 { return runtimeGeneration.Add(1) }

// BuildRuntime runs the full boot assembly and returns the controller
// together with the extension kernel's frozen snapshot of the exact resources
// the build wired — tools, skills, commands, hooks, MCP servers, providers,
// and the composed system prompt. The snapshot is assembled from the in-hand
// objects the build itself produced (discovery never re-runs), so it cannot
// drift from what the controller actually uses, and it never makes an
// otherwise-successful build fail: an assembly error degrades to a nil
// Snapshot with a logged warning.
func BuildRuntime(ctx context.Context, opts Options) (*BuildResult, error) {
	return build(ctx, opts)
}

// Build loads config, resolves the model(s), and returns a Controller wrapping a
// single Agent, or a two-model Coordinator when agent.planner_model is set. The
// returned controller owns plugin subprocesses; call Close (via Controller.Close)
// to release them.
//
// Build is the compatibility wrapper over BuildRuntime: frontends keep their
// existing signature. The runtime set is NOT closed here — it is chained into
// the controller's cleanup (the way LSP cleanup is chained), so extension
// sidecars live exactly as long as their controller.
func Build(ctx context.Context, opts Options) (*control.Controller, error) {
	res, err := BuildRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	return res.Controller, nil
}

// legacyAssembly carries the already-assembled runtime resources the kernel
// snapshot is built from. Every field is the exact object the rest of the
// build wired into the controller — the snapshot never re-derives anything.
type legacyAssembly struct {
	systemPrompt string
	registry     *tool.Registry
	skills       []skill.Skill
	commands     []command.Command
	hooks        []hook.ResolvedHook
	mcpSpecs     []plugin.Spec
	providers    []provider.Descriptor
}

// extensionBoot carries the extension sidecar launch inputs into snapshot
// assembly: the session the sidecars serve, where non-fatal warnings go, and
// (stage 8a) the UI hub the sidecars' host/ui/* calls bind to.
type extensionBoot struct {
	session   protocol.SessionContext
	onWarning func(string)
	ui        *uihub.Hub
	// skipPromptStrategy skips system_prompt.build strategy when the RuntimePlan
	// is a no-op (or does not affect cache), preserving the previous prompt.
	skipPromptStrategy bool
	// previousDispatcher reuses an interceptor chain when the plan is no-op.
	previousDispatcher *dispatch.Dispatcher
}

func (p extensionBoot) warn(msg string) {
	if p.onWarning != nil {
		p.onWarning(msg)
	}
}

// startExtensionPackages is the sidecar launch seam (tests override it).
// previous+plan adopt Unchanged packages without respawn.
var startExtensionPackages = sidecar.StartPackagesWithPlan

// preflightExtensionRuntimes starts enabled runtime packages before model
// resolution. Required failures are fatal; optional failures warn. With
// prev+plan, only Added/Reloaded start and Unchanged are adopted.
func preflightExtensionRuntimes(ctx context.Context, home string, ext extensionBoot, prev *sidecar.Manager, plan *extension.RuntimePlan) (*sidecar.Manager, error) {
	if strings.TrimSpace(home) == "" {
		return nil, nil
	}
	packages, loadWarnings := sidecar.LoadRuntimePackages(home)
	if len(packages) == 0 {
		// No v2 runtime packages: surface the installed-state warnings exactly
		// as the in-assembly StartPackages call did, and take the untouched
		// pre-sidecar path (no processes, no Manager).
		for _, warning := range loadWarnings {
			ext.warn(warning)
		}
		return nil, nil
	}
	// The stage-8a UI hub installs as each sidecar's host/ui/* handler; a nil
	// hub keeps the "ui not available" default. Guard against the typed-nil
	// trap: a nil *uihub.Hub must stay a nil UIHandler.
	var ui sidecar.UIHandler
	if ext.ui != nil {
		ui = ext.ui
	}
	mgr, warnings, err := startExtensionPackages(ctx, home, ext.session, ui, prev, plan)
	for _, warning := range warnings {
		ext.warn(warning)
	}
	if err != nil {
		// A required runtime failed: the Manager already shut down whatever
		// started. The call site treats *RequiredStartError as fatal.
		return nil, err
	}
	if len(mgr.Clients()) == 0 {
		// Every package was optional and failed: retire the empty Manager and
		// take the pre-sidecar path with warnings already surfaced.
		_ = mgr.Close()
		return nil, nil
	}
	return mgr, nil
}

// assembleLegacySnapshot freezes one boot's resources into an extension
// kernel snapshot. ConflictCollect is deliberate: the legacy sources already
// resolved their clashes inside their own discovery passes, and any residual
// dispute must land on the snapshot's Diagnostics without changing whether
// the session boots.
//
// Extension sidecars (stage 5b): mgr is the Manager preflightExtensionRuntimes
// started before model resolution; this function takes OVER its ownership —
// on every error path it closes mgr, and on success the Manager is registered
// into the RuntimeSet at activation, so it dies with its controller
// generation. Starting before the builder freezes is load-bearing: the
// handshake's declared providers and UI actions can only enter the catalog
// from a live handshake, and required-runtime failures must fail the build,
// which the activator seam (post-freeze) could only do after the catalog
// already settled.
//
// Dispatch wiring (stage 6b1): with live sidecars the boot also runs the
// system_prompt.build strategy BEFORE the freeze, so the frozen snapshot's
// SystemPrompt and CacheHash cover the final (possibly replaced) prompt —
// the hash honestly attributes the prompt the session was built with rather
// than a pre-strategy draft. A strategy failure fails the build because the
// slot owner is required-class by definition. After the freeze the boot
// builds the generation's Dispatcher from the snapshot's frozen chain and
// replacements, and broadcasts the system_prompt.build event with the final
// prompt to every observer. The replaced prompt lands in the snapshot; the
// build's tail (stage 6b2) swaps the live executor session to the same final
// prompt when it differs from the host-composed one the session was built
// with, so snapshot and session describe the same session before any turn.
// With a nil Manager the path is byte-identical to the pre-sidecar one: no
// processes, no contributions, no dispatcher, an empty runtime set.
func assembleLegacySnapshot(ctx context.Context, in legacyAssembly, generation uint64, ext extensionBoot, mgr *sidecar.Manager) (*extension.RuntimeSnapshot, *extension.RuntimeSet, *dispatch.Dispatcher, error) {
	legacy := legacyContributions(in)
	b := extension.NewBuilder().
		WithGeneration(generation).
		WithConflictPolicy(extension.ConflictCollect).
		AddContributor(extension.ContributorFunc{
			ContributorName: "boot-legacy",
			Fn: func(context.Context) ([]extension.Contribution, error) {
				return legacy, nil
			},
		})

	prompt := in.systemPrompt
	var dispatcher *dispatch.Dispatcher
	// postFreeze runs after a successful b.Build when sidecars are live: it
	// builds the generation's dispatcher from the snapshot's own frozen chain
	// and replacements, then broadcasts system_prompt.build with the final
	// prompt so observers see exactly what froze.
	var postFreeze func(snap *extension.RuntimeSnapshot)
	if mgr != nil && len(mgr.Clients()) > 0 {
		managed := mgr
		bindExtensionUI(ext.ui, managed, ext.warn)
		sidecarContribs := managed.Contributions()
		b.AddContributor(extension.ContributorFunc{
			ContributorName: "boot-extension-runtimes",
			Fn: func(context.Context) ([]extension.Contribution, error) {
				return sidecarContribs, nil
			},
		})
		b.WithActivator(func(actx context.Context, snap *extension.RuntimeSnapshot) (*extension.RuntimeSet, error) {
			rs := extension.NewRuntimeSet(snap.Generation())
			// Track the sidecar manager as a cancelable generation effect so
			// mid-activation failure and drain share one EffectScope owner.
			if err := rs.Track(extension.Effect{
				ID:        "sidecar-manager",
				Owner:     "boot",
				Component: "extension-runtimes",
				Class:     extension.Cancelable,
				Dispose: func(ctx context.Context) error {
					_ = ctx
					return managed.Close()
				},
			}); err != nil {
				_ = managed.Close()
				return nil, err
			}
			// UI hub binding is a reversible generation effect (rebind is free).
			if err := extension.TrackUIHub(rs.Scope(), snap.Generation()); err != nil {
				_ = rs.Close()
				return nil, err
			}
			// Per-client MCP/process handles stay under the manager dispose above;
			// track an event-subscription style teardown for each live client so
			// EffectScope inventory matches the live process set for doctor.
			for _, client := range managed.Clients() {
				pluginID := client.PluginID()
				_ = extension.TrackEventSubscription(rs.Scope(), "sidecar:"+pluginID, func() error {
					// Manager.Close already tears down clients; this is inventory.
					return nil
				})
			}
			_ = actx
			return rs, nil
		})

		// Resolve replacement-slot claims over the exact contribution set
		// the builder will see, mirroring the kernel's claim pass, so a
		// conflict fails BEFORE any strategy runs and the winning claims
		// drive the pre-freeze strategy below.
		claims, err := resolveReplacementClaims(legacy, sidecarContribs)
		if err != nil {
			_ = managed.Close()
			return nil, nil, nil, err
		}
		required := requiredRuntimeSet(managed)
		clients := sidecarClientResolver(managed)
		dispatchOpts := dispatch.Options{Warn: ext.warn}
		// system_prompt.build strategy: the slot's owner rules on the
		// composed prompt before the snapshot freezes. Skipped on no-op plans
		// so CacheHash stays stable across rebuilds.
		if !ext.skipPromptStrategy {
			if _, owned := claims[extension.SlotSystemPrompt]; owned {
				strategyDispatcher := dispatch.New(nil, claims, clients, required, dispatchOpts)
				payload := dispatch.SystemPromptPayload{Prompt: prompt, WorkspaceRoot: ext.session.WorkspaceRoot}
				if err := strategyDispatcher.RunStrategy(ctx, extension.SlotSystemPrompt, extension.PointSystemPromptBuild, &payload); err != nil {
					_ = managed.Close()
					return nil, nil, nil, err
				}
				prompt = payload.Prompt
			}
		}

		postFreeze = func(snap *extension.RuntimeSnapshot) {
			if ext.previousDispatcher != nil && ext.skipPromptStrategy {
				dispatcher = ext.previousDispatcher
			} else {
				dispatcher = dispatch.New(snap.InterceptorChain(), snap.Replacements(), clients, required, dispatchOpts)
			}
			dispatcher.Event(extension.PointSystemPromptBuild, dispatch.SystemPromptPayload{
				Prompt: prompt, WorkspaceRoot: ext.session.WorkspaceRoot,
			})
		}
	} else if mgr != nil {
		// Defensive: preflight already retires client-less managers, but a
		// caller-owned Manager must never leak through assembly either way.
		_ = mgr.Close()
		mgr = nil
	}

	b.WithSystemPrompt(prompt)
	snap, runtimeSet, err := b.Build(ctx)
	if err != nil {
		if mgr != nil {
			_ = mgr.Close()
		}
		return nil, nil, nil, err
	}
	if postFreeze != nil {
		postFreeze(snap)
	}
	return snap, runtimeSet, dispatcher, nil
}

// resolveReplacementClaims replays the kernel's slot-claim pass (see
// resolveContributions) over the contribution set before the builder freezes:
// claims come from every contribution's SlotClaimer payload, winners and
// losers alike, and a second claimant is a *SlotConflictError.
func resolveReplacementClaims(groups ...[]extension.Contribution) (map[extension.Slot]extension.ContributionSource, error) {
	claims := extension.NewReplaceClaims()
	for _, group := range groups {
		for _, ct := range group {
			claimer, ok := ct.Payload.(extension.SlotClaimer)
			if !ok {
				continue
			}
			for _, slot := range claimer.ReplacementSlots() {
				if err := claims.Claim(slot, ct.Source); err != nil {
					return nil, err
				}
			}
		}
	}
	return claims.Claims(), nil
}

// requiredRuntimeSet marks every started sidecar whose manifest declared the
// runtime required:true, the dispatcher's required-class input.
func requiredRuntimeSet(mgr *sidecar.Manager) map[string]bool {
	clients := mgr.Clients()
	out := make(map[string]bool, len(clients))
	for _, client := range clients {
		out[client.PluginID()] = client.Required()
	}
	return out
}

// sidecarClientResolver adapts the Manager to the dispatcher's client lookup.
// The dispatcher requires an untyped nil for missing clients; Manager.Client
// returns a typed *sidecar.Client.
func sidecarClientResolver(mgr *sidecar.Manager) func(pluginID string) dispatch.Client {
	return func(pluginID string) dispatch.Client {
		if client := mgr.Client(pluginID); client != nil {
			return client
		}
		return nil
	}
}

// bindExtensionUI finishes the stage-8a hub binding once sidecars are live:
// the client resolver routes later /<plugin>:<action> invocations and form
// submissions, and each client's handshake-declared UI actions enter the
// registry. An invalid declaration degrades to a warning (the plugin's other
// contributions are unaffected) rather than failing the build.
func bindExtensionUI(hub *uihub.Hub, mgr *sidecar.Manager, warn func(string)) {
	if hub == nil || mgr == nil {
		return
	}
	hub.SetResolver(func(pluginID string) uihub.ActionClient {
		if client := mgr.Client(pluginID); client != nil {
			return client
		}
		return nil
	})
	for _, client := range mgr.Clients() {
		actions := client.Handshake().UIActions
		if len(actions) == 0 {
			continue
		}
		if err := hub.RegisterActions(client.PluginID(), actions); err != nil {
			warn(fmt.Sprintf("%s: extension UI actions not registered: %v", client.PluginID(), err))
		}
	}
}

// legacyContributions maps the assembled runtime resources to kernel
// contributions, reusing the extension package's per-kind mappers so scope
// attribution stays in one place. Contributions whose IDs predate or violate
// the kernel's ID contract are skipped: one pathological legacy name must not
// take down the whole snapshot, and the live runtime is untouched either way.
func legacyContributions(in legacyAssembly) []extension.Contribution {
	specByName := map[string]plugin.Spec{}
	for _, spec := range in.mcpSpecs {
		specByName[mcpNormalizedServerName(spec.Name)] = spec
	}
	var out []extension.Contribution
	for _, entry := range in.registry.ContractEntries() {
		if !extension.ValidToolID(entry.Name) {
			// Legacy MCP name normalization preserves uppercase characters,
			// which predates the kernel's lowercase tool-ID contract. Such a
			// tool keeps working in the live registry; it is skipped here
			// rather than failing the whole snapshot.
			continue
		}
		out = append(out, extension.Contribution{
			Kind:    extension.KindTool,
			ID:      entry.Name,
			Source:  legacyToolSource(entry.Name, specByName),
			Payload: entry,
		})
	}
	for _, sk := range in.skills {
		if !kernelID(sk.SlashName()) {
			continue
		}
		out = append(out, extension.SkillContribution(sk))
	}
	for _, cmd := range in.commands {
		if !kernelID(cmd.Name) {
			continue
		}
		out = append(out, extension.CommandContribution(cmd))
	}
	perEvent := map[hook.Event]int{}
	for _, h := range in.hooks {
		seq := perEvent[h.Event]
		perEvent[h.Event]++
		out = append(out, extension.HookContribution(h, seq))
	}
	for _, spec := range in.mcpSpecs {
		if !kernelID(spec.Name) {
			// A server name outside the kernel ID contract cannot be a
			// catalog entry; the server's tools still attribute through
			// specByName above.
			continue
		}
		out = append(out, extension.MCPServerContribution(spec))
	}
	for _, desc := range in.providers {
		if !extension.IsProviderRef(desc.Ref) {
			// The kernel keys providers on <name>/<model> refs; legacy
			// catalogs can carry a bare provider name or a model that itself
			// contains a slash. Those entries stay resolvable through the
			// ordinary provider path but cannot be catalogued in a v2
			// snapshot.
			continue
		}
		out = append(out, extension.ProviderContribution(desc))
	}
	return out
}

// kernelID mirrors the generic ID hygiene the kernel validates for every
// kind (non-empty, no whitespace). The kernel's parse step trims first, so
// only interior whitespace or emptiness disqualifies.
func kernelID(id string) bool {
	id = strings.TrimSpace(id)
	return id != "" && !strings.ContainsAny(id, " \t\n")
}

// legacyToolSource attributes a registry tool to its origin. MCP-backed tools
// (the mcp__<server>__<tool> namespace) belong to the server's spec: a
// package-owned server attributes to the plugin package, otherwise to the
// server itself, both at the plugin tier. Everything else — and any MCP tool
// whose server can no longer be matched to a spec — stays at the builtin
// tier: the registry is the compile-time default surface, and an
// unattributable entry must not masquerade as a higher one. Tool IDs are
// unique inside one registry, so in stage 3a the scope is provenance only and
// never decides a shadow race.
func legacyToolSource(name string, specByName map[string]plugin.Spec) extension.ContributionSource {
	if server, _, ok := tool.SplitMCPName(name); ok {
		if spec, found := specByName[server]; found {
			pluginID := strings.TrimSpace(spec.Package)
			if pluginID == "" {
				pluginID = spec.Name
			}
			origin := strings.TrimSpace(spec.ConfigSource)
			if origin == "" {
				origin = "mcp"
			}
			return extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: pluginID, Origin: origin}
		}
	}
	return extension.ContributionSource{Scope: extension.ScopeBuiltin, Origin: "builtin"}
}

// mcpNormalizedServerName returns the server name as it appears inside
// mcp__<server>__<tool> registry names, i.e. after the plugin package's name
// normalization (invalid characters replaced, collision hash appended).
func mcpNormalizedServerName(name string) string {
	return strings.TrimSuffix(strings.TrimPrefix(plugin.ToolPrefix(name), tool.MCPNamePrefix), "__")
}

// enabledMCPSpecs returns the deduplicated set of MCP server specs a build
// enabled: the configured eager/background tiers plus host-session extras, or
// the on-demand connect set under the Economy profile (whose servers enter
// the registry only through connect_tool_source). Names are deduplicated so
// the kernel catalog holds one entry per server.
func enabledMCPSpecs(configSpecs, extraSpecs []plugin.Spec, onDemandNames []string, onDemandSpecs map[string]plugin.Spec, tokenEconomy bool) []plugin.Spec {
	var out []plugin.Spec
	seen := map[string]bool{}
	add := func(spec plugin.Spec) {
		name := strings.TrimSpace(spec.Name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, spec)
	}
	if tokenEconomy {
		for _, name := range onDemandNames {
			add(onDemandSpecs[name])
		}
		return out
	}
	for _, spec := range configSpecs {
		add(spec)
	}
	for _, spec := range extraSpecs {
		add(spec)
	}
	return out
}

// gateExtensionUIRequest serves a sidecar's blocking host/ui/request once the
// session controller exists. A sidecar may legally ask right after
// extension/initialized — while the build is still assembling the controller —
// so the preflight hub cannot answer immediately. Rather than failing the
// prompt (which would deadlock an extension waiting on its own startup
// request), the gate waits for the controller to become ready, for the build
// to fail, or for the request context to cancel. serve is only invoked after
// load reports a controller.
func gateExtensionUIRequest(reqCtx context.Context, load func() *control.Controller, ready <-chan struct{}, failed <-chan struct{}, serve func(*control.Controller) (map[string]any, bool, error)) (map[string]any, bool, error) {
	if c := load(); c != nil {
		return serve(c)
	}
	select {
	case <-ready:
		c := load()
		if c == nil {
			return nil, false, fmt.Errorf("extension UI request: controller readiness signalled without a controller")
		}
		return serve(c)
	case <-failed:
		return nil, false, fmt.Errorf("extension UI request arrived but the session build failed")
	case <-reqCtx.Done():
		return nil, false, reqCtx.Err()
	}
}

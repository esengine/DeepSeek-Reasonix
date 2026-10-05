package boot

import (
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension/providerext"
	"reasonix/internal/ext/hook"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/coordinator"
	"reasonix/internal/runtime/guardian"
	"reasonix/internal/runtime/recovery"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// roleWiring is what every model role beside the executor is built from: the
// planner, the guardian and the recovery reviewer each resolve their own model
// and run behind the same headless gate.
type roleWiring struct {
	cfg       *config.Config
	roots     config.Roots
	resolver  provider.Resolver // effective: config plus sidecar providers
	extension provider.Resolver // non-nil only when a sidecar declared providers
	proxy     netclient.ProxySpec
	sink      event.Sink
	gate      *control.SharedHeadlessGate
	reg       *tool.Registry
	keep      agent.KeepPolicy
	hooks     *hook.Runner // each role fires it under a session of its own
}

// planner wraps the executor in a Coordinator when a distinct planner_model is
// configured. The planner keeps its own session for cache stability and a
// read-only tool set; an unresolvable planner degrades to the executor (#4615).
func (w roleWiring) planner(opts Options, executor *agent.Agent, executorModel, staticContext string, capRuntime *usecap.MCPCapabilityRuntime) (agent.Runner, string, error) {
	pm := effectivePlannerModel(w.cfg, opts)
	if pm == "" {
		return executor, executorModel, nil
	}
	pe, ok := resolveOptionalEntry(w.resolver, w.cfg, pm)
	if !ok {
		slog.Warn("planner model is not a configured provider — planning disabled", "model", pm)
		report(w.sink, event.Event{Level: event.LevelWarn,
			Text: fmt.Sprintf("planner_model %q is not a configured provider — continuing with the executor alone", pm)})
		return executor, executorModel, nil
	}
	if pe.Model == executorModel {
		return executor, executorModel, nil
	}
	plannerProv, err := resolveProvider(w.resolver, w.cfg, w.proxy, provider.Selection{Ref: modelRefFromEntry(pe)})
	if err != nil {
		return nil, "", fmt.Errorf("planner %q: %w", pm, err)
	}
	plannerSess := sessionstore.NewSession(coordinator.PlannerPromptWithContext(staticContext))
	// Its own ledger and use_capability frontend keep the planner's MCP calls
	// from satisfying or poisoning the executor's delivery gates.
	plannerLedger := capability.NewLedger()
	plannerAudit := &capability.Audit{}
	plannerTools := agent.PlannerToolRegistry(w.reg)
	if capRuntime != nil {
		if _, ok := plannerTools.Get("use_capability"); ok {
			plannerTools.RemovePrefix("use_capability")
		}
		plannerTools.Add(capRuntime.NewFrontend(plannerLedger, plannerAudit))
	}
	plannerOpts := agent.Options{
		MaxSteps:                     0,
		Gate:                         w.gate,
		Hooks:                        w.hooks.ForRole("planner"),
		ModelRef:                     modelRefFromEntry(pe),
		ContextWindow:                pe.ContextWindow,
		CompactRatio:                 w.cfg.Agent.CompactRatio,
		ContextEditing:               w.cfg.Agent.ContextEditing,
		RecentKeep:                   w.cfg.Agent.RecentKeep,
		CompactionBudgets:            compactionBudgets(w.cfg),
		ArchiveDir:                   w.roots.ArchiveDir(),
		KeepPolicy:                   w.keep,
		ReasoningLanguage:            w.cfg.ReasoningLanguage(),
		CapabilityLedger:             plannerLedger,
		CapabilityAudit:              plannerAudit,
		MissingReasoningWarnStateDir: config.MissingReasoningWarnStateDir(),
	}
	runner := coordinator.NewCoordinatorWithPlannerPolicy(plannerProv, plannerSess, pe.Price, plannerTools, plannerOpts, executor, w.cfg.Agent.Temperature, w.sink, control.NewPlannerPolicy())
	return runner, executorModel + " + planner " + pe.Model, nil
}

// guardian is the LLM safety reviewer guardian_model asks for: it may
// auto-allow a safe Ask and annotates a risky one before the human sees it.
// A model that cannot be resolved or started disables it with a notice.
func (w roleWiring) guardian() *guardian.Session {
	guardianModel := w.cfg.Agent.GuardianModel
	if guardianModel == "" {
		return nil
	}
	ge, ok := resolveOptionalEntry(w.resolver, w.cfg, guardianModel)
	if !ok {
		slog.Warn("guardian model is not a configured provider — guardian disabled", "model", guardianModel)
		report(w.sink, event.Event{Level: event.LevelWarn, Text: "Guardian was disabled because its model was not found.", Detail: fmt.Sprintf("guardian_model %q not found — guardian disabled", guardianModel)})
		return nil
	}
	pProv, err := resolveProvider(w.resolver, w.cfg, w.proxy, provider.Selection{Ref: modelRefFromEntry(ge)})
	if err != nil {
		slog.Warn("guardian provider construction failed — guardian disabled", "model", guardianModel, "err", err)
		report(w.sink, event.Event{Level: event.LevelWarn, Text: "Guardian was disabled because it could not start.", Detail: fmt.Sprintf("guardian construction failed: %v — guardian disabled", err)})
		return nil
	}
	guardianReg := agent.FilterReadOnlyRegistry(w.reg, agent.SubagentMetaTools()...)
	g := guardian.NewSession(pProv, guardianReg, w.hooks.ForRole("guardian"), guardian.PolicyPrompt(), modelRefFromEntry(ge), w.cfg.Agent.GuardianTemperature, ge.Price, w.sink)
	report(w.sink, event.Event{Level: event.LevelInfo, Text: fmt.Sprintf("guardian enabled · model=%s", ge.Model)})
	return g
}

// recoveryReviewer prefers recovery_model, then guardian_model, then the main
// model, each in an isolated session. Without one, recovery is rule-only.
func (w roleWiring) recoveryReviewer(mainRef string) recovery.Reviewer {
	model := strings.TrimSpace(w.cfg.Agent.RecoveryModel)
	if model == "" {
		model = strings.TrimSpace(w.cfg.Agent.GuardianModel)
	}
	if model == "" {
		model = mainRef
	}
	if model == "" {
		return nil
	}
	// A plugin-namespaced ref resolves only through the extension resolver.
	if w.extension != nil && providerext.PluginRefOwner(model) != "" {
		re, ok := resolveOptionalEntry(w.extension, w.cfg, model)
		if !ok {
			return nil
		}
		rProv, err := w.extension.Resolve(provider.Selection{Ref: modelRefFromEntry(re)})
		if err != nil {
			slog.Warn("recovery reviewer provider construction failed — rule-only recovery", "model", model, "err", err)
			return nil
		}
		return recovery.NewSessionWithSink(rProv, re.Price, modelRefFromEntry(re), w.sink)
	}
	re, ok := w.cfg.ResolveModel(model)
	if !ok {
		return nil
	}
	rProv, err := NewProviderWithProxy(re, w.proxy)
	if err != nil {
		slog.Warn("recovery reviewer provider construction failed — rule-only recovery", "model", model, "err", err)
		return nil
	}
	return recovery.NewSessionWithSink(rProv, re.Price, modelRefFromEntry(re), w.sink)
}

// semanticRouter is one role-neutral router, so an in-place role switch never
// rebuilds the controller. Building it sends no request; the turn's policy
// decides whether it is consulted.
func (w roleWiring) semanticRouter(sub subagentConfig, exec provider.Provider, execRef string, execPrice *provider.Pricing, audit *capability.Audit) *capability.SemanticRouter {
	if modelRef := strings.TrimSpace(w.cfg.Agent.SubagentModels["capability-router"]); modelRef != "" {
		effortRef := strings.TrimSpace(w.cfg.Agent.SubagentEfforts["capability-router"])
		if p, price, _, err := sub.resolveProvider(modelRef, effortRef); err == nil && p != nil {
			usageModelRef, _ := sub.identity(modelRef, effortRef)
			return &capability.SemanticRouter{Provider: p, Sink: w.sink, Model: usageModelRef, Pricing: price, Audit: audit}
		}
	}
	return &capability.SemanticRouter{Provider: exec, Sink: w.sink, Model: execRef, Pricing: execPrice, Audit: audit}
}

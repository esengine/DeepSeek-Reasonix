package delegation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/usecap"
)

type RunnerOptions struct {
	Registry      *tool.Registry
	Provider      provider.Provider
	Price         *provider.Pricing
	ContextWindow int
	CapRuntime    *usecap.MCPCapabilityRuntime
	Store         *SubagentStore
	WorkspaceRoot string
	MaxDepth      int
	MaxSteps      int
	SystemPrompt  string
	Resolve       func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	Identity      func(modelRef, effort string) (string, string)
	RunOptions    func(ctx context.Context, steps int, price *provider.Pricing, ctxWin, childDepth int) agent.Options
}

type Runner struct{ RunnerOptions }

func NewRunner(opts RunnerOptions) *Runner { return &Runner{opts} }

func (r *Runner) SetCapabilityRuntime(rt *usecap.MCPCapabilityRuntime) {
	if r != nil {
		r.CapRuntime = rt
	}
}

type RunSpec struct {
	Title        string
	Task         string
	Kind         string
	Name         string
	SystemPrompt string
	Tools        []string
	Model        string
	Effort       string
	MaxSteps     int
	Index        int
	ReadOnly     bool
}

type RunOutput struct {
	Ref    string
	Answer string
	Read   []string
	Wrote  []string
}

func (r *Runner) steps(own int) int {
	steps := r.MaxSteps
	if steps > 0 {
		if steps /= 2; steps < 5 {
			steps = 5
		}
	}
	if own > 0 && (steps <= 0 || own < steps) {
		return own
	}
	return steps
}

func (r *Runner) runtimeFor(ctx context.Context, spec RunSpec) (provider.Provider, *provider.Pricing, int, string, string, error) {
	modelRef, effortRef := agent.VisionRefFor(ctx, spec.Model), agent.VisionEffortFor(ctx, spec.Model, spec.Effort)
	if modelRef == "" && effortRef == "" || r.Resolve == nil {
		return r.Provider, r.Price, r.ContextWindow, modelRef, effortRef, nil
	}
	prov, price, window, err := r.Resolve(modelRef, effortRef)
	if err != nil {
		return nil, nil, 0, "", "", err
	}
	return prov, price, window, modelRef, effortRef, nil
}

func (r *Runner) Run(ctx context.Context, spec RunSpec) (RunOutput, error) {
	if strings.TrimSpace(spec.Task) == "" {
		return RunOutput{}, fmt.Errorf("task is required")
	}
	childDepth := agent.SubagentDepth(ctx) + 1
	if childDepth > r.MaxDepth {
		return RunOutput{}, fmt.Errorf("sub-agents cannot start more sub-agents here (max_subagent_depth=%d)", r.MaxDepth)
	}
	ctx = agent.BindTaskImages(ctx, spec.Task)
	prov, price, window, modelRef, effortRef, err := r.runtimeFor(ctx, spec)
	if err != nil {
		return RunOutput{}, fmt.Errorf("sub-agent model: %w", err)
	}
	readOnly := spec.ReadOnly || agent.PlanModeFromContext(ctx)
	var subReg *tool.Registry
	if readOnly {
		names := spec.Tools
		if len(names) == 0 {
			names = subagentBaseTools
		}
		subReg = agent.ReadOnlySubagentToolRegistryForDepthWithRuntime(r.Registry, names, childDepth, r.MaxDepth, r.CapRuntime)
	} else {
		subReg = agent.SubagentToolRegistryForDepthWithRuntime(r.Registry, baseTools(spec.Tools), childDepth, r.MaxDepth, r.CapRuntime)
	}
	if subReg.Len() == 0 {
		return RunOutput{}, fmt.Errorf("sub-agent has no tools available")
	}
	system := firstNonEmpty(spec.SystemPrompt, r.SystemPrompt)
	system = agent.ChildCompositionPrompt(ctx, system)
	run, err := r.prepare(ctx, spec, system, subReg, modelRef, effortRef)
	if err != nil {
		return RunOutput{}, err
	}
	defer run.Release()
	if err := agent.BindChildComposition(ctx, run.Session, false); err != nil {
		return RunOutput{}, err
	}
	if err := r.Store.MarkRunning(run); err != nil {
		return RunOutput{}, err
	}
	opts := r.RunOptions(ctx, r.steps(spec.MaxSteps), price, window, childDepth)
	if r.Identity != nil {
		opts.ModelRef, _ = r.Identity(modelRef, effortRef)
	}
	parentID, parent, _, _ := agent.CallContext(ctx)
	childCtx := agent.WithUserImages(ctx, agent.SubagentImageCandidates(ctx))
	runChild := agent.RunSubAgentWithSession
	if readOnly {
		runChild = agent.RunReadOnlySubAgentWithSession
	}
	answer, err := runChild(childCtx, prov, subReg, run.Session, agent.SubagentImageNote(ctx)+spec.Task, opts, childSink(parentID, spec.Index, parent))
	if err != nil {
		return RunOutput{Ref: run.Ref}, joinErr(err, r.Store.SaveFailed(run))
	}
	if err := r.Store.SaveCompleted(run); err != nil {
		return RunOutput{Ref: run.Ref, Answer: answer}, err
	}
	read, wrote := touchedPaths(run.Session.Snapshot())
	return RunOutput{Ref: run.Ref, Answer: answer, Read: read, Wrote: wrote}, nil
}

func (r *Runner) prepare(ctx context.Context, spec RunSpec, system string, subReg *tool.Registry, modelRef, effortRef string) (*SubagentRun, error) {
	parentSession := strings.TrimSpace(agent.ParentSession(ctx))
	if r.Store == nil || parentSession == "" {
		return EphemeralSubagentRun(system), nil
	}
	parentID, _, _, _ := agent.CallContext(ctx)
	model, effort := modelRef, effortRef
	if r.Identity != nil {
		model, effort = r.Identity(modelRef, effortRef)
	}
	return r.Store.PrepareFresh(SubagentSpec{
		Kind:             firstNonEmpty(spec.Kind, "task"),
		Name:             firstNonEmpty(spec.Name, spec.Title, "task"),
		WorkspaceRoot:    r.WorkspaceRoot,
		ParentSession:    parentSession,
		ParentToolCallID: parentID,
		ParentTurn:       TurnIdentity(ctx),
		SystemPrompt:     system,
		ExecutionID:      executionFor(parentID, spec.Index),
		Registry:         subReg,
		ToolContext:      childToolIdentityContext(ctx),
		Model:            model,
		Effort:           effort,
	})
}

var subagentBaseTools = []string{"read_file", "write_file", "bash"}

func baseTools(allowed []string) []string {
	if len(allowed) == 0 {
		return subagentBaseTools
	}
	var out []string
	for _, name := range subagentBaseTools {
		if slices.Contains(allowed, name) {
			out = append(out, name)
		}
	}
	return out
}

func executionFor(parentID string, index int) string {
	if parentID == "" {
		return "exec-" + strconv.Itoa(index)
	}
	return parentID + "/" + strconv.Itoa(index)
}

func joinErr(err, other error) error {
	if other == nil {
		return err
	}
	return errors.Join(err, other)
}

type childEvents struct {
	event.AuditForwarder
	parentID string
	prefix   string
	parent   event.Sink
}

func childSink(parentID string, index int, parent event.Sink) event.Sink {
	if parent == nil {
		return event.Discard
	}
	return childEvents{AuditForwarder: event.AuditForwarder{Inner: parent}, parentID: parentID, prefix: parentID + "/" + strconv.Itoa(index), parent: parent}
}

func (s childEvents) Emit(e event.Event) {
	switch e.Kind {
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		e.Tool.ParentID = s.parentID
		e.Tool.ID = namespaceToolID(s.prefix, e.Tool.ID)
		e.Source = event.UsageSourceSubagent
		s.parent.Emit(e)
	case event.Usage:
		if e.UsageSource == "" {
			e.UsageSource = event.UsageSourceSubagent
		}
		s.parent.Emit(e)
	}
}

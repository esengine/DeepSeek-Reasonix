package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// DefaultTaskSystemPrompt steers a sub-agent toward focused, terse delivery —
// it doesn't see the parent's conversation so it must self-contain.
const DefaultTaskSystemPrompt = `You are a sub-agent invoked by a parent coding agent to carry out one focused task.
Use the provided tools to investigate or act. Return a single final answer that is concise
and self-contained — the parent will see only that answer, not your tool calls or reasoning.
If you need to ask for clarification, fail with a precise question instead of guessing.`

var subagentMetaTools = []string{
	"task",
	"run_skill",
	"install_skill",
	"explore",
	"research",
	"review",
	"security_review",
}

// SubagentMetaTools returns the tool names that spawned agents should not inherit
// from the parent registry unless a future call site deliberately opts into a
// different boundary. They can spawn or author more agent work, so excluding them
// preserves one layer of delegation without adding a spawn-count cap.
func SubagentMetaTools() []string {
	out := make([]string, len(subagentMetaTools))
	copy(out, subagentMetaTools)
	return out
}

// TaskTool spawns a sub-agent in its own session for a focused sub-task. The
// sub-agent runs with a filtered tool whitelist and the same step budget shape
// as the parent (see Execute); its tool calls are forwarded to the parent's
// event stream nested under this call, while only its final assistant message is
// returned to the parent model. Use cases: keep noisy tool sequences (multi-file
// exploration, repeated grep / read_file) out of the parent's context budget, or
// parallel research across independent areas (the parallel-dispatch path picks
// these up only when readOnly, which task is not).
type TaskTool struct {
	prov              provider.Provider
	pricing           *provider.Pricing
	parentReg         *tool.Registry
	maxSteps          int
	contextWindow     int
	softCompactRatio  float64
	compactRatio      float64
	compactForceRatio float64
	temperature       float64
	archiveDir        string
	sysPrompt         string
	gate              Gate
	subagentModel     string
	subagentEffort    string
	resolveProvider   func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
}

// NewTaskTool wires a task tool to the parent agent's environment so its
// sub-agents can use the same provider and tools. sysPrompt is the system
// prompt every sub-agent starts with; pass "" for DefaultTaskSystemPrompt. gate
// is the permission gate sub-agents inherit — pass the headless variant so
// deny rules still bite while autonomous sub-agents are never blocked on an
// interactive prompt (there is no UI to answer one).
func NewTaskTool(prov provider.Provider, pricing *provider.Pricing, parentReg *tool.Registry,
	maxSteps, contextWindow int, softCompactRatio, compactRatio, compactForceRatio, temperature float64, archiveDir, sysPrompt string, gate Gate,
	subagentModel, subagentEffort string, resolveProvider func(string, string) (provider.Provider, *provider.Pricing, int, error)) *TaskTool {
	if sysPrompt == "" {
		sysPrompt = DefaultTaskSystemPrompt
	}
	return &TaskTool{
		prov:              prov,
		pricing:           pricing,
		parentReg:         parentReg,
		maxSteps:          maxSteps,
		contextWindow:     contextWindow,
		softCompactRatio:  softCompactRatio,
		compactRatio:      compactRatio,
		compactForceRatio: compactForceRatio,
		temperature:       temperature,
		archiveDir:        archiveDir,
		sysPrompt:         sysPrompt,
		gate:              gate,
		subagentModel:     subagentModel,
		subagentEffort:    subagentEffort,
		resolveProvider:   resolveProvider,
	}
}

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Description() string {
	return "Spawn a sub-agent for a focused sub-task. The sub-agent runs in its own session with the same provider and a filtered tool list (defaults to every parent tool except subagent/skill meta-tools, so delegation stays one layer deep). Only its final answer is returned. Use this to (a) keep long exploration sequences out of the parent's context budget, or (b) delegate self-contained work like 'find every place that calls X and summarise the patterns'."
}

func (t *TaskTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "prompt":{"type":"string","description":"What the sub-agent should accomplish. Be specific about the deliverable — the sub-agent does not see this conversation."},
  "description":{"type":"string","description":"Short label for the sub-task (3-7 words). Surfaced in the dispatch line so the user sees what's running."},
  "tools":{"type":"array","items":{"type":"string"},"description":"Optional tool whitelist. Subagent/skill meta-tools are still excluded so delegation stays one layer deep."},
  "max_steps":{"type":"integer","description":"Optional cap on tool-call rounds. Defaults to half the parent's cap (min 5).","minimum":1},
  "run_in_background":{"type":"boolean","description":"Run the sub-agent asynchronously: returns a job id immediately and keeps working across turns. Collect its final answer with wait, and you'll be notified when it finishes. Use for long, independent sub-tasks you don't need to block on right now."},
  "model":{"type":"string","description":"Optional model override for the sub-agent (a configured provider/model name)."},
  "effort":{"type":"string","description":"Optional reasoning effort for the sub-agent (e.g. high, max)."}
},
"required":["prompt"]
}`)
}

// ReadOnly is false: a sub-agent can invoke any whitelisted tool, including
// writers. Conservative classification keeps the parallel-dispatch path from
// running two sub-agents at once and letting their writes race.
func (t *TaskTool) ReadOnly() bool { return false }

// ResolveProfile extracts model/effort from task args and applies config defaults.
func (t *TaskTool) ResolveProfile(args json.RawMessage) *event.Profile {
	var p struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil
	}
	model, effort := t.effectiveProfile(p.Model, p.Effort)
	if model == "" && effort == "" {
		return nil
	}
	return &event.Profile{Model: model, Effort: effort}
}

func (t *TaskTool) effectiveProfile(model, effort string) (string, string) {
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	if model == "" {
		model = strings.TrimSpace(t.subagentModel)
	}
	if effort == "" {
		effort = strings.TrimSpace(t.subagentEffort)
	}
	return model, effort
}

func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Prompt          string   `json:"prompt"`
		Description     string   `json:"description"`
		Tools           []string `json:"tools"`
		MaxSteps        int      `json:"max_steps"`
		RunInBackground bool     `json:"run_in_background"`
		Model           string   `json:"model"`
		Effort          string   `json:"effort"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	maxSteps := p.MaxSteps
	if maxSteps <= 0 {
		// No explicit cap from the caller: mirror the parent. A finite parent caps
		// the sub-agent at half its budget (min 5) so a delegated sub-task stays
		// shorter than the whole turn; an unbounded parent yields an unbounded
		// sub-agent. The sub-agent shares the parent's ctx, so cancelling the turn
		// stops it, and it compacts its own context — the same bounds the parent has.
		if t.maxSteps > 0 {
			maxSteps = t.maxSteps / 2
			if maxSteps < 5 {
				maxSteps = 5
			}
		}
	}

	subReg := t.buildSubReg(p.Tools)
	modelRef, effortRef := t.effectiveProfile(p.Model, p.Effort)

	// Background: register a job that runs the sub-agent under the manager's
	// session context (so it survives this turn) and return immediately. The
	// sub-agent's tool activity still streams, nested under this call, because the
	// nested sink captures the parent ID + stream now (not from the job ctx).
	if p.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			return "", fmt.Errorf("background execution is not available in this context")
		}
		parentID, parent, _, _ := CallContext(ctx)
		nested := subSinkFor(parentID, parent)
		label := p.Description
		if label == "" {
			label = "task"
		}
		job := jm.Start("task", label, func(jobCtx context.Context, _ io.Writer) (string, error) {
			return t.runSub(jobCtx, p.Prompt, subReg, nested, maxSteps, modelRef, effortRef)
		})
		return fmt.Sprintf("Started background task %q (%s). It runs across turns; collect its final answer with wait (or wait will return it once done), and you'll be notified when it finishes.", job.ID, label), nil
	}

	// Foreground: run synchronously, nesting events under this call.
	return t.runSub(ctx, p.Prompt, subReg, subSink(ctx), maxSteps, modelRef, effortRef)
}

// buildSubReg returns the sub-agent's tool set: the named whitelist (minus
// subagent/skill meta-tools, to bar recursive nesting), or every parent tool
// except those meta-tools.
func (t *TaskTool) buildSubReg(names []string) *tool.Registry {
	return FilterRegistry(t.parentReg, names, SubagentMetaTools()...)
}

// FilterRegistry builds a sub-registry from parent: the named whitelist (empty =
// every parent tool), minus any excluded names. Used to scope what a spawned
// sub-agent — a `task` sub-agent or a subagent skill — may call, e.g. excluding
// `task` to bar recursive nesting, or restricting to a skill's allowed-tools.
func FilterRegistry(parent *tool.Registry, names []string, exclude ...string) *tool.Registry {
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	src := names
	if len(src) == 0 {
		src = parent.Names()
	}
	for _, name := range src {
		if ex[name] {
			continue
		}
		if tl, ok := parent.Get(name); ok {
			sub.Add(tl)
		}
	}
	return sub
}

var plannerNonResearchTools = []string{
	"ask",
	"bash_output",
	"complete_step",
	"slash_command",
	"todo_write",
	"wait",
}

// PlannerToolRegistry returns the tool set exposed to the two-model planner:
// read-only research tools only. It deliberately excludes workflow/meta tools
// that are technically read-only but can prompt the user, update visible task
// state, wait on jobs, or expand commands instead of inspecting context.
func PlannerToolRegistry(parent *tool.Registry) *tool.Registry {
	exclude := append(SubagentMetaTools(), plannerNonResearchTools...)
	return FilterReadOnlyRegistry(parent, exclude...)
}

// FilterReadOnlyRegistry builds a sub-registry containing only tools whose
// ReadOnly contract is true, minus explicit exclusions.
func FilterReadOnlyRegistry(parent *tool.Registry, exclude ...string) *tool.Registry {
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	if parent == nil {
		return sub
	}
	for _, name := range parent.Names() {
		if ex[name] {
			continue
		}
		tl, ok := parent.Get(name)
		if !ok || !tl.ReadOnly() {
			continue
		}
		sub.Add(tl)
	}
	return sub
}

// runSub builds a sub-agent over subReg, runs prompt to completion emitting to
// sink, and returns its final assistant answer. Shared by the foreground and
// background paths. modelRef and effort override the parent defaults when non-empty.
func (t *TaskTool) runSub(ctx context.Context, prompt string, subReg *tool.Registry, sink event.Sink, maxSteps int, modelRef, effort string) (string, error) {
	prov, pricing, ctxWin := t.prov, t.pricing, t.contextWindow
	if t.resolveProvider != nil && (modelRef != "" || effort != "") {
		p, pr, cw, err := t.resolveProvider(modelRef, effort)
		if err != nil {
			return "", fmt.Errorf("sub-agent profile: %w", err)
		}
		prov, pricing, ctxWin = p, pr, cw
	}
	return RunSubAgent(ctx, prov, subReg, t.sysPrompt, prompt, Options{
		MaxSteps:          maxSteps,
		Temperature:       t.temperature,
		Pricing:           pricing,
		Gate:              t.gate,
		ContextWindow:     ctxWin,
		SoftCompactRatio:  t.softCompactRatio,
		CompactRatio:      t.compactRatio,
		CompactForceRatio: t.compactForceRatio,
		ArchiveDir:        t.archiveDir,
	}, sink)
}

// RunSubAgent runs prompt to completion in a fresh sub-agent session over reg,
// emitting tool activity to sink, and returns the sub-agent's final assistant
// answer. It is the shared core behind the `task` tool and subagent skills: a
// caller supplies the system prompt (the task persona or the skill body), the
// tool registry (already filtered), and the run Options (model budget, gate).
func RunSubAgent(ctx context.Context, prov provider.Provider, reg *tool.Registry, sysPrompt, prompt string, opts Options, sink event.Sink) (string, error) {
	sess := NewSession(sysPrompt)
	sub := New(prov, reg, sess, opts, sink)
	if err := sub.Run(ctx, prompt); err != nil {
		return "", fmt.Errorf("sub-agent: %w", err)
	}
	// Walk the session backwards for the last assistant message with content —
	// that's the sub-agent's final answer. Intermediate assistant messages with
	// tool_calls but no text don't count.
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		m := sess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("sub-agent finished without producing a final answer")
}

// NestedSink returns a sink that forwards a sub-agent's tool activity to the
// parent stream, nested under the tool call carried by ctx, so a frontend shows
// it beneath that call (the same nesting `task` uses). Falls back to the given
// sink when ctx carries no call context. Used by subagent skills.
func NestedSink(ctx context.Context, fallback event.Sink) event.Sink {
	parentID, parent, _, ok := CallContext(ctx)
	if !ok || parent == nil {
		return fallback
	}
	return subSinkFor(parentID, parent)
}

// subSink forwards a sub-agent's tool dispatch/result events to the parent's
// event stream, tagged with the parent task call's ID so a frontend nests them
// under it. The sub-agent's own turn/usage/text/reasoning events are dropped —
// only its tool activity (the part worth seeing live) and its final answer
// (returned by Execute) reach the parent. The forwarded call IDs are namespaced
// with the parent ID so a sub-agent call can never collide with a parent call in
// the frontend's dispatch→result matching. Falls back to Discard when there's no
// parent stream (the headless run loop, or a direct Execute in tests).
func subSink(ctx context.Context) event.Sink {
	parentID, parent, _, ok := CallContext(ctx)
	if !ok || parent == nil {
		return event.Discard
	}
	return subSinkFor(parentID, parent)
}

// subSinkFor builds the nesting sink from an already-captured parent ID + stream,
// for the background path where the job runs under a context that no longer
// carries the call context. Falls back to Discard when there's no parent stream.
func subSinkFor(parentID string, parent event.Sink) event.Sink {
	if parent == nil {
		return event.Discard
	}
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ToolDispatch, event.ToolResult:
			e.Tool.ParentID = parentID
			e.Tool.ID = parentID + "/" + e.Tool.ID
			parent.Emit(e)
		}
	})
}

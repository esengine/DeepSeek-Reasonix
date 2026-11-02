package agent

import (
	"context"
	"fmt"
	"reasonix/internal/contract/hostaudit"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/sessiontemp"
	"slices"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/memory"
	"reasonix/internal/tools/jobs"
)

// RunSubAgentWithSession continues an existing sub-agent session with prompt and
// returns the latest final assistant answer. Fresh sub-agents pass a newly-created
// session; continued sub-agents pass a loaded transcript session.
//
// Each call installs an independent session-private temporary directory Manager
// so parent, sibling, and nested sub-agents never share temporary files.
// continue_from restores conversation history only — a new run still gets a
// fresh temporary directory.
func RunSubAgentWithSession(ctx context.Context, prov provider.Provider, reg *tool.Registry, sess *sessionstore.Session, prompt string, opts Options, sink event.Sink) (answer string, err error) {
	if sess == nil {
		return "", fmt.Errorf("sub-agent session is nil")
	}
	if err := BindChildComposition(ctx, sess, true); err != nil {
		return "", err
	}
	restrictChildComposition(ctx, reg)
	// Isolate temporary files for this run before any tool execution.
	ctx = tool.WithoutGoalTurnRecorder(ctx)
	if opts.MemoryQueue != nil {
		ctx = memory.WithQueue(ctx, opts.MemoryQueue)
	} else {
		ctx = memory.WithoutQueue(ctx)
	}
	if opts.Jobs == nil {
		ctx = jobs.WithoutManager(ctx)
	}
	ctx, releaseTemp := withSubagentSessionTemp(ctx)
	defer releaseTemp()
	if opts.SubagentDepth > 0 {
		ctx = WithSubagentDepth(ctx, opts.SubagentDepth)
	}
	// Callers that wrap the prompt themselves (runSubSession) set
	// ClassifierTaskText before wrapping; for everyone else the prompt is
	// still pristine here, so capture it before host framing is prepended.
	if strings.TrimSpace(opts.ClassifierTaskText) == "" {
		opts.ClassifierTaskText = prompt
	}
	planWorkflow := PlanModeFromContext(ctx)
	if opts.SubagentDepth > 0 && isFreshSubagentSession(sess) {
		prompt = subagentStartContext + "\n\n" + prompt
	}
	if planWorkflow && !strings.Contains(prompt, planmode.Marker) {
		prompt = planmode.Marker + "\n\n" + prompt
	}
	ctx = WithoutRunTimeSink(ctx)
	sub := newDelegatedAgent(ctx, prov, reg, sess, opts, sink)
	sub.SetPlanMode(planWorkflow)
	if err := sub.Run(ctx, prompt); err != nil {
		// Still merge any partial child evidence so parent gates see real writes.
		mergeChildEvidence(ctx, sub)
		return "", fmt.Errorf("sub-agent: %w", err)
	}
	mergeChildEvidence(ctx, sub)
	if answer := latestAssistantAnswer(sess); answer != "" {
		return composeSubagentAnswer(ctx, answer, sub, writeclaim.SubagentWriteClaim(ctx), opts.ClassifierTaskText), nil
	}
	return "", fmt.Errorf("sub-agent finished without producing a final answer")
}

// readOnlyAgentConstruction is the single pairing every strictly read-only
// loop shares: the permanent ReadOnlyExecution flag plus the final registry
// filter. Batch children (RunReadOnlySubAgentWithSession) and legacy call sites
// that still use NewReadOnlyAgent build through it, so a missed call site
// cannot set only half the boundary. The interactive two-model planner uses
// NewPlannerAgent instead (PlannerMCPExecution).
func readOnlyAgentConstruction(reg *tool.Registry, opts Options) (*tool.Registry, Options) {
	opts.ReadOnlyExecution = true
	opts.PlannerMCPExecution = false
	return strictReadOnlyExecutionRegistry(reg), opts
}

// NewPlannerAgent constructs the interactive two-model planner: permanent
// ReadOnlyExecution still blocks bash, file writers, and ordinary non-MCP
// writers, while PlannerMCPExecution allows authorized, non-destructive MCP
// through the stable use_capability proxy without requiring readOnlyHint.
func NewPlannerAgent(prov provider.Provider, reg *tool.Registry, sess *sessionstore.Session, opts Options, sink event.Sink) *Agent {
	opts.EventSource = event.UsageSourcePlanner
	opts.ReadOnlyExecution = true
	opts.PlannerMCPExecution = true
	// Keep construction-time filter for ordinary tools; use_capability stays
	// because it is ReadOnly. Direct mcp__* tools are already excluded by
	// PlannerToolRegistry. Dynamic MCP targets are re-checked after resolve.
	reg = plannerExecutionRegistry(reg)
	return New(prov, reg, sess, opts, sink)
}

// plannerExecutionRegistry is the construction-time filter for NewPlannerAgent.
// It removes ordinary writers and destructive direct MCP tools while keeping
// use_capability and built-in research tools. Host-starting deferred MCP
// targets are allowed at execution time under PlannerMCPExecution.
func plannerExecutionRegistry(reg *tool.Registry) *tool.Registry {
	filtered := tool.NewRegistry()
	if reg == nil {
		return filtered
	}
	filtered.CopyDisabledMCPFrom(reg)
	for _, name := range reg.Names() {
		target, ok := reg.Get(name)
		if !ok {
			continue
		}
		if name == "use_capability" {
			filtered.Add(target)
			continue
		}
		if strings.HasPrefix(name, tool.MCPNamePrefix) {
			// Defense in depth: planner never exposes direct MCP schemas.
			continue
		}
		if !target.ReadOnly() || tool.HasMCPDestructiveHint(target) {
			continue
		}
		if h, ok := target.(tool.ReadOnlyExecutionHostMutation); ok && h.ReadOnlyExecutionHostMutation() {
			// Ordinary host mutations stay out; MCP startup is only via proxy.
			continue
		}
		filtered.Add(target)
	}
	return filtered
}

// RunReadOnlySubAgentWithSession is the construction boundary for every
// strictly read-only child loop. Registry filtering limits the visible surface;
// this permanent execution flag also re-checks targets resolved dynamically by
// proxy tools such as use_capability. It never enables PlannerMCPExecution.
func RunReadOnlySubAgentWithSession(ctx context.Context, prov provider.Provider, reg *tool.Registry, sess *sessionstore.Session, prompt string, opts Options, sink event.Sink) (string, error) {
	reg, opts = readOnlyAgentConstruction(reg, opts)
	return RunSubAgentWithSession(ctx, prov, reg, sess, prompt, opts, sink)
}

// strictReadOnlyExecutionRegistry is the final construction-time filter shared
// by every strict child. Callers still apply role-specific filtering (review,
// planner, profile allowlists), while this layer guarantees that a missed call
// site cannot expose writers, destructive MCP tools, readers from unauthorized
// servers, or an unauthorized host-starting target to the model.
func strictReadOnlyExecutionRegistry(reg *tool.Registry) *tool.Registry {
	filtered := tool.NewRegistry()
	if reg == nil {
		return filtered
	}
	filtered.CopyDisabledMCPFrom(reg)
	for _, name := range reg.Names() {
		target, ok := reg.Get(name)
		if !ok || !target.ReadOnly() || tool.HasMCPDestructiveHint(target) {
			continue
		}
		if isInstalledMCPTool(target) && !tool.IsMCPServerAuthorized(target) {
			continue
		}
		if mutation, ok := target.(tool.ReadOnlyExecutionHostMutation); ok && mutation.ReadOnlyExecutionHostMutation() && !readOnlyExecutionAllowsMCPStartup(target) {
			continue
		}
		filtered.Add(target)
	}
	return filtered
}

// latestAssistantAnswer walks the session backwards for the last assistant
// message with content — that's the sub-agent's final answer. Intermediate
// assistant messages with tool_calls but no text don't count.
func latestAssistantAnswer(sess *sessionstore.Session) string {
	if sess == nil {
		return ""
	}
	for _, v := range slices.Backward(sess.Messages) {
		m := v
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content
		}
	}
	return ""
}

// mergeChildEvidence folds a sub-agent's real receipts into the parent ledger
// carried on ctx. Meta tools themselves are never mutations.
func mergeChildEvidence(ctx context.Context, sub *Agent) {
	if sub == nil {
		return
	}
	parent, ok := evidence.FromContext(ctx)
	if !ok || parent == nil {
		return
	}
	parent.MergeChild(sub.EvidenceSummary())
}

func isFreshSubagentSession(sess *sessionstore.Session) bool {
	if sess == nil {
		return false
	}
	snap := sess.Snapshot()
	return len(snap) == 1 && snap[0].Role == provider.RoleSystem
}

// EvidenceSummary exports this agent's turn-scoped receipts for parent merge.
func (a *Agent) EvidenceSummary() evidence.ChildEvidenceSummary {
	if a == nil || a.task.ledger == nil {
		return evidence.ChildEvidenceSummary{}
	}
	return a.task.ledger.Summary()
}

// composeSubagentAnswer assembles everything the parent is shown for one child
// run: the child's own prose, then the host's receipts.
func composeSubagentAnswer(ctx context.Context, answer string, sub *Agent, claims writeclaim.WritePathSet, delegationText string) string {
	summary := sub.EvidenceSummary()
	recordDelegationAudit(ctx, summary, claims, delegationText)
	return AppendHostReceipts(answer, summary, claims)
}

// newDelegatedAgent builds the child a delegated run executes as, wired from
// the call context rather than from what a caller remembered to pass. Only a
// delegated run gets an asker: its parent is blocked and a person is waiting,
// while fleet items, parallel tasks and background jobs stay silent.
func newDelegatedAgent(ctx context.Context, prov provider.Provider, reg *tool.Registry,
	sess *sessionstore.Session, opts Options, sink event.Sink,
) *Agent {
	sub := New(prov, reg, sess, opts, sink)
	if _, _, asker, ok := CallContext(ctx); ok && asker != nil {
		sub.SetAsker(asker)
	}
	if opts.OnStart != nil {
		opts.OnStart(sub)
	}
	return sub
}

const subagentStartContext = `<subagent-context event="SubagentStart">
Before acting, check the available skills and tools. If a relevant skill is available, invoke it before continuing. Delegate to another sub-agent only when the task genuinely benefits from isolated context and the delegation tool is available.
</subagent-context>`

// withSubagentSessionTemp installs a fresh session-private temporary directory
// Manager for one sub-agent run. The returned release must be deferred by the
// caller so the directory is retired when the run ends (including background
// sub-agent completion).
func withSubagentSessionTemp(ctx context.Context) (context.Context, func()) {
	m := sessiontemp.New()
	m.Retain()
	return sessiontemp.WithManager(ctx, m), m.Release
}

// mutationObserverSetter is a tool that spawns sub-agents and hands them the
// parent's mutation observer.
type mutationObserverSetter interface {
	SetMutationObserver(*checkpoint.MutationObserver)
}

// recordDelegationAudit emits one structured receipt per child run. It reports
// what the host observed and what it refused to back, so an orchestration
// benchmark can separate real gains from extra tokens spent. delegationText is
// the parent-authored task before host framing, which is what makes the
// evidence-origin split a host record rather than a claim.
func recordDelegationAudit(ctx context.Context, summary evidence.ChildEvidenceSummary, claims writeclaim.WritePathSet, delegationText string) {
	audit := hostaudit.DelegationAudit{
		Depth:           SubagentDepth(ctx),
		ToolCalls:       len(summary.Receipts),
		MutationPaths:   summary.MutationPaths(),
		ClaimViolations: len(claimViolations(summary, claims)),
	}
	audit.Mutations = len(audit.MutationPaths)
	evidence.ClassifyEvidenceOrigin(&audit, delegationText, summary.EvidencePaths())
	_, sink, _, _ := CallContext(ctx)
	event.RecordDelegationAudit(sink, audit)
}

// AppendHostReceipts attaches the host's own attestation to a child's answer.
// A child cannot write, suppress, or contradict these lines. An empty block is
// omitted entirely, so read-only research children stay exactly as cheap as
// they were before.
func AppendHostReceipts(answer string, summary evidence.ChildEvidenceSummary, claims writeclaim.WritePathSet) string {
	block := formatHostReceipts(summary, claims)
	if block == "" {
		return answer
	}
	if strings.TrimSpace(answer) == "" {
		return block
	}
	return answer + "\n\n" + block
}

// claimViolations returns the mutations the host observed outside the write
// claim the child declared. Tool-level confinement already refuses these, so a
// non-empty result means a write reached the workspace through a surface the
// claim could not bind — the parent must not treat the run as scoped.
func claimViolations(summary evidence.ChildEvidenceSummary, claims writeclaim.WritePathSet) []string {
	if claims.Empty() {
		return nil
	}
	var out []string
	for _, p := range summary.MutationPaths() {
		if !claims.AllowsPath(p) {
			out = append(out, p)
		}
	}
	return out
}

// formatHostReceipts renders only what the host is willing to attest to: files
// the child really changed, and commands whose outcome the host observed.
// Ordinary reads and greps are excluded on purpose — they are not claims a
// parent has to adjudicate, and every rendered line costs parent context.
func formatHostReceipts(summary evidence.ChildEvidenceSummary, claims writeclaim.WritePathSet) string {
	changed := summary.MutationPaths()
	commands := hostReceiptCommands(summary)
	violations := claimViolations(summary, claims)
	if len(changed) == 0 && len(commands) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(HostReceiptsHeader)
	if len(changed) > 0 {
		b.WriteString("\n  changed: ")
		b.WriteString(joinBoundedReceipts(changed))
	}
	if len(commands) > 0 {
		b.WriteString("\n  commands: ")
		b.WriteString(joinBoundedReceipts(commands))
	}
	if len(violations) > 0 {
		b.WriteString("\n  " + HostReceiptsViolationLabel + ": ")
		b.WriteString(joinBoundedReceipts(violations))
	}
	if claims.Optimistic {
		b.WriteString("\n  " + HostReceiptsUnfencedLabel + ": " + hostReceiptsUnfencedText)
	}
	return b.String()
}

// hostReceiptCommands keeps only shell receipts carrying an outcome worth
// attesting: a host-classified verification, or a command that did not succeed.
func hostReceiptCommands(summary evidence.ChildEvidenceSummary) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range summary.Receipts {
		cmd := strings.TrimSpace(r.Command)
		outcome := hostReceiptOutcome(r)
		if cmd == "" || outcome == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		out = append(out, cmd+outcome)
	}
	return out
}

const HostReceiptsHeader = "Host receipts (recorded by the host as the sub-agent ran, not claimed by it):"

const HostReceiptsViolationLabel = "OUTSIDE DECLARED write_paths"

// HostReceiptsUnfencedLabel marks a run that held no path fence: it ran under
// optimistic claims, so shell and MCP writes were observed after the fact and
// were never confined to a path.
const HostReceiptsUnfencedLabel = "NOT FENCED"

const hostReceiptsUnfencedText = "optimistic sub-agent claims were in force; shell and MCP writes are only observed after the run, not confined to any path"

func joinBoundedReceipts(items []string) string {
	if len(items) <= hostReceiptsMaxItems {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:hostReceiptsMaxItems], ", ") +
		fmt.Sprintf(" (+%d more)", len(items)-hostReceiptsMaxItems)
}

func hostReceiptOutcome(r evidence.Receipt) string {
	var parts []string
	switch r.Verification {
	case evidence.VerificationPassed:
		parts = append(parts, "verification passed")
	case evidence.VerificationFailed:
		parts = append(parts, "verification failed")
	}
	switch {
	case r.ExitCode != nil && (*r.ExitCode != 0 || len(parts) > 0):
		parts = append(parts, fmt.Sprintf("exit %d", *r.ExitCode))
	case r.ExitCode == nil && !r.Success:
		parts = append(parts, "did not complete")
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// hostReceiptsMaxItems bounds each rendered line so a long child run cannot
// crowd the parent's context out with paths and commands.
const hostReceiptsMaxItems = 8
